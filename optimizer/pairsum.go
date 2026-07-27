package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// fuseAllPairsSum is the second signature optimization. It recognizes an
// `All Pairs` (k=2) over List<Int> whose Using: lambda is a sum-to-constant
// test — `(a, b) -> a + b = K` — and replaces the O(n²) pair scan with an O(n)
// hash-set complement scan that produces an identical result. The named
// algorithm was a request; the optimizer honors the result, not the method.
func fuseAllPairsSum(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for _, n := range p.Nodes {
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
		lam, _ := n.Meta["lambda"].(*ast.Lambda)
		if lam == nil {
			continue
		}
		target, ok := matchSumPair(lam)
		if !ok {
			continue
		}

		rewriteAllPairsNode(n, mode, target)
		rewrites = append(rewrites, Rewrite{
			Message: fmt.Sprintf(
				"Domain rewrote All Pairs (sum = %d) → Cursed Hash-Set Scan. Guaranteed hit.", target),
		})
	}
	return rewrites
}

// rewriteAllPairsNode swaps a node's interpreter for the hash-set version,
// keeping its type signature.
func rewriteAllPairsNode(n *ir.Node, mode string, target int64) {
	pos := n.Pos
	n.Prim = "HashSetPairScan"
	n.Display = fmt.Sprintf("Cursed Hash-Set Scan (sum = %d, Mode: %s)", target, mode)
	n.Meta["target"] = target
	if mode == "Count" {
		n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsIntSlice(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "HashSetPairScan", Pos: pos, Msg: err.Error()}
			}
			return CountPairSum(xs, target), nil
		}
		return
	}
	// First
	n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
		xs, err := ir.AsIntSlice(v)
		if err != nil {
			return nil, &ir.RuntimeError{Prim: "HashSetPairScan", Pos: pos, Msg: err.Error()}
		}
		pair, ok := FindPairSum(xs, target)
		if !ok {
			return nil, &ir.RuntimeError{Prim: "HashSetPairScan", Pos: pos,
				Msg: "no combination satisfied the predicate"}
		}
		return ir.IntsToValue(pair), nil
	}
}

// matchSumPair recognizes `(a, b) -> a + b = K` (in either operand order, and
// with the literal on either side of '='), returning K.
func matchSumPair(lam *ast.Lambda) (int64, bool) {
	if len(lam.Params) != 2 {
		return 0, false
	}
	p0, p1 := lam.Params[0], lam.Params[1]
	if p0 == p1 {
		// A lambda like `(a, a) -> a + a = K` binds both params to the same
		// name; eval.EvalLambda's map-based Env makes the second binding
		// shadow the first, so the naive path only ever sees one element
		// doubled. The hash-set rewrite computes a real two-distinct-element
		// sum instead, which would silently diverge from the naive oracle.
		return 0, false
	}
	eq, ok := lam.Body.(*ast.BinaryExpr)
	if !ok || eq.Op != token.EQ {
		return 0, false
	}
	if k, ok := intLit(eq.Left); ok && isSumOf(eq.Right, p0, p1) {
		return k, true
	}
	if k, ok := intLit(eq.Right); ok && isSumOf(eq.Left, p0, p1) {
		return k, true
	}
	return 0, false
}

func intLit(e ast.Expr) (int64, bool) {
	if lit, ok := e.(*ast.IntLit); ok {
		return lit.Value, true
	}
	return 0, false
}

// isSumOf reports whether e is `p0 + p1` or `p1 + p0`.
func isSumOf(e ast.Expr, p0, p1 string) bool {
	be, ok := e.(*ast.BinaryExpr)
	if !ok || be.Op != token.PLUS {
		return false
	}
	l, lok := identName(be.Left)
	r, rok := identName(be.Right)
	if !lok || !rok {
		return false
	}
	return (l == p0 && r == p1) || (l == p1 && r == p0)
}

func identName(e ast.Expr) (string, bool) {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name, true
	}
	return "", false
}

// FindPairSum returns the values [xs[i], xs[j]] of the lexicographically-first
// index pair i<j with xs[i]+xs[j] == target — identical to the naive scan's
// First result — in O(n) using a complement multiset.
func FindPairSum(xs []int64, target int64) ([]int64, bool) {
	// remaining[v] = count of v among indices not yet passed.
	remaining := make(map[int64]int, len(xs))
	for _, x := range xs {
		remaining[x]++
	}
	for i := 0; i < len(xs); i++ {
		remaining[xs[i]]-- // now reflects indices > i
		need := target - xs[i]
		if remaining[need] > 0 {
			return []int64{xs[i], need}, true
		}
	}
	return nil, false
}

// CountPairSum counts index pairs i<j with xs[i]+xs[j] == target, in O(n).
func CountPairSum(xs []int64, target int64) int64 {
	seen := make(map[int64]int64, len(xs))
	var count int64
	for _, x := range xs {
		count += seen[target-x]
		seen[x]++
	}
	return count
}
