package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// The functional trio that Fold's seeded left fold leaves out:
//
//	Reduce — the seedless fold: the accumulator starts as the first element,
//	         so it works over any element type without a literal Seed:, and
//	         the combining lambda is T x T -> T.
//	Scan   — the running fold: every intermediate accumulator, one per input
//	         element, so the result lines up with the list it came from.
//	Pairs  — adjacent elements as tuples (`zip xs (tail xs)`), the shape
//	         "compare each item with the next" wants.
//
// Scan and Pairs are transforms (List -> List) and live under Cursed
// Technique; Reduce collapses a list to a single value and lives with the
// other reductions under Maximum Technique.

// ---------------------------------------------------------------------------
// Maximum Technique: Reduce — List<T> x (T, T -> T) -> T.
// ---------------------------------------------------------------------------

var reduce = &Primitive{
	ID:      "Reduce",
	Keyword: "Maximum Technique",
	// Sliding Reduce is the windowed Domain Expansion, not this fold.
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Reduce") && !hasWord(op, "Sliding") && !hasWord(op, "Rolling")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Reduce", pos)
		if err != nil {
			return nil, err
		}
		lam, err := requireLambda(args, 2, "Reduce", pos)
		if err != nil {
			return nil, err
		}
		// Seedless means the accumulator *is* an element, so the lambda has to
		// close over the element type: T x T -> T.
		accType, err := typecheck.LambdaType(lam, elem, elem)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Reduce: " + err.Error()}
		}
		if !accType.Equal(elem) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Reduce lambda must return the element type %s, got %s (use Fold with a Seed: to accumulate a different type)",
				elem, accType)}
		}
		params := []*ir.Type{elem, elem}
		return &ir.Node{
			Prim:    "Reduce",
			In:      in,
			Out:     elem,
			Display: "Reduce",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Reduce", pos, "%v", err)
				}
				if len(items) == 0 {
					return nil, runtimeErr("Reduce", pos,
						"Reduce of an empty list is undefined (Fold with a Seed: has an answer for it)")
				}
				acc := items[0]
				for i, e := range items[1:] {
					acc, err = eval.EvalLambdaTyped(lam, params, acc, e)
					if err != nil {
						return nil, runtimeErr("Reduce", pos, "element %d: %v", i+1, err)
					}
				}
				return acc, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Scan — the running fold.
//
//	with    Seed:  List<T> x Seed x (Acc, T -> Acc) -> List<Acc>
//	without Seed:  List<T> x (T, T -> T)            -> List<T>
//
// Either way the output has exactly one element per input element: the
// accumulator *after* folding that element in. The seeded form therefore does
// not re-emit the seed (Haskell's scanl minus its head), which is what keeps a
// Scan aligned with the list it scanned — index i of the result is the fold of
// the first i+1 inputs.
// ---------------------------------------------------------------------------

var scan = &Primitive{
	ID:      "Scan",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Scan") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Scan", pos)
		if err != nil {
			return nil, err
		}
		seedVal, accType, seeded, err := optionalSeed(args, pos)
		if err != nil {
			return nil, err
		}
		if !seeded {
			accType = elem
		}
		lam, err := requireLambda(args, 2, "Scan", pos)
		if err != nil {
			return nil, err
		}
		bodyType, err := typecheck.LambdaType(lam, accType, elem)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Scan: " + err.Error()}
		}
		if !bodyType.Equal(accType) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Scan lambda must return the accumulator type %s, got %s", accType, bodyType)}
		}
		meta := map[string]any{"lambda": lam}
		display := "Scan"
		if seeded {
			meta["seed"] = seedVal
			display = fmt.Sprintf("Scan (Seed: %s)", ir.FormatValue(seedVal))
		}
		params := []*ir.Type{accType, elem}
		return &ir.Node{
			Prim:    "Scan",
			In:      in,
			Out:     ir.List(accType),
			Display: display,
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Scan", pos, "%v", err)
				}
				out := make([]ir.Value, len(items))
				start := 0
				var acc ir.Value
				switch {
				case seeded:
					acc = seedVal
				case len(items) == 0:
					return out, nil
				default:
					// Seedless: the first element seeds the scan and is its own
					// first result, exactly as it seeds a Reduce.
					acc, out[0], start = items[0], items[0], 1
				}
				for i := start; i < len(items); i++ {
					acc, err = eval.EvalLambdaTyped(lam, params, acc, items[i])
					if err != nil {
						return nil, runtimeErr("Scan", pos, "element %d: %v", i, err)
					}
					out[i] = acc
				}
				return out, nil
			},
		}, nil
	},
}

// optionalSeed reads the Seed: argument the way foldSeed does, but tolerates
// its absence — Scan and Reduce both have a defined meaning without one.
func optionalSeed(args ArgSet, pos token.Position) (ir.Value, *ir.Type, bool, error) {
	if !args.Has("Seed") {
		return nil, nil, false, nil
	}
	v, t, err := foldSeed(args, pos)
	if err != nil {
		return nil, nil, false, err
	}
	return v, t, true, nil
}

// ---------------------------------------------------------------------------
// Cursed Technique: Pairs — List<T> -> List<(T, T)>: every element paired with
// the one after it, so a list of n yields n-1 pairs (fewer than 2 yields none).
// ---------------------------------------------------------------------------

var pairs = &Primitive{
	ID:      "Pairs",
	Keyword: "Cursed Technique",
	// "All Pairs" is the Domain Expansion combination generator, a different
	// operation entirely; excluding it here is what keeps a keyword-free
	// `All Pairs` line unambiguous.
	Match: func(op *ast.Operation) bool { return hasWord(op, "Pairs") && !hasWord(op, "All") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Pairs", pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Pairs",
			In:      in,
			Out:     ir.List(ir.Tuple(elem, elem)),
			Display: "Pairs",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Pairs", pos, "%v", err)
				}
				if len(xs) < 2 {
					return []ir.Value{}, nil
				}
				out := make([]ir.Value, len(xs)-1)
				for i := 0; i+1 < len(xs); i++ {
					out[i] = []ir.Value{xs[i], xs[i+1]}
				}
				return out, nil
			},
		}, nil
	},
}
