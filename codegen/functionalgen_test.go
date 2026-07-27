package codegen_test

import (
	"testing"

	"domain/codegen"
)

// TestCompiledFunctionalMatchesInterpreter pins interpreter/binary parity for
// Reduce, Scan (seeded and seedless), and Pairs — including the empty-list
// edges, where the three deliberately differ from each other.
func TestCompiledFunctionalMatchesInterpreter(t *testing.T) {
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
			name: "reduce is a left fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Reduce
    Using: (a, b) -> a * 10 + b
Reveal: stdout
`,
			input: "1,2,3,4",
		},
		{
			name: "reduce over points needs no seed",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> point(x, 1)
Maximum Technique: Reduce
    Using: (a, b) -> padd(a, b)
Reveal: stdout
`,
			input: "1,2,3,4",
		},
		{
			name: "reduce of a single element skips the lambda",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Reduce
    Using: (a, b) -> a - b
Reveal: stdout
`,
			input: "42",
		},
		{
			name: "seedless scan renders running totals",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Scan
    Using: (a, b) -> a + b
Reveal: stdout
`,
			input: "1,2,3,4,5",
		},
		{
			name: "seeded scan changes the accumulator type",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Scan
    Seed: 0
    Using: (acc, s) -> acc + toint(s)
Reveal: stdout
`,
			input: "2,3,1",
		},
		{
			name: "scan of an empty list is empty",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Using: (x) -> x > 100
Cursed Technique: Scan
    Using: (a, b) -> a + b
Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`,
			input: "1,2,3",
		},
		{
			name: "scan over points is a running position",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> point(x, 1)
Cursed Technique: Scan
    Using: (a, b) -> padd(a, b)
Reveal: stdout
`,
			input: "1,2,3",
		},
		{
			name: "pairs count increases",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Pairs
Reveal: stdout
Maximum Technique: Count Matching
    Using: (p) -> pcol(p) > prow(p)
Reveal: stdout
`,
			input: "199\n200\n208\n210\n200\n207\n240\n269\n260\n263",
		},
		{
			name: "pairs of text, and of a list too short to pair",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Pairs
Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`,
			input: "solo",
		},
		{
			name: "scan then pairs then reduce compose",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Scan
    Using: (a, b) -> a + b
Cursed Technique: Pairs
Cursed Technique: Map Each
    Using: (p) -> pcol(p) - prow(p)
Maximum Technique: Reduce
    Using: (a, b) -> a + b
Reveal: stdout
`,
			input: "5,1,4,1,3",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			p, optimize := p, optimize
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
