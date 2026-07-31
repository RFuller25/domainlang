package prims

import (
	"fmt"
	"slices"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// The section-D remainder: sequence transforms (Window, Flatten, Enumerate)
// and key-lambda reductions (Count By, Min By, Max By, Sort By), plus the
// standalone Difference reduction. Zip lives with the other From: consumers
// in channel.go.

// ---------------------------------------------------------------------------
// Cursed Technique: Window — List<T> -> List<List<T>>: sliding windows of a
// fixed size, fully contained (n-size+1 windows for step 1). An optional
// second integer sets the step: `Window 3 2` slides by 2.
// ---------------------------------------------------------------------------

var window = &Primitive{
	ID:      "Window",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Window") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Window", pos)
		if err != nil {
			return nil, err
		}
		sizeM, err := requireMeasuredInt(op, args, "Window", "Size", 0, 1, in, pos, "a size", "Window 3")
		if err != nil {
			return nil, err
		}
		stepM, ok, err := measuredInt(op, args, "Window", "Step", 1, 1, in, pos)
		if err != nil {
			return nil, err
		}
		if !ok {
			stepM = Measured{Lit: 1, Min: 1, Prim: "Window", Name: "Step", Pos: pos}
		}
		if err := checkLiteralBounds(sizeM, stepM); err != nil {
			return nil, err
		}

		display := "Window " + sizeM.Describe()
		if stepM.IsMeasured() || stepM.Lit != 1 {
			display += fmt.Sprintf(" (step %s)", stepM.Describe())
		}
		meta := map[string]any{}
		sizeM.Meta(meta, "size")
		stepM.Meta(meta, "step")
		return &ir.Node{
			Prim:    "Window",
			In:      in,
			Out:     ir.List(ir.List(elem)),
			Display: display,
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Window", pos, "%v", err)
				}
				size, err := sizeM.Resolve(v)
				if err != nil {
					return nil, err
				}
				step, err := stepM.Resolve(v)
				if err != nil {
					return nil, err
				}
				out := []ir.Value{}
				for i := int64(0); i+size <= int64(len(xs)); i += step {
					out = append(out, append([]ir.Value(nil), xs[i:i+size]...))
				}
				return out, nil
			},
		}, nil
	},
}

// checkLiteralBounds rejects a literal size or step below 1 at resolve time,
// keeping the message Window and Sliding Reduce have always produced when both
// are written in the phrase. A measured argument has no value yet, so its
// bound is checked in measureWindow instead.
func checkLiteralBounds(sizeM, stepM Measured) error {
	if sizeM.IsMeasured() || stepM.IsMeasured() {
		if !sizeM.IsMeasured() && sizeM.Lit < 1 {
			return sizeM.atLeast(1, sizeM.Lit, "")
		}
		if !stepM.IsMeasured() && stepM.Lit < 1 {
			return stepM.atLeast(1, stepM.Lit, "")
		}
		return nil
	}
	if sizeM.Lit < 1 || stepM.Lit < 1 {
		return &ResolveError{Pos: sizeM.Pos, Msg: fmt.Sprintf(
			"%s size and step must be >= 1, got size %d step %d",
			sizeM.Prim, sizeM.Lit, stepM.Lit)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cursed Technique: Flatten — List<List<T>> -> List<T>.
// ---------------------------------------------------------------------------

var flatten = &Primitive{
	ID:      "Flatten",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Flatten") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KList || in.Elem == nil || in.Elem.Kind != ir.KList {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Flatten expects List<List<T>>, got %s", in)}
		}
		return &ir.Node{
			Prim:    "Flatten",
			In:      in,
			Out:     in.Elem,
			Display: "Flatten",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				groups, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Flatten", pos, "%v", err)
				}
				out := []ir.Value{}
				for i, g := range groups {
					inner, err := ir.AsList(g)
					if err != nil {
						return nil, runtimeErr("Flatten", pos, "group %d: %v", i, err)
					}
					out = append(out, inner...)
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Enumerate — List<T> -> List<(Int, T)>: pair every element
// with its 0-based index. With Int elements the pairs are points, so
// prow/pcol read the index/value.
// ---------------------------------------------------------------------------

var enumerate = &Primitive{
	ID:      "Enumerate",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Enumerate") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Enumerate", pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Enumerate",
			In:      in,
			Out:     ir.List(ir.Tuple(ir.Int(), elem)),
			Display: "Enumerate",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Enumerate", pos, "%v", err)
				}
				out := make([]ir.Value, len(xs))
				for i, x := range xs {
					out[i] = []ir.Value{int64(i), x}
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Count By — List<T> x (T -> K) -> Map<K, Int>: frequency
// map of the lambda's key, keys in first-seen order.
// ---------------------------------------------------------------------------

var countBy = &Primitive{
	ID:      "Count By",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Count") && hasWord(op, "By") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Count By", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "Count By", pos)
		if err != nil {
			return nil, err
		}
		keyType, err := typecheck.LambdaType(lam, append([]*ir.Type{elem}, ambientTypes()...)...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Count By: " + err.Error()}
		}
		if err := requireKeyable(keyType, "Count By", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Count By",
			In:      in,
			Out:     ir.Map(keyType, ir.Int()),
			Display: "Count By",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Count By", pos, "%v", err)
				}
				m := ir.NewMapValue()
				for i, x := range xs {
					k, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{elem}, ambientTypes()...), append([]ir.Value{x}, ambientArgs()...)...)
					if err != nil {
						return nil, runtimeErr("Count By", pos, "item %d: %v", i, err)
					}
					if cur, ok := m.Get(k); ok {
						m.Put(k, cur.(int64)+1)
					} else {
						m.Put(k, int64(1))
					}
				}
				return m, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Min By / Max By — List<T> x (T -> Int) -> T: the element
// with the smallest/largest key (first wins on ties; empty list is an error).
// ---------------------------------------------------------------------------

var minBy = keyedExtremum("Min By", func(k, best int64) bool { return k < best })
var maxBy = keyedExtremum("Max By", func(k, best int64) bool { return k > best })

func keyedExtremum(id string, better func(k, best int64) bool) *Primitive {
	words := splitID(id)
	return &Primitive{
		ID:      id,
		Keyword: "Maximum Technique",
		Match: func(op *ast.Operation) bool {
			return hasWord(op, words[0]) && hasWord(op, "By")
		},
		Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
			elem, err := listElem(in, id, pos)
			if err != nil {
				return nil, err
			}
			lam, err := requireLambda(args, 1, id, pos)
			if err != nil {
				return nil, err
			}
			keyType, err := typecheck.LambdaType(lam, append([]*ir.Type{elem}, ambientTypes()...)...)
			if err != nil {
				return nil, &ResolveError{Pos: pos, Msg: id + ": " + err.Error()}
			}
			if !keyType.Equal(ir.Int()) {
				return nil, &ResolveError{Pos: pos,
					Msg: fmt.Sprintf("%s key lambda must return Int, got %s", id, keyType)}
			}
			return &ir.Node{
				Prim:    id,
				In:      in,
				Out:     elem,
				Display: id,
				Meta:    map[string]any{"lambda": lam},
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr(id, pos, "%v", err)
					}
					if len(xs) == 0 {
						return nil, runtimeErr(id, pos, "%s of an empty list is undefined", id)
					}
					best := xs[0]
					bestKey, err := intKey(lam, elem, xs[0])
					if err != nil {
						return nil, runtimeErr(id, pos, "item 0: %v", err)
					}
					for i, x := range xs[1:] {
						k, err := intKey(lam, elem, x)
						if err != nil {
							return nil, runtimeErr(id, pos, "item %d: %v", i+1, err)
						}
						if better(k, bestKey) {
							best, bestKey = x, k
						}
					}
					return best, nil
				},
			}, nil
		},
	}
}

// intKey evaluates a key lambda expected to produce an Int; elem is the
// statically inferred parameter type (nil when unknown).
func intKey(lam *ast.Lambda, elem *ir.Type, x ir.Value) (int64, error) {
	k, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{elem}, ambientTypes()...), append([]ir.Value{x}, ambientArgs()...)...)
	if err != nil {
		return 0, err
	}
	n, ok := k.(int64)
	if !ok {
		return 0, fmt.Errorf("key lambda did not return an Int (got %s)", ir.DescribeValue(k))
	}
	return n, nil
}

// anyKey is intKey's untyped sibling: it hands back whatever ordered value
// the key lambda produced, for the comparison in ir.Compare to order. The
// key's type was checked at resolve time.
func anyKey(lam *ast.Lambda, elem *ir.Type, x ir.Value) (ir.Value, error) {
	return eval.EvalLambdaTyped(lam,
		append([]*ir.Type{elem}, ambientTypes()...),
		append([]ir.Value{x}, ambientArgs()...)...)
}

func splitID(id string) []string { return strings.Split(id, " ") }

// ---------------------------------------------------------------------------
// Domain Expansion: Sort By — List<T> x (T -> Int) -> List<T>: stable sort by
// the lambda's key, ascending (Descending modifier flips).
// ---------------------------------------------------------------------------

var sortBy = &Primitive{
	ID:      "Sort By",
	Keyword: "Domain Expansion",
	Match: func(op *ast.Operation) bool {
		return (hasWord(op, "Sort") || hasWord(op, "Quicksort")) && hasWord(op, "By")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Sort By", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "Sort By", pos)
		if err != nil {
			return nil, err
		}
		keyType, err := typecheck.LambdaType(lam, append([]*ir.Type{elem}, ambientTypes()...)...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Sort By: " + err.Error()}
		}
		// Any ordered key: Int, Float, Text, or a tuple of them. A tuple key
		// is how a tiebreak gets written — `tuple(r.group, r.score)` sorts by
		// group and then by score, in one pass.
		if !ir.Ordered(keyType) {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Sort By key lambda must return an ordered type "+
					"(Int, Float, Text, or a Tuple of them), got %s", keyType)}
		}
		desc := hasModifier(op, "Descending")
		order := "Ascending"
		if desc {
			order = "Descending"
		}
		return &ir.Node{
			Prim:      "Sort By",
			In:        in,
			Out:       in,
			Display:   "Sort By, " + order,
			Swappable: true,
			Meta:      map[string]any{"lambda": lam, "desc": desc, "key": keyType.String()},
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Sort By", pos, "%v", err)
				}
				keys := make([]ir.Value, len(xs))
				for i, x := range xs {
					if keys[i], err = anyKey(lam, elem, x); err != nil {
						return nil, runtimeErr("Sort By", pos, "item %d: %v", i, err)
					}
				}
				idx := make([]int, len(xs))
				for i := range idx {
					idx[i] = i
				}
				slices.SortStableFunc(idx, func(a, b int) int {
					c := ir.Compare(keys[a], keys[b])
					if desc {
						return -c
					}
					return c
				})
				out := make([]ir.Value, len(xs))
				for i, j := range idx {
					out[i] = xs[j]
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Difference (standalone) — List<List<T>> -> Set<T>
// (T keyable): the first group's elements not present in any later group, in
// the first group's order. (The two-channel form via From: still exists.)
// ---------------------------------------------------------------------------

var differenceAll = &Primitive{
	ID:      "Difference",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Difference") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KList || in.Elem == nil || in.Elem.Kind != ir.KList {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Difference expects List<List<T>>, got %s", in)}
		}
		elem := in.Elem.Elem
		if err := requireKeyable(elem, "Difference", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "DifferenceAll",
			In:      in,
			Out:     ir.Set(elem),
			Display: "Difference",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				groups, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Difference", pos, "%v", err)
				}
				if len(groups) == 0 {
					return ir.NewSetValue(), nil
				}
				first, err := ir.AsList(groups[0])
				if err != nil {
					return nil, runtimeErr("Difference", pos, "group 0: %v", err)
				}
				rest := ir.NewSetValue()
				for i, g := range groups[1:] {
					inner, err := ir.AsList(g)
					if err != nil {
						return nil, runtimeErr("Difference", pos, "group %d: %v", i+1, err)
					}
					for _, e := range inner {
						rest.Add(e)
					}
				}
				out := ir.NewSetValue()
				for _, e := range first {
					if !rest.Has(e) {
						out.Add(e)
					}
				}
				return out, nil
			},
		}, nil
	},
}
