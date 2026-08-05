package typecheck

import "domain/ir"

// The resolve-time half of `Consider x As/Of …` bindings (see ast.Binding):
// the types of the locals currently in scope. LambdaType seeds every lambda's
// environment from here, so a lambda body can name a binding without the
// primitive consuming that lambda knowing bindings exist — the same trick
// prims/ambient.go plays for a `For` loop's variable, one layer down.
//
// Like that stack this is package-level mutable state rather than a parameter,
// because a primitive is a stateless package-level closure with no resolver
// handle: threading a scope through Primitive.Build would mean changing every
// registration in the vocabulary. prims.Resolve owns the push/pop pairing and
// is never called concurrently within one process, which is the same bargain
// the ambient stack already makes.

type binding struct {
	name string
	typ  *ir.Type
}

var bindings []binding

// PushBinding brings a named local into scope for the lambdas resolved after
// it. A later binding of the same name shadows an earlier one.
func PushBinding(name string, t *ir.Type) {
	bindings = append(bindings, binding{name, t})
}

// PopBindings removes the n most recently pushed bindings.
func PopBindings(n int) {
	bindings = bindings[:len(bindings)-n]
}

// ResetBindings drops every binding. Resolution is a fresh start: an error
// part-way through one program must not leak scope into the next, which is
// what the language server and the REPL would otherwise see.
func ResetBindings() { bindings = nil }

// BindingDepth is how many bindings are currently in scope.
func BindingDepth() int { return len(bindings) }

// seedBindings copies the in-scope bindings into env, which the caller then
// overwrites with the lambda's own parameters — so a parameter always shadows
// a binding of the same name.
func seedBindings(env Env) {
	for _, b := range bindings {
		env[b.name] = b.typ
	}
}

// BindingEnv is the in-scope bindings as an environment of their own, for
// typing an expression that is not a lambda body — a binding's own value.
func BindingEnv() Env {
	env := make(Env, len(bindings))
	seedBindings(env)
	return env
}
