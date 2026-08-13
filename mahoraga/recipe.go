package mahoraga

// The recipe: the durable artifact.
//
// The binary is fast; the recipe is *why*, and it is the half that survives a
// rebuild, shows up in a diff, and can be replayed. A binary that is 2× faster
// for unexplained reasons is a liability. It is designed to be committed
// beside the program.
//
// It records rejected adaptations as well as accepted ones, with the reason.
// A recipe that lists only wins hides the shape of the search — in particular
// it hides how much was tried and found to be inside the noise, which is the
// most useful thing to know when deciding whether to trust the rest.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"domain/codegen"
	"domain/optimizer"
)

// RecipeVersion is bumped when the shape changes incompatibly.
const RecipeVersion = 1

// Recipe is the record of one adaptation.
type Recipe struct {
	Version int    `json:"version"`
	Program string `json:"program"`
	Input   string `json:"input"`

	// Expected is the path of the expected-output file, not its contents.
	// The answer is not recorded here for the same reason it never reaches the
	// code generator.
	Expected  string    `json:"expected"`
	AdaptedAt time.Time `json:"adapted_at"`
	Artifact  string    `json:"artifact,omitempty"`

	InputFingerprint Fingerprint `json:"input_fingerprint"`

	Baseline      MeasurementJSON `json:"baseline"`
	Champion      MeasurementJSON `json:"champion"`
	Speedup       float64         `json:"speedup"`
	NoiseFloorPct float64         `json:"noise_floor_pct"`

	// Final is the fresh re-measurement of champion against baseline, taken
	// after the search. It, not the running champion figure, is what the
	// verdict reports: a champion picked across many noisy measurements is
	// partly picked for favourable noise.
	FinalBaseline MeasurementJSON `json:"final_baseline"`

	Adaptations []Adaptation `json:"adaptations"`
	Schedule    ScheduleJSON `json:"schedule"`
	Build       BuildJSON    `json:"build"`

	// Tuning is what the code generator was told about the input. It is the
	// half of a rebuild that lives nowhere else — a pass schedule and a build
	// flag can be re-derived from the command line, a measured element count
	// cannot — so a replay that dropped it would silently produce a different
	// binary from the one that was measured.
	Tuning TuningJSON `json:"tuning,omitempty"`

	// Facts are the measurements the catalogue's preconditions were decided on,
	// recorded so a reader can check the reasoning rather than take it.
	Facts FactsJSON `json:"input_facts,omitempty"`

	// IdleStages are the stages turn 2 watched do nothing, whether or not
	// removing them turned out to be worth it. A reader deciding whether to
	// trust a removed filter wants to see what the observation was.
	IdleStages []IdleStageJSON `json:"idle_stages,omitempty"`

	// ReconNote says why the interpreted reconnaissance run did not happen,
	// when it did not. "Looked and found nothing" and "could not look" are
	// different reports and the recipe must not conflate them.
	ReconNote string `json:"reconnaissance_note,omitempty"`

	// Contract is what a pinned adaptation requires of an input. It is empty
	// unless something pinned was kept.
	Contract Contract `json:"contract,omitempty"`

	Tier string `json:"tier"`

	// TurnsSkipped counts turns that are not yet built, so a reader can tell
	// "found nothing" from "did not look".
	TurnsSkipped int `json:"turns_not_implemented"`

	// Profile is the CPU profile the champion was built against, as a filename
	// beside the recipe.
	//
	// It is a separate field rather than a `-pgo=` entry in Build.Flags because
	// the path the search used pointed into a temp directory it deletes on the
	// way out: a recipe recording that flag verbatim was a recipe that could
	// never be replayed. Recording the basename and resolving it against the
	// recipe's own directory is what lets the pair be committed together and
	// moved together.
	Profile string `json:"profile,omitempty"`

	// Inconclusive counts candidates that measured faster than the champion by
	// more than the minimum effect and still could not be distinguished from it.
	//
	// They are the search's own account of its measurement budget: on a quiet
	// machine this is zero, and a large number means the runs were too few or
	// the box too noisy to answer the questions that were asked. That is a
	// different report from "nothing worked", and the remedy — more runs — is
	// one the user can act on.
	Inconclusive int `json:"inconclusive,omitempty"`

	// Reverted records that the search's champion failed its final
	// re-measurement and the baseline was written instead.
	Reverted bool `json:"reverted_to_baseline,omitempty"`

	// OverturnedChampion is what the rejected champion measured, kept so the
	// report can say what was turned down and by how much.
	//
	// It exists because Champion and Speedup must describe the binary that was
	// actually written, and after a revert that is the baseline. They used not
	// to: a reverted recipe recorded speedup 1.016 and a champion mean of
	// 704ms for a file that was byte-for-byte the baseline build. Two numbers
	// about a binary nobody has is worse than no numbers at all.
	OverturnedChampion MeasurementJSON `json:"overturned_champion,omitempty"`

	// BestRatio is the champion's cost as a fraction of the baseline's, taken
	// as the product of the ratios each accepted race measured. It is the
	// drift-free version of Champion/Baseline and is what a display should
	// use; see Search.bestRatio.
	BestRatio float64 `json:"best_ratio,omitempty"`

	// DriftedRaces counts races taken while the baseline binary was costing
	// noticeably more or less than it did in turn 1. It rejects nothing — the
	// baseline runs in every race, so every comparison is internal to the race
	// making it — and is the search's account of how quiet the machine was.
	DriftedRaces int `json:"drifted_races,omitempty"`

	// dir is where this recipe lives, for resolving Profile. It is not part of
	// the file: a recipe that recorded its own location would stop being
	// correct the moment anyone moved it.
	dir string

	// searchProfile is where the champion's profile sat *during* the search, in
	// a temp directory. keepProfile copies it beside the recipe and sets
	// Profile; until then there is nothing durable to record.
	searchProfile string
}

// Fingerprint identifies the input a recipe was adapted to.
type Fingerprint struct {
	Bytes int64 `json:"bytes"`
	// Lines is how many lines the file has, counted the way a reader counts
	// them and the way Facts.Segments does: the pieces a split by newline
	// produces, which is one more than the separators between them.
	//
	// It used to be the raw count of '\n' bytes, which put "lines: 1" and
	// "segments: 2" in the same recipe describing the same 55-byte file. Both
	// were right by their own definition and the pair was useless.
	Lines  int    `json:"lines"`
	SHA256 string `json:"sha256"`
}

type MeasurementJSON struct {
	MeanNanos   int64 `json:"mean_nanos"`
	MinNanos    int64 `json:"min_nanos"`
	StdDevNanos int64 `json:"stddev_nanos"`
	Runs        int   `json:"runs"`
}

type ScheduleJSON struct {
	Passes    []string `json:"passes,omitempty"`
	MaxRounds int      `json:"max_rounds,omitempty"`
}

type BuildJSON struct {
	Flags []string `json:"flags,omitempty"`
	Env   []string `json:"env,omitempty"`
}

// TuningJSON is codegen.Tuning as it appears in a recipe. It is a separate
// type rather than the codegen one embedded, so the recipe's field names and
// the compiler's struct can move independently — a recipe is a file format.
type TuningJSON struct {
	ListCapacity     int      `json:"list_capacity,omitempty"`
	ASCIIText        bool     `json:"ascii_text,omitempty"`
	GCPercent        int      `json:"gc_percent,omitempty"`
	MemoryLimitBytes int64    `json:"memory_limit_bytes,omitempty"`
	ElideNodes       []string `json:"elide_nodes,omitempty"`
}

func tuningJSON(t codegen.Tuning) TuningJSON {
	return TuningJSON{
		ListCapacity: t.ListCapacity, ASCIIText: t.ASCIIText,
		GCPercent: t.GCPercent, MemoryLimitBytes: t.MemoryLimitBytes,
		ElideNodes: t.ElideNodes,
	}
}

func (t TuningJSON) tuning() codegen.Tuning {
	return codegen.Tuning{
		ListCapacity: t.ListCapacity, ASCIIText: t.ASCIIText,
		GCPercent: t.GCPercent, MemoryLimitBytes: t.MemoryLimitBytes,
		ElideNodes: t.ElideNodes,
	}
}

// IdleStageJSON is one stage measured doing nothing to its value.
type IdleStageJSON struct {
	Key   string `json:"key"`
	Prim  string `json:"prim"`
	Line  int    `json:"line"`
	Why   string `json:"why"`
	Size  int    `json:"size,omitempty"`
	Calls int    `json:"calls,omitempty"`
}

// Contract is what a pinned recipe requires of the input it is run on.
//
// It exists because "pinned" was too blunt. A pinned adaptation used to bind a
// recipe to one input by SHA-256, which is correct and needlessly strict: the
// assumption behind "no UTF-8 decoding" is *every byte is one rune*, and any
// number of other inputs satisfy it. The contract records the assumption itself
// so a different input can be checked against it.
//
// The split that matters is between clauses that can be re-established by
// looking at a candidate input, and clauses that cannot. Whether a file is all
// ASCII is a property of the file. Whether a Filter would keep every element of
// it is a property of running the program, and there is no cheap way to answer
// it — so a recipe carrying one of those is pinned to the exact input it was
// measured from, and says so.
type Contract struct {
	// ASCII requires every byte of the input to decode as one rune. Checkable.
	ASCII bool `json:"ascii,omitempty"`

	// MinSegments requires the input to have at least this many newline-
	// separated pieces. Checkable.
	MinSegments int `json:"min_segments,omitempty"`

	// Unverifiable lists assumptions that cannot be re-established without
	// running the program on the input. Any entry here binds the recipe to the
	// exact input it was adapted to.
	Unverifiable []string `json:"unverifiable,omitempty"`
}

// Empty reports whether this contract constrains nothing.
func (c Contract) Empty() bool {
	return !c.ASCII && c.MinSegments == 0 && len(c.Unverifiable) == 0
}

// Check evaluates the contract against a measured input, returning the clauses
// that fail. A satisfied contract returns nothing.
func (c Contract) Check(f Facts) []string {
	var bad []string
	for _, u := range c.Unverifiable {
		bad = append(bad, u+" — there is no way to re-establish this without running the program")
	}
	if c.ASCII && !f.ASCII {
		bad = append(bad, "this input is not all ASCII, and the binary decodes no runes")
	}
	if c.MinSegments > 0 && f.Segments < c.MinSegments {
		bad = append(bad, fmt.Sprintf("this input has %d segments; the binary assumes at least %d",
			f.Segments, c.MinSegments))
	}
	return bad
}

// FactsJSON is what the search measured about the input.
type FactsJSON struct {
	Bytes       int64  `json:"bytes,omitempty"`
	Segments    int    `json:"segments,omitempty"`
	ASCII       bool   `json:"ascii,omitempty"`
	LongestLine int    `json:"longest_line,omitempty"`
	HeapSys     uint64 `json:"heap_sys,omitempty"`
	TotalAlloc  uint64 `json:"total_alloc,omitempty"`
	NumGC       uint32 `json:"num_gc,omitempty"`
}

// Adaptation is one thing tried, with what it measured and whether it stuck.
type Adaptation struct {
	Turn      int             `json:"turn"`
	TurnName  string          `json:"turn_name"`
	ID        string          `json:"id"`
	Tier      string          `json:"tier"`
	EffectPct float64         `json:"effect_pct"`
	Kept      bool            `json:"kept"`
	Reason    string          `json:"reason,omitempty"`
	Measured  MeasurementJSON `json:"measured"`
}

func newRecipe(opts Options) *Recipe {
	r := &Recipe{
		Version:   RecipeVersion,
		Program:   opts.Program,
		Input:     opts.Input,
		Expected:  opts.Expected,
		AdaptedAt: time.Now().UTC(),
		Tier:      opts.Tier.String(),
	}
	r.InputFingerprint = fingerprint(opts.Input)
	r.dir = filepath.Dir(opts.Recipe)
	return r
}

// splitPGO separates the -pgo= entry from the rest of a build flag list.
// The two are recorded separately: everything else is a toolchain flag that
// means the same thing on any machine, and this one is a path.
func splitPGO(flags []string) (rest []string, profile string) {
	for _, f := range flags {
		if p, ok := strings.CutPrefix(f, "-pgo="); ok {
			profile = p
			continue
		}
		rest = append(rest, f)
	}
	return rest, profile
}

func fingerprint(path string) Fingerprint {
	f, err := os.Open(path)
	if err != nil {
		return Fingerprint{}
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	var bytes int64
	// One more line than there are separators, and none at all for an empty
	// file. See Fingerprint.Lines.
	newlines, lastByte := 0, byte(0)
	buf := make([]byte, 64<<10)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			bytes += int64(n)
			for _, b := range buf[:n] {
				if b == '\n' {
					newlines++
				}
			}
			lastByte = buf[n-1]
		}
		if err != nil {
			break
		}
	}
	lines := newlines + 1
	switch {
	case bytes == 0:
		lines = 0
	case lastByte == '\n':
		// A trailing newline terminates the last line rather than starting an
		// empty one, which is how every text tool counts and how dmReadSource
		// treats it.
		lines = newlines
	}
	return Fingerprint{Bytes: bytes, Lines: lines, SHA256: hex.EncodeToString(h.Sum(nil))}
}

func measurementJSON(m Measurement) MeasurementJSON {
	return MeasurementJSON{
		MeanNanos:   m.Mean.Nanoseconds(),
		MinNanos:    m.Min.Nanoseconds(),
		StdDevNanos: m.StdDev.Nanoseconds(),
		Runs:        m.Runs,
	}
}

func (r *Recipe) setBaseline(m Measurement, noiseFloor float64) {
	r.Baseline = measurementJSON(m)
	r.Champion = r.Baseline
	r.NoiseFloorPct = noiseFloor * 100
	r.Speedup = 1
}

func (r *Recipe) addAdaptation(turn int, turnName string, c Candidate, m Measurement, effect float64, kept bool, reason string) {
	r.Adaptations = append(r.Adaptations, Adaptation{
		Turn: turn, TurnName: turnName, ID: c.Label, Tier: c.Tier.String(),
		EffectPct: effect * 100, Kept: kept, Reason: reason, Measured: measurementJSON(m),
	})
	if kept {
		r.Champion = measurementJSON(m)
		r.Schedule = ScheduleJSON{Passes: c.Schedule.Passes, MaxRounds: c.Schedule.MaxRounds}
		flags, profile := splitPGO(c.Build.Flags)
		r.Build = BuildJSON{Flags: flags, Env: c.Build.Env}
		r.searchProfile = profile
		r.Tuning = tuningJSON(c.Tuning)
	}
}

func (r *Recipe) noteTurnSkipped() { r.TurnsSkipped++ }

// noteDriftedRace counts a race taken while the baseline binary was measuring
// far from what it measured in turn 1 — the machine's own account of how quiet
// it was, and nothing more: the race's comparisons are internal to it.
func (r *Recipe) noteDriftedRace() { r.DriftedRaces++ }

// setBestRatio records the champion's cost as a fraction of the baseline's,
// composed from the ratios each accepted race measured.
func (r *Recipe) setBestRatio(ratio float64) { r.BestRatio = ratio }

// noteReconFailed records that the interpreted reconnaissance run could not be
// taken, and why.
func (r *Recipe) noteReconFailed(why string) { r.ReconNote = why }

// pin adds a clause to the recipe's contract.
func (r *Recipe) pin(add func(*Contract)) { add(&r.Contract) }

// setIdleStages records what turn 2 observed, findings and all.
func (r *Recipe) setIdleStages(stages []IdleStage) {
	r.IdleStages = nil
	for _, st := range stages {
		r.IdleStages = append(r.IdleStages, IdleStageJSON{
			Key: st.Key, Prim: st.Prim, Line: st.Line,
			Why: st.Why, Size: st.Size, Calls: st.Calls,
		})
	}
}

// markInconclusive counts a candidate the measurement could not decide.
func (r *Recipe) markInconclusive(yes bool) {
	if yes {
		r.Inconclusive++
	}
}

// setFacts records the reconnaissance the catalogue's preconditions were
// decided on.
func (r *Recipe) setFacts(f Facts) {
	r.Facts = FactsJSON{
		Bytes: f.Bytes, Segments: f.Segments, ASCII: f.ASCII, LongestLine: f.LongestLine,
	}
	if f.HeapReported {
		r.Facts.HeapSys, r.Facts.TotalAlloc, r.Facts.NumGC = f.HeapSys, f.TotalAlloc, f.NumGC
	}
}

// setFinal records the fresh champion-versus-baseline re-measurement, and it
// is this speedup the report publishes.
func (r *Recipe) setFinal(baseline, champion Measurement) {
	r.FinalBaseline = measurementJSON(baseline)
	r.Champion = measurementJSON(champion)
	if champion.Mean > 0 && baseline.Mean > 0 {
		r.Speedup = float64(baseline.Mean) / float64(champion.Mean)
	}
}

// revertToBaseline discards the search's champion in favour of the baseline,
// after the final re-measurement failed to confirm it.
//
// Reverted is set only when there was actually something to overturn. A search
// that adapted nothing has the baseline as its champion already, so its
// re-measurement is a comparison of the baseline with itself — a tie, and not
// a finding. Reporting that as "the search thought otherwise" would put words
// in its mouth.
func (r *Recipe) revertToBaseline() {
	demoted := 0
	for i := range r.Adaptations {
		if r.Adaptations[i].Kept {
			r.Adaptations[i].Kept = false
			r.Adaptations[i].Reason = "did not survive the final re-measurement"
			demoted++
		}
	}
	if demoted == 0 {
		return
	}
	r.Reverted = true
	// Champion and Speedup describe the artifact, and the artifact is now the
	// baseline. What the rejected champion measured is kept beside them rather
	// than in place of them.
	r.OverturnedChampion = r.Champion
	r.Champion = r.FinalBaseline
	r.Speedup = 1
	// BestRatio describes the champion too, and the champion is now the
	// baseline. Leaving 0.887 here would have a display draw a win beside a
	// binary that is the baseline build, which is the same class of bug as the
	// speedup this replaced.
	r.BestRatio = 1
	r.Schedule = ScheduleJSON{}
	r.Build = BuildJSON{}
	r.Tuning = TuningJSON{}
	r.Contract = Contract{}
	r.Profile, r.searchProfile = "", ""
}

// HighestTier is how far the adaptations that actually stuck commit.
//
// It is not the same as Tier, which records how far the search was *allowed*
// to go. A search run at the default pinned tier that kept only general and
// guarded adaptations has produced a binary that is correct on any input, and
// warning about a pin nobody made would be the report inventing a cost.
func (r *Recipe) HighestTier() Tier {
	high := General
	for _, a := range r.Kept() {
		// An empty tier is not a pin. ParseTier reads "" as pinned because that
		// is what an empty --tier flag means, and reusing it here would make
		// every recipe written before tiers were recorded look like a pin.
		if a.Tier == "" {
			continue
		}
		if t, ok := ParseTier(a.Tier); ok && t > high {
			high = t
		}
	}
	return high
}

// Kept returns the adaptations that stuck.
func (r *Recipe) Kept() []Adaptation {
	var out []Adaptation
	for _, a := range r.Adaptations {
		if a.Kept {
			out = append(out, a)
		}
	}
	return out
}

// Improved reports whether the adapted binary actually beat the baseline on
// the final re-measurement.
//
// It requires an adaptation to have been kept, and that is not a formality: a
// search that adapted nothing has the *baseline* as its champion, so the two
// sides of the final measurement are the same configuration built twice, and
// any difference between them is noise by construction. Without this check a
// search that did nothing at all would report a speedup — which it did, before
// the check existed.
func (r *Recipe) Improved() bool {
	return !r.Reverted && len(r.Kept()) > 0 && r.Speedup > 1+r.NoiseFloorPct/100
}

// Overturned reports whether the search believed it had won and the final
// re-measurement disagreed.
func (r *Recipe) Overturned() bool { return r.Reverted }

// keepProfile copies the champion's CPU profile beside the recipe, so a replay
// has the file the -pgo= flag names. Returning quietly when there is nothing to
// copy is the common case: most searches keep no profile-guided build.
func (r *Recipe) keepProfile(recipePath string) error {
	if r.searchProfile == "" {
		return nil
	}
	stem := strings.TrimSuffix(filepath.Base(recipePath), ".json")
	name := stem + ".pprof"
	if err := copyFile(r.searchProfile, filepath.Join(filepath.Dir(recipePath), name)); err != nil {
		return err
	}
	r.dir, r.Profile = filepath.Dir(recipePath), name
	return nil
}

// profilePath resolves the recorded profile against the recipe's own directory.
func (r *Recipe) profilePath() string {
	if r.Profile == "" {
		return ""
	}
	return filepath.Join(r.dir, r.Profile)
}

// Write saves the recipe as indented JSON.
func (r *Recipe) Write(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// ReadRecipe loads a recipe.
func ReadRecipe(path string) (*Recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Recipe
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	r.dir = filepath.Dir(path)
	if r.Version != RecipeVersion {
		return nil, fmt.Errorf("%s is a version %d recipe; this domain reads version %d",
			path, r.Version, RecipeVersion)
	}
	return &r, nil
}

// Candidate reconstructs the build configuration a recipe describes, so a
// replay produces the same binary the search did.
func (r *Recipe) Candidate() Candidate {
	flags := append([]string(nil), r.Build.Flags...)
	if p := r.profilePath(); p != "" {
		flags = append(flags, "-pgo="+p)
	}
	return Candidate{
		Label:    "replayed from recipe",
		Schedule: optimizer.Schedule{Passes: r.Schedule.Passes, MaxRounds: r.Schedule.MaxRounds},
		Build:    codegen.BuildConfig{Flags: flags, Env: r.Build.Env},
		Tuning:   r.Tuning.tuning(),
	}
}

// WriteJSON renders the recipe to a writer, for `--json`.
//
// Not named WriteTo: that name belongs to io.WriterTo, whose contract is a
// byte count and an error, and vet is right to insist the two not be confused.
func (r *Recipe) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
