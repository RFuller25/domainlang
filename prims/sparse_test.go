package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// pointLines parses "r,c" lines into List<(Int, Int)> — the standard way a
// point cloud enters a program.
const pointLines = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\"\n" +
	"Cursed Technique: Match Pattern\n" +
	"    Using: \"{int},{int}\"\n" +
	"    Mode: Each\n"

func TestSparseFromPoints(t *testing.T) {
	src := pointLines +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: \".\"\n" +
		"    Mark: \"#\"\n"
	v, _ := runPipeline(t, src, "0,0\n1,2\n0,0")
	sp, ok := v.(*ir.SparseValue)
	if !ok {
		t.Fatalf("expected Sparse, got %T", v)
	}
	if sp.Len() != 2 { // duplicate point collapses
		t.Fatalf("Len = %d, want 2", sp.Len())
	}
	if got := ir.FormatValue(sp); got != "{[0, 0]: #, [1, 2]: #}" {
		t.Fatalf("render = %q", got)
	}
}

func TestSparseDensifyToPicture(t *testing.T) {
	src := pointLines +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: \".\"\n" +
		"    Mark: \"#\"\n" +
		"Channeled Energy: Convert To Grid\n"
	// Negative coordinates: densify translates the bounding box to (0, 0).
	v, _ := runPipeline(t, src, "-1,-1\n1,2")
	g, ok := v.(*ir.GridValue)
	if !ok {
		t.Fatalf("expected Grid, got %T", v)
	}
	if got := ir.FormatValue(g); got != "#...\n....\n...#" {
		t.Fatalf("picture = %q", got)
	}
}

func TestSparseFromGridDropsDefaults(t *testing.T) {
	src := digitGrid +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: 0\n" +
		"Maximum Technique: Count Cells\n" +
		"    Using: (h) -> h >= 5\n"
	v, _ := runPipeline(t, src, "305\n007")
	// Set cells are 3,5,7 (zeros equal the default); >= 5 keeps 5 and 7.
	if v.(int64) != 2 {
		t.Fatalf("count = %v, want 2", v)
	}
}

func TestSparseFromMap(t *testing.T) {
	src := pointLines +
		"Cursed Technique: Map Each\n" +
		"    Using: (xs) -> point(item(xs, 0), item(xs, 1))\n" +
		"Maximum Technique: Count By\n" +
		"    Using: (p) -> p\n" +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: 0\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (g) -> at(g, 0, 0)\n"
	v, _ := runPipeline(t, src, "0,0\n1,2\n0,0")
	if v.(int64) != 2 {
		t.Fatalf("visit count at (0,0) = %v, want 2", v)
	}
}

func TestSparseMapCellsMapsDefaultToo(t *testing.T) {
	src := pointLines +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: \".\"\n" +
		"    Mark: \"#\"\n" +
		"Cursed Technique: Map Cells\n" +
		"    Using: (c) -> if c = \"#\" then 1 else 0\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (g) -> at(g, 9, 9) + at(g, 0, 0)\n"
	v, _ := runPipeline(t, src, "0,0")
	// Unset (9,9) reads the mapped default 0; set (0,0) maps to 1.
	if v.(int64) != 1 {
		t.Fatalf("mapped reads = %v, want 1", v)
	}
}

func TestSparseFindCellsSortedRowMajor(t *testing.T) {
	src := pointLines +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: \".\"\n" +
		"    Mark: \"#\"\n" +
		"Cursed Technique: Find Cells\n" +
		"    Using: (c) -> c = \"#\"\n"
	v, _ := runPipeline(t, src, "2,0\n0,5\n0,1")
	xs, _ := ir.AsList(v)
	if got := ir.FormatValue(xs); got != "[[0, 1], [0, 5], [2, 0]]" {
		t.Fatalf("find cells = %q", got)
	}
}

func TestSparseBuiltins(t *testing.T) {
	src := pointLines +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: 0\n" +
		"    Mark: 1\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (g) -> list(minrow(g), maxrow(g), mincol(g), maxcol(g), cells(g))\n"
	v, _ := runPipeline(t, src, "-2,7\n4,-3")
	if got := ir.FormatValue(v); got != "[-2, 4, -3, 7, 2]" {
		t.Fatalf("bounds/cells = %q", got)
	}
}

func TestSparsePutHasAndConstructor(t *testing.T) {
	// sparse(d) + put build a grid from nothing inside the expression layer;
	// put is functional (the original is untouched).
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (x) -> put(sparse(0), 3, 4, 7)\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (g) -> if has(g, 0, 0) then -1 else (if has(g, 3, 4) then at(g, 3, 4) else -2)\n"
	v, _ := runPipeline(t, src, "ignored")
	if v.(int64) != 7 {
		t.Fatalf("put/has/at = %v, want 7", v)
	}
}

func TestSparseResolveErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"missing default",
			pointLines + "Channeled Energy: Convert To Sparse Grid\n    Mark: \"#\"\n",
			"requires Default:"},
		{"mark type mismatch",
			pointLines + "Channeled Energy: Convert To Sparse Grid\n    Default: \".\"\n    Mark: 1\n",
			"must match"},
		{"grid default type mismatch",
			digitGrid + "Channeled Energy: Convert To Sparse Grid\n    Default: \"x\"\n",
			"grid cells are Int"},
		{"positional lambda on sparse",
			pointLines + "Channeled Energy: Convert To Sparse Grid\n    Default: 0\n    Mark: 1\n" +
				"Maximum Technique: Count Cells\n    Using: (g, r, c) -> at(g, r, c) = 1\n",
			"positional (grid, row, col) form needs dense bounds"},
		{"bad source type",
			intsPrelude + "Channeled Energy: Convert To Sparse Grid\n    Default: 0\n",
			"expects Grid<T>, Map<(Int, Int), V>, List<(Int, Int)>, or List<List<Int>>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runErr(t, tc.src, "0,0")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestSparseRuntimeErrors(t *testing.T) {
	// Empty sparse grid: bounds builtins are partial.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (x) -> minrow(sparse(0))\n"
	_, err := runErr(t, src, "ignored")
	if err == nil || !strings.Contains(err.Error(), "minrow of an empty sparse grid is undefined") {
		t.Fatalf("empty bounds error = %v", err)
	}

	// Densify guard: far-apart cells exceed ir.MaxSparseDense.
	src = pointLines +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: \".\"\n" +
		"    Mark: \"#\"\n" +
		"Channeled Energy: Convert To Grid\n"
	_, err = runErr(t, src, "0,0\n5000000,5000000")
	if err == nil || !strings.Contains(err.Error(), "too large to densify") {
		t.Fatalf("densify guard error = %v", err)
	}
}

// A sparse grid threads through Iterate Until Fixed Point via DeepEqual.
func TestSparseFixedPoint(t *testing.T) {
	src := pointLines +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: 0\n" +
		"    Mark: 1\n" +
		"Simple Domain: Iterate Until Fixed Point\n" +
		"    Cursed Technique: Apply\n" +
		"        Using: (g) -> if has(g, 0, 3) then g else put(g, 0, mincol(g) + cells(g), 1)\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (g) -> cells(g)\n"
	v, _ := runPipeline(t, src, "0,0")
	// Starts with (0,0); puts (0,1), (0,2), (0,3) then converges: 4 cells.
	if v.(int64) != 4 {
		t.Fatalf("fixed point cells = %v, want 4", v)
	}
}
