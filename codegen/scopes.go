// Which Innate Domains this backend can compile.
//
// The compiler consumes a pipeline, and a pipeline is only a linear chain of
// nodes in a scope that says it is. A scope whose programs are shaped
// differently needs a backend that knows their shape, and until one exists
// `domain build` has to refuse rather than emit an ordinary program from an
// extraordinary pipeline.
//
// That refusal is the same discipline the package already follows for a
// primitive with no lowering: fail with a positioned error and keep working
// under `domain run`, rather than diverge from the interpreter in silence.
package codegen

import (
	"fmt"

	"domain/ir"
)

// compilable is the set of scope names this backend has an emitter for. The
// empty name covers a pipeline built without one — every test fixture, and
// every caller from before scopes existed.
var compilable = map[string]bool{
	"":               true,
	"Advent of Code": true,
}

// RegisterScope records that this backend can compile a scope's programs. It
// is how a scope with its own emitter opts in.
func RegisterScope(name string) { compilable[name] = true }

// Compilable reports whether this backend has an emitter for a scope. It is
// for the tools that decide what to *attempt* — a documentation harness that
// compiles every example, say — so that a scope which is run-only is skipped
// deliberately rather than failing.
func Compilable(scope string) bool { return compilable[scope] }

// checkScope refuses a pipeline this backend has no emitter for.
//
// The error carries the position of the program's first node, which is the
// closest thing a whole-program refusal has to a line: the alternative is a
// bare message with no position at all, and every other error this package
// produces has one.
func checkScope(p *ir.Pipeline) error {
	if compilable[p.Scope] {
		return nil
	}
	err := &ir.RuntimeError{
		Prim: "domain build",
		Msg: fmt.Sprintf("the %s Innate Domain does not compile yet; run it with `domain run` instead",
			p.Scope),
	}
	if len(p.Nodes) > 0 {
		err.Pos = p.Nodes[0].Pos
	}
	return err
}
