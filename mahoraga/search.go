package mahoraga

// The search: build a candidate, measure it against the champion, keep it or
// say why not.
//
// Everything that decides whether a number means anything lives here, because
// a tuner that cannot tell a win from noise is a random number generator with
// good typography. Three rules, all from the spec:
//
//   - **A noise floor**, taken from the baseline's own spread. A candidate
//     that does not clear it is rejected and *recorded as noise*, never
//     quietly kept.
//   - **Screen then confirm.** Most candidates lose, so they get a cheap
//     measurement first and a full one only if they look like winning.
//   - **The champion is re-measured at the end**, interleaved with the
//     baseline, because a champion picked across many noisy measurements is
//     partly picked *for* favourable noise.

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"

	"domain/codegen"
	"domain/optimizer"
	"domain/runner"
)

// Search is one adaptation run.
type Search struct {
	opts   Options
	oracle *Oracle
	rng    *rand.Rand
	report Reporter

	// domainBin is only needed if a turn ever measures the interpreter; the
	// adapted artifact is always compiled.
	domainBin string

	// workDir holds every binary the search builds, removed at the end.
	workDir string

	// champion is the best configuration found so far, and champMeasure what
	// it measured. baseline never changes.
	baseline   Measurement
	champion   Candidate
	champMeasu Measurement

	// noiseFloor is the fraction of the baseline mean that counts as
	// indistinguishable from it.
	noiseFloor float64

	// profile is the CPU profile turn 1 collected, for turn 3 to build against.
	// Empty when collection failed, which stands turn 3 down rather than
	// failing the search.
	profile string

	// baseBin and champBin are the two binaries every race carries alongside
	// the candidate. champBin is baseBin until something beats it, and the
	// race drops the duplicate when they are the same.
	baseBin  string
	champBin string

	// bestRatio is the champion's cost as a fraction of the baseline's, taken
	// from the race that accepted it — where both ran alternately, in the same
	// minute.
	//
	// It is the figure a display should use. Dividing the champion's own mean
	// by the baseline's is what once drew an 842ms candidate as "best" beside a
	// 713ms baseline, reading 0.85× under a tick: two numbers from two
	// different machines.
	bestRatio float64

	// facts and baselineGo are turn 1's other reconnaissance: what the input is
	// shaped like, and the Go the compiler actually emitted for it. The
	// catalogue's preconditions read both — see catalogue.go on why the emitted
	// source is the right place to ask some of these questions.
	facts      Facts
	baselineGo string

	recipe *Recipe

	// stop is closed when a caller asks the search to finish early. The wheel's
	// `q` key does this: a search with no time limit has to let the user say
	// "good enough" at any moment and walk away with everything found so far,
	// which is why this stops the search rather than aborting it — the champion
	// is still re-measured and still written.
	stop     chan struct{}
	stopOnce sync.Once

	// skip is the turn a caller has asked to abandon, or zero. Turn 4 tries one
	// candidate per optimizer pass and turn 5 sixteen orderings, so a turn that
	// is plainly going nowhere is worth minutes; abandoning one is not the same
	// as stopping, and the turns after it still run.
	skip atomic.Int64
}

// Stop asks the search to finish after the candidate in flight. It is safe to
// call more than once and from any goroutine.
func (s *Search) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// stopping reports whether Stop has been called.
func (s *Search) stopping() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// SkipTurn abandons the remaining candidates of turn n. Safe from any
// goroutine; a turn number that is not the one running is simply never reached.
func (s *Search) SkipTurn(n int) { s.skip.Store(int64(n)) }

// skipped reports whether the turn in progress has been abandoned.
func (s *Search) skipped(turn int) bool { return s.skip.Load() == int64(turn) }

// NewSearch prepares a search without running it.
func NewSearch(opts Options, report Reporter) (*Search, error) {
	oracle, err := NewOracle(opts.Expected)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{opts.Program, opts.Input} {
		if _, err := os.Stat(p); err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp("", "mahoraga-*")
	if err != nil {
		return nil, err
	}
	seed := opts.Seed
	if seed == 0 {
		seed = 1
	}
	return &Search{
		opts:      opts,
		oracle:    oracle,
		rng:       rand.New(rand.NewSource(seed)),
		report:    report,
		workDir:   dir,
		recipe:    newRecipe(opts),
		stop:      make(chan struct{}),
		bestRatio: 1,
	}, nil
}

// Close removes the search's working files.
func (s *Search) Close() {
	if s.workDir != "" {
		_ = os.RemoveAll(s.workDir)
	}
	runner.Cleanup()
}

func (s *Search) emit(e Event) {
	if s.report != nil {
		s.report.Report(e)
	}
}

// ---------------------------------------------------------------------------
// Building and measuring one candidate
// ---------------------------------------------------------------------------

// build compiles a candidate to its own binary and returns the path.
func (s *Search) build(c Candidate, name string) (string, error) {
	bin, _, err := s.buildWithSource(c, name)
	return bin, err
}

// buildWithSource is build, also handing back the Go it compiled.
func (s *Search) buildWithSource(c Candidate, name string) (bin, goSrc string, err error) {
	goSrc, err = s.emitSource(c)
	if err != nil {
		return "", "", err
	}
	bin = filepath.Join(s.workDir, name)
	if err := codegen.BuildBinaryWith(goSrc, bin, c.Build); err != nil {
		return "", "", err
	}
	return bin, goSrc, nil
}

// emitSource generates a candidate's Go without compiling it.
//
// The catalogue's preconditions are questions about what the backend chose —
// whether it guessed a capacity, whether it decodes runes — and the emitted
// program is where those have answers. Asking them costs a code generation and
// no build, which is why turn 6 can ask them of the *champion* rather than of
// the baseline: by the time it runs, turns 4 and 5 may have changed the pass
// schedule out from under the baseline's answers.
func (s *Search) emitSource(c Candidate) (string, error) {
	pipe, err := runner.LoadPipelineSchedule(s.opts.Program, c.Schedule)
	if err != nil {
		return "", err
	}
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{Tuning: c.Tuning})
	if err != nil {
		return "", fmt.Errorf("generating Go: %w", err)
	}
	return goSrc, nil
}

// measure runs a built binary and reports what it cost, checking every run's
// output. Correctness is checked on *every* run rather than once: a program
// that answers differently between runs is not a program worth timing, and
// this is the cheapest place to find that out.
func (s *Search) measure(bin string, runs int) Measurement {
	results, err := runner.RaceContestants(
		[]runner.Contestant{{Label: "candidate", Argv: []string{bin}, Dir: filepath.Dir(s.opts.Input)}},
		runner.Input{Path: s.opts.Input},
		s.runnerOpts(runs),
	)
	if err != nil {
		return Measurement{Failure: err.Error()}
	}
	r := &results[0]
	switch {
	case r.Err != nil:
		return Measurement{Failure: r.Err.Error()}
	case r.Timeout:
		return Measurement{Failure: "did not finish"}
	case r.ExitCode != 0:
		return Measurement{Failure: fmt.Sprintf("exit %d", r.ExitCode)}
	}
	return measurementFrom(r.Samples, s.oracle.Correct(r.Stdout))
}

// race measures the baseline, the champion and a candidate *interleaved*,
// alternating run by run, and reports all three.
//
// This is the single most important thing about how the search measures, and it
// took two goes to get right.
//
// The first version timed a candidate on its own and compared it against a
// champion figure taken minutes earlier. On a shared machine the drift between
// those two moments lands entirely on the candidate, and a real fifteen-percent
// win reads as "slower by ten". bench/README.md settled that long ago:
// alternate, so drift lands on both sides.
//
// The second version raced the champion and the candidate but left the
// *baseline* behind in turn 1, so the champion's standing against it was a
// product of ratios accumulated across turns, and any question about whether
// the machine had moved needed an anchor that no longer existed. Guarding that
// with a drift threshold rejected over half the races on an ordinary CI box —
// trading a rare false win for a constant false negative, which is worse.
//
// So the baseline runs in every race. Every figure the search compares is then
// measured in the same minute as the figure it is compared against, the
// champion's standing is measured rather than accumulated, and drift needs no
// guard because there is nothing left for it to corrupt. It costs one extra set
// of runs per candidate, against builds that dominate the wall clock anyway —
// and the baseline is dropped from the race when the champion *is* the baseline
// binary, which is most of a search that finds nothing.
func (s *Search) race(candBin string, runs int) (base, champ, cand Measurement) {
	dir := filepath.Dir(s.opts.Input)
	cs := []runner.Contestant{{Label: "baseline", Argv: []string{s.baseBin}, Dir: dir}}
	champIsBase := s.champBin == s.baseBin
	if !champIsBase {
		cs = append(cs, runner.Contestant{Label: "champion", Argv: []string{s.champBin}, Dir: dir})
	}
	cs = append(cs, runner.Contestant{Label: "candidate", Argv: []string{candBin}, Dir: dir})

	results, err := runner.RaceContestants(cs, runner.Input{Path: s.opts.Input}, s.runnerOpts(runs))
	if err != nil {
		m := Measurement{Failure: err.Error()}
		return m, m, m
	}
	base = s.resultMeasurement(&results[0])
	cand = s.resultMeasurement(&results[len(results)-1])
	champ = base
	if !champIsBase {
		champ = s.resultMeasurement(&results[1])
	}
	// The baseline is in every race, so how far the machine has moved since
	// turn 1 is observable rather than inferred. It is recorded and reported
	// and deliberately does not reject anything: every comparison this race
	// makes is internal to it, so a slow minute makes the search slower to run
	// and not wronger.
	s.noteDrift(base)
	return base, champ, cand
}

// noteDrift records how far the baseline binary has moved from what it cost in
// turn 1, so a search run on a busy machine says so.
func (s *Search) noteDrift(base Measurement) {
	if s.baseline.Mean <= 0 || base.Mean <= 0 {
		return
	}
	off := (float64(base.Mean) - float64(s.baseline.Mean)) / float64(s.baseline.Mean)
	if off < 0 {
		off = -off
	}
	if off > DriftNotable {
		s.recipe.noteDriftedRace()
	}
}

// resultMeasurement turns one side of a race into a measurement.
func (s *Search) resultMeasurement(r *runner.Result) Measurement {
	switch {
	case r.Err != nil:
		return Measurement{Failure: r.Err.Error()}
	case r.Timeout:
		return Measurement{Failure: "did not finish"}
	case r.ExitCode != 0:
		return Measurement{Failure: fmt.Sprintf("exit %d", r.ExitCode)}
	}
	return measurementFrom(r.Samples, s.oracle.Correct(r.Stdout))
}

// ---------------------------------------------------------------------------
// The accept/reject decision
// ---------------------------------------------------------------------------

// effectOf is how much faster cand is than champ, as a fraction. Positive is an
// improvement. Both figures come from the same race, which is what makes the
// subtraction mean anything.
func effectOf(champ, cand Measurement) float64 {
	if champ.Mean <= 0 || cand.Mean <= 0 {
		return 0
	}
	return (float64(champ.Mean) - float64(cand.Mean)) / float64(champ.Mean)
}

// DriftNotable is how far the baseline binary's cost may move from what it was
// in turn 1 before a race is worth mentioning as having been taken on a machine
// that was not the machine the baseline was measured on.
//
// It reports and does not reject. An earlier version rejected, on the reasoning
// that a race whose control had moved could not be read — and on a CI container
// that refused twenty-seven races out of fifty, which would have thrown away a
// genuine forty-one percent win the same program had found the day before.
// Since the baseline now runs in every race, a moved machine costs the search
// nothing but time: the comparisons are all internal to the race it is making.
const DriftNotable = 0.10

// threshold is the improvement a candidate must show to be worth confirming.
// The accept decision itself is Distinguishable, which compares the two
// measurements' uncertainties rather than a single global figure; this is the
// cheaper screen in front of it.
func (s *Search) threshold() float64 { return s.noiseFloor + s.opts.minEffect() }

// consider screens a candidate, confirms it if it looks like winning, and
// accepts or rejects it. It reports every outcome, including the rejections:
// a recipe that lists only wins hides the shape of the search.
func (s *Search) consider(turn int, turnName string, c Candidate, index, total int) bool {
	if s.stopping() || s.skipped(turn) {
		return false
	}
	s.emit(Event{Kind: CandidateStart, Turn: turn, TurnName: turnName,
		Candidate: c.Label, Index: index, Total: total})

	bin, err := s.build(c, fmt.Sprintf("t%d-c%d", turn, index))
	if err != nil {
		s.reject(turn, turnName, c, Measurement{Failure: firstLine(err.Error())},
			Measurement{}, 0, firstLine(err.Error()), false)
		return false
	}

	_, champS, screen := s.race(bin, s.opts.screenRuns())
	s.emit(Event{Kind: CandidateMeasured, Turn: turn, TurnName: turnName,
		Candidate: c.Label, Measurement: screen, Champion: champS,
		Effect: effectOf(champS, screen)})

	if !screen.OK() {
		s.reject(turn, turnName, c, screen, champS, 0, screen.Failure, false)
		return false
	}
	// The screen only has to look promising: it is a cheap measurement, so
	// requiring the full threshold here would discard candidates that a proper
	// measurement would have kept.
	if eff := effectOf(champS, screen); eff < s.threshold()/2 {
		why, unclear := s.rejectReasonFor(champS, screen)
		s.reject(turn, turnName, c, screen, champS, eff, why, unclear)
		return false
	}

	baseC, champC, confirm := s.race(bin, s.opts.baselineRuns())
	if !confirm.OK() {
		s.reject(turn, turnName, c, confirm, champC, 0, confirm.Failure, false)
		return false
	}
	// The real decision: is this measurably different from the champion, given
	// how precisely each is known — and both figures taken in the same minute,
	// alternating, so neither carries drift the other does not.
	eff := effectOf(champC, confirm)
	if !Distinguishable(champC, confirm, s.opts.minEffect()) {
		why, unclear := s.rejectReasonFor(champC, confirm)
		s.reject(turn, turnName, c, confirm, champC, eff, why, unclear)
		return false
	}

	s.champion, s.champMeasu, s.champBin = c, confirm, bin
	// Measured, not accumulated: the baseline ran in this same race, so the
	// champion's standing against it is one division rather than a product of
	// ratios taken across several minutes.
	if baseC.Mean > 0 && confirm.Mean > 0 {
		s.bestRatio = float64(confirm.Mean) / float64(baseC.Mean)
		s.recipe.setBestRatio(s.bestRatio)
	}
	s.recipe.addAdaptation(turn, turnName, c, confirm, eff, true, "")
	// A pinned adaptation that stuck adds its assumption to the recipe's
	// contract. This is what lets `--verify` accept a different input that
	// satisfies the same assumption instead of binding the recipe to one file
	// by hash — and, for the assumptions that cannot be re-established without
	// running the program, what makes the refusal say which one.
	if c.Pin != nil {
		s.recipe.pin(func(ct *Contract) { c.Pin(s.facts, ct) })
	}
	s.emit(Event{Kind: Adapted, Turn: turn, TurnName: turnName, Candidate: c.Label,
		Measurement: confirm, Champion: champC, Effect: eff, Tier: c.Tier,
		Schedule: c.Schedule, Build: c.Build, Tuning: c.Tuning})
	return true
}

// rejectReason names why a measurement did not earn acceptance, keeping three
// findings apart that a report conflating them would bury: slower, too small
// to matter, and *too noisy to tell*.
//
// The third is the one worth flagging separately. A candidate that measured
// 17% faster on a machine whose baseline is known to ±9% has not been shown to
// be faster — but it has not been shown not to be either, and "the measurement
// could not answer this" is a different thing to tell a user than "this did not
// work". The second return says which of those happened, so the verdict can
// suggest the one remedy that exists: more runs.
func (s *Search) rejectReasonFor(champ, m Measurement) (reason string, inconclusive bool) {
	eff := effectOf(champ, m)
	switch {
	case eff <= -s.opts.minEffect():
		return fmt.Sprintf("slower by %.1f%%", -eff*100), false
	case eff < s.opts.minEffect():
		return fmt.Sprintf("%.1f%% — below the %.1f%% worth recording",
			eff*100, s.opts.minEffect()*100), false
	}
	return fmt.Sprintf("%.1f%%, but inside the measurement's own uncertainty", eff*100), true
}

// reject records a candidate that did not earn acceptance. The effect is passed
// in rather than recomputed: it belongs to the race the rejection came from, and
// re-deriving it from the champion's stale figure is exactly the mistake the
// interleaved race exists to stop.
func (s *Search) reject(turn int, turnName string, c Candidate, m, champ Measurement, eff float64, reason string, inconclusive bool) {
	s.recipe.addAdaptation(turn, turnName, c, m, eff, false, reason)
	s.recipe.markInconclusive(inconclusive)
	s.emit(Event{Kind: Rejected, Turn: turn, TurnName: turnName, Candidate: c.Label,
		Measurement: m, Champion: champ, Effect: eff, Reason: reason, Tier: c.Tier})
}

// ---------------------------------------------------------------------------
// Running the wheel
// ---------------------------------------------------------------------------

// turn is one adaptation stage.
type turn struct {
	n     int
	name  string
	built bool
	run   func(*Search) error
}

// turns are the eight, in order. The ones not yet built report themselves as
// unimplemented rather than being silently absent, so the wheel's shape stays
// honest while the catalogue is filled in.
var turns = []turn{
	{1, "baseline and reconnaissance", true, (*Search).turnBaseline},
	{2, "idle for this input", true, (*Search).turnIdle},
	{3, "profile-guided rebuild", true, (*Search).turnPGO},
	{4, "pass ablation", true, (*Search).turnAblation},
	{5, "pass ordering", true, (*Search).turnOrdering},
	{6, "templated codegen edits", true, (*Search).turnCatalogue},
	{7, "guarded specialisation", true, (*Search).turnGuarded},
	{8, "pinned specialisation", true, (*Search).turnPinned},
}

// Run turns the wheel and returns the recipe.
func (s *Search) Run() (*Recipe, error) {
	for _, t := range turns {
		if t.n > s.opts.turns() || s.stopping() {
			break
		}
		s.emit(Event{Kind: TurnStart, Turn: t.n, TurnName: t.name})
		if err := t.run(s); err != nil {
			return nil, fmt.Errorf("turn %d (%s): %w", t.n, t.name, err)
		}
		s.emit(Event{Kind: TurnEnd, Turn: t.n, TurnName: t.name})
	}
	if err := s.finish(); err != nil {
		return nil, err
	}
	s.emit(Event{Kind: SearchDone, Measurement: s.champMeasu})
	return s.recipe, nil
}

// turnNotYet is a turn the catalogue has not reached. It is a real entry
// rather than a gap so the wheel still has eight handles and the report says
// plainly which ones are not built.
func (s *Search) turnNotYet() error {
	s.recipe.noteTurnSkipped()
	return nil
}

// finish re-measures the champion against the baseline, interleaved, and
// writes the adapted binary.
//
// The re-measurement is not ceremony. A champion selected across dozens of
// noisy measurements is partly selected *for* favourable noise, so the figure
// reported is this one — taken fresh, at full length, alternating with the
// baseline so drift lands on both.
func (s *Search) finish() error {
	champBin := s.champBin
	baseBin, err := s.build(baselineCandidate(), "baseline-final")
	if err != nil {
		return err
	}
	results, err := runner.RaceContestants([]runner.Contestant{
		{Label: "champion", Argv: []string{champBin}, Dir: filepath.Dir(s.opts.Input)},
		{Label: "baseline", Argv: []string{baseBin}, Dir: filepath.Dir(s.opts.Input)},
	}, runner.Input{Path: s.opts.Input}, s.runnerOpts(s.opts.baselineRuns()))
	if err != nil {
		return err
	}
	champ, base := &results[0], &results[1]
	if !s.oracle.Correct(champ.Stdout) {
		return fmt.Errorf("the champion answered wrongly on re-measurement — nothing was written")
	}
	final := measurementFrom(champ.Samples, true)
	rebase := measurementFrom(base.Samples, s.oracle.Correct(base.Stdout))
	s.recipe.setFinal(rebase, final)
	s.champMeasu = final

	// The re-measurement can overturn the search, and when it does the search
	// was wrong rather than the re-measurement.
	//
	// A champion is chosen across dozens of measurements, so it is partly
	// chosen *for* favourable noise — the more candidates tried, the more
	// likely the winner won by luck. This fresh, full-length, interleaved
	// measurement is the one that counts, and when it says the adapted binary
	// is no faster than the baseline, shipping the adapted one would be
	// shipping a slower program for no reason. So the baseline is written
	// instead and the recipe records that it was.
	write, label := champBin, s.opts.Out
	if !Distinguishable(rebase, final, s.opts.minEffect()) {
		write = baseBin
		s.recipe.revertToBaseline()
	}
	// The champion's CPU profile lives in the work directory Close is about to
	// delete, so a replay would find the -pgo= flag pointing at nothing. Copy it
	// beside the recipe while it still exists. Failing to is not worth failing
	// the search over — the binary is already correct — but the recipe must not
	// then claim a profile it cannot produce.
	if err := s.recipe.keepProfile(s.opts.Recipe); err != nil {
		s.recipe.Profile = ""
	}
	if label != "" {
		if err := copyFile(write, label); err != nil {
			return err
		}
		s.recipe.Artifact = label
	}
	return nil
}

// baselineCandidate is what `domain build` produces: the default schedule and
// the default build flags. Everything is measured against it.
func baselineCandidate() Candidate {
	return Candidate{Label: "baseline (domain build)", Tier: General}
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.WriteFile(to, data, 0o755); err != nil {
		return err
	}
	return nil
}

// profileCommand runs a built binary with the CPU profile hook enabled.
func profileCommand(bin, out string) *exec.Cmd {
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "DOMAIN_CPU_PROFILE="+out)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd
}

func firstLine(s string) string {
	for i := range len(s) {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// scheduleWithout is the champion's pass list minus one pass — the ablation
// candidate.
func scheduleWithout(base optimizer.Schedule, drop string) optimizer.Schedule {
	names := base.Passes
	if names == nil {
		names = optimizer.PassNames()
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != drop {
			out = append(out, n)
		}
	}
	next := base
	next.Passes = out
	return next
}
