package diag

import (
	"strings"
	"testing"
)

// analyze is a shorthand that runs the analyzer over inline source.
func analyze(t *testing.T, src string) *Report {
	t.Helper()
	return Analyze("test.domain", src)
}

// errAt finds the first diagnostic of the given severity containing substr.
func diagWith(r *Report, sev Severity, substr string) *Diagnostic {
	for i := range r.Diags {
		if r.Diags[i].Severity == sev && strings.Contains(r.Diags[i].Msg, substr) {
			return &r.Diags[i]
		}
	}
	return nil
}

const day1 = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
`

func TestCleanProgramHasNoDiagnostics(t *testing.T) {
	r := analyze(t, day1)
	if len(r.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", r.Diags)
	}
	if r.Pipe == nil {
		t.Fatal("expected a resolved pipeline")
	}
}

func TestKeywordTypoSuggestionAndFix(t *testing.T) {
	src := strings.Replace(day1, "Cursed Technique: Split Text", "Cursed Tecnique: Split Text", 1)
	r := analyze(t, src)
	d := diagWith(r, Error, `unknown keyword "Cursed Tecnique"`)
	if d == nil {
		t.Fatalf("no keyword diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, `"Cursed Technique"`) {
		t.Errorf("help = %q, want a Cursed Technique suggestion", d.Help)
	}
	if !d.HasConfidentFix() {
		t.Fatal("expected a confident fix")
	}
	if r.FixedSrc != day1 {
		t.Errorf("FixedSrc =\n%s\nwant original day1", r.FixedSrc)
	}
}

func TestCaseOnlyKeywordMismatchIsConfident(t *testing.T) {
	src := strings.Replace(day1, "Maximum Technique: Sum Each Group", "maximum technique: Sum Each Group", 1)
	r := analyze(t, src)
	d := diagWith(r, Error, "unknown keyword")
	if d == nil || !d.HasConfidentFix() {
		t.Fatalf("expected a confident case fix, got %+v", d)
	}
	if r.FixedSrc != day1 {
		t.Errorf("case-only mismatch not repaired:\n%s", r.FixedSrc)
	}
}

func TestMultipleErrorsSurfacedInOnePass(t *testing.T) {
	src := strings.NewReplacer(
		"Cursed Technique: Split Text", "Cursed Tecnique: Split Text",
		"Maximum Technique: Sum Each Group", "Maximum Tecnique: Sum Each Group",
		"Reveal: stdout", "Reveal stdout",
	).Replace(day1)
	r := analyze(t, src)
	errs, _, _ := r.Counts()
	if errs < 3 {
		t.Fatalf("expected at least 3 errors surfaced in one pass, got %d: %v", errs, r.Diags)
	}
	if r.FixedSrc != day1 {
		t.Errorf("all three should be repaired; got\n%s", r.FixedSrc)
	}
}

func TestMissingColonAfterKeyword(t *testing.T) {
	src := strings.Replace(day1, "Reveal: stdout", "Reveal stdout", 1)
	r := analyze(t, src)
	d := diagWith(r, Error, "expected ':'")
	if d == nil {
		t.Fatalf("no missing-colon diagnostic in %v", r.Diags)
	}
	if !d.HasConfidentFix() {
		t.Fatal("expected a confident colon insertion")
	}
	if r.FixedSrc != day1 {
		t.Errorf("colon not restored:\n%s", r.FixedSrc)
	}
}

func TestOperationUnderWrongKeyword(t *testing.T) {
	src := strings.Replace(day1, "Maximum Technique: Select Top 3, Sum", "Cursed Technique: Select Top 3, Sum", 1)
	r := analyze(t, src)
	d := diagWith(r, Error, "unknown operation")
	if d == nil {
		t.Fatalf("no unknown-operation diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, "Maximum Technique") {
		t.Errorf("help = %q, want a Maximum Technique redirect", d.Help)
	}
	if r.FixedSrc != day1 {
		t.Errorf("keyword swap not applied:\n%s", r.FixedSrc)
	}
}

func TestOperationTypoFuzzyFix(t *testing.T) {
	src := strings.Replace(day1, "Cursed Technique: Split Each", "Cursed Technique: Splitt Each", 1)
	r := analyze(t, src)
	d := diagWith(r, Error, "unknown operation")
	if d == nil {
		t.Fatalf("no unknown-operation diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, `"Split Each"`) {
		t.Errorf("help = %q, want Split Each suggestion", d.Help)
	}
	if r.FixedSrc != day1 {
		t.Errorf("typo not repaired:\n%s", r.FixedSrc)
	}
}

func TestLexerRepairs(t *testing.T) {
	cases := []struct {
		name, src, wantMsg string
		wantFixed          string
	}{
		{
			name:      "tab indentation",
			src:       "Shikigami \"S\"\n\tMaximum Technique: Sum\n" + day1,
			wantMsg:   "tabs are not allowed",
			wantFixed: "Shikigami \"S\"\n    Maximum Technique: Sum\n" + day1,
		},
		{
			name:      "smart quotes",
			src:       strings.Replace(day1, `"\n\n"`, "“\\n\\n”", 1),
			wantMsg:   "unexpected character",
			wantFixed: day1,
		},
		{
			name:      "trailing semicolon",
			src:       strings.Replace(day1, "Reveal: stdout", "Reveal: stdout;", 1),
			wantMsg:   "unexpected character",
			wantFixed: day1,
		},
		{
			name:      "unterminated string",
			src:       strings.Replace(day1, `Split Text by "\n\n"`, `Split Text by "\n\n`, 1),
			wantMsg:   "unterminated string",
			wantFixed: day1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := analyze(t, c.src)
			if d := diagWith(r, Error, c.wantMsg); d == nil {
				t.Fatalf("no %q diagnostic in %v", c.wantMsg, r.Diags)
			}
			if r.FixedSrc != c.wantFixed {
				t.Errorf("fixed source:\n%q\nwant:\n%q", r.FixedSrc, c.wantFixed)
			}
		})
	}
}

func TestIndentWidthsAboveExcludesCommentLine(t *testing.T) {
	// A comment line's incidental leading-space count must not pollute the
	// set of enclosing-block indentation widths: here the comment sits at
	// width 8, deeper than either surrounding code block (0 and 4).
	src := "A:\n" +
		"    B: x\n" +
		"        # a comment far more indented than either block\n" +
		"    C: y\n" +
		"D: z\n"
	widths := indentWidthsAbove(src, 6) // one past the last line
	has := func(w int) bool {
		for _, x := range widths {
			if x == w {
				return true
			}
		}
		return false
	}
	if has(8) {
		t.Fatalf("comment line's incidental width 8 leaked into widths: %v", widths)
	}
	if !has(0) || !has(4) {
		t.Fatalf("expected widths to include the real block widths 0 and 4, got %v", widths)
	}
}

func TestTypeMismatchConversionAdvice(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Maximum Technique: Sum
Reveal: stdout
`
	r := analyze(t, src)
	d := diagWith(r, Error, "expects")
	if d == nil {
		t.Fatalf("no type diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, "Convert To Integers") {
		t.Errorf("help = %q, want Convert To Integers advice", d.Help)
	}
}

func TestUnknownChannelSuggestion(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami "Ints Of"
    Cursed Technique: Split Text by "\n"
    Channeled Energy: Convert To Integers
Shikigami: Ints Of
Channel "totals":
    Maximum Technique: Sum
Domain Expansion: Combine
    From: totols
    Using: (a) -> a
Reveal: stdout
`
	r := analyze(t, src)
	d := diagWith(r, Error, `unknown channel "totols"`)
	if d == nil {
		t.Fatalf("no channel diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, `"totals"`) {
		t.Errorf("help = %q, want totals suggestion", d.Help)
	}
}

func TestUnknownShikigamiSuggestion(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Blockz
Reveal: stdout
`
	r := analyze(t, src)
	d := diagWith(r, Error, `unknown Shikigami "Blockz"`)
	if d == nil {
		t.Fatalf("no shikigami diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, `"Blocks"`) {
		t.Errorf("help = %q, want prelude Blocks suggestion", d.Help)
	}
}

// --- Lint ---

func TestLintFindings(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami "Unused Helper"
    Maximum Technique: Sum
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Domain Expansion: Sort
Reverse Cursed Technique: Reverse
Channel "orphan":
    Maximum Technique: Max
Maximum Technique: Sum
Reveal: stdout
`
	r := analyze(t, src)
	for _, want := range []struct {
		sev Severity
		msg string
	}{
		{Warning, `Shikigami "Unused Helper" is defined but never summoned`},
		{Warning, `Channel "orphan" is defined but never consumed`},
		{Hint, "Sort followed by Reverse"},
	} {
		if diagWith(r, want.sev, want.msg) == nil {
			t.Errorf("missing %s %q in %v", want.sev, want.msg, r.Diags)
		}
	}
}

func TestLintNoReveal(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
`
	r := analyze(t, src)
	if diagWith(r, Warning, "never revealed") == nil {
		t.Errorf("missing no-reveal warning in %v", r.Diags)
	}
}

func TestLintDoubleSortAndTakeFirst(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Domain Expansion: Sort
Domain Expansion: Sort, Descending
Cursed Technique: Take Item 0
Reveal: stdout
`
	r := analyze(t, src)
	if diagWith(r, Warning, "two sorts in a row") == nil {
		t.Errorf("missing double-sort warning in %v", r.Diags)
	}
	if diagWith(r, Hint, "O(n log n)") == nil {
		t.Errorf("missing sort+take-first hint in %v", r.Diags)
	}
}

// --- Fix ---

func TestFixSrcSeparatesAppliedAndRemaining(t *testing.T) {
	// One fixable typo plus one genuinely ambiguous error (bogus operation).
	src := strings.NewReplacer(
		"Cursed Technique: Split Text", "Cursed Tecnique: Split Text",
		"Maximum Technique: Sum Each Group", "Maximum Technique: Frobnicate Wildly",
	).Replace(day1)
	res := FixSrc("test.domain", src)
	if len(res.Applied) == 0 {
		t.Fatal("expected at least one applied fix")
	}
	if len(res.Remaining) == 0 {
		t.Fatal("expected the bogus operation to remain")
	}
	if !strings.Contains(res.Fixed, "Cursed Technique: Split Text") {
		t.Errorf("typo not fixed:\n%s", res.Fixed)
	}
}

// --- Source-level optimization ---

func TestOptimizeSourceFusesSortReverse(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Domain Expansion: Sort
Reverse Cursed Technique: Reverse
Maximum Technique: Select Top 3, Sum
Reveal: stdout
`
	out, rewrites := OptimizeSource(src)
	if len(rewrites) != 1 {
		t.Fatalf("rewrites = %v, want exactly the fusion", rewrites)
	}
	if !strings.Contains(out, "Domain Expansion: Sort, Descending") {
		t.Errorf("missing flipped sort:\n%s", out)
	}
	if strings.Contains(out, "Reverse") {
		t.Errorf("Reverse not removed:\n%s", out)
	}
}

func TestOptimizeSourceRemovesRedundantSortDeadCodeAndOrphanChannel(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Domain Expansion: Sort
Domain Expansion: Sort, Descending
Channel "orphan":
    Maximum Technique: Sum
Reveal: stdout
Binding Vow: All Values > 0
Cursed Technique: Unique
`
	out, rewrites := OptimizeSource(src)
	if len(rewrites) != 3 {
		t.Fatalf("rewrites = %v, want redundant sort + orphan channel + dead code", rewrites)
	}
	for _, gone := range []string{"Domain Expansion: Sort\n", `Channel "orphan"`, "Unique"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should have been removed:\n%s", gone, out)
		}
	}
	if !strings.Contains(out, "Binding Vow") {
		t.Errorf("Binding Vow after Reveal must survive:\n%s", out)
	}
}

func TestOptimizeSourceLeavesCleanProgramAlone(t *testing.T) {
	out, rewrites := OptimizeSource(day1)
	if len(rewrites) != 0 || out != day1 {
		t.Fatalf("expected no rewrites, got %v", rewrites)
	}
}

// --- Rendering ---

func TestRenderShowsCaretHelpAndFixNote(t *testing.T) {
	src := strings.Replace(day1, "Cursed Technique: Split Text", "Cursed Tecnique: Split Text", 1)
	r := analyze(t, src)
	d := diagWith(r, Error, "unknown keyword")
	if d == nil {
		t.Fatal("no diagnostic")
	}
	out := Render(d, "test.domain", false)
	for _, want := range []string{
		"error[name]:",
		"test.domain:2:1",
		"Cursed Tecnique: Split Text",
		"^^^^^^^^^^^^^^^",
		"help: did you mean",
		"auto-fixable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render output missing %q:\n%s", want, out)
		}
	}
}
