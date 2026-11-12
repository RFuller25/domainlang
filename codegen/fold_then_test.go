package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Fold's Then: clause, compiled vs interpreted, with every fold-matching
// optimizer pass given a chance to fire (and required to leave the Then alone).
func TestFoldThenParity(t *testing.T) {
	head := "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nChanneled Energy: Convert To Integers\n"
	progs := []struct{ name, src, input string }{
		{"bindings", head + "Maximum Technique: Fold\n    Consider depth As 0\n    Consider dist As 0\n    Seed: 0\n" +
			"    Using: (acc, x) -> if x > 0 then (dist := dist + x) else (depth := depth - x)\n" +
			"    Then: (acc) -> depth * dist\nReveal: stdout\n", "5,-3,2,-4"},
		{"sum idiom", head + "Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + x\n    Then: (acc) -> acc * 10\nReveal: stdout\n", "1,2,3"},
		{"linear accumulator", head + "Maximum Technique: Fold\n    Seed: 1\n    Using: (acc, x) -> acc * 3 + x\n    Then: (acc) -> mod(acc, 1000)\nReveal: stdout\n", "1,2,3,4,5,6,7,8"},
		{"to text", head + "Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + x\n    Then: (acc) -> text(acc * 2)\nReveal: stdout\n", "1,2,3"},
		{"empty list", head + "Cursed Technique: Filter\n    Using: (x) -> x > 100\nMaximum Technique: Fold\n    Seed: 7\n    Using: (acc, x) -> acc + x\n    Then: (acc) -> acc * 2\nReveal: stdout\n", "1,2,3"},
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
