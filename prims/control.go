package prims

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Control flow (M8): Simple Domain loops, plus the Apply transform and the
// Reverse inversion.

// maxLoopIterations bounds While / Iterate Until Fixed Point, and Unfold's
// element count (prims/generate.go). (Repeat is already bounded by its count.)
//
// One billion is the ceiling, chosen to sit past any run that finishes in a
// human's lifetime rather than past any run someone might write: a limit must
// never be the reason a correct program cannot run, and the 1,000,000 Domain
// used to hard-code turned legitimate long-running simulations — a 40,000,000
// step generator, say — into spurious failures. Above the ceiling a loop is
// not slow, it is stuck, and failing loudly beats spinning until interrupted.
//
// Zero still means unlimited, and tests lower it to keep their own runaway
// cases quick. Whatever this is, codegen.dmMaxLoopIterations must match it, or
// the same program means two different things depending on the backend.
var maxLoopIterations = 1_000_000_000

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
					slices.Reverse(rs)
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
	var rewriteErr error
	node, err := r.loopNode(stmt, cur, &rewriteErr)
	// A lambda the binding scope could not rewrite is the more specific
	// failure; see resolveOne.
	if rewriteErr != nil {
		return nil, rewriteErr
	}
	return node, err
}

func (r *resolver) loopNode(stmt *ast.Statement, cur *ir.Type, rewriteErr *error) (*ir.Node, error) {
	if cur == nil {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "Simple Domain loop needs an upstream value"}
	}
	if stmt.Op == nil {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: "Simple Domain needs a loop kind (Repeat N / Iterate Until Fixed Point / While / For)"}
	}
	if len(stmt.Block) == 0 {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "Simple Domain loop has an empty body", NeedsBlock: true}
	}

	op := stmt.Op
	if hasWord(op, "For") {
		return r.resolveForLoop(stmt, op, cur)
	}

	subNodes, bodyOut, err := r.resolveSequence(stmt.Block, cur, scopeLoop)
	if err != nil {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "in loop body: " + err.Error()}
	}
	if !bodyOut.Equal(cur) {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("loop body must preserve the value type (got %s -> %s)", cur, bodyOut)}
	}

	switch {
	case hasWord(op, "Repeat"):
		timesM, err := requireMeasuredInt(op, r.args(stmt, rewriteErr), "Repeat", "Times", 0, 0, cur,
			stmt.Pos, "a count", "Repeat 3")
		if err != nil {
			return nil, err
		}
		if !timesM.IsMeasured() && timesM.Lit < 0 {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: "Repeat count must be >= 0"}
		}
		return repeatNode(subNodes, timesM, cur, stmt.Pos), nil

	case hasWord(op, "While"):
		lam, ok := r.args(stmt, rewriteErr).Lambda("Using")
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
	subNodes, bodyOut, err := r.resolveSequence(stmt.Block, cur, scopeLoop)
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
			for i, x := range xs {
				pushAmbientValue(x, elemType)
				var err error
				label := fmt.Sprintf("For %s iter %d/%d", varName, i+1, len(xs))
				v, err = runIteration(ctx, subNodes, v, label, cur)
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
func runIteration(ctx *ir.Context, nodes []*ir.Node, v ir.Value, label string, t *ir.Type) (ir.Value, error) {
	// The lap's own result closes its frame, so a stepper can show what one
	// iteration made of what it was given without opening it. A lap that failed
	// reports nil, which is what marks the frame unfinished.
	var out ir.Value
	ctx.PushFrame(label, t)
	defer func() { ctx.PopFrame(out) }()

	out, err := runBody(ctx, nodes, v)
	if err != nil {
		out = nil
	}
	return out, err
}

// ownLoopState gives a loop's state storage of its own, when the optimizer
// marked an update in the body as in-place.
//
// It is ownAccumulator's twin for a loop, and it exists for the same reason:
// the analysis proves nothing inside the body reads the copied-from value
// after a write, and proves nothing at all about who else holds the value the
// loop was handed. A `Part` or a `Channel` branches from one value —
// bench/mahoraga/i05_jumps runs two loops over the *same* parsed list, one per
// Part — so without this the first loop's writes would be visible to the
// second, which is a wrong answer rather than a slow one.
//
// It clones every list the state reaches through tuple fields, which is
// exactly the set optimizer.ownableLoopState allows a mark to be rooted at.
// One copy on entry, amortized over every write the loop makes.
func ownLoopState(body []*ir.Node, v ir.Value, t *ir.Type, meta map[string]any) ir.Value {
	if len(body) == 0 {
		return v
	}
	// Any stage may carry the marks: the pass looks at every body stage that
	// threads the state, not only the first.
	if !anyStageInPlace(body) {
		return v
	}
	return ownValue(v, t, "", ownedFields(meta))
}

// anyStageInPlace reports whether some body stage carries a marked update.
func anyStageInPlace(body []*ir.Node) bool {
	for _, stage := range body {
		if lam, _ := stage.Meta["lambda"].(*ast.Lambda); lam != nil && ast.HasInPlace(lam.Body) {
			return true
		}
	}
	return false
}

// ownedFields reads the state fields the optimizer said a marked update writes
// through. A loop that has not been through the pass has no entry, and owning
// everything is the reading that is never wrong.
func ownedFields(meta map[string]any) map[string]bool {
	paths, ok := meta[ir.OwnedFields].([]string)
	if !ok {
		return map[string]bool{ir.OwnsEverything: true}
	}
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[p] = true
	}
	return out
}

// needsOwn reports whether the storage at path has to be copied: because a
// marked update writes it, writes something inside it, or writes a container it
// sits in.
func needsOwn(path string, owned map[string]bool) bool {
	for w := range owned {
		if w == path || pathAncestor(w, path) || pathAncestor(path, w) {
			return true
		}
	}
	return false
}

// pathAncestor reports whether a is a strict ancestor of b. The root path is an
// ancestor of everything, which is the case a plain HasPrefix gets wrong: the
// root is "", so "" + "." is "." and no real path begins with a dot.
func pathAncestor(a, b string) bool {
	if a == b {
		return false
	}
	if a == ir.OwnsEverything {
		return true
	}
	return strings.HasPrefix(b, a+".")
}

func fieldPath(path string, i int) string {
	if path == "" {
		return strconv.Itoa(i)
	}
	return path + "." + strconv.Itoa(i)
}

// ownValue deep-copies the storage a marked update reaches, following tuple
// fields, and leaves everything else shared.
func ownValue(v ir.Value, t *ir.Type, path string, owned map[string]bool) ir.Value {
	if t == nil || !needsOwn(path, owned) {
		return v
	}
	switch t.Kind {
	case ir.KList:
		xs, err := ir.AsList(v)
		if err != nil {
			return v
		}
		return append([]ir.Value(nil), xs...)
	case ir.KTuple:
		// Elems, not Fields — see the note in codegen's ownValueExpr.
		xs, err := ir.AsList(v)
		if err != nil || len(xs) != len(t.Elems) {
			return v
		}
		out := append([]ir.Value(nil), xs...)
		for i, ft := range t.Elems {
			out[i] = ownValue(out[i], ft, fieldPath(path, i), owned)
		}
		return out
	}
	// Every other collection a mark can be rooted at through a tuple field.
	// optimizer.projectedCollection lets an update write into a Map or Set held
	// in loop state, so the copy on entry has to reach those too — a Map taken
	// by reference into the state would have the loop writing through to
	// whatever the caller still holds.
	return ir.CloneCollection(v)
}

func repeatNode(body []*ir.Node, timesM Measured, t *ir.Type, pos token.Position) *ir.Node {
	meta := map[string]any{"kind": "repeat", "nodes": body}
	timesM.Meta(meta, "n")
	return &ir.Node{
		Prim: "Simple Domain (Repeat)", In: t, Out: t,
		Display: "Repeat " + timesM.Describe(), Pos: pos,
		Meta: meta,
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			v = ownLoopState(body, v, t, meta)
			// Measured once, before the first lap: the count is a property of
			// the value entering the loop, not of each lap's value.
			n, err := timesM.Resolve(v)
			if err != nil {
				return nil, err
			}
			for i := int64(0); i < n; i++ {
				label := fmt.Sprintf("Repeat %d iter %d/%d", n, i+1, n)
				if v, err = runIteration(ctx, body, v, label, t); err != nil {
					return nil, err
				}
			}
			return v, nil
		},
	}
}

func whileNode(body []*ir.Node, lam *ast.Lambda, t *ir.Type, pos token.Position) *ir.Node {
	// Hoisted so the Eval closure can read what the optimizer records on it
	// later — the node's Meta and this map are the same map.
	meta := map[string]any{"kind": "while", "nodes": body, "lambda": lam}
	return &ir.Node{
		Prim: "Simple Domain (While)", In: t, Out: t,
		Display: "While", Pos: pos,
		Meta: meta,
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			v = ownLoopState(body, v, t, meta)
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
				if v, err = runIteration(ctx, body, v, fmt.Sprintf("While iter %d", iters+1), t); err != nil {
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
				nv, err := runIteration(ctx, body, v, fmt.Sprintf("Fixed Point iter %d", iters+1), t)
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
