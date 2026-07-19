package prims

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"domain/ir"
)

// digitGrid builds the prelude that turns digit lines into a Grid<Int>.
const digitGrid = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\"\n" +
	"Cursed Technique: Split Each by \"\"\n" +
	"Channeled Energy: Convert Each List to Integers\n" +
	"Channeled Energy: Convert To Grid\n"

func TestConvertToGridAndCountCells(t *testing.T) {
	src := digitGrid +
		"Maximum Technique: Count Cells\n" +
		"    Using: (h) -> h >= 5\n"
	v, _ := runPipeline(t, src, "30373\n25512\n65332\n33549\n35390")
	if v.(int64) != 9 {
		t.Fatalf("count tall trees: got %v want 9", v)
	}
}

func TestConvertToGridCharsAndPredicate(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Grid\n" +
		"Maximum Technique: Count Cells\n" +
		"    Using: (c) -> c = \"X\"\n"
	v, _ := runPipeline(t, src, "XOX\nOXO")
	if v.(int64) != 3 {
		t.Fatalf("count X cells: got %v want 3", v)
	}
}

func TestTranspose(t *testing.T) {
	src := digitGrid + "Cursed Technique: Transpose\n"
	v, _ := runPipeline(t, src, "12\n34\n56") // 3 rows x 2 cols
	g, ok := v.(*ir.GridValue)
	if !ok {
		t.Fatalf("expected Grid, got %T", v)
	}
	if g.Rows != 2 || g.Cols != 3 {
		t.Fatalf("transposed dims: got %dx%d want 2x3", g.Rows, g.Cols)
	}
	// Original (1,0)=3 becomes transposed (0,1).
	if cell, _ := g.At(0, 1); cell.(int64) != 3 {
		t.Fatalf("transposed cell (0,1): got %v want 3", cell)
	}
}

func TestMapCells(t *testing.T) {
	src := digitGrid +
		"Cursed Technique: Map Cells\n" +
		"    Using: (h) -> h * 2\n" +
		"Maximum Technique: Count Cells\n" +
		"    Using: (h) -> h >= 4\n"
	v, _ := runPipeline(t, src, "12\n34") // -> [[2,4],[6,8]], cells >=4: 4,6,8
	if v.(int64) != 3 {
		t.Fatalf("map cells then count: got %v want 3", v)
	}
}

func TestConvertToGridNotRectangular(t *testing.T) {
	src := digitGrid // requires rectangular rows
	_, err := runErr(t, src+"Reveal: stdout\n", "12\n345")
	if err == nil || !strings.Contains(err.Error(), "not rectangular") {
		t.Fatalf("expected non-rectangular error, got %v", err)
	}
}

// TestTransposeTwiceIsIdentity is a property test: Transpose∘Transpose ==
// identity, over many random rectangular grids.
func TestTransposeTwiceIsIdentity(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for iter := 0; iter < 100; iter++ {
		rows := rng.Intn(6) + 1
		cols := rng.Intn(6) + 1
		lines := make([]string, rows)
		for r := 0; r < rows; r++ {
			digits := make([]byte, cols)
			for c := 0; c < cols; c++ {
				digits[c] = byte('0' + rng.Intn(10))
			}
			lines[r] = string(digits)
		}
		src := digitGrid + "Cursed Technique: Transpose\nCursed Technique: Transpose\n"
		v, _ := runPipeline(t, src, strings.Join(lines, "\n"))
		g, ok := v.(*ir.GridValue)
		if !ok {
			t.Fatalf("iter %d: expected Grid, got %T", iter, v)
		}
		if g.Rows != rows || g.Cols != cols {
			t.Fatalf("iter %d: dims got %dx%d want %dx%d", iter, g.Rows, g.Cols, rows, cols)
		}
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				cell, _ := g.At(r, c)
				want, _ := strconv.ParseInt(string(lines[r][c]), 10, 64)
				if cell.(int64) != want {
					t.Fatalf("iter %d: cell (%d,%d) got %v want %d", iter, r, c, cell, want)
				}
			}
		}
	}
}

func TestTransposeSquareAndNonSquare(t *testing.T) {
	// Non-square: transposing once must actually swap dimensions.
	src := digitGrid + "Cursed Technique: Transpose\n"
	v, _ := runPipeline(t, src, "123\n456") // 2x3 -> 3x2
	g := v.(*ir.GridValue)
	if g.Rows != 3 || g.Cols != 2 {
		t.Fatalf("got %dx%d want 3x2", g.Rows, g.Cols)
	}
}

func TestGridResolveErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"count cells on non-grid",
			"Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nMaximum Technique: Count Cells\n    Using: (c) -> c = \"x\"\n",
			"Count Cells expects a Grid",
		},
		{
			"transpose on non-grid",
			"Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nCursed Technique: Transpose\n",
			"Transpose expects a Grid",
		},
	}
	for _, c := range cases {
		_, err := resolveSrc(t, c.src)
		if err == nil {
			t.Fatalf("%s: expected resolve error", c.name)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error %q does not contain %q", c.name, err.Error(), c.want)
		}
	}
}
