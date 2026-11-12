package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
)

// A List of keyable elements is a hash key where the key stays inside the
// primitive: Find Cycle, Unique (and its Count fusion) and the `unique`
// builtin. The compiled side hashes it through an injective byte encoding
// (codegen/keyfn.go); these hold the binary to the interpreter, optimized and
// naive, including the shapes that make the encoding's injectivity matter
// (lists that print alike, nested lists, Text with separators in it).
func TestCompiledListKeysMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct{ name, body string }{
		{"find cycle on a trajectory of lists", `Cursed Technique: Iterate 30
    Using: (xs) -> concat(drop(xs, 1), list((item(xs, 0) + 1) % 3))
Maximum Technique: Find Cycle
`},
		{"find cycle on lists of lists", `Cursed Technique: Chunk 2
Cursed Technique: Iterate 20
    Using: (g) -> concat(drop(g, 1), list(list(item(item(g, 0), 1), (item(item(g, 0), 0) + 1) % 3)))
Maximum Technique: Find Cycle
`},
		{"find cycle with no repeat", `Cursed Technique: Chunk 1
Maximum Technique: Find Cycle
`},
		{"unique lists", `Cursed Technique: Chunk 2
Cursed Technique: Unique
`},
		{"unique lists then count", `Cursed Technique: Chunk 2
Cursed Technique: Unique
Maximum Technique: Count
`},
		{"unique of lists of lists", `Cursed Technique: Map Each
    Using: (x) -> list(list(x % 2), list(x % 3, x % 2))
Cursed Technique: Unique
`},
		{"unique tuples holding lists", `Cursed Technique: Map Each
    Using: (x) -> tuple(totext(x % 2), list(x % 3, 1), x % 2)
Cursed Technique: Unique
`},
		{"unique records holding lists", `Cursed Technique: Map Each
    Using: (x) -> {a: list(x % 3), b: x % 2}
Cursed Technique: Unique
`},
		{"unique of text lists", `Cursed Technique: Map Each
    Using: (x) -> list(textjoin(list("a", totext(x % 2)), ","), "b")
Cursed Technique: Unique
`},
		{"the unique builtin on lists", `Cursed Technique: Chunk 2
Cursed Technique: Apply
    Using: (cs) -> unique(cs)
`},
		{"empty and singleton lists are distinct keys", `Cursed Technique: Map Each
    Using: (x) -> take(list(x), x % 2)
Cursed Technique: Unique
`},
		{"nested shapes that print alike stay apart", `Cursed Technique: Map Each
    Using: (x) -> if x % 2 = 0 then list(list(1, 2), list(3)) else list(list(1), list(2, 3))
Cursed Technique: Unique
`},
	}
	for _, tc := range cases {
		inputs := []string{"0 1 2", "5 1 9 5 1 7 9 2 2 8 4 1", "7", ""}
		if strings.HasPrefix(tc.name, "find cycle on") && tc.name != "find cycle with no repeat" {
			// The step reads item 0 (and 1 of a pair), so it needs lists that have them.
			inputs = []string{"0 1 2 3", "5 1 9 5 1 7 9 2"}
		}
		for _, input := range inputs {
			name := tc.name + "/" + strings.ReplaceAll(input, " ", "_")
			t.Run(name, func(t *testing.T) {
				src := head + tc.body + "Reveal: stdout\n"
				pipe, err := frontEnd(src, true)
				if err != nil {
					t.Fatalf("front end: %v", err)
				}
				want := runInterpreter(t, pipe, []byte(input))
				naive := runInterpreter(t, compilePipeline(t, src, false), []byte(input))
				if want != naive {
					t.Fatalf("optimized interpreter %q, naive %q", want, naive)
				}
				if got := buildAndRun(t, pipe, []byte(input), codegen.Options{}); got != want {
					t.Errorf("interpreter %q, binary %q", want, got)
				}
				naivePipe := compilePipeline(t, src, false)
				if got := buildAndRun(t, naivePipe, []byte(input), codegen.Options{}); got != want {
					t.Errorf("interpreter %q, unoptimized binary %q", want, got)
				}
			})
		}
	}
}

// Everything a Map key or Set element would have to hand back out of a
// collection stays refused — in the typechecker, so the two backends cannot
// disagree about it. Float lists are never keyable.
func TestListKeysRefusedWhereTheyCannotBeHandedBack(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\nCursed Technique: Chunk 2\n"
	cases := []struct{ name, body string }{
		{"group by a float list", "Maximum Technique: Group By\n    Using: (c) -> list(tofloat(1))\n"},
		{"count by a float list", "Maximum Technique: Count By\n    Using: (c) -> list(tofloat(1))\n"},
		{"emptymap keyed by a float list", "Cursed Technique: Apply\n    Using: (c) -> size(emptymap(list(tofloat(1)), 0))\n"},
		{"explore over a float list state", "Cursed Technique: Map Each\n    Using: (c) -> tofloat(1)\nDomain Expansion: Explore\n    Using: (s) -> list(s)\n"},
		{"graph over float list nodes", "Cursed Technique: Apply\n    Using: (c) -> size(graph(list(tuple(list(tofloat(1)), list(tofloat(2))))))\n"},
		{"emptygraph over a float list", "Cursed Technique: Apply\n    Using: (c) -> size(emptygraph(list(tofloat(1))))\n"},
		{"convert to graph over float lists", "Cursed Technique: Map Each\n    Using: (c) -> tuple(list(tofloat(1)), list(tofloat(2)))\nChanneled Energy: Convert To Graph\n"},
		{"map nodes to float lists", "Cursed Technique: Map Each\n    Using: (c) -> tuple(first(c), last(c))\nChanneled Energy: Convert To Graph\nCursed Technique: Map Nodes\n    Using: (n) -> list(tofloat(n))\n"},
		{"float lists", "Cursed Technique: Map Each\n    Using: (c) -> list(tofloat(1))\nCursed Technique: Unique\n"},
		{"float lists in find cycle", "Cursed Technique: Map Each\n    Using: (c) -> list(tofloat(1))\nMaximum Technique: Find Cycle\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := frontEnd(head+tc.body+"Reveal: stdout\n", false)
			if err == nil || !strings.Contains(err.Error(), "keyable") {
				t.Fatalf("got %v, want a keyable error", err)
			}
		})
	}
}
