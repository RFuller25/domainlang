// Hosts: how a pipeline is run.
//
// Every program was run one way — thread a value through a linear chain of
// nodes — because every program was the same kind of program. An `Innate
// Domain` can now say otherwise, and a scope whose programs are not a linear
// chain needs a runner that is not Run.
//
// Hosts are keyed by scope name, which is the one thing a pipeline already
// carries. That is not only convenience: interp cannot import prims to ask a
// scope what its runner is called, because prims' own tests import interp,
// and a package cannot be on both ends of that.
//
// The default is Run. A scope that says nothing about how it runs gets the
// behaviour every program has always had, so registering a host is something
// only a scope that needs one does.
package interp

import (
	"fmt"

	"domain/ir"
)

// Host runs a whole pipeline and returns its final value.
type Host func(p *ir.Pipeline, ctx *ir.Context) (ir.Value, error)

// hosts maps a scope name to the runner that scope's programs need. A scope
// absent from it runs on Run.
var hosts = map[string]Host{}

// RegisterHost gives a scope a runner of its own. Registering twice for one
// scope panics: two runners answering for one Innate Domain is a mistake in
// how the binary was assembled, not a condition to recover from at run time.
func RegisterHost(scope string, h Host) {
	if _, taken := hosts[scope]; taken {
		panic(fmt.Sprintf("interp: the %s Innate Domain already has a host", scope))
	}
	hosts[scope] = h
}

// HasHost reports whether a scope registered a runner of its own.
//
// It is for a binary that wants to assert it linked one in. A host registers
// itself from an init, so a package that runs programs asks for it with a
// blank import — and a blank import is exactly the kind of line somebody
// removes while tidying, with no compile error to stop them.
func HasHost(scope string) bool {
	_, ok := hosts[scope]
	return ok
}

// HostFor returns the runner a pipeline's scope asks for, which is Run unless
// that scope registered otherwise.
func HostFor(p *ir.Pipeline) Host {
	if h, ok := hosts[p.Scope]; ok {
		return h
	}
	return Run
}

// RunScoped runs a pipeline under its scope's host.
//
// Callers holding a whole program should use this rather than Run: a program
// is only a linear chain of nodes in a scope that says it is, and running one
// that is not through Run would walk its Parts in source order and quietly
// produce nothing.
func RunScoped(p *ir.Pipeline, ctx *ir.Context) (ir.Value, error) {
	return HostFor(p)(p, ctx)
}
