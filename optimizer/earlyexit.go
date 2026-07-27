package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
)

// Rewrites that reach the early-exit and single-pass primitives from the naive
// spellings people actually write.
//
//	Map Each(f) + Sum        → Sum By(f)      folds as it maps, no mapped list
//	Map Each(f) + Product    → Product By(f)  likewise
//	Filter(p)  + Take Item 0 → Find(p)        stops at the first match
//	Zip + Map Each(f)        → ZipMap(f)      no intermediate tuple list
//
// The first and last are unconditional: they neither reorder lambda calls nor
// substitute one body into another, so every evaluation the naive pipeline
// performed still happens, in the same order, and a lambda that fails still
// fails on the same element. The Filter one is different — it *stops early*,
// which would swallow a failure the full scan would have reported, so it
// requires a total predicate exactly as fuseMapMap requires a total body.

// fuseMapReduceBy turns Map Each feeding Sum or Product into the keyed
// reduction, folding each mapped value as it is produced instead of collecting
// them all first.
func fuseMapReduceBy(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Map Each" {
			return nil, "", false
		}
		var id string
		var identity int64
		var step func(acc, k int64) int64
		switch b.Prim {
		case "Sum":
			id, identity, step = "Sum By", 0, func(acc, k int64) int64 { return acc + k }
		case "Product":
			id, identity, step = "Product By", 1, func(acc, k int64) int64 { return acc * k }
		default:
			return nil, "", false
		}
		// Int-only: the Float reductions have their own nodes, and the keyed
		// forms fold int64 accumulators.
		if !isIntList(a.Out) || b.Out == nil || !b.Out.Equal(ir.Int()) {
			return nil, "", false
		}
		lam := nodeLambda(a)
		if lam == nil || len(lam.Params) != 1 {
			return nil, "", false
		}
		elem := a.In.Elem
		pos := a.Pos
		fused := &ir.Node{
			Prim:    id,
			In:      a.In,
			Out:     ir.Int(),
			Display: id + " (fused)",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: id, Pos: pos, Msg: err.Error()}
				}
				acc := identity
				for i, x := range xs {
					r, err := eval.EvalLambdaTyped(lam, []*ir.Type{elem}, x)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: id, Pos: pos,
							Msg: fmt.Sprintf("item %d: %v", i, err)}
					}
					k, ok := r.(int64)
					if !ok {
						return nil, &ir.RuntimeError{Prim: id, Pos: pos,
							Msg: fmt.Sprintf("item %d: lambda did not return an Int (got %s)", i, ir.DescribeValue(r))}
					}
					acc = step(acc, k)
				}
				return acc, nil
			},
		}
		return []*ir.Node{fused},
			fmt.Sprintf("Domain rewrote Map Each + %s → %s (folds as it maps, no intermediate list). Guaranteed hit.",
				b.Prim, id),
			true
	})
}

// takeItemEmptyMessage is what `Take Item 0` reports on an empty list. The
// fused Find carries it so the rewrite is invisible on the failure path too.
const takeItemEmptyMessage = "index 0 out of range (length 0)"

// fuseFilterFirst turns Filter + Take Item 0 into Find: the naive pair tests
// every element and builds the list of all matches only to keep the first one.
//
// Find stops at the first match, so it only fires for a predicate that cannot
// fail — otherwise a failure on a later element, which the full Filter scan
// would have reported, would silently disappear. The fused node reports the
// *Take Item* message on no match, so a program that hits the empty case fails
// the way it did before the rewrite.
func fuseFilterFirst(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Filter" || b.Prim != "Take Item" {
			return nil, "", false
		}
		if idx, ok := b.Meta["index"].(int); !ok || idx != 0 {
			return nil, "", false
		}
		lam := nodeLambda(a)
		if lam == nil || len(lam.Params) != 1 || !isTotal(lam.Body) {
			return nil, "", false
		}
		elem := a.In.Elem
		pos := a.Pos
		fused := &ir.Node{
			Prim:    "Find",
			In:      a.In,
			Out:     b.Out,
			Display: "Cursed First Match (from Filter + Take Item 0)",
			Meta:    map[string]any{"lambda": lam, "nomatch": takeItemEmptyMessage},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Find", Pos: pos, Msg: err.Error()}
				}
				for i, x := range xs {
					r, err := eval.EvalLambdaTyped(lam, []*ir.Type{elem}, x)
					if err != nil {
						return nil, &ir.RuntimeError{Prim: "Find", Pos: pos,
							Msg: fmt.Sprintf("element %d: %v", i, err)}
					}
					hit, ok := r.(bool)
					if !ok {
						return nil, &ir.RuntimeError{Prim: "Find", Pos: pos,
							Msg: fmt.Sprintf("element %d: predicate did not return a Bool", i)}
					}
					if hit {
						return x, nil // the rest of the list is never touched
					}
				}
				return nil, &ir.RuntimeError{Prim: "Find", Pos: pos, Msg: takeItemEmptyMessage}
			},
		}
		return []*ir.Node{fused},
			"Domain rewrote Filter + Take Item 0 → Cursed First Match (stops at the first hit). Guaranteed hit.",
			true
	})
}

// fuseZipWith turns Zip + Map Each into one node that builds each pair locally
// and applies the lambda to it, so the list of tuples is never materialized.
// The lambda is untouched — it still takes the tuple — so this is purely the
// removal of an intermediate slice.
func fuseZipWith(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Zip" || b.Prim != "Map Each" {
			return nil, "", false
		}
		lam := nodeLambda(b)
		if lam == nil || len(lam.Params) != 1 {
			return nil, "", false
		}
		froms, _ := a.Meta["from"].([]string)
		if len(froms) != 2 {
			return nil, "", false
		}
		zipEval, mapEval := a.Eval, b.Eval
		return []*ir.Node{{
				Prim:    "ZipMap",
				In:      a.In,
				Out:     b.Out,
				Display: "Zip + Map Each (fused)",
				Meta:    map[string]any{"from": froms, "lambda": lam, "tuple": a.Out.Elem},
				Pos:     a.Pos,
				// The interpreter keeps running the two steps back to back: its
				// Zip builds a []Value per pair either way, so there is nothing
				// to win here. The saving is in the compiled backend, which emits
				// a single loop with no []tuple in it (codegen.emitZipWith).
				Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
					zipped, err := zipEval(ctx, v)
					if err != nil {
						return nil, err
					}
					return mapEval(ctx, zipped)
				},
			}},
			"Domain rewrote Zip + Map Each → one fused pass (no intermediate tuple list). Guaranteed hit.",
			true
	})
}

// ---------------------------------------------------------------------------
// Constant predicates on the early-exit primitives.
//
// simplifyLambdaBodies folds a predicate to a literal often enough that the
// structural passes have to know what a literal predicate means for each
// primitive that takes one. elideConstPredicates covers Filter and Count
// Matching; this covers Any, All, Take While and Drop While.
// ---------------------------------------------------------------------------

func elideConstEarlyExits(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite

	// Whole-list passthroughs disappear. This changes the list's length, so
	// it is confined to the top level (see nodeLists).
	var out []*ir.Node
	for _, n := range p.Nodes {
		// Only the two prefix transforms can vanish: Take While (always true)
		// and Drop While (never true) each hand back the list unchanged. Any
		// and All change the value's *type*, so they always leave a node
		// behind, however constant their predicate is.
		if b, ok := literalPredicate(n); ok &&
			((n.Prim == "Take While" && b) || (n.Prim == "Drop While" && !b)) {
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain rewrote %s (constant predicate) → nothing at all. Guaranteed hit.", n.Prim)})
			continue
		}
		out = append(out, n)
	}
	p.Nodes = out

	// The rest are in-place node swaps, safe on every list.
	for _, list := range nodeLists(p) {
		for i, n := range list {
			b, ok := literalPredicate(n)
			if !ok {
				continue
			}
			switch n.Prim {
			case "Take While", "Drop While":
				// The boundary is at index 0, so the result is empty.
				list[i] = emptyListNode(n)
				rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
					"Domain rewrote %s (constant predicate) → the empty list, unscanned. Guaranteed hit.", n.Prim)})
			case "Any", "All":
				// Any(false) is false and All(true) is true whatever the list
				// holds; the opposite pairing reduces to "is it non-empty?".
				if (n.Prim == "All") == b {
					list[i] = constBoolNode(n, b)
					rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
						"Domain rewrote %s (constant predicate) → a constant, unscanned. Guaranteed hit.", n.Prim)})
				} else {
					list[i] = nonEmptyNode(n, b)
					rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
						"Domain rewrote %s (constant predicate) → an emptiness test. Guaranteed hit.", n.Prim)})
				}
			}
		}
	}
	return rewrites
}

func emptyListNode(n *ir.Node) *ir.Node {
	return &ir.Node{
		Prim: n.Prim, In: n.In, Out: n.Out,
		Display: n.Prim + " (empty)", Pos: n.Pos,
		Meta: map[string]any{"empty": true},
		Eval: func(_ *ir.Context, _ ir.Value) (ir.Value, error) { return []ir.Value{}, nil },
	}
}

func constBoolNode(n *ir.Node, answer bool) *ir.Node {
	return &ir.Node{
		Prim: n.Prim, In: n.In, Out: ir.Bool(),
		Display: fmt.Sprintf("%s (constant %v)", n.Prim, answer), Pos: n.Pos,
		Meta: map[string]any{"const": answer},
		Eval: func(_ *ir.Context, _ ir.Value) (ir.Value, error) { return answer, nil },
	}
}

// nonEmptyNode answers "is the list non-empty?", mapped through want: Any with
// an always-true predicate is true exactly when the list is non-empty, and All
// with an always-false one is true exactly when it is empty.
func nonEmptyNode(n *ir.Node, want bool) *ir.Node {
	prim, pos := n.Prim, n.Pos
	return &ir.Node{
		Prim: prim, In: n.In, Out: ir.Bool(),
		Display: prim + " (emptiness test)", Pos: pos,
		Meta: map[string]any{"nonempty": want},
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: prim, Pos: pos, Msg: err.Error()}
			}
			return (len(xs) > 0) == want, nil
		},
	}
}

// literalPredicate reports the value of a predicate the expression simplifier
// folded to a Bool literal, for the primitives handled above. Nodes this pass
// has already rewritten carry no lambda, so it cannot fire on them twice.
func literalPredicate(n *ir.Node) (value bool, ok bool) {
	switch n.Prim {
	case "Any", "All", "Take While", "Drop While":
	default:
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
