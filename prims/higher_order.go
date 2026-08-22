package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// This file holds the v0.2 higher-order primitives: the ones that consume a
// Using: lambda. Their output types are computed via the typecheck package
// (the M1 inference foundation), and their interpreters run lambda bodies via
// the eval package.

// listElem is the element type a list-shaped primitive consumes. A Set is
// accepted wherever a List is: its insertion order is already the order it
// renders and iterates in, so reading one as a sequence is unambiguous. The
// result is a List — the primitive builds ir.List(elem) — because a transform
// may map two distinct elements onto the same value, and silently
// deduplicating would lose data the program asked for.
func listElem(in *ir.Type, prim string, pos token.Position) (*ir.Type, error) {
	if in != nil {
		switch in.Kind {
		case ir.KList, ir.KSet:
			return in.Elem, nil
		case ir.KMap:
			// A Map reads as its entries, in insertion order — the order it
			// already renders and iterates in, and the shape Convert To
			// Entries produces. Group By and Count By are among the most
			// reached-for reductions, so without this almost every program
			// that uses one spends a Convert To Entries getting back to a
			// shape the rest of the language accepts.
			return ir.Tuple(in.Key, in.Elem), nil
		}
	}
	return nil, &ResolveError{Pos: pos,
		Msg: fmt.Sprintf("%s expects a List input, got %s", prim, in)}
}

// seqOut is the output type of a primitive that keeps its input's *shape* —
// Filter, Unique, Sort By, Take/Drop While, Merge Ranges, Partition.
//
// For a List that is the input type unchanged. For a Set or a Map it is a
// List of the element type, because these operations are list-producing:
// Filter drops elements and Sort By imposes an order, neither of which a Set
// or a Map has a place to put. Claiming the input type back was a lie the
// interpreter already told — it rendered a filtered Set as a list — and one
// the compiler could not tell, so it emitted Go that did not build.
func seqOut(in, elem *ir.Type) *ir.Type {
	if in != nil && in.Kind == ir.KList {
		return in
	}
	return ir.List(elem)
}

// requireLambda fetches the Using: lambda and checks its arity. An indented
// pipeline body counts as one: it is the same signature written as stages
// instead of an expression, so every primitive that calls this accepts both
// spellings without knowing there are two (see prims/block.go).
func requireLambda(args ArgSet, arity int, prim string, pos token.Position) (*ast.Lambda, error) {
	lam, ok := args.Lambda("Using")
	if !ok {
		if args.hasBlock() {
			blockLam, err := args.res.blockLambda(args, args.block, arity, prim, pos)
			if err != nil {
				return nil, err
			}
			*args.blockUse = true
			return blockLam, nil
		}
		// NeedsBlock is now true in a second sense: an indented block does not
		// merely *precede* the lambda, it can *be* the lambda. Either way the
		// REPL should keep waiting for the following lines rather than drop the
		// statement.
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("%s requires a Using: lambda", prim), NeedsBlock: true}
	}
	if args.hasBlock() {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s takes either a Using: lambda or an indented pipeline body, not both", prim)}
	}
	wantArity := arity + ambientDepth()
	if len(lam.Params) != wantArity {
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("%s lambda must take %d parameter(s), got %d", prim, wantArity, len(lam.Params))}
	}
	// Params: names a *body's* parameters. Beside a written lambda it names
	// nothing, and silently ignoring it would let a program say one thing and
	// do another.
	if _, ok := args.Idents("Params"); ok {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s: Params: names the parameters of an indented body, and this Using: is a "+
				"lambda that already names its own", prim)}
	}
	return lam, nil
}

// requireKeyable checks a type is usable as a Map/Set key: Int, Text, or a
// Tuple/Record of keyable types (ir.Keyable). Lists stay unkeyable — their
// compiled representation is a Go slice, which cannot be a map key.
func requireKeyable(t *ir.Type, prim string, pos token.Position) error {
	if !ir.Keyable(t) {
		return &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("%s needs keyable keys/elements (Int, Text, or Tuples/Records of them), got %s", prim, t)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cursed Technique: Map Each — List<T> x (T -> U) -> List<U>.
// ---------------------------------------------------------------------------

var mapEach = &Primitive{
	ID:      "Map Each",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Map") && hasWord(op, "Each") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Map Each", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "Map Each", pos)
		if err != nil {
			return nil, err
		}
		outElem, err := typecheck.LambdaType(lam, append([]*ir.Type{elem}, ambientTypes()...)...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Map Each: " + err.Error()}
		}
		return &ir.Node{
			Prim:    "Map Each",
			In:      in,
			Out:     ir.List(outElem),
			Display: "Map Each",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Map Each", pos, "%v", err)
				}
				out := make([]ir.Value, len(items))
				for i, e := range items {
					r, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{elem}, ambientTypes()...), append([]ir.Value{e}, ambientArgs()...)...)
					if err != nil {
						return nil, runtimeErr("Map Each", pos, "element %d: %v", i, err)
					}
					out[i] = r
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Filter — List<T> x (T -> Bool) -> List<T>.
// ---------------------------------------------------------------------------

var filter = &Primitive{
	ID:      "Filter",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Filter") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Filter", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "Filter", pos)
		if err != nil {
			return nil, err
		}
		if err := requirePredicate(lam, elem, "Filter", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Filter",
			In:      in,
			Out:     seqOut(in, elem),
			Display: "Filter",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Filter", pos, "%v", err)
				}
				var out []ir.Value
				for i, e := range items {
					keep, err := evalPredicate(lam, elem, e)
					if err != nil {
						return nil, runtimeErr("Filter", pos, "element %d: %v", i, err)
					}
					if keep {
						out = append(out, e)
					}
				}
				if out == nil {
					out = []ir.Value{}
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Unique — List<T> -> List<T> (dedup, order-preserving).
// ---------------------------------------------------------------------------

var unique = &Primitive{
	ID:      "Unique",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Unique") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Unique", pos)
		if err != nil {
			return nil, err
		}
		if err := requireKeyable(elem, "Unique", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Unique",
			In:      in,
			Out:     seqOut(in, elem),
			Display: "Unique",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Unique", pos, "%v", err)
				}
				return ir.SetFromList(items).Elems(), nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Count Matching — List<T> x (T -> Bool) -> Int.
// ---------------------------------------------------------------------------

var countMatching = &Primitive{
	ID:      "Count Matching",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Count") && hasWord(op, "Matching") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Count Matching", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "Count Matching", pos)
		if err != nil {
			return nil, err
		}
		if err := requirePredicate(lam, elem, "Count Matching", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Count Matching",
			In:      in,
			Out:     ir.Int(),
			Display: "Count Matching",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Count Matching", pos, "%v", err)
				}
				var n int64
				for i, e := range items {
					keep, err := evalPredicate(lam, elem, e)
					if err != nil {
						return nil, runtimeErr("Count Matching", pos, "element %d: %v", i, err)
					}
					if keep {
						n++
					}
				}
				return n, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Count — List<T>|Set<T> -> Int (cardinality).
// ---------------------------------------------------------------------------

var count = &Primitive{
	ID:      "Count",
	Keyword: "Maximum Technique",
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Count") && !hasWord(op, "Matching") &&
			!hasWord(op, "Cells") && !hasWord(op, "By")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		// Count reads the length rather than the elements, so it takes any
		// sequence — the Map case is what makes `len(m)` in the toolbox
		// spelled the way that page always claimed it was.
		if _, err := listElem(in, "Count", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Count",
			In:      in,
			Out:     ir.Int(),
			Display: "Count",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				switch x := v.(type) {
				case []ir.Value:
					return int64(len(x)), nil
				case *ir.SetValue:
					return int64(x.Len()), nil
				case *ir.MapValue:
					return int64(x.Len()), nil
				default:
					return nil, runtimeErr("Count", pos, "expected a List, Set or Map, got %s", ir.DescribeValue(v))
				}
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Max / Min / Product — List<Int> -> Int.
// ---------------------------------------------------------------------------

var maxPrim = reduceIntPrim("Max", func(op *ast.Operation) bool {
	return hasWord(op, "Max") || hasWord(op, "Maximum")
}, func(acc, x int64) int64 { return max(acc, x) })

var minPrim = reduceIntPrim("Min", func(op *ast.Operation) bool {
	// "Spanning" is what tells this apart from Domain Expansion: Minimum
	// Spanning Tree, the way "Group" and "Each" tell Sum from Sum Each Group.
	// Without it a prefix-free `Minimum Spanning Tree` names two primitives
	// under two keywords, which is ambiguous rather than merely surprising.
	return (hasWord(op, "Min") || hasWord(op, "Minimum")) && !hasWord(op, "Spanning")
}, func(acc, x int64) int64 { return min(acc, x) })

var product = reduceIntPrim("Product", func(op *ast.Operation) bool {
	return hasWord(op, "Product")
}, func(acc, x int64) int64 { return acc * x })

// reduceIntPrim builds a List<Int> -> Int reduction seeded with the first
// element. Product is the exception (identity 1), handled by seeding behavior
// below: all three seed with the first element, which is correct for Product
// too (acc starts at xs[0]).
func reduceIntPrim(id string, match func(*ast.Operation) bool, step func(acc, x int64) int64) *Primitive {
	return &Primitive{
		ID:      id,
		Keyword: "Maximum Technique",
		Match:   match,
		Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
			if in != nil && in.Equal(ir.List(ir.Float())) {
				return buildFloatReduce(id, pos), nil
			}
			want := ir.List(ir.Int())
			if !in.Equal(want) {
				return nil, typeErr(pos, id, want, in)
			}
			return &ir.Node{
				Prim:    id,
				In:      want,
				Out:     ir.Int(),
				Display: id,
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsIntSlice(v)
					if err != nil {
						return nil, runtimeErr(id, pos, "%v", err)
					}
					if len(xs) == 0 {
						return nil, runtimeErr(id, pos, "%s of an empty list is undefined", id)
					}
					acc := xs[0]
					for _, x := range xs[1:] {
						acc = step(acc, x)
					}
					return acc, nil
				},
			}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Maximum Technique: Fold — List<T> x seed x (acc, T -> acc) -> acc.
// ---------------------------------------------------------------------------

var fold = &Primitive{
	ID:      "Fold",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Fold") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Fold", pos)
		if err != nil {
			return nil, err
		}
		seedM, err := foldSeed(args, in, pos)
		if err != nil {
			return nil, err
		}
		seedType := seedM.Type
		lam, err := requireLambda(args, 2, "Fold", pos)
		if err != nil {
			return nil, err
		}
		accType, err := typecheck.LambdaType(lam, append([]*ir.Type{seedType, elem}, ambientTypes()...)...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Fold: " + err.Error()}
		}
		if !accType.Equal(seedType) {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Fold lambda must return the accumulator type %s, got %s", seedType, accType)}
		}
		meta := map[string]any{"lambda": lam}
		seedM.Meta(meta, "seed")
		return &ir.Node{
			Prim:    "Fold",
			In:      in,
			Out:     seedType,
			Display: "Fold",
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Fold", pos, "%v", err)
				}
				acc, err := seedM.Resolve(v)
				if err != nil {
					return nil, err
				}
				acc = ownAccumulator(lam, acc)
				for i, e := range items {
					acc, err = eval.EvalLambdaTyped(lam, append([]*ir.Type{seedType, elem}, ambientTypes()...), append([]ir.Value{acc, e}, ambientArgs()...)...)
					if err != nil {
						return nil, runtimeErr("Fold", pos, "element %d: %v", i, err)
					}
				}
				return acc, nil
			},
		}, nil
	},
}

// foldSeed reads the accumulator's starting value. It is a measured argument,
// and the one where that widens what Fold can do rather than only where the
// value may come from: a literal seed can only be Int or Text — the two a
// named argument can spell — while a measured one takes its type from the
// lambda body, so `Seed: (xs) -> tuple(0, first(xs))` gives a fold with a
// tuple accumulator. The lambda's own return type is then checked against it
// as before, so nothing else about Fold changes.
func foldSeed(args ArgSet, in *ir.Type, pos token.Position) (MeasuredValue, error) {
	if !args.Has("Seed") {
		return MeasuredValue{}, &ResolveError{Pos: pos,
			Msg:        "Fold requires a Seed: (an Int or Text literal, or a lambda over the current value)",
			NeedsBlock: true}
	}
	return measuredValue(args, "Fold", "Seed", in, pos)
}

// ---------------------------------------------------------------------------
// Maximum Technique: Group By — List<T> x (T -> K) -> Map<K, List<T>>.
// ---------------------------------------------------------------------------

var groupBy = &Primitive{
	ID:      "Group By",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Group") && hasWord(op, "By") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Group By", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "Group By", pos)
		if err != nil {
			return nil, err
		}
		keyType, err := typecheck.LambdaType(lam, append([]*ir.Type{elem}, ambientTypes()...)...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Group By: " + err.Error()}
		}
		if err := requireKeyable(keyType, "Group By", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Group By",
			In:      in,
			Out:     ir.Map(keyType, ir.List(elem)),
			Display: "Group By",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Group By", pos, "%v", err)
				}
				m := ir.NewMapValue()
				for i, e := range items {
					k, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{elem}, ambientTypes()...), append([]ir.Value{e}, ambientArgs()...)...)
					if err != nil {
						return nil, runtimeErr("Group By", pos, "element %d: %v", i, err)
					}
					existing, _ := m.Get(k)
					var bucket []ir.Value
					if existing != nil {
						bucket = existing.([]ir.Value)
					}
					m.Put(k, append(bucket, e))
				}
				return m, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Intersect / Union — List<List<T>> -> Set<T>.
// ---------------------------------------------------------------------------

var intersectAll = setReducePrim("Intersect", func(op *ast.Operation) bool {
	return hasWord(op, "Intersect")
}, ir.SetIntersect)

var unionAll = setReducePrim("Union", func(op *ast.Operation) bool {
	return hasWord(op, "Union")
}, ir.SetUnion)

// setReducePrim builds a List<List<T>> -> Set<T> reduction over groups.
func setReducePrim(id string, match func(*ast.Operation) bool, combine func(a, b *ir.SetValue) *ir.SetValue) *Primitive {
	return &Primitive{
		ID:      id,
		Keyword: "Maximum Technique",
		Match:   match,
		Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
			if in == nil || in.Kind != ir.KList || in.Elem == nil || in.Elem.Kind != ir.KList {
				return nil, &ResolveError{Pos: pos,
					Msg: fmt.Sprintf("%s expects a List of lists (groups), got %s", id, in)}
			}
			elem := in.Elem.Elem
			if err := requireKeyable(elem, id, pos); err != nil {
				return nil, err
			}
			return &ir.Node{
				Prim:    id,
				In:      in,
				Out:     ir.Set(elem),
				Display: id + " (over groups)",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					groups, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr(id, pos, "%v", err)
					}
					if len(groups) == 0 {
						return ir.NewSetValue(), nil
					}
					acc, err := setFromGroup(groups[0], id, pos)
					if err != nil {
						return nil, err
					}
					for _, g := range groups[1:] {
						s, err := setFromGroup(g, id, pos)
						if err != nil {
							return nil, err
						}
						acc = combine(acc, s)
					}
					return acc, nil
				},
			}, nil
		},
	}
}

func setFromGroup(g ir.Value, id string, pos token.Position) (*ir.SetValue, error) {
	items, err := ir.AsList(g)
	if err != nil {
		return nil, runtimeErr(id, pos, "%v", err)
	}
	return ir.SetFromList(items), nil
}

// ---------------------------------------------------------------------------
// Shared predicate helpers.
// ---------------------------------------------------------------------------

func requirePredicate(lam *ast.Lambda, elem *ir.Type, prim string, pos token.Position) error {
	bodyType, err := typecheck.LambdaType(lam, append([]*ir.Type{elem}, ambientTypes()...)...)
	if err != nil {
		return &ResolveError{Pos: pos, Msg: prim + ": " + err.Error()}
	}
	if !bodyType.Equal(ir.Bool()) {
		return &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("%s predicate must return Bool, got %s", prim, bodyType)}
	}
	return nil
}

// evalPredicate runs a one-parameter predicate lambda; elem is the
// statically inferred parameter type (nil when unknown).
func evalPredicate(lam *ast.Lambda, elem *ir.Type, e ir.Value) (bool, error) {
	r, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{elem}, ambientTypes()...), append([]ir.Value{e}, ambientArgs()...)...)
	if err != nil {
		return false, err
	}
	b, ok := r.(bool)
	if !ok {
		return false, fmt.Errorf("predicate did not return a Bool (got %s)", ir.DescribeValue(r))
	}
	return b, nil
}

// ownAccumulator gives a fold's accumulator storage of its own when the
// optimizer marked any update in the lambda as in-place.
//
// The analysis in optimizer/linear.go proves that nothing *inside* the lambda
// reads the copied-from value after an update. It says nothing about who else
// holds the seed, and that is a real question: a Part or a Channel branches
// from one upstream value, `FoldOver`'s seed *is* the current pipeline value,
// and `Reduce`'s accumulator starts as an element of the input list. One copy
// here, amortized over every write the fold makes, closes all three.
//
// Without a marked update this is the identity, so the naive pipeline keeps
// exactly the allocation profile it had.
func ownAccumulator(lam *ast.Lambda, acc ir.Value) ir.Value {
	if lam == nil || !ast.HasInPlace(lam.Body) {
		return acc
	}
	return ir.CloneCollection(acc)
}
