package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Sparse grid primitives: the dedicated nested/sparse grid type.
// A Sparse<T> is an unbounded 2D plane with a default value; only written
// cells are stored. See ir/sparse.go for the locked semantics and
// docs/data-model.md for the user-facing contract.

// ---------------------------------------------------------------------------
// Channeled Energy: Convert To Sparse Grid — build a Sparse<T>.
//   Grid<T>             + Default: d          -> Sparse<T> (cells ≠ d are set)
//   Map<(Int, Int), V>  + Default: d          -> Sparse<V> (every entry set)
//   List<(Int, Int)>    + Default: d, Mark: m -> Sparse<T> (each point set to m)
//   List<List<Int>>     + Default: d, Mark: m -> Sparse<T> (rows of two ints,
//                                                the shape Match Pattern gives)
// Default:/Mark: are Int or Text literals; their type fixes (and must match)
// the element type.
// ---------------------------------------------------------------------------

var convertToSparseGrid = &Primitive{
	ID:      "Convert To Sparse Grid",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Convert") && hasWord(op, "Sparse") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		defM, err := literalArg(args, "Default", in, pos)
		if err != nil {
			return nil, err
		}
		defType := defM.Type
		point := ir.Tuple(ir.Int(), ir.Int())
		switch {
		case in != nil && in.Kind == ir.KGrid:
			if !in.Elem.Equal(defType) {
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Convert To Sparse Grid: Default: is %s but the grid cells are %s", defType, in.Elem)}
			}
			return sparseFromGridNode(in, defM, pos), nil
		case in != nil && in.Kind == ir.KMap && in.Key.Equal(point):
			if !in.Elem.Equal(defType) {
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Convert To Sparse Grid: Default: is %s but the map values are %s", defType, in.Elem)}
			}
			return sparseFromMapNode(in, defM, pos), nil
		case in != nil && in.Kind == ir.KList &&
			(in.Elem.Equal(point) || in.Elem.Equal(ir.List(ir.Int()))):
			markM, err := literalArg(args, "Mark", in, pos)
			if err != nil {
				return nil, err
			}
			if !markM.Type.Equal(defType) {
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Convert To Sparse Grid: Mark: is %s but Default: is %s (they must match)", markM.Type, defType)}
			}
			return sparseFromPointsNode(in, defM, markM, defType, pos), nil
		default:
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Convert To Sparse Grid expects Grid<T>, Map<(Int, Int), V>, List<(Int, Int)>, or List<List<Int>> (two per row), got %s", in)}
		}
	},
}

// literalArg reads a required Int or Text argument, returning the runtime value
// and its type. Sparse element types are pinned by it, which is why Sparse<T>
// built by this primitive has T ∈ {Int, Text} (the expression layer's
// sparse(d) builtin can seed any element type).
//
// It is a measured argument: a lambda over the current value works wherever
// the literal does, and answers with the type its body produces — still
// checked against the cells it has to match, so the Int/Text restriction holds
// either way.
func literalArg(args ArgSet, name string, in *ir.Type, pos token.Position) (MeasuredValue, error) {
	m, err := measuredValue(args, "Convert To Sparse Grid", name, in, pos)
	if err != nil {
		return MeasuredValue{}, err
	}
	if !m.Type.Equal(ir.Int()) && !m.Type.Equal(ir.Text()) {
		return MeasuredValue{}, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Convert To Sparse Grid: %s: must be Int or Text, got %s", name, m.Type)}
	}
	return m, nil
}

func sparseFromGridNode(in *ir.Type, defM MeasuredValue, pos token.Position) *ir.Node {
	meta := map[string]any{"source": "grid"}
	defM.Meta(meta, "default")
	return &ir.Node{
		Prim:    "Convert To Sparse Grid",
		In:      in,
		Out:     ir.Sparse(in.Elem),
		Display: "Convert To Sparse Grid",
		Meta:    meta,
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			g, ok := v.(*ir.GridValue)
			if !ok {
				return nil, runtimeErr("Convert To Sparse Grid", pos, "expected Grid, got %s", ir.DescribeValue(v))
			}
			def, err := defM.Resolve(v)
			if err != nil {
				return nil, err
			}
			out := ir.NewSparseValue(def)
			for r := 0; r < g.Rows; r++ {
				for c := 0; c < g.Cols; c++ {
					cell, _ := g.At(r, c)
					if !ir.DeepEqual(cell, def) {
						out.Put(int64(r), int64(c), cell)
					}
				}
			}
			return out, nil
		},
	}
}

func sparseFromMapNode(in *ir.Type, defM MeasuredValue, pos token.Position) *ir.Node {
	meta := map[string]any{"source": "map"}
	defM.Meta(meta, "default")
	return &ir.Node{
		Prim:    "Convert To Sparse Grid",
		In:      in,
		Out:     ir.Sparse(in.Elem),
		Display: "Convert To Sparse Grid",
		Meta:    meta,
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			m, ok := v.(*ir.MapValue)
			if !ok {
				return nil, runtimeErr("Convert To Sparse Grid", pos, "expected Map, got %s", ir.DescribeValue(v))
			}
			def, err := defM.Resolve(v)
			if err != nil {
				return nil, err
			}
			out := ir.NewSparseValue(def)
			for _, k := range m.Keys() {
				pt, ok := k.([]ir.Value)
				if !ok || len(pt) != 2 {
					return nil, runtimeErr("Convert To Sparse Grid", pos, "map key is not a point")
				}
				r, err1 := ir.AsInt(pt[0])
				c, err2 := ir.AsInt(pt[1])
				if err1 != nil || err2 != nil {
					return nil, runtimeErr("Convert To Sparse Grid", pos, "map key is not a point")
				}
				val, _ := m.Get(k)
				out.Put(r, c, val)
			}
			return out, nil
		},
	}
}

func sparseFromPointsNode(in *ir.Type, defM, markM MeasuredValue, elem *ir.Type, pos token.Position) *ir.Node {
	meta := map[string]any{"source": "points"}
	defM.Meta(meta, "default")
	markM.Meta(meta, "mark")
	return &ir.Node{
		Prim:    "Convert To Sparse Grid",
		In:      in,
		Out:     ir.Sparse(elem),
		Display: "Convert To Sparse Grid (points)",
		Meta:    meta,
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsList(v)
			if err != nil {
				return nil, runtimeErr("Convert To Sparse Grid", pos, "%v", err)
			}
			def, err := defM.Resolve(v)
			if err != nil {
				return nil, err
			}
			mark, err := markM.Resolve(v)
			if err != nil {
				return nil, err
			}
			out := ir.NewSparseValue(def)
			for i, x := range xs {
				// Tuples and Int rows share the []Value representation; the
				// row length is only known at runtime for List<Int> rows.
				pt, ok := x.([]ir.Value)
				if !ok || len(pt) != 2 {
					return nil, runtimeErr("Convert To Sparse Grid", pos,
						"item %d is not a point (need exactly two integers)", i)
				}
				r, err1 := ir.AsInt(pt[0])
				c, err2 := ir.AsInt(pt[1])
				if err1 != nil || err2 != nil {
					return nil, runtimeErr("Convert To Sparse Grid", pos, "item %d is not a point", i)
				}
				out.Put(r, c, mark)
			}
			return out, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Convert To Grid over Sparse<T>: densify the bounding box, translated so
// (minrow, mincol) lands at (0, 0), unset cells filled with the default.
// Guarded by ir.MaxSparseDense — two far-apart cells imply a huge box, and a
// clear error beats an OOM. The empty sparse grid densifies to the 0x0 grid.
// ---------------------------------------------------------------------------

func gridFromSparseNode(in *ir.Type, pos token.Position) *ir.Node {
	return &ir.Node{
		Prim:    "Convert To Grid",
		In:      in,
		Out:     ir.Grid(in.Elem),
		Display: "Convert To Grid (densify)",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			sp, ok := v.(*ir.SparseValue)
			if !ok {
				return nil, runtimeErr("Convert To Grid", pos, "expected Sparse, got %s", ir.DescribeValue(v))
			}
			if minR, minC, maxR, maxC, has := sp.Bounds(); has {
				rows, cols := maxR-minR+1, maxC-minC+1
				// Per-side checks first so rows*cols cannot overflow int64.
				if ir.TooLargeToDensify(rows, cols) {
					return nil, runtimeErr("Convert To Grid", pos,
						"sparse grid too large to densify (%dx%d, limit %d cells)", rows, cols, ir.DensifyLimit())
				}
			}
			return sp.ToGrid(), nil
		},
	}
}

// ---------------------------------------------------------------------------
// Map Cells / Count Cells / Find Cells over Sparse<T>: they visit the SET
// cells only, in sorted row-major order. Map Cells additionally maps the
// default through the lambda (the whole infinite plane is transformed, which
// is what makes Sparse<T> -> Sparse<U> well-defined). The positional
// (grid, row, col) lambda form is dense-only: the default has no position.
// ---------------------------------------------------------------------------

func sparseCellLambda(args ArgSet, prim string, pos token.Position) (*ast.Lambda, error) {
	lam, ok := args.Lambda("Using")
	if !ok {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("%s requires a Using: lambda", prim)}
	}
	wantArity := 1 + ambientDepth()
	if len(lam.Params) != wantArity {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s over a Sparse grid takes a %d-parameter lambda (the cell value, plus any enclosing For loop variable(s)); the positional (grid, row, col) form needs dense bounds — Convert To Grid first", prim, wantArity)}
	}
	return lam, nil
}

func mapCellsSparseNode(args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
	lam, err := sparseCellLambda(args, "Map Cells", pos)
	if err != nil {
		return nil, err
	}
	outElem, err := typecheck.LambdaType(lam, append([]*ir.Type{in.Elem}, ambientTypes()...)...)
	if err != nil {
		return nil, &ResolveError{Pos: pos, Msg: "Map Cells: " + err.Error()}
	}
	return &ir.Node{
		Prim:    "Map Cells",
		In:      in,
		Out:     ir.Sparse(outElem),
		Display: "Map Cells",
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			sp, ok := v.(*ir.SparseValue)
			if !ok {
				return nil, runtimeErr("Map Cells", pos, "expected Sparse, got %s", ir.DescribeValue(v))
			}
			newDef, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{in.Elem}, ambientTypes()...), append([]ir.Value{sp.Def}, ambientArgs()...)...)
			if err != nil {
				return nil, runtimeErr("Map Cells", pos, "default: %v", err)
			}
			out := ir.NewSparseValue(newDef)
			for _, p := range sp.Points() {
				r, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{in.Elem}, ambientTypes()...), append([]ir.Value{sp.At(p[0], p[1])}, ambientArgs()...)...)
				if err != nil {
					return nil, runtimeErr("Map Cells", pos, "cell (%d, %d): %v", p[0], p[1], err)
				}
				out.Put(p[0], p[1], r)
			}
			return out, nil
		},
	}, nil
}

func countCellsSparseNode(args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
	lam, err := sparseCellLambda(args, "Count Cells", pos)
	if err != nil {
		return nil, err
	}
	if err := requirePredicate(lam, in.Elem, "Count Cells", pos); err != nil {
		return nil, err
	}
	return &ir.Node{
		Prim:    "Count Cells",
		In:      in,
		Out:     ir.Int(),
		Display: "Count Cells",
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			sp, ok := v.(*ir.SparseValue)
			if !ok {
				return nil, runtimeErr("Count Cells", pos, "expected Sparse, got %s", ir.DescribeValue(v))
			}
			var n int64
			for _, p := range sp.Points() {
				keep, err := evalPredicate(lam, in.Elem, sp.At(p[0], p[1]))
				if err != nil {
					return nil, runtimeErr("Count Cells", pos, "cell (%d, %d): %v", p[0], p[1], err)
				}
				if keep {
					n++
				}
			}
			return n, nil
		},
	}, nil
}

func findCellsSparseNode(args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
	lam, err := requireLambda(args, 1, "Find Cells", pos)
	if err != nil {
		return nil, err
	}
	if err := requirePredicate(lam, in.Elem, "Find Cells", pos); err != nil {
		return nil, err
	}
	return &ir.Node{
		Prim:    "Find Cells",
		In:      in,
		Out:     ir.List(ir.Tuple(ir.Int(), ir.Int())),
		Display: "Find Cells",
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			sp, ok := v.(*ir.SparseValue)
			if !ok {
				return nil, runtimeErr("Find Cells", pos, "expected Sparse, got %s", ir.DescribeValue(v))
			}
			out := []ir.Value{}
			for _, p := range sp.Points() {
				keep, err := evalPredicate(lam, in.Elem, sp.At(p[0], p[1]))
				if err != nil {
					return nil, runtimeErr("Find Cells", pos, "cell (%d, %d): %v", p[0], p[1], err)
				}
				if keep {
					out = append(out, []ir.Value{p[0], p[1]})
				}
			}
			return out, nil
		},
	}, nil
}
