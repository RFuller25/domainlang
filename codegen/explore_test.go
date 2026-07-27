package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Explore compiles, so the search that replaces recursion is available in a
// built binary too. Parity matters especially here: the visited set and the
// BFS queue are reimplemented in generated Go rather than shared with the
// interpreter, so the two could drift.
func TestCompiledExploreMatchesInterpreter(t *testing.T) {
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
			name: "collect in BFS order",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Using: (n) -> if n > 8 then list(n) else list(n * 2, n + 3)
Cursed Technique: Map Each
    Using: (n) -> totext(n)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "1",
		},
		{
			// Cyclic: terminates only because the visited set does its job.
			name: "count over a cyclic space",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Count
    Using: (n) -> list(mod(n + 1, 5), mod(n + 2, 5))
Reveal: stdout
`,
			input: "0",
		},
		{
			name: "distances are shortest",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Distances
    Using: (n) -> if n > 6 then list(n) else list(n + 1, n + 2)
Reveal: stdout
`,
			input: "0",
		},
		{
			name: "steps to the first match",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Steps
    Until: (n) -> n = 27
    Using: (n) -> if n > 40 then list(n) else list(n * 2, n + 3)
Reveal: stdout
`,
			input: "3",
		},
		{
			name: "steps when the seed already matches",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Steps
    Until: (n) -> n > 0
    Using: (n) -> list(n + 1)
Reveal: stdout
`,
			input: "5",
		},
		{
			name: "steps when unreachable",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Steps
    Until: (n) -> n = 7
    Using: (n) -> list(mod(n + 2, 6))
Reveal: stdout
`,
			input: "0",
		},
		{
			// Compound state: a tuple keys the visited set structurally, and
			// compiles to a comparable Go struct.
			name: "explore over tuple states",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> point(0, 0)
Domain Expansion: Explore
    Mode: Count
    Using: (p) -> if prow(p) + pcol(p) > 2 then list(p) else list(point(prow(p) + 1, pcol(p)), point(prow(p), pcol(p) + 1))
Reveal: stdout
`,
			input: "x",
		},
		{
			name: "explore with Until pruning in collect mode",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Until: (n) -> n > 12
    Using: (n) -> list(n * 2, n + 3)
Maximum Technique: Count
Reveal: stdout
`,
			input: "3",
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
