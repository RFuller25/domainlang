package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// All Pairs / Combinations (Domain Expansion) — generate the k-combinations of
// a list and apply a Using: lambda to each. The lambda is the spec; the
// optimizer is free to substitute a faster algorithm for the same result (see
// optimizer.fuseAllPairsSum, which lowers the sum-to-constant pattern to an
// O(n) hash-set scan).
//
//	Domain Expansion: All Pairs        # k = 2
//	    Mode: First                    # Filter | Count | First | Map
//	    Using: (a, b) -> a + b = 2020
//
// Modes:
//	Filter -> List<List<T>>  (combos whose predicate is true)
//	Count  -> Int            (how many satisfy the predicate)
//	First  -> List<T>        (the first satisfying combo; errors if none)
//	Map    -> List<U>        (lambda applied to every combo)

var allPairs = &Primitive{
	ID:      "All Pairs",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "All") && hasWord(op, "Pairs") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		return buildCombinations("All Pairs", 2, args, in, pos)
	},
}

var combinations = &Primitive{
	ID:      "Combinations",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Combinations") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if len(op.Ints) == 0 {
			return nil, &ResolveError{Pos: pos, Msg: "Combinations requires a size, e.g. Combinations 3"}
		}
		k := int(op.Ints[0])
		if k < 1 {
			return nil, &ResolveError{Pos: pos, Msg: "Combinations size must be >= 1"}
		}
		return buildCombinations("Combinations", k, args, in, pos)
	},
}

func buildCombinations(prim string, k int, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
	if in == nil || in.Kind != ir.KList {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("%s expects a List input, got %s", prim, in)}
	}
	elem := in.Elem

	mode, _ := args.Ident("Mode")
	if mode == "" {
		mode = "Filter"
	}
	lam, err := requireLambda(args, k, prim, pos)
	if err != nil {
		return nil, err
	}
	params := repeatType(elem, k)
	bodyType, err := typecheck.LambdaType(lam, params...)
	if err != nil {
		return nil, &ResolveError{Pos: pos, Msg: prim + ": " + err.Error()}
	}

	var out *ir.Type
	switch mode {
	case "Filter":
		if !bodyType.Equal(ir.Bool()) {
			return nil, predicateBoolErr(prim, bodyType, pos)
		}
		out = ir.List(ir.List(elem))
	case "Count":
		if !bodyType.Equal(ir.Bool()) {
			return nil, predicateBoolErr(prim, bodyType, pos)
		}
		out = ir.Int()
	case "First":
		if !bodyType.Equal(ir.Bool()) {
			return nil, predicateBoolErr(prim, bodyType, pos)
		}
		out = ir.List(elem)
	case "Map":
		out = ir.List(bodyType)
	default:
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("%s Mode must be Filter, Count, First, or Map (got %q)", prim, mode)}
	}

	return &ir.Node{
		Prim:      prim,
		In:        in,
		Out:       out,
		Display:   fmt.Sprintf("%s (k=%d, Mode: %s)", prim, k, mode),
		Swappable: true,
		Meta:      map[string]any{"k": k, "mode": mode, "lambda": lam},
		Pos:       pos,
		Eval:      combosEval(prim, k, mode, lam, elem, pos),
	}, nil
}

func combosEval(prim string, k int, mode string, lam *ast.Lambda, elem *ir.Type, pos token.Position) func(*ir.Context, ir.Value) (ir.Value, error) {
	params := repeatType(elem, k)
	return func(_ *ir.Context, v ir.Value) (ir.Value, error) {
		xs, err := ir.AsList(v)
		if err != nil {
			return nil, runtimeErr(prim, pos, "%v", err)
		}
		n := len(xs)

		switch mode {
		case "Count":
			var c int64
			err := genCombos(n, k, func(idx []int) (bool, error) {
				ok, err := evalComboPredicate(lam, params, xs, idx)
				if err != nil {
					return false, err
				}
				if ok {
					c++
				}
				return false, nil
			})
			if err != nil {
				return nil, runtimeErr(prim, pos, "%v", err)
			}
			return c, nil

		case "Filter":
			out := []ir.Value{}
			err := genCombos(n, k, func(idx []int) (bool, error) {
				ok, err := evalComboPredicate(lam, params, xs, idx)
				if err != nil {
					return false, err
				}
				if ok {
					out = append(out, pick(xs, idx))
				}
				return false, nil
			})
			if err != nil {
				return nil, runtimeErr(prim, pos, "%v", err)
			}
			return out, nil

		case "First":
			var found ir.Value
			err := genCombos(n, k, func(idx []int) (bool, error) {
				ok, err := evalComboPredicate(lam, params, xs, idx)
				if err != nil {
					return false, err
				}
				if ok {
					found = pick(xs, idx)
					return true, nil
				}
				return false, nil
			})
			if err != nil {
				return nil, runtimeErr(prim, pos, "%v", err)
			}
			if found == nil {
				return nil, runtimeErr(prim, pos, "no combination satisfied the predicate")
			}
			return found, nil

		case "Map":
			out := make([]ir.Value, 0)
			err := genCombos(n, k, func(idx []int) (bool, error) {
				r, err := eval.EvalLambdaTyped(lam, params, pick(xs, idx)...)
				if err != nil {
					return false, err
				}
				out = append(out, r)
				return false, nil
			})
			if err != nil {
				return nil, runtimeErr(prim, pos, "%v", err)
			}
			return out, nil
		}
		return nil, runtimeErr(prim, pos, "unknown mode %q", mode)
	}
}

func evalComboPredicate(lam *ast.Lambda, params []*ir.Type, xs []ir.Value, idx []int) (bool, error) {
	r, err := eval.EvalLambdaTyped(lam, params, pick(xs, idx)...)
	if err != nil {
		return false, err
	}
	b, ok := r.(bool)
	if !ok {
		return false, fmt.Errorf("predicate did not return a Bool (got %s)", ir.DescribeValue(r))
	}
	return b, nil
}

// pick materializes the combination at the given indices.
func pick(xs []ir.Value, idx []int) []ir.Value {
	combo := make([]ir.Value, len(idx))
	for i, j := range idx {
		combo[i] = xs[j]
	}
	return combo
}

// genCombos visits every index combination i0<i1<...<i_{k-1} in lexicographic
// order. visit may return stop=true to end early (used by First).
func genCombos(n, k int, visit func(idx []int) (stop bool, err error)) error {
	if k > n || k < 0 {
		return nil
	}
	idx := make([]int, k)
	var rec func(start, depth int) (bool, error)
	rec = func(start, depth int) (bool, error) {
		if depth == k {
			return visit(idx)
		}
		for i := start; i <= n-(k-depth); i++ {
			idx[depth] = i
			stop, err := rec(i+1, depth+1)
			if err != nil || stop {
				return stop, err
			}
		}
		return false, nil
	}
	_, err := rec(0, 0)
	return err
}

func repeatType(t *ir.Type, k int) []*ir.Type {
	out := make([]*ir.Type, k)
	for i := range out {
		out[i] = t
	}
	return out
}

func predicateBoolErr(prim string, got *ir.Type, pos token.Position) error {
	return &ResolveError{Pos: pos,
		Msg: fmt.Sprintf("%s predicate must return Bool, got %s", prim, got)}
}
