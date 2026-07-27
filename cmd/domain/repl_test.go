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
	if !strings.Contains(out, `error:`) || !strings.Contains(out, "Cursed Tecnique") {
		t.Errorf("typo not reported:\n%s", out)
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
