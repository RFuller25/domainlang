package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
)

// The algorithm-substitution passes: like the original Sort+TopK and
// All-Pairs-sum rewrites, these swap a *named* algorithm for a faster one
// with the identical result. Each rewritten node records its arguments in
// Meta so codegen can lower it (QuickselectItem, HashSetTripleScan,
// HashSetDiffScan, LinearMapExtremum all have emitNode cases).

// ---------------------------------------------------------------------------
// Sort + Take Item k → quickselect the kth order statistic
// ---------------------------------------------------------------------------

// fuseSortTakeItem replaces a full Sort feeding a single Take Item with a
// quickselect of the kth order statistic — O(n) instead of O(n log n).
func fuseSortTakeItem(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Sort" || b.Prim != "Take Item" || !isIntList(a.In) {
			return nil, "", false
		}
		if hasMeasuredArg(b) {
			return nil, "", false // the rewrite is only valid for a known index
		}
		desc, _ := a.Meta["desc"].(bool)
		idx, ok := b.Meta["index"].(int)
		if !ok {
			return nil, "", false
		}
		pos := a.Pos
		fused := &ir.Node{
			Prim:    "QuickselectItem",
			In:      a.In,
			Out:     b.Out,
			Display: fmt.Sprintf("Cursed Quickselect: item %d of the %s order", idx, orderName(desc)),
			Meta:    map[string]any{"index": idx, "desc": desc},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsIntSlice(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "QuickselectItem", Pos: pos, Msg: err.Error()}
				}
				if idx < 0 || idx >= len(xs) {
					return nil, &ir.RuntimeError{Prim: "QuickselectItem", Pos: pos,
						Msg: fmt.Sprintf("index %d out of range (length %d)", idx, len(xs))}
				}
				return KthOrderStatistic(xs, idx, desc), nil
			},
		}
		return []*ir.Node{fused},
			fmt.Sprintf("Domain rewrote Quicksort (%s) + Take Item %d → Cursed Quickselect (kth order statistic). Guaranteed hit.",
				orderName(desc), idx),
			true
	})
}

// KthOrderStatistic returns the element at index k of xs sorted in the
// requested order, without fully sorting. k must be in range.
func KthOrderStatistic(xs []int64, k int, desc bool) int64 {
	top := TopK(xs, k+1, desc)
	return top[k]
}

// ---------------------------------------------------------------------------
// Combinations 3 sum-to-constant → hash pair scan (the 3SUM rewrite)
// ---------------------------------------------------------------------------

// fuseTripleSum recognizes `Combinations 3` over List<Int> in Mode First or
// Count whose lambda is `(a, b, c) -> a + b + c = K` (any operand order or
// association, the literal on either side of `=`) and lowers the O(n³) scan
// to O(n²) hashing. This is AoC 2020 Day 1 Part 2's shape.
func fuseTripleSum(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for _, list := range nodeLists(p) {
		for _, n := range list {
			if n.Prim != "Combinations" {
				continue
			}
			if hasMeasuredArg(n) {
				continue // the arity this rewrite assumes must be a constant
			}
			if k, _ := n.Meta["k"].(int); k != 3 {
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
			target, ok := matchSumTriple(lam)
			if !ok {
				continue
			}

			pos := n.Pos
			n.Prim = "HashSetTripleScan"
			n.Display = fmt.Sprintf("Cursed Hash-Set Triple Scan (sum = %d, Mode: %s)", target, mode)
			n.Meta["target"] = target
			if mode == "Count" {
				n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsIntSlice(v)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "HashSetTripleScan", Pos: pos, Msg: err.Error()}
					}
					return CountTripleSum(xs, target), nil
				}
			} else {
				n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsIntSlice(v)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "HashSetTripleScan", Pos: pos, Msg: err.Error()}
					}
					triple, ok := FindTripleSum(xs, target)
					if !ok {
						return nil, &ir.RuntimeError{Prim: "HashSetTripleScan", Pos: pos,
							Msg: "no combination satisfied the predicate"}
					}
					return ir.IntsToValue(triple), nil
				}
			}
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain rewrote Combinations 3 (sum = %d) → Cursed Hash-Set Triple Scan. Guaranteed hit.", target)})
		}
	}
	return rewrites
}

// matchSumTriple recognizes `(a, b, c) -> a + b + c = K` with three distinct
// parameters, each appearing exactly once in the sum.
func matchSumTriple(lam *ast.Lambda) (int64, bool) {
	if len(lam.Params) != 3 {
		return 0, false
	}
	p0, p1, p2 := lam.Params[0], lam.Params[1], lam.Params[2]
	if p0 == p1 || p0 == p2 || p1 == p2 {
		// Duplicate names shadow each other in the map-based lambda Env (see
		// matchSumPair) — the naive path would not see three elements.
		return 0, false
	}
	eq, ok := lam.Body.(*ast.BinaryExpr)
	if !ok || eq.Op != token.EQ {
		return 0, false
	}
	if k, ok := intLit(eq.Left); ok && isSumOfAll(eq.Right, p0, p1, p2) {
		return k, true
	}
	if k, ok := intLit(eq.Right); ok && isSumOfAll(eq.Left, p0, p1, p2) {
		return k, true
	}
	return 0, false
}

// isSumOfAll reports whether e is a `+` tree whose leaves are exactly the
// given identifiers, each occurring once (any order, any association).
func isSumOfAll(e ast.Expr, params ...string) bool {
	var leaves []string
	if !collectSumLeaves(e, &leaves) || len(leaves) != len(params) {
		return false
	}
	need := map[string]int{}
	for _, p := range params {
		need[p]++
	}
	for _, l := range leaves {
		need[l]--
		if need[l] < 0 {
			return false
		}
	}
	return true
}

func collectSumLeaves(e ast.Expr, out *[]string) bool {
	switch x := e.(type) {
	case *ast.Ident:
		*out = append(*out, x.Name)
		return true
	case *ast.BinaryExpr:
		return x.Op == token.PLUS && collectSumLeaves(x.Left, out) && collectSumLeaves(x.Right, out)
	default:
		return false
	}
}

// CountTripleSum counts index triples i<j<k with xs[i]+xs[j]+xs[k] == target
// in O(n²): sweeping k while maintaining the multiset of pair sums from the
// prefix.
func CountTripleSum(xs []int64, target int64) int64 {
	pairSums := make(map[int64]int64)
	var count int64
	for k := 0; k < len(xs); k++ {
		count += pairSums[target-xs[k]]
		for i := 0; i < k; i++ {
			pairSums[xs[i]+xs[k]]++
		}
	}
	return count
}

// FindTripleSum returns the values of the lexicographically-first index
// triple i<j<k with xs[i]+xs[j]+xs[k] == target — identical to the naive
// scan's First result — in O(n²) using complement multisets.
func FindTripleSum(xs []int64, target int64) ([]int64, bool) {
	for i := 0; i < len(xs); i++ {
		// remaining[v] = count of v among indices > j (starts as indices > i).
		remaining := make(map[int64]int, len(xs)-i)
		for _, x := range xs[i+1:] {
			remaining[x]++
		}
		for j := i + 1; j < len(xs); j++ {
			remaining[xs[j]]--
			need := target - xs[i] - xs[j]
			if remaining[need] > 0 {
				return []int64{xs[i], xs[j], need}, true
			}
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// All Pairs difference-to-constant → hash complement scan
// ---------------------------------------------------------------------------

// fusePairDiff recognizes an All Pairs (k=2) whose lambda tests a fixed
// difference — `(a, b) -> a - b = K` or `(a, b) -> b - a = K` — and lowers
// the O(n²) scan to an O(n) complement scan, the sibling of the sum rewrite.
func fusePairDiff(p *ir.Pipeline) []Rewrite {
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
			target, flipped, ok := matchDiffPair(lam)
			if !ok {
				continue
			}

			pos := n.Pos
			n.Prim = "HashSetDiffScan"
			n.Display = fmt.Sprintf("Cursed Hash-Set Difference Scan (diff = %d, Mode: %s)", target, mode)
			n.Meta["target"] = target
			n.Meta["flipped"] = flipped
			if mode == "Count" {
				n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsIntSlice(v)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "HashSetDiffScan", Pos: pos, Msg: err.Error()}
					}
					return CountPairDiff(xs, target, flipped), nil
				}
			} else {
				n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsIntSlice(v)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "HashSetDiffScan", Pos: pos, Msg: err.Error()}
					}
					pair, ok := FindPairDiff(xs, target, flipped)
					if !ok {
						return nil, &ir.RuntimeError{Prim: "HashSetDiffScan", Pos: pos,
							Msg: "no combination satisfied the predicate"}
					}
					return ir.IntsToValue(pair), nil
				}
			}
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain rewrote All Pairs (difference = %d) → Cursed Hash-Set Scan. Guaranteed hit.", target)})
		}
	}
	return rewrites
}

// matchDiffPair recognizes `(a, b) -> a - b = K` (flipped=false) or
// `(a, b) -> b - a = K` (flipped=true), with the literal on either side of
// `=` and distinct parameter names.
func matchDiffPair(lam *ast.Lambda) (target int64, flipped bool, ok bool) {
	if len(lam.Params) != 2 {
		return 0, false, false
	}
	p0, p1 := lam.Params[0], lam.Params[1]
	if p0 == p1 {
		return 0, false, false // shadowed binding, see matchSumPair
	}
	eq, isEq := lam.Body.(*ast.BinaryExpr)
	if !isEq || eq.Op != token.EQ {
		return 0, false, false
	}
	try := func(k int64, e ast.Expr) (int64, bool, bool) {
		be, isBin := e.(*ast.BinaryExpr)
		if !isBin || be.Op != token.MINUS {
			return 0, false, false
		}
		l, lok := identName(be.Left)
		r, rok := identName(be.Right)
		if !lok || !rok {
			return 0, false, false
		}
		if l == p0 && r == p1 {
			return k, false, true
		}
		if l == p1 && r == p0 {
			return k, true, true
		}
		return 0, false, false
	}
	if k, isLit := intLit(eq.Left); isLit {
		return try(k, eq.Right)
	}
	if k, isLit := intLit(eq.Right); isLit {
		return try(k, eq.Left)
	}
	return 0, false, false
}

// CountPairDiff counts index pairs i<j whose difference hits target —
// xs[i]-xs[j] == target normally, xs[j]-xs[i] == target when flipped — in
// O(n) with a running multiset of earlier values.
func CountPairDiff(xs []int64, target int64, flipped bool) int64 {
	seen := make(map[int64]int64, len(xs))
	var count int64
	for _, x := range xs {
		if flipped {
			count += seen[x-target] // need earlier value v with x - v = target
		} else {
			count += seen[x+target] // need earlier value v with v - x = target
		}
		seen[x]++
	}
	return count
}

// FindPairDiff returns the values [xs[i], xs[j]] of the lexicographically-
// first index pair i<j hitting the difference, matching the naive First
// result, in O(n) via a complement multiset.
func FindPairDiff(xs []int64, target int64, flipped bool) ([]int64, bool) {
	remaining := make(map[int64]int, len(xs))
	for _, x := range xs {
		remaining[x]++
	}
	for i := 0; i < len(xs); i++ {
		remaining[xs[i]]-- // now reflects indices > i
		need := xs[i] - target // xs[i] - xs[j] = target
		if flipped {
			need = xs[i] + target // xs[j] - xs[i] = target
		}
		if remaining[need] > 0 {
			return []int64{xs[i], need}, true
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Linear Map Each + Max/Min → reduce first, map once
// ---------------------------------------------------------------------------

// fuseLinearMapExtremum rewrites `Map Each ((x) -> a*x + b)` followed by Max
// or Min into finding the input extremum first and applying the lambda once
// — n lambda applications become 1, and no mapped list is built. A linear
// map with a > 0 is monotonically increasing (max f = f(max)); with a < 0 it
// is decreasing (max f = f(min)), so the input extremum flips. Like all
// Domain arithmetic, this assumes values stay within int64.
func fuseLinearMapExtremum(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Map Each" || (b.Prim != "Max" && b.Prim != "Min") {
			return nil, "", false
		}
		if a.In == nil || !a.In.Equal(ir.List(ir.Int())) {
			return nil, "", false
		}
		lam := nodeLambda(a)
		if lam == nil || len(lam.Params) != 1 {
			return nil, "", false
		}
		coeff, _, ok := linearForm(lam.Body, lam.Params[0])
		if !ok || coeff == 0 {
			return nil, "", false
		}
		reduce := b.Prim
		pickMin := (reduce == "Max") == (coeff < 0)
		pos := a.Pos
		fused := &ir.Node{
			Prim:    "LinearMapExtremum",
			In:      a.In,
			Out:     ir.Int(),
			Display: fmt.Sprintf("%s via input %s + one lambda application", reduce, extremumName(pickMin)),
			Meta:    map[string]any{"lambda": lam, "pickMin": pickMin, "reduce": reduce},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsIntSlice(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "LinearMapExtremum", Pos: pos, Msg: err.Error()}
				}
				if len(xs) == 0 {
					return nil, &ir.RuntimeError{Prim: "LinearMapExtremum", Pos: pos,
						Msg: fmt.Sprintf("%s of an empty list is undefined", reduce)}
				}
				ext := xs[0]
				for _, x := range xs[1:] {
					if (pickMin && x < ext) || (!pickMin && x > ext) {
						ext = x
					}
				}
				r, err := eval.EvalLambdaTyped(lam, []*ir.Type{ir.Int()}, ext)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "LinearMapExtremum", Pos: pos, Msg: err.Error()}
				}
				return r, nil
			},
		}
		return []*ir.Node{fused},
			fmt.Sprintf("Domain rewrote Map Each (linear) + %s → input %s + one application (monotone maps commute with extrema). Guaranteed hit.",
				reduce, extremumName(pickMin)),
			true
	})
}

func extremumName(pickMin bool) string {
	if pickMin {
		return "Min"
	}
	return "Max"
}
