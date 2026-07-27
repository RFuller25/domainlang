package optimizer

import (
	"fmt"
	"sort"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
)

// Structural passes over the top-level node list: dead-code elimination of
// reorderings, involution/idempotence cancellation, and node fusion. These
// passes change the node list's length, so they run on p.Nodes only —
// sub-pipelines captured by Channel/loop Eval closures cannot be re-sliced
// without desynchronizing the interpreter from Meta (see nodeLists).

// pairRewrite is one adjacent-pair rule: given nodes (a, b) it returns the
// replacement node list and an --explain message, or match=false.
type pairRewrite func(a, b *ir.Node) (repl []*ir.Node, msg string, match bool)

// rewritePairs runs one adjacent-pair rule left to right over p.Nodes. After
// a match it skips past the replacement, so a rule that re-emits its inputs
// (e.g. a swap) cannot loop within one scan.
func rewritePairs(p *ir.Pipeline, rule pairRewrite) []Rewrite {
	var rewrites []Rewrite
	var out []*ir.Node
	for i := 0; i < len(p.Nodes); i++ {
		if i+1 < len(p.Nodes) {
			if repl, msg, ok := rule(p.Nodes[i], p.Nodes[i+1]); ok {
				out = append(out, repl...)
				rewrites = append(rewrites, Rewrite{Message: msg})
				i++ // consume b as well
				continue
			}
		}
		out = append(out, p.Nodes[i])
	}
	p.Nodes = out
	return rewrites
}

// nodeLambda fetches a node's Using: lambda from Meta.
func nodeLambda(n *ir.Node) *ast.Lambda {
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	return lam
}

// isOrderInsensitiveReduce reports whether prim folds a List into a result
// that cannot depend on element order. Count Matching is excluded on purpose:
// its result is order-insensitive but its per-element error messages are not.
func isOrderInsensitiveReduce(prim string) bool {
	switch prim {
	case "Sum", "Count", "Max", "Min", "Product":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Reordering dead-code passes
// ---------------------------------------------------------------------------

// elideRedundantSort drops a Sort that is immediately re-sorted: only the
// last ordering survives.
func elideRedundantSort(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Sort" || b.Prim != "Sort" || !isIntList(a.In) {
			return nil, "", false
		}
		return []*ir.Node{b},
			"Domain rewrote Quicksort + Quicksort → single Quicksort (the first ordering is dead). Guaranteed hit.",
			true
	})
}

// cancelReversePairs removes Reverse ∘ Reverse — an involution applied twice
// is the identity.
func cancelReversePairs(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Reverse" || b.Prim != "Reverse" {
			return nil, "", false
		}
		return nil,
			"Domain rewrote Reverse + Reverse → nothing at all (a double inversion is the identity). Guaranteed hit.",
			true
	})
}

// fuseSortReverse folds Sort ∘ Reverse into one Sort with the opposite order:
// for List<Int>, reversing an ascending sort is exactly a descending sort
// (duplicate values are indistinguishable).
func fuseSortReverse(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Sort" || b.Prim != "Reverse" || !isIntList(a.In) {
			return nil, "", false
		}
		desc, _ := a.Meta["desc"].(bool)
		flipped := newSortNode(a, !desc)
		from, to := orderName(desc), orderName(!desc)
		return []*ir.Node{flipped},
			fmt.Sprintf("Domain rewrote Quicksort (%s) + Reverse → Quicksort (%s). Guaranteed hit.", from, to),
			true
	})
}

// elideReorderBeforeReduce drops a Sort or Reverse whose only consumer is an
// order-insensitive reduction — the reordering is dead work.
func elideReorderBeforeReduce(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Sort" && a.Prim != "Reverse" {
			return nil, "", false
		}
		if !isOrderInsensitiveReduce(b.Prim) {
			return nil, "", false
		}
		// Float reductions are order-sensitive in the bits (float addition is
		// not associative), so the reordering only dies on integer pipelines.
		if typeHasFloat(a.In) || typeHasFloat(b.Out) {
			return nil, "", false
		}
		return []*ir.Node{b},
			fmt.Sprintf("Domain rewrote %s + %s → %s (the reordering cannot change an order-insensitive result). Guaranteed hit.",
				a.Prim, b.Prim, b.Prim),
			true
	})
}

// elideRedundantUnique drops the second of two adjacent Unique nodes —
// deduplication is idempotent.
func elideRedundantUnique(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Unique" || b.Prim != "Unique" {
			return nil, "", false
		}
		return []*ir.Node{a},
			"Domain rewrote Unique + Unique → single Unique (deduplication is idempotent). Guaranteed hit.",
			true
	})
}

// elideUniqueBeforeExtremum drops a Unique feeding Max or Min — removing
// duplicates cannot change an extremum, and both agree on the empty-list
// error.
func elideUniqueBeforeExtremum(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Unique" || (b.Prim != "Max" && b.Prim != "Min") {
			return nil, "", false
		}
		return []*ir.Node{b},
			fmt.Sprintf("Domain rewrote Unique + %s → %s (duplicates cannot move an extremum). Guaranteed hit.", b.Prim, b.Prim),
			true
	})
}

// hoistUniqueBeforeSort swaps Sort ∘ Unique into Unique ∘ Sort: both orders
// yield the sorted distinct values, but deduplicating first sorts d ≤ n
// elements. The node pointers are reused — only their order changes.
func hoistUniqueBeforeSort(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Sort" || b.Prim != "Unique" || !isIntList(a.In) {
			return nil, "", false
		}
		return []*ir.Node{b, a},
			"Domain rewrote Quicksort + Unique → Unique + Quicksort (dedupe first, sort fewer). Guaranteed hit.",
			true
	})
}

// ---------------------------------------------------------------------------
// Map/Filter dead-code and fusion passes
// ---------------------------------------------------------------------------

// elideIdentityMap drops a Map Each whose lambda is the identity, `(x) -> x`
// — often the residue of algebraic simplification (`x + 0`).
func elideIdentityMap(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	var out []*ir.Node
	for _, n := range p.Nodes {
		if n.Prim == "Map Each" {
			if lam := nodeLambda(n); lam != nil && len(lam.Params) == 1 {
				if id, ok := lam.Body.(*ast.Ident); ok && id.Name == lam.Params[0] {
					rewrites = append(rewrites, Rewrite{Message: "Domain rewrote Map Each ((x) -> x) → nothing at all (mapping the identity is dead work). Guaranteed hit."})
					continue
				}
			}
		}
		out = append(out, n)
	}
	p.Nodes = out
	return rewrites
}

// elideMapBeforeCount drops a Map Each feeding Count: mapping preserves
// length. Only fires when the lambda is total, so no runtime error is lost
// with the discarded work.
func elideMapBeforeCount(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Map Each" || b.Prim != "Count" {
			return nil, "", false
		}
		lam := nodeLambda(a)
		if lam == nil || !isTotal(lam.Body) {
			return nil, "", false
		}
		return []*ir.Node{b},
			"Domain rewrote Map Each + Count → Count (mapping preserves length). Guaranteed hit.",
			true
	})
}

// fuseMapMap fuses two adjacent Map Each nodes into one that applies the
// composed lambda per element — one pass, no intermediate list. The composed
// body (g's body with its parameter replaced by f's body) drives *both*
// backends, so it only fires when f's body is total: substitution can place
// f's body behind a short-circuit (or drop it if g ignores its parameter),
// which must not erase a runtime error the naive pipeline reports.
func fuseMapMap(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Map Each" || b.Prim != "Map Each" {
			return nil, "", false
		}
		f, g := nodeLambda(a), nodeLambda(b)
		if f == nil || g == nil || len(f.Params) != 1 || len(g.Params) != 1 {
			return nil, "", false
		}
		if !isTotal(f.Body) {
			return nil, "", false
		}
		composed := &ast.Lambda{
			Params: f.Params,
			Body:   substIdent(g.Body, g.Params[0], f.Body),
			Pos:    f.Pos,
		}
		pos, elem := a.Pos, elemType(a.In)
		fused := &ir.Node{
			Prim:    "Map Each",
			In:      a.In,
			Out:     b.Out,
			Display: "Map Each (fused)",
			Meta:    map[string]any{"lambda": composed},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Map Each", Pos: pos, Msg: err.Error()}
				}
				out := make([]ir.Value, len(items))
				for i, e := range items {
					r, err := eval.EvalLambdaTyped(composed, []*ir.Type{elem}, e)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "Map Each", Pos: pos,
							Msg: fmt.Sprintf("element %d: %v", i, err)}
					}
					out[i] = r
				}
				return out, nil
			},
		}
		return []*ir.Node{fused},
			"Domain rewrote Map Each + Map Each → fused Map Each (one pass, no intermediate list). Guaranteed hit.",
			true
	})
}

// fuseFilterFilter fuses two adjacent Filters into one whose predicate is the
// conjunction — one pass, and elements failing the first predicate never
// reach the second, exactly like the naive pipeline.
func fuseFilterFilter(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Filter" || b.Prim != "Filter" {
			return nil, "", false
		}
		f, g := nodeLambda(a), nodeLambda(b)
		if f == nil || g == nil || len(f.Params) != 1 || len(g.Params) != 1 {
			return nil, "", false
		}
		conj := &ast.Lambda{
			Params: f.Params,
			Body: &ast.BinaryExpr{
				Op:   token.AND,
				Left: f.Body,
				// Rebind g's parameter to f's so both operands see the element.
				Right: substIdent(g.Body, g.Params[0], &ast.Ident{Name: f.Params[0], Pos: g.Pos}),
				Pos:   f.Pos,
			},
			Pos: f.Pos,
		}
		pos := a.Pos
		fused := &ir.Node{
			Prim:    "Filter",
			In:      a.In,
			Out:     a.Out,
			Display: "Filter (fused)",
			Meta:    map[string]any{"lambda": conj},
			Pos:     pos,
			Eval:    filterEval("Filter", conj, elemType(a.In), pos),
		}
		return []*ir.Node{fused},
			"Domain rewrote Filter + Filter → fused Filter (one pass over the list). Guaranteed hit.",
			true
	})
}

// fuseFilterCount replaces Filter ∘ Count with the existing Count Matching
// primitive: count without materializing the filtered list.
func fuseFilterCount(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Filter" || b.Prim != "Count" {
			return nil, "", false
		}
		lam := nodeLambda(a)
		if lam == nil {
			return nil, "", false
		}
		pos, elem := a.Pos, elemType(a.In)
		fused := &ir.Node{
			Prim:    "Count Matching",
			In:      a.In,
			Out:     ir.Int(),
			Display: "Count Matching (from Filter + Count)",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Count Matching", Pos: pos, Msg: err.Error()}
				}
				var c int64
				for i, e := range items {
					keep, err := evalPredicate(lam, elem, e)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "Count Matching", Pos: pos,
							Msg: fmt.Sprintf("element %d: %v", i, err)}
					}
					if keep {
						c++
					}
				}
				return c, nil
			},
		}
		return []*ir.Node{fused},
			"Domain rewrote Filter + Count → Count Matching (count without building the list). Guaranteed hit.",
			true
	})
}

// fuseFoldSum recognizes Fold with Seed: 0 and a plain running-sum lambda —
// `(acc, x) -> acc + x` in either operand order — and replaces it with the
// Sum primitive (Sum of an empty list is 0, matching the seed).
func fuseFoldSum(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for _, list := range nodeLists(p) {
		for i, n := range list {
			if n.Prim != "Fold" {
				continue
			}
			if hasMeasuredArg(n) {
				continue // valid only because the seed is literally 0
			}
			if seed, ok := n.Meta["seed"].(int64); !ok || seed != 0 {
				continue
			}
			lam := nodeLambda(n)
			if lam == nil || len(lam.Params) != 2 || !isSumOf(lam.Body, lam.Params[0], lam.Params[1]) {
				continue
			}
			pos := n.Pos
			list[i] = &ir.Node{
				Prim:    "Sum",
				In:      n.In,
				Out:     ir.Int(),
				Display: "Sum (from Fold)",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsIntSlice(v)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "Sum", Pos: pos, Msg: err.Error()}
					}
					var s int64
					for _, x := range xs {
						s += x
					}
					return s, nil
				},
			}
			rewrites = append(rewrites, Rewrite{
				Message: "Domain rewrote Fold (Seed: 0, running sum) → Sum. Guaranteed hit."})
		}
	}
	return rewrites
}

// elideConstPredicates handles predicates the expression passes folded to a
// literal: an always-true Filter disappears, an always-false Filter returns
// the empty list without scanning, an always-true Count Matching becomes
// Count, and an always-false Count Matching returns 0.
func elideConstPredicates(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite

	// Always-true Filter: pure passthrough — drop the node (top level only,
	// this changes the list length).
	var out []*ir.Node
	for _, n := range p.Nodes {
		if n.Prim == "Filter" {
			if b, ok := constPredicate(n); ok && b {
				rewrites = append(rewrites, Rewrite{Message: "Domain rewrote Filter (always true) → nothing at all. Guaranteed hit."})
				continue
			}
		}
		out = append(out, n)
	}
	p.Nodes = out

	// The in-place rewrites may fire on every node list.
	for _, list := range nodeLists(p) {
		for i, n := range list {
			b, ok := constPredicate(n)
			if !ok {
				continue
			}
			pos := n.Pos
			switch {
			case n.Prim == "Filter" && !b:
				n.Display = "Filter (never matches)"
				n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					if _, err := ir.AsList(v); err != nil {
						return nil, &ir.RuntimeError{Prim: "Filter", Pos: pos, Msg: err.Error()}
					}
					return []ir.Value{}, nil
				}
				rewrites = append(rewrites, Rewrite{Message: "Domain rewrote Filter (always false) → empty list (no scan needed). Guaranteed hit."})
			case n.Prim == "Count Matching" && b:
				list[i] = &ir.Node{
					Prim:    "Count",
					In:      n.In,
					Out:     ir.Int(),
					Display: "Count (from Count Matching, always true)",
					Pos:     pos,
					Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
						items, err := ir.AsList(v)
						if err != nil {
							return nil, &ir.RuntimeError{Prim: "Count", Pos: pos, Msg: err.Error()}
						}
						return int64(len(items)), nil
					},
				}
				rewrites = append(rewrites, Rewrite{Message: "Domain rewrote Count Matching (always true) → Count. Guaranteed hit."})
			case n.Prim == "Count Matching" && !b:
				n.Display = "Count Matching (never matches)"
				n.Eval = func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					if _, err := ir.AsList(v); err != nil {
						return nil, &ir.RuntimeError{Prim: "Count Matching", Pos: pos, Msg: err.Error()}
					}
					return int64(0), nil
				}
				rewrites = append(rewrites, Rewrite{Message: "Domain rewrote Count Matching (always false) → 0 (no scan needed). Guaranteed hit."})
			}
		}
	}
	return rewrites
}

// constPredicate reports whether n's Using: lambda has been folded to a
// boolean literal. It refuses nodes already rewritten (Display changed) so
// the in-place rules fire once.
func constPredicate(n *ir.Node) (value bool, ok bool) {
	if n.Prim != "Filter" && n.Prim != "Count Matching" {
		return false, false
	}
	switch n.Display {
	case "Filter (never matches)", "Count Matching (never matches)":
		return false, false
	}
	lam := nodeLambda(n)
	if lam == nil {
		return false, false
	}
	b, isBool := lam.Body.(*ast.BoolLit)
	if !isBool {
		return false, false
	}
	return b.Value, true
}

// ---------------------------------------------------------------------------
// Shared node constructors / eval helpers
// ---------------------------------------------------------------------------

func orderName(desc bool) string {
	if desc {
		return "Descending"
	}
	return "Ascending"
}

// newSortNode builds a Sort node over List<Int> mirroring the primitive's
// semantics, used when a pass needs to change the ordering.
func newSortNode(orig *ir.Node, desc bool) *ir.Node {
	pos := orig.Pos
	return &ir.Node{
		Prim:      "Sort",
		In:        orig.In,
		Out:       orig.In,
		Display:   "Quicksort, " + orderName(desc) + " (flipped)",
		Swappable: true,
		Meta:      map[string]any{"desc": desc},
		Pos:       pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsIntSlice(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Sort", Pos: pos, Msg: err.Error()}
			}
			out := append([]int64(nil), xs...)
			if desc {
				sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
			} else {
				sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
			}
			return ir.IntsToValue(out), nil
		},
	}
}

// elemType returns a container type's element type, nil when the type is
// unknown — EvalLambdaTyped degrades gracefully on a nil parameter type.
func elemType(t *ir.Type) *ir.Type {
	if t == nil {
		return nil
	}
	return t.Elem
}

// evalPredicate runs a one-parameter predicate lambda, mirroring the prims
// helper of the same name; elem is the statically inferred parameter type
// (nil when unknown).
func evalPredicate(lam *ast.Lambda, elem *ir.Type, e ir.Value) (bool, error) {
	r, err := eval.EvalLambdaTyped(lam, []*ir.Type{elem}, e)
	if err != nil {
		return false, err
	}
	b, ok := r.(bool)
	if !ok {
		return false, fmt.Errorf("predicate did not return a Bool (got %s)", ir.DescribeValue(r))
	}
	return b, nil
}

// filterEval builds a Filter interpreter over a predicate lambda; elem is
// the statically inferred element type (nil when unknown).
func filterEval(prim string, lam *ast.Lambda, elem *ir.Type, pos token.Position) func(*ir.Context, ir.Value) (ir.Value, error) {
	return func(_ *ir.Context, v ir.Value) (ir.Value, error) {
		items, err := ir.AsList(v)
		if err != nil {
			return nil, &ir.RuntimeError{Prim: prim, Pos: pos, Msg: err.Error()}
		}
		out := []ir.Value{}
		for i, e := range items {
			keep, err := evalPredicate(lam, elem, e)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: prim, Pos: pos,
					Msg: fmt.Sprintf("element %d: %v", i, err)}
			}
			if keep {
				out = append(out, e)
			}
		}
		return out, nil
	}
}
