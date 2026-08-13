package mahoraga

// The turns of the wheel that are built: 1 (baseline), 3 (PGO), 4 (ablation)
// and 5 (ordering). All four are general-tier — a pass schedule and a build
// flag cannot change what a program computes — which is why they come first:
// the whole search loop, the statistics and the recipe can be built and
// trusted before anything that could be wrong exists.

import (
	"fmt"
	"os"
	"path/filepath"

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

// Turn 3 — profile-guided rebuild.
//
// Rebuilds against a profile of the program doing the actual work on the
// actual input. bench/README.md found PGO to be noise across its suite — on
// flat single-file mains with nothing to devirtualize — but that is a claim
// about those programs, and it costs one build to re-ask about this one.
func (s *Search) turnPGO() error {
	if s.profile == "" {
		s.recipe.noteTurnSkipped()
		return nil
	}
	c := s.champion
	c.Label = "PGO from this run's profile"
	c.Build = codegen.BuildConfig{Flags: append(append([]string(nil), c.Build.Flags...), "-pgo="+s.profile)}
	c.Tier = General
	s.consider(3, "profile-guided rebuild", c, 1, 1)
	return nil
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
	return s.runCatalogue(6, "templated codegen edits", Guarded)
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
	return s.runCatalogue(8, "pinned specialisation", Pinned)
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
