// Package interp is the tree-walking evaluator. It threads a single "current
// value" through the linear IR pipeline, invoking each node's interpreter
// implementation in turn.
package interp

import (
	"fmt"
	"sync"

	"domain/eval"
	"domain/ir"
)

// runMu makes one interpretation at a time, process-wide.
//
// The evaluator keeps several stacks at package level rather than threading
// them through every primitive's signature: the local bindings (eval/bindings.go),
// the current Context (ir/trace.go), the ambient For-loop variables
// (prims/ambient.go), and the lambda and foreign-block watchers. Each of those
// is documented as resting on one assumption — that Run is never called
// concurrently within a process — and until now nothing enforced it. It was not
// hypothetical: the compiler's differential tests run in parallel and each one
// interprets a program as its oracle, which is a data race the detector finds.
//
// A mutex is the small fix and the honest one. Threading five stacks through
// every primitive would be a redesign of the whole registration signature, and
// the assumption is not one anybody wants to relax: nothing in this tree
// interprets for throughput. The timed path runs subprocesses precisely so that
// in-process state cannot be shared (see runner/runner.go), and the editors and
// the REPL run one program at a time by construction. What concurrency there is
// wants correctness, not parallelism, and now gets it.
//
// Run is not re-entrant, and cannot become so without deadlocking: nested
// bodies — a loop iteration, a Channel, a Part — go through ir.EvalNode and
// runBody rather than back through here, which is what makes a plain mutex
// safe. A caller that installs a watcher (eval.WatchApplications,
// prims.WatchForeignRuns) around a run still has to do its own sequencing; the
// lock covers the run, not the setup either side of it.
var runMu sync.Mutex

// Run executes the pipeline, returning the final value. Binding Vows are
// evaluated in place; on violation they surface as an *ir.RuntimeError.
//
// One run at a time, process-wide: see runMu.
func Run(p *ir.Pipeline, ctx *ir.Context) (result ir.Value, err error) {
	runMu.Lock()
	defer runMu.Unlock()
	// Guard against any primitive panicking so users never see a raw stack.
	// An interrupt (ir/interrupt.go) arrives the same way but is not a bug:
	// it is the only way out of a half-finished loop iteration, so it is
	// translated back into an ordinary error here.
	defer func() {
		if r := recover(); r != nil {
			if ir.IsInterrupt(r) {
				result, err = nil, ir.ErrInterrupted
				return
			}
			err = fmt.Errorf("internal error during interpretation: %v", r)
		}
	}()

	if ctx.Channels == nil {
		ctx.Channels = map[string]ir.Value{}
	}
	// Local bindings are dynamically scoped, pushed and popped by the Consider
	// nodes that own them (prims/locals.go). A run that ended inside one — an
	// error, an interrupt — leaves its bindings behind, so each run starts by
	// clearing them rather than trusting the last one to have unwound.
	eval.ResetBindings()
	// Globals are not scoped and so are never unwound by a node on the way
	// out; the run sizes and clears the whole array up front instead, from the
	// count the pipeline itself carries.
	eval.ResetGlobals(p.Globals)
	var cur ir.Value
	for _, n := range p.Nodes {
		// ir.EvalNode reports to ctx.Trace when one is set; without a tracer it
		// is n.Eval plus a nil check.
		v, e := ir.EvalNode(ctx, n, cur)
		if e != nil {
			return nil, e
		}
		cur = v
	}
	return cur, nil
}
