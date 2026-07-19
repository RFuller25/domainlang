package codegen_test

import (
	"testing"

	"domain/codegen"
)

// TestCompiledSeqMatchesInterpreter pins interpreter/binary parity for the
// section-D remainder: Window, Flatten, Enumerate, Count By, Min/Max By,
// Sort By, standalone Difference, Zip, and the bit-op builtins.
func TestCompiledSeqMatchesInterpreter(t *testing.T) {
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
			name: "window increases (2021 D1 shape)",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Window 2
Maximum Technique: Count Matching
    Using: (w) -> last(w) > first(w)
Reveal: stdout
`,
			input: "199\n200\n208\n210\n200\n207\n240\n269\n260\n263",
		},
		{
			name: "window with step renders",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Window 2 2
Reveal: stdout
`,
			input: "a,b,c,d,e",
		},
		{
			name: "flatten blocks",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Cursed Technique: Flatten
Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`,
			input: "a\nb\n\nc",
		},
		{
			name: "enumerate feeds point accessors",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Enumerate
Reveal: stdout
Cursed Technique: Map Each
    Using: (p) -> prow(p) * 100 + pcol(p)
Maximum Technique: Sum
Reveal: stdout
`,
			input: "7,8,9",
		},
		{
			name: "count by renders a frequency map",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Count By
    Using: (n) -> n / 10
Reveal: stdout
`,
			input: "1,12,15,23,9",
		},
		{
			name: "min by and max by",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{w:word} {n:int}"
Maximum Technique: Max By
    Using: (r) -> r.n
Cursed Technique: Apply
    Using: (r) -> r.w
Reveal: stdout
`,
			input: "low 1\nhigh 9\nmid 5",
		},
		{
			name: "sort by stable and descending",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Domain Expansion: Sort By
    Using: (s) -> occurrences(s, "a")
Reveal: stdout
Domain Expansion: Sort By, Descending
    Using: (s) -> length(list(s))
Reveal: stdout
`,
			input: "bb,aa,ba,ab,cc",
		},
		{
			name: "standalone difference",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Maximum Technique: Difference
Reveal: stdout
`,
			input: "abcd\nbd\nc",
		},
		{
			name: "zip channels dot product",
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
Reveal: stdout
Cursed Technique: Map Each
    Using: (p) -> prow(p) * pcol(p)
Maximum Technique: Sum
Reveal: stdout
`,
			input: "1,2,3;4,5",
		},
		{
			name: "bit builtins and frombin",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (s) -> band(frombin(s), 12) + bor(frombin(s), 1) * 100 + bxor(frombin(s), 5) + shl(frombin(s), 2) + shr(frombin(s), 1)
Maximum Technique: Sum
Reveal: stdout
`,
			input: "110\n11\n10110",
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
