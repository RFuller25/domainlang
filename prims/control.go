package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Control flow (M8): Simple Domain loops, plus the Apply transform and the
// Reverse inversion.

// maxLoopIterations bounds While / Iterate Until Fixed Point so a buggy program
// fails loudly instead of hanging. (Repeat is already bounded by its count.) It
// is a var so tests can lower it.
var maxLoopIterations = 1_000_000

// ---------------------------------------------------------------------------
// Cursed Technique: Apply — T x (T -> U) -> U. Transforms the single current
// value via a lambda (the scalar analogue of Map Each). Useful on its own and
// as the body of scalar loops.
// ---------------------------------------------------------------------------

var apply = &Primitive{
	ID:      "Apply",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Apply") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Apply has no input value"}
		}
		lam, err := requireLambda(args, 1, "Apply", pos)
		if err != nil {
			return nil, err
		}
		outT, err := typecheck.LambdaType(lam, in)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Apply: " + err.Error()}
		}
		return &ir.Node{
			Prim:    "Apply",
			In:      in,
			Out:     outT,
			Display: "Apply",
			Meta:    map[string]any{"lambda": lam},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				r, err := eval.EvalLambdaTyped(lam, []*ir.Type{in}, v)
				if err != nil {
					return nil, runtimeErr("Apply", pos, "%v", err)
				}
				return r, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Reverse Cursed Technique: Reverse — List<T> -> List<T>.
// ---------------------------------------------------------------------------

var reverse = &Primitive{
	ID:      "Reverse",
	Keyword: "Reverse Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Reverse") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KList {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Reverse expects a List, got %s", in)}
		}
		return &ir.Node{
			Prim:    "Reverse",
			In:      in,
			Out:     in,
			Display: "Reverse",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Reverse", pos, "%v", err)
				}
				out := make([]ir.Value, len(items))
				for i, e := range items {
					out[len(items)-1-i] = e
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Simple Domain: loops. The body is a sub-pipeline that must preserve the value
// type (its output type equals its input type) so it can iterate.
//
//	Simple Domain: Repeat 3
//	    <body>
//	Simple Domain: Iterate Until Fixed Point
//	    <body>
//	Simple Domain: While
//	    Using: (v) -> <predicate>
//	    <body>
// ---------------------------------------------------------------------------

func (r *resolver) resolveLoop(stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	if cur == nil {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "Simple Domain loop needs an upstream value"}
	}
	if stmt.Op == nil {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: "Simple Domain needs a loop kind (Repeat N / Iterate Until Fixed Point / While)"}
	}
	if len(stmt.Block) == 0 {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "Simple Domain loop has an empty body"}
	}

	subNodes, bodyOut, err := r.resolveSequence(stmt.Block, cur, false)
	if err != nil {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "in loop body: " + err.Error()}
	}
	if !bodyOut.Equal(cur) {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("loop body must preserve the value type (got %s -> %s)", cur, bodyOut)}
	}

	op := stmt.Op
	switch {
	case hasWord(op, "Repeat"):
		if len(op.Ints) == 0 {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: "Repeat needs a count, e.g. Repeat 3"}
		}
		if op.Ints[0] < 0 {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: "Repeat count must be >= 0"}
		}
		return repeatNode(subNodes, op.Ints[0], cur, stmt.Pos), nil

	case hasWord(op, "While"):
		lam, ok := ArgSet{stmt.Args}.Lambda("Using")
		if !ok {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: "While needs a Using: predicate"}
		}
		bt, err := typecheck.LambdaType(lam, cur)
		if err != nil {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: "While: " + err.Error()}
		}
		if !bt.Equal(ir.Bool()) {
			return nil, &ResolveError{Pos: stmt.Pos,
				Msg: fmt.Sprintf("While predicate must return Bool, got %s", bt)}
		}
		return whileNode(subNodes, lam, cur, stmt.Pos), nil

	case hasWord(op, "Fixed") || hasWord(op, "Iterate"):
		return fixedPointNode(subNodes, cur, stmt.Pos), nil

	default:
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("unknown Simple Domain loop kind %q", op.Raw)}
	}
}

func runBody(ctx *ir.Context, nodes []*ir.Node, v ir.Value) (ir.Value, error) {
	var err error
	for _, n := range nodes {
		if v, err = n.Eval(ctx, v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func repeatNode(body []*ir.Node, n int64, t *ir.Type, pos token.Position) *ir.Node {
	return &ir.Node{
		Prim: "Simple Domain (Repeat)", In: t, Out: t,
		Display: fmt.Sprintf("Repeat %d", n), Pos: pos,
		Meta: map[string]any{"kind": "repeat", "nodes": body, "n": n},
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			var err error
			for i := int64(0); i < n; i++ {
				if v, err = runBody(ctx, body, v); err != nil {
					return nil, err
				}
			}
			return v, nil
		},
	}
}

func whileNode(body []*ir.Node, lam *ast.Lambda, t *ir.Type, pos token.Position) *ir.Node {
	return &ir.Node{
		Prim: "Simple Domain (While)", In: t, Out: t,
		Display: "While", Pos: pos,
		Meta: map[string]any{"kind": "while", "nodes": body, "lambda": lam},
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			for iters := 0; ; iters++ {
				r, err := eval.EvalLambdaTyped(lam, []*ir.Type{t}, v)
				if err != nil {
					return nil, runtimeErr("Simple Domain (While)", pos, "predicate: %v", err)
				}
				cond, ok := r.(bool)
				if !ok {
					return nil, runtimeErr("Simple Domain (While)", pos, "predicate did not return a Bool")
				}
				if !cond {
					return v, nil
				}
				if iters >= maxLoopIterations {
					return nil, runtimeErr("Simple Domain (While)", pos,
						"loop exceeded %d iterations (non-terminating?)", maxLoopIterations)
				}
				if v, err = runBody(ctx, body, v); err != nil {
					return nil, err
				}
			}
		},
	}
}

func fixedPointNode(body []*ir.Node, t *ir.Type, pos token.Position) *ir.Node {
	return &ir.Node{
		Prim: "Simple Domain (Fixed Point)", In: t, Out: t,
		Display: "Iterate Until Fixed Point", Pos: pos,
		Meta: map[string]any{"kind": "fixedpoint", "nodes": body},
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			for iters := 0; ; iters++ {
				nv, err := runBody(ctx, body, v)
				if err != nil {
					return nil, err
				}
				if ir.DeepEqual(nv, v) {
					return nv, nil
				}
				if iters >= maxLoopIterations {
					return nil, runtimeErr("Simple Domain (Fixed Point)", pos,
						"did not converge within %d iterations", maxLoopIterations)
				}
				v = nv
			}
		},
	}
}
