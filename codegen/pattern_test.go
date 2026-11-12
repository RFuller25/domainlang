package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
	"domain/optimizer"
)

// Destructuring lambda parameters (docs/expressions.md) are desugared by the
// parser into `consider … as item($p, k) in`, so both backends see ordinary
// lets. These hold the binary to the interpreter, optimized and naive.
func TestCompiledDestructuringLambdaMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct{ name, body string }{
		{"fold with a pair parameter", `Cursed Technique: Enumerate
Maximum Technique: Fold
    Seed: 0
    Using: (acc, (i, x)) -> acc + i * x
`},
		{"nested pattern and ignored field", `Cursed Technique: Map Each
    Using: (x) -> tuple(x, tuple(x + 1, x * 2), 7)
Maximum Technique: Fold
    Seed: 0
    Using: (acc, (a, (b, c), _)) -> acc + a + b + c
`},
		{"filter and map over tuples", `Cursed Technique: Enumerate
Cursed Technique: Filter
    Using: ((i, x)) -> i % 2 = 0
Cursed Technique: Map Each
    Using: ((i, x)) -> i + x
`},
		{"text fields", `Cursed Technique: Map Each
    Using: (x) -> tuple(totext(x), x)
Cursed Technique: Map Each
    Using: ((s, n)) -> length(s) + n
`},
		{"function binding with a pattern", `Cursed Technique: Map Each
    Consider addp As (k, (a, b)) -> k * (a + b)
    Using: (x) -> addp(2, tuple(x, 1))
`},
		{"state loop", `Cursed Technique: Apply
    Using: (xs) -> tuple(xs, 0, length(xs), 0)
Simple Domain: While
    Consider l Of (x) -> item(x, 2)
    Using: ((xs, i, n, c)) -> i > -1 and i < l
    Apply
        Using: ((xs, i, n, c)) ->
            consider jump as item(xs, i) in
            consider newl as set(xs, i, jump + 1) in
            tuple(newl, i + jump, n, c + 1)
Cursed Technique: Apply
    Using: ((_, _, _, c)) -> c
`},
	}
	for _, tc := range cases {
		for _, input := range []string{"5 1 9 5 1 7 9 2 2 8 4", "0 3 0 1 -3", "7"} {
			t.Run(tc.name+"/"+strings.ReplaceAll(input, " ", "_"), func(t *testing.T) {
				src := head + tc.body + "Reveal: stdout\n"
				pipe, err := frontEnd(src, true)
				if err != nil {
					t.Skipf("front end: %v", err) // a shape this input cannot satisfy
				}
				want := runInterpreter(t, pipe, []byte(input))
				naive := runInterpreter(t, compilePipeline(t, src, false), []byte(input))
				if want != naive {
					t.Fatalf("optimized interpreter %q, naive %q", want, naive)
				}
				if got := buildAndRun(t, pipe, []byte(input), codegen.Options{}); got != want {
					t.Errorf("interpreter %q, binary %q", want, got)
				}
			})
		}
	}
}

// The linear in-place pass refuses an update when the state is read after the
// write. A destructured parameter binds every field before the body, which is
// the shape it wants, so a While threading a tuple through `set` still
// writes in place.
func TestDestructuredWhileStateWritesInPlace(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Extract Integers
Cursed Technique: Apply
    Using: (x) -> tuple(x, 0, length(x), 0)
Simple Domain: While
    Consider l Of (x) -> item(x, 2)
    Using: ((xs, i, _, c)) -> i > -1 and i < l
    Apply
        Using: ((xs, i, n, c)) ->
            consider jump as item(xs, i) in
            consider newl as set(xs, i, jump + 1) in
            tuple(newl, i + jump, n, c + 1)
Cursed Technique: Apply
    Using: ((_, _, _, c)) -> c
Reveal: stdout
`
	pipe := compilePipeline(t, src, false)
	resolveMu.Lock()
	rewrites := optimizer.Optimize(pipe, true)
	resolveMu.Unlock()
	for _, r := range rewrites {
		if strings.Contains(r.Message, "write in place") {
			return
		}
	}
	t.Fatalf("expected a `write in place` rewrite, got %v", rewrites)
}

func TestDestructuringTypeErrors(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct{ name, body, want string }{
		{"not a tuple", "Cursed Technique: Map Each\n    Using: ((a, b)) -> a + b\n",
			"the pattern (a, b) destructures a Tuple, but this parameter is Int"},
		{"arity", "Cursed Technique: Map Each\n    Using: (x) -> tuple(x, x, x)\nCursed Technique: Map Each\n    Using: ((a, b)) -> a + b\n",
			"the pattern (a, b) has 2 fields, but this parameter is (Int, Int, Int) with 3"},
		{"nested", "Cursed Technique: Map Each\n    Using: (x) -> tuple(x, x)\nCursed Technique: Map Each\n    Using: ((a, (b, c))) -> a\n",
			"the pattern (b, c) destructures a Tuple, but this parameter is Int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := frontEnd(head+tc.body+"Reveal: stdout\n", false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}
