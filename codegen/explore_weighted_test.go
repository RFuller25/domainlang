package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Interpreter/binary byte parity for Explore's weighted and folding modes.
//
// Mode: Costs is the one that pins more than the answer: it renders a Map in
// *settle* order, so the generated heap has to break equal priorities the way
// ir.PQ does or the two backends print different text for the same costs.
func TestCompiledWeightedExploreMatchesInterpreter(t *testing.T) {
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
			name: "cheapest over a node cost",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Cheapest
    Cost: (n) -> n % 7
    Until: (n) -> n = 30
    Using: (n) -> if n < 30 then list(n + 1, n + 3, n + 7) else take(list(n), 0)
Reveal: stdout
`,
			input: "0",
		},
		{
			name: "cheapest over an edge cost",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Cheapest
    Cost: (a, b) -> (b - a) * (b - a)
    Until: (n) -> n = 24
    Using: (n) -> if n < 24 then list(n + 1, n + 2, n + 5) else take(list(n), 0)
Reveal: stdout
`,
			input: "0",
		},
		{
			// Equal costs everywhere, so the whole answer is settle order.
			name: "costs map renders in settle order",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Costs
    Cost: (n) -> 1
    Using: (n) -> if n < 12 then list(n + 1, n + 2, n + 3) else take(list(n), 0)
Reveal: stdout
`,
			input: "0",
		},
		{
			name: "costs map with competing routes",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Costs
    Cost: (n) -> n % 5
    Using: (n) -> if n < 15 then list(n + 1, n + 2) else take(list(n), 0)
Reveal: stdout
`,
			input: "0",
		},
		{
			name: "unreachable cheapest is the -1 sentinel",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Cheapest
    Cost: (n) -> 1
    Until: (n) -> n = 999
    Using: (n) -> if n < 5 then list(n + 1) else take(list(n), 0)
Reveal: stdout
`,
			input: "0",
		},
		{
			// A grid Dijkstra written as a state search, beside the primitive
			// that answers the same question — three implementations of one
			// ordering agreeing at once.
			name: "cheapest over a grid beside grid Dijkstra",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert To Integers
Channeled Energy: Convert To Grid

Part "builtin":
    Domain Expansion: Dijkstra from 0 0
    Cursed Technique: Apply
        Using: (g) -> at(g, 3, 3)
    Reveal: stdout

Part "explore":
    Cursed Technique: Apply
        Consider g Of Apply
            Using: (x) -> x
        Cursed Technique: Apply
            Using: (x) -> point(0, 0)
        Domain Expansion: Explore
            Mode: Cheapest
            Until: (p) -> p = point(3, 3)
            Cost: (p) -> at(g, prow(p), pcol(p))
            Using: (p) -> neighbors4(g, prow(p), pcol(p))
    Reveal: stdout
`,
			input: "1991\n1991\n1111\n9991",
		},
		{
			// Tally over a lattice with heavy sharing: 61 states, trillions of
			// paths. If either backend lost the memo this would not finish.
			name: "tally counts paths over a shared lattice",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Tally
    Value: (n) -> 1
    Combine: (a, b) -> a + b
    Using: (n) -> if n < 60 then list(n + 1, n + 2) else take(list(n), 0)
Reveal: stdout
`,
			input: "0",
		},
		{
			// Until: marks a leaf, so this is "count the paths that reach the
			// goal" — AoC 2020 Day 10 Part 2 as a DAG fold.
			name: "tally with Until marking the leaf",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Consider adapters Of Convert To Set
    Consider top Of Max
    Cursed Technique: Apply
        Using: (xs) -> 0
    Domain Expansion: Explore
        Mode: Tally
        Until: (j) -> j = top
        Value: (j) -> 1
        Combine: (a, b) -> a + b
        Cursed Technique: Apply
            Using: (j) -> list(j + 1, j + 2, j + 3)
        Cursed Technique: Filter
            Using: (n) -> contains(adapters, n)
Reveal: stdout
`,
			input: "16\n10\n15\n5\n1\n11\n7\n19\n6\n12\n4",
		},
		{
			// Value: is not restricted to Int, and Combine: folds whatever it
			// produced — so the fold's type flows through both backends.
			name: "tally folding Text",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Domain Expansion: Explore
    Mode: Tally
    Value: (n) -> totext(n)
    Combine: (a, b) -> a + "|" + b
    Using: (n) -> if n < 4 then list(n + 1, n + 3) else take(list(n), 0)
Reveal: stdout
`,
			input: "0",
		},
		{
			// Tuple states, which compile to generated structs — the map keys
			// and the heap both have to carry them.
			name: "cheapest over tuple states",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> point(0, 0)
Domain Expansion: Explore
    Mode: Cheapest
    Cost: (p) -> prow(p) + pcol(p)
    Until: (p) -> p = point(4, 4)
    Using: (p) -> if prow(p) < 4 and pcol(p) < 4
        then list(point(prow(p) + 1, pcol(p)), point(prow(p), pcol(p) + 1))
        else take(list(p), 0)
Reveal: stdout
`,
			input: "ignored",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
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
