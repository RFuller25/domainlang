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
// There are exactly four places nodes are evaluated — interp.Run,
// prims.runBody (shared by all three loop kinds), the Channel node's Eval, and
// the Part node's Eval — and instrumenting them covers the language.

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
	// iteration, a Channel body, or a Part body.
	PushFrame(label string)
	PopFrame()
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

// PushFrame enters a nested sub-pipeline. It is a no-op without a tracer, so
// loop bodies pay nothing for the labels in an ordinary run.
func (c *Context) PushFrame(label string) {
	if c == nil || c.Trace == nil {
		return
	}
	c.frames = append(c.frames, label)
	c.Trace.PushFrame(label)
}

// PopFrame leaves the innermost sub-pipeline.
func (c *Context) PopFrame() {
	if c == nil || c.Trace == nil || len(c.frames) == 0 {
		return
	}
	c.frames = c.frames[:len(c.frames)-1]
	c.Trace.PopFrame()
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
