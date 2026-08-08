package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// The weighted and folding halves of Explore: Mode: Cheapest / Costs, which
// settle states in cost order rather than step order, and Mode: Tally, which
// folds the reachable DAG instead of walking it.

// A line graph whose edges cost the state being entered. From 0 the reachable
// states are 0..7 by steps of 1 or 2, so there are competing routes and the
// cheapest one is not the shortest one.
const weightedSrc = "Cursed Energy: stdin\n" +
	"Cursed Technique: Apply\n" +
	"    Using: (s) -> toint(s)\n" +
	"Domain Expansion: Explore\n"

const weightedSucc = "    Using: (n) -> if n < 6 then list(n + 1, n + 2) else take(list(n), 0)\n"

func TestExploreCheapestBeatsTheShortestPath(t *testing.T) {
	// Entering n costs n, so skipping a state is cheaper than stepping
	// through it: 0 → 2 → 4 → 6 costs 12, while 0 → 1 → 2 → … → 6 costs 21.
	src := weightedSrc +
		"    Mode: Cheapest\n" +
		"    Cost: (n) -> n\n" +
		"    Until: (n) -> n = 6\n" + weightedSucc
	v, _ := runPipeline(t, src, "0")
	if v.(int64) != 12 {
		t.Fatalf("cheapest: got %v, want 12", v)
	}
	// The same search counting steps answers 3, which is the point: the two
	// questions have different answers over one graph.
	steps := weightedSrc +
		"    Mode: Steps\n" +
		"    Until: (n) -> n = 6\n" + weightedSucc
	v, _ = runPipeline(t, steps, "0")
	if v.(int64) != 3 {
		t.Fatalf("steps: got %v, want 3", v)
	}
}

func TestExploreCheapestIsMinusOneWhenUnreachable(t *testing.T) {
	src := weightedSrc +
		"    Mode: Cheapest\n" +
		"    Cost: (n) -> 1\n" +
		"    Until: (n) -> n = 99\n" + weightedSucc
	v, _ := runPipeline(t, src, "0")
	if v.(int64) != -1 {
		t.Fatalf("unreachable: got %v, want -1 (the Find Index sentinel)", v)
	}
}

// Mode: Costs is to Cheapest what Distances is to Steps.
func TestExploreCostsReportsEveryState(t *testing.T) {
	src := weightedSrc +
		"    Mode: Costs\n" +
		"    Cost: (n) -> n\n" + weightedSucc
	v, _ := runPipeline(t, src, "0")
	m, ok := v.(*ir.MapValue)
	if !ok {
		t.Fatalf("expected a Map, got %T", v)
	}
	for _, tc := range []struct {
		state, want int64
	}{{0, 0}, {1, 1}, {2, 2}, {3, 4}, {4, 6}, {6, 12}} {
		got, present := m.Get(tc.state)
		if !present {
			t.Errorf("state %d missing from the cost map", tc.state)
			continue
		}
		if got.(int64) != tc.want {
			t.Errorf("cost to %d: got %v, want %d", tc.state, got, tc.want)
		}
	}
}

// The 2-parameter Cost: is the edge weight a node weight cannot express.
func TestExploreCostTakesAnEdge(t *testing.T) {
	src := weightedSrc +
		"    Mode: Cheapest\n" +
		"    Cost: (a, b) -> (b - a) * (b - a)\n" +
		"    Until: (n) -> n = 6\n" + weightedSucc
	// Squared step lengths: three 2-steps cost 12, six 1-steps cost 6.
	v, _ := runPipeline(t, src, "0")
	if v.(int64) != 6 {
		t.Fatalf("edge cost: got %v, want 6", v)
	}
}

// Dijkstra settles a state the first time it is popped, which a negative edge
// can invalidate after the fact — so it is an error rather than a wrong
// answer, exactly as grid Dijkstra refuses negative cells.
func TestExploreRefusesANegativeCost(t *testing.T) {
	src := weightedSrc +
		"    Mode: Cheapest\n" +
		"    Cost: (n) -> 0 - n\n" +
		"    Until: (n) -> n = 6\n" + weightedSucc
	_, err := runErr(t, src+"Reveal: stdout\n", "0")
	if err == nil || !strings.Contains(err.Error(), "negative cost") {
		t.Fatalf("expected a negative-cost error, got %v", err)
	}
}

// Whatever Explore's weighted search answers, the grid Dijkstra primitive
// answers too — one ordering reached two ways, over the same graph.
func TestExploreCheapestAgreesWithGridDijkstra(t *testing.T) {
	const grid = "199\n199\n111"
	builtin := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \"\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Channeled Energy: Convert To Grid\n" +
		"Domain Expansion: Dijkstra from 0 0\n" +
		"Cursed Technique: Apply\n    Using: (g) -> at(g, 2, 2)\n"
	viaExplore := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \"\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Channeled Energy: Convert To Grid\n" +
		"Cursed Technique: Apply\n" +
		"    Consider g Of Apply\n        Using: (x) -> x\n" +
		"    Cursed Technique: Apply\n        Using: (x) -> point(0, 0)\n" +
		"    Domain Expansion: Explore\n" +
		"        Mode: Cheapest\n" +
		"        Until: (p) -> p = point(2, 2)\n" +
		"        Cost: (p) -> at(g, prow(p), pcol(p))\n" +
		"        Using: (p) -> neighbors4(g, prow(p), pcol(p))\n"
	want, _ := runPipeline(t, builtin, grid)
	got, _ := runPipeline(t, viaExplore, grid)
	if got != want {
		t.Fatalf("Explore Mode: Cheapest = %v, grid Dijkstra = %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Mode: Tally
// ---------------------------------------------------------------------------

// AoC 2020 Day 10 Part 2 as a DAG fold rather than a linear DP: how many ways
// are there from 0 to the largest adapter in jumps of 1, 2 or 3? This is the
// question that had no spelling before Tally — the subproblems form a DAG,
// not a line.
func TestExploreTallyCountsPaths(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Cursed Technique: Apply\n" +
		"    Consider adapters Of Convert To Set\n" +
		"    Consider top Of Max\n" +
		"    Cursed Technique: Apply\n        Using: (xs) -> 0\n" +
		"    Domain Expansion: Explore\n" +
		"        Mode: Tally\n" +
		"        Until: (j) -> j = top\n" +
		"        Value: (j) -> 1\n" +
		"        Combine: (a, b) -> a + b\n" +
		"        Cursed Technique: Apply\n            Using: (j) -> list(j + 1, j + 2, j + 3)\n" +
		"        Cursed Technique: Filter\n            Using: (n) -> contains(adapters, n)\n"
	v, _ := runPipeline(t, src, "16\n10\n15\n5\n1\n11\n7\n19\n6\n12\n4")
	if v.(int64) != 8 {
		t.Fatalf("path count: got %v, want 8", v)
	}
}

// A state reached more than once is folded once, and the size of the answer
// is what proves it: every state under 60 has two successors that meet again,
// so there are 4,052,739,537,881 distinct paths and only 61 distinct states.
// Without the memo this test would not finish; with it, it is 61 folds.
func TestExploreTallyMemoizesSharedSubproblems(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n" +
		"Domain Expansion: Explore\n" +
		"    Mode: Tally\n" +
		"    Value: (n) -> 1\n" +
		"    Combine: (a, b) -> a + b\n" +
		"    Using: (n) -> if n < 60 then list(n + 1, n + 2) else take(list(n), 0)\n"
	v, _ := runPipeline(t, src, "0")
	if v.(int64) != 4052739537881 {
		t.Fatalf("tally: got %v, want 4052739537881", v)
	}
}

// Value: is not restricted to Int — Combine: folds whatever Value: produced.
func TestExploreTallyFoldsAnyValueType(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n" +
		"Domain Expansion: Explore\n" +
		"    Mode: Tally\n" +
		"    Value: (n) -> totext(n)\n" +
		"    Combine: (a, b) -> a + \"|\" + b\n" +
		"    Using: (n) -> if n < 3 then list(n + 1, n + 3) else take(list(n), 0)\n"
	v, _ := runPipeline(t, src, "0")
	if _, ok := v.(string); !ok {
		t.Fatalf("expected the Text Value: produced, got %T (%v)", v, v)
	}
}

// A cycle has no finite fold. The message names the state, because "there is
// a cycle" alone leaves it to be found by hand in a large search space —
// the same reason Topological Sort names a blocked node.
func TestExploreTallyNamesACycle(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n" +
		"Domain Expansion: Explore\n" +
		"    Mode: Tally\n" +
		"    Value: (n) -> 1\n" +
		"    Combine: (a, b) -> a + b\n" +
		"    Using: (n) -> list((n + 1) % 3)\n" +
		"Reveal: stdout\n"
	_, err := runErr(t, src, "0")
	if err == nil || !strings.Contains(err.Error(), "reachable from itself") {
		t.Fatalf("expected a named cycle error, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "0") {
		t.Errorf("the cycle error should name the offending state: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Argument rules
// ---------------------------------------------------------------------------

// Each argument belongs to the modes it changes the answer for, and says so
// where it does not — the same shape of rule as Until: being required by
// Steps.
func TestExploreModeArgumentRules(t *testing.T) {
	succ := "    Using: (n) -> take(list(n), 0)\n"
	for _, tc := range []struct{ name, args, want string }{
		{"Cheapest needs a Cost", "    Mode: Cheapest\n    Until: (n) -> n = 1\n",
			"needs a Cost: lambda"},
		{"Cheapest needs an Until", "    Mode: Cheapest\n    Cost: (n) -> 1\n",
			"needs an Until: predicate"},
		{"Costs needs a Cost", "    Mode: Costs\n", "needs a Cost: lambda"},
		{"Cost is refused by a step mode", "    Mode: Distances\n    Cost: (n) -> 1\n",
			"Cost: applies to Mode: Cheapest and Mode: Costs"},
		{"Tally needs Value and Combine", "    Mode: Tally\n    Value: (n) -> 1\n",
			"needs both a Value: lambda"},
		{"Value is refused elsewhere", "    Mode: Count\n    Value: (n) -> 1\n",
			"Value: applies to Mode: Tally"},
		{"Combine must preserve the type", "    Mode: Tally\n    Value: (n) -> 1\n" +
			"    Combine: (a, b) -> totext(a)\n", "must fold two Int into one"},
		{"Cost arity is 1 or 2", "    Mode: Cheapest\n    Until: (n) -> n = 1\n" +
			"    Cost: (a, b, c) -> 1\n", "takes 1 parameter"},
		{"unknown mode lists them all", "    Mode: Cheapo\n", "Cheapest, Costs, Tally"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Cursed Energy: stdin\n" +
				"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n" +
				"Domain Expansion: Explore\n" + tc.args + succ + "Reveal: stdout\n"
			_, err := runErr(t, src, "0")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected an error containing %q, got %v", tc.want, err)
			}
		})
	}
}
