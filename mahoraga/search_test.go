package mahoraga

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"domain/optimizer"
)

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
}

// arena writes a program, an input and the expected output, and returns them.
func arena(t *testing.T) (prog, input, expected string) {
	t.Helper()
	dir := t.TempDir()
	src := `Cursed Energy: in.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return write("prog.domain", src), write("in.txt", "1\n2\n3\n4\n"), write("want.txt", "10\n")
}

// The oracle is the wall between the expected answer and the code generator.
// What it must do is answer the question and nothing else.
func TestOracle(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "want.txt")
	if err := os.WriteFile(want, []byte("42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := NewOracle(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []string{"42", "42\n", "42\n\n", "42\r\n"} {
		if !o.Correct([]byte(got)) {
			t.Errorf("Correct(%q) = false; a trailing newline is not a wrong answer", got)
		}
	}
	for _, got := range []string{"43", "", "4 2", " 42"} {
		if o.Correct([]byte(got)) {
			t.Errorf("Correct(%q) = true", got)
		}
	}
	if _, err := NewOracle(filepath.Join(dir, "nope")); err == nil {
		t.Error("a missing expected-output file was accepted")
	}
}

// The whole search over a tiny program. Only turn 1 runs, so it is one build
// and a handful of runs — fast enough for CI, and it exercises the baseline,
// the noise floor, the final re-measurement, the artifact and the recipe.
func TestSearchEndToEnd(t *testing.T) {
	requireGo(t)
	prog, input, expected := arena(t)
	out := filepath.Join(t.TempDir(), "adapted")

	var events []Event
	s, err := NewSearch(Options{
		Program: prog, Input: input, Expected: expected,
		Turns: 1, BaselineRuns: 3, ScreenRuns: 2, Out: out,
	}, ReporterFunc(func(e Event) { events = append(events, e) }))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}

	if r.Baseline.MeanNanos <= 0 {
		t.Error("no baseline was measured")
	}
	if r.FinalBaseline.MeanNanos <= 0 {
		t.Error("the champion was not re-measured against the baseline")
	}
	// Turn 1 adapts nothing, so the baseline must still be champion — and the
	// command must be able to say so without inventing a win.
	if r.Improved() {
		t.Errorf("a search that ran only the baseline turn reported an improvement (%.2f×)", r.Speedup)
	}
	// Seven turns were not run, and the recipe must distinguish "found
	// nothing" from "did not look".
	if r.TurnsSkipped != 0 {
		t.Errorf("turns skipped = %d; turns beyond --turns are not run, not skipped", r.TurnsSkipped)
	}

	// The artifact exists, is executable, and answers correctly.
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("no adapted binary written: %v", err)
	}
	in, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	cmd := exec.Command(out)
	cmd.Stdin = in
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the adapted binary: %v", err)
	}
	if strings.TrimSpace(string(got)) != "10" {
		t.Errorf("the adapted binary printed %q, want 10", got)
	}

	// The event stream is what the wheel will consume, so it has to carry the
	// turn and the measurement.
	var sawTurn, sawMeasured, sawDone bool
	for _, e := range events {
		switch e.Kind {
		case TurnStart:
			sawTurn = true
		case CandidateMeasured:
			sawMeasured = true
		case SearchDone:
			sawDone = true
		}
	}
	if !sawTurn || !sawMeasured || !sawDone {
		t.Errorf("event stream incomplete: turn=%v measured=%v done=%v", sawTurn, sawMeasured, sawDone)
	}
}

// A program that does not produce the expected output has nothing to adapt,
// and saying so is more useful than tuning the wrong answer for an hour.
func TestSearchRefusesAWrongProgram(t *testing.T) {
	requireGo(t)
	prog, input, _ := arena(t)
	wrong := filepath.Join(filepath.Dir(prog), "wrong.txt")
	if err := os.WriteFile(wrong, []byte("999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewSearch(Options{
		Program: prog, Input: input, Expected: wrong,
		Turns: 1, BaselineRuns: 2, ScreenRuns: 1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.Run(); err == nil {
		t.Fatal("a program that answers wrongly was accepted as a baseline")
	} else if !strings.Contains(err.Error(), "expected output") {
		t.Errorf("the error does not explain the problem: %v", err)
	}
}

func TestSearchRejectsMissingFiles(t *testing.T) {
	prog, input, expected := arena(t)
	missing := filepath.Join(t.TempDir(), "nope")
	for _, o := range []Options{
		{Program: missing, Input: input, Expected: expected},
		{Program: prog, Input: missing, Expected: expected},
		{Program: prog, Input: input, Expected: missing},
	} {
		if _, err := NewSearch(o, nil); err == nil {
			t.Errorf("a missing file was accepted: %+v", o)
		}
	}
}

// The recipe is meant to be committed beside the program, so it has to survive
// a round trip and rebuild the same configuration.
func TestRecipeRoundTrip(t *testing.T) {
	prog, input, expected := arena(t)
	r := newRecipe(Options{Program: prog, Input: input, Expected: expected, Tier: Pinned})
	r.setBaseline(measurementFrom(msSamples(10, 10, 10, 10), true), 0.01)
	r.addAdaptation(4, "pass ablation",
		Candidate{Label: "without fuseMapMap", Tier: General,
			Schedule: scheduleWithout(scheduleAll(), "fuseMapMap")},
		measurementFrom(msSamples(8, 8, 8, 8), true), 0.2, true, "")
	r.addAdaptation(3, "profile-guided rebuild",
		Candidate{Label: "PGO", Tier: General},
		measurementFrom(msSamples(10, 10, 10, 10), true), 0.001, false, "inside the noise")

	path := filepath.Join(t.TempDir(), "prog.mahoraga.json")
	if err := r.Write(path); err != nil {
		t.Fatal(err)
	}
	back, err := ReadRecipe(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Adaptations) != 2 {
		t.Fatalf("adaptations = %d want 2 — rejections are recorded too", len(back.Adaptations))
	}
	if len(back.Kept()) != 1 {
		t.Errorf("kept = %d want 1", len(back.Kept()))
	}
	// The rejection keeps its reason: a recipe listing only wins hides how
	// much was tried and found to be noise.
	var rejected *Adaptation
	for i := range back.Adaptations {
		if !back.Adaptations[i].Kept {
			rejected = &back.Adaptations[i]
		}
	}
	if rejected == nil || rejected.Reason == "" {
		t.Error("the rejected adaptation lost its reason")
	}
	// And the configuration rebuilds.
	c := back.Candidate()
	for _, name := range c.Schedule.Passes {
		if name == "fuseMapMap" {
			t.Error("the replayed schedule still contains the ablated pass")
		}
	}
	if len(c.Schedule.Passes) == 0 {
		t.Error("the replayed schedule has no passes")
	}
	if back.InputFingerprint.SHA256 == "" || back.InputFingerprint.Lines != 4 {
		t.Errorf("the input fingerprint did not survive: %+v", back.InputFingerprint)
	}
}

// A champion the final re-measurement does not confirm must not be written:
// shipping a binary that is no faster than the baseline is worse than useless,
// and the record has to say the search was overturned.
func TestRevertToBaselineClearsTheChampion(t *testing.T) {
	r := newRecipe(Options{Tier: Pinned})
	r.setBaseline(measurementFrom(msSamples(10, 10, 10, 10), true), 0.01)
	r.addAdaptation(4, "pass ablation",
		Candidate{Label: "without something", Tier: General,
			Schedule: scheduleWithout(scheduleAll(), "fuseMapMap")},
		measurementFrom(msSamples(9, 9, 9, 9), true), 0.1, true, "")
	if len(r.Kept()) != 1 {
		t.Fatal("setup: the adaptation was not kept")
	}

	r.revertToBaseline()
	if !r.Overturned() {
		t.Error("the recipe does not record that it was overturned")
	}
	if r.Improved() {
		t.Error("an overturned recipe still claims an improvement")
	}
	if len(r.Kept()) != 0 {
		t.Error("an overturned recipe still lists kept adaptations")
	}
	if len(r.Schedule.Passes) != 0 {
		t.Error("the schedule was not reset to the baseline's")
	}
	// The attempts are still recorded, with why they did not stand.
	if len(r.Adaptations) != 1 || r.Adaptations[0].Reason == "" {
		t.Error("the overturned adaptation lost its record")
	}
}

// scheduleAll is the full default pass list, as a starting point for the
// ablation helper.
func scheduleAll() optimizer.Schedule {
	return optimizer.Schedule{Passes: optimizer.PassNames()}
}

func TestDefaultPaths(t *testing.T) {
	if got := DefaultOut("aoc/day11.domain"); got != filepath.Join("aoc", "day11-adapted") {
		t.Errorf("DefaultOut = %q", got)
	}
	if got := DefaultRecipe("aoc/day11.domain"); got != filepath.Join("aoc", "day11.mahoraga.json") {
		t.Errorf("DefaultRecipe = %q", got)
	}
}

func TestParseTier(t *testing.T) {
	for s, want := range map[string]Tier{"general": General, "guarded": Guarded, "pinned": Pinned, "": Pinned} {
		got, ok := ParseTier(s)
		if !ok || got != want {
			t.Errorf("ParseTier(%q) = %v, %v; want %v", s, got, ok, want)
		}
	}
	if _, ok := ParseTier("reckless"); ok {
		t.Error("an unknown tier was accepted")
	}
}

// A search that adapted nothing must never report a speedup.
//
// Its champion *is* the baseline, so the final measurement compares one
// configuration with itself built twice, and any difference is noise by
// construction. Before this was checked, a search that did nothing at all
// reported "ADAPTED — 1.18× faster".
func TestNoAdaptationMeansNoImprovement(t *testing.T) {
	r := newRecipe(Options{Tier: Pinned})
	r.setBaseline(measurementFrom(msSamples(10, 10, 10, 10), true), 0)
	// A flattering final measurement, with nothing to have caused it.
	r.setFinal(
		measurementFrom(msSamples(10, 10, 10, 10), true),
		measurementFrom(msSamples(8, 8, 8, 8), true))

	if r.Speedup <= 1 {
		t.Fatalf("setup: expected a favourable speedup, got %.2f", r.Speedup)
	}
	if r.Improved() {
		t.Error("a search that kept no adaptation reported an improvement")
	}
	if r.Overturned() {
		t.Error("a search that kept no adaptation reported being overturned")
	}
}

// Reverting is a finding about the *search*, so it only applies when the
// search had actually claimed something.
func TestRevertWithNothingKeptIsNotAnOverturn(t *testing.T) {
	r := newRecipe(Options{Tier: Pinned})
	r.setBaseline(measurementFrom(msSamples(10, 10, 10, 10), true), 0)
	r.addAdaptation(4, "pass ablation", Candidate{Label: "no good", Tier: General},
		measurementFrom(msSamples(10, 10, 10, 10), true), 0.001, false, "inside the noise")

	r.revertToBaseline()
	if r.Overturned() {
		t.Error("a search whose candidates were all already rejected was reported as overturned")
	}
	// The rejection keeps its original reason rather than being relabelled.
	if r.Adaptations[0].Reason != "inside the noise" {
		t.Errorf("the rejection's reason was overwritten: %q", r.Adaptations[0].Reason)
	}
}

// Two samples say nothing about a distribution, so a comparison built on them
// is not a comparison. Without this, low run counts degenerate into "whichever
// mean is lower wins" and accept noise every time.
func TestTooFewSamplesIsNotDistinguishable(t *testing.T) {
	champ := measurementFrom([]time.Duration{ms(30), ms(10), ms(10)}, true)
	cand := measurementFrom([]time.Duration{ms(30), ms(5), ms(5)}, true)
	if champ.Runs >= MinSamplesForSpread {
		t.Fatalf("setup: expected fewer than %d kept samples, got %d", MinSamplesForSpread, champ.Runs)
	}
	if Distinguishable(champ, cand, 0.02) {
		t.Error("a 50% difference measured from two samples was called distinguishable")
	}
	// With enough samples the same difference is real.
	champ = measurementFrom(msSamples(10, 10, 10, 10), true)
	cand = measurementFrom(msSamples(5, 5, 5, 5), true)
	if !Distinguishable(champ, cand, 0.02) {
		t.Error("a 50% difference over four samples was not distinguishable")
	}
}

// The second look exists because "I could not tell" was a verdict the search
// printed and never acted on. What it re-races is exactly the candidates that
// earned that verdict — never the ones that were plainly slower, and never a
// candidate whose binary was never built.
func TestOnlyUnsettledCandidatesGetASecondLook(t *testing.T) {
	s := &Search{}
	s.noteUnclear(true, 6, "templated codegen edits", Candidate{Label: "promising"}, "/tmp/bin-a", 0.28)
	s.noteUnclear(false, 4, "pass ablation", Candidate{Label: "plainly slower"}, "/tmp/bin-b", -0.11)
	s.noteUnclear(true, 4, "pass ablation", Candidate{Label: "never built"}, "", 0.40)

	if len(s.unclear) != 1 || s.unclear[0].cand.Label != "promising" {
		t.Fatalf("the second look queued %+v", s.unclear)
	}
}

// Most promising first, and bounded. A search on a machine where everything is
// inconclusive must not double in length, and the candidates it then declines
// to re-race are the ones that looked least like a win.
func TestSecondLookQueueIsOrderedAndBounded(t *testing.T) {
	s := &Search{}
	for i := range maxSecondLook + 3 {
		s.noteUnclear(true, 6, "turn", Candidate{Label: fmt.Sprint(i)}, "/tmp/bin", float64(i)/100)
	}
	q := s.secondLookQueue()
	if len(q) != maxSecondLook {
		t.Fatalf("queued %d candidates, want %d", len(q), maxSecondLook)
	}
	for i := 1; i < len(q); i++ {
		if q[i-1].eff < q[i].eff {
			t.Errorf("the queue is not ordered by effect: %+v", q)
		}
	}
	if q[0].eff != float64(maxSecondLook+2)/100 {
		t.Errorf("the most promising candidate is not first: %+v", q[0])
	}
}

// The inconclusive count is what the verdict tells a reader to spend more runs
// on. Once the second look has re-raced them at full length they are no longer
// unsettled — whatever it decided is the answer — so a recipe that still
// advised `--runs` would be advising a reader to settle a settled question.
func TestASecondLookClearsWhatItReAsked(t *testing.T) {
	r := newRecipe(Options{Program: "p.domain", Input: "in", Expected: "want"})
	r.markInconclusive(true)
	r.markInconclusive(true)
	if r.Inconclusive != 2 {
		t.Fatalf("inconclusive = %d before the second look, want 2", r.Inconclusive)
	}

	r.noteSecondLook(2)
	if r.SecondLook != 2 {
		t.Errorf("second_look = %d, want 2", r.SecondLook)
	}
	if r.Inconclusive != 0 {
		t.Errorf("inconclusive = %d after re-asking, want 0", r.Inconclusive)
	}
	// Anything the second look could still not settle counts again, and that is
	// the number a reader should act on.
	r.markInconclusive(true)
	if r.Inconclusive != 1 {
		t.Errorf("inconclusive = %d after the second look left one open, want 1", r.Inconclusive)
	}
}
