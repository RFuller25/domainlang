package codegen_test

import (
	"testing"

	"domain/codegen"
)

// TestCompiledListOpsMatchInterpreter pins interpreter/binary parity for the
// list-shaping, generating, early-exit and keyed primitives, plus the
// optimizer rewrites that produce them. Every program runs in both optimized
// and naive modes and must print exactly what the interpreter printed.
func TestCompiledListOpsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		// --- Take While / Drop While ---
		{
			name: "take while and drop while split at the boundary",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Take While
    Using: (x) -> x < 4
Reveal: stdout
`,
			input: "1,2,9,3",
		},
		{
			name: "drop while keeps the rest",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Drop While
    Using: (x) -> x < 4
Reveal: stdout
Maximum Technique: Sum
Reveal: stdout
`,
			input: "1,2,9,3",
		},
		{
			name: "take while taking everything, and nothing",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Take While
    Using: (x) -> x < 100
Reveal: stdout
Cursed Technique: Take While
    Using: (x) -> x > 100
Reveal: stdout
`,
			input: "1,2,3",
		},
		{
			name: "concat onto a take while result does not alias",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Take While
    Using: (x) -> x < 4
Cursed Technique: Apply
    Using: (xs) -> sum(concat(xs, list(100)))
Reveal: stdout
`,
			input: "1,2,9,3",
		},
		// --- Chunk ---
		{
			name: "chunk keeps the short final block",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Chunk 2
Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`,
			input: "a,b,c,d,e",
		},
		{
			name: "chunk larger than the list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Chunk 9
Reveal: stdout
`,
			input: "1,2",
		},
		{
			name: "chunk then sum each group",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Chunk 3
Maximum Technique: Sum Each Group
Reveal: stdout
`,
			input: "1,2,3,4,5,6,7",
		},
		// --- Partition ---
		{
			name: "partition halves are both reachable",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Partition
    Using: (x) -> x > 2
Reveal: stdout
Cursed Technique: Take Item 0
Maximum Technique: Sum
Reveal: stdout
`,
			input: "1,5,2,4,3",
		},
		{
			name: "partition of an all-matching list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Partition
    Using: (x) -> x > 0
Reveal: stdout
`,
			input: "1,2,3",
		},
		// --- Iterate / Unfold ---
		{
			name: "iterate keeps the trajectory",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Take Item 0
Cursed Technique: Iterate 5
    Using: (x) -> x * 2
Reveal: stdout
Maximum Technique: Sum
Reveal: stdout
`,
			input: "1",
		},
		{
			name: "iterate zero steps",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Take Item 0
Cursed Technique: Iterate 0
    Using: (x) -> x * 2
Reveal: stdout
`,
			input: "7",
		},
		{
			name: "iterate over a list state",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Iterate 2
    Using: (xs) -> concat(xs, list(sum(xs)))
Reveal: stdout
`,
			input: "1,2",
		},
		{
			name: "unfold halves to one",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Take Item 0
Cursed Technique: Unfold
    While: (x) -> x > 1
    Using: (x) -> x / 2
Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`,
			input: "20",
		},
		{
			name: "unfold whose predicate starts false",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Take Item 0
Cursed Technique: Unfold
    While: (x) -> x > 100
    Using: (x) -> x / 2
Reveal: stdout
`,
			input: "5",
		},
		// --- Any / All / Find / Find Index ---
		{
			name: "all short-circuits before a failing element",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: All
    Using: (x) -> 10 / x > 100
Reveal: stdout
`,
			// The 0 would fail the predicate; All stops at the 5, which is
			// already false, so neither backend ever divides by it.
			input: "5,0",
		},
		{
			name: "any true and all false",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Any
    Using: (x) -> x > 4
Reveal: stdout
`,
			input: "1,2,5",
		},
		{
			name: "all over an emptied list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Using: (x) -> x > 100
Maximum Technique: All
    Using: (x) -> x > 0
Reveal: stdout
`,
			input: "1,2",
		},
		{
			name: "any over an emptied list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Using: (x) -> x > 100
Maximum Technique: Any
    Using: (x) -> x > 0
Reveal: stdout
`,
			input: "1,2",
		},
		{
			name: "find and find index",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Find Index
    Using: (x) -> x > 3
Reveal: stdout
`,
			input: "1,5,2,7",
		},
		{
			name: "find index with no match is -1",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Find Index
    Using: (x) -> x > 100
Reveal: stdout
`,
			input: "1,2,3",
		},
		{
			name: "find returns the element",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{w:word} {n:int}"
Maximum Technique: Find
    Using: (r) -> r.n > 3
Cursed Technique: Apply
    Using: (r) -> r.w
Reveal: stdout
`,
			input: "low 1\nhigh 9\nmid 5",
		},
		// --- Sum By / Product By, and the Map Each fusion into them ---
		{
			name: "sum by and product by",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Sum By
    Using: (x) -> x * x
Reveal: stdout
`,
			input: "1,2,3",
		},
		{
			name: "product by",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Product By
    Using: (x) -> x + 1
Reveal: stdout
`,
			input: "1,2,3",
		},
		{
			name: "map each then sum fuses into sum by",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> x * 3 + 1
Maximum Technique: Sum
Reveal: stdout
`,
			input: "4,8,15,16,23,42",
		},
		{
			name: "map each then product fuses into product by",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> x + 2
Maximum Technique: Product
Reveal: stdout
`,
			input: "1,2,3",
		},
		{
			name: "filter then take item 0 fuses into a first match",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Using: (x) -> x > 2
Cursed Technique: Take Item 0
Reveal: stdout
`,
			input: "1,5,2,7",
		},
		// --- Sliding Reduce (all four modes, and the Sum fusion) ---
		{
			name: "sliding reduce sum",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Domain Expansion: Sliding Reduce 3
Reveal: stdout
`,
			input: "1,2,3,4,5",
		},
		{
			name: "sliding reduce max with a step",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Domain Expansion: Sliding Reduce 2 2
    Mode: Max
Reveal: stdout
`,
			input: "5,1,4,4,7,-2",
		},
		{
			name: "sliding reduce min",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Domain Expansion: Sliding Reduce 3
    Mode: Min
Reveal: stdout
`,
			input: "5,1,9,3,7,2,8",
		},
		{
			name: "sliding reduce product",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Domain Expansion: Sliding Reduce 3
    Mode: Product
Reveal: stdout
`,
			input: "1,2,3,4,0,5",
		},
		{
			name: "sliding reduce then sum",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Domain Expansion: Sliding Reduce 3
    Mode: Product
Maximum Technique: Sum
Reveal: stdout
`,
			input: "1,2,3,4,5",
		},
		{
			name: "sliding reduce over too short a list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Domain Expansion: Sliding Reduce 9
Reveal: stdout
`,
			input: "1,2",
		},
		// --- Zip With, and the Zip + Map Each fusion ---
		{
			name: "zip with combines two channels",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ";"

Channel "a":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by ","
    Channeled Energy: Convert List to Integers

Channel "b":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by ","
    Channeled Energy: Convert List to Integers

Maximum Technique: Zip
    From: a, b
    Using: (x, y) -> x * y
Reveal: stdout
Maximum Technique: Sum
Reveal: stdout
`,
			input: "1,2,3;4,5,6",
		},
		{
			name: "zip with truncates to the shorter channel",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ";"

Channel "a":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by ","
    Channeled Energy: Convert List to Integers

Channel "b":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by ","
    Channeled Energy: Convert List to Integers

Maximum Technique: Zip
    From: a, b
    Using: (x, y) -> x + y
Reveal: stdout
`,
			input: "1,2,3;10,20",
		},
		// --- a composed pipeline touching several at once ---
		{
			name: "chunk, sum by, partition and find compose",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Chunk 2
Cursed Technique: Map Each
    Using: (g) -> sum(g)
Reveal: stdout
Cursed Technique: Partition
    Using: (x) -> x > 5
Cursed Technique: Take Item 0
Reveal: stdout
Maximum Technique: Find Index
    Using: (x) -> x > 8
Reveal: stdout
`,
			input: "1,2,3,4,5,6,7",
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
