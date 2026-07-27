// Package interp is the tree-walking evaluator. It threads a single "current
// value" through the linear IR pipeline, invoking each node's interpreter
// implementation in turn.
package interp

import (
	"fmt"

	"domain/ir"
)

// Run executes the pipeline, returning the final value. Binding Vows are
// evaluated in place; on violation they surface as an *ir.RuntimeError.
func Run(p *ir.Pipeline, ctx *ir.Context) (result ir.Value, err error) {
	// Guard against any primitive panicking so users never see a raw stack.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error during interpretation: %v", r)
		}
	}()

	if ctx.Channels == nil {
		ctx.Channels = map[string]ir.Value{}
	}
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
