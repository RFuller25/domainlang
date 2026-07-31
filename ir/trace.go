package ir

import "time"

// Execution tracing.
//
// interp.Run is a flat loop over the top-level node list, and every nested
// construct — loop bodies, Channel bodies, Part bodies — is captured inside its
// parent's Eval closure, out of reach of the node list. So anything that wants
// to observe what a program actually did (a step-by-step visualizer, per-stage
// timings) cannot get it by walking the pipeline; it has to be told as the run
// happens.
//
// The hook lives on Context for the same reason Release does: the nodes that
// need to report are reached through closures, so there is nothing to rewrite.
// A nil Tracer costs one nil check per node, which is why an untraced
// `domain run` is unaffected (BenchmarkTracedVsUntraced pins that).
//
// There are exactly two places nodes are evaluated — interp.Run for the top
// level, and prims.runBody, which every nested construct goes through: the
// three loop kinds, the Channel and Part node Evals, and one application of a
// nested `Using:` body. Instrumenting those two covers the language, and a
// construct added later cannot slip past them.

// StepEvent is one node evaluation.
type StepEvent struct {
	Node  *Node
	Depth int    // 0 at the top level, deeper inside loop/channel/part bodies
	Frame string // the innermost enclosing frame, e.g. `Repeat 4 iter 2/4`
	In    Value
	Out   Value
	Err   error
	Dur   time.Duration
}

// Tracer observes a run. Implementations decide what to keep: the --stats
// aggregator keeps counts and durations and no values at all, while the
// visualizer's recorder keeps bounded renderings.
type Tracer interface {
	// Step is called after each node evaluation, including a failing one.
	Step(StepEvent)
	// PushFrame and PopFrame bracket a nested sub-pipeline: one loop
	// iteration, a Channel body, a Part body, or one application of a nested
	// `Using:` body. PushFrame carries the type the body produces and PopFrame
	// the value it produced — see Context.PushFrame and Context.PopFrame.
	PushFrame(label string, out *Type)
	PopFrame(out Value)
}

// currentCtx is the Context of the evaluation currently in progress, so code
// reached through a lambda body — which has no Context parameter of its own —
// can still find it. A nested pipeline standing in for a lambda body needs one
// (a `Reveal:` inside it prints, a `Binding Vow` inside it reads Release), and
// threading a Context through the whole expression evaluator to serve those two
// would change every lambda-running primitive's call.
//
// It is package level for the same reason prims' ambient stack is, and under
// the same standing assumption: interp.Run is never called concurrently within
// one process. EvalNode saves and restores rather than assigning, so a nested
// run (a loop body, a block body) leaves the outer Context intact on the way
// out — and a pipeline run from inside another one cannot strand the wrong one.
var currentCtx *Context

// CurrentContext is the Context of the node evaluation in progress, or nil
// outside one.
func CurrentContext() *Context { return currentCtx }

// EvalNode runs one node, reporting it to the Context's tracer when there is
// one. Every evaluation site goes through this, so tracing can never miss a
// construct that was added later — and neither can the current-Context record.
func EvalNode(ctx *Context, n *Node, in Value) (Value, error) {
	prev := currentCtx
	currentCtx = ctx
	defer func() { currentCtx = prev }()

	if ctx == nil || ctx.Trace == nil {
		return n.Eval(ctx, in)
	}
	start := time.Now()
	out, err := n.Eval(ctx, in)
	ctx.Trace.Step(StepEvent{
		Node:  n,
		Depth: len(ctx.frames),
		Frame: ctx.currentFrame(),
		In:    in,
		Out:   out,
		Err:   err,
		Dur:   time.Since(start),
	})
	return out, err
}

// Tracing reports whether anything is watching this run. It exists for the one
// caller that cannot make its frame free otherwise: a `Using:` body runs once
// per element, and the bookkeeping that brackets it — a deferred call, to close
// the frame even if the run is interrupted — costs something per element even
// when the frame goes nowhere. Everywhere else, PushFrame's own nil check is
// the whole cost.
func (c *Context) Tracing() bool { return c != nil && c.Trace != nil }

// PushFrame enters a nested sub-pipeline, naming the type its body produces.
//
// The type comes from the caller because only the caller knows it: a loop's lap
// produces the loop's own type, a Channel or Part body produces whatever its
// last stage does, and a `Using:` body produces one element's worth of result.
// Nothing downstream can work it out from the value alone.
//
// It is a no-op without a tracer, so loop bodies pay nothing for the labels in
// an ordinary run.
func (c *Context) PushFrame(label string, out *Type) {
	if c == nil || c.Trace == nil {
		return
	}
	c.frames = append(c.frames, label)
	c.Trace.PushFrame(label, out)
}

// PopFrame leaves the innermost sub-pipeline, reporting what it produced.
//
// The result is the frame's own answer, which is not always the value its
// enclosing node returns: a Channel and a Part are passthroughs — the pipeline
// carries on with the value that entered them — so the body's result is
// visible nowhere else, and a visualizer showing only the node's output would
// report the block's input as its output. A body that did not finish (it
// failed, or the run was interrupted) reports nil.
func (c *Context) PopFrame(out Value) {
	if c == nil || c.Trace == nil || len(c.frames) == 0 {
		return
	}
	c.frames = c.frames[:len(c.frames)-1]
	c.Trace.PopFrame(out)
}

func (c *Context) currentFrame() string {
	if len(c.frames) == 0 {
		return ""
	}
	return c.frames[len(c.frames)-1]
}

// SizeOf reports how much data a value holds, and whether that is a meaningful
// question. Collections report their element count, Text its length in bytes,
// and a Grid its cell count; scalars report nothing, since "1" is not a size.
func SizeOf(v Value) (int, bool) {
	switch x := v.(type) {
	case string:
		return len(x), true
	case []Value: // List and Tuple share this representation
		return len(x), true
	case *RecordValue:
		return len(x.Fields), true
	case *MapValue:
		return x.Len(), true
	case *SetValue:
		return x.Len(), true
	case *GridValue:
		return x.Rows * x.Cols, true
	case *SparseValue:
		return x.Len(), true
	}
	return 0, false
}
