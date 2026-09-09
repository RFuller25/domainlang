package optimizer_test

import (
	"testing"

	"domain/ir"
	"domain/optimizer"
)

// The optimizer must hand back a pipeline that still knows which Innate
// Domain produced it. It mutates in place today, so the field survives
// without anything being done about it — which is exactly why this is worth
// pinning: a pass that ever rebuilds the pipeline would drop the scope
// silently, and the symptom would be a program running under the wrong host.
func TestOptimizeKeepsScope(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		p := &ir.Pipeline{Scope: "Advent of Code", Globals: 2}
		optimizer.Optimize(p, enabled)
		if p.Scope != "Advent of Code" {
			t.Errorf("optimize(enabled=%v): scope = %q, want it preserved", enabled, p.Scope)
		}
		if p.Globals != 2 {
			t.Errorf("optimize(enabled=%v): globals = %d, want 2", enabled, p.Globals)
		}
	}
}
