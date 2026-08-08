package shape

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// the oracle
// ---------------------------------------------------------------------------

// Every program in the repository sits beside the input it reads. That makes
// the suggester checkable against what a person actually wrote, rather than
// against what the author of the suggester expected — which is the difference
// between a test and a demonstration, and is why this package exists outside
// cmd/domain where it could not have one.
//
// The claim is not that the top suggestion is right. It is that the *ranked
// list contains* the opening the real program uses: the ordering is a guess
// and the choice is the user's.
func TestSuggestionsContainWhatTheRealProgramsDo(t *testing.T) {
	pairs := corpus(t)
	if len(pairs) < 30 {
		t.Fatalf("only %d program/input pairs found; the corpus is what gives this test its teeth", len(pairs))
	}

	var missed []string
	for _, p := range pairs {
		want := openingOf(t, p.program)
		if want == "" {
			continue // no source stage, so nothing to suggest for
		}
		var got []string
		hit := false
		for _, c := range Suggest(read(t, p.input)) {
			got = append(got, c.First())
			if equivalentOpening(c.First(), want) {
				hit = true
			}
		}
		if !hit {
			missed = append(missed, filepath.Base(p.input)+
				"\n      wrote:    "+want+
				"\n      suggested: "+strings.Join(got, " · "))
		}
	}

	// Two inputs in the corpus are a single integer — a seed, not a shape.
	// Nothing about the file says what the program will do with it (one counts
	// to 14, the other runs a Collatz sequence), so no reading of the input
	// could produce their opening. They are named here rather than quietly
	// tolerated: if a third appears, this test should be looked at again.
	const knownUnderivable = 2
	if len(missed) > knownUnderivable {
		t.Errorf("%d of %d openings not suggested (expected at most %d):\n    %s",
			len(missed), len(pairs), knownUnderivable, strings.Join(missed, "\n    "))
	}
	t.Logf("%d/%d openings covered", len(pairs)-len(missed), len(pairs))
}

// A single integer is a seed, and a seed has no shape: whatever the program
// does with it is a decision the file cannot record. Pinning this keeps the
// suggester honest rather than tempting it to guess.
func TestASingleIntegerSuggestsNothingStructural(t *testing.T) {
	cands := Suggest("27\n")
	for _, c := range cands {
		if strings.Contains(c.First(), "Split") || strings.Contains(c.First(), "Grid") {
			t.Errorf("a one-line seed was given structure: %q", c.First())
		}
	}
}

// ---------------------------------------------------------------------------
// the ambiguity the design exists to handle
// ---------------------------------------------------------------------------

// A rectangle of digits is a grid or a column of numbers, and the file cannot
// say which. Both readings are always offered; only their order changes.
func TestDigitRectangleOffersBothReadings(t *testing.T) {
	// Ten digits a line: this is the shortest-path grid in the corpus.
	grid := Suggest("1163751742\n1381373672\n2136511328\n")
	if len(grid) < 2 {
		t.Fatalf("only %d suggestions", len(grid))
	}
	if grid[0].First() != "Shikigami: Digit Grid" {
		t.Errorf("a wide digit rectangle should read as a grid first, got %q", grid[0].First())
	}
	if !offers(grid, "Shikigami: Ints") {
		t.Error("the other reading was not offered at all")
	}

	// Two digits a line: the same shape, and almost certainly numbers.
	ints := Suggest("17\n21\n36\n")
	if ints[0].First() != "Shikigami: Ints" {
		t.Errorf("a narrow digit rectangle should read as integers first, got %q", ints[0].First())
	}
	if !offers(ints, "Shikigami: Digit Grid") {
		t.Error("the grid reading was not offered at all")
	}
}

// ---------------------------------------------------------------------------
// the individual readings
// ---------------------------------------------------------------------------

func TestBlankLinesReadAsBlocks(t *testing.T) {
	cands := Suggest("1000\n2000\n\n3000\n4000\n")
	if !strings.Contains(cands[0].First(), `Split Text by "\n\n"`) {
		t.Errorf("first suggestion is %q", cands[0].First())
	}
	if !offers(cands, "Shikigami: Blocks") {
		t.Error("the prelude form was not offered")
	}
	// The groups are numbers, so the conversion is part of the suggestion.
	if !strings.Contains(strings.Join(cands[0].Statements, "\n"), "Convert Each List to Integers") {
		t.Errorf("numeric blocks did not suggest the conversion:\n%v", cands[0].Statements)
	}
}

func TestBlocksOfTextDoNotSuggestAConversion(t *testing.T) {
	cands := Suggest("alice\nbob\n\ncarol\ndave\n")
	if strings.Contains(strings.Join(cands[0].Statements, "\n"), "Integers") {
		t.Errorf("text blocks were converted to integers:\n%v", cands[0].Statements)
	}
}

func TestACharacterMapReadsAsAGrid(t *testing.T) {
	cands := Suggest("..#..\n..#..\n###..\n")
	joined := strings.Join(cands[0].Statements, "\n")
	if !strings.Contains(joined, "Convert To Grid") {
		t.Errorf("a `.#` map did not suggest a grid:\n%v", cands[0].Statements)
	}
}

func TestASingleDelimitedLineReadsAsAList(t *testing.T) {
	cands := Suggest("alice, bob, carol, dave\n")
	if !strings.Contains(cands[0].First(), `Split Text by ", "`) {
		t.Errorf("first suggestion is %q", cands[0].First())
	}
}

// A separator that appears once is punctuation, not structure.
func TestASingleCommaIsNotASeparator(t *testing.T) {
	for _, c := range Suggest("hello, world\n") {
		if strings.Contains(c.First(), "Split Text by") {
			t.Errorf("one comma was read as structure: %q", c.First())
		}
	}
}

// ---------------------------------------------------------------------------
// template inference
// ---------------------------------------------------------------------------

func TestTemplateInference(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{"coordinates", []string{"6,10", "0,14", "9,10"}, "{int},{int}"},
		{"ranges", []string{"2-4,6-8", "2-3,4-5"}, "{int}-{int},{int}-{int}"},
		// The middle word is identical on every line, so it is part of the
		// format rather than a field. Turning it into a hole would capture a
		// constant and lose the thing that identifies the line.
		{"a constant word stays literal", []string{"alice grade 12", "bob grade 7"}, "{word} grade {int}"},
		{"words that vary become holes", []string{"alice likes cake", "bob hates peas"}, "{word} {word} {word}"},
	}
	for _, c := range cases {
		got, ok := inferTemplate(c.lines)
		if !ok {
			t.Errorf("%s: no template inferred", c.name)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestTemplateInferenceRefusesMixedShapes(t *testing.T) {
	// A file whose lines are not all one shape has no single template, and
	// pretending otherwise would produce one that fails on most of the file.
	if got, ok := inferTemplate([]string{"6,10", "fold along y=7", "0,14"}); ok {
		t.Errorf("mixed lines produced the template %q", got)
	}
}

func TestTemplateInferenceRefusesConstantLines(t *testing.T) {
	// Every line identical: a template with no holes describes nothing.
	if got, ok := inferTemplate([]string{"hello", "hello"}); ok {
		t.Errorf("identical lines produced the template %q", got)
	}
}

func TestATemplatedFileSuggestsMatchPattern(t *testing.T) {
	cands := Suggest("6,10\n0,14\n9,10\n")
	joined := strings.Join(cands[0].Statements, "\n")
	if !strings.Contains(joined, "Match Pattern") || !strings.Contains(joined, "{int},{int}") {
		t.Errorf("no template suggestion:\n%v", cands[0].Statements)
	}
}

// ---------------------------------------------------------------------------
// edges
// ---------------------------------------------------------------------------

func TestEmptyInputSuggestsNothing(t *testing.T) {
	if got := Suggest(""); len(got) != 0 {
		t.Errorf("an empty file suggested %v", got)
	}
	if got := Suggest("\n\n"); len(got) != 0 {
		t.Errorf("a file of blank lines suggested %v", got)
	}
}

// Every suggestion must be something a person could act on: statements, and a
// reason for them.
func TestEverySuggestionIsWellFormed(t *testing.T) {
	inputs := []string{
		"1\n2\n3\n", "1000\n\n3000\n", "..#\n#..\n", "6,10\n0,14\n",
		"alice, bob, carol\n", "a b c\nd e f\n", "1163751742\n1381373672\n",
	}
	for _, in := range inputs {
		for _, c := range Suggest(in) {
			if len(c.Statements) == 0 {
				t.Errorf("%q: a suggestion with no statements", in)
			}
			if c.Why == "" {
				t.Errorf("%q: %q has no reason", in, c.First())
			}
			for _, st := range c.Statements {
				if strings.TrimSpace(st) == "" {
					t.Errorf("%q: blank statement line in %v", in, c.Statements)
				}
			}
		}
	}
}

// Suggestions must not repeat themselves: two rows offering the same opening
// is a list that looks longer than the choice actually is.
func TestSuggestionsAreDistinct(t *testing.T) {
	for _, in := range []string{"1\n2\n3\n", "1000\n\n3000\n", "..#\n#..\n", "6,10\n0,14\n"} {
		seen := map[string]bool{}
		for _, c := range Suggest(in) {
			key := strings.Join(c.Statements, "\n")
			if seen[key] {
				t.Errorf("%q: duplicate suggestion %q", in, c.First())
			}
			seen[key] = true
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type pair struct{ program, input string }

func corpus(t *testing.T) []pair {
	t.Helper()
	var out []pair
	for _, dir := range []string{"../examples", "../challenges"} {
		progs, err := filepath.Glob(filepath.Join(dir, "*.domain"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range progs {
			in := strings.TrimSuffix(p, ".domain") + ".input"
			if _, err := os.Stat(in); err == nil {
				out = append(out, pair{program: p, input: in})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].input < out[j].input })
	return out
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// openingOf is the statement a program runs immediately after its source
// stage — the one a suggestion is trying to produce.
func openingOf(t *testing.T, path string) string {
	t.Helper()
	seenSource := false
	for _, line := range strings.Split(read(t, path), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if seenSource {
			return trimmed
		}
		if strings.HasPrefix(trimmed, "Cursed Energy:") {
			seenSource = true
		}
	}
	return ""
}

// equivalentOpening reports whether a suggestion says the same thing as what
// was written. Two spellings genuinely mean one operation: the prelude's
// `Lines` *is* a split on newlines, and its `Ints` is that plus a conversion
// (language.md documents both expansions). A suggester that offered one and
// was marked wrong for not offering the other would be measuring spelling
// rather than understanding.
func equivalentOpening(suggested, written string) bool {
	norm := func(s string) string {
		switch strings.TrimSpace(s) {
		case `Cursed Technique: Split Text by "\n"`:
			return "Shikigami: Lines"
		}
		return strings.TrimSpace(s)
	}
	return norm(suggested) == norm(written)
}

func offers(cands []Candidate, first string) bool {
	for _, c := range cands {
		if c.First() == first {
			return true
		}
	}
	return false
}
