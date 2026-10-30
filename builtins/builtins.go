// Package builtins is the registry of expression-layer functions.
//
// A builtin used to be three separate `case` arms: a typing rule in
// typecheck.callType, an evaluator in eval.evalCall and an emitter in
// codegen.compileCall, plus an entry in each of two name tables. Nothing but
// tests said the arms agreed. Here one Spec holds all of it: the name, the
// argument count, how it types, how the interpreter runs it and — attached by
// package codegen, which owns the helper text an emitter splices in — how the
// compiler emits it.
//
// The layers still own what they always owned. This package imports only ir, so
// typecheck, eval and codegen can all depend on it without a cycle, and what a
// layer needs from its surroundings arrives through a small interface (Env for
// the evaluator, Emitter for the code generator) rather than an import.
//
// Migration is one group at a time. A name found here is dispatched here; a
// name not found falls through to the switch it has always lived in, so the two
// can coexist until the last arm moves. text.go is the first group.
package builtins

import (
	"fmt"
	"sort"

	"domain/ir"
)

// Spec describes one builtin. Type and Eval report errors without a source
// position; the layer that called them adds it, so the message each builtin
// writes is the same whichever layer is looking.
type Spec struct {
	Name  string
	Arity int // the exact argument count

	// Type returns the call's type given its argument types. A nil argument
	// type is possible (an expression the typer could not resolve) and every
	// check here must tolerate it, which ir.Type.Equal already does.
	Type func(args []*ir.Type) (*ir.Type, error)

	// Eval runs the call on already-evaluated arguments.
	Eval func(env Env, args []ir.Value) (ir.Value, error)

	// Emit produces the Go expression for the call. It is set by package
	// codegen (see SetEmit) because the helper declarations it splices in live
	// there.
	Emit EmitFunc
}

// Env is what the interpreter lends an evaluator: the limits that depend on
// whether the program is being run or merely constant-folded.
type Env interface {
	// BuildLimit is the largest collection or text one call may construct.
	BuildLimit() int64
}

// Emitter is what the code generator lends an emitter.
type Emitter interface {
	// Imp records a standard-library import the emitted expression needs.
	Imp(pkgs ...string)
	// Helper declares a named top-level helper once, with the imports it needs.
	Helper(name, src string, imports ...string)
}

// EmitFunc returns the Go expression for a call, and its Domain type. args are
// the already-compiled argument expressions, types their Domain types.
type EmitFunc func(e Emitter, args []string, types []*ir.Type) (string, *ir.Type, error)

var registry = map[string]*Spec{}

func register(specs ...Spec) {
	for i := range specs {
		s := &specs[i]
		if s.Type == nil || s.Eval == nil {
			panic("builtins: " + s.Name + " needs a Type and an Eval")
		}
		if _, dup := registry[s.Name]; dup {
			panic("builtins: " + s.Name + " registered twice")
		}
		registry[s.Name] = s
	}
}

// Lookup returns the registered builtin, or nil if the name still lives in the
// layers' own switches.
func Lookup(name string) *Spec { return registry[name] }

// Names lists every registered builtin, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SetEmit attaches the compiler's emitter to a registered builtin. It is called
// from package codegen's init, and panics on a name that is not registered or
// already has one, because either is a wiring mistake rather than a runtime
// condition.
func SetEmit(name string, f EmitFunc) {
	s := registry[name]
	if s == nil {
		panic(fmt.Sprintf("builtins: emitter for unregistered builtin %q", name))
	}
	if s.Emit != nil {
		panic(fmt.Sprintf("builtins: %q already has an emitter", name))
	}
	s.Emit = f
}
