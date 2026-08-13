package mahoraga

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"domain/codegen"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The segment count has to be what the program will actually see: dmReadSource
// trims trailing newlines and the split then produces one more piece than there
// are separators left. Getting this wrong would make a capacity hint that is
// wrong by one on every input that ends in a newline — which is all of them.
func TestFactsSegmentCount(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"trailing newline", "1\n2\n3\n4\n", 4},
		{"no trailing newline", "1\n2\n3", 3},
		{"crlf", "1\r\n2\r\n", 2},
		{"one line", "42", 1},
		{"empty", "", 1},
		{"blank lines count", "1\n\n3\n", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := readFacts(writeTemp(t, "in.txt", tc.content))
			if err != nil {
				t.Fatal(err)
			}
			if f.Segments != tc.want {
				t.Errorf("segments = %d, want %d", f.Segments, tc.want)
			}
			if f.Bytes != int64(len(tc.content)) {
				t.Errorf("bytes = %d, want %d", f.Bytes, len(tc.content))
			}
		})
	}
}

// ASCII is the precondition of a pinned adaptation, so a false positive here is
// a wrong answer in a binary that carries no check.
func TestFactsASCII(t *testing.T) {
	f, err := readFacts(writeTemp(t, "a.txt", "abc\n123\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !f.ASCII {
		t.Error("plain ASCII was not reported as ASCII")
	}
	if f.LongestLine != 3 {
		t.Errorf("longest line = %d, want 3", f.LongestLine)
	}

	f, err = readFacts(writeTemp(t, "b.txt", "abc\ncafé\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.ASCII {
		t.Error("a multibyte rune was reported as ASCII — a pinned entry would have cut it in half")
	}
}

// A catalogue entry has to check the *program* as well as the input. Without
// that, every search would spend a compile and a full measurement proving that
// changing nothing changes nothing.
func TestCatalogueChecksTheEmittedProgram(t *testing.T) {
	facts := Facts{Segments: 500, ASCII: true, HeapReported: true, NumGC: 6, HeapSys: 8 << 20}

	// A program with none of the shapes the source-shaped entries apply to.
	// The collector entry is deliberately not one of them: it rests on a fact
	// about the *run* — that collections happened — and every program has a
	// heap, so it has no code shape to look for.
	for _, e := range catalogueFor(facts, "package main\nfunc main() {}\n", Pinned) {
		if e.ID != "collector off for one run" {
			t.Errorf("%q applied to a program with nowhere to apply it", e.ID)
		}
	}

	// One with all of them.
	src := "make([]int64, 0, len(v1)/2+1)\nif v1[i] == '\\n' {\nutf8.RuneLen(ch)\n"
	all := catalogueFor(facts, src, Pinned)
	if len(all) != len(catalogue) {
		t.Errorf("only %d of %d entries applied: %v", len(all), len(catalogue), labels(all))
	}
	// The labels carry the measured number, so a report says what was assumed
	// rather than merely that something was.
	if !strings.Contains(strings.Join(labels(all), " "), "500") {
		t.Errorf("no entry label carries the measurement it rests on: %v", labels(all))
	}
}

// The collector entry rests on having *seen* a collection. A program that never
// collected has nothing to win, and building it again to find that out is a
// minute nobody gets back.
func TestCollectorEntryNeedsAnObservedCollection(t *testing.T) {
	src := "make([]int64, 0, len(v1)/2+1)\nif v1[i] == '\\n' {\n"
	has := func(f Facts) bool {
		for _, e := range catalogueFor(f, src, Pinned) {
			if e.ID == "collector off for one run" {
				return true
			}
		}
		return false
	}
	if has(Facts{Segments: 10, HeapReported: true, NumGC: 0, HeapSys: 4 << 20}) {
		t.Error("the collector entry applied to a run that never collected")
	}
	if has(Facts{Segments: 10, HeapReported: false, NumGC: 9}) {
		t.Error("the collector entry applied without any allocation figures to reason from")
	}
	if has(Facts{Segments: 10, HeapReported: true, NumGC: 9, HeapSys: 8 << 30}) {
		t.Error("the collector entry applied to a run whose heap is larger than the limit that would guard it")
	}
	if !has(Facts{Segments: 10, HeapReported: true, NumGC: 9, HeapSys: 8 << 20}) {
		t.Error("the collector entry did not apply to a short run that collected nine times")
	}
}

// The memory limit is the whole reason a disabled collector is guarded rather
// than pinned: without it, one measurement would be a promise about every
// future input.
func TestMemoryLimitIsAlwaysSetWithTheCollector(t *testing.T) {
	f := Facts{Segments: 10, HeapReported: true, NumGC: 9, HeapSys: 8 << 20}
	var tuning codegen.Tuning
	for _, e := range catalogue {
		if e.ID == "collector off for one run" {
			e.Apply(f, &tuning)
		}
	}
	if tuning.GCPercent != -1 {
		t.Errorf("GCPercent = %d, want -1", tuning.GCPercent)
	}
	if tuning.MemoryLimitBytes <= int64(f.HeapSys) {
		t.Errorf("the memory limit (%d) is not above the observed heap (%d), so any real "+
			"input would immediately turn the collector back on",
			tuning.MemoryLimitBytes, f.HeapSys)
	}
	// A tiny heap must not produce a limit so small that ordinary variation
	// crosses it.
	small := memoryLimitFor(1 << 10)
	if small < 64<<20 {
		t.Errorf("the floor on the memory limit is missing: %d", small)
	}
}

// --tier caps how far the catalogue may go, and the cap has to be enforced
// where the entries are chosen rather than left to the turn that runs them.
func TestCatalogueRespectsTheTierCap(t *testing.T) {
	facts := Facts{Segments: 500, ASCII: true, HeapReported: true, NumGC: 6, HeapSys: 8 << 20}
	src := "make([]int64, 0, len(v1)/2+1)\nif v1[i] == '\\n' {\nutf8.RuneLen(ch)\n"

	for _, e := range catalogueFor(facts, src, General) {
		if e.Tier != General {
			t.Errorf("--tier general admitted a %s entry: %s", e.Tier, e.ID)
		}
	}
	for _, e := range catalogueFor(facts, src, Guarded) {
		if e.Tier > Guarded {
			t.Errorf("--tier guarded admitted a %s entry: %s", e.Tier, e.ID)
		}
	}
	// And pinned entries exist, or the cap would be untested.
	pinned := 0
	for _, e := range catalogueFor(facts, src, Pinned) {
		if e.Tier == Pinned {
			pinned++
		}
	}
	if pinned == 0 {
		t.Error("no pinned entry in the catalogue — the tier cap is not actually being tested")
	}
}

// A guarded adaptation keeps a fallback, so it stays *correct* on a different
// input and only stops being optimal. Reporting it as unsafe would make
// "guarded" and "pinned" the same tier in the one place the distinction is
// checked.
func TestVerifyTreatsGuardedAndPinnedDifferently(t *testing.T) {
	other := writeTemp(t, "other.txt", "1\n2\n")
	base := func(tier string) *Recipe {
		return &Recipe{
			Version:          RecipeVersion,
			InputFingerprint: Fingerprint{Bytes: 100, Lines: 10, SHA256: "abc"},
			Adaptations: []Adaptation{
				{Turn: 6, ID: "exact list capacity", Tier: tier, Kept: true},
			},
		}
	}

	guarded := Verify(base("guarded"), other)
	if guarded.Matches {
		t.Error("a different input was reported as matching")
	}
	if !guarded.Safe {
		t.Error("a guarded adaptation was reported unsafe; its fallback is what makes it guarded")
	}
	if len(guarded.Reasons) == 0 {
		t.Error("a guarded adaptation on a different input said nothing at all")
	}
	if !strings.Contains(strings.Join(guarded.Reasons, " "), "stays correct") {
		t.Errorf("the guarded reason does not say the binary is still correct: %v", guarded.Reasons)
	}

	// A pinned adaptation with no contract recorded has nothing a different
	// input could be checked against, so it is refused — and says that rather
	// than something vaguer.
	pinned := Verify(base("pinned"), other)
	if pinned.Safe {
		t.Error("a pinned adaptation was reported safe on an input it was not verified against")
	}
	if !strings.Contains(strings.Join(pinned.Reasons, " "), "records no contract") {
		t.Errorf("the pinned refusal does not say why it is unsafe: %v", pinned.Reasons)
	}
}

// A pin is an assumption about the input, not about one file. A recipe whose
// only pinned adaptation assumed "every byte is one rune" holds for any input
// that is also all ASCII, and binding it to a hash was correct and needlessly
// strict.
func TestAContractAdmitsADifferentConformingInput(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	r := &Recipe{
		Version:          RecipeVersion,
		InputFingerprint: Fingerprint{Bytes: 100, Lines: 10, SHA256: "abc"},
		Adaptations: []Adaptation{
			{Turn: 8, ID: "no UTF-8 decoding", Tier: "pinned", Kept: true},
		},
		Contract: Contract{ASCII: true},
	}

	ok := Verify(r, write("ascii.txt", "abc\ndef\n"))
	if ok.Matches {
		t.Error("a different input was reported as matching")
	}
	if !ok.Safe {
		t.Errorf("a conforming input was refused: %v", ok.Reasons)
	}
	if !strings.Contains(strings.Join(ok.Reasons, " "), "contract holds") {
		t.Errorf("the acceptance does not say the contract was checked: %v", ok.Reasons)
	}

	bad := Verify(r, write("utf8.txt", "abc\ncafé\n"))
	if bad.Safe {
		t.Error("an input with a multibyte rune satisfied an ASCII contract")
	}
	if !strings.Contains(strings.Join(bad.Reasons, " "), "not all ASCII") {
		t.Errorf("the refusal does not name the clause that failed: %v", bad.Reasons)
	}
}

// Some assumptions cannot be re-established by looking at a file — whether a
// filter would keep every element of it is a property of running the program.
// Those clauses are what still bind a recipe to one input, and the refusal has
// to name which one did the binding.
func TestAnUnverifiableClauseBindsToTheInput(t *testing.T) {
	other := writeTemp(t, "other.txt", "1\n2\n")
	r := &Recipe{
		Version:          RecipeVersion,
		InputFingerprint: Fingerprint{Bytes: 100, Lines: 10, SHA256: "abc"},
		Adaptations: []Adaptation{
			{Turn: 2, ID: "drop Filter at line 4", Tier: "pinned", Kept: true},
		},
		Contract: Contract{Unverifiable: []string{"the Filter at line 4 kept every element"}},
	}
	v := Verify(r, other)
	if v.Safe {
		t.Error("a clause nobody can re-check was treated as satisfied")
	}
	joined := strings.Join(v.Reasons, " ")
	if !strings.Contains(joined, "Filter at line 4") {
		t.Errorf("the refusal does not name the clause that binds it: %v", v.Reasons)
	}
	if !strings.Contains(joined, "without running the program") {
		t.Errorf("the refusal does not say why the clause cannot be checked: %v", v.Reasons)
	}
}

// The tuning is the half of a rebuild that lives nowhere else: a pass schedule
// can be re-derived from a command line, a measured element count cannot. A
// recipe that dropped it would replay a different binary from the one measured.
func TestRecipeRoundTripsTheTuning(t *testing.T) {
	r := newRecipe(Options{Program: "p.domain", Input: "in.txt", Expected: "want.txt"})
	c := Candidate{
		Label: "exact list capacity (500)",
		Tier:  Guarded,
		Tuning: codegen.Tuning{
			ListCapacity: 500, ASCIIText: true, GCPercent: -1, MemoryLimitBytes: 1 << 26,
		},
	}
	r.addAdaptation(6, "templated codegen edits", c, Measurement{Mean: 1, Correct: true}, 0.1, true, "")

	path := filepath.Join(t.TempDir(), "r.json")
	if err := r.Write(path); err != nil {
		t.Fatal(err)
	}
	back, err := ReadRecipe(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Candidate().Tuning; !reflect.DeepEqual(got, c.Tuning) {
		t.Errorf("the tuning did not survive the recipe: got %+v, want %+v", got, c.Tuning)
	}

	// And a champion overturned by the final re-measurement drops the tuning
	// with everything else, or the baseline binary would be built with it.
	back.revertToBaseline()
	if !back.Candidate().Tuning.Empty() {
		t.Errorf("a reverted recipe kept its tuning: %+v", back.Candidate().Tuning)
	}
}

func labels(es []appliedEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Label
	}
	return out
}

// Every race carries the baseline, so the champion's standing against it is
// measured rather than accumulated, and a machine that has moved costs the
// search time rather than correctness.
//
// An earlier version raced only the champion and guarded the result with a
// drift threshold. On a CI container that refused twenty-seven races out of
// fifty and would have thrown away a genuine forty-one percent win the same
// program had found the day before — a rare false positive traded for a
// constant false negative.
func TestDriftIsReportedAndNeverRejects(t *testing.T) {
	r := newRecipe(Options{Program: "p.domain", Input: "in", Expected: "want"})
	s := &Search{baseline: Measurement{Mean: 713 * time.Millisecond, Runs: 9}, recipe: r}

	s.noteDrift(Measurement{Mean: 715 * time.Millisecond, Runs: 9})
	s.noteDrift(Measurement{Mean: 680 * time.Millisecond, Runs: 9})
	if r.DriftedRaces != 0 {
		t.Errorf("a steady machine reported %d drifted races", r.DriftedRaces)
	}

	s.noteDrift(Measurement{Mean: 871 * time.Millisecond, Runs: 9}) // the contaminated window
	s.noteDrift(Measurement{Mean: 500 * time.Millisecond, Runs: 9}) // suspiciously fast
	if r.DriftedRaces != 2 {
		t.Errorf("drifted races = %d, want 2", r.DriftedRaces)
	}
}

// After a revert the binary on disk is the baseline, so Champion and Speedup
// must describe the baseline. They used not to: a reverted recipe recorded
// speedup 1.016 and a champion mean of 704ms for a file that was byte-for-byte
// the baseline build.
func TestRevertMakesTheNumbersDescribeTheArtifact(t *testing.T) {
	r := newRecipe(Options{Program: "p.domain", Input: "in", Expected: "want"})
	r.addAdaptation(4, "pass ablation",
		Candidate{Label: "without fuseLinearMapExtremum", Tier: General},
		Measurement{Mean: 842 * time.Millisecond, Runs: 9, Correct: true}, 0.033, true, "")
	r.setBestRatio(0.887)
	r.setFinal(
		Measurement{Mean: 715 * time.Millisecond, Runs: 9, Correct: true},
		Measurement{Mean: 704 * time.Millisecond, Runs: 9, Correct: true})

	if r.Speedup <= 1 {
		t.Fatalf("the setup is wrong: speedup is %v before the revert", r.Speedup)
	}
	r.revertToBaseline()

	if !r.Reverted {
		t.Fatal("the revert was not recorded")
	}
	if r.Speedup != 1 {
		t.Errorf("speedup = %v after a revert; the baseline was written, so it is 1", r.Speedup)
	}
	if r.Champion != r.FinalBaseline {
		t.Errorf("champion = %+v, want the final baseline %+v — those numbers describe "+
			"the binary on disk, and after a revert that is the baseline",
			r.Champion, r.FinalBaseline)
	}
	if r.OverturnedChampion.MeanNanos != (704 * time.Millisecond).Nanoseconds() {
		t.Errorf("what the rejected champion measured was lost: %+v", r.OverturnedChampion)
	}
	if r.Improved() {
		t.Error("a reverted recipe reports an improvement")
	}
	// BestRatio describes the champion as well, and after a revert the champion
	// is the baseline. Every number that describes the artifact has to move
	// together or a display picks the one that was left behind.
	if r.BestRatio != 1 {
		t.Errorf("best_ratio = %v after a revert; the baseline was written", r.BestRatio)
	}
}

// A fingerprint's line count and the measured segment count describe the same
// file and must agree. They used to read "lines: 1" and "segments: 2" side by
// side in one recipe.
func TestFingerprintLinesAgreeWithSegments(t *testing.T) {
	for _, content := range []string{
		"a\nb\n", "a\nb", "one line", "", "a\n\nc\n", "x\r\ny\r\n",
	} {
		p := writeTemp(t, "in.txt", content)
		fp := fingerprint(p)
		f, err := readFacts(p)
		if err != nil {
			t.Fatal(err)
		}
		if content == "" {
			if fp.Lines != 0 {
				t.Errorf("an empty file has %d lines", fp.Lines)
			}
			continue
		}
		if fp.Lines != f.Segments {
			t.Errorf("%q: fingerprint says %d lines, facts say %d segments",
				content, fp.Lines, f.Segments)
		}
	}
}
