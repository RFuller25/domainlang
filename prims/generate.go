package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Generators: the two primitives that turn a single value into a list by
// applying a step repeatedly. `Simple Domain` loops already thread a value
// through a body, but they keep only where it ended up — these keep the
// trajectory, which is what "did this state repeat?" and "where had I been
// after k steps?" actually need.
//
//	Iterate n — a fixed number of steps, so the result is sized up front
//	Unfold    — steps until a While: predicate stops holding, bounded against
//	            a step that never terminates

// ---------------------------------------------------------------------------
// Cursed Technique: Iterate n — T x (T -> T) -> List<T>: the value after each
// of n applications of the step. Like Scan, the starting value is not
// re-emitted, so the result has exactly n elements and index i holds the state
// after i+1 steps.
// ---------------------------------------------------------------------------

var iterate = &Primitive{
	ID:      "Iterate",
	Keyword: "Cursed Technique",
	// `Iterate Until Fixed Point` is a Simple Domain loop kind, not this;
	// excluding its words is what keeps a keyword-free loop head unambiguous.
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Iterate") && !hasWord(op, "Until") && !hasWord(op, "Fixed")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Iterate has no input value to start from"}
		}
		timesM, err := requireMeasuredInt(op, args, "Iterate", "Times", 0, 0, in, pos,
			"a step count", "Iterate 5")
		if err != nil {
			return nil, err
		}
		if !timesM.IsMeasured() && timesM.Lit < 0 {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Iterate count must be >= 0, got %d", timesM.Lit)}
		}
		lam, err := requireLambda(args, 1, "Iterate", pos)
		if err != nil {
			return nil, err
		}
		stepType, err := typecheck.LambdaType(lam, in)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Iterate: " + err.Error()}
		}
		if !stepType.Equal(in) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Iterate step must return its own input type %s so it can be applied again, got %s",
				in, stepType)}
		}
		params := []*ir.Type{in}
		meta := map[string]any{"lambda": lam}
		timesM.Meta(meta, "n")
		return &ir.Node{
			Prim:    "Iterate",
			In:      in,
			Out:     ir.List(in),
			Display: "Iterate " + timesM.Describe(),
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				n, err := timesM.Resolve(v)
				if err != nil {
					return nil, err
				}
				out := make([]ir.Value, n)
				cur := v
				for i := int64(0); i < n; i++ {
					cur, err = eval.EvalLambdaTyped(lam, params, cur)
					if err != nil {
						return nil, runtimeErr("Iterate", pos, "step %d: %v", i+1, err)
					}
					out[i] = cur
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Unfold — T x (T -> Bool) x (T -> T) -> List<T>: the dual
// of Fold. Where Fold consumes a list into a value, Unfold grows a value into
// a list, emitting the state while the While: predicate holds and advancing it
// with the Using: step.
//
//	Cursed Technique: Unfold
//	    While: (x) -> x > 1
//	    Using: (x) -> x / 2
//
// The starting value is emitted first when it satisfies the predicate, so a
// predicate that is false immediately gives the empty list.
// ---------------------------------------------------------------------------

var unfold = &Primitive{
	ID:      "Unfold",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Unfold") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Unfold has no input value to start from"}
		}
		cond, ok := args.Lambda("While")
		if !ok {
			return nil, &ResolveError{Pos: pos,
				Msg: "Unfold requires a While: predicate (it decides when to stop)"}
		}
		if len(cond.Params) != 1 {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Unfold While: predicate must take 1 parameter, got %d", len(cond.Params))}
		}
		condType, err := typecheck.LambdaType(cond, in)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Unfold While: " + err.Error()}
		}
		if !condType.Equal(ir.Bool()) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Unfold While: predicate must return Bool, got %s", condType)}
		}
		lam, err := requireLambda(args, 1, "Unfold", pos)
		if err != nil {
			return nil, err
		}
		stepType, err := typecheck.LambdaType(lam, in)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Unfold: " + err.Error()}
		}
		if !stepType.Equal(in) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Unfold step must return its own input type %s so it can be applied again, got %s",
				in, stepType)}
		}
		params := []*ir.Type{in}
		return &ir.Node{
			Prim:    "Unfold",
			In:      in,
			Out:     ir.List(in),
			Display: "Unfold",
			Meta:    map[string]any{"lambda": lam, "while": cond},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				out := []ir.Value{}
				cur := v
				for {
					keep, err := evalPredicate(cond, in, cur)
					if err != nil {
						return nil, runtimeErr("Unfold", pos, "While: after %d step(s): %v", len(out), err)
					}
					if !keep {
						return out, nil
					}
					out = append(out, cur)
					// Bounded like the Simple Domain loops: a step that never
					// falsifies the predicate fails loudly instead of hanging.
					if maxLoopIterations > 0 && len(out) > maxLoopIterations {
						return nil, runtimeErr("Unfold", pos,
							"produced more than %d elements (non-terminating While:?)", maxLoopIterations)
					}
					if cur, err = eval.EvalLambdaTyped(lam, params, cur); err != nil {
						return nil, runtimeErr("Unfold", pos, "step %d: %v", len(out), err)
					}
				}
			},
		}, nil
	},
}
