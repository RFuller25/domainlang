package codegen_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
)

// Stream fusion (streamgen.go) compiles a source, a run of elementwise stages
// and a sink into one loop. These hold it to the interpreter, which runs the
// same chains stage by stage, and check that the chains named here really do
// take the fused path.
func TestCompiledStreamFusionMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n"
	cases := []struct {
		name, body, input string
	}{
		{"map filter collect", `Cursed Technique: Map Each
    Using: (s) -> upper(s)
Cursed Technique: Filter
    Using: (s) -> length(s) > 1
Reveal: stdout
`, "a\nbc\n\ndef"},
		{"map then count", `Cursed Technique: Map Each
    Using: (s) -> s + "!"
Maximum Technique: Count
Reveal: stdout
`, "a\nb\n\nc"},
		{"filter then count matching", `Cursed Technique: Filter
    Using: (s) -> length(s) > 0
Maximum Technique: Count Matching
    Using: (s) -> startswith(s, "b")
Reveal: stdout
`, "b1\n\nb2\nc\nb"},
		{"join with no separator, first element empty", `Cursed Technique: Map Each
    Using: (s) -> lower(s)
Maximum Technique: Join with ""
Reveal: stdout
`, "\nAB\n\nC"},
		{"join with separator, first element empty", `Cursed Technique: Map Each
    Using: (s) -> "<" + s + ">"
Maximum Technique: Join with ", "
Reveal: stdout
`, "\nx\ny"},
		{"map to a bare literal, summed", `Cursed Technique: Map Each
    Using: (s) -> 5
Maximum Technique: Sum
Reveal: stdout
`, "a\nb\nc"},
		{"ints, filter, map, sum over a sorted list", `Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort
Cursed Technique: Filter
    Using: (x) -> x % 2 = 0
Cursed Technique: Map Each
    Using: (x) -> x * 3
Maximum Technique: Sum
Reveal: stdout
`, "5\n-4\n2\n7\n8"},
		{"filter then max", `Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort
Cursed Technique: Filter
    Using: (x) -> x < 8
Maximum Technique: Max
Reveal: stdout
`, "5\n-4\n2\n7\n8"},
		{"map then min", `Cursed Technique: Map Each
    Using: (s) -> length(s)
Maximum Technique: Min
Reveal: stdout
`, "abc\nde\nfghi"},
		{"map then product", `Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort
Cursed Technique: Map Each
    Using: (x) -> x + 1
Maximum Technique: Product
Reveal: stdout
`, "1\n2\n3"},
		{"group sums, in first-seen key order", `Channeled Energy: Convert List to Integers
Maximum Technique: Group By
    Using: (n) -> n % 3
Cursed Technique: Map Values
    Using: (b) -> sum(b)
Reveal: stdout
`, "5\n3\n4\n9\n-2\n7"},
		{"group lengths", `Maximum Technique: Group By
    Using: (s) -> length(s)
Cursed Technique: Map Values
    Using: (b) -> length(b)
Reveal: stdout
`, "ab\nc\nde\nfgh\ni"},
		{"parsed floats summed", `Channeled Energy: Convert List to Floats
Maximum Technique: Sum
Reveal: stdout
`, "0.1\n0.2\n-3.5\n1e3"},
		{"widened ints, filtered, maxed", `Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort
Channeled Energy: Convert List to Floats
Cursed Technique: Filter
    Using: (x) -> x > 1.5
Maximum Technique: Max
Reveal: stdout
`, "3\n1\n2"},
		{"a text fold becomes one buffer", `Maximum Technique: Fold
    Seed: "<"
    Using: (acc, s) -> acc + upper(s) + ","
Reveal: stdout
`, "ab\n\ncd"},
		{"float sum", `Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort
Cursed Technique: Map Each
    Using: (x) -> x / 2.0
Maximum Technique: Sum
Reveal: stdout
`, "3\n4\n5"},
	}
	for _, tc := range cases {
		for _, optimize := range []bool{false, true} {
			name := tc.name + "/naive"
			if optimize {
				name = tc.name + "/optimized"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, head+tc.body, optimize)
				want := runInterpreter(t, pipe, []byte(tc.input))
				got := buildAndRun(t, pipe, []byte(tc.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}

// TestStreamFusionRemovesTheLists checks the fused path is the one taken: a
// Split feeding a stage never becomes a []string.
func TestStreamFusionRemovesTheLists(t *testing.T) {
	src := "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Map Each\n    Using: (s) -> upper(s) + \"!\"\n" +
		"Maximum Technique: Join with \"\\n\"\nReveal: stdout\n"
	goSrc, err := codegen.EmitProgram(compilePipeline(t, src, true), codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"strings.Split(", "strings.Join(", "make([]string"} {
		if strings.Contains(goSrc, unwanted) {
			t.Errorf("fused program still contains %q:\n%s", unwanted, goSrc)
		}
	}
}

// TestStreamFusionKeepsTheFirstFailure is the reason a fused run holds at most
// one stage that can fail. Here two can: the parse fails on the second line
// and the division on the first. Stage by stage the parse fails first, since
// it finishes every line before the division sees any; element by element the
// division would get there first. Both backends must report the parse.
func TestStreamFusionKeepsTheFirstFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	src := "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Map Each\n    Using: (s) -> trim(s)\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Cursed Technique: Map Each\n    Using: (x) -> 100 / x\n" +
		"Maximum Technique: Count\nReveal: stdout\n"
	input := []byte("0\nnope")
	pipe := compilePipeline(t, src, false)

	ctx := &ir.Context{Stdin: bytes.NewReader(input), Stdout: &bytes.Buffer{}}
	_, ierr := interp.Run(pipe, ctx)
	if ierr == nil || !strings.Contains(ierr.Error(), "not an integer") {
		t.Fatalf("interpreter error = %v, want the parse failure", ierr)
	}

	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		t.Fatalf("BuildBinary: %v\n%s", err, goSrc)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("the binary exited 0 where the interpreter failed")
	}
	if !strings.Contains(stderr.String(), "not an integer") {
		t.Errorf("binary reported %q, want the parse failure\n%s", stderr.String(), goSrc)
	}
}

// TestStreamFusionEmptyExtremumFails checks a fused Max still fails on an
// empty list — here one the filter emptied — in both backends.
func TestStreamFusionEmptyExtremumFails(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	src := "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Filter\n    Using: (s) -> length(s) > 9\n" +
		"Cursed Technique: Map Each\n    Using: (s) -> length(s)\n" +
		"Maximum Technique: Max\nReveal: stdout\n"
	input := []byte("a\nbb")
	pipe := compilePipeline(t, src, false)
	ctx := &ir.Context{Stdin: bytes.NewReader(input), Stdout: &bytes.Buffer{}}
	if _, err := interp.Run(pipe, ctx); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("interpreter error = %v, want the empty-list failure", err)
	}
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		t.Fatalf("BuildBinary: %v\n%s", err, goSrc)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "Max of an empty list") {
		t.Errorf("binary: err=%v stderr=%q, want the empty-list failure", err, stderr.String())
	}
}

// TestCompiledComponentsMatchInterpreter covers the compiled Connected
// Components (a flood fill that clears the mask it is given) and the
// one-character predicate that skips cutting each cell out of its line:
// either operand order, a multi-byte character, a byte that is not UTF-8,
// and both connectivities.
func TestCompiledComponentsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	prog := func(pred, mode string) string {
		src := "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n" +
			"Channeled Energy: Convert To Grid\nDomain Expansion: Connected Components\n" +
			"    Using: " + pred + "\n"
		if mode != "" {
			src += "    Mode: " + mode + "\n"
		}
		return src + "Reveal: stdout\n"
	}
	cases := []struct{ name, src, input string }{
		{"four-connected", prog(`(c) -> c = "#"`, ""), "#.#\n.#.\n#.#"},
		{"eight-connected", prog(`(c) -> c = "#"`, "8"), "#.#\n.#.\n#.#"},
		{"literal first", prog(`(c) -> "#" = c`, ""), "##.\n..#\n#.#"},
		{"multi-byte character", prog(`(c) -> c = "█"`, ""), "█·█\n██·\n··█"},
		{"a byte that is not UTF-8", prog(`(c) -> c = "#"`, ""), "#\xff#\n\xff##"},
		{"a general predicate", prog(`(c) -> c = "#" or c = "@"`, "8"), "#.@\n.@.\n..#"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pipe := compilePipeline(t, tc.src, true)
			want := runInterpreter(t, pipe, []byte(tc.input))
			if got := buildAndRun(t, pipe, []byte(tc.input), codegen.Options{}); got != want {
				t.Errorf("interpreter %q, binary %q", want, got)
			}
		})
	}
}
