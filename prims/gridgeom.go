// Grid geometry beyond Transpose, plus Find Cycle.
//
// Transpose was the only structural grid transform, so rotation, reflection
// and "give me the rows back" all required a round trip the language could not
// actually express — Convert To Grid was a one-way door.
package prims

import (
	"fmt"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// ---------------------------------------------------------------------------
// Cursed Technique: Rotate Grid — Grid<T> -> Grid<T>.
// ---------------------------------------------------------------------------

var rotateGrid = &Primitive{
	ID:      "Rotate Grid",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Rotate") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Rotate Grid expects a Grid, got %s", in)}
		}
		mode, _ := args.Ident("Mode")
		if mode == "" {
			mode = "Right"
		}
		switch mode {
		case "Right", "Left", "Half":
		default:
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Rotate Grid: unknown Mode %q (Right, Left, Half)", mode)}
		}
		return &ir.Node{
			Prim: "Rotate Grid", In: in, Out: in,
			Display: "Rotate Grid, " + mode,
			Meta:    map[string]any{"mode": mode}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Rotate Grid", pos, "expected a Grid, got %s", ir.DescribeValue(v))
				}
				return rotateGridValue(g, mode), nil
			},
		}, nil
	},
}

// rotateGridValue rotates in grid coordinates: Right sends (r, c) to
// (c, rows-1-r), which is the clockwise quarter turn you get by reading the
// first column bottom-to-top as the new first row.
func rotateGridValue(g *ir.GridValue, mode string) *ir.GridValue {
	rows, cols := g.Rows, g.Cols
	switch mode {
	case "Half":
		out := ir.NewGridValue(rows, cols)
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				v, _ := g.At(r, c)
				out.SetAt(rows-1-r, cols-1-c, v)
			}
		}
		return out
	case "Left":
		out := ir.NewGridValue(cols, rows)
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				v, _ := g.At(r, c)
				out.SetAt(cols-1-c, r, v)
			}
		}
		return out
	default: // Right
		out := ir.NewGridValue(cols, rows)
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				v, _ := g.At(r, c)
				out.SetAt(c, rows-1-r, v)
			}
		}
		return out
	}
}

// ---------------------------------------------------------------------------
// Cursed Technique: Flip Grid — Grid<T> -> Grid<T>.
// ---------------------------------------------------------------------------

var flipGrid = &Primitive{
	ID:      "Flip Grid",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Flip") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Flip Grid expects a Grid, got %s", in)}
		}
		mode, _ := args.Ident("Mode")
		if mode == "" {
			mode = "Horizontal"
		}
		if mode != "Horizontal" && mode != "Vertical" {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Flip Grid: unknown Mode %q (Horizontal, Vertical)", mode)}
		}
		return &ir.Node{
			Prim: "Flip Grid", In: in, Out: in,
			Display: "Flip Grid, " + mode,
			Meta:    map[string]any{"mode": mode}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Flip Grid", pos, "expected a Grid, got %s", ir.DescribeValue(v))
				}
				out := ir.NewGridValue(g.Rows, g.Cols)
				for r := 0; r < g.Rows; r++ {
					for c := 0; c < g.Cols; c++ {
						// Horizontal mirrors left-right (columns reverse);
						// Vertical mirrors top-bottom (rows reverse).
						cell, _ := g.At(r, c)
						if mode == "Horizontal" {
							out.SetAt(r, g.Cols-1-c, cell)
						} else {
							out.SetAt(g.Rows-1-r, c, cell)
						}
					}
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Channeled Energy: Convert To Rows — Grid<T> -> List<List<T>>.
// The inverse of Convert To Grid, which was otherwise a one-way door.
// ---------------------------------------------------------------------------

var convertToRows = &Primitive{
	ID:      "Convert To Rows",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Rows") && hasWord(op, "Convert") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Convert To Rows expects a Grid, got %s", in)}
		}
		out := ir.List(ir.List(in.Elem))
		return &ir.Node{
			Prim: "Convert To Rows", In: in, Out: out,
			Display: "Convert To Rows", Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Convert To Rows", pos, "expected a Grid, got %s", ir.DescribeValue(v))
				}
				rows := make([]ir.Value, g.Rows)
				for r := 0; r < g.Rows; r++ {
					row := make([]ir.Value, g.Cols)
					for c := 0; c < g.Cols; c++ {
						cell, _ := g.At(r, c)
						row[c] = cell
					}
					rows[r] = row
				}
				return rows, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Find Cycle — List<T> -> (Int, Int) (T keyable).
//
// Iterate produces a trajectory precisely so a program can ask "have I been
// here before?" — but the asking had no primitive: Find Index needs a
// predicate over one element, not a seen-set over the prefix. The answer is
// what turns "run this 1,000,000,000 times" into arithmetic.
// ---------------------------------------------------------------------------

var findCycle = &Primitive{
	ID:      "Find Cycle",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Find") && hasWord(op, "Cycle") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Find Cycle", pos)
		if err != nil {
			return nil, err
		}
		if !ir.Keyable(elem) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Find Cycle needs keyable elements (Int, Text, or a Tuple/Record of them), got %s", elem)}
		}
		return &ir.Node{
			Prim: "Find Cycle", In: in, Out: ir.Tuple(ir.Int(), ir.Int()),
			Display: "Find Cycle", Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Find Cycle", pos, "%v", err)
				}
				first := map[any]int64{}
				for i, e := range xs {
					k := ir.KeyOf(e)
					if at, seen := first[k]; seen {
						// (index where the repeated value first appeared,
						// period) — everything before `at` is the tail.
						return []ir.Value{at, int64(i) - at}, nil
					}
					first[k] = int64(i)
				}
				// No repeat: (-1, 0), the same "not there" sentinel Find Index
				// uses, rather than an error — a trajectory that never repeats
				// is a legitimate answer.
				return []ir.Value{int64(-1), int64(0)}, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Range — -> List<Int>.
//
// `challenges/01_fizzbuzz.domain` built 1..15 by hand with a Repeat loop
// appending length+1, and said so in its header: "Domain has no range
// generator". `For x in range(N)` already existed as loop-header syntax, so
// the concept was in the grammar but unreachable as a value.
//
// Half-open, and deliberately: `range(N)` in a For header yields 0..N-1, and
// two different meanings of "range" in one language would be worse than the
// occasional `Range 1 16`. It matches `slice`/`take`/`drop` too.
// ---------------------------------------------------------------------------

var rangePrim = &Primitive{
	ID:      "Range",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Range") && !hasWord(op, "Merge") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		// One phrase int is the high bound (`Range 10`), two are low and high.
		// Measured, they are named: `Low:` and `High:`. Range replaces the
		// current value rather than transforming it, but the measuring lambda
		// still sees what flowed in — which is the whole point of
		// `High: (xs) -> length(xs)`, unwritable in any literal spelling.
		loSlot, hiSlot := -1, 0
		if len(op.Ints) > 1 {
			loSlot, hiSlot = 0, 1
		}
		loM, hasLo, err := measuredInt(op, args, "Range", "Low", loSlot, NoBound, in, pos)
		if err != nil {
			return nil, err
		}
		if !hasLo {
			loM = Measured{Lit: 0, Min: NoBound, Prim: "Range", Name: "Low", Pos: pos}
		}
		hiM, err := requireMeasuredInt(op, args, "Range", "High", hiSlot, NoBound, in, pos,
			"one or two Int bounds", "Range 10 (0..9) or Range 1 16 (1..15)")
		if err != nil {
			return nil, err
		}
		if !loM.IsMeasured() && !hiM.IsMeasured() && hiM.Lit < loM.Lit {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Range bounds are half-open [%d, %d), so the high bound may not be below the low one",
				loM.Lit, hiM.Lit)}
		}
		display := fmt.Sprintf("Range %s..%s", loM.Describe(), hiM.Describe())
		if !hiM.IsMeasured() {
			display = fmt.Sprintf("Range %s..%d", loM.Describe(), hiM.Lit-1)
		}
		meta := map[string]any{}
		loM.Meta(meta, "lo")
		hiM.Meta(meta, "hi")
		out := ir.List(ir.Int())
		return &ir.Node{
			// Replaces the current value rather than transforming it — like
			// Combine and Zip, which also ignore the main pipeline value.
			Prim: "Range", In: in, Out: out,
			Display: display,
			Meta:    meta, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				lo, err := loM.Resolve(v)
				if err != nil {
					return nil, err
				}
				hi, err := hiM.Resolve(v)
				if err != nil {
					return nil, err
				}
				if hi < lo {
					return nil, runtimeErr("Range", pos,
						"bounds are half-open [%d, %d), so the high bound may not be below the low one",
						lo, hi)
				}
				xs := make([]ir.Value, 0, hi-lo)
				for n := lo; n < hi; n++ {
					xs = append(xs, n)
				}
				return xs, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Subgrid — Grid<T> -> Grid<T>, a rectangular crop.
// ---------------------------------------------------------------------------

var subgrid = &Primitive{
	ID:      "Subgrid",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Subgrid") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Subgrid expects a Grid, got %s", in)}
		}
		if len(op.Ints) != 4 && len(op.Ints) != 0 {
			return nil, &ResolveError{Pos: pos,
				Msg: "Subgrid needs four Ints: Subgrid ROW COL HEIGHT WIDTH"}
		}
		var ms [4]Measured
		for i, name := range [4]string{"Row", "Col", "Height", "Width"} {
			m, ok, err := measuredInt(op, args, "Subgrid", name, i, NoBound, in, pos)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, &ResolveError{Pos: pos,
					Msg: "Subgrid needs four Ints: Subgrid ROW COL HEIGHT WIDTH (or Row:/Col:/Height:/Width:)"}
			}
			ms[i] = m
		}
		rowM, colM, heightM, widthM := ms[0], ms[1], ms[2], ms[3]
		for _, m := range []Measured{heightM, widthM} {
			if !m.IsMeasured() && m.Lit < 0 {
				return nil, &ResolveError{Pos: pos, Msg: "Subgrid height and width must be >= 0"}
			}
		}
		meta := map[string]any{}
		for i, key := range [4]string{"row", "col", "height", "width"} {
			ms[i].Meta(meta, key)
		}
		return &ir.Node{
			Prim: "Subgrid", In: in, Out: in,
			Display: fmt.Sprintf("Subgrid %s %s %s %s",
				rowM.Describe(), colM.Describe(), heightM.Describe(), widthM.Describe()),
			Meta: meta, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Subgrid", pos, "expected a Grid, got %s", ir.DescribeValue(v))
				}
				r0, err := rowM.Resolve(v)
				if err != nil {
					return nil, err
				}
				c0, err := colM.Resolve(v)
				if err != nil {
					return nil, err
				}
				h, err := heightM.Resolve(v)
				if err != nil {
					return nil, err
				}
				w, err := widthM.Resolve(v)
				if err != nil {
					return nil, err
				}
				if h < 0 || w < 0 {
					return nil, runtimeErr("Subgrid", pos,
						"height and width must be >= 0, measured %dx%d", h, w)
				}
				// Out of bounds is an error rather than a clamp: a crop that
				// silently returned fewer rows than asked for would give a
				// wrong answer that looks right.
				if r0 < 0 || c0 < 0 || r0+h > int64(g.Rows) || c0+w > int64(g.Cols) {
					return nil, runtimeErr("Subgrid", pos,
						"crop (%d, %d) %dx%d does not fit a %dx%d grid", r0, c0, h, w, g.Rows, g.Cols)
				}
				out := ir.NewGridValue(int(h), int(w))
				for r := int64(0); r < h; r++ {
					for c := int64(0); c < w; c++ {
						cell, _ := g.At(int(r0+r), int(c0+c))
						out.SetAt(int(r), int(c), cell)
					}
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Pad Grid — Grid<T> -> Grid<T>, a border of Fill:.
//
// The standard move before a flood fill: a one-cell border lets the fill
// reach every outside cell without special-casing the edges.
// ---------------------------------------------------------------------------

var padGrid = &Primitive{
	ID:      "Pad Grid",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Pad") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Pad Grid expects a Grid, got %s", in)}
		}
		nM, ok, err := measuredInt(op, args, "Pad Grid", "Thickness", 0, 0, in, pos)
		if err != nil {
			return nil, err
		}
		if !ok {
			nM = Measured{Lit: 1, Min: 0, Prim: "Pad Grid", Name: "Thickness", Pos: pos}
		}
		if !nM.IsMeasured() && nM.Lit < 0 {
			return nil, &ResolveError{Pos: pos, Msg: "Pad Grid width must be >= 0"}
		}
		// The fill must match the element type, so it is spelled the way the
		// Sparse default is: an Int or Text literal — or a lambda over the grid
		// that produces one.
		fillM, err := measuredValue(args, "Pad Grid", "Fill", in, pos)
		if err != nil {
			return nil, err
		}
		if !fillM.Type.Equal(in.Elem) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Pad Grid: Fill: is %s but the grid holds %s", fillM.Type, in.Elem)}
		}
		meta := map[string]any{}
		nM.Meta(meta, "n")
		fillM.Meta(meta, "fill")
		return &ir.Node{
			Prim: "Pad Grid", In: in, Out: in,
			Display: "Pad Grid " + nM.Describe(),
			Meta:    meta, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Pad Grid", pos, "expected a Grid, got %s", ir.DescribeValue(v))
				}
				fill, err := fillM.Resolve(v)
				if err != nil {
					return nil, err
				}
				n, err := nM.Resolve(v)
				if err != nil {
					return nil, err
				}
				out := ir.NewGridValue(g.Rows+int(2*n), g.Cols+int(2*n))
				for i := range out.Cells {
					out.Cells[i] = fill
				}
				for r := 0; r < g.Rows; r++ {
					for c := 0; c < g.Cols; c++ {
						cell, _ := g.At(r, c)
						out.SetAt(r+int(n), c+int(n), cell)
					}
				}
				return out, nil
			},
		}, nil
	},
}
