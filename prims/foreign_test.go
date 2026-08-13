package prims

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domain/ir"
)

// needsPython skips a test on a machine with no Python. The foreign block
// tests are the one place in this package that depends on software outside the
// repository, so they say so rather than failing.
func needsPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("no python3 on PATH")
		}
	}
}

// runForeignProgram resolves and runs a whole program, returning its Reveal
// output. Unlike runPipeline it returns errors rather than failing the test:
// half these cases are about what a failure says.
func runForeignProgram(t *testing.T, src, stdin string) (string, error) {
	t.Helper()
	pipe, err := resolveSrc(t, src)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader(stdin), Stdout: &out}
	var cur ir.Value
	for _, n := range pipe.Nodes {
		if cur, err = ir.EvalNode(ctx, n, cur); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

func TestForeignPythonTextToText(t *testing.T) {
	needsPython(t)
	src := "Cursed Energy: input.txt\n" +
		"Domain Expansion: Python\n" +
		"    import sys\n" +
		"    print(sys.stdin.read().upper(), end=\"\")\n" +
		"Reveal: stdout\n"
	got, err := runForeignProgram(t, src, "abc\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ABC\n" {
		t.Errorf("got %q, want %q", got, "ABC\n")
	}
}

func TestForeignPythonDeclaredSignature(t *testing.T) {
	needsPython(t)
	src := "Cursed Energy: input.txt\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Domain Expansion: Python : List<Int> -> Int\n" +
		"    import sys\n" +
		"    print(sum(int(x) for x in sys.stdin))\n" +
		"Reveal: stdout\n"
	got, err := runForeignProgram(t, src, "1\n2\n39\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "42\n" {
		t.Errorf("got %q, want %q", got, "42\n")
	}
}

func TestForeignPythonListOut(t *testing.T) {
	needsPython(t)
	src := "Cursed Energy: input.txt\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Domain Expansion: Python : List<Int> -> List<Int>\n" +
		"    import sys\n" +
		"    for x in sys.stdin:\n" +
		"        print(int(x) * 2)\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	got, err := runForeignProgram(t, src, "1\n2\n3\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "12\n" {
		t.Errorf("got %q, want %q", got, "12\n")
	}
}

// A block that fails must surface the runtime's own words, positioned at the
// statement rather than swallowed.
func TestForeignFailureReportsStderr(t *testing.T) {
	needsPython(t)
	src := "Cursed Energy: input.txt\n" +
		"Domain Expansion: Python\n" +
		"    raise ValueError(\"deliberate\")\n" +
		"Reveal: stdout\n"
	_, err := runForeignProgram(t, src, "x\n")
	if err == nil {
		t.Fatal("a raising block was accepted")
	}
	if !strings.Contains(err.Error(), "deliberate") {
		t.Errorf("error does not carry the traceback: %v", err)
	}
	if !strings.Contains(err.Error(), "Python block failed with status") {
		t.Errorf("error does not name the language: %v", err)
	}
}

// Output that does not match the declared type is the program's mistake, and
// the message says which line of the output was wrong.
func TestForeignOutputTypeMismatch(t *testing.T) {
	needsPython(t)
	src := "Cursed Energy: input.txt\n" +
		"Domain Expansion: Python : Text -> Int\n" +
		"    print(\"not a number\")\n" +
		"Reveal: stdout\n"
	_, err := runForeignProgram(t, src, "x\n")
	if err == nil {
		t.Fatal("non-numeric output was accepted as an Int")
	}
	if !strings.Contains(err.Error(), "expected an Int") {
		t.Errorf("unhelpful message: %v", err)
	}
}

// A missing runtime is a runtime error naming the override, not a panic.
func TestForeignMissingRuntime(t *testing.T) {
	t.Setenv("DOMAIN_PYTHON", "definitely-not-a-real-interpreter-xyz")
	src := "Cursed Energy: input.txt\n" +
		"Domain Expansion: Python\n" +
		"    print(1)\n" +
		"Reveal: stdout\n"
	_, err := runForeignProgram(t, src, "x\n")
	if err == nil {
		t.Fatal("a missing runtime was not reported")
	}
	if !strings.Contains(err.Error(), "could not run the Python block") {
		t.Errorf("unhelpful message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Resolve-time rules
// ---------------------------------------------------------------------------

func TestForeignResolveErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			"first stage",
			"Domain Expansion: Python\n    print(1)\n",
			"cannot be the first stage",
		},
		{
			"declared input disagrees with the pipeline",
			"Cursed Energy: input.txt\n" +
				"Domain Expansion: Python : List<Int> -> Int\n    print(1)\n",
			"declares it takes List<Int>, but the pipeline produced Text",
		},
		{
			"an output the wire format cannot carry",
			"Cursed Energy: input.txt\n" +
				"Domain Expansion: Python : Text -> Map<Text, Int>\n    print(1)\n",
			"cannot produce Map<Text, Int>",
		},
		{
			"an input the wire format cannot carry",
			"Cursed Energy: input.txt\n" +
				"Cursed Technique: Split Text by \"\\n\"\n" +
				"Channeled Energy: Convert To Set\n" +
				"Domain Expansion: Python\n    print(1)\n",
			"cannot be given Set<Text>",
		},
		{
			"a block under a keyword that takes none",
			"Cursed Energy: input.txt\nReveal: Python\n    print(1)\n",
			"does not take a block of Python code",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := resolveSrc(t, c.src)
			if err == nil {
				t.Fatalf("accepted:\n%s", c.src)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// The optimizer must not be able to reason about a foreign stage, so the node
// is never marked swappable.
func TestForeignNodeIsOpaque(t *testing.T) {
	pipe, err := resolveSrc(t, "Cursed Energy: input.txt\nDomain Expansion: Python\n    print(1)\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range pipe.Nodes {
		if n.Prim == "Foreign Block" && n.Swappable {
			t.Error("a foreign block is marked swappable; the optimizer may rewrite it")
		}
	}
}

// A Shikigami may not take a language's name, for the same reason it may not
// take a built-in's: the keyword is optional, so the call would be ambiguous.
func TestShikigamiCannotBeNamedAfterALanguage(t *testing.T) {
	for _, lang := range []string{"Python", "Go", "rask", "cRust", "Weave"} {
		src := "Shikigami \"" + lang + "\"\n    Maximum Technique: Sum\n"
		if _, err := resolveSrc(t, src); err == nil {
			t.Errorf("a Shikigami named %q was accepted", lang)
		}
	}
}

// ---------------------------------------------------------------------------
// The wire format, without a subprocess
// ---------------------------------------------------------------------------

func TestForeignEncode(t *testing.T) {
	cases := []struct {
		v    ir.Value
		t    *ir.Type
		want string
	}{
		{"abc", ir.Text(), "abc\n"},
		{int64(42), ir.Int(), "42\n"},
		{3.5, ir.Float(), "3.5\n"},
		{true, ir.Bool(), "true\n"},
		{[]ir.Value{int64(1), int64(2)}, ir.List(ir.Int()), "1\n2\n"},
		{[]ir.Value{}, ir.List(ir.Int()), ""},
		{"", ir.Text(), ""},
	}
	for _, c := range cases {
		got, err := foreignEncode(c.v, c.t)
		if err != nil {
			t.Errorf("encode %v: %v", c.v, err)
			continue
		}
		if got != c.want {
			t.Errorf("encode %v as %s: got %q, want %q", c.v, c.t, got, c.want)
		}
	}
}

func TestForeignDecode(t *testing.T) {
	cases := []struct {
		out  string
		t    *ir.Type
		want ir.Value
	}{
		{"abc\n", ir.Text(), "abc"},
		{"abc", ir.Text(), "abc"},
		{"42\n", ir.Int(), int64(42)},
		{"3.5\n", ir.Float(), 3.5},
		{"true\n", ir.Bool(), true},
		{"1\n2\n", ir.List(ir.Int()), []ir.Value{int64(1), int64(2)}},
		{"", ir.List(ir.Int()), []ir.Value{}},
	}
	for _, c := range cases {
		got, err := foreignDecode(c.out, c.t)
		if err != nil {
			t.Errorf("decode %q as %s: %v", c.out, c.t, err)
			continue
		}
		if !ir.DeepEqual(got, c.want) {
			t.Errorf("decode %q as %s: got %#v, want %#v", c.out, c.t, got, c.want)
		}
	}
}

// Encoding then decoding is the identity on every type that can make the round
// trip, which is what makes the two halves one format rather than two.
func TestForeignRoundTrip(t *testing.T) {
	cases := []struct {
		v ir.Value
		t *ir.Type
	}{
		{"a line", ir.Text()},
		{int64(-7), ir.Int()},
		{2.25, ir.Float()},
		{false, ir.Bool()},
		{[]ir.Value{int64(3), int64(1), int64(4)}, ir.List(ir.Int())},
		{[]ir.Value{"x", "y"}, ir.List(ir.Text())},
		{[]ir.Value{}, ir.List(ir.Int())},
	}
	for _, c := range cases {
		wire, err := foreignEncode(c.v, c.t)
		if err != nil {
			t.Errorf("encode %v: %v", c.v, err)
			continue
		}
		back, err := foreignDecode(wire, c.t)
		if err != nil {
			t.Errorf("decode %q: %v", wire, err)
			continue
		}
		if !ir.DeepEqual(back, c.v) {
			t.Errorf("round trip of %#v as %s gave %#v (wire %q)", c.v, c.t, back, wire)
		}
	}
}

// ---------------------------------------------------------------------------
// The four runners
// ---------------------------------------------------------------------------

// TestForeignCommands pins how each language is started: the file the block is
// written to, and the command line. Most are "interpreter, then the program";
// Go needs a module built around it and runs the directory, and Weave takes a
// `run` subcommand ahead of the file — its CLI documents `weave run
// file.weave` as "compile and run, feeding stdin to Source", which is the wire
// format's contract exactly.
func TestForeignCommands(t *testing.T) {
	cases := []struct {
		lang, env, file string
		wantTail        []string
		wantFiles       []string
	}{
		{"Python", "DOMAIN_PYTHON", "program.py", []string{"/dir/program.py"}, nil},
		{"rask", "DOMAIN_RASK", "program.rask", []string{"/dir/program.rask"}, nil},
		{"cRust", "DOMAIN_CRUST", "program.crust", []string{"/dir/program.crust"}, nil},
		{"Go", "DOMAIN_GO", "main.go", []string{"run", "."}, []string{"go.mod"}},
		{"Weave", "DOMAIN_WEAVE", "program.weave", []string{"run", "/dir/program.weave"}, nil},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			t.Setenv(c.env, "the-runtime")
			argv, files, err := foreignCommand(c.lang, "/dir")
			if err != nil {
				t.Fatal(err)
			}
			if got := foreignFile(c.lang); got != c.file {
				t.Errorf("block is written to %q, want %q", got, c.file)
			}
			want := append([]string{"the-runtime"}, c.wantTail...)
			if strings.Join(argv, " ") != strings.Join(want, " ") {
				t.Errorf("argv %q, want %q", argv, want)
			}
			for _, f := range c.wantFiles {
				if _, ok := files[f]; !ok {
					t.Errorf("%s needs a %s beside the block", c.lang, f)
				}
			}
		})
	}
}

// An override may name a command with arguments, which is what makes a runtime
// reachable when it is not a bare binary on PATH ("uv run python").
func TestForeignOverrideTakesArguments(t *testing.T) {
	t.Setenv("DOMAIN_PYTHON", "uv run python")
	argv, _, err := foreignCommand("Python", "/dir")
	if err != nil {
		t.Fatal(err)
	}
	want := "uv run python /dir/program.py"
	if strings.Join(argv, " ") != want {
		t.Errorf("argv %q, want %q", strings.Join(argv, " "), want)
	}
}

// rask, cRust and Weave are unlikely to be installed wherever this runs, so
// the whole path through them is exercised against a stand-in runtime named by
// the same override a user would set. What is under test is Domain's half of the
// bargain: the block reaches a file, the value reaches stdin, stdout comes
// back as the next value.
func TestForeignRunnerPlumbing(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	for _, c := range []struct{ lang, env string }{
		{"rask", "DOMAIN_RASK"},
		{"cRust", "DOMAIN_CRUST"},
		{"Weave", "DOMAIN_WEAVE"},
	} {
		t.Run(c.lang, func(t *testing.T) {
			dir := t.TempDir()
			// A runtime that proves it received both halves: it echoes the
			// program it was handed, then the input it was piped.
			stub := filepath.Join(dir, "stub.sh")
			// Weave is invoked as `weave run <program>`, so the stub drops a
			// leading `run` before treating its argument as the program.
			script := "#!/bin/sh\n[ \"$1\" = run ] && shift\ncat \"$1\"\ncat\n"
			if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv(c.env, "sh "+stub)

			src := "Cursed Energy: input.txt\n" +
				"Domain Expansion: " + c.lang + "\n" +
				"    THE BLOCK\n" +
				"Reveal: stdout\n"
			got, err := runForeignProgram(t, src, "THE INPUT\n")
			if err != nil {
				t.Fatal(err)
			}
			want := "THE BLOCK\nTHE INPUT\n"
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// A successful block's stderr is a debugging channel, not part of the value:
// it reaches the program's stderr and never the pipeline.
func TestForeignStderrPassesThroughOnSuccess(t *testing.T) {
	needsPython(t)
	src := "Cursed Energy: input.txt\n" +
		"Domain Expansion: Python\n" +
		"    import sys\n" +
		"    print(\"noted\", file=sys.stderr)\n" +
		"    print(\"answer\")\n" +
		"Reveal: stdout\n"
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader("x\n"), Stdout: &out, Stderr: &errOut}
	var cur ir.Value
	for _, n := range pipe.Nodes {
		if cur, err = ir.EvalNode(ctx, n, cur); err != nil {
			t.Fatal(err)
		}
	}
	if out.String() != "answer\n" {
		t.Errorf("stdout %q, want %q", out.String(), "answer\n")
	}
	if !strings.Contains(errOut.String(), "noted") {
		t.Errorf("the block's stderr did not reach the program's: %q", errOut.String())
	}
}
