package codegen_test

import (
	"testing"

	"domain/codegen"
)

// All Pairs / Combinations Mode: Any and Mode: All, compiled vs interpreted,
// optimized (the duplicate-check rewrite) and naive, over inputs too short to
// hold a combination as well as ordinary ones.
func TestPairsAnyAllParity(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	progs := []struct{ name, body string }{
		{"any equal", "Domain Expansion: All Pairs\n    Mode: Any\n    Using: (a, b) -> a = b\n"},
		{"all distinct", "Domain Expansion: All Pairs\n    Mode: All\n    Using: (a, b) -> ikke a = b\n"},
		{"any sum", "Domain Expansion: All Pairs\n    Mode: Any\n    Using: (a, b) -> a + b = 10\n"},
		{"all small", "Domain Expansion: All Pairs\n    Mode: All\n    Using: (a, b) -> a + b < 12\n"},
		{"triple any", "Domain Expansion: Combinations 3\n    Mode: Any\n    Using: (a, b, c) -> a + b + c = 15\n"},
		{"triple all", "Domain Expansion: Combinations 3\n    Mode: All\n    Using: (a, b, c) -> a + b + c < 20\n"},
	}
	for _, p := range progs {
		for _, input := range []string{"", "7", "7 3", "5 1 9 5 1 7 9 2 2 8 4", "1 2 3 4"} {
			for _, optimize := range []bool{true, false} {
				p, input, optimize := p, input, optimize
				name := p.name + "/" + input
				if optimize {
					name += "/optimized"
				} else {
					name += "/naive"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					src := head + p.body + "Reveal: stdout\n"
					pipe := compilePipeline(t, src, optimize)
					want := runInterpreter(t, pipe, []byte(input))
					got := buildAndRun(t, pipe, []byte(input), codegen.Options{})
					if got != want {
						t.Errorf("interpreter %q, binary %q", want, got)
					}
				})
			}
		}
	}
}
