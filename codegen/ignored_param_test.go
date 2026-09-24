package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
)

// A lambda may ignore its parameters — `(x) -> 5` is legal Domain — and every
// list primitive that takes one is run here with one that ignores what it is
// given, in both backends. Two kinds of program the interpreter ran and the
// compiler refused were found this way:
//
//   - Map Each, Scan, Fold and Sort By bound the element to a loop variable
//     only the lambda used, and Go refuses a variable nothing reads;
//   - Any, All, Take While and Drop While with a constant predicate are
//     rewritten by the optimizer into nodes with no lambda, which the backend
//     did not recognise.
func TestCompiledLambdaMayIgnoreItsParameters(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	stages := []string{
		"Cursed Technique: Apply\n    Using: (x) -> 5",
		"Cursed Technique: Drop While\n    Using: (x) -> 1 = 0",
		"Cursed Technique: Filter\n    Using: (x) -> 1 = 1",
		"Cursed Technique: Filter\n    Using: (x) -> 1 = 0",
		"Cursed Technique: Map Each\n    Using: (x) -> 5",
		"Cursed Technique: Partition\n    Using: (x) -> 1 = 1",
		"Cursed Technique: Scan\n    Seed: 0\n    Using: (a, x) -> a + 1",
		"Cursed Technique: Scan\n    Using: (a, x) -> a",
		"Cursed Technique: Take While\n    Using: (x) -> 1 = 1",
		"Cursed Technique: Take While\n    Using: (x) -> 1 = 0",
		"Cursed Technique: Drop While\n    Using: (x) -> 1 = 1",
		"Maximum Technique: All\n    Using: (x) -> 1 = 1",
		"Maximum Technique: All\n    Using: (x) -> 1 = 0",
		"Maximum Technique: Any\n    Using: (x) -> 1 = 1",
		"Maximum Technique: Any\n    Using: (x) -> 1 = 0",
		"Maximum Technique: Count By\n    Using: (x) -> 1",
		"Maximum Technique: Count Matching\n    Using: (x) -> 1 = 1",
		"Maximum Technique: Count Matching\n    Using: (x) -> 1 = 0",
		"Maximum Technique: Find\n    Using: (x) -> 1 = 1",
		"Maximum Technique: Find Index\n    Using: (x) -> 1 = 1",
		"Maximum Technique: Fold\n    Seed: 0\n    Using: (a, x) -> a + 1",
		"Maximum Technique: Group By\n    Using: (x) -> 1",
		"Maximum Technique: Max By\n    Using: (x) -> 1",
		"Maximum Technique: Min By\n    Using: (x) -> 1",
		"Maximum Technique: Product By\n    Using: (x) -> 2",
		"Maximum Technique: Reduce\n    Using: (a, b) -> a",
		"Maximum Technique: Sum By\n    Using: (x) -> 1",
		"Domain Expansion: Sort By\n    Using: (x) -> 1",
	}
	for _, stage := range stages {
		for _, sorted := range []bool{false, true} {
			for _, optimize := range []bool{false, true} {
				src := "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n" +
					"Channeled Energy: Convert List to Integers\n"
				// A sort in front changes which lowerings the optimizer and the
				// fusers pick for the same stage.
				if sorted {
					src += "Domain Expansion: Quicksort\n"
				}
				src += stage + "\nReveal: stdout\n"
				name := strings.SplitN(stage, "\n", 2)[0]
				if sorted {
					name += "/sorted"
				}
				if optimize {
					name += "/optimized"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					pipe := compilePipeline(t, src, optimize)
					input := []byte("3\n1\n2")
					want := runInterpreter(t, pipe, input)
					if got := buildAndRun(t, pipe, input, codegen.Options{}); got != want {
						t.Errorf("interpreter %q, binary %q", want, got)
					}
				})
			}
		}
	}
}
