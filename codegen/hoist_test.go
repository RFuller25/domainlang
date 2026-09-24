package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
	"domain/optimizer"
)

// Loop-invariant code motion (optimizer/hoist.go) wraps a stage in a
// synthesized Consider that binds the work its lambda repeated per element.
// The compiled backend runs that Consider like any written one; these hold it
// to the interpreter, over empty input too — the case where the stage never
// ran its lambda and the hoisted value is computed anyway.
func TestCompiledHoistingMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	// Extract Integers reads empty input as the empty list, which is the case
	// that matters most here: the stage never runs its lambda at all.
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct {
		name, body, hoisted string
	}{
		{"membership in a bound list becomes a set", `Cursed Technique: Filter
    Consider wanted Of (xs) -> take(xs, 3)
    Using: (x) -> contains(wanted, x)
Reveal: stdout
`, "a set once"},
		{"invariant work in a map", `Cursed Technique: Map Each
    Consider all Of Itself
    Using: (x) -> x * 100 + sum(sort(all))
Reveal: stdout
`, "hoisted"},
		{"membership in a global list", `Cursed Object: primes As list(2, 3, 5, 7)
Cursed Technique: Filter
    Using: (x) -> contains(primes, x)
Reveal: stdout
`, "a set once"},
		{"inside a loop body, once per lap", `Simple Domain: Repeat 2
    Cursed Technique: Map Each
        Consider all Of Itself
        Using: (x) -> x + length(unique(all))
Reveal: stdout
`, "hoisted"},
		{"a For variable is not invariant", `Simple Domain: For d in range(2)
    Cursed Technique: Map Each
        Consider all Of Itself
        Using: (x, d) -> x + d + sum(all)
Reveal: stdout
`, "hoisted"},
	}
	for _, tc := range cases {
		for _, input := range []string{"5 1 9 5 1 7 9", ""} {
			name := tc.name
			if input == "" {
				name += "/empty"
			}
			// Serial: resolving a For loop pushes onto package-level ambient
			// state (prims/ambient.go) that the interpreter reads, so one
			// subtest resolving while another interprets races.
			t.Run(name, func(t *testing.T) {
				pipe := compilePipeline(t, head+tc.body, false)
				// Under the lock frontEnd resolves under: the optimizer types
				// the hoisted expressions, and the typechecker keeps state.
				resolveMu.Lock()
				rewrites := optimizer.Optimize(pipe, true)
				resolveMu.Unlock()
				fired := false
				for _, r := range rewrites {
					fired = fired || strings.Contains(r.Message, tc.hoisted)
				}
				if !fired {
					t.Fatalf("expected a rewrite containing %q, got %v", tc.hoisted, rewrites)
				}
				want := runInterpreter(t, pipe, []byte(input))
				naive := runInterpreter(t, compilePipeline(t, head+tc.body, false), []byte(input))
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
