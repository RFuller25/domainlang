package codegen_test

import (
	"testing"

	"domain/codegen"
)

// The Map operations and the expression-layer escape hatches, pinned across
// backends. dmMap is insertion-ordered, so ordering is the thing most likely
// to drift between the two implementations.
func TestCompiledMapOpsMatchInterpreter(t *testing.T) {
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
			name: "map values and filter entries",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count By
    Using: (s) -> charat(s, 0)
Cursed Technique: Map Values
    Using: (n) -> n * 10
Cursed Technique: Filter Entries
    Using: (k, n) -> n > 5 or k = "z"
Reveal: stdout
`,
			input: "bee\napple\nant\ncow\nzebra",
		},
		{
			name: "entries round trip preserves order",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count By
    Using: (s) -> charat(s, 0)
Channeled Energy: Convert To Entries
Channeled Energy: Convert To Map
Reveal: stdout
`,
			input: "bee\napple\nant\ncow",
		},
		{
			// The idiom the whole group exists for.
			name: "most common element",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count By
    Using: (s) -> charat(s, 0)
Channeled Energy: Convert To Entries
Domain Expansion: Sort By, Descending
    Using: (e) -> item(e, 1)
Cursed Technique: Map Each
    Using: (e) -> item(e, 0) + "=" + totext(item(e, 1))
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "bee\napple\nant\ncow\navocado\nbat",
		},
		{
			name: "map escape hatches in a lambda",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count By
    Using: (s) -> charat(s, 0)
Cursed Technique: Apply
    Using: (m) -> textjoin(list(totext(size(m)), totext(getor(m, "a", 0)), totext(getor(m, "zz", 0 - 1)), (if haskey(m, "b") then "Y" else "N"), textjoin(keys(m), "+"), totext(sum(values(m)))), "|")
Reveal: stdout
`,
			input: "apple\nant\nbee\ncow",
		},
		{
			name: "point builtins",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> totext(prow(psub(point(n, n), point(1, 1))) + pcol(pscale(point(n, 2), 3)) + chebyshev(point(n, 0), point(0, n)) + length(dirs8()) + length(around4(point(n, n))) + prow(item(around8(point(n, n)), 0)))
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "-3\n0\n5",
		},
		{
			name: "set to list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Set
Cursed Technique: Apply
    Using: (s) -> textjoin(tolist(s), ",") + "/" + totext(size(s))
Reveal: stdout
`,
			input: "b\na\nb\nc",
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
