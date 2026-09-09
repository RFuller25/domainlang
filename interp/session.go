// A session: running a program that is not a chain of nodes.
//
// interp.Run threads one value through a linear pipeline and returns. A scope
// whose programs are shaped otherwise — a game, whose Parts are driven by
// events rather than by order — needs to evaluate node lists itself, many
// times, over the lifetime of one run.
//
// It cannot simply call Run per event. Run takes a process-wide lock and is
// not re-entrant, and the machinery it guards is package-level state: the
// local bindings (eval/bindings.go), the globals array (eval/globals.go), the
// ambient For-loop stack (prims/ambient.go) and the current context
// (ir/trace.go). Every one of those is documented as resting on "one
// interpretation at a time, process-wide", and a host that stepped through
// them without holding the lock would be the data race the mutex was added to
// stop.
//
// A Session is that lock, held for the whole run, with the same one-time
// resets Run performs and a way to evaluate a body inside it.
package interp

import (
	"fmt"

	"domain/eval"
	"domain/ir"
)

// Session is one run of a program, held open across many evaluations.
//
// It must be closed, and it is not safe to hold two at once — the second
// would deadlock on the lock the first is holding, which is the honest
// failure for what would otherwise be a silent race.
type Session struct {
	ctx    *ir.Context
	closed bool
}

// NewSession begins a run: it takes the interpretation lock, clears the state
// a previous run may have left behind, and sizes the globals array from the
// pipeline that is about to run.
//
// The caller must Close it. Until then nothing else in this process may
// interpret anything.
func NewSession(p *ir.Pipeline, ctx *ir.Context) *Session {
	runMu.Lock()
	if ctx.Channels == nil {
		ctx.Channels = map[string]ir.Value{}
	}
	// The same two resets Run does, and for the same reasons: a run that
	// ended inside a binding's scope left it behind, and globals are never
	// unwound by a node on the way out.
	eval.ResetBindings()
	eval.ResetGlobals(p.Globals)
	return &Session{ctx: ctx}
}

// Close ends the run and releases the lock. Closing twice is a no-op, so a
// deferred Close beside an explicit one is safe.
func (s *Session) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	runMu.Unlock()
}

// Context is the session's evaluation context.
func (s *Session) Context() *ir.Context { return s.ctx }

// Binding is one value a host puts in scope for a body before running it —
// the key an input carried, the reply a request completed with.
//
// It mirrors prims.RoleBind, which is what the resolver typed the body
// against; the two must agree in name and type or the body will read a value
// of a type it was not checked for.
type Binding struct {
	Name  string
	Type  *ir.Type
	Value ir.Value
}

// StepWith is Step with values in scope for the duration of the body.
//
// The bindings are the ordinary `Consider` mechanism (eval/bindings.go), not
// extra lambda parameters: every lambda in the body — including inside any
// Shikigami inlined there — reads them by name and is otherwise left exactly
// as written, which is what lets one Shikigami be called from bodies carrying
// different payloads.
func (s *Session) StepWith(nodes []*ir.Node, in ir.Value, binds []Binding) (ir.Value, error) {
	if len(binds) == 0 {
		return s.Step(nodes, in)
	}
	for _, b := range binds {
		eval.PushBinding(b.Name, b.Value, b.Type)
	}
	defer eval.PopBindings(len(binds))
	return s.Step(nodes, in)
}

// Step evaluates a body — the resolved nodes of one Part — against a value,
// and returns what it produced.
//
// A panic inside becomes an ordinary error, exactly as Run does at its own
// boundary: a session outlives one bad event, and a host painting a terminal
// must not die with a stack trace over a half-drawn screen.
func (s *Session) Step(nodes []*ir.Node, in ir.Value) (out ir.Value, err error) {
	if s == nil || s.closed {
		return nil, fmt.Errorf("internal error: evaluating in a closed session")
	}
	defer func() {
		if r := recover(); r != nil {
			if ir.IsInterrupt(r) {
				out, err = nil, ir.ErrInterrupted
				return
			}
			out, err = nil, fmt.Errorf("internal error while running a body: %v", r)
		}
	}()
	v := in
	for _, n := range nodes {
		nv, e := ir.EvalNode(s.ctx, n, v)
		if e != nil {
			return nil, e
		}
		v = nv
	}
	return v, nil
}
