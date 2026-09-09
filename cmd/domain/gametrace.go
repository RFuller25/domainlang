// --trace: watching a Game Dev run one Part at a time.
//
// A game has no linear pipeline for `expansion: visualize` to step through —
// its "steps" are Part bodies, dispatched by game/host.go across an
// unbounded, event-driven run — so this does not reuse that stepper. It
// reuses something smaller and already there: game/replay.go already
// brackets every Part body with ctx.PushFrame/PopFrame (interp's existing
// Tracer hook, the one --stats and expansion: visualize both already use),
// and PushFrame is a no-op with nothing costed unless a Tracer is installed.
// Installing this one is the whole of --trace.
package main

import (
	"fmt"
	"io"

	"domain/ir"
)

// gameTracer prints one line per top-level frame: the label a Part body was
// written under (already "Part On \"key a\":", "Part Every 100:" and so on —
// see game/host.go's gamePart.desc) and what it produced.
//
// PushFrame/PopFrame bracket every nested sub-pipeline, not only a Part's own
// body — a loop iteration inside it, a Consider body, a Shikigami inlined at
// a call site all open and close their own frames in between. Only the
// outermost one closing (the label stack running empty again) is a Part
// finishing, which is the granularity --trace promises: one line per step of
// the run, not a walk of everything inside it.
type gameTracer struct {
	w      io.Writer
	labels []string
}

func (t *gameTracer) Step(ir.StepEvent) {}

func (t *gameTracer) PushFrame(label string, _ *ir.Type) {
	t.labels = append(t.labels, label)
}

func (t *gameTracer) PopFrame(out ir.Value) {
	if len(t.labels) == 0 {
		return
	}
	label := t.labels[len(t.labels)-1]
	t.labels = t.labels[:len(t.labels)-1]
	if len(t.labels) != 0 {
		return
	}
	if out == nil {
		fmt.Fprintf(t.w, "%s -> (failed)\n", label)
		return
	}
	fmt.Fprintf(t.w, "%s -> %s\n", label, ir.FormatValue(out))
}
