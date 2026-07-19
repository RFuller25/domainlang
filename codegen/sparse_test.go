package codegen_test

import (
	"testing"

	"domain/codegen"
)

// TestCompiledSparseMatchesInterpreter pins interpreter/binary byte parity
// for the sparse grid surface: every Convert To Sparse Grid source shape,
// densify, the cell primitives, and the expression-layer builtins — in
// both optimizer modes.
func TestCompiledSparseMatchesInterpreter(t *testing.T) {
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
			name: "points to sparse renders sorted",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: "."
    Mark: "#"
Reveal: stdout
`,
			input: "2,0\n0,5\n0,1\n2,0",
		},
		{
			name: "sparse densify with negative coordinates",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: "."
    Mark: "#"
Channeled Energy: Convert To Grid
Reveal: stdout
`,
			input: "-1,-1\n1,2\n0,0",
		},
		{
			name: "grid to sparse drops defaults and counts",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert Each List to Integers
Channeled Energy: Convert To Grid
Channeled Energy: Convert To Sparse Grid
    Default: 0
Reveal: stdout
Maximum Technique: Count Cells
    Using: (h) -> h >= 5
Reveal: stdout
`,
			input: "305\n007",
		},
		{
			name: "map to sparse via count by",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Cursed Technique: Map Each
    Using: (xs) -> point(item(xs, 0), item(xs, 1))
Maximum Technique: Count By
    Using: (p) -> p
Channeled Energy: Convert To Sparse Grid
    Default: 0
Reveal: stdout
Cursed Technique: Apply
    Using: (g) -> at(g, 0, 0) + at(g, 9, 9)
Reveal: stdout
`,
			input: "0,0\n1,2\n0,0",
		},
		{
			name: "sparse map cells transforms plane and default",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: "."
    Mark: "#"
Cursed Technique: Map Cells
    Using: (c) -> if c = "#" then 1 else 0
Reveal: stdout
Cursed Technique: Apply
    Using: (g) -> at(g, 9, 9)
Reveal: stdout
`,
			input: "0,0\n2,3",
		},
		{
			name: "sparse find cells sorted row-major",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: 0
    Mark: 1
Cursed Technique: Find Cells
    Using: (v) -> v = 1
Reveal: stdout
`,
			input: "5,5\n-2,8\n5,-5\n-2,-8",
		},
		{
			name: "sparse builtins bounds cells put has",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: 0
    Mark: 1
Cursed Technique: Apply
    Using: (g) -> put(g, maxrow(g) + 1, maxcol(g) + 1, cells(g))
Reveal: stdout
Cursed Technique: Apply
    Using: (g) -> list(minrow(g), maxrow(g), mincol(g), maxcol(g), cells(g), at(g, 100, 100))
Reveal: stdout
Cursed Technique: Apply
    Using: (xs) -> if length(xs) = 6 then "ok" else "bad"
Reveal: stdout
`,
			input: "-2,7\n4,-3",
		},
		{
			name: "sparse constructor in expression layer",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (x) -> put(put(sparse("."), 0, 0, x), 1, 3, "B")
Reveal: stdout
Channeled Energy: Convert To Grid
Reveal: stdout
`,
			input: "A",
		},
		{
			name: "sparse fixed point growth",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: 0
    Mark: 1
Simple Domain: Iterate Until Fixed Point
    Cursed Technique: Apply
        Using: (g) -> if has(g, 0, 3) then g else put(g, 0, mincol(g) + cells(g), 1)
Cursed Technique: Apply
    Using: (g) -> cells(g)
Reveal: stdout
`,
			input: "0,0",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			p := p
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
