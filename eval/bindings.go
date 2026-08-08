package eval

import (
	"sync/atomic"

	"domain/ast"
	"domain/ir"
	"domain/typecheck"
)

// The runtime half of `Consider x As/Of …` bindings (see ast.Binding): the
// values of the locals currently in scope. EvalLambdaTyped seeds every
// application's environment from here, so a lambda body can read a binding
// without any of the primitives that apply lambdas knowing bindings exist.
//
// It mirrors typecheck's resolve-time stack one-for-one, exactly as
// prims/ambient.go's value stack mirrors its type stack, and for the same
// reason: the types are known when the program is lowered, the values only
// once data arrives. prims' Bind node owns the push/pop pairing.

type binding struct {
	name string
	cell *Cell
	typ  *ir.Type
}

var bindings []binding

// Cell is a value a `:=` can write to: the box a name is bound to when the
// program being run contains an update at all (see Updates).
//
// It exists because an environment is *copied* on the way into every scope —
// EvalLambdaTyped builds one per application, letExpr extends one per
// evaluation — so a write into a map would be lost the moment the scope that
// made the copy returned. Copies share the box instead, which is what gives a
// write the lifetime of the name it was written to rather than the lifetime of
// the expression that wrote it: a `consider` local's box dies with the
// expression, a stage binding's box lives in the binding stack and so is still
// there for the next element.
//
// A Cell is stored *as* the value in an Env; evalExpr unwraps one wherever a
// name is read. Nothing outside this package ever sees one, because the only
// door in is a name lookup and the only door out is Deref.
type Cell struct{ V ir.Value }

// Deref unwraps a cell, and passes anything else through. Env values reach a
// few places outside evalExpr — the trace recorder, a binding's own value —
// and every one of them wants the value, not the box.
func Deref(v ir.Value) ir.Value {
	if c, ok := v.(*Cell); ok {
		return c.V
	}
	return v
}

// Whether anything in this process may write to a name.
//
// Boxing is not free — a cell per binding per application — and the vast
// majority of programs never write to a name, so it is off until a resolver
// finds a `:=` (prims.Resolve). With it off, an environment holds exactly the
// values it always held and an application costs exactly what it used to; with
// it on, every binding is boxed rather than only the ones written to, because
// deciding *which* ones would mean walking each body on every application to
// find out.
//
// It only ever turns *on*, which is what makes it safe to keep here rather
// than on the pipeline: boxing is invisible to a program that does not write
// (a name still reads as its value), so a process that has resolved one
// updating program may box for all of them. A flag that could also turn off
// would be a different thing entirely — the language server, the REPL and the
// test suite all resolve one program while another is still running, and
// switching the representation under a run in progress is exactly the race
// this avoids. Atomic for the same reason.
var updates atomic.Bool

// EnableUpdates records that a program containing `:=` has been resolved, and
// so that bindings must be boxed from here on. There is deliberately no way
// back: see updates.
func EnableUpdates() { updates.Store(true) }

// PushBinding brings a named local into scope for the lambdas applied after
// it. A later binding of the same name shadows an earlier one.
func PushBinding(name string, v ir.Value, t *ir.Type) {
	bindings = append(bindings, binding{name, &Cell{V: v}, t})
}

// bindValue is what a binding contributes to an environment: the box when the
// program can write to it, the bare value when it cannot.
func (b binding) bindValue() ir.Value {
	if updates.Load() {
		return b.cell
	}
	return b.cell.V
}

// assign writes v to the innermost binding named name, reporting whether there
// was one. A stage binding outlives the application that writes to it, which
// is the whole point: the next element sees the new value.
func assign(name string, v ir.Value) bool {
	for i := len(bindings) - 1; i >= 0; i-- {
		if bindings[i].name == name {
			bindings[i].cell.V = v
			return true
		}
	}
	return false
}

// PopBindings removes the n most recently pushed bindings.
func PopBindings(n int) {
	bindings = bindings[:len(bindings)-n]
}

// ResetBindings drops every binding, so a run that ended part-way through a
// scope cannot leak it into the next one.
func ResetBindings() { bindings = nil }

// BindingDepth is how many bindings are currently in scope.
func BindingDepth() int { return len(bindings) }

// BindingEnv is the in-scope bindings as an environment and a matching type
// environment, for evaluating an expression that is not a lambda body — a
// binding's own value, computed when the scope opens.
func BindingEnv() (Env, typecheck.Env) {
	env := make(Env, len(bindings))
	types := make(typecheck.Env, len(bindings))
	for _, b := range bindings {
		env[b.name] = b.bindValue()
		types[b.name] = b.typ
	}
	return env, types
}

// EvalExprTyped evaluates an expression against an environment and its static
// types, which is what a binding's value needs: it is an expression rather
// than a lambda body, so it has no parameters to carry the types in.
func EvalExprTyped(e ast.Expr, env Env, types typecheck.Env) (ir.Value, error) {
	return evalExpr(e, env, types)
}
