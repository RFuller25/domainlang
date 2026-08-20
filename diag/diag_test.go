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

// The phrase in the message is the phrase the reader wrote, quoted once.
// prims quotes it on the way out and the analyzer used to quote what it
// captured a second time, so an operation carrying a string argument came back
// with every quote tripled: a line reading `Splitt Each by ","` was reported as
// `unknown operation "Splitt Each by \\\",\\\""`.
func TestUnknownOperationMessageQuotesThePhraseOnce(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \";\"\n" +
		"Cursed Technique: Splitt Each by \",\"\n" +
		"Reveal: stdout\n"
	r := analyze(t, src)
	d := diagWith(r, Error, "unknown operation")
	if d == nil {
		t.Fatalf("no unknown-operation diagnostic in %v", r.Diags)
	}
	want := `unknown operation "Splitt Each by \",\"" under "Cursed Technique"`
	if d.Msg != want {
		t.Errorf("Msg  = %s\nwant = %s", d.Msg, want)
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

// A program whose Parts each reveal is complete, even with no top-level Reveal.
func TestLintPartsSatisfyTheRevealCheck(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Part "1":
    Maximum Technique: Count
    Reveal: stdout
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, "never revealed"); d != nil {
		t.Errorf("a revealing Part should satisfy the reveal check, got %v", d)
	}
}

// The hazard of Parts printing only what they explicitly Reveal.
func TestLintPartWithoutReveal(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Part "quiet":
    Maximum Technique: Count
Reveal: stdout
`
	r := analyze(t, src)
	if diagWith(r, Warning, `Part "quiet" never reveals anything`) == nil {
		t.Errorf("missing silent-Part warning in %v", r.Diags)
	}
}

// A Reveal inside a nested block still counts as revealing.
func TestLintPartRevealingInsideALoop(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Part "1":
    Simple Domain: Repeat 2
        Reveal: stdout
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, "never reveals anything"); d != nil {
		t.Errorf("a Reveal nested in a loop body counts, got %v", d)
	}
}

func TestLintDuplicatePartLabels(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Part "1":
    Maximum Technique: Count
    Reveal: stdout
Part "1":
    Maximum Technique: Count
    Reveal: stdout
`
	r := analyze(t, src)
	if diagWith(r, Warning, `Part "1" is defined more than once`) == nil {
		t.Errorf("missing duplicate-Part warning in %v", r.Diags)
	}
}

// A Part is a passthrough, so top-level statements after one are not dead.
func TestLintStatementsAfterAPartAreNotDead(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Part "1":
    Maximum Technique: Count
    Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, "runs after the last Reveal"); d != nil {
		t.Errorf("statements after a Part are still observed, got %v", d)
	}
}

// Within a Part, though, work after its own final Reveal is dead.
func TestLintDeadCodeInsideAPart(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Part "1":
    Reveal: stdout
    Maximum Technique: Count
`
	r := analyze(t, src)
	if diagWith(r, Warning, "runs after the last Reveal") == nil {
		t.Errorf("missing dead-code warning inside a Part in %v", r.Diags)
	}
}

func TestLintUnusedImport(t *testing.T) {
	src := `Innate Domain: shapes
Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Reveal: stdout
`
	r := analyze(t, src)
	if diagWith(r, Warning, `library "shapes" is imported but nothing from it is summoned`) == nil {
		t.Errorf("missing unused-import warning in %v", r.Diags)
	}
}

// Calling a Shikigami the file does not define itself counts as using an
// import, so the warning must not fire.
func TestLintImportUsedByBareCall(t *testing.T) {
	src := `Innate Domain: shapes
Cursed Energy: input.txt
Shikigami: Doubled
Reveal: stdout
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, "nothing from it is summoned"); d != nil {
		t.Errorf("a summoned import should not warn, got %v", d)
	}
}

func TestLintDuplicateImport(t *testing.T) {
	src := `Innate Domain: shapes
Innate Domain: shapes
Cursed Energy: input.txt
Shikigami: Doubled
Reveal: stdout
`
	r := analyze(t, src)
	if diagWith(r, Warning, `library "shapes" is imported more than once`) == nil {
		t.Errorf("missing duplicate-import warning in %v", r.Diags)
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
	out, rewrites := OptimizeSource("", src)
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
	out, rewrites := OptimizeSource("", src)
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
	out, rewrites := OptimizeSource("", day1)
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

// ---------------------------------------------------------------------------
// Prefix-free programs

// day1Bare is day1 with every themed keyword left out.
const day1Bare = `input.txt
Split Text by "\n\n"
Split Each by "\n"
Convert Each List to Integers
Sum Each Group
Quicksort, Descending
Select Top 3, Sum
stdout
`

// A prefix-free program is a clean program: the analyzer must not report the
// absent keywords, and the lint rules (which read keywords) must still see a
// pipeline that reads its input and reveals its result.
func TestPrefixFreeProgramIsClean(t *testing.T) {
	r := analyze(t, day1Bare)
	if len(r.Diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", r.Diags)
	}
	if r.Pipe == nil {
		t.Fatal("expected a resolved pipeline")
	}
}

// A misspelled bare operation gets the same "did you mean" treatment as a
// misspelled one under a keyword — and here the repair is the phrase itself,
// since there is no keyword on the line to move.
func TestBareOperationTypoSuggestionAndFix(t *testing.T) {
	src := strings.Replace(day1Bare, "Split Each by", "Splt Each by", 1)
	r := analyze(t, src)
	d := diagWith(r, Error, "cannot infer a keyword")
	if d == nil {
		t.Fatalf("no inference diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, `"Split Each"`) {
		t.Errorf("help = %q, want a Split Each suggestion", d.Help)
	}
	if !d.HasConfidentFix() {
		t.Fatal("expected a confident fix")
	}
	if r.FixedSrc != day1Bare {
		t.Errorf("FixedSrc =\n%s\nwant original day1Bare", r.FixedSrc)
	}
}

// A phrase nothing is spelled like says so, and offers the escape hatch.
func TestBareOperationWithNoSuggestion(t *testing.T) {
	src := strings.Replace(day1Bare, "Sum Each Group", "Frobnicate Everything", 1)
	r := analyze(t, src)
	d := diagWith(r, Error, "cannot infer a keyword")
	if d == nil {
		t.Fatalf("no inference diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, "Keyword: operation") {
		t.Errorf("help = %q, want the explicit-keyword hint", d.Help)
	}
}

// Naming a Shikigami after a built-in is an error that explains itself.
func TestShikigamiNamedAfterBuiltin(t *testing.T) {
	src := "Shikigami \"Quicksort\"\n    Maximum Technique: Sum\n\n" + day1
	r := analyze(t, src)
	d := diagWith(r, Error, "is named after")
	if d == nil {
		t.Fatalf("no naming diagnostic in %v", r.Diags)
	}
	if !strings.Contains(d.Help, "reserved") {
		t.Errorf("help = %q, want an explanation of the reserved names", d.Help)
	}
}

// Advice that proposes a replacement line is written in the style of the
// program it is advising: no themed keyword in a prefix-free file.
func TestLintAdviceMatchesTheSourceStyle(t *testing.T) {
	bare := "stdin\nInts\nQuicksort\nReverse\nstdout\n"
	keyworded := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Domain Expansion: Quicksort\nReverse Cursed Technique: Reverse\nReveal: stdout\n"

	d := diagWith(analyze(t, bare), Hint, "Sort followed by Reverse")
	if d == nil {
		t.Fatal("expected the Sort+Reverse hint on the prefix-free program")
	}
	if !strings.Contains(d.Help, "`Quicksort, Descending`") {
		t.Errorf("prefix-free advice should stay prefix-free, got %q", d.Help)
	}

	d = diagWith(analyze(t, keyworded), Hint, "Sort followed by Reverse")
	if d == nil {
		t.Fatal("expected the Sort+Reverse hint on the keyworded program")
	}
	if !strings.Contains(d.Help, "`Domain Expansion: Quicksort, Descending`") {
		t.Errorf("keyworded advice should carry the keyword, got %q", d.Help)
	}
}

// ---------------------------------------------------------------------------
// Ignored arguments and expressions written into a phrase
// ---------------------------------------------------------------------------

// An argument no primitive reads is silently dropped at runtime, which makes
// it the quietest way to write a program that does something other than what
// it says. prims.ArgSet records every lookup; this reads those marks back.
func TestLintIgnoredArgument(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Maximum Technique: Join
    Sze: 3
Reveal: stdout
`
	r := analyze(t, src)
	d := diagWith(r, Warning, `ignores the argument "Sze"`)
	if d == nil {
		t.Fatalf("missing ignored-argument warning in %v", r.Diags)
	}
	if !strings.Contains(d.Help, "Size:") {
		t.Errorf("expected a did-you-mean for the misspelling, got %q", d.Help)
	}
	if d.Pos.Line != 4 {
		t.Errorf("the warning should point at the argument line, got line %d", d.Pos.Line)
	}
}

// The arguments real programs write are all read by the primitives they sit
// on; a lint that fires on those would be worse than no lint.
func TestLintIgnoredArgumentIsQuietOnGoodPrograms(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Using: (x) -> x > 2
Maximum Technique: Fold
    Seed: 0
    Using: (acc, x) -> acc + x
Reveal: stdout
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, "ignores the argument"); d != nil {
		t.Fatalf("unexpected ignored-argument warning: %v", *d)
	}
}

// A Shikigami's body is resolved as a substituted copy, so the arguments in
// the definition are marked at substitution rather than by the copy's reads.
func TestLintIgnoredArgumentSkipsShikigamiBodies(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami "Big" (k: Int)
    Cursed Technique: Filter
        Using: (x) -> x > k
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Shikigami: Big
    k: 2
Reveal: stdout
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, "ignores the argument"); d != nil {
		t.Fatalf("unexpected ignored-argument warning: %v", *d)
	}
}

// `Window length(xs) / 2` parses to the words [Window length xs] and the int 2,
// and runs as `Window 2`. The phrase layer takes literals only, and this is the
// warning that says so.
func TestLintExpressionInAPhrase(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Window length(xs) / 2
Reveal: stdout
`
	r := analyze(t, src)
	if diagWith(r, Warning, "looks like an expression") == nil {
		t.Fatalf("missing expression-in-phrase warning in %v", r.Diags)
	}
}

// The test is a call shape, not a name: a channel or loop variable that
// happens to share a builtin's name is not an expression.
func TestLintExpressionInAPhraseIgnoresBareNames(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Channel "cells":
    Maximum Technique: Sum
Simple Domain: For x in cells
    Cursed Technique: Apply
        Using: (v, x) -> v + x
Reveal: stdout
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, "looks like an expression"); d != nil {
		t.Fatalf("unexpected expression-in-phrase warning: %v", *d)
	}
}

// A `Consider` binding nothing reads is a warning, like an argument the
// primitive never read: both are silent at runtime, and both are usually a
// rename that did not finish. A binding that *is* read must stay quiet.
func TestLintUnusedBinding(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider spare As 3
    Consider dropped Of Sum
    Consider used As 5
    Using: (x) -> x + used
Reveal: stdout
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, `nothing reads the binding "spare"`); d == nil {
		t.Errorf("missing unused-binding warning in %v", r.Diags)
	}
	// An `Of` binding is worse than dead weight — it computes a value on every
	// pass through its stage — so its help says so.
	d := diagWith(r, Warning, `nothing reads the binding "dropped"`)
	if d == nil {
		t.Fatalf("missing unused Of-binding warning in %v", r.Diags)
	}
	if !strings.Contains(d.Help, "thrown away") {
		t.Errorf("help = %q, want it to mention the wasted computation", d.Help)
	}
	if diagWith(r, Warning, `nothing reads the binding "used"`) != nil {
		t.Errorf("a binding that is read was reported unused: %v", r.Diags)
	}
}

// A binding shadowed by a lambda parameter of the same name is never resolved,
// so it arrives at the linter as exactly what it is: a binding nothing reads.
func TestLintBindingShadowedByParameter(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider x As 3
    Using: (x) -> x + 1
Reveal: stdout
`
	r := analyze(t, src)
	if diagWith(r, Warning, `nothing reads the binding "x"`) == nil {
		t.Errorf("a fully shadowed binding was not reported: %v", r.Diags)
	}
}

// The hint that says why a copy stayed.
//
// `lint` advertises performance hints, and the largest one the language has to
// give was the one it would not say: a loop deep-copying a map on every lap was
// reported as "clean — no errors, warnings, or hints". The pass knew the reason
// and threw it away.
func TestLintReportsWhyAnUpdateStillCopies(t *testing.T) {
	// A map in loop state, written every lap, with the state read again after
	// the write — so the copy is observed and the pass is right to keep it.
	const readAfter = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(xs, emptymap(0, 0), 0)
Simple Domain: While
    Using: (s) -> item(s, 2) < 40
    Cursed Technique: Apply
        Using: (s) -> consider m as insert(item(s, 1), item(s, 2), 1) in
                      tuple(item(s, 0), m, item(s, 2) + size(item(s, 1)))
Cursed Technique: Apply
    Using: (s) -> size(item(s, 1))
Reveal: stdout
`
	r := analyze(t, readAfter)
	d := diagWith(r, Hint, "copies the whole collection on every pass")
	if d == nil {
		t.Fatalf("no hint about the retained copy, got %v", r.Diags)
	}
	if !strings.Contains(d.Msg, "read again after this update") {
		t.Errorf("hint does not name the reason: %q", d.Msg)
	}
	if d.Code != "perf" {
		t.Errorf("code = %q, want perf", d.Code)
	}
	// A cost, not a mistake: some of these refusals are the only correct answer.
	if d.Severity != Hint {
		t.Errorf("severity = %v, want Hint", d.Severity)
	}

	// The same program with the other fields bound first qualifies, and must
	// draw no hint at all — otherwise the advice is noise on the programs that
	// took it.
	const bound = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(xs, emptymap(0, 0), 0)
Simple Domain: While
    Using: (s) -> item(s, 2) < 40
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider n as item(s, 2) in
                      consider m as insert(item(s, 1), n, 1) in
                      tuple(xs, m, n + 1)
Cursed Technique: Apply
    Using: (s) -> size(item(s, 1))
Reveal: stdout
`
	if d := diagWith(analyze(t, bound), Hint, "copies the whole collection"); d != nil {
		t.Errorf("the rewrite applies here, so there is nothing to advise: %q", d.Msg)
	}
}

// The reason has to be the one that actually applied, and it has to point at the
// update it is about — a body where one update qualifies and another does not is
// where a report that names the wrong site shows up.
func TestLintNamesTheRightDeclinedUpdate(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> emptymap(0, emptyset(0))
    Using: (acc, x) -> insert(acc, x % 3, insert(getor(acc, x % 3, emptyset(0)), x))
Cursed Technique: Apply
    Using: (m) -> size(m)
Reveal: stdout
`
	r := analyze(t, src)
	d := diagWith(r, Hint, "copies the whole collection")
	if d == nil {
		t.Fatalf("no hint for the inner insert, got %v", r.Diags)
	}
	if !strings.Contains(d.Msg, "receiver is not the accumulator") {
		t.Errorf("wrong reason: %q", d.Msg)
	}
	// The outer insert is rooted at acc and was rewritten; only the inner one,
	// reached through getor, still copies. Exactly one hint, and it is the
	// inner call — which is further along the line than the outer one.
	n := 0
	for _, x := range r.Diags {
		if x.Severity == Hint && strings.Contains(x.Msg, "copies the whole collection") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("got %d hints, want exactly the inner insert", n)
	}
	// Both inserts are on one line, so the column is what tells them apart.
	line := d.LineText
	outer := strings.Index(line, "insert(acc") + 1
	if outer <= 0 {
		t.Fatalf("hint landed on an unexpected line: %q", line)
	}
	if d.Pos.Col <= outer {
		t.Errorf("hint points at column %d, at or before the outer insert at %d; "+
			"the outer one was rewritten and the inner one is what still copies",
			d.Pos.Col, outer)
	}
}
