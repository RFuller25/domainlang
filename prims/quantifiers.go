package prims

import (
	"fmt"

	"domain/ast"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Reductions that stop early, plus the key-lambda arithmetic pair.
//
// Any / All / Find / Find Index all answer from a prefix of the list: the
// first element that decides the question ends the scan. Spelling them as
// `Count Matching … > 0` or `Filter … + Take Item 0` gets the same answer, but
// only after visiting every element and (for Filter) building a list of the
// ones that matched. The optimizer knows about the second of those and
// rewrites it here — see optimizer.fuseFilterFirst.
//
// Sum By / Product By complete the key-lambda family that already has Count
// By, Min By and Max By, and fold in one pass what `Map Each` + `Sum` does in
// two (optimizer.fuseMapReduceBy performs exactly that rewrite).

// ---------------------------------------------------------------------------
// Maximum Technique: Any / All — List<T> x (T -> Bool) -> Bool.
// ---------------------------------------------------------------------------

// anyPrim short-circuits on the first true; allPrim on the first false. Both
// answer the empty list from the identity of their connective: Any is false
// (nothing satisfies it), All is true (nothing violates it).
var anyPrim = quantifierPrim("Any", false)
var allPrim = quantifierPrim("All", true)

func quantifierPrim(id string, universal bool) *Primitive {
	return &Primitive{
		ID:      id,
		Keyword: "Maximum Technique",
		Match: func(op *ast.Operation) bool {
			// "All Pairs" is the Domain Expansion combination generator and
			// "All Values > n" is a Binding Vow predicate; neither is this.
			return hasWord(op, id) && !hasWord(op, "Pairs") && !hasWord(op, "Values")
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
				Out:     ir.Bool(),
				Display: id,
				Meta:    map[string]any{"lambda": lam},
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr(id, pos, "%v", err)
					}
					for i, e := range xs {
						hit, err := evalPredicate(lam, elem, e)
						if err != nil {
							return nil, runtimeErr(id, pos, "element %d: %v", i, err)
						}
						// Any stops on true, All stops on false; either way the
						// element that decided it is the last one examined.
						if hit != universal {
							return !universal, nil
						}
					}
					return universal, nil
				},
			}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Maximum Technique: Find / Find Index — List<T> x (T -> Bool) -> T | Int: the
// first element satisfying the predicate, or its 0-based position. Find on no
// match is a runtime error (there is no element to return); Find Index answers
// -1, the sentinel the expression layer already uses for "not there".
// ---------------------------------------------------------------------------

var findPrim = findLike("Find", false)
var findIndex = findLike("Find Index", true)

func findLike(id string, wantIndex bool) *Primitive {
	return &Primitive{
		ID:      id,
		Keyword: "Maximum Technique",
		Match: func(op *ast.Operation) bool {
			// Find Cells is the grid search; Find Index takes the Index word.
			if hasWord(op, "Cells") {
				return false
			}
			return hasWord(op, "Find") && hasWord(op, "Index") == wantIndex
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
			out := elem
			if wantIndex {
				out = ir.Int()
			}
			return &ir.Node{
				Prim:    id,
				In:      in,
				Out:     out,
				Display: id,
				Meta:    map[string]any{"lambda": lam},
				Pos:     pos,
				Eval:    findEval(id, wantIndex, lam, elem, noMatchMessage(id), pos),
			}, nil
		},
	}
}

// noMatchMessage is the runtime error a Find reports when nothing matched. It
// travels in Meta["nomatch"] on optimizer-produced Find nodes so a rewritten
// pipeline reports the failure the shape it replaced would have reported.
func noMatchMessage(id string) string {
	return fmt.Sprintf("no element satisfied the %s predicate", id)
}

// findEval is shared with the optimizer's Filter + Take Item 0 rewrite, which
// needs the same scan under a different no-match message.
func findEval(id string, wantIndex bool, lam *ast.Lambda, elem *ir.Type, noMatch string, pos token.Position) func(*ir.Context, ir.Value) (ir.Value, error) {
	return func(_ *ir.Context, v ir.Value) (ir.Value, error) {
		xs, err := ir.AsList(v)
		if err != nil {
			return nil, runtimeErr(id, pos, "%v", err)
		}
		for i, e := range xs {
			hit, err := evalPredicate(lam, elem, e)
			if err != nil {
				return nil, runtimeErr(id, pos, "element %d: %v", i, err)
			}
			if hit {
				if wantIndex {
					return int64(i), nil
				}
				return e, nil // found: the rest of the list is never touched
			}
		}
		if wantIndex {
			return int64(-1), nil
		}
		return nil, runtimeErr(id, pos, "%s", noMatch)
	}
}

// ---------------------------------------------------------------------------
// Maximum Technique: Sum By / Product By — List<T> x (T -> Int) -> Int: fold
// the lambda's Int key over the list without building the mapped list first.
// ---------------------------------------------------------------------------

var sumBy = keyedArithmetic("Sum By", 0, func(acc, k int64) int64 { return acc + k })
var productBy = keyedArithmetic("Product By", 1, func(acc, k int64) int64 { return acc * k })

func keyedArithmetic(id string, identity int64, step func(acc, k int64) int64) *Primitive {
	verb := splitID(id)[0]
	return &Primitive{
		ID:      id,
		Keyword: "Maximum Technique",
		Match: func(op *ast.Operation) bool {
			// Sum Each Group is the per-group reduction, not a keyed one.
			return hasWord(op, verb) && hasWord(op, "By") &&
				!hasWord(op, "Group") && !hasWord(op, "Each")
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
			keyType, err := typecheck.LambdaType(lam, elem)
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
				Out:     ir.Int(),
				Display: id,
				Meta:    map[string]any{"lambda": lam},
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr(id, pos, "%v", err)
					}
					acc := identity // the empty list folds to the identity
					for i, x := range xs {
						k, err := intKey(lam, elem, x)
						if err != nil {
							return nil, runtimeErr(id, pos, "item %d: %v", i, err)
						}
						acc = step(acc, k)
					}
					return acc, nil
				},
			}, nil
		},
	}
}
