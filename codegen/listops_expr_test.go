package codegen_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
)

// Interpreter/binary byte parity for the first-order list builtins.
//
// The corners are what these pin: chunk keeps a short final block and windows
// drops a partial one, zip truncates to the shorter, product is 1 on the empty
// list, and sort reaches every ordered element type — including tuples, which
// go through the interned three-way compare rather than a Go operator.
func TestCompiledListBuiltinsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			name: "the whole table over one list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> textjoin(list(
        textjoin(list(totext(first(sort(xs))), totext(last(sort(xs)))), ","),
        totext(length(unique(xs))),
        totext(product(take(xs, 3))),
        totext(length(zip(xs, take(xs, 2)))),
        totext(item(last(enumerate(xs)), 0)),
        totext(length(chunk(xs, 3))),
        totext(length(windows(xs, 3))),
        totext(length(flatten(chunk(xs, 2))))
    ), "|")
Reveal: stdout
`,
			input: "5\n3\n9\n3\n1\n7\n2",
		},
		{
			// chunk keeps a short final block, windows drops a partial one —
			// tested at a length where the two disagree.
			name: "chunk and windows at the boundary",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> textjoin(list(
        totext(length(chunk(xs, 3))), totext(length(last(chunk(xs, 3)))),
        totext(length(windows(xs, 3))),
        totext(length(chunk(xs, 99))), totext(length(windows(xs, 99)))
    ), "|")
Reveal: stdout
`,
			input: "1\n2\n3\n4",
		},
		{
			// sort over Text and over tuples: the second goes through the
			// interned dmCmpN rather than a Go operator.
			name: "sort over text and tuple elements",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Apply
    Using: (ws) -> textjoin(list(
        textjoin(sort(ws), ","),
        textjoin(unique(sort(ws)), ","),
        totext(prow(first(sort(zip(list(3, 1, 2), list(9, 8, 7))))))
    ), "|")
Reveal: stdout
`,
			input: "pear\nfig\napple\nfig",
		},
		{
			// transpose inside a lambda, over the list-of-lists a positional
			// Match Pattern produces.
			name: "transpose in an expression",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Apply
    Using: (rows) -> textjoin(list(
        totext(length(transpose(rows))),
        totext(sum(first(transpose(rows)))),
        totext(sum(flatten(transpose(rows))))
    ), "|")
Reveal: stdout
`,
			input: "1 2 3\n4 5 6",
		},
		{
			// Inside a Fold, where a pipeline body cannot stand in for the
			// 2-parameter lambda at all — the place these were not merely
			// verbose to do without but impossible.
			name: "used inside a fold lambda",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Maximum Technique: Fold
    Seed: (xs) -> 0
    Using: (acc, row) -> acc + first(sort(row)) * length(unique(row))
Reveal: stdout
`,
			input: "3 1 2\n9 9 4\n5 5 5",
		},
		{
			name: "product over floats",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Floats
Cursed Technique: Apply
    Using: (xs) -> totext(product(xs)) + "|" + totext(product(take(xs, 0)))
Reveal: stdout
`,
			input: "0.5\n4.0\n3.0",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}

// The partial ones must fail in both backends, or the two disagree about
// whether the program runs at all.
func TestCompiledListBuiltinFailuresMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	for _, tc := range []struct{ name, body string }{
		{"chunk of zero", "(xs) -> length(chunk(xs, 0))"},
		{"windows of zero", "(xs) -> length(windows(xs, 0))"},
		{"ragged transpose", "(xs) -> length(transpose(list(list(1, 2), list(3))))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Cursed Energy: stdin\n" +
				"Cursed Technique: Split Text by \"\\n\"\n" +
				"Channeled Energy: Convert List to Integers\n" +
				"Cursed Technique: Apply\n    Using: " + tc.body + "\nReveal: stdout\n"
			pipe := compilePipeline(t, src, false)
			input := []byte("1\n2")

			var out bytes.Buffer
			ctx := &ir.Context{Stdin: bytes.NewReader(input), Stdout: &out}
			if _, err := interp.Run(pipe, ctx); err == nil {
				t.Fatal("the interpreter should have failed")
			}
			goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
			if err != nil {
				t.Fatalf("EmitProgram: %v", err)
			}
			dir := t.TempDir()
			bin := filepath.Join(dir, "prog")
			if err := codegen.BuildBinary(goSrc, bin); err != nil {
				t.Fatalf("BuildBinary: %v\n%s", err, goSrc)
			}
			cmd := exec.Command(bin)
			cmd.Stdin = bytes.NewReader(input)
			cmd.Dir = dir
			if err := cmd.Run(); err == nil {
				t.Error("the binary exited 0 where the interpreter failed")
			}
		})
	}
}
