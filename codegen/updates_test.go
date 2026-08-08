package codegen_test

import (
	"strings"
	"sync"
	"testing"

	"domain/codegen"
	"domain/ir"
)

// frontEndMu serializes the two halves of a case that touch package-level
// state: resolution (typecheck's binding stack) and interpretation (eval's).
// Both are documented as single-threaded, and these cases lean on bindings
// harder than anything else in the suite — every one of them has a `Consider`
// whose cell the run writes to. Building the binary, which is the slow half,
// stays parallel.
var frontEndMu sync.Mutex

// oracleFront runs the serialized half of an oracle case: resolve the program,
// then interpret it for the expected output.
//
// It exists because the obvious spelling — Lock, compile, interpret, Unlock —
// leaks the mutex whenever either step calls t.Fatal, since Goexit runs
// deferred functions and there were none. The leak is worse than the failure it
// follows: every other parallel case then blocks on a mutex nobody will ever
// release, the package hits the ten-minute timeout, and the subtest that
// actually failed never returns, so its buffered output is never printed. A
// one-line assertion failure comes out as a goroutine dump with no FAIL in it.
func oracleFront(t *testing.T, src string, optimize bool, input []byte) (*ir.Pipeline, string) {
	t.Helper()
	frontEndMu.Lock()
	defer frontEndMu.Unlock()
	pipe := compilePipeline(t, src, optimize)
	return pipe, runInterpreter(t, pipe, input)
}

// The interpreter-vs-binary oracle for `:=` and `also`.
//
// Ordering is what these are really about. The interpreter evaluates operands
// left to right, always; Go orders the *function calls* in an expression and
// says nothing about when a bare variable is read relative to them. So every
// program below is written so that a read and a write to the same name race
// inside one expression, and every one of them must give the same answer from
// both backends — in both optimizer modes, since the optimizer's stand-down on
// an updating lambda is part of what makes that true.

// TestUpdateOracle compiles each program and diffs it against the interpreter.
func TestUpdateOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
`
	// input defaults to the five-line list; a program with a different shape
	// of source names its own.
	cases := []struct{ name, src, input string }{
		{"read before write", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> n + (n := x)
Reveal: stdout
`, ""},
		{"read after write", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> (n := x) + n
Reveal: stdout
`, ""},
		{"read on both sides", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> n + (n := x) + n
Reveal: stdout
`, ""},
		{"argument order", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> item(list(n, (n := x), n), 0) * 100 + item(list(n, (n := 0 - x), n), 2)
Reveal: stdout
`, ""},
		{"short-circuited write never runs", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> if x > 3 and (n := n + 1) > 0 then n else 0 - n
Reveal: stdout
`, ""},
		{"unselected arm never runs", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> (if x > 3 then n := n + 10 else n := n + 1) * 1000 + n
Reveal: stdout
`, ""},
		{"also runs after the body", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> n also n := n + x
Reveal: stdout
`, ""},
		{"also inside an expression", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> (x also n := n + x) + n
Reveal: stdout
`, ""},
		{"consider local written and read", header + `Cursed Technique: Map Each
    Using: (x) -> consider t as x in ((t := t * 2) + t)
Reveal: stdout
`, ""},
		{"shadowing local leaves the binding alone", header + `Cursed Technique: Map Each
    Consider n As 5
    Using: (x) -> (consider n as x in (n := n + 1)) * 100 + n
Reveal: stdout
`, ""},
		{"write survives into the next stage", header + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> x also n := n + x
Maximum Technique: Sum
Cursed Technique: Apply
    Using: (s) -> s
Reveal: stdout
`, ""},
		{"filter counts what it kept", header + `Cursed Technique: Filter
    Consider kept As 0
    Using: (x) -> x > 2 and (kept := kept + 1) > 0
Maximum Technique: Count
Reveal: stdout
`, ""},
		{"fold accumulator beside a binding", header + `Maximum Technique: Fold
    Seed: (xs) -> 0
    Consider seen As 0
    Using: (acc, x) -> acc + x + (seen := seen + 1)
Reveal: stdout
`, ""},
		{"nested writes in one expression", header + `Cursed Technique: Map Each
    Consider a As 1
    Consider b As 2
    Using: (x) -> (a := b := x) + a + b
Reveal: stdout
`, ""},
		{"write inside a loop lap", `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(trim(s))
Simple Domain: Repeat 4
    Consider laps As 0
    Cursed Technique: Apply
        Using: (v) -> v * 2 also laps := laps + 1
    Cursed Technique: Apply
        Using: (v) -> v + laps
Reveal: stdout
`, "7\n"},
	}

	for _, c := range cases {
		input := []byte("1\n2\n3\n4\n5\n")
		if c.input != "" {
			input = []byte(c.input)
		}
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe, want := oracleFront(t, c.src, optimize, input)
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q\n\n%s", want, got, c.src)
				}
			})
		}
	}
}

// TestUpdateInsideBlockBody covers the shape the compiler used to refuse. A
// `Using:` written as an indented pipeline becomes a Go function of its own,
// so a binding it writes to travels as a `*T` rather than a copy — which is
// the Go analogue of the interpreter's one shared binding stack. The write has
// to be visible to the next element and the next lap, in both backends.
func TestUpdateInsideBlockBody(t *testing.T) {
	header := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
`
	cases := []struct {
		name string
		src  string
	}{
		{"body writes an enclosing binding", header + `Cursed Technique: Map Each
    Consider n As 0
    Cursed Technique: Apply
        Using: (s) -> s + (n := n + 1)
Reveal: stdout
`},
		{"the write outlives the body", header + `Cursed Technique: Apply
    Consider n As 0
    Consider tot Of
        Cursed Technique: Map Each
            Cursed Technique: Apply
                Using: (x) -> x + (n := n + 1)
        Maximum Technique: Sum
    Using: (xs) -> tot * 100 + n
Reveal: stdout
`},
		{"bodies nested two deep", header + `Cursed Technique: Map Each
    Using: (x) -> list(x, x + 1)
Cursed Technique: Map Each
    Consider n As 0
    Cursed Technique: Map Each
        Cursed Technique: Apply
            Using: (x) -> x + (n := n + 1)
    Maximum Technique: Sum
Reveal: stdout
`},
		{"loop body inside a pipeline body", header + `Cursed Technique: Map Each
    Consider n As 0
    Simple Domain: Repeat 2
        Cursed Technique: Apply
            Using: (x) -> x + (n := n + 1)
Reveal: stdout
`},
		{"a body writes the binding it opened itself", header + `Cursed Technique: Map Each
    Cursed Technique: Apply
        Consider n As 7
        Using: (x) -> x + (n := n + 1) + n
Reveal: stdout
`},
		{"predicate body writes, and filters on it", header + `Cursed Technique: Filter
    Consider kept As 0
    Cursed Technique: Apply
        Using: (x) -> (kept := kept + 1) < 3
Reveal: stdout
`},
	}

	for _, c := range cases {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				input := []byte("1\n2\n3\n4\n5\n")
				pipe, want := oracleFront(t, c.src, optimize, input)
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q\n\n%s", want, got, c.src)
				}
			})
		}
	}
}

// TestBlockBodyPassesOnlyWrittenBindingsByPointer pins the cost of the above to
// the programs that pay for it. A binding a body only *reads* still travels by
// value, so taking its address never forces it to the heap, and a body that
// writes nothing emits the signature it always emitted.
func TestBlockBodyPassesOnlyWrittenBindingsByPointer(t *testing.T) {
	header := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
`
	readOnly := compilePipeline(t, header+`Cursed Technique: Map Each
    Consider n As 3
    Cursed Technique: Apply
        Using: (x) -> x + n
Reveal: stdout
`, false)
	goSrc, err := codegen.EmitProgram(readOnly, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(goSrc, "*int64") {
		t.Fatalf("a read-only binding was passed by pointer:\n%s", goSrc)
	}

	written := compilePipeline(t, header+`Cursed Technique: Map Each
    Consider n As 3
    Cursed Technique: Apply
        Using: (x) -> x + (n := n + 1)
Reveal: stdout
`, false)
	goSrc, err = codegen.EmitProgram(written, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(goSrc, "*int64") {
		t.Fatalf("a written binding was not passed by pointer:\n%s", goSrc)
	}
}

// TestNoUpdatesNoOrderingWrappers keeps the ordering machinery off the path of
// every program that does not write: a lambda with no `:=` must compile to
// exactly the Go it compiled to before the operator existed.
func TestNoUpdatesNoOrderingWrappers(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Consider n As 3
    Using: (x) -> x + n * 2
Reveal: stdout
`
	pipe := compilePipeline(t, src, false)
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(goSrc, "func() int64 { return ") {
		t.Fatalf("an ordering wrapper was emitted for a program with no updates:\n%s", goSrc)
	}
}
