package prims

import (
	"fmt"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Graph-search Domain Expansions over grids. Like Sort and All Pairs, these
// name an *algorithm request*: the optimizer owes the result, not the literal
// method. All of them use 4-connectivity (orthogonal neighbors).
//
//	Domain Expansion: BFS from 0 0            # Grid<T> -> Grid<Int> distances
//	    Using: (c) -> c = "."                 # which cells are walkable
//	Domain Expansion: Dijkstra from 0 0       # Grid<Int> -> Grid<Int> distances
//	Domain Expansion: Flood Fill from 0 0     # Grid<T> -> Grid<Int> 0/1 mask
//	    Using: (c) -> c = "#"                 # which cells belong to regions
//	Domain Expansion: Connected Components    # Grid<T> -> Int
//	    Using: (c) -> c = "#"
//
// Internally BFS runs on an ir.Queue, Flood Fill on an ir.Stack, Dijkstra on
// an ir.PQ (min-heap), and Connected Components on an ir.UnionFind.

// startCoords reads the required "from R C" coordinates of a search phrase.
func startCoords(op *ast.Operation, prim string, pos token.Position) (int64, int64, error) {
	if len(op.Ints) < 2 {
		return 0, 0, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("%s requires start coordinates, e.g. %s from 0 0", prim, prim)}
	}
	return op.Ints[0], op.Ints[1], nil
}

// walkableMask evaluates a cell predicate over every cell of a grid; elem is
// the statically inferred cell type (nil when unknown).
func walkableMask(g *ir.GridValue, lam *ast.Lambda, elem *ir.Type, prim string, pos token.Position) ([]bool, error) {
	mask := make([]bool, len(g.Cells))
	for i, cell := range g.Cells {
		ok, err := evalPredicate(lam, elem, cell)
		if err != nil {
			return nil, runtimeErr(prim, pos, "cell %d: %v", i, err)
		}
		mask[i] = ok
	}
	return mask, nil
}

// checkStart validates the start cell of a grid search against the mask.
func checkStart(g *ir.GridValue, r, c int64, mask []bool, prim, role string, pos token.Position) error {
	if !g.InBounds(int(r), int(c)) {
		return runtimeErr(prim, pos, "start (%d, %d) is out of bounds (grid %dx%d)",
			r, c, g.Rows, g.Cols)
	}
	if mask != nil && !mask[int(r)*g.Cols+int(c)] {
		return runtimeErr(prim, pos, "start (%d, %d) is not %s", r, c, role)
	}
	return nil
}

// connectivity reads the optional `Mode: 4 | 8` argument shared by the grid
// searches. 4 (orthogonal) is the default and what every one of them used to
// hard-code; 8 adds the diagonals. It is a per-call choice rather than a
// property of the grid, matching how the neighbor builtins already work.
func connectivity(args ArgSet, prim string, pos token.Position) (bool, error) {
	m, ok := args.Ident("Mode")
	if !ok {
		if n, isInt := args.Int("Mode"); isInt {
			switch n {
			case 4:
				return false, nil
			case 8:
				return true, nil
			}
		} else {
			return false, nil
		}
		return false, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("%s: Mode must be 4 or 8", prim)}
	}
	switch m {
	case "4":
		return false, nil
	case "8":
		return true, nil
	}
	return false, &ResolveError{Pos: pos, Msg: fmt.Sprintf("%s: unknown Mode %q (4 or 8)", prim, m)}
}

// distGrid builds a Grid<Int> initialized to -1 (unreached).
func distGrid(rows, cols int) *ir.GridValue {
	out := ir.NewGridValue(rows, cols)
	for i := range out.Cells {
		out.Cells[i] = int64(-1)
	}
	return out
}

// ---------------------------------------------------------------------------
// BFS — Grid<T> x walkable predicate -> Grid<Int> of step distances from the
// start (-1 where unreachable or unwalkable).
// ---------------------------------------------------------------------------

var bfs = &Primitive{
	ID:      "BFS",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "BFS") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("BFS expects a Grid, got %s", in)}
		}
		r, c, err := startCoords(op, "BFS", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "BFS", pos)
		if err != nil {
			return nil, err
		}
		if err := requirePredicate(lam, in.Elem, "BFS", pos); err != nil {
			return nil, err
		}
		diagonal, err := connectivity(args, "BFS", pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:      "BFS",
			In:        in,
			Out:       ir.Grid(ir.Int()),
			Display:   fmt.Sprintf("BFS from (%d, %d)", r, c),
			Swappable: true,
			Meta:      map[string]any{"row": r, "col": c, "lambda": lam, "diagonal": diagonal},
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("BFS", pos, "expected Grid, got %s", ir.DescribeValue(v))
				}
				mask, err := walkableMask(g, lam, in.Elem, "BFS", pos)
				if err != nil {
					return nil, err
				}
				if err := checkStart(g, r, c, mask, "BFS", "walkable", pos); err != nil {
					return nil, err
				}
				out := distGrid(g.Rows, g.Cols)
				var q ir.Queue[[2]int]
				out.SetAt(int(r), int(c), int64(0))
				q.Push([2]int{int(r), int(c)})
				for {
					cur, ok := q.Pop()
					if !ok {
						break
					}
					d, _ := out.At(cur[0], cur[1])
					for _, nb := range g.Neighbors(cur[0], cur[1], diagonal) {
						i := nb[0]*g.Cols + nb[1]
						if !mask[i] || out.Cells[i] != int64(-1) {
							continue
						}
						out.Cells[i] = d.(int64) + 1
						q.Push(nb)
					}
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Dijkstra — Grid<Int> -> Grid<Int>: minimum total cost from the start to
// every cell, where a step *into* a cell costs that cell's value (the AoC
// risk-map convention; the start's own value is not paid). -1 = unreachable
// (only possible in a 0-cell grid's neighbors — all Int cells are enterable —
// but kept for symmetry with BFS). Negative cell costs are refused.
// ---------------------------------------------------------------------------

var dijkstra = &Primitive{
	ID:      "Dijkstra",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Dijkstra") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		want := ir.Grid(ir.Int())
		if !in.Equal(want) {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Dijkstra expects Grid<Int> (cell entry costs), got %s", in)}
		}
		r, c, err := startCoords(op, "Dijkstra", pos)
		if err != nil {
			return nil, err
		}
		diagonal, err := connectivity(args, "Dijkstra", pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:      "Dijkstra",
			In:        want,
			Out:       want,
			Display:   fmt.Sprintf("Dijkstra from (%d, %d)", r, c),
			Swappable: true,
			Meta:      map[string]any{"row": r, "col": c, "diagonal": diagonal},
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Dijkstra", pos, "expected Grid, got %s", ir.DescribeValue(v))
				}
				for i, cell := range g.Cells {
					if n, ok := cell.(int64); !ok || n < 0 {
						return nil, runtimeErr("Dijkstra", pos,
							"cell %d has a negative or non-Int cost (%s)", i, ir.FormatValue(cell))
					}
				}
				if err := checkStart(g, r, c, nil, "Dijkstra", "", pos); err != nil {
					return nil, err
				}
				out := distGrid(g.Rows, g.Cols)
				var pq ir.PQ[[2]int]
				pq.Push([2]int{int(r), int(c)}, 0)
				for {
					cur, d, ok := pq.Pop()
					if !ok {
						break
					}
					i := cur[0]*g.Cols + cur[1]
					if out.Cells[i] != int64(-1) {
						continue // already settled with a smaller distance
					}
					out.Cells[i] = d
					for _, nb := range g.Neighbors(cur[0], cur[1], diagonal) {
						j := nb[0]*g.Cols + nb[1]
						if out.Cells[j] != int64(-1) {
							continue
						}
						pq.Push(nb, d+g.Cells[j].(int64))
					}
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Flood Fill — Grid<T> x region predicate -> Grid<Int>: 1 for every cell in
// the start's 4-connected region, 0 elsewhere.
// ---------------------------------------------------------------------------

var floodFill = &Primitive{
	ID:      "Flood Fill",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Flood") && hasWord(op, "Fill") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Flood Fill expects a Grid, got %s", in)}
		}
		r, c, err := startCoords(op, "Flood Fill", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "Flood Fill", pos)
		if err != nil {
			return nil, err
		}
		if err := requirePredicate(lam, in.Elem, "Flood Fill", pos); err != nil {
			return nil, err
		}
		diagonal, err := connectivity(args, "Flood Fill", pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:      "Flood Fill",
			In:        in,
			Out:       ir.Grid(ir.Int()),
			Display:   fmt.Sprintf("Flood Fill from (%d, %d)", r, c),
			Swappable: true,
			Meta:      map[string]any{"row": r, "col": c, "lambda": lam, "diagonal": diagonal},
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Flood Fill", pos, "expected Grid, got %s", ir.DescribeValue(v))
				}
				mask, err := walkableMask(g, lam, in.Elem, "Flood Fill", pos)
				if err != nil {
					return nil, err
				}
				if err := checkStart(g, r, c, mask, "Flood Fill", "in the region (its predicate is false there)", pos); err != nil {
					return nil, err
				}
				out := ir.NewGridValue(g.Rows, g.Cols)
				for i := range out.Cells {
					out.Cells[i] = int64(0)
				}
				var st ir.Stack[[2]int]
				out.SetAt(int(r), int(c), int64(1))
				st.Push([2]int{int(r), int(c)})
				for {
					cur, ok := st.Pop()
					if !ok {
						break
					}
					for _, nb := range g.Neighbors(cur[0], cur[1], diagonal) {
						i := nb[0]*g.Cols + nb[1]
						if !mask[i] || out.Cells[i] == int64(1) {
							continue
						}
						out.Cells[i] = int64(1)
						st.Push(nb)
					}
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Connected Components — Grid<T> x membership predicate -> Int: how many
// 4-connected regions of matching cells the grid contains.
// ---------------------------------------------------------------------------

var connectedComponents = &Primitive{
	ID:      "Connected Components",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Connected") && hasWord(op, "Components") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGrid {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Connected Components expects a Grid, got %s", in)}
		}
		lam, err := requireLambda(args, 1, "Connected Components", pos)
		if err != nil {
			return nil, err
		}
		if err := requirePredicate(lam, in.Elem, "Connected Components", pos); err != nil {
			return nil, err
		}
		diagonal, err := connectivity(args, "Connected Components", pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:      "Connected Components",
			In:        in,
			Out:       ir.Int(),
			Display:   "Connected Components",
			Swappable: true,
			Meta:      map[string]any{"lambda": lam, "diagonal": diagonal},
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, runtimeErr("Connected Components", pos,
						"expected Grid, got %s", ir.DescribeValue(v))
				}
				mask, err := walkableMask(g, lam, in.Elem, "Connected Components", pos)
				if err != nil {
					return nil, err
				}
				uf := ir.NewUnionFind(len(g.Cells))
				for r := 0; r < g.Rows; r++ {
					for c := 0; c < g.Cols; c++ {
						i := r*g.Cols + c
						if !mask[i] {
							continue
						}
						if c+1 < g.Cols && mask[i+1] {
							uf.Union(i, i+1)
						}
						if r+1 < g.Rows && mask[i+g.Cols] {
							uf.Union(i, i+g.Cols)
						}
						// Under Mode: 8 the two downward diagonals complete the
						// neighbourhood; the upward ones are covered by the
						// cell above having already unioned toward this one.
						if diagonal && r+1 < g.Rows {
							if c+1 < g.Cols && mask[i+g.Cols+1] {
								uf.Union(i, i+g.Cols+1)
							}
							if c > 0 && mask[i+g.Cols-1] {
								uf.Union(i, i+g.Cols-1)
							}
						}
					}
				}
				var n int64
				for i, m := range mask {
					if m && uf.Find(i) == i {
						n++
					}
				}
				return n, nil
			},
		}, nil
	},
}
