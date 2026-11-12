package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Find Cycle's step form, compiled vs interpreted.
func TestFindCycleStepFormParity(t *testing.T) {
	head := "Cursed Energy: stdin\nCursed Technique: Apply\n    Using: (s) -> toint(s)\n"
	progs := []struct{ name, src, input string }{
		{"plain", head + "Maximum Technique: Find Cycle\n    Using: (n) -> mod(n * 3 + 1, 7)\nReveal: stdout\n", "3"},
		{"two-state", head + "Maximum Technique: Find Cycle\n    Using: (n) -> 1 - n\nReveal: stdout\n", "0"},
		{"limit no repeat", head + "Maximum Technique: Find Cycle\n    Using: (n) -> n + 1\n    Limit: 5\nReveal: stdout\n", "0"},
		{"measured limit", head + "Maximum Technique: Find Cycle\n    Using: (n) -> n + 1\n    Limit: (n) -> n + 3\nReveal: stdout\n", "2"},
		{"key", head + "Maximum Technique: Find Cycle\n    Using: (n) -> n + 1\n    Key: (n) -> mod(n, 4)\nReveal: stdout\n", "0"},
		{"tuple state", head + "Cursed Technique: Apply\n    Using: (n) -> tuple(n, 0)\n" +
			"Maximum Technique: Find Cycle\n    Using: (p) -> tuple(mod(item(p, 0) + 2, 5), item(p, 1) + 1)\n    Key: (p) -> item(p, 0)\nReveal: stdout\n", "1"},
		{"list state, no key", "Cursed Energy: stdin\nCursed Technique: Extract Integers\n" +
			"Maximum Technique: Find Cycle\n    Using: (xs) -> concat(drop(xs, 1), take(xs, 1))\nReveal: stdout\n", "4 1 4 1 7 7"},
		{"list state behind a key holding a list", "Cursed Energy: stdin\nCursed Technique: Extract Integers\n" +
			"Maximum Technique: Find Cycle\n    Using: (xs) -> concat(drop(xs, 1), take(xs, 1))\n    Key: (xs) -> tuple(take(xs, 2), length(xs))\nReveal: stdout\n", "4 1 4 1 7 7"},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			p, optimize := p, optimize
			name := p.name + "/naive"
			if optimize {
				name = p.name + "/optimized"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("interpreter %q, binary %q", want, got)
				}
			})
		}
	}
}
