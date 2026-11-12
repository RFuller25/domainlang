package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// buildFindCycleState is Find Cycle's step form:
//
//	Maximum Technique: Find Cycle
//	    Using: (s) -> step(s)
//	    Limit: 1000000        # optional
//	    Key: (s) -> key(s)    # optional
//
// The current value is the start state. The step is applied until a state
// repeats, and the answer is the (start, period) pair `Iterate n` + `Find
// Cycle` would give for any n large enough: the trajectory is the states
// AFTER each step (the start state itself is not in it, exactly as Iterate
// does not emit it), so `start` is the 0-based index into that trajectory —
// state i is the state after i+1 steps — and `period` is the cycle length.
// Limit: caps the number of steps taken; no repeat within it answers (-1, 0),
// like the list form. With no Limit: the ceiling Unfold has applies and
// exceeding it is an error. States are compared by value (ir.KeyOf) when
// keyable; otherwise Key: must map a state to something that is.
func buildFindCycleState(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
	if in == nil {
		return nil, &ResolveError{Pos: pos, Msg: "Find Cycle has no input value to start from"}
	}
	step, err := requireLambda(args, 1, "Find Cycle", pos)
	if err != nil {
		return nil, err
	}
	stepType, err := typecheck.LambdaType(step, in)
	if err != nil {
		return nil, &ResolveError{Pos: pos, Msg: "Find Cycle: " + err.Error()}
	}
	if !stepType.Equal(in) {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Find Cycle step must return its own input type %s so it can be applied again, got %s", in, stepType)}
	}
	keyed := in
	key, hasKey := args.Lambda("Key")
	if hasKey {
		if len(key.Params) != 1+ambientDepth() {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Find Cycle Key: lambda must take %d parameter(s), got %d", 1+ambientDepth(), len(key.Params))}
		}
		if keyed, err = typecheck.LambdaType(key, in); err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Find Cycle Key: " + err.Error()}
		}
	}
	if !ir.ListKeyable(keyed) {
		if hasKey {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Find Cycle Key: must produce something keyable (Int, Text, or Lists/Tuples/Records of them), got %s", keyed)}
		}
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Find Cycle compares states, and %s is not keyable; add Key: (s) -> ... producing an Int, Text, or Lists/Tuples/Records of them", in)}
	}
	limit, hasLimit, err := measuredInt(op, args, "Find Cycle", "Limit", -1, 0, in, pos)
	if err != nil {
		return nil, err
	}
	if hasLimit && !limit.IsMeasured() && limit.Lit < 0 {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Find Cycle Limit: must be >= 0, got %d", limit.Lit)}
	}
	meta := map[string]any{"lambda": step, "keyType": keyed}
	if hasKey {
		meta["key"] = key
	}
	if hasLimit {
		limit.Meta(meta, "limit")
	}
	params := []*ir.Type{in}
	return &ir.Node{
		Prim: "Find Cycle", In: in, Out: ir.Tuple(ir.Int(), ir.Int()),
		Display: "Find Cycle", Meta: meta, Pos: pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			bound := int64(maxLoopIterations)
			if hasLimit {
				n, err := limit.Resolve(v)
				if err != nil {
					return nil, err
				}
				bound = n
			}
			first := map[any]int64{}
			cur := v
			for i := int64(0); ; i++ {
				if i >= bound && (hasLimit || maxLoopIterations > 0) {
					if hasLimit {
						return []ir.Value{int64(-1), int64(0)}, nil
					}
					return nil, runtimeErr("Find Cycle", pos,
						"no state repeated within %d steps (non-terminating step? add Limit:)", maxLoopIterations)
				}
				var err error
				if cur, err = eval.EvalLambdaTyped(step, params, cur); err != nil {
					return nil, runtimeErr("Find Cycle", pos, "step %d: %v", i+1, err)
				}
				kv := cur
				if hasKey {
					if kv, err = eval.EvalLambdaTyped(key, params, cur); err != nil {
						return nil, runtimeErr("Find Cycle", pos, "Key: at step %d: %v", i+1, err)
					}
				}
				k := ir.KeyOf(kv)
				if at, seen := first[k]; seen {
					return []ir.Value{at, i - at}, nil
				}
				first[k] = i
			}
		},
	}, nil
}
