// Package optimizer rewrites the IR before interpretation. This is the whole
// thesis of Domain in miniature: a named algorithm (Quicksort) followed by a
// Top-K selection is a *request*, and the optimizer is free to honor the result
// with a faster algorithm — a partial selection that never fully sorts.
package optimizer

import (
	"fmt"
	"sort"

	"domain/ir"
)

// Rewrite records a single applied optimization, for --explain.
type Rewrite struct {
	Message string
}

// passes is the pass pipeline, in priority order within one round: the
// expression-layer simplifier first (folding can expose patterns to every
// later pass), then the algorithm substitutions (most specific patterns),
// then reordering dead-code elimination, then map/filter dead code and
// fusion. Passes cascade — e.g. Sort + Reverse first flips into one Sort,
// which can then fuse with a following Select Top K — so Optimize reruns the
// rounds until a full round applies nothing.
// isIntList reports whether t is exactly List<Int> — the shape every
// int-specialized rewrite requires. Float pipelines fail this check and keep
// their naive nodes (Float sorts stay unswapped: quickselect helpers are
// int64-typed, and NaN would make reordering visible).
func isIntList(t *ir.Type) bool {
	return t != nil && t.Equal(ir.List(ir.Int()))
}

// typeHasFloat reports whether Float appears anywhere in t.
func typeHasFloat(t *ir.Type) bool {
	if t == nil {
		return false
	}
	if t.Kind == ir.KFloat {
		return true
	}
	if typeHasFloat(t.Elem) || typeHasFloat(t.Key) {
		return true
	}
	for _, e := range t.Elems {
		if typeHasFloat(e) {
			return true
		}
	}
	for _, f := range t.Fields {
		if typeHasFloat(f.Type) {
			return true
		}
	}
	return false
}

var passes = []func(*ir.Pipeline) []Rewrite{
	// expression layer
	simplifyLambdaBodies,
	// algorithm substitutions
	fuseSortThenTopK,
	fuseSortTakeItem,
	fuseAllPairsSum,
	fusePairDiff,
	fuseAllPairsProduct,
	fuseTripleSum,
	fuseLinearMapExtremum,
	fuseWindowReduce,
	fuseSearchTarget,
	// reordering dead code
	fuseSortReverse,
	elideRedundantSort,
	hoistUniqueBeforeSort,
	elideReorderBeforeReduce,
	cancelReversePairs,
	elideRedundantUnique,
	elideUniqueBeforeExtremum,
	// map/filter dead code and fusion
	elideIdentityMap,
	elideMapBeforeCount,
	fuseMapMap,
	fuseFilterFilter,
	fuseFilterCount,
	fuseFoldSum,
	elideConstPredicates,
}

// maxRounds caps the cascade loop. Every pass either strictly shrinks the
// pipeline, rewrites a node so its own guard stops matching, or (the one
// swap, Sort + Unique) produces a shape no pass reverses — so the fixpoint
// is reached long before this bound; it exists as a backstop.
const maxRounds = 16

// Optimize applies the optimization passes to the pipeline in place, returning
// the list of rewrites performed. When enabled is false it is a no-op, leaving
// the naive pipeline intact (useful as a correctness oracle).
func Optimize(p *ir.Pipeline, enabled bool) []Rewrite {
	if !enabled {
		return nil
	}
	var rewrites []Rewrite
	for round := 0; round < maxRounds; round++ {
		applied := 0
		for _, pass := range passes {
			rs := pass(p)
			rewrites = append(rewrites, rs...)
			applied += len(rs)
		}
		if applied == 0 {
			break
		}
	}
	return rewrites
}

// fuseSortThenTopK finds a Sort node immediately followed by a SelectTopK node
// and replaces the pair with a single PartialSelect node.
func fuseSortThenTopK(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	var out []*ir.Node

	for i := 0; i < len(p.Nodes); i++ {
		n := p.Nodes[i]
		if i+1 < len(p.Nodes) && n.Prim == "Sort" && p.Nodes[i+1].Prim == "SelectTopK" && isIntList(n.In) {
			next := p.Nodes[i+1]
			desc, _ := n.Meta["desc"].(bool)
			k, _ := next.Meta["k"].(int64)
			thenSum, _ := next.Meta["sum"].(bool)

			fused := newPartialSelect(n, next, k, desc, thenSum)
			out = append(out, fused)

			order := "Ascending"
			if desc {
				order = "Descending"
			}
			rewrites = append(rewrites, Rewrite{
				Message: fmt.Sprintf(
					"Domain rewrote Quicksort (%s) + Top %d → Cursed Quickselect. Guaranteed hit.",
					order, k),
			})
			i++ // consume the SelectTopK node too
			continue
		}
		out = append(out, n)
	}

	p.Nodes = out
	return rewrites
}

// newPartialSelect builds the fused node. Its output is guaranteed identical to
// running Sort then SelectTopK, but it avoids a full sort of the input.
func newPartialSelect(sortNode, topNode *ir.Node, k int64, desc, thenSum bool) *ir.Node {
	display := fmt.Sprintf("Cursed Quickselect: Top %d", k)
	if thenSum {
		display += ", Sum"
	}
	return &ir.Node{
		Prim:    "PartialSelect",
		In:      sortNode.In,
		Out:     topNode.Out,
		Display: display,
		Meta:    map[string]any{"k": k, "desc": desc, "sum": thenSum},
		Pos:     sortNode.Pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsIntSlice(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "PartialSelect", Pos: sortNode.Pos, Msg: err.Error()}
			}
			top := TopK(xs, int(k), desc)
			if thenSum {
				var s int64
				for _, x := range top {
					s += x
				}
				return s, nil
			}
			return ir.IntsToValue(top), nil
		},
	}
}

// TopK returns the k elements that would appear first when xs is sorted in the
// requested order, themselves sorted in that order. It uses quickselect to
// partition, so it never fully sorts the input (only the k selected elements).
func TopK(xs []int64, k int, desc bool) []int64 {
	if k <= 0 || len(xs) == 0 {
		return []int64{}
	}
	if k > len(xs) {
		k = len(xs)
	}
	a := append([]int64(nil), xs...)

	// "front" reports whether x belongs ahead of y in the requested order.
	front := func(x, y int64) bool {
		if desc {
			return x > y
		}
		return x < y
	}

	quickselect(a, k, front)
	res := a[:k]
	sort.Slice(res, func(i, j int) bool { return front(res[i], res[j]) })
	return res
}

// quickselect rearranges a so that the k front-most elements occupy a[:k]
// (in arbitrary order), via Lomuto partitioning.
func quickselect(a []int64, k int, front func(x, y int64) bool) {
	lo, hi := 0, len(a)-1
	for lo < hi {
		p := partition(a, lo, hi, front)
		switch {
		case p == k-1:
			return
		case p < k-1:
			lo = p + 1
		default:
			hi = p - 1
		}
	}
}

func partition(a []int64, lo, hi int, front func(x, y int64) bool) int {
	// Median-of-three-ish: use the middle element as pivot to avoid worst case
	// on already-sorted input.
	mid := lo + (hi-lo)/2
	a[mid], a[hi] = a[hi], a[mid]
	pivot := a[hi]
	i := lo
	for j := lo; j < hi; j++ {
		if front(a[j], pivot) {
			a[i], a[j] = a[j], a[i]
			i++
		}
	}
	a[i], a[hi] = a[hi], a[i]
	return i
}
