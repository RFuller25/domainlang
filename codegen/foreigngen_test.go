package codegen_test

import (
	"os/exec"
	"strings"
	"testing"

	"domain/codegen"
)

// The interpreter is the oracle for foreign blocks exactly as it is for every
// other primitive: the same program's stdout, byte for byte, from `domain run`
// and from the binary. That is the only thing holding the two implementations
// of the wire format together — one in prims/foreign.go, one emitted by
// codegen/foreigngen.go — so it is worth having over every shape the format
// distinguishes.

func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("no python3 on PATH")
		}
	}
}

func TestCompiledForeignMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	requirePython(t)

	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			name: "text in, text out",
			src: `Cursed Energy: stdin
Domain Expansion: Python
    import sys
    print(sys.stdin.read().upper(), end="")
Reveal: stdout
`,
			input: "hello\nworld\n",
		},
		{
			name: "list of ints in, one int out",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Python : List<Int> -> Int
    import sys
    print(sum(int(x) for x in sys.stdin))
Reveal: stdout
`,
			input: "1\n2\n3\n4\n",
		},
		{
			name: "list in, list out, then the vocabulary takes over again",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Python : List<Int> -> List<Int>
    import sys
    for line in sys.stdin:
        print(int(line) * 3)
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 2, Sum
Reveal: stdout
`,
			input: "5\n1\n9\n2\n",
		},
		{
			name: "list of text out",
			src: `Cursed Energy: stdin
Domain Expansion: Python : Text -> List<Text>
    import sys
    for line in sys.stdin:
        print(line.strip()[::-1])
Maximum Technique: Join
Reveal: stdout
`,
			input: "abc\ndef\n",
		},
		{
			name: "floats and bools cross the wire",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Floats
Domain Expansion: Python : List<Float> -> Float
    import sys
    print(sum(float(x) for x in sys.stdin) / 2)
Reveal: stdout
`,
			input: "1.5\n2.25\n",
		},
		{
			name: "a bool answer",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Python : List<Int> -> Bool
    import sys
    print(str(all(int(x) > 0 for x in sys.stdin)).lower())
Reveal: stdout
`,
			input: "3\n4\n5\n",
		},
		{
			name: "a grid goes in as its picture",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: Python : Grid<Text> -> Int
    import sys
    print(sum(row.count("#") for row in sys.stdin))
Reveal: stdout
`,
			input: "#.#\n..#\n###\n",
		},
		{
			name: "the empty list makes the round trip",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Using: (x) -> x > 1000
Domain Expansion: Python : List<Int> -> List<Int>
    import sys
    for line in sys.stdin:
        print(line.strip())
Maximum Technique: Count
Reveal: stdout
`,
			input: "1\n2\n3\n",
		},
		{
			name: "two blocks in one pipeline",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Python : List<Int> -> List<Int>
    import sys
    for line in sys.stdin:
        print(int(line) + 1)
Domain Expansion: Python : List<Int> -> Int
    import sys
    print(max(int(x) for x in sys.stdin))
Reveal: stdout
`,
			input: "7\n3\n11\n",
		},
		{
			name: "inside a Part block",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Part "1":
    Maximum Technique: Sum
    Reveal: stdout
Part "2":
    Domain Expansion: Python : List<Int> -> Int
        import sys
        print(max(int(x) for x in sys.stdin))
    Reveal: stdout
`,
			input: "4\n9\n2\n",
		},
	}

	for _, p := range progs {
		t.Run(p.name, func(t *testing.T) {
			for _, optimize := range []bool{false, true} {
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("optimize=%v: binary printed %q, interpreter printed %q",
						optimize, got, want)
				}
			}
		})
	}
}

// A foreign block is opaque: the optimizer has no rewrite that names it, and
// the compiled program must contain the block exactly as written rather than
// anything derived from it.
func TestCompiledForeignEmbedsTheBlock(t *testing.T) {
	src := `Cursed Energy: stdin
Domain Expansion: Python
    import sys
    print(sys.stdin.read(), end="")
Reveal: stdout
`
	goSrc, err := codegen.EmitProgram(compilePipeline(t, src, true), codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(goSrc, `"import sys\nprint(sys.stdin.read(), end=\"\")\n"`) {
		t.Error("the block was not embedded verbatim in the generated source")
	}
	if !strings.Contains(goSrc, `Env: "DOMAIN_PYTHON"`) {
		t.Error("the generated program does not honor the runtime override")
	}
}

// A failing block must fail the binary the way it fails the interpreter: the
// runtime's own words, and a non-zero exit.
func TestCompiledForeignFailureIsReported(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	requirePython(t)

	src := `Cursed Energy: stdin
Domain Expansion: Python
    raise ValueError("deliberate")
Reveal: stdout
`
	goSrc, err := codegen.EmitProgram(compilePipeline(t, src, true), codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := dir + "/prog"
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		t.Fatalf("BuildBinary: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader("x\n")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("the binary succeeded despite a raising block")
	}
	if !strings.Contains(string(out), "deliberate") {
		t.Errorf("the binary swallowed the traceback: %s", out)
	}
	if !strings.Contains(string(out), "Python block failed with status") {
		t.Errorf("the binary did not name the language: %s", out)
	}
}
