package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runRepl feeds lines to the REPL and returns everything it printed.
func runRepl(t *testing.T, lines ...string) string {
	t.Helper()
	var out strings.Builder
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	if code := Repl(in, &out); code != 0 {
		t.Fatalf("repl exit code = %d", code)
	}
	return out.String()
}

func TestReplThreadsValuesAndTypes(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("3\n1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"Cursed Energy: nums.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Channeled Energy: Convert To Integers",
		"Domain Expansion: Sort, Descending",
		":type",
		":quit",
	)
	for _, want := range []string{
		`=> "3\n1\n2" : Text`,
		`=> ["3", "1", "2"] : List<Text>`,
		"=> [3, 1, 2] : List<Int>",
		"=> [3, 2, 1] : List<Int>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestReplContinuationModeForBlocks(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"Cursed Energy: nums.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Channeled Energy: Convert To Integers",
		"Cursed Technique: Map Each", // needs Using: — must enter continuation mode
		"    Using: (x) -> x * 10",
		"", // blank line completes the block
		":quit",
	)
	if !strings.Contains(out, "   ...> ") {
		t.Errorf("continuation prompt missing:\n%s", out)
	}
	if !strings.Contains(out, "=> [10, 20] : List<Int>") {
		t.Errorf("block statement result missing:\n%s", out)
	}
}

func TestReplErrorsKeepTheSessionAlive(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"Cursed Energy: nums.txt",
		"Cursed Tecnique: Split", // typo: reported, dropped
		":type",                  // still the Text from line 1
		":quit",
	)
	// The report is the diagnostics engine's, the same one `domain check`
	// prints: the offending line, carets under it, and the repair.
	for _, want := range []string{
		`error[name]: unknown keyword "Cursed Tecnique"`,
		"--> repl:2:1",
		"2 | Cursed Tecnique: Split",
		`help: did you mean "Cursed Technique"?`,
		"fix: Cursed Technique: Split",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "domain> Text\n") {
		t.Errorf("session state lost after error:\n%s", out)
	}
}

func TestReplUndoAndList(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("2\n1"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"Cursed Energy: nums.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Channeled Energy: Convert To Integers",
		"Domain Expansion: Sort",
		":undo",
		":list",
		":quit",
	)
	if !strings.Contains(out, "=> [2, 1] : List<Int>") {
		t.Errorf("undo did not restore the previous value:\n%s", out)
	}
	if strings.Contains(strings.Split(out, ":list")[0], "(empty domain)") {
		t.Errorf("unexpected empty domain:\n%s", out)
	}
	if !strings.Contains(out, "Channeled Energy: Convert To Integers\n") {
		t.Errorf(":list missing program text:\n%s", out)
	}
	if strings.Contains(out, "Domain Expansion: Sort\n") {
		t.Errorf(":list still shows the undone statement:\n%s", out)
	}
}

func TestReplSaveAndLoadRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"Cursed Energy: nums.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Channeled Energy: Convert To Integers",
		"Maximum Technique: Sum",
		":save prog.domain",
		":reset",
		":load prog.domain",
		":quit",
	)
	if !strings.Contains(out, "saved 4 statement(s)") {
		t.Errorf("save report missing:\n%s", out)
	}
	// After :load the replay shows the program's final value again.
	if strings.Count(out, "=> 6 : Int") < 2 {
		t.Errorf("load did not replay to the saved value:\n%s", out)
	}
	saved, err := os.ReadFile(filepath.Join(".", "prog.domain"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "Maximum Technique: Sum") {
		t.Errorf("saved program incomplete:\n%s", saved)
	}
}

func TestReplRuntimeErrorRollsBack(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"Cursed Energy: nums.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Channeled Energy: Convert To Integers", // "x" is not an integer → runtime error
		":type",                                 // still List<Text>
		":quit",
	)
	if !strings.Contains(out, "runtime error:") || !strings.Contains(out, "statement dropped") {
		t.Errorf("runtime failure not rolled back:\n%s", out)
	}
	if !strings.Contains(out, "List<Text>\n") {
		t.Errorf("session type lost after runtime error:\n%s", out)
	}
}

// TestReplWithoutKeywords: every REPL line may be written as a bare operation
// phrase — the same session, with the themed keywords left out.
func TestReplWithoutKeywords(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("3\n1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"nums.txt",
		`Split Text by "\n"`,
		"Convert To Integers",
		"Sort, Descending",
		"Map Each", // needs Using: — continuation mode works bare too
		"    Using: (x) -> x * 10",
		"",
		"Sum",
		"stdout",
		":quit",
	)
	for _, want := range []string{
		`=> "3\n1\n2" : Text`,
		`=> ["3", "1", "2"] : List<Text>`,
		"=> [3, 1, 2] : List<Int>",
		"=> [3, 2, 1] : List<Int>",
		"   ...> ",
		"=> [30, 20, 10] : List<Int>",
		"=> 60 : Int",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A phrase the REPL cannot name is reported and dropped, like any other bad
// statement — the session survives it.
func TestReplUnknownBarePhraseIsReported(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t, "nums.txt", "Frobnicate", `Split Text by "\n"`, ":quit")
	if !strings.Contains(out, "cannot infer a keyword") {
		t.Errorf("expected the inference error, got:\n%s", out)
	}
	if !strings.Contains(out, `=> ["1", "2"] : List<Text>`) {
		t.Errorf("the session should carry on after a dropped line:\n%s", out)
	}
}

// A comment typed at the prompt is not a statement of its own: it travels
// with the statement it introduces, so :undo drops the pair and :list shows
// them together.
func TestReplTopLevelCommentTravelsWithItsStatement(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"# the puzzle input",
		"Cursed Energy: nums.txt",
		":list",
		":undo",
		":list",
		":quit",
	)
	if !strings.Contains(out, "# the puzzle input\nCursed Energy: nums.txt") {
		t.Errorf("comment did not stay with its statement:\n%s", out)
	}
	if !strings.Contains(out, "(empty domain)") {
		t.Errorf(":undo left the comment behind as a statement:\n%s", out)
	}
	// The comment is not a pipeline stage, so it never produced a value of
	// its own — only the one statement did.
	if got := strings.Count(out, "=> "); got != 1 {
		t.Errorf("comment line was evaluated: %d results in\n%s", got, out)
	}
}

// :load and :save round-trip a real program — comments, blank lines and all —
// and count statements rather than lines.
func TestReplLoadSaveKeepsCommentsAndLayout(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	program := `# Sum the numbers.
#
# Two comment blocks and a blank line, all of which should survive.
Cursed Energy: nums.txt
Cursed Technique: Split Text by "\n"

# Now the arithmetic.
Channeled Energy: Convert To Integers
Maximum Technique: Sum
`
	if err := os.WriteFile("prog.domain", []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t, ":load prog.domain", ":save! prog.domain", ":quit")
	if !strings.Contains(out, "saved 4 statement(s)") {
		t.Errorf("comments were counted as statements:\n%s", out)
	}
	saved, err := os.ReadFile("prog.domain")
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != program {
		t.Errorf("round trip changed the file:\ngot:\n%s\nwant:\n%s", saved, program)
	}
}

// :save refuses to clobber an existing file; :save! is the way to say you
// meant it.
func TestReplSaveDoesNotOverwriteByAccident(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("keep.domain", []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t, "Cursed Energy: nums.txt", ":save keep.domain", ":quit")
	if !strings.Contains(out, "already exists") {
		t.Errorf("an existing file was overwritten without a word:\n%s", out)
	}
	if got, _ := os.ReadFile("keep.domain"); string(got) != "precious\n" {
		t.Errorf("the file was overwritten anyway: %q", got)
	}

	out = runRepl(t, "Cursed Energy: nums.txt", ":save! keep.domain", ":quit")
	if !strings.Contains(out, "saved 1 statement(s)") {
		t.Errorf(":save! did not overwrite:\n%s", out)
	}
}

// A path with a space in it is one path, not two.
func TestReplLoadAcceptsPathsWithSpaces(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("my program.domain", []byte("Cursed Energy: nums.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t, ":load my program.domain", ":quit")
	if strings.Contains(out, "no such file") {
		t.Errorf("the path was split on its space:\n%s", out)
	}
	if !strings.Contains(out, `=> "1\n2" : Text`) {
		t.Errorf("the program did not load:\n%s", out)
	}
}

// There is no program stdin in a REPL, so a file target that does not exist is
// a mistake to report — not an empty string to carry on with.
func TestReplMissingSourceFileIsReported(t *testing.T) {
	t.Chdir(t.TempDir())
	out := runRepl(t, "Cursed Energy: typo.txt", ":list", ":quit")
	if !strings.Contains(out, "typo.txt") || !strings.Contains(out, "runtime error") {
		t.Errorf("a missing file was not reported:\n%s", out)
	}
	if strings.Contains(out, `=> "" : Text`) {
		t.Errorf("a missing file silently read as empty text:\n%s", out)
	}
	if !strings.Contains(out, "(empty domain)") {
		t.Errorf("the failed statement was kept:\n%s", out)
	}
}

// :stats replays under the profiler and charts the result.
func TestReplStatsChartsTheProfile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("3\n1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"Cursed Energy: nums.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Channeled Energy: Convert To Integers",
		"Maximum Technique: Sum",
		":stats",
		":quit",
	)
	if !strings.Contains(out, "[stats] 4 stage(s)") {
		t.Errorf("stats header missing:\n%s", out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("no bars in the profile:\n%s", out)
	}
	if !strings.Contains(out, "Read Source") || !strings.Contains(out, "%") {
		t.Errorf("stage rows missing from the profile:\n%s", out)
	}
	// The session is unchanged by profiling it.
	if !strings.Contains(out, "=> 6 : Int") {
		t.Errorf("the program did not run:\n%s", out)
	}
}

func TestReplStatsOnAnEmptySession(t *testing.T) {
	t.Chdir(t.TempDir())
	out := runRepl(t, ":stats", ":quit")
	if !strings.Contains(out, "(empty domain)") {
		t.Errorf("expected an empty-domain notice:\n%s", out)
	}
}

// Continuation mode is driven by the error itself, not by matching its
// wording: a statement missing its block waits, and an error that an indented
// block cannot fix is reported at once.
func TestReplContinuationIsDrivenByTheErrorNotItsWording(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	waits := runRepl(t,
		"Cursed Energy: nums.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Channel \"evens\":", // a Channel body is exactly what an indent supplies
		"    Maximum Technique: Count",
		"",
		":list",
		":quit",
	)
	if !strings.Contains(waits, "   ...> ") {
		t.Errorf("Channel did not enter continuation mode:\n%s", waits)
	}
	if !strings.Contains(waits, `Channel "evens":`) {
		t.Errorf("the Channel was not accepted:\n%s", waits)
	}

	// A From: consumer inside a loop body is a hard error. It used to be
	// caught by a substring that matched this message by accident, which left
	// the session waiting for a block that could never fix it.
	hard := runRepl(t,
		"Cursed Energy: nums.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Channeled Energy: Convert To Integers",
		"Simple Domain: Repeat 2",
		"    Maximum Technique: Combine",
		"        From: nothing",
		"",
		":quit",
	)
	if strings.Contains(hard, "   ...> \n") {
		t.Errorf("a hard error left the session in continuation mode:\n%s", hard)
	}
	if !strings.Contains(hard, "error") {
		t.Errorf("the error was not reported:\n%s", hard)
	}
}

// :doc <name> answers from the catalog the language server hovers with.
func TestReplDocLooksUpAPrimitive(t *testing.T) {
	t.Chdir(t.TempDir())
	out := runRepl(t, ":doc Fold", ":doc zzzz", ":doc window", ":quit")

	if !strings.Contains(out, "Maximum Technique: Fold") {
		t.Errorf(":doc did not name the primitive:\n%s", out)
	}
	if !strings.Contains(out, "primitives.md#fold") {
		t.Errorf(":doc did not point at the reference page:\n%s", out)
	}
	if !strings.Contains(out, `no primitive matches "zzzz"`) {
		t.Errorf("a miss was not reported:\n%s", out)
	}
	if !strings.Contains(out, "Window") {
		t.Errorf("a case-insensitive lookup failed:\n%s", out)
	}
}

// A vague query lists what it matched rather than picking for you.
func TestReplDocShortlistsAVagueQuery(t *testing.T) {
	t.Chdir(t.TempDir())
	out := runRepl(t, ":doc grid", ":quit")
	if !strings.Contains(out, "primitives match") {
		t.Errorf("expected a shortlist:\n%s", out)
	}
}

// Piped, :visualize prints the trace instead of opening a stepper there is no
// terminal to drive.
func TestReplVisualizePipedPrintsTheTrace(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("3\n1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runRepl(t,
		"Cursed Energy: nums.txt",
		"Cursed Technique: Extract Integers",
		"Maximum Technique: Sum",
		":visualize",
		":quit",
	)
	if !strings.Contains(out, "Read Source") || !strings.Contains(out, "Sum") {
		t.Errorf("the trace is missing:\n%s", out)
	}
	// And the session is unchanged by having watched itself run.
	if !strings.Contains(out, "=> 6 : Int") {
		t.Errorf("the program did not run:\n%s", out)
	}
}

func TestReplVisualizeEmptySession(t *testing.T) {
	t.Chdir(t.TempDir())
	if out := runRepl(t, ":visualize", ":quit"); !strings.Contains(out, "(empty domain)") {
		t.Errorf("expected an empty-domain notice:\n%s", out)
	}
}
