package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
)

// listParity runs each program through the optimized interpreter, the naive
// interpreter, the optimized binary and the naive binary, and requires all four
// to print the same bytes.
func listParity(t *testing.T, head string, cases []struct{ name, body string }, inputs []string) {
	t.Helper()
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	for _, tc := range cases {
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
				if got := buildAndRun(t, compilePipeline(t, src, false), []byte(input), codegen.Options{}); got != want {
					t.Errorf("interpreter %q, unoptimized binary %q", want, got)
				}
			})
		}
	}
}

var listCollInputs = []string{"", "7", "0 1 2", "5 1 9 5 1 7 9 2 2 8 4 1", "12 3 1 23 12 3 1 23"}

// Sets keyed by a List: the compiled dmLSet against the interpreter's Set.
func TestCompiledListSetsMatchInterpreter(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct{ name, body string }{
		{"convert to set of chunks", "Cursed Technique: Chunk 2\nChanneled Energy: Convert To Set\n"},
		{"set size", "Cursed Technique: Chunk 2\nChanneled Energy: Convert To Set\nCursed Technique: Apply\n    Using: (s) -> size(s)\n"},
		{"toset builtin and tolist", "Cursed Technique: Chunk 2\nCursed Technique: Apply\n    Using: (c) -> tolist(toset(c))\n"},
		{"contains hit and miss", "Cursed Technique: Chunk 2\nChanneled Energy: Convert To Set\nCursed Technique: Apply\n    Using: (s) -> list(contains(s, list(1, 2)), contains(s, list(5, 1)), contains(s, emptylist(0)))\n"},
		{"emptyset insert contains", "Cursed Technique: Apply\n    Using: (xs) -> contains(insert(insert(emptyset(list(0)), xs), list(1)), xs)\n"},
		{"insert duplicate keeps order", "Cursed Technique: Apply\n    Using: (xs) -> insert(insert(insert(emptyset(list(0)), list(2, 1)), list(1, 2)), list(2, 1))\n"},
		{"del", "Cursed Technique: Chunk 2\nChanneled Energy: Convert To Set\nCursed Technique: Apply\n    Using: (s) -> list(del(s, list(1, 2)), del(s, list(99)))\n"},
		{"union intersect difference", `Cursed Technique: Chunk 2
Cursed Technique: Apply
    Using: (cs) -> list(union(toset(take(cs, 2)), toset(drop(cs, 1))), intersect(toset(take(cs, 2)), toset(drop(cs, 1))), difference(toset(take(cs, 2)), toset(drop(cs, 1))))
`},
		{"set equality", "Cursed Technique: Chunk 2\nCursed Technique: Apply\n    Using: (cs) -> toset(cs) = toset(reverse(cs))\n"},
		{"keys that print alike", "Cursed Technique: Apply\n    Using: (xs) -> toset(list(list(1, 23), list(12, 3), list(1, 23), list(123)))\n"},
		{"text lists with separators", "Cursed Technique: Apply\n    Using: (xs) -> toset(list(list(\"a,b\"), list(\"a\", \"b\"), list(\"a,b\")))\n"},
		{"nested lists", "Cursed Technique: Map Each\n    Using: (x) -> list(list(x % 2), list(x % 3, x % 2))\nChanneled Energy: Convert To Set\n"},
		{"tuples and records holding lists", "Cursed Technique: Map Each\n    Using: (x) -> tuple(x % 2, list(x % 3))\nChanneled Energy: Convert To Set\n"},
		{"records holding lists", "Cursed Technique: Map Each\n    Using: (x) -> {a: list(x % 3), b: x % 2}\nChanneled Energy: Convert To Set\n"},
		{"map elements", "Cursed Technique: Chunk 2\nChanneled Energy: Convert To Set\nCursed Technique: Map Elements\n    Using: (c) -> take(c, 1)\n"},
		{"fold inserting list keys", "Cursed Technique: Chunk 1\nMaximum Technique: Fold\n    Seed: (xs) -> emptyset(list(0))\n    Using: (acc, c) -> insert(acc, c)\n"},
		{"fold inserting list keys, counted", "Cursed Technique: Map Each\n    Using: (x) -> list(x % 5, x % 3)\nMaximum Technique: Fold\n    Seed: (xs) -> emptyset(list(0))\n    Using: (acc, c) -> insert(acc, c)\nCursed Technique: Apply\n    Using: (s) -> size(s)\n"},
		{"intersect over groups of lists", "Cursed Technique: Chunk 4\nCursed Technique: Map Each\n    Using: (c) -> list(take(c, 2), drop(c, 2), list(1, 2))\nMaximum Technique: Intersect\n"},
		{"union over groups of lists", "Cursed Technique: Chunk 4\nCursed Technique: Map Each\n    Using: (c) -> list(take(c, 2), drop(c, 2), list(1, 2))\nMaximum Technique: Union\n"},
		{"difference over groups of lists", "Cursed Technique: Chunk 4\nCursed Technique: Map Each\n    Using: (c) -> list(take(c, 2), drop(c, 2), list(1, 2))\nMaximum Technique: Difference\n"},
		{"difference of channels of lists", `Channel "one":
    Cursed Technique: Chunk 2

Channel "two":
    Cursed Technique: Map Each
        Using: (x) -> x + x % 2
    Cursed Technique: Chunk 2

Maximum Technique: Difference
    From: one, two
`},
		{"for each over a set of lists", "Cursed Technique: Chunk 2\nChanneled Energy: Convert To Set\nCursed Technique: Map Each\n    Using: (c) -> sum(c)\n"},
	}
	listParity(t, head, cases, listCollInputs)
}

// Maps keyed by a List: the compiled dmLMap against the interpreter's Map.
func TestCompiledListMapsMatchInterpreter(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct{ name, body string }{
		{"count by a list key", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3, x % 2)\n"},
		{"group by a list key", "Maximum Technique: Group By\n    Using: (x) -> list(x % 3, x % 2)\n"},
		{"count by keys that print alike", "Maximum Technique: Count By\n    Using: (x) -> if x % 2 = 0 then list(1, 23) else list(12, 3)\n"},
		{"group by a text list (anagrams)", "Cursed Technique: Map Each\n    Using: (x) -> totext(x * 7)\nMaximum Technique: Group By\n    Using: (w) -> sort(split(w, \"\"))\n"},
		{"count by nested list", "Maximum Technique: Count By\n    Using: (x) -> list(list(x % 2), list(x % 3, x % 2))\n"},
		{"count by a tuple holding a list", "Maximum Technique: Count By\n    Using: (x) -> tuple(x % 2, list(x % 3))\n"},
		{"count by a record holding a list", "Maximum Technique: Count By\n    Using: (x) -> {a: list(x % 3), b: x % 2}\n"},
		{"group reduce (count)", "Maximum Technique: Group By\n    Using: (x) -> list(x % 3)\nCursed Technique: Map Values\n    Using: (g) -> length(g)\n"},
		{"group reduce (sum)", "Maximum Technique: Group By\n    Using: (x) -> list(x % 3)\nCursed Technique: Map Values\n    Using: (g) -> sum(g)\n"},
		{"convert to map and entries", "Cursed Technique: Map Each\n    Using: (x) -> tuple(list(x % 3, x % 2), x)\nChanneled Energy: Convert To Map\nChanneled Energy: Convert To Entries\n"},
		{"convert to map, last write wins", "Cursed Technique: Map Each\n    Using: (x) -> tuple(list(x % 3), x)\nChanneled Energy: Convert To Map\n"},
		{"map values", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3)\nCursed Technique: Map Values\n    Using: (n) -> n * 10\n"},
		{"filter entries", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3)\nCursed Technique: Filter Entries\n    Using: (k, n) -> n > 1\n"},
		{"map keys merges collisions", "Maximum Technique: Count By\n    Using: (x) -> list(x % 4, x % 2)\nCursed Technique: Map Keys\n    Using: (k) -> take(k, 1)\n"},
		{"map keys into lists", "Maximum Technique: Count By\n    Using: (x) -> x % 4\nCursed Technique: Map Keys\n    Using: (k) -> list(k % 2)\n"},
		{"get getor haskey", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3)\nCursed Technique: Apply\n    Using: (m) -> tuple(getor(m, list(0), -1), getor(m, list(9), -1), haskey(m, list(1)), haskey(m, list(7)), size(m))\n"},
		{"get on a present key", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3)\nCursed Technique: Apply\n    Using: (m) -> if haskey(m, list(0)) then get(m, list(0)) else 0\n"},
		{"keys values entries", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3, 1)\nCursed Technique: Apply\n    Using: (m) -> tuple(keys(m), values(m), entries(m))\n"},
		{"pluck", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3)\nCursed Technique: Apply\n    Using: (m) -> if size(m) > 1 then pluck(m, list(list(1), list(0))) else emptylist(0)\n"},
		{"emptymap insert del", "Cursed Technique: Apply\n    Using: (xs) -> del(insert(insert(insert(emptymap(list(0), \"\"), list(1, 2), \"a\"), list(12), \"b\"), list(1, 2), \"c\"), list(12))\n"},
		{"del absent key", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3)\nCursed Technique: Apply\n    Using: (m) -> del(m, list(99))\n"},
		{"tomap builtin", "Cursed Technique: Chunk 2\nCursed Technique: Map Each\n    Using: (c) -> tuple(c, sum(c))\nCursed Technique: Apply\n    Using: (ps) -> tomap(ps)\n"},
		{"map equality", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3)\nCursed Technique: Apply\n    Using: (m) -> m = insert(m, list(0), 0)\n"},
		{"fold inserting list keys", "Maximum Technique: Fold\n    Seed: (xs) -> emptymap(list(0), 0)\n    Using: (acc, x) -> insert(acc, list(x % 50, x % 7), x)\nCursed Technique: Apply\n    Using: (m) -> size(m)\n"},
		{"set of keys", "Maximum Technique: Count By\n    Using: (x) -> list(x % 3)\nCursed Technique: Apply\n    Using: (m) -> toset(keys(m))\n"},
	}
	listParity(t, head, cases, listCollInputs)
}

// contains and indexof over a List of lists, and the optimizer's hoist of an
// invariant list into a set probe.
func TestCompiledListMembershipMatchesInterpreter(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\nCursed Technique: Chunk 2\n"
	cases := []struct{ name, body string }{
		{"contains over a list of lists", "Cursed Technique: Apply\n    Using: (cs) -> tuple(contains(cs, list(1, 2)), contains(cs, list(5, 1)), contains(cs, emptylist(0)))\n"},
		{"indexof over a list of lists", "Cursed Technique: Apply\n    Using: (cs) -> tuple(indexof(cs, list(1, 2)), indexof(cs, list(9, 9)))\n"},
		{"filter with an invariant list literal", "Cursed Technique: Filter\n    Using: (c) -> contains(list(list(1, 2), list(3, 4), list(5, 1)), c)\n"},
		{"hoisted list membership", "Cursed Technique: Filter\n    Consider wanted Of (cs) -> take(cs, 2)\n    Using: (c) -> contains(wanted, c)\n"},
		{"cheaper builtins over lists of lists", "Cursed Technique: Apply\n    Using: (cs) -> tuple(contains(keys(tomap(zip(cs, cs))), list(1, 2)), contains(tolist(toset(cs)), list(3, 4)), length(tolist(toset(cs))))\n"},
		{"filter by indexof", "Cursed Technique: Filter\n    Using: (c) -> indexof(list(list(1, 2), list(3, 4)), c) >= 0\n"},
	}
	listParity(t, head, cases, listCollInputs)
}
