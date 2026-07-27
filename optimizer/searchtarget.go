package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/ir"
)

// fuseSearchTarget recognizes a BFS or Dijkstra distance grid consumed by
// nothing but a single-cell read — `Apply ((g) -> at(g, R, C))` with literal
// coordinates — and replaces the pair with an early-exit search that stops
// the moment the target cell settles. The full search explores every
// reachable cell; the early exit explores only cells at distance/cost up to
// the target's, which on a big grid with a near start-and-target pair is the
// difference between the whole map and a neighborhood. BFS labels cells at
// enqueue time and Dijkstra settles them at pop time, so returning at exactly
// those moments reproduces the full search's value bit-for-bit — including
// -1 for an unreachable or unwalkable target. Every validation the naive
// pair performs (predicate errors, start checks, Dijkstra's cost check, the
// target bounds check at() would do) fires with identical messages.
func fuseSearchTarget(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		kind := a.Prim
		if kind != "BFS" && kind != "Dijkstra" {
			return nil, "", false
		}
		if b.Prim != "Apply" {
			return nil, "", false
		}
		lam := nodeLambda(b)
		if lam == nil {
			return nil, "", false
		}
		tr, tc, ok := matchAtTarget(lam)
		if !ok {
			return nil, "", false
		}
		// The early-exit search takes its start as data, so a measured one
		// rides along — the fused node measures it from the same grid the
		// naive pair would have, and reports the same out-of-bounds error.
		rowA, okRow := readArg(a, "row")
		colA, okCol := readArg(a, "col")
		if !okRow || !okCol {
			return nil, "", false
		}
		searchLam := nodeLambda(a) // the walkable predicate; nil for Dijkstra
		cellType := elemType(a.In) // the grid's cell type, for the predicate

		searchPos, applyPos := a.Pos, b.Pos
		meta := map[string]any{"kind": kind, "trow": tr, "tcol": tc}
		rowA.writeMeta(meta, "row")
		colA.writeMeta(meta, "col")
		if searchLam != nil {
			meta["lambda"] = searchLam
		}
		fused := &ir.Node{
			Prim:    "SearchTarget",
			In:      a.In,
			Out:     ir.Int(),
			Display: fmt.Sprintf("Early-Exit %s (%s, %s) → (%d, %d)", kind, rowA.describe(), colA.describe(), tr, tc),
			Meta:    meta,
			Pos:     searchPos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GridValue)
				if !ok {
					return nil, &ir.RuntimeError{Prim: kind, Pos: searchPos,
						Msg: fmt.Sprintf("expected Grid, got %s", ir.DescribeValue(v))}
				}
				row, err := rowA.value(v)
				if err != nil {
					return nil, err
				}
				col, err := colA.value(v)
				if err != nil {
					return nil, err
				}
				// The naive pair's validations, in the same order with the
				// same wording (prims/search.go, eval's at).
				var mask []bool
				var costs []int64
				if kind == "BFS" {
					mask = make([]bool, len(g.Cells))
					for i, cell := range g.Cells {
						keep, err := evalPredicate(searchLam, cellType, cell)
						if err != nil {
							return nil, &ir.RuntimeError{Prim: "BFS", Pos: searchPos,
								Msg: fmt.Sprintf("cell %d: %v", i, err)}
						}
						mask[i] = keep
					}
				} else {
					costs = make([]int64, len(g.Cells))
					for i, cell := range g.Cells {
						n, isInt := cell.(int64)
						if !isInt || n < 0 {
							return nil, &ir.RuntimeError{Prim: "Dijkstra", Pos: searchPos,
								Msg: fmt.Sprintf("cell %d has a negative or non-Int cost (%s)", i, ir.FormatValue(cell))}
						}
						costs[i] = n
					}
				}
				if !g.InBounds(int(row), int(col)) {
					return nil, &ir.RuntimeError{Prim: kind, Pos: searchPos,
						Msg: fmt.Sprintf("start (%d, %d) is out of bounds (grid %dx%d)", row, col, g.Rows, g.Cols)}
				}
				if mask != nil && !mask[int(row)*g.Cols+int(col)] {
					return nil, &ir.RuntimeError{Prim: "BFS", Pos: searchPos,
						Msg: fmt.Sprintf("start (%d, %d) is not walkable", row, col)}
				}
				// The naive pipeline discovers a bad target only after the
				// full search, inside at(); the search itself cannot fail
				// past this point, so checking first is unobservable.
				if !g.InBounds(int(tr), int(tc)) {
					return nil, &ir.RuntimeError{Prim: "Apply", Pos: applyPos,
						Msg: fmt.Sprintf("at: position (%d, %d) out of range (grid %dx%d)", tr, tc, g.Rows, g.Cols)}
				}
				if kind == "BFS" {
					return BFSTarget(g.Rows, g.Cols, mask, row, col, tr, tc), nil
				}
				return DijkstraTarget(g.Rows, g.Cols, costs, row, col, tr, tc), nil
			},
		}
		return []*ir.Node{fused},
			fmt.Sprintf("Domain rewrote %s + at(%d, %d) → early-exit search (stops when the target settles). Guaranteed hit.",
				kind, tr, tc),
			true
	})
}

// matchAtTarget recognizes a one-parameter lambda whose whole body is
// `at(g, R, C)` — the parameter itself indexed at two integer literals.
func matchAtTarget(lam *ast.Lambda) (tr, tc int64, ok bool) {
	if len(lam.Params) != 1 {
		return 0, 0, false
	}
	call, isCall := lam.Body.(*ast.CallExpr)
	if !isCall || len(call.Args) != 3 {
		return 0, 0, false
	}
	fn, fok := identName(call.Fn)
	arg, aok := identName(call.Args[0])
	if !fok || !aok || fn != "at" || arg != lam.Params[0] {
		return 0, 0, false
	}
	tr, rok := intLit(call.Args[1])
	tc, cok := intLit(call.Args[2])
	if !rok || !cok {
		return 0, 0, false
	}
	return tr, tc, true
}

// BFSTarget returns the BFS step distance from (sr,sc) to (tr,tc) over the
// masked grid, or -1 when the target is unwalkable or unreachable, stopping
// as soon as the target is labeled. Distances are assigned at enqueue time,
// exactly like the full search, so the returned value is identical to
// reading the full distance grid at the target.
func BFSTarget(rows, cols int, mask []bool, sr, sc, tr, tc int64) int64 {
	w := int64(cols)
	target := tr*w + tc
	start := sr*w + sc
	if start == target {
		return 0
	}
	dist := make([]int64, rows*cols)
	for i := range dist {
		dist[i] = -1
	}
	dist[start] = 0
	var q ir.Queue[[2]int64]
	q.Push([2]int64{sr, sc})
	for {
		cur, ok := q.Pop()
		if !ok {
			return -1
		}
		d := dist[cur[0]*w+cur[1]]
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur[0]+dl[0], cur[1]+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= w {
				continue
			}
			i := nr*w + nc
			if !mask[i] || dist[i] != -1 {
				continue
			}
			dist[i] = d + 1
			if i == target {
				return d + 1
			}
			q.Push([2]int64{nr, nc})
		}
	}
}

// DijkstraTarget returns the minimum entry-cost total from (sr,sc) to
// (tr,tc), or -1 when unreachable, stopping when the target pops off the
// heap — the moment Dijkstra settles a cell its distance is final, so the
// early return matches the full search exactly.
func DijkstraTarget(rows, cols int, costs []int64, sr, sc, tr, tc int64) int64 {
	w := int64(cols)
	target := tr*w + tc
	dist := make([]int64, rows*cols)
	for i := range dist {
		dist[i] = -1
	}
	var pq ir.PQ[[2]int64]
	pq.Push([2]int64{sr, sc}, 0)
	for {
		cur, d, ok := pq.Pop()
		if !ok {
			return -1
		}
		i := cur[0]*w + cur[1]
		if dist[i] != -1 {
			continue
		}
		dist[i] = d
		if i == target {
			return d
		}
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur[0]+dl[0], cur[1]+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= w {
				continue
			}
			j := nr*w + nc
			if dist[j] != -1 {
				continue
			}
			pq.Push([2]int64{nr, nc}, d+costs[j])
		}
	}
}
