package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Grid primitives (M5), built on ir.GridValue. The grid value type and its
// access/neighbor helpers were added in M1; this file wires them into the
// pipeline vocabulary.

// ---------------------------------------------------------------------------
// Channeled Energy: Convert To Grid — build a Grid<T>.
//   List<List<T>> -> Grid<T>   (each inner list a row; must be rectangular)
//   List<Text>    -> Grid<Text> (each char a cell; rows must be equal length)
// ---------------------------------------------------------------------------

var convertToGrid = &Primitive{
	ID:      "Convert To Grid",
	Keyword: "Channeled Energy",
	// "Sparse" is excluded so Convert To Sparse Grid (which also names Grid)
	// never falls through to this matcher, regardless of registry order.
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Convert") && hasWord(op, "Grid") && !hasWord(op, "Sparse")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		switch {
		case in != nil && in.Kind == ir.KSparse:
			return gridFromSparseNode(in, pos), nil
		case in != nil && in.Kind == ir.KList && in.Elem != nil && in.Elem.Kind == ir.KList:
			elem := in.Elem.Elem
			return gridFromRowsNode(in, elem, pos), nil
		case in.Equal(ir.List(ir.Text())):
			return gridFromTextNode(in, pos), nil
		default:
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Convert To Grid expects List<List<T>>, List<Text>, or Sparse<T>, got %s", in)}
		}
	},
}

func gridFromRowsNode(in, elem *ir.Type, pos token.Position) *ir.Node {
	return &ir.Node{
		Prim:    "Convert To Grid",
		In:      in,
		Out:     ir.Grid(elem),
		Display: "Convert To Grid",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			rows, err := ir.AsList(v)
			if err != nil {
				return nil, runtimeErr("Convert To Grid", pos, "%v", err)
			}
			return buildGrid(rows, pos, func(cell ir.Value) (ir.Value, error) { return cell, nil })
		},
	}
}

func gridFromTextNode(in *ir.Type, pos token.Position) *ir.Node {
	return &ir.Node{
		Prim:    "Convert To Grid",
		In:      in,
		Out:     ir.Grid(ir.Text()),
		Display: "Convert To Grid (chars)",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			lines, err := ir.AsList(v)
			if err != nil {
				return nil, runtimeErr("Convert To Grid", pos, "%v", err)
			}
			rows := make([]ir.Value, len(lines))
			for i, line := range lines {
				s, ok := line.(string)
				if !ok {
					return nil, runtimeErr("Convert To Grid", pos, "row %d is not Text", i)
				}
				cells := make([]ir.Value, 0, len(s))
				for _, r := range s {
					cells = append(cells, string(r))
				}
				rows[i] = cells
			}
			return buildGrid(rows, pos, func(cell ir.Value) (ir.Value, error) { return cell, nil })
		},
	}
}

// buildGrid assembles a rectangular GridValue from a list of row lists.
func buildGrid(rows []ir.Value, pos token.Position, conv func(ir.Value) (ir.Value, error)) (ir.Value, error) {
	if len(rows) == 0 {
		return ir.NewGridValue(0, 0), nil
	}
	first, err := ir.AsList(rows[0])
	if err != nil {
		return nil, runtimeErr("Convert To Grid", pos, "row 0: %v", err)
	}
	cols := len(first)
	g := ir.NewGridValue(len(rows), cols)
	for r, rowVal := range rows {
		cells, err := ir.AsList(rowVal)
		if err != nil {
			return nil, runtimeErr("Convert To Grid", pos, "row %d: %v", r, err)
		}
		if len(cells) != cols {
			return nil, runtimeErr("Convert To Grid", pos,
				"grid is not rectangular: row %d has %d cells, expected %d", r, len(cells), cols)
		}
		for c, cell := range cells {
			cv, err := conv(cell)
			if err != nil {
				return nil, err
			}
			g.SetAt(r, c, cv)
		}
	}
	return g, nil
}

// ---------------------------------------------------------------------------
// Cursed Technique: Map Cells — Grid<T> x (T -> U) -> Grid<U>.
// ---------------------------------------------------------------------------

// cellLambda fetches a grid primitive's Using: lambda and reports its form:
// arity 1 binds the cell value; arity 3 is the positional form binding
// (grid, row, col) so the body can look around with at/row/col/inbounds.
func cellLambda(args ArgSet, prim string, pos token.Position) (*ast.Lambda, bool, error) {
	lam, ok := args.Lambda("Using")
	if !ok {
		return nil, false, &ResolveError{Pos: pos, Msg: fmt.Sprintf("%s requires a Using: lambda", prim), NeedsBlock: true}
	}
	depth := ambientDepth()
	switch len(lam.Params) {
	case 1 + depth:
		return lam, false, nil
	case 3 + depth:
		return lam, true, nil
	}
	return nil, false, &ResolveError{Pos: pos,
		Msg: fmt.Sprintf("%s lambda must take %d parameter (the cell) or %d (grid, row, col), got %d",
			prim, 1+depth, 3+depth, len(lam.Params))}
}

var mapCells = &Primitive{
	ID:      "Map Cells",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Map") && hasWord(op, "Cells") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in != nil && in.Kind == ir.KSparse {
			return mapCellsSparseNode(args, in, pos)
		}
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Map Cells expects a Grid or Sparse, got %s", in)}
		}
		lam, positional, err := cellLambda(args, "Map Cells", pos)
		if err != nil {
			return nil, err
		}
		var outElem *ir.Type
		if positional {
			outElem, err = typecheck.LambdaType(lam, append([]*ir.Type{in, ir.Int(), ir.Int()}, ambientTypes()...)...)
		} else {
			outElem, err = typecheck.LambdaType(lam, append([]*ir.Type{in.Elem}, ambientTypes()...)...)
		}
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Map Cells: " + err.Error()}
		}
		return &ir.Node{
			Prim:    "Map Cells",
			In:      in,
			Out:     ir.Grid(outElem),
			Display: "Map Cells",
			Meta:    map[string]any{"lambda": lam, "positional": positional},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Map Cells", pos, "expected Grid, got %s", ir.DescribeValue(v))
				}
				out := ir.NewGridValue(g.Rows, g.Cols)
				for i, cell := range g.Cells {
					var r ir.Value
					var err error
					if positional {
						r, err = eval.EvalLambdaTyped(lam, append([]*ir.Type{in, ir.Int(), ir.Int()}, ambientTypes()...),
							append([]ir.Value{g, int64(i / g.Cols), int64(i % g.Cols)}, ambientArgs()...)...)
					} else {
						r, err = eval.EvalLambdaTyped(lam, append([]*ir.Type{in.Elem}, ambientTypes()...), append([]ir.Value{cell}, ambientArgs()...)...)
					}
					if err != nil {
						return nil, runtimeErr("Map Cells", pos, "cell %d: %v", i, err)
					}
					out.Cells[i] = r
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Find Cells — Grid<T> x (T -> Bool) -> List<(Int, Int)>:
// the (row, col) positions of every cell satisfying the predicate, in
// row-major order. The positions are points — (Int, Int) tuples — ready for
// the point builtins (prow/pcol/manhattan/...).
// ---------------------------------------------------------------------------

var findCells = &Primitive{
	ID:      "Find Cells",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Find") && hasWord(op, "Cells") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in != nil && in.Kind == ir.KSparse {
			return findCellsSparseNode(args, in, pos)
		}
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Find Cells expects a Grid or Sparse, got %s", in)}
		}
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
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Find Cells", pos, "expected Grid, got %s", ir.DescribeValue(v))
				}
				out := []ir.Value{}
				for r := range g.Rows {
					for c := range g.Cols {
						cell, _ := g.At(r, c)
						keep, err := evalPredicate(lam, in.Elem, cell)
						if err != nil {
							return nil, runtimeErr("Find Cells", pos, "cell (%d, %d): %v", r, c, err)
						}
						if keep {
							out = append(out, []ir.Value{int64(r), int64(c)})
						}
					}
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Transpose — Grid<T> -> Grid<T>, or List<List<T>> ->
// List<List<T>> (swap rows and columns).
//
// The list-of-lists shape is what Extract Integers, Split Fields and a
// positional Match Pattern all produce, so requiring a Grid meant a column-wise
// question had to detour through Convert To Grid — which additionally demands
// one element type across the whole thing, a constraint transposition does not
// need. Both shapes error on a ragged row rather than truncating, with the
// wording Convert To Grid uses.
// ---------------------------------------------------------------------------

var transpose = &Primitive{
	ID:      "Transpose",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Transpose") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || (in.Kind != ir.KGrid && !isListOfLists(in)) {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Transpose expects a Grid or a List<List<T>>, got %s", in)}
		}
		evalFn := transposeGrid(pos)
		if in.Kind != ir.KGrid {
			evalFn = transposeRows(pos)
		}
		return &ir.Node{
			Prim:    "Transpose",
			In:      in,
			Out:     in,
			Display: "Transpose",
			Pos:     pos,
			Eval:    evalFn,
		}, nil
	},
}

// isListOfLists reports the List<List<T>> shape Transpose also accepts.
func isListOfLists(t *ir.Type) bool {
	return t != nil && t.Kind == ir.KList && t.Elem != nil && t.Elem.Kind == ir.KList
}

func transposeGrid(pos token.Position) func(*ir.Context, ir.Value) (ir.Value, error) {
	return func(_ *ir.Context, v ir.Value) (ir.Value, error) {
		g, ok := v.(*ir.GridValue)
		if !ok {
			return nil, runtimeErr("Transpose", pos, "expected Grid, got %s", ir.DescribeValue(v))
		}
		out := ir.NewGridValue(g.Cols, g.Rows)
		for r := range g.Rows {
			for c := range g.Cols {
				cell, _ := g.At(r, c)
				out.SetAt(c, r, cell)
			}
		}
		return out, nil
	}
}

func transposeRows(pos token.Position) func(*ir.Context, ir.Value) (ir.Value, error) {
	return func(_ *ir.Context, v ir.Value) (ir.Value, error) {
		rows, err := ir.AsList(v)
		if err != nil {
			return nil, runtimeErr("Transpose", pos, "%v", err)
		}
		cells := make([][]ir.Value, len(rows))
		cols := 0
		for r, rowVal := range rows {
			if cells[r], err = ir.AsList(rowVal); err != nil {
				return nil, runtimeErr("Transpose", pos, "row %d: %v", r, err)
			}
			if r == 0 {
				cols = len(cells[0])
			} else if len(cells[r]) != cols {
				return nil, runtimeErr("Transpose", pos,
					"grid is not rectangular: row %d has %d cells, expected %d", r, len(cells[r]), cols)
			}
		}
		out := make([]ir.Value, cols)
		for c := range cols {
			col := make([]ir.Value, len(cells))
			for r := range cells {
				col[r] = cells[r][c]
			}
			out[c] = col
		}
		return out, nil
	}
}

// ---------------------------------------------------------------------------
// Maximum Technique: Count Cells — Grid<T> x (T -> Bool) -> Int.
// ---------------------------------------------------------------------------

var countCells = &Primitive{
	ID:      "Count Cells",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Count") && hasWord(op, "Cells") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in != nil && in.Kind == ir.KSparse {
			return countCellsSparseNode(args, in, pos)
		}
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Count Cells expects a Grid or Sparse, got %s", in)}
		}
		lam, positional, err := cellLambda(args, "Count Cells", pos)
		if err != nil {
			return nil, err
		}
		if positional {
			bodyType, err := typecheck.LambdaType(lam, append([]*ir.Type{in, ir.Int(), ir.Int()}, ambientTypes()...)...)
			if err != nil {
				return nil, &ResolveError{Pos: pos, Msg: "Count Cells: " + err.Error()}
			}
			if !bodyType.Equal(ir.Bool()) {
				return nil, predicateBoolErr("Count Cells", bodyType, pos)
			}
		} else if err := requirePredicate(lam, in.Elem, "Count Cells", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Count Cells",
			In:      in,
			Out:     ir.Int(),
			Display: "Count Cells",
			Meta:    map[string]any{"lambda": lam, "positional": positional},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Count Cells", pos, "expected Grid, got %s", ir.DescribeValue(v))
				}
				var n int64
				for i, cell := range g.Cells {
					var keep bool
					var err error
					if positional {
						var r ir.Value
						r, err = eval.EvalLambdaTyped(lam, append([]*ir.Type{in, ir.Int(), ir.Int()}, ambientTypes()...),
							append([]ir.Value{g, int64(i / g.Cols), int64(i % g.Cols)}, ambientArgs()...)...)
						if err == nil {
							b, ok := r.(bool)
							if !ok {
								err = fmt.Errorf("predicate did not return a Bool (got %s)", ir.DescribeValue(r))
							} else {
								keep = b
							}
						}
					} else {
						keep, err = evalPredicate(lam, in.Elem, cell)
					}
					if err != nil {
						return nil, runtimeErr("Count Cells", pos, "cell %d: %v", i, err)
					}
					if keep {
						n++
					}
				}
				return n, nil
			},
		}, nil
	},
}
