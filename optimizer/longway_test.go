package optimizer

import (
	"math/rand"
	"testing"

	"domain/ast"
)

// Differential tests for the rules that came out of reading hand-written
// puzzle solutions (longway.go), the typed totality analysis (walk.go), and
// the length-changing passes reaching inside `Using:` pipeline bodies. Same
// oracle as TestPassesMatchNaiveOracle: the naive pipeline, over random input.

func longWayCases() []diffCase {
	p := func(body string) string { return listHeader + body + "Reveal: stdout\n" }
	return []diffCase{
		// --- typed totality: a tuple projection is total wherever it sits ---
		{name: "fold over tuples, projection inside a conditional",
			src: p("Cursed Technique: Enumerate\nMaximum Technique: Fold\n    Seed: 0\n" +
				"    Using: (acc, p) -> acc + if item(p, 0) > 2 then item(p, 1) else 0\n"),
			explain: "→ Sum By"},
		{name: "fold over tuples, projection behind a consider",
			src: p("Cursed Technique: Enumerate\nMaximum Technique: Fold\n    Seed: 0\n" +
				"    Using: (acc, p) -> acc + (consider v as item(p, 1) in v * 2)\n"),
			explain: "→ Sum By"},
		{name: "fold over lists keeps its item partial",
			src: p("Cursed Technique: Chunk 2\nMaximum Technique: Fold\n    Seed: 0\n" +
				"    Using: (acc, w) -> acc + item(w, 1)\n"),
			explainAbsent: "→ Sum By",
			extraInputs:   []string{"1\n2\n3"}},
		{name: "tuple projection out of range of a nested list stays partial",
			src: p("Cursed Technique: Chunk 2\nCursed Technique: Enumerate\nMaximum Technique: Fold\n    Seed: 0\n" +
				"    Using: (acc, p) -> acc + item(item(p, 1), 1)\n"),
			explainAbsent: "→ Sum By",
			extraInputs:   []string{"1\n2\n3"}},
		{name: "maps over tuples fuse",
			src: p("Cursed Technique: Enumerate\nCursed Technique: Map Each\n    Using: (p) -> item(p, 0) + item(p, 1)\n" +
				"Cursed Technique: Map Each\n    Using: (y) -> y * 2\n"),
			explain: "fused Map Each"},

		// --- Window + the named reduction ---
		{name: "window then Sum Each",
			src:     p("Cursed Technique: Window 3\nMaximum Technique: Sum Each Group\n"),
			explain: "Sliding-Window Sum"},

		// --- Window 2 read by position ---
		{name: "window 2 counted by its ends",
			src:     p("Cursed Technique: Window 2\nMaximum Technique: Count Matching\n    Using: (w) -> first(w) < last(w)\n"),
			explain: "Pairs + Count Matching"},
		{name: "window 2 mapped by item, then summed",
			src: p("Cursed Technique: Window 2\nCursed Technique: Map Each\n" +
				"    Using: (w) -> if item(w, 0) = item(w, 1) then item(w, 0) else 0\nMaximum Technique: Sum\n"),
			explain: "Pairs + Map Each"},
		{name: "window 2 read whole is left alone",
			src:           p("Cursed Technique: Window 2\nCursed Technique: Map Each\n    Using: (w) -> length(w) + first(w)\n"),
			explainAbsent: "Pairs +"},
		{name: "window 2 read past its ends is left alone",
			src:           p("Cursed Technique: Window 2\nCursed Technique: Map Each\n    Using: (w) -> item(w, 2)\n"),
			explainAbsent: "Pairs +"},
		{name: "window 3 is not a pair",
			src:           p("Cursed Technique: Window 3\nMaximum Technique: Count Matching\n    Using: (w) -> first(w) < last(w)\n"),
			explainAbsent: "Pairs +"},
		{name: "window 2 with a step is not Pairs",
			src:           p("Cursed Technique: Window 2 2\nMaximum Technique: Count Matching\n    Using: (w) -> first(w) < last(w)\n"),
			explainAbsent: "Pairs +"},

		{name: "window 2 counted by a lambda that can fail",
			src:         p("Cursed Technique: Window 2\nMaximum Technique: Count Matching\n    Using: (w) -> 10 / (first(w) - last(w)) > 1\n"),
			explain:     "Pairs + Count Matching",
			extraInputs: []string{"1\n2\n2\n5", "4\n4"}},
		{name: "window 2 mapped by a lambda that can fail",
			src:         p("Cursed Technique: Window 2\nCursed Technique: Map Each\n    Using: (w) -> 10 / (item(w, 0) - item(w, 1))\n"),
			explain:     "Pairs + Map Each",
			extraInputs: []string{"1\n2\n2\n5", "4\n4"}},
		{name: "window 2 summed by a lambda that can fail",
			src:         p("Cursed Technique: Window 2\nMaximum Technique: Sum By\n    Using: (w) -> 10 / (first(w) - last(w))\n"),
			explain:     "Pairs + Sum By",
			extraInputs: []string{"1\n2\n2\n5", "4\n4"}},
		{name: "window 2 quantified by a lambda that can fail",
			src:         p("Cursed Technique: Window 2\nMaximum Technique: All\n    Using: (w) -> 10 / (first(w) - last(w)) < 9\n"),
			explain:     "Pairs + All",
			extraInputs: []string{"1\n2\n2\n5", "4\n4"}},

		// --- any window, read by position ---
		{name: "window 3 mapped by its ends",
			src:         p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> if first(w) = last(w) then first(w) else 0\n"),
			explain:     "Window Positions + Map Each",
			extraInputs: []string{"1\n2\n1\n2\n2\n2", "5\n5"}},
		{name: "window 4 step 2 counted by item",
			src:     p("Cursed Technique: Window 4 2\nMaximum Technique: Count Matching\n    Using: (w) -> item(w, 1) < item(w, 3)\n"),
			explain: "Window Positions + Count Matching"},
		{name: "window 3 single position",
			src:     p("Cursed Technique: Window 3\nMaximum Technique: Sum By\n    Using: (w) -> item(w, 2) * 2\n"),
			explain: "Window Positions + Sum By"},
		{name: "window 3 any",
			src:     p("Cursed Technique: Window 3\nMaximum Technique: Any\n    Using: (w) -> first(w) > last(w) + 4\n"),
			explain: "Window Positions + Any"},
		{name: "window 3 all",
			src:     p("Cursed Technique: Window 3\nMaximum Technique: All\n    Using: (w) -> first(w) < last(w) + 4\n"),
			explain: "Window Positions + All"},
		{name: "window 3 lambda that fails reports the same window",
			src:         p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> 10 / (first(w) - last(w))\n"),
			explain:     "Window Positions + Map Each",
			extraInputs: []string{"1\n2\n3\n3\n9\n9", "4\n4\n4"}},
		{name: "window 3 any stops before a later failure",
			src:         p("Cursed Technique: Window 3\nMaximum Technique: Any\n    Using: (w) -> 10 / (first(w) - last(w)) > 3\n"),
			explain:     "Window Positions + Any",
			extraInputs: []string{"1\n2\n3\n3\n9\n9", "4\n4\n4"}},
		{name: "window larger than the list",
			src:         p("Cursed Technique: Window 40\nCursed Technique: Map Each\n    Using: (w) -> first(w) + last(w)\n"),
			explain:     "Window Positions + Map Each",
			extraInputs: []string{"1\n2\n3"}},
		{name: "measured window size",
			src: p("Cursed Technique: Window\n    Size: (xs) -> length(xs) / 4 + 1\n" +
				"Cursed Technique: Map Each\n    Using: (w) -> if first(w) = last(w) then first(w) else 0\n"),
			explain:     "Window Positions + Map Each",
			extraInputs: []string{"1\n1\n2\n2\n3\n3\n4\n4\n1", "1\n2\n3\n4\n5"}},
		{name: "measured window size that fails to be positive",
			src: p("Cursed Technique: Window\n    Size: (xs) -> length(xs) - 3\n" +
				"Cursed Technique: Map Each\n    Using: (w) -> first(w) + item(w, 0)\n"),
			explain:     "Window Positions + Map Each",
			extraInputs: []string{"1\n2\n3", "1\n2\n3\n4\n5"}},
		{name: "measured window step that fails",
			src: p("Cursed Technique: Window\n    Size: (xs) -> 2\n    Step: (xs) -> length(xs) - 4\n" +
				"Cursed Technique: Map Each\n    Using: (w) -> first(w) + last(w)\n"),
			explain:     "Window Positions + Map Each",
			extraInputs: []string{"1\n2\n3\n4\n5\n6", "1\n2\n3"}},
		{name: "measured size with an item past zero is left alone",
			src: p("Cursed Technique: Window\n    Size: (xs) -> length(xs) / 2\n" +
				"Cursed Technique: Map Each\n    Using: (w) -> item(w, 1)\n"),
			explainAbsent: "Window Positions",
			extraInputs:   []string{"1\n2\n3\n4\n5\n6", "1\n2\n3\n4"}},
		{name: "item past a literal size is left alone",
			src:           p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> 10 / (first(w) - last(w)) + item(w, 3)\n"),
			explainAbsent: "Window Positions",
			extraInputs:   []string{"1\n2\n3\n3\n9\n9", "4\n4\n4"}},
		{name: "window read whole is left alone",
			src:           p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> sum(w) + first(w)\n"),
			explainAbsent: "Window Positions"},
		{name: "window item with a computed index is left alone",
			src:           p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> item(w, first(w) % 3)\n"),
			explainAbsent: "Window Positions"},
		{name: "window never read is left alone",
			src:           p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> 1\n"),
			explainAbsent: "Window Positions"},

		// --- duplicates ---
		{name: "sort, window, any neighbours equal",
			src: p("Domain Expansion: Quicksort\nCursed Technique: Window 2\nMaximum Technique: Any\n" +
				"    Using: (a) -> first(a) = last(a)\n"),
			explain: "a duplicate check needs a set, not an ordering"},
		{name: "sort, window, all neighbours differ",
			src: p("Domain Expansion: Quicksort, Descending\nCursed Technique: Window 2\nMaximum Technique: All\n" +
				"    Using: (a) -> not (item(a, 0) = item(a, 1))\n"),
			explain: "a duplicate check needs a set, not an ordering"},
		{name: "sort, window, any neighbour smaller is not a duplicate check",
			src: p("Domain Expansion: Quicksort\nCursed Technique: Window 2\nMaximum Technique: Any\n" +
				"    Using: (a) -> first(a) < last(a)\n"),
			explainAbsent: "duplicate check"},
		{name: "all pairs mapped to a flag, any flag set",
			src: p("Domain Expansion: All Pairs\n    Mode: Map\n    Using: (a, b) -> if a = b then 0 else 1\n" +
				"Maximum Technique: Any\n    Using: (x) -> x = 0\n"),
			explain: "not every pair"},
		{name: "all pairs mapped to a bool, all false",
			src: p("Domain Expansion: All Pairs\n    Mode: Map\n    Using: (a, b) -> a = b\n" +
				"Maximum Technique: All\n    Using: (x) -> ikke x\n"),
			explain: "not every pair"},
		{name: "all pairs, Mode: Any, a = b",
			src:     p("Domain Expansion: All Pairs\n    Mode: Any\n    Using: (a, b) -> a = b\n"),
			explain: "Mode: Any"},
		{name: "all pairs, Mode: Any, flipped",
			src:     p("Domain Expansion: All Pairs\n    Mode: Any\n    Using: (x, y) -> y = x\n"),
			explain: "Mode: Any"},
		{name: "all pairs, Mode: All, all distinct",
			src:     p("Domain Expansion: All Pairs\n    Mode: All\n    Using: (a, b) -> ikke a = b\n"),
			explain: "Mode: All"},
		{name: "all pairs, Mode: All, a = b is not a duplicate check",
			src:           p("Domain Expansion: All Pairs\n    Mode: All\n    Using: (a, b) -> a = b\n"),
			explainAbsent: "hash pass"},
		{name: "all pairs, Mode: Any, a sum is not a duplicate check",
			src:           p("Domain Expansion: All Pairs\n    Mode: Any\n    Using: (a, b) -> a + b = 4\n"),
			explainAbsent: "hash pass"},
		{name: "all pairs over lists, mapped to a flag, any flag set",
			src: p("Cursed Technique: Chunk 2\nDomain Expansion: All Pairs\n    Mode: Map\n    Using: (a, b) -> if a = b then 0 else 1\n" +
				"Maximum Technique: Any\n    Using: (x) -> x = 0\n"),
			explain:     "not every pair",
			extraInputs: []string{"1\n2\n3\n1\n2", "1\n2\n3\n4\n1\n2", "5\n5"}},
		{name: "all pairs mapped to a sum is not a duplicate check",
			src: p("Domain Expansion: All Pairs\n    Mode: Map\n    Using: (a, b) -> a + b\n" +
				"Maximum Technique: Any\n    Using: (x) -> x = 0\n"),
			explainAbsent: "duplicate check"},

		// --- a Bool summed as a number ---
		{name: "sum by a one-or-zero",
			src:     p("Maximum Technique: Sum By\n    Using: (x) -> if x > 2 then 1 else 0\n"),
			explain: "→ Count Matching (a Bool"},
		{name: "sum by a zero-or-one",
			src:     p("Maximum Technique: Sum By\n    Using: (x) -> if x > 2 then 0 else 1\n"),
			explain: "→ Count Matching (a Bool"},
		{name: "map to one-or-zero then sum",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> if x % 2 = 0 then 1 else 0\nMaximum Technique: Sum\n"),
			explain: "→ Count Matching (a Bool"},
		{name: "sum by a failing condition keeps its name",
			src:           p("Maximum Technique: Sum By\n    Using: (x) -> if 10 / x > 2 then 1 else 0\n"),
			explainAbsent: "→ Count Matching (a Bool",
			extraInputs:   []string{"3\n0\n2"}},
		{name: "sum by a two-or-zero is a sum",
			src:           p("Maximum Technique: Sum By\n    Using: (x) -> if x > 2 then 2 else 0\n"),
			explainAbsent: "→ Count Matching (a Bool"},
		{name: "a bool turned into a number and asked what it was",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> (if x > 2 then 0 else 1) = 0\n"),
			explain: "boolean round trip"},
		{name: "a number compared the other way round",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> 1 > (if x > 2 then 0 else 1)\n"),
			explain: "boolean round trip"},

		// --- empty lists filtered out of a Flatten ---
		{name: "non-empty filter before flatten",
			src: p("Cursed Technique: Chunk 3\nCursed Technique: Filter\n    Using: (c) -> length(c) > 0\n" +
				"Cursed Technique: Flatten\n"),
			explain: "adds nothing to a concatenation"},
		{name: "a filter that keeps the long ones is not dead",
			src: p("Cursed Technique: Chunk 3\nCursed Technique: Filter\n    Using: (c) -> length(c) > 2\n" +
				"Cursed Technique: Flatten\n"),
			explainAbsent: "adds nothing to a concatenation"},

		// --- Iterate searched for its first repeat ---
		{name: "iterate then find cycle",
			src: p("Cursed Technique: Apply\n    Using: (xs) -> sum(xs)\nCursed Technique: Iterate 40\n" +
				"    Using: (x) -> (x * 3 + 1) % 7\nMaximum Technique: Find Cycle\n"),
			explain: "stops at its first repeated state"},
		{name: "iterate, a many-to-one map, find cycle",
			src: p("Cursed Technique: Apply\n    Using: (xs) -> sum(xs)\nCursed Technique: Iterate 60\n" +
				"    Using: (x) -> (x * 5 + 3) % 23\nCursed Technique: Map Each\n    Using: (x) -> x % 3\n" +
				"Maximum Technique: Find Cycle\n"),
			explain: "stops at its first repeated state"},
		{name: "iterate with a step that can fail",
			src: p("Cursed Technique: Apply\n    Using: (xs) -> sum(xs)\nCursed Technique: Iterate 30\n" +
				"    Using: (x) -> (100 / (x - 3)) % 11\nMaximum Technique: Find Cycle\n"),
			explain:     "stops at its first repeated state",
			extraInputs: []string{"3", "4", "5", "103", "-97"}},
		{name: "iterate with a map that can fail",
			src: p("Cursed Technique: Apply\n    Using: (xs) -> sum(xs)\nCursed Technique: Iterate 30\n" +
				"    Using: (x) -> (x * 3 + 1) % 7\nCursed Technique: Map Each\n    Using: (x) -> 10 / (x - 5)\n" +
				"Maximum Technique: Find Cycle\n"),
			explain:     "stops at its first repeated state",
			extraInputs: []string{"5", "4", "1"}},
		{name: "iterate over a whole list, in a body, keyed by a map",
			src: p("Cursed Technique: Iterate 25\n" +
				"    Cursed Technique: Apply\n        Using: (xs) -> concat(drop(xs, 1), take(xs, 1))\n" +
				"Cursed Technique: Map Each\n" +
				"    Cursed Technique: Map Each\n        Using: (x) -> totext(x)\n" +
				"    Cursed Technique: Apply\n        Using: (ts) -> textjoin(ts, \",\")\n" +
				"Maximum Technique: Find Cycle\n"),
			explain: "stops at its first repeated state"},
		{name: "iterate never repeating runs to its count",
			src: p("Cursed Technique: Apply\n    Using: (xs) -> sum(xs)\nCursed Technique: Iterate 12\n" +
				"    Using: (x) -> x + 1\nMaximum Technique: Find Cycle\n"),
			explain: "stops at its first repeated state"},
		{name: "iterate read whole keeps every lap",
			src: p("Cursed Technique: Apply\n    Using: (xs) -> sum(xs)\nCursed Technique: Iterate 12\n" +
				"    Using: (x) -> (x * 3 + 1) % 7\n"),
			explainAbsent: "first repeated state"},
		{name: "iterate filtered before the search keeps every lap",
			src: p("Cursed Technique: Apply\n    Using: (xs) -> sum(xs)\nCursed Technique: Iterate 12\n" +
				"    Using: (x) -> (x * 3 + 1) % 7\nCursed Technique: Filter\n    Using: (x) -> x > 1\n" +
				"Maximum Technique: Find Cycle\n"),
			explainAbsent: "first repeated state"},

		// --- length-changing passes inside a Using: body ---
		{name: "duplicate check inside a Map Each body",
			src: p("Cursed Technique: Chunk 4\nCursed Technique: Map Each\n" +
				"    Domain Expansion: Quicksort\n    Cursed Technique: Window 2\n" +
				"    Maximum Technique: Any\n        Using: (a) -> first(a) = last(a)\n" +
				"Maximum Technique: Sum By\n    Using: (x) -> if x then 0 else 1\n"),
			explain: "a duplicate check needs a set, not an ordering"},
		{name: "map and sum fuse inside a Map Each body",
			src: p("Cursed Technique: Chunk 3\nCursed Technique: Map Each\n" +
				"    Cursed Technique: Map Each\n        Using: (x) -> x * 2\n    Maximum Technique: Sum\n"),
			explain: "→ Sum By"},
		{name: "dead sort inside a body inside a body",
			src: p("Cursed Technique: Chunk 4\nCursed Technique: Chunk 2\nCursed Technique: Map Each\n" +
				"    Cursed Technique: Map Each\n" +
				"        Domain Expansion: Quicksort\n        Maximum Technique: Sum\n" +
				"    Maximum Technique: Max\n"),
			explain: "order-insensitive"},

		// --- Map + Map: a called body read twice is bound, not repeated ---
		{name: "map fusion binds a body that calls",
			src: p("Cursed Technique: Chunk 3\nCursed Technique: Map Each\n    Using: (c) -> sum(c) + length(c)\n" +
				"Cursed Technique: Map Each\n    Using: (y) -> y * y + y\n"),
			explain: "fused Map Each"},
	}
}

func TestLongWayMatchesNaiveOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for _, c := range longWayCases() {
		t.Run(c.name, func(t *testing.T) {
			naive, _ := resolveProgram(t, c.src, false)
			opt, rewrites := resolveProgram(t, c.src, true)

			if c.explain != "" && !containsMessage(rewrites, c.explain) {
				t.Fatalf("expected a rewrite containing %q, got %v", c.explain, messages(rewrites))
			}
			if c.explainAbsent != "" && containsMessage(rewrites, c.explainAbsent) {
				t.Fatalf("rewrite containing %q must not fire, got %v", c.explainAbsent, messages(rewrites))
			}

			inputs := append([]string{}, c.extraInputs...)
			inputs = append(inputs, "", "0", "3\n3\n3", "1\n2\n3\n4\n5\n6\n7")
			for range 80 {
				inputs = append(inputs, intsInput(randInts(rng, 12, 9)))
			}
			for _, input := range inputs {
				wantOut, wantErr := interpret(naive, input)
				gotOut, gotErr := interpret(opt, input)
				if (gotErr != nil) != (wantErr != nil) {
					t.Fatalf("input %q: error divergence\noptimized err: %v\nnaive err: %v", input, gotErr, wantErr)
				}
				if wantErr != nil && gotErr.Error() != wantErr.Error() {
					t.Fatalf("input %q: the failure changed its words\noptimized: %v\nnaive:     %v", input, gotErr, wantErr)
				}
				if wantErr == nil && gotOut != wantOut {
					t.Fatalf("input %q: output divergence\noptimized: %q\nnaive:     %q", input, gotOut, wantOut)
				}
			}
		})
	}
}

// The mapped body of a fused Map + Map must be evaluated once per element even
// when the second lambda reads its parameter twice.
func TestMapFusionDoesNotRepeatACall(t *testing.T) {
	src := listHeader + "Cursed Technique: Chunk 3\nCursed Technique: Map Each\n    Using: (c) -> sum(c)\n" +
		"Cursed Technique: Map Each\n    Using: (y) -> y * y + y\nReveal: stdout\n"
	pipe, rewrites := resolveProgram(t, src, true)
	if !containsMessage(rewrites, "fused Map Each") {
		t.Fatalf("expected the maps to fuse, got %v", messages(rewrites))
	}
	for _, n := range pipe.Nodes {
		if n.Prim != "Map Each" {
			continue
		}
		lam := nodeLambda(n)
		if got := countCalls(lam.Body, "sum"); got != 1 {
			t.Fatalf("the fused body calls sum %d times, want 1", got)
		}
	}
}

func countCalls(e ast.Expr, name string) int {
	n := 0
	switch x := e.(type) {
	case *ast.CallExpr:
		if builtinName(x) == name {
			n++
		}
		for _, a := range x.Args {
			n += countCalls(a, name)
		}
	case *ast.UnaryExpr:
		n += countCalls(x.X, name)
	case *ast.BinaryExpr:
		n += countCalls(x.Left, name) + countCalls(x.Right, name)
	case *ast.CondExpr:
		n += countCalls(x.Cond, name) + countCalls(x.Then, name) + countCalls(x.Else, name)
	case *ast.LetExpr:
		n += countCalls(x.Value, name) + countCalls(x.Body, name)
	}
	return n
}
