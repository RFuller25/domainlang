package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// Explore is the language's answer for problems that look recursive. These
// cover the four modes, termination over a cyclic state space, and the
// keyable-state requirement that makes termination possible.

func exploreSrc(body string) string {
	return "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (s) -> toint(s)\n" + body
}

func TestExploreCollectsInBFSOrder(t *testing.T) {
	v, _ := runPipeline(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Using: (n) -> if n > 8 then list(n) else list(n * 2, n + 3)\n"), "1")
	// Seed first, then depth 1, then depth 2 — BFS order is what makes the
	// distances below the *shortest* ones.
	if got := ir.FormatValue(v); !strings.HasPrefix(got, "[1, 2, 4") {
		t.Fatalf("expected BFS order from the seed, got %s", got)
	}
}

func TestExploreCountsDistinctStates(t *testing.T) {
	// A cyclic space: without the visited set this never terminates.
	v, _ := runPipeline(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Mode: Count\n"+
			"    Using: (n) -> list(mod(n + 1, 5))\n"), "0")
	if v.(int64) != 5 {
		t.Fatalf("a 5-cycle has 5 distinct states, got %v", v)
	}
}

func TestExploreDistancesAreShortest(t *testing.T) {
	v, _ := runPipeline(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Mode: Distances\n"+
			"    Using: (n) -> if n > 4 then list(n) else list(n + 1, n + 2)\n"), "0")
	m, ok := v.(*ir.MapValue)
	if !ok {
		t.Fatalf("Distances should produce a Map, got %s", ir.DescribeValue(v))
	}
	// 0 -> 2 -> 4 is two steps, shorter than 0 -> 1 -> 2 -> 3 -> 4.
	d, _ := m.Get(int64(4))
	if d != int64(2) {
		t.Fatalf("shortest distance to 4 is 2, got %v", d)
	}
	if d0, _ := m.Get(int64(0)); d0 != int64(0) {
		t.Fatalf("the seed is at distance 0, got %v", d0)
	}
}

func TestExploreStepsToFirstMatch(t *testing.T) {
	v, _ := runPipeline(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Mode: Steps\n"+
			"    Until: (n) -> n = 27\n"+
			"    Using: (n) -> if n > 40 then list(n) else list(n * 2, n + 3)\n"), "3")
	if v.(int64) != 4 {
		t.Fatalf("27 is 4 steps from 3, got %v", v)
	}
}

// A seed that already satisfies Until: is zero steps, and nothing is expanded.
func TestExploreStepsSeedAlreadyMatches(t *testing.T) {
	v, _ := runPipeline(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Mode: Steps\n"+
			"    Until: (n) -> n > 0\n"+
			"    Using: (n) -> list(n + 1)\n"), "5")
	if v.(int64) != 0 {
		t.Fatalf("a seed that already matches is 0 steps, got %v", v)
	}
}

// An unreachable target answers -1, the sentinel Find Index already uses,
// rather than erroring.
func TestExploreStepsUnreachable(t *testing.T) {
	v, _ := runPipeline(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Mode: Steps\n"+
			"    Until: (n) -> n = 7\n"+
			"    Using: (n) -> list(mod(n + 2, 6))\n"), "0")
	if v.(int64) != -1 {
		t.Fatalf("an unreachable target is -1, got %v", v)
	}
}

// Compound states are exactly what tuple() was added for, and they key the
// visited set structurally.
func TestExploreOverTupleStates(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (s) -> point(0, 0)\n" +
		"Domain Expansion: Explore\n" +
		"    Mode: Count\n" +
		"    Using: (p) -> if prow(p) + pcol(p) > 2 then list(p) else " +
		"list(point(prow(p) + 1, pcol(p)), point(prow(p), pcol(p) + 1))\n"
	v, _ := runPipeline(t, src, "x")
	// The reachable set is every point with row+col <= 3: 1+2+3+4 = 10.
	if v.(int64) != 10 {
		t.Fatalf("expected 10 reachable points, got %v", v)
	}
}

func TestExploreRejectsUnkeyableState(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Domain Expansion: Explore\n" +
		"    Using: (xs) -> list(xs)\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected a keyability error for a List state")
	}
	if msg := err.Error(); !strings.Contains(msg, "keyable") || !strings.Contains(msg, "tuple(") {
		t.Errorf("error should explain keyability and suggest tuple(...), got: %s", msg)
	}
}

func TestExploreRejectsWrongSuccessorType(t *testing.T) {
	_, err := resolveSrc(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Using: (n) -> n + 1\n"))
	if err == nil {
		t.Fatal("expected an error for a non-List successor lambda")
	}
	if msg := err.Error(); !strings.Contains(msg, "List<Int>") {
		t.Errorf("error should name the expected successor type, got: %s", msg)
	}
}

func TestExploreStepsRequiresUntil(t *testing.T) {
	_, err := resolveSrc(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Mode: Steps\n"+
			"    Using: (n) -> list(n + 1)\n"))
	if err == nil {
		t.Fatal("expected an error: Steps has nothing to measure to")
	}
	if msg := err.Error(); !strings.Contains(msg, "Until:") {
		t.Errorf("error should ask for Until:, got: %s", msg)
	}
}

func TestExploreRejectsUnknownMode(t *testing.T) {
	_, err := resolveSrc(t, exploreSrc(
		"Domain Expansion: Explore\n"+
			"    Mode: Wander\n"+
			"    Using: (n) -> list(n + 1)\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown Mode") {
		t.Fatalf("expected an unknown-Mode error, got %v", err)
	}
}
