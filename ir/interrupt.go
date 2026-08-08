package ir

import (
	"errors"
	"sync/atomic"
)

// Interrupting a run in progress.
//
// `While` and `Iterate Until Fixed Point` are unbounded by design (a limit
// must never be the reason a correct program cannot run), so a
// non-terminating loop spins until something stops it. Under `domain run`
// that something is Ctrl+C killing the process. The REPL has no such escape:
// it owns the terminal in raw mode, so the key never becomes a signal, and
// the evaluation it is waiting on is the only thing that can end.
//
// An Interrupter is that escape. It rides the existing trace hook — the one
// place every node evaluation in the language already passes through — and
// aborts the run by panicking with a private signal value that interp.Run
// recognizes and turns back into ErrInterrupted. Panicking rather than
// returning an error is what makes it work from inside a loop body: there is
// no error path out of a half-finished iteration, but there is a stack.
//
// The granularity is one node: a run stops between evaluations, not inside
// one, so a single long-running primitive (a huge Permutations, say) still
// has to finish. Loops, which are what actually run away, are interruptible
// after every stage of every iteration.

// ErrInterrupted is the error a run fails with after its Interrupter was
// stopped.
var ErrInterrupted = errors.New("interrupted")

// interruptSignal is the panic value an Interrupter raises. It is
// deliberately unexported: nothing outside this package should be able to
// forge one, and interp identifies it through IsInterrupt.
type interruptSignal struct{}

// IsInterrupt reports whether a recovered panic value is an interrupt raised
// by an Interrupter, rather than a genuine bug to report as one.
func IsInterrupt(recovered any) bool {
	_, ok := recovered.(interruptSignal)
	return ok
}

// Interrupter is a Tracer that aborts the run it is installed on when Stop is
// called from another goroutine. Inner, when set, is a tracer to keep feeding
// — so a run can be profiled and interruptible at the same time.
type Interrupter struct {
	Inner   Tracer
	stopped atomic.Bool
}

// NewInterrupter returns an Interrupter wrapping inner (which may be nil).
func NewInterrupter(inner Tracer) *Interrupter { return &Interrupter{Inner: inner} }

// Stop asks the run to abort at its next node boundary. It is safe to call
// from any goroutine, and safe to call after the run has already finished.
func (i *Interrupter) Stop() { i.stopped.Store(true) }

// Stopped reports whether Stop was called. A caller that reports the outcome
// of a run needs it: a run someone interrupted and a run that failed on its
// own are different things to say about a program, and the error alone cannot
// tell them apart.
func (i *Interrupter) Stopped() bool { return i.stopped.Load() }

// Step forwards the event and then aborts if the run has been stopped. The
// check is after the inner tracer, so a profile keeps the step that was
// running when the interrupt arrived.
func (i *Interrupter) Step(e StepEvent) {
	if i.Inner != nil {
		i.Inner.Step(e)
	}
	if i.stopped.Load() {
		panic(interruptSignal{})
	}
}

// PushFrame forwards the frame. It never interrupts: a frame push is bracketed
// by a matching pop, and unwinding between them would leave the tracer it
// wraps holding a frame that never closed. Step runs often enough — every
// stage of every iteration — that waiting for the next one costs nothing.
func (i *Interrupter) PushFrame(label string, out *Type) {
	if i.Inner != nil {
		i.Inner.PushFrame(label, out)
	}
}

// PopFrame forwards the frame close.
func (i *Interrupter) PopFrame(out Value) {
	if i.Inner != nil {
		i.Inner.PopFrame(out)
	}
}
