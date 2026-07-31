package prims

import (
	"domain/ast"
	"domain/ir"
	"domain/token"
)

// List-shaping transforms: the predicate-driven prefix/suffix pair (Take
// While, Drop While), fixed-size blocking (Chunk), and the one-pass two-way
// split (Partition).
//
// All four stop or allocate exactly as much as they must: Take While and Drop
// While quit scanning at the boundary rather than testing every element the
// way a Filter does, Chunk sizes its output up front, and Partition makes one
// pass where two negated Filters would make two.

// ---------------------------------------------------------------------------
// Cursed Technique: Take While / Drop While — List<T> x (T -> Bool) -> List<T>.
// The longest prefix whose elements all satisfy the predicate, and everything
// after it. Both stop testing at the first failure, which is what separates
// them from Filter: `Take While (x) -> x > 0` over [1, 2, -1, 3] is [1, 2],
// where Filter would also have kept the 3.
// ---------------------------------------------------------------------------

var takeWhile = prefixPrim("Take While", "Take")
var dropWhile = prefixPrim("Drop While", "Drop")

func prefixPrim(id, verb string) *Primitive {
	take := verb == "Take"
	return &Primitive{
		ID:      id,
		Keyword: "Cursed Technique",
		Match: func(op *ast.Operation) bool {
			return hasWord(op, verb) && hasWord(op, "While")
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
			if err := requirePredicate(lam, elem, id, pos); err != nil {
				return nil, err
			}
			return &ir.Node{
				Prim:    id,
				In:      in,
				Out:     in,
				Display: id,
				Meta:    map[string]any{"lambda": lam},
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr(id, pos, "%v", err)
					}
					cut := len(xs)
					for i, e := range xs {
						keep, err := evalPredicate(lam, elem, e)
						if err != nil {
							return nil, runtimeErr(id, pos, "element %d: %v", i, err)
						}
						if !keep {
							cut = i
							break // the boundary decides both halves; stop testing
						}
					}
					if take {
						// The prefix is copied: a later concat appending onto a
						// shared backing array would otherwise clobber the
						// elements this slice stops short of.
						return append([]ir.Value{}, xs[:cut]...), nil
					}
					// The suffix may alias — appending past the end of the
					// original slice reallocates rather than overwriting it.
					return xs[cut:], nil
				},
			}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Cursed Technique: Chunk n — List<T> -> List<List<T>>: consecutive blocks of
// n, keeping a short final block. `Window n n` covers the same ground but
// silently drops that final block, which is usually a bug rather than intent.
// ---------------------------------------------------------------------------

var chunk = &Primitive{
	ID:      "Chunk",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Chunk") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Chunk", pos)
		if err != nil {
			return nil, err
		}
		sizeM, err := requireMeasuredInt(op, args, "Chunk", "Size", 0, 1, in, pos, "a size", "Chunk 3")
		if err != nil {
			return nil, err
		}
		if !sizeM.IsMeasured() {
			if err := sizeM.atLeast(1, sizeM.Lit, ""); err != nil {
				return nil, err
			}
		}
		meta := map[string]any{}
		sizeM.Meta(meta, "size")
		return &ir.Node{
			Prim:    "Chunk",
			In:      in,
			Out:     ir.List(ir.List(elem)),
			Display: "Chunk " + sizeM.Describe(),
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Chunk", pos, "%v", err)
				}
				size, err := sizeM.Resolve(v)
				if err != nil {
					return nil, err
				}
				n := int64(len(xs))
				out := make([]ir.Value, 0, (n+size-1)/size)
				for i := int64(0); i < n; i += size {
					end := min(i+size, n) // the short final block is kept, not dropped
					out = append(out, append([]ir.Value{}, xs[i:end]...))
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Partition — List<T> x (T -> Bool) -> List<List<T>>: a
// two-element list, [matching, non-matching], each in the input's order. One
// pass and one predicate evaluation per element, where a Filter and its
// negation cost two of each.
//
// The result is a list rather than a tuple so the halves are reachable the way
// input sections already are: `Take Item 0` / `Take Item 1` in the pipeline,
// `first(p)` / `last(p)` in a lambda.
// ---------------------------------------------------------------------------

var partition = &Primitive{
	ID:      "Partition",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Partition") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Partition", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 1, "Partition", pos)
		if err != nil {
			return nil, err
		}
		if err := requirePredicate(lam, elem, "Partition", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Partition",
			In:      in,
			Out:     ir.List(in),
			Display: "Partition",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Partition", pos, "%v", err)
				}
				yes, no := []ir.Value{}, []ir.Value{}
				for i, e := range xs {
					keep, err := evalPredicate(lam, elem, e)
					if err != nil {
						return nil, runtimeErr("Partition", pos, "element %d: %v", i, err)
					}
					if keep {
						yes = append(yes, e)
					} else {
						no = append(no, e)
					}
				}
				return []ir.Value{yes, no}, nil
			},
		}, nil
	},
}
