package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Grid geometry, Find Cycle and the general Holds vow, pinned across backends.
// The grid transforms are index arithmetic written twice — once in the
// interpreter, once in generated Go — so a transposed sign is exactly the kind
// of thing only a parity test catches.
func TestCompiledGridGeometryMatchesInterpreter(t *testing.T) {
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
			name: "rotate right, left and half",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Part "right":
    Cursed Technique: Rotate Grid
        Mode: Right
    Reveal: stdout
Part "left":
    Cursed Technique: Rotate Grid
        Mode: Left
    Reveal: stdout
Part "half":
    Cursed Technique: Rotate Grid
        Mode: Half
    Reveal: stdout
`,
			input: "abc\ndef",
		},
		{
			name: "flip horizontal and vertical",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Part "h":
    Cursed Technique: Flip Grid
        Mode: Horizontal
    Reveal: stdout
Part "v":
    Cursed Technique: Flip Grid
        Mode: Vertical
    Reveal: stdout
`,
			input: "abc\ndef",
		},
		{
			// Four right turns is the identity — a sign error in the index
			// arithmetic would not survive this.
			name: "four rotations are the identity",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Simple Domain: Repeat 4
    Cursed Technique: Rotate Grid
        Mode: Right
Reveal: stdout
`,
			input: "abcd\nefgh\nijkl",
		},
		{
			name: "convert to rows round trip",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Rotate Grid
    Mode: Left
Channeled Energy: Convert To Rows
Reveal: stdout
`,
			input: "abc\ndef",
		},
		{
			name: "find cycle over a trajectory",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Cursed Technique: Iterate 12
    Using: (n) -> mod(n * 3 + 1, 7)
Maximum Technique: Find Cycle
Reveal: stdout
`,
			input: "3",
		},
		{
			name: "find cycle with no repeat",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Cursed Technique: Iterate 5
    Using: (n) -> n + 1
Maximum Technique: Find Cycle
Reveal: stdout
`,
			input: "0",
		},
		{
			// The general vow reaches a Grid, which neither literal vow shape
			// could express.
			name: "holds vow over a grid",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Binding Vow: Holds
    Using: (g) -> rows(g) = 2 and cols(g) = 3
Cursed Technique: Transpose
Reveal: stdout
`,
			input: "abc\ndef",
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
