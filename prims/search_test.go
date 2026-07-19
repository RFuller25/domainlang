package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// charGrid builds a Grid<Text> from stdin lines.
const charGrid = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\"\n" +
	"Channeled Energy: Convert To Grid\n"

func TestBFSDistances(t *testing.T) {
	src := charGrid +
		"Domain Expansion: BFS from 0 0\n" +
		"    Using: (c) -> c = \".\"\n"
	// . . #     0 1 -1
	// # . .  →  -1 2 3
	// . . .     -1 3 4   (left column blocked from above? no: (2,0) reachable
	//                     via (2,1): 0→(0,1)1→(1,1)2→(1,2)3/(2,1)3→(2,0)4,(2,2)4)
	v, _ := runPipeline(t, src, "..#\n#..\n...")
	g := v.(*ir.GridValue)
	want := []int64{0, 1, -1, -1, 2, 3, 4, 3, 4}
	for i, w := range want {
		if g.Cells[i] != w {
			t.Fatalf("cell %d: got %v want %d (grid:\n%s)", i, g.Cells[i], w, ir.FormatValue(g))
		}
	}
}

func TestBFSUnreachableStaysMinusOne(t *testing.T) {
	src := charGrid +
		"Domain Expansion: BFS from 0 0\n" +
		"    Using: (c) -> c = \".\"\n"
	v, _ := runPipeline(t, src, ".#.\n###\n...")
	g := v.(*ir.GridValue)
	// The bottom row is cut off entirely.
	for c := 0; c < 3; c++ {
		if cell, _ := g.At(2, c); cell.(int64) != -1 {
			t.Fatalf("cell (2,%d) should be unreachable, got %v", c, cell)
		}
	}
}

func TestBFSStartErrors(t *testing.T) {
	oob := charGrid +
		"Domain Expansion: BFS from 9 9\n" +
		"    Using: (c) -> c = \".\"\n"
	_, err := runErr(t, oob, "..\n..")
	if err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("expected out-of-bounds error, got %v", err)
	}
	blocked := charGrid +
		"Domain Expansion: BFS from 0 0\n" +
		"    Using: (c) -> c = \".\"\n"
	_, err = runErr(t, blocked, "#.\n..")
	if err == nil || !strings.Contains(err.Error(), "not walkable") {
		t.Fatalf("expected not-walkable error, got %v", err)
	}
	noCoords := charGrid +
		"Domain Expansion: BFS\n" +
		"    Using: (c) -> c = \".\"\n"
	_, err = runErr(t, noCoords, "..\n..")
	if err == nil || !strings.Contains(err.Error(), "requires start coordinates") {
		t.Fatalf("expected missing-coordinates error, got %v", err)
	}
}

// digitCostGrid builds a Grid<Int> of per-cell entry costs.
const digitCostGrid = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\"\n" +
	"Cursed Technique: Split Each by \"\"\n" +
	"Channeled Energy: Convert Each List to Integers\n" +
	"Channeled Energy: Convert To Grid\n"

func TestDijkstraRiskMap(t *testing.T) {
	// The AoC 2021 D15 example, shrunk: entering a cell costs its digit; the
	// start's cost is not paid.
	src := digitCostGrid +
		"Domain Expansion: Dijkstra from 0 0\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (g) -> at(g, 2, 2)\n"
	// 1 1 6
	// 1 3 8
	// 2 1 3
	// Cheapest to (2,2): 1→1→3→1→3 = down,down? path 0,0→1,0(1)→2,0(2)→2,1(1)→2,2(3) = 7.
	v, _ := runPipeline(t, src, "116\n138\n213")
	if v.(int64) != 7 {
		t.Fatalf("min cost: got %v want 7", v)
	}
}

func TestDijkstraPrefersCheapDetour(t *testing.T) {
	src := digitCostGrid +
		"Domain Expansion: Dijkstra from 0 0\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (g) -> at(g, 0, 2)\n"
	// 1 9 1: straight right costs 9+1=10; around the bottom row it is four
	// steps of cost 1 (down, right, right, up) = 4.
	v, _ := runPipeline(t, src, "191\n111")
	if v.(int64) != 4 {
		t.Fatalf("detour cost: got %v want 4", v)
	}
}

func TestDijkstraRejectsNegativeAndNonIntGrids(t *testing.T) {
	neg := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Fields\n" +
		"Channeled Energy: Convert Each List to Integers\n" +
		"Channeled Energy: Convert To Grid\n" +
		"Domain Expansion: Dijkstra from 0 0\n"
	_, err := runErr(t, neg, "1 -2\n3 4")
	if err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("expected negative-cost error, got %v", err)
	}
	text := charGrid + "Domain Expansion: Dijkstra from 0 0\n"
	_, err = runErr(t, text, "ab\ncd")
	if err == nil || !strings.Contains(err.Error(), "expects Grid<Int>") {
		t.Fatalf("expected a resolve-time type error, got %v", err)
	}
}

func TestFloodFillMasksOneRegion(t *testing.T) {
	src := charGrid +
		"Domain Expansion: Flood Fill from 0 0\n" +
		"    Using: (c) -> c = \"#\"\n" +
		"Maximum Technique: Count Cells\n" +
		"    Using: (m) -> m = 1\n"
	// Two # regions; only the one containing (0,0) is filled.
	v, _ := runPipeline(t, src, "##.\n.#.\n..#")
	if v.(int64) != 3 {
		t.Fatalf("region size: got %v want 3", v)
	}
}

func TestFloodFillStartOutsideRegionErrors(t *testing.T) {
	src := charGrid +
		"Domain Expansion: Flood Fill from 0 0\n" +
		"    Using: (c) -> c = \"#\"\n"
	_, err := runErr(t, src, ".#\n##")
	if err == nil || !strings.Contains(err.Error(), "is not in the region") {
		t.Fatalf("expected start-not-in-region error, got %v", err)
	}
}

func TestConnectedComponents(t *testing.T) {
	src := charGrid +
		"Domain Expansion: Connected Components\n" +
		"    Using: (c) -> c = \"#\"\n"
	// Three diagonal-only # groups: diagonals do not connect (4-connectivity).
	v, _ := runPipeline(t, src, "#.#\n.#.\n...")
	if v.(int64) != 3 {
		t.Fatalf("components: got %v want 3", v)
	}
	v, _ = runPipeline(t, src, "##.\n.#.\n..#")
	if v.(int64) != 2 {
		t.Fatalf("components: got %v want 2", v)
	}
	v, _ = runPipeline(t, src, "...\n...\n...")
	if v.(int64) != 0 {
		t.Fatalf("empty grid: got %v want 0", v)
	}
}

func TestBFSAgreesWithDijkstraOnUnitCosts(t *testing.T) {
	// Property: on a grid of all-1 costs, Dijkstra's distances equal BFS step
	// counts (with an all-walkable predicate).
	input := "1111\n1111\n1111"
	bfsSrc := digitCostGrid +
		"Domain Expansion: BFS from 1 2\n" +
		"    Using: (c) -> c = 1\n"
	dijSrc := digitCostGrid +
		"Domain Expansion: Dijkstra from 1 2\n"
	bv, _ := runPipeline(t, bfsSrc, input)
	dv, _ := runPipeline(t, dijSrc, input)
	if !ir.DeepEqual(bv, dv) {
		t.Fatalf("BFS %s != Dijkstra %s on unit costs", ir.FormatValue(bv), ir.FormatValue(dv))
	}
}
