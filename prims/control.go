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

// maxLoopIterations optionally bounds While / Iterate Until Fixed Point.
// (Repeat is already bounded by its count.)
//
// Zero — the default — means unlimited. Domain used to hard-code a ceiling of
// 1,000,000 here, which turned a legitimate long-running simulation into a
// spurious failure; a limit must never be the reason a correct program cannot
// run. The trade is real and deliberate: a genuinely non-terminating loop now
// spins until interrupted instead of failing loudly. Tests set this to keep
// their own runaway cases quick.
var maxLoopIterations = 0

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
		outT, err := typecheck.LambdaType(lam, append([]*ir.Type{in}, ambientTypes()...)...)
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
				r, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{in}, ambientTypes()...), append([]ir.Value{v}, ambientArgs()...)...)
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
		// Text reverses by rune, the unit every other text operation uses.
		// A palindrome check used to round-trip through Split Text by "".
		if in != nil && in.Kind == ir.KText {
			return &ir.Node{
				Prim: "Reverse", In: in, Out: in, Display: "Reverse (Text)", Pos: pos,
				Meta: map[string]any{"text": true},
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					s, ok := v.(string)
					if !ok {
						return nil, runtimeErr("Reverse", pos, "expected Text, got %s", ir.DescribeValue(v))
					}
					rs := []rune(s)
					for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
						rs[i], rs[j] = rs[j], rs[i]
					}
					return string(rs), nil
				},
			}, nil
		}
		if in == nil || in.Kind != ir.KList {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Reverse expects a List or Text, got %s", in)}
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
			Msg: "Simple Domain needs a loop kind (Repeat N / Iterate Until Fixed Point / While / For)"}
	}
	if len(stmt.Block) == 0 {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "Simple Domain loop has an empty body"}
	}

	op := stmt.Op
	if hasWord(op, "For") {
		return r.resolveForLoop(stmt, op, cur)
	}

	subNodes, bodyOut, err := r.resolveSequence(stmt.Block, cur, scopeNested)
	if err != nil {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "in loop body: " + err.Error()}
	}
	if !bodyOut.Equal(cur) {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("loop body must preserve the value type (got %s -> %s)", cur, bodyOut)}
	}

	switch {
	case hasWord(op, "Repeat"):
		timesM, err := requireMeasuredInt(op, ArgSet{stmt.Args}, "Repeat", "Times", 0, 0, cur,
			stmt.Pos, "a count", "Repeat 3")
		if err != nil {
			return nil, err
		}
		if !timesM.IsMeasured() && timesM.Lit < 0 {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: "Repeat count must be >= 0"}
		}
		return repeatNode(subNodes, timesM, cur, stmt.Pos), nil

	case hasWord(op, "While"):
		lam, ok := ArgSet{stmt.Args}.Lambda("Using")
		if !ok {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: "While needs a Using: predicate"}
		}
		bt, err := typecheck.LambdaType(lam, append([]*ir.Type{cur}, ambientTypes()...)...)
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

// resolveForLoop lowers `Simple Domain: For x in <source>`. <source> is
// either a channel name (holding a List<T>) or an inline range(N) (N an Int
// literal). x becomes an ambient extra trailing parameter on every Using:
// lambda inside the body — nested For loops stack, outermost first (see
// prims/ambient.go). The current pipeline value still threads across laps
// exactly like While/Repeat.
func (r *resolver) resolveForLoop(stmt *ast.Statement, op *ast.Operation, cur *ir.Type) (*ir.Node, error) {
	varName, source, isRange, err := parseForHeader(op, stmt.Pos)
	if err != nil {
		return nil, err
	}

	var elemType *ir.Type
	var rangeN int64
	var channelName string
	if isRange {
		n, err := parseRangeArg(op, stmt.Pos)
		if err != nil {
			return nil, err
		}
		rangeN = n
		elemType = ir.Int()
	} else {
		t, ok := r.channels[source]
		if !ok {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("unknown channel %q in For", source)}
		}
		if t == nil || t.Kind != ir.KList {
			return nil, &ResolveError{Pos: stmt.Pos,
				Msg: fmt.Sprintf("For channel %q must hold a List, got %s", source, t)}
		}
		channelName = source
		elemType = t.Elem
	}

	pushAmbient(varName, elemType)
	subNodes, bodyOut, err := r.resolveSequence(stmt.Block, cur, scopeNested)
	popAmbient()
	if err != nil {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "in loop body: " + err.Error()}
	}
	if !bodyOut.Equal(cur) {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("loop body must preserve the value type (got %s -> %s)", cur, bodyOut)}
	}

	display := fmt.Sprintf("For %s in %s", varName, source)
	if isRange {
		display = fmt.Sprintf("For %s in range(%d)", varName, rangeN)
	}
	return &ir.Node{
		Prim: "Simple Domain (For)", In: cur, Out: cur,
		Display: display, Pos: stmt.Pos,
		// isRange/rangeN/channel and the element type are recorded so the
		// compiler backend can emit the same loop; the interpreter reads them
		// from the closure below.
		Meta: map[string]any{
			"kind": "for", "nodes": subNodes, "varName": varName,
			"isRange": isRange, "rangeN": rangeN, "channel": channelName,
			"elem": elemType,
		},
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			var xs []ir.Value
			if isRange {
				xs = make([]ir.Value, rangeN)
				for i := int64(0); i < rangeN; i++ {
					xs[i] = i
				}
			} else {
				cv, ok := ctx.Channel(channelName)
				if !ok {
					return nil, runtimeErr("Simple Domain (For)", stmt.Pos, "channel %q has no value", channelName)
				}
				items, err := ir.AsList(cv)
				if err != nil {
					return nil, runtimeErr("Simple Domain (For)", stmt.Pos, "channel %q: %v", channelName, err)
				}
				xs = items
			}
			for _, x := range xs {
				pushAmbientValue(x, elemType)
				var err error
				v, err = runBody(ctx, subNodes, v)
				popAmbientValue()
				if err != nil {
					return nil, err
				}
			}
			return v, nil
		},
	}, nil
}

// parseForHeader extracts the loop variable name and source from a
// `For x in <source>` operation phrase. Words for "For x in y" are
// ["For", "x", "in", "y"]. For "For x in range(5)" they are also 4 words —
// ["For", "x", "in", "range"] — since range's literal Int argument is an
// INT token, which the operation-phrase scanner routes to op.Ints, never
// op.Words (only IDENT tokens land there); "range" as the fourth word is
// what distinguishes the two forms, not word count.
func parseForHeader(op *ast.Operation, pos token.Position) (varName, source string, isRange bool, err error) {
	if len(op.Words) < 4 || op.Words[0] != "For" || op.Words[2] != "in" {
		return "", "", false, &ResolveError{Pos: pos,
			Msg: "For needs a variable and source, e.g. For x in y or For x in range(5)"}
	}
	varName = op.Words[1]
	// range's own literal Int argument (op.Ints[0]) never lands in op.Words
	// (only IDENT tokens do — see the operation-phrase scanner in
	// parser/parser.go), so "For x in range(5)" and "For x in y" both parse
	// to exactly 4 Words; "range" appearing as the fourth word is what
	// distinguishes the two, not word count.
	if op.Words[3] == "range" {
		return varName, "range", true, nil
	}
	if len(op.Words) == 4 {
		return varName, op.Words[3], false, nil
	}
	return "", "", false, &ResolveError{Pos: pos, Msg: "For source must be a channel name or range(N)"}
}

// parseRangeArg resolves range(...)'s single Int-literal argument — always
// op.Ints[0], the same field Repeat's literal count already uses.
func parseRangeArg(op *ast.Operation, pos token.Position) (int64, error) {
	if len(op.Ints) == 0 {
		return 0, &ResolveError{Pos: pos, Msg: "range(...) needs an integer, e.g. range(5)"}
	}
	if op.Ints[0] < 0 {
		return 0, &ResolveError{Pos: pos, Msg: "range(...) argument must be >= 0"}
	}
	return op.Ints[0], nil
}

// runBody runs a loop body once. Every loop kind shares it, which is what lets
// one instrumentation point cover all three for tracing.
func runBody(ctx *ir.Context, nodes []*ir.Node, v ir.Value) (ir.Value, error) {
	var err error
	for _, n := range nodes {
		if v, err = ir.EvalNode(ctx, n, v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// runIteration runs one loop iteration inside a labelled trace frame, so a
// visualizer can step into `Repeat 4 iter 2/4` and --stats can attribute nested
// work to its loop. Without a tracer the frame calls are no-ops.
func runIteration(ctx *ir.Context, nodes []*ir.Node, v ir.Value, label string) (ir.Value, error) {
	ctx.PushFrame(label)
	defer ctx.PopFrame()
	return runBody(ctx, nodes, v)
}

func repeatNode(body []*ir.Node, timesM Measured, t *ir.Type, pos token.Position) *ir.Node {
	meta := map[string]any{"kind": "repeat", "nodes": body}
	timesM.Meta(meta, "n")
	return &ir.Node{
		Prim: "Simple Domain (Repeat)", In: t, Out: t,
		Display: "Repeat " + timesM.Describe(), Pos: pos,
		Meta: meta,
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			// Measured once, before the first lap: the count is a property of
			// the value entering the loop, not of each lap's value.
			n, err := timesM.Resolve(v)
			if err != nil {
				return nil, err
			}
			for i := int64(0); i < n; i++ {
				label := fmt.Sprintf("Repeat %d iter %d/%d", n, i+1, n)
				if v, err = runIteration(ctx, body, v, label); err != nil {
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
				r, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{t}, ambientTypes()...), append([]ir.Value{v}, ambientArgs()...)...)
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
				if maxLoopIterations > 0 && iters >= maxLoopIterations {
					return nil, runtimeErr("Simple Domain (While)", pos,
						"loop exceeded %d iterations (non-terminating?)", maxLoopIterations)
				}
				if v, err = runIteration(ctx, body, v, fmt.Sprintf("While iter %d", iters+1)); err != nil {
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
				nv, err := runIteration(ctx, body, v, fmt.Sprintf("Fixed Point iter %d", iters+1))
				if err != nil {
					return nil, err
				}
				if ir.DeepEqual(nv, v) {
					return nv, nil
				}
				if maxLoopIterations > 0 && iters >= maxLoopIterations {
					return nil, runtimeErr("Simple Domain (Fixed Point)", pos,
						"did not converge within %d iterations", maxLoopIterations)
				}
				v = nv
			}
		},
	}
}
