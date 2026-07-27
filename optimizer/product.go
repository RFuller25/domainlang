package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// fuseAllPairsProduct recognizes an All Pairs (k=2) over List<Int> whose
// Using: lambda tests a fixed product — `(a, b) -> a * b = K` — and lowers
// the O(n²) scan to an O(n) divisor scan: for each element x the only
// possible partner is K/x (when K divides evenly), looked up in a complement
// multiset. K = 0 is the case the division shortcut would get wrong — a
// zero element pairs with *everything* — so zeros are counted against the
// number of earlier elements instead. Like the sum and difference scans,
// this assumes Domain's numeric model (products stay within int64).
func fuseAllPairsProduct(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for _, list := range nodeLists(p) {
		for _, n := range list {
			if n.Prim != "All Pairs" {
				continue
			}
			if hasMeasuredArg(n) {
				continue // the arity this rewrite assumes must be a constant
			}
			if k, _ := n.Meta["k"].(int); k != 2 {
				continue
			}
			mode, _ := n.Meta["mode"].(string)
			if mode != "First" && mode != "Count" {
				continue
			}
			if n.In == nil || !n.In.Equal(ir.List(ir.Int())) {
				continue
			}
			lam := nodeLambda(n)
			if lam == nil {
				continue
			}
			target, ok := matchProductPair(lam)
			if !ok {
				continue
			}

			pos := n.Pos
			n.Prim = "DivisorPairScan"
			n.Display = fmt.Sprintf("Cursed Divisor Scan (product = %d, Mode: %s)", target, mode)
			n.Meta["target"] = target
			if mode == "Count" {
				n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsIntSlice(v)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "DivisorPairScan", Pos: pos, Msg: err.Error()}
					}
					return CountPairProduct(xs, target), nil
				}
			} else {
				n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsIntSlice(v)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "DivisorPairScan", Pos: pos, Msg: err.Error()}
					}
					pair, ok := FindPairProduct(xs, target)
					if !ok {
						return nil, &ir.RuntimeError{Prim: "DivisorPairScan", Pos: pos,
							Msg: "no combination satisfied the predicate"}
					}
					return ir.IntsToValue(pair), nil
				}
			}
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain rewrote All Pairs (product = %d) → Cursed Divisor Scan. Guaranteed hit.", target)})
		}
	}
	return rewrites
}

// matchProductPair recognizes `(a, b) -> a * b = K` (in either operand order,
// and with the literal on either side of '='), returning K.
func matchProductPair(lam *ast.Lambda) (int64, bool) {
	if len(lam.Params) != 2 {
		return 0, false
	}
	p0, p1 := lam.Params[0], lam.Params[1]
	if p0 == p1 {
		return 0, false // shadowed binding, see matchSumPair
	}
	eq, ok := lam.Body.(*ast.BinaryExpr)
	if !ok || eq.Op != token.EQ {
		return 0, false
	}
	if k, ok := intLit(eq.Left); ok && isProductOf(eq.Right, p0, p1) {
		return k, true
	}
	if k, ok := intLit(eq.Right); ok && isProductOf(eq.Left, p0, p1) {
		return k, true
	}
	return 0, false
}

// isProductOf reports whether e is `p0 * p1` or `p1 * p0`.
func isProductOf(e ast.Expr, p0, p1 string) bool {
	be, ok := e.(*ast.BinaryExpr)
	if !ok || be.Op != token.STAR {
		return false
	}
	l, lok := identName(be.Left)
	r, rok := identName(be.Right)
	if !lok || !rok {
		return false
	}
	return (l == p0 && r == p1) || (l == p1 && r == p0)
}

// CountPairProduct counts index pairs i<j with xs[i]*xs[j] == target in O(n).
// A nonzero element x pairs only with target/x (when target divides evenly);
// a zero element pairs with every earlier element exactly when target is 0.
func CountPairProduct(xs []int64, target int64) int64 {
	seen := make(map[int64]int64, len(xs))
	var count, earlier int64
	for _, x := range xs {
		if x == 0 {
			if target == 0 {
				count += earlier
			}
		} else if target%x == 0 {
			count += seen[target/x]
		}
		seen[x]++
		earlier++
	}
	return count
}

// FindPairProduct returns the values [xs[i], xs[j]] of the lexicographically-
// first index pair i<j with xs[i]*xs[j] == target — identical to the naive
// scan's First result — in O(n) via a complement multiset. When xs[i] is 0
// and target is 0 every later element matches, so the partner is xs[i+1].
func FindPairProduct(xs []int64, target int64) ([]int64, bool) {
	remaining := make(map[int64]int, len(xs))
	for _, x := range xs {
		remaining[x]++
	}
	for i := 0; i < len(xs); i++ {
		x := xs[i]
		remaining[x]-- // now reflects indices > i
		if x == 0 {
			if target == 0 && i+1 < len(xs) {
				return []int64{0, xs[i+1]}, true
			}
			continue
		}
		if target%x == 0 && remaining[target/x] > 0 {
			return []int64{x, target / x}, true
		}
	}
	return nil, false
}
