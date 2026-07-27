// Ambient bindings for `Simple Domain: For x in <source>` loops. Domain's
// primitives are stateless, package-level closures with no resolver handle
// (Primitive.Build is func(op, args, in, pos), nothing more), so a For
// loop's variable can't be threaded through a changed function signature
// without touching every primitive registration. Instead it lives here, at
// package level: resolveForLoop (prims/control.go) is the only pusher/
// popper, always paired around resolving/evaluating one loop body, and
// prims.Resolve / interp.Run are never called concurrently within one
// process (matching how the rest of this package already assumes
// single-threaded, one-call-at-a-time resolution).
package prims

import "domain/ir"

// ambientBinding is one enclosing For loop's variable: its name (currently
// unused beyond documentation/debugging — lambdas bind ambient params
// positionally, not by name) and its element type.
type ambientBinding struct {
	name string
	typ  *ir.Type
}

var ambientStack []ambientBinding

// pushAmbient adds a new enclosing For loop's binding (resolve time).
func pushAmbient(name string, typ *ir.Type) {
	ambientStack = append(ambientStack, ambientBinding{name, typ})
}

// popAmbient removes the innermost enclosing For loop's binding.
func popAmbient() {
	ambientStack = ambientStack[:len(ambientStack)-1]
}

// ambientDepth is how many extra trailing lambda parameters are currently
// expected — added to a primitive's own fixed arity before comparing
// against a lambda's actual written parameter count.
func ambientDepth() int {
	return len(ambientStack)
}

// ambientTypes returns the currently enclosing For loops' element types,
// outermost first, as a fresh slice — append these after a primitive's own
// lambda parameter types before calling typecheck.LambdaType.
//
// ambientStack (resolve time) is always fully unwound by the time any Eval
// closure runs — resolveForLoop pops it right after resolving the body, long
// before interp.Run ever calls Eval. So during Eval, ambientTypes falls back
// to the runtime value stack below, which carries each lap's element type
// alongside its value for exactly this purpose: static-type-dependent
// eval-time logic (e.g. sum() of a runtime-empty list picking Int vs Float
// via eval.go's sumIsFloat) needs the real element type inside a For body,
// not a nil paramTypes it would otherwise see.
func ambientTypes() []*ir.Type {
	if len(ambientStack) > 0 {
		ts := make([]*ir.Type, len(ambientStack))
		for i, b := range ambientStack {
			ts[i] = b.typ
		}
		return ts
	}
	if len(ambientValues) == 0 {
		return nil
	}
	ts := make([]*ir.Type, len(ambientValues))
	for i, b := range ambientValues {
		ts[i] = b.typ
	}
	return ts
}

// ambientRuntimeBinding is one enclosing For loop's current lap: its element
// value plus the value's static type (known at resolve time, captured in
// resolveForLoop's Eval closure, and threaded through here so ambientTypes
// can still answer correctly once resolve-time's ambientStack has unwound).
type ambientRuntimeBinding struct {
	value ir.Value
	typ   *ir.Type
}

// ambientValues is the runtime stack of enclosing For loops' current lap
// bindings — pushed/popped once per lap by resolveForLoop's Eval, mirroring
// ambientStack's shape one-for-one.
var ambientValues []ambientRuntimeBinding

// pushAmbientValue adds the current lap's element value and its static type
// (runtime).
func pushAmbientValue(v ir.Value, t *ir.Type) {
	ambientValues = append(ambientValues, ambientRuntimeBinding{value: v, typ: t})
}

// popAmbientValue removes the innermost enclosing For loop's current
// binding.
func popAmbientValue() {
	ambientValues = ambientValues[:len(ambientValues)-1]
}

// ambientArgs returns the currently enclosing For loops' current element
// values, outermost first, as a fresh slice — append these after a
// primitive's own lambda argument values before calling
// eval.EvalLambdaTyped.
func ambientArgs() []ir.Value {
	if len(ambientValues) == 0 {
		return nil
	}
	vs := make([]ir.Value, len(ambientValues))
	for i, b := range ambientValues {
		vs[i] = b.value
	}
	return vs
}
