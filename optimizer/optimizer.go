// Package optimizer rewrites the IR before interpretation. This is the whole
// thesis of Domain in miniature: a named algorithm (Quicksort) followed by a
// Top-K selection is a *request*, and the optimizer is free to honor the result
// with a faster algorithm — a partial selection that never fully sorts.
package optimizer

import (
	"fmt"
	"slices"

	"domain/ir"
)

// Rewrite records a single applied optimization, for --explain.
type Rewrite struct {
	// Pass names the pass that produced this rewrite, matching the function
	// name in passes below ("fuseSortThenTopK"). --explain never shows it;
	// it exists for the tools that report which passes fired on a program
	// (`domain expansion: stats`, `golf`), which cannot recover a pass
	// identity by pattern-matching English out of Message.
	//
	// It is stamped in Optimize rather than at each construction site,
	// because several passes are built on shared helpers (rewritePairs) and
	// a site inside one of those has no idea which pass is calling it.
	Pass    string
	Message string
}

// stamp labels every rewrite a pass returned with that pass's name.
func stamp(name string, rs []Rewrite) []Rewrite {
	for i := range rs {
		rs[i].Pass = name
	}
	return rs
}

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

// passes is the pass pipeline, in priority order within one round: the
// expression-layer simplifier first (folding can expose patterns to every
// later pass), then the algorithm substitutions (most specific patterns),
// then reordering dead-code elimination, then map/filter dead code and
// fusion. Passes cascade — e.g. Sort + Reverse first flips into one Sort,
// which can then fuse with a following Select Top K — so Optimize reruns the
// rounds until a full round applies nothing.
// A pass carries its own name so a Rewrite can report where it came from. The
// name is written out rather than derived by reflection at run time: a test
// (optimizer_test.go) checks each entry's name against the function it holds,
// so the duplication cannot drift and the production path stays plain.
type pass struct {
	name string
	run  func(*ir.Pipeline) []Rewrite
}

var passes = []pass{
	// expression layer
	{"simplifyLambdaBodies", simplifyLambdaBodies},
	// algorithm substitutions
	{"fuseSortThenTopK", fuseSortThenTopK},
	{"fuseSortTakeItem", fuseSortTakeItem},
	{"fuseAllPairsSum", fuseAllPairsSum},
	{"fusePairDiff", fusePairDiff},
	{"fuseAllPairsProduct", fuseAllPairsProduct},
	{"fuseTripleSum", fuseTripleSum},
	{"fuseLinearMapExtremum", fuseLinearMapExtremum},
	{"fuseWindowReduce", fuseWindowReduce},
	{"fuseSearchTarget", fuseSearchTarget},
	{"fuseFilterFirst", fuseFilterFirst},
	{"fuseUnfoldStream", fuseUnfoldStream},
	// reordering dead code
	{"fuseSortReverse", fuseSortReverse},
	{"elideRedundantSort", elideRedundantSort},
	{"hoistUniqueBeforeSort", hoistUniqueBeforeSort},
	{"elideReorderBeforeReduce", elideReorderBeforeReduce},
	{"cancelReversePairs", cancelReversePairs},
	{"elideRedundantUnique", elideRedundantUnique},
	{"elideUniqueBeforeExtremum", elideUniqueBeforeExtremum},
	// map/filter dead code and fusion
	{"elideIdentityMap", elideIdentityMap},
	{"elideMapBeforeCount", elideMapBeforeCount},
	{"fuseMapMap", fuseMapMap},
	{"fuseFilterFilter", fuseFilterFilter},
	{"fuseFilterCount", fuseFilterCount},
	{"fuseFoldSum", fuseFoldSum},
	{"fuseMapReduceBy", fuseMapReduceBy},
	{"fuseZipWith", fuseZipWith},
	{"elideConstPredicates", elideConstPredicates},
	{"elideConstEarlyExits", elideConstEarlyExits},
}

// maxRounds caps the cascade loop. Every pass either strictly shrinks the
// pipeline, rewrites a node so its own guard stops matching, or (the one
// swap, Sort + Unique) produces a shape no pass reverses — so the fixpoint
// is reached long before this bound; it exists as a backstop.
const maxRounds = 16

// Optimize applies the optimization passes to the pipeline in place, returning
// the list of rewrites performed. When enabled is false it is a no-op, leaving
// the naive pipeline intact (useful as a correctness oracle).
//
// This is the pipeline every ordinary run and build takes: the full pass list,
// in the order above, to a fixpoint. Callers that want to vary that — which
// today means `domain expansion: mahoraga` searching for the subset and order
// that suits one program — go through OptimizeWith.
func Optimize(p *ir.Pipeline, enabled bool) []Rewrite {
	if !enabled {
		return nil
	}
	return OptimizeWith(p, Schedule{})
}

// Schedule selects which passes run, in what order, and how many rounds the
// cascade may take.
//
// It exists because "which passes help" is a question with a different answer
// per program, and the default list can only be right on average. A pass that
// pessimises one particular program cannot be found by the optimizer itself —
// the optimizer is not allowed to know anything about the input — but it can
// be found by measuring, which is what mahoraga does with this.
//
// The zero Schedule is the default pipeline, so OptimizeWith(p, Schedule{}) and
// Optimize(p, true) are the same thing.
type Schedule struct {
	// Passes names the passes to run, in order. Nil means every pass in the
	// declared order. An empty non-nil slice means none of them, which is a
	// meaningful request: it isolates what the linear-accumulator pass alone
	// is worth.
	Passes []string

	// MaxRounds caps the cascade. Zero means the default.
	MaxRounds int

	// SkipLinear stands down markLinearAccumulators, which otherwise always
	// runs once after the cascade settles.
	SkipLinear bool
}

// PassNames is every pass in its declared order — what a caller enumerates to
// build a Schedule, and what a report names a rewrite by.
func PassNames() []string {
	out := make([]string, len(passes))
	for i, ps := range passes {
		out[i] = ps.name
	}
	return out
}

// LinearPassName is the pass that runs after the cascade rather than in it.
// Named here so a caller building a schedule can talk about it without
// hardcoding the string.
const LinearPassName = "markLinearAccumulators"

// UnknownPasses returns the names in a schedule that name no pass. A caller
// validates before running rather than silently optimizing less than it asked
// for — a misspelled pass name that quietly did nothing would make a
// measurement mean the opposite of what it appeared to.
func (s Schedule) UnknownPasses() []string {
	if s.Passes == nil {
		return nil
	}
	known := map[string]bool{}
	for _, ps := range passes {
		known[ps.name] = true
	}
	var bad []string
	for _, name := range s.Passes {
		if !known[name] {
			bad = append(bad, name)
		}
	}
	return bad
}

// selected resolves a schedule to the passes it names, in the order it names
// them. A name repeated in Passes runs twice per round, which is a legitimate
// thing to measure.
func (s Schedule) selected() []pass {
	if s.Passes == nil {
		return passes
	}
	byName := make(map[string]pass, len(passes))
	for _, ps := range passes {
		byName[ps.name] = ps
	}
	out := make([]pass, 0, len(s.Passes))
	for _, name := range s.Passes {
		if ps, ok := byName[name]; ok {
			out = append(out, ps)
		}
	}
	return out
}

func (s Schedule) rounds() int {
	if s.MaxRounds <= 0 {
		return maxRounds
	}
	return s.MaxRounds
}

// OptimizeWith applies a chosen schedule of passes to the pipeline in place.
//
// Every schedule is semantics-preserving, because every individual pass is:
// running fewer passes, or the same passes in a different order, can change
// how fast a program is and cannot change what it computes. That is what makes
// searching this space safe in a way that mutating the emitted code is not.
func OptimizeWith(p *ir.Pipeline, s Schedule) []Rewrite {
	sel := s.selected()
	var rewrites []Rewrite
	for range s.rounds() {
		applied := 0
		for _, ps := range sel {
			rs := stamp(ps.name, ps.run(p))
			rewrites = append(rewrites, rs...)
			applied += len(rs)
		}
		if applied == 0 {
			break
		}
	}
	// Linear accumulators run once, *after* the cascade has settled, rather
	// than inside it. The pass annotates expressions instead of rewriting the
	// pipeline, so it could never feed the cascade — and it must not be fed
	// back into: expression simplification folds a constant by applying a
	// lambda twice, which is precisely what an in-place update must not have
	// done to it. See optimizer/linear.go.
	if !s.SkipLinear {
		rewrites = append(rewrites, stamp(LinearPassName, markLinearAccumulators(p))...)
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
			// TopK takes k as an ordinary value, so a measured count is carried
			// onto the fused node rather than folded. Reading it through readArg
			// is what makes that safe: a count in neither form stops the rewrite
			// instead of arriving as a plausible zero.
			k, ok := readArg(next, "k")
			if !ok {
				out = append(out, n)
				continue
			}
			desc, _ := n.Meta["desc"].(bool)
			thenSum, _ := next.Meta["sum"].(bool)

			fused := newPartialSelect(n, next, k, desc, thenSum)
			out = append(out, fused)

			order := "Ascending"
			if desc {
				order = "Descending"
			}
			rewrites = append(rewrites, Rewrite{
				Message: fmt.Sprintf(
					"Domain rewrote Quicksort (%s) + Top %s → Cursed Quickselect. Guaranteed hit.",
					order, k.describe()),
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
func newPartialSelect(sortNode, topNode *ir.Node, k arg, desc, thenSum bool) *ir.Node {
	display := fmt.Sprintf("Cursed Quickselect: Top %s", k.describe())
	if thenSum {
		display += ", Sum"
	}
	meta := map[string]any{"desc": desc, "sum": thenSum}
	k.writeMeta(meta, "k")
	return &ir.Node{
		Prim:    "PartialSelect",
		In:      sortNode.In,
		Out:     topNode.Out,
		Display: display,
		Meta:    meta,
		Pos:     sortNode.Pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsIntSlice(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "PartialSelect", Pos: sortNode.Pos, Msg: err.Error()}
			}
			kk, err := k.value(v)
			if err != nil {
				return nil, err
			}
			top := TopK(xs, int(kk), desc)
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
	k = min(k, len(xs))
	a := slices.Clone(xs)

	// "front" reports whether x belongs ahead of y in the requested order.
	front := func(x, y int64) bool {
		if desc {
			return x > y
		}
		return x < y
	}

	quickselect(a, k, front)
	res := a[:k]
	slices.Sort(res)
	if desc {
		slices.Reverse(res)
	}
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
