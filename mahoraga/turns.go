package mahoraga

// The turns of the wheel, and the reconnaissance the later ones rest on.
//
// The general-tier turns came first and still read that way: 1 (baseline), 3
// (how the program is compiled), 4 (ablation) and 5 (ordering) cannot change
// what a program computes, so the whole search loop, the statistics and the
// recipe could be built and trusted before anything that could be wrong
// existed. The turns that commit — 2, 6, 7 and 8 — were added on top of a
// machine that was already known to measure honestly.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"domain/codegen"
	"domain/optimizer"
	"domain/runner"
)

// Turn 1 — baseline and reconnaissance.
//
// Establishes the number everything else is measured against, the spread that
// sets the noise floor, and the CPU profile turn 3 feeds to PGO. It adapts
// nothing.
func (s *Search) turnBaseline() error {
	base := baselineCandidate()
	bin, goSrc, err := s.buildWithSource(base, "baseline")
	if err != nil {
		return fmt.Errorf("building the program as `domain build` would: %w", err)
	}
	s.baselineGo = goSrc
	// The input's shape is measured before anything is timed. It cannot fail
	// the search — a fact that could not be read simply stands its catalogue
	// entries down — but it can only be wrong if the file changed underneath
	// us, which is worth knowing about.
	if f, ferr := readFacts(s.opts.Input); ferr == nil {
		s.facts = f
	}
	m := s.measure(bin, s.opts.baselineRuns())
	switch {
	case m.Failure == "wrong answer":
		return fmt.Errorf("the program does not produce the expected output — " +
			"there is nothing to adapt until it does")
	case m.Failure != "":
		return fmt.Errorf("measuring the baseline: %s", m.Failure)
	}

	s.baseline, s.champion, s.champMeasu = m, base, m
	s.baseBin, s.champBin = bin, bin

	// The noise floor is how precisely the baseline mean is known, relative to
	// itself — the standard *error*, not the deviation. The difference is a
	// factor of root-N and it matters: on a 20ms program the raw deviation put
	// the floor near 20%, which would have rejected every real improvement.
	if m.Mean > 0 {
		s.noiseFloor = float64(m.StdErr) / float64(m.Mean)
	}
	s.recipe.setBaseline(m, s.noiseFloor)
	s.emit(Event{Kind: CandidateMeasured, Turn: 1, TurnName: "baseline and reconnaissance",
		Candidate: base.Label, Measurement: m})

	// The profile is collected in its own run, untimed, so profiling overhead
	// cannot contaminate the baseline it is recorded beside.
	s.profile = filepath.Join(s.workDir, "cpu.pprof")
	if err := s.collectProfile(bin, s.profile); err != nil {
		s.profile = "" // turn 3 stands down rather than failing the search
	}
	// The allocation figures come from their own untimed run for the same
	// reason: reading MemStats stops the world. They are what turn 6 decides
	// the collector entry on — a program that ran no collections has nothing to
	// win by running fewer.
	s.collectAlloc(bin)
	// And the bindings and accumulator lengths, from a third untimed run of a
	// build that reports on itself. Same reasoning as the other two: what
	// turns 6 and 8 need is an account of what the program *held* and how far
	// its lists *grew*, and no amount of looking at the input file produces
	// either. See probe.go.
	s.collectProbeFacts()
	s.recipe.setFacts(s.facts)
	return nil
}

// collectAlloc runs the baseline once more with the allocation hook on and
// folds what it reports into the facts.
func (s *Search) collectAlloc(bin string) {
	opts := s.runnerOpts(1)
	opts.Alloc = true
	opts.KeepStdout = false
	results, err := runner.RaceContestants(
		[]runner.Contestant{{Label: "alloc", Argv: []string{bin}, Dir: filepath.Dir(s.opts.Input)}},
		runner.Input{Path: s.opts.Input}, opts)
	if err != nil || len(results) == 0 {
		return
	}
	a := results[0].Alloc
	if !a.Reported {
		return
	}
	s.facts.HeapReported = true
	s.facts.HeapSys, s.facts.TotalAlloc, s.facts.NumGC = a.HeapSys, a.TotalAlloc, a.NumGC
}

// collectProfile runs the binary once with the profile hook enabled.
func (s *Search) collectProfile(bin, out string) error {
	f, err := os.Open(s.opts.Input)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	cmd := profileCommand(bin, out)
	cmd.Stdin = f
	cmd.Dir = filepath.Dir(s.opts.Input)
	if err := cmd.Run(); err != nil {
		return err
	}
	info, err := os.Stat(out)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("the profile is empty")
	}
	return nil
}

// Turn 3 — how the program is compiled.
//
// The turn asks the one question the Domain compiler never gets to ask: given
// this program and this profile of it running, is `go build` being invoked the
// right way? Every candidate here is a toolchain setting, so all of them are
// general tier by construction — a compiler flag cannot change what a program
// computes — and the recipe records each one, since a binary must never end up
// faster for a reason nobody wrote down.
//
// Greedy, like the rest of the wheel: a flag that wins stays in the champion
// and the next flag is measured on top of it. That is what lets PGO and a
// larger inlining budget compound, which they do — the profile is what tells
// the inliner which of the newly-eligible calls are hot.
func (s *Search) turnRebuild() error {
	cands := make([]Candidate, 0, 4)

	// The profile first: it is the one candidate here with a measurement
	// behind it rather than a guess, and everything after it is then chosen
	// against a profiled champion. bench/README.md found PGO to be noise
	// across its suite — flat single-file mains with nothing to devirtualize —
	// but that is a claim about those programs, and it costs one build to
	// re-ask about this one.
	if s.profile != "" {
		c := s.champion
		c.Label = "PGO from this run's profile"
		c.Build = addFlags(c.Build, "-pgo="+s.profile)
		cands = append(cands, c)
	}
	// A larger inlining budget. The generator emits `consider … in` as nested
	// immediately-invoked closures and every builtin as a call, so a Domain
	// binary is unusually inlining-sensitive: the default budget stops part
	// way down a chain the source had no functions in at all.
	inline := s.champion
	inline.Label = "aggressive inlining (-gcflags=all=-l=4)"
	inline.Build = addFlags(inline.Build, "-gcflags=all=-l=4")
	cands = append(cands, inline)

	// The instruction set this machine actually has. Go's default target is
	// fifteen years old; v3 is AVX2, BMI2 and FMA, which is what the box under
	// the search almost certainly is.
	//
	// The cost is real and is not a correctness cost: a v3 binary does the
	// same thing on every input and refuses to start on an older CPU. That is
	// a portability claim rather than an input contract, so it is recorded in
	// the recipe's build environment where a reader will see it, and the tier
	// stays general because the tiers are about inputs.
	if lvl := amd64Level(); lvl != "" {
		c := s.champion
		c.Label = "GOAMD64=" + lvl + " (this machine's instruction set)"
		c.Build = addEnv(c.Build, "GOAMD64="+lvl)
		cands = append(cands, c)
	}

	for i, c := range cands {
		c.Tier = General
		s.consider(3, "how the program is compiled", c, i+1, len(cands))
	}
	return nil
}

// addFlags returns a build config with more toolchain flags, copying rather
// than appending in place: the champion's slice is shared with every candidate
// built from it, and appending to it would edit configurations already
// measured.
func addFlags(b codegen.BuildConfig, flags ...string) codegen.BuildConfig {
	out := b
	out.Flags = append(append([]string(nil), b.Flags...), flags...)
	return out
}

// addEnv is addFlags for the build environment.
func addEnv(b codegen.BuildConfig, env ...string) codegen.BuildConfig {
	out := b
	out.Env = append(append([]string(nil), b.Env...), env...)
	return out
}

// amd64Level names the highest GOAMD64 microarchitecture level this machine
// supports, or "" when the question does not arise.
//
// It is asked before building rather than discovered by building: a binary for
// an instruction set the CPU lacks dies with SIGILL, which the search would
// duly record as a rejected candidate having spent a build and a race on it.
// Reading the flags costs a file read.
func amd64Level() string {
	if runtime.GOARCH != "amd64" || runtime.GOOS != "linux" {
		return ""
	}
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	text := string(data)
	has := func(flag string) bool { return strings.Contains(text, " "+flag+" ") }
	// v4's AVX-512 is the level whose presence says least about whether it is
	// worth using — on several generations it costs frequency — so the search
	// asks for v3, which is where the cheap wins are.
	if has("avx2") && has("bmi2") && has("fma") {
		return "v3"
	}
	return ""
}

// Turn 4 — pass ablation.
//
// Turns each optimizer pass off in turn and measures. What it finds is passes
// that *hurt this program*, which the general optimizer cannot know because it
// is not allowed to know anything about the input.
//
// The search is greedy: a pass whose removal helps is removed for good, and
// the next pass is tested against the reduced schedule. That compounds
// removals that only pay together, and keeps the whole turn to one build per
// pass rather than one per subset.
func (s *Search) turnAblation() error {
	names := optimizer.PassNames()
	for i, name := range names {
		c := s.champion
		c.Label = "without " + name
		c.Schedule = scheduleWithout(s.champion.Schedule, name)
		c.Tier = General
		s.consider(4, "pass ablation", c, i+1, len(names))
	}
	return nil
}

// Turn 5 — pass ordering and round count.
//
// Not permutations: 29! is not a search space. A bounded number of seeded
// shuffles of the surviving passes, plus the round cap, hill-climbing from the
// champion. The spec records the expectation that this pays less than
// ablation — order matters less than presence when the passes cascade to a
// fixpoint — and the recipe will show whether that held.
func (s *Search) turnOrdering() error {
	const shuffles = 12
	candidates := make([]Candidate, 0, shuffles+4)

	// Round caps first: cheap, and a program whose cascade settles in two
	// rounds is paying for sixteen.
	for _, rounds := range []int{1, 2, 4, 8} {
		c := s.champion
		c.Label = fmt.Sprintf("max rounds %d", rounds)
		c.Schedule.MaxRounds = rounds
		c.Tier = General
		candidates = append(candidates, c)
	}

	names := s.champion.Schedule.Passes
	if names == nil {
		names = optimizer.PassNames()
	}
	for i := range shuffles {
		shuffled := append([]string(nil), names...)
		s.rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})
		c := s.champion
		c.Label = fmt.Sprintf("shuffled order #%d", i+1)
		c.Schedule.Passes = shuffled
		c.Tier = General
		candidates = append(candidates, c)
	}

	for i, c := range candidates {
		s.consider(5, "pass ordering", c, i+1, len(candidates))
	}
	return nil
}

// Turn 6 — templated codegen edits.
//
// The catalogue proper (catalogue.go): each entry a measured fact about the
// input plus a place in the emitted Go where that fact is worth something.
// This turn runs the entries that keep a fallback — general and guarded tier —
// so a binary it produces is still correct on any input, merely tuned for one.
// The entries with no fallback are turn 8's.
//
// Greedy, and greedy on purpose: an entry that wins stays in the champion, and
// the next entry is measured on top of it. Adaptations that only pay together
// compound that way, and the turn costs one build per entry rather than one per
// subset.
func (s *Search) turnCatalogue() error {
	if err := s.runCatalogue(6, "templated codegen edits", Guarded); err != nil {
		return err
	}
	return s.sizeAccumulators()
}

// sizeAccumulators reserves, for each list the generator could not estimate,
// the length the probe watched it reach.
//
// It sits beside the catalogue rather than in it for the same reason turn 8's
// constants do: there is no fixed number of these. The catalogue is a table of
// edits; this is one edit applied to however many accumulator sites a program
// has, and which sites those are is not known until the probe has run.
//
// The entry is guarded, and genuinely so: a capacity is a hint that `append`
// overrides, so a binary built with one is correct on every input. What it can
// be on a different input is *wasteful* — forty megabytes reserved for a run
// that produces ten elements — which is why the compiler does not guess it and
// why measuring is what licenses it.
func (s *Search) sizeAccumulators() error {
	sites := s.sizeableSites()
	if len(sites) == 0 {
		return nil
	}
	// All of them first, then one at a time.
	//
	// The exception to the wheel's usual one-change-per-candidate rule, and it
	// was bought with a wrong answer. `i15_generators` builds two five-million
	// element lists; reserving both is worth 23% and reserving either alone is
	// worth 11%, and on a program whose runs vary by a fifth, 11% is a coin
	// toss and 23% is not. The search duly rejected both halves of a win it
	// had found. Offering the whole set first is what makes the effect big
	// enough to measure; the individual candidates still follow, so a program
	// where only one site matters still attributes it to that site.
	cands := make([]Candidate, 0, len(sites)+1)
	if len(sites) > 1 {
		all := s.champion
		all.Label = fmt.Sprintf("reserve the measured length of all %d unestimated lists",
			len(sites))
		for _, site := range sites {
			all.Tuning.ListCapacities = withCapacity(all.Tuning.ListCapacities, site.Key, site.Length)
		}
		cands = append(cands, all)
	}
	for _, site := range sites {
		c := s.champion
		c.Label = fmt.Sprintf("reserve %s for the list at line %d (measured, not grown into)",
			plural(int64(site.Length), "element"), site.Line)
		c.Tuning.ListCapacities = withCapacity(c.Tuning.ListCapacities, site.Key, site.Length)
		cands = append(cands, c)
	}
	for i, c := range cands {
		c.Tier = Guarded
		s.consider(6, "templated codegen edits", c, i+1, len(cands))
	}
	return nil
}

// minSizeableLength is how long an accumulator has to have grown before
// reserving for it is worth a build. Below a few thousand elements the growth
// is a handful of copies of a small slice, which is not what anybody's program
// is spending its time on — and `append`'s doubling has already made those
// copies amortized-free.
const minSizeableLength = 4096

// maxSizedAccumulators bounds the turn, like turn 8's pins: each site is a
// build and a race, and the sites are tried longest-first so the ones dropped
// are the ones with least to win.
const maxSizedAccumulators = 4

// sizeableSites is the accumulators worth reserving for, longest first.
func (s *Search) sizeableSites() []ListSite {
	var out []ListSite
	for _, site := range s.facts.ListSites {
		if site.Length < minSizeableLength {
			continue
		}
		out = append(out, site)
	}
	slices.SortStableFunc(out, func(a, b ListSite) int {
		if a.Length != b.Length {
			return b.Length - a.Length
		}
		return strings.Compare(a.Key, b.Key)
	})
	if len(out) > maxSizedAccumulators {
		out = out[:maxSizedAccumulators]
	}
	return out
}

// withCapacity returns the map plus one entry, leaving the original alone —
// the champion's map is shared with every candidate built from it.
func withCapacity(m map[string]int, key string, n int) map[string]int {
	out := make(map[string]int, len(m)+1)
	for k, old := range m {
		out[k] = old
	}
	out[key] = n
	return out
}

// Turn 8 — pinned specialisation.
//
// The same catalogue, the entries that give up generality entirely: no
// fallback, no runtime check, and an assumption promoted into the recipe's
// contract. The verification happened here, against every byte of the real
// input, which is the whole reason the binary is allowed to carry none — and
// why `--verify` and `--replay` refuse a recipe like this on a different input.
//
// The spec has this turn entered only when the earlier ones left measurable
// time on the table. It is entered unconditionally instead: the catalogue's
// pinned entries are a handful of candidates, not a search, and "the pinned
// adaptation was tried and was inside the noise" is a more useful thing for a
// recipe to record than its absence.
func (s *Search) turnPinned() error {
	if s.opts.Tier < Pinned {
		// --tier guarded stops the wheel at seven, and says so rather than
		// reporting a turn that found nothing.
		s.recipe.noteTurnSkipped()
		return nil
	}
	if err := s.runCatalogue(8, "pinned specialisation", Pinned); err != nil {
		return err
	}
	return s.pinConstants()
}

// pinConstants is turn 8's other half: the `Consider` bindings a probe build
// watched hold one value for the whole run, emitted as that value.
//
// It is not a catalogue entry because there is no fixed number of them. The
// catalogue is a table of edits the compiler knows how to make; this is one
// edit applied to however many binding sites the program has, and which sites
// those are is not known until the probe has run.
//
// What the pin buys is not the binding's own arithmetic — `length(x)` is one
// load — but everything the Go compiler can do once the value is in front of
// it. The measured case is a modulus: `(i + 1) % l` against a local compiles
// to a hardware division, and against `16` it compiles to an AND.
//
// Greedy and ordered by how often the binding was evaluated, so the first
// build spent is the one most likely to pay. Each accepted pin stays in the
// champion, which is what lets two pins in the same loop compound.
func (s *Search) pinConstants() error {
	consts := s.pinnableConstants()
	if len(consts) == 0 {
		return nil
	}
	for i, c := range consts {
		cand := s.champion
		cand.Label = fmt.Sprintf("pin %s (evaluated %s)", c, plural(c.Calls, "time"))
		cand.Tier = Pinned
		// A copy per candidate: the champion's map is shared with every
		// candidate built from it, and writing into it would edit a
		// configuration that has already been measured.
		cand.Tuning.Constants = withConstant(cand.Tuning.Constants, c.Key, c.Value)
		cand.Pin = pinFor(c)
		s.consider(8, "pinned specialisation", cand, i+1, len(consts))
	}
	return nil
}

// maxPinnedConstants bounds the turn. Each pin is a build and a race, and a
// program with forty bindings is one where the fortieth is not what is costing
// it time — the ordering by evaluation count means the ones dropped here are
// always the least-evaluated.
const maxPinnedConstants = 6

// minPinnedCalls is how often a binding has to have been evaluated to be worth
// a build.
//
// One, and the reasoning behind the number is worth recording because the
// obvious answer is wrong. A binding evaluated *once* looks like the cheap
// case not worth a build — until you notice that the generator hoists a
// `Consider` out of the loop it was written in, so the binding a fifty
// thousand lap loop reads on every lap is evaluated exactly once. What a pin
// is worth has nothing to do with how often the binding is computed and
// everything to do with how often it is *read*, which is a number no probe at
// the definition can see.
const minPinnedCalls = 1

// pinnableConstants is the constants worth trying, most-evaluated first.
func (s *Search) pinnableConstants() []Constant {
	var out []Constant
	for _, c := range s.facts.Constants {
		if c.Calls < minPinnedCalls {
			continue
		}
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(a, b Constant) int {
		if a.Calls != b.Calls {
			return int(b.Calls - a.Calls)
		}
		return strings.Compare(a.Key, b.Key)
	})
	if len(out) > maxPinnedConstants {
		out = out[:maxPinnedConstants]
	}
	return out
}

// withConstant returns the map plus one entry, leaving the original alone.
func withConstant(m map[string]int64, key string, v int64) map[string]int64 {
	out := make(map[string]int64, len(m)+1)
	for k, old := range m {
		out[k] = old
	}
	out[key] = v
	return out
}

// pinFor is what a pinned constant assumes of a future input.
//
// The clause is unverifiable in general — there is no way to know what a
// binding will hold on an input nobody has run the program on — but the
// clause still says which binding and which value, because a refusal a reader
// can act on names the thing that would have to be re-checked.
func pinFor(c Constant) func(Facts, *Contract) {
	return func(_ Facts, ct *Contract) {
		ct.Unverifiable = append(ct.Unverifiable,
			fmt.Sprintf("the binding %q at line %d holds %d", c.Name, c.Line, c.Value))
	}
}

func plural(n int64, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Turn 7 — guarded specialisation.
//
// The same class of transformation as turn 6's, committing harder: a fast path
// for the shape that was observed, with the general path compiled in beside it
// and a run-time test choosing between them. The binary stays correct on any
// input and is faster on the one it was adapted to.
//
// This is the turn that makes the tiers mean something concrete rather than
// procedural. The ASCII grid builder appears twice in the catalogue: here with
// a per-line check and the rune decode kept as a fallback, and in turn 8 with
// both removed. `--tier guarded` buys the first; `--tier pinned` buys the
// second and gives up the ability to notice a different input.
func (s *Search) turnGuarded() error {
	return s.runCatalogue(7, "guarded specialisation", Guarded)
}

// runCatalogue tries the catalogue entries belonging to one turn.
//
// The split is by tier and shape together: turn 6 takes the parameter edits at
// or below guarded, turn 7 the guarded specialisations, and turn 8 everything
// pinned. An entry therefore runs in exactly one turn, which is what keeps the
// report readable — a win attributed to two turns would be a win counted twice.
func (s *Search) runCatalogue(turn int, turnName string, tier Tier) error {
	// The preconditions are asked of the champion's source, not the baseline's:
	// turns 4 and 5 may have changed the pass schedule, and with it whether the
	// backend still emits the shape an entry rewrites. Falling back to the
	// baseline's answers keeps the turn running when the champion cannot be
	// generated, which cannot happen — it has already been built — but is not
	// worth failing a search over if it ever does.
	goSrc, err := s.emitSource(s.champion)
	if err != nil {
		goSrc = s.baselineGo
	}
	if goSrc == "" {
		s.recipe.noteTurnSkipped()
		return nil
	}
	// Entries at or below the requested tier, filtered to the ones this turn
	// owns.
	var entries []appliedEntry
	for _, e := range catalogueFor(s.facts, goSrc, min(tier, s.opts.Tier)) {
		if !belongsToTurn(e.entry, turn) {
			continue
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil
	}
	for i, e := range entries {
		c := s.champion
		c.Label = e.Label
		c.Tier = e.Tier
		c.Pin = e.Pin
		e.Apply(s.facts, &c.Tuning)
		s.consider(turn, turnName, c, i+1, len(entries))
	}
	return nil
}

// Turn 2 — idle for this input.
//
// The spec calls this turn "dead for this input" and asks it to cut what never
// ran. A survey of the corpus says nothing ever does: a Domain pipeline is
// straight-line, every stage is entered, and the only nodes with a zero call
// count are ones the optimizer already fused away — which emit no code, so
// cutting them wins nothing. That is a finding about the language, not a gap in
// the turn, and it is recorded in the spec's status section.
//
// What *is* there, and is worth the interpreted run to find, is the second item
// on the spec's own list for this turn: a stage that ran and did nothing. A
// `Filter` that kept every one of two million elements still evaluated its
// predicate two million times and still copied the list. See recon.go for why
// the whitelist of primitives is short and why that is the safety argument.
//
// Pinned tier, and unavoidably so: a removed filter is removed, and a binary
// carrying no check cannot notice an input where the predicate would have
// failed. The contract goes in the recipe.
func (s *Search) turnIdle() error {
	if s.opts.Tier < Pinned {
		s.recipe.noteTurnSkipped()
		return nil
	}
	idle, err := s.findIdleStages()
	if err != nil {
		// Reconnaissance that could not be taken is not a failed search. The
		// reason is recorded so the recipe distinguishes "looked and found
		// nothing" from "could not look".
		s.recipe.noteReconFailed(err.Error())
		return nil
	}
	s.recipe.setIdleStages(idle)
	if len(idle) == 0 {
		return nil
	}
	// One candidate per stage, greedily: two idle filters are two eliminations,
	// and measuring them together would hide which one paid.
	for i, st := range idle {
		c := s.champion
		c.Label = fmt.Sprintf("drop %s at line %d — %s (%d elements)",
			st.Prim, st.Line, st.Why, st.Size)
		c.Tier = Pinned
		// The champion carries whatever earlier stages were kept, so appending
		// to a copy of its list is what makes the search greedy: an accepted
		// elision becomes part of the champion and the next stage is measured
		// on top of it.
		c.Tuning.ElideNodes = append(append([]string(nil), c.Tuning.ElideNodes...), st.Key)
		// There is no cheap way to re-establish "this filter would keep every
		// element" for an input nobody has run the program on, so this clause
		// goes in the contract's unverifiable list and binds the recipe to the
		// input it was measured from. Saying which clause did the binding is the
		// difference between a refusal a reader can act on and one they cannot.
		clause := fmt.Sprintf("the %s at line %d %s", st.Prim, st.Line, st.Why)
		c.Pin = func(_ Facts, ct *Contract) {
			ct.Unverifiable = append(ct.Unverifiable, clause)
		}
		s.consider(2, "idle for this input", c, i+1, len(idle))
	}
	return nil
}

// belongsToTurn says which turn of the wheel runs a catalogue entry.
//
// Tier and shape together, so an entry runs exactly once: a parameter edit is
// turn 6's, a specialisation with a fallback is turn 7's, and anything that
// gives up the fallback is turn 8's whatever shape it has.
func belongsToTurn(e entry, turn int) bool {
	switch turn {
	case 6:
		return e.Tier <= Guarded && e.Kind == kindEdit
	case 7:
		return e.Tier == Guarded && e.Kind == kindSpecialisation
	case 8:
		return e.Tier == Pinned
	}
	return false
}
