package eval

import (
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
	name  string
	value ir.Value
	typ   *ir.Type
}

var bindings []binding

// PushBinding brings a named local into scope for the lambdas applied after
// it. A later binding of the same name shadows an earlier one.
func PushBinding(name string, v ir.Value, t *ir.Type) {
	bindings = append(bindings, binding{name, v, t})
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
		env[b.name] = b.value
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
