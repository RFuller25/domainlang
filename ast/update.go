package ast

import "slices"

// HasUpdate reports whether e contains a `:=` anywhere inside it — that is,
// whether evaluating it writes to a name as well as producing a value.
//
// It lives here rather than in any one consumer because every layer needs the
// same answer and none of them can afford a different one: the resolver turns
// the interpreter's boxing on with it (prims.Resolve), the optimizer stands
// its rewrites down with it, the compiler decides where to force evaluation
// order with it, and the visualizer refuses to replay an application with it.
// A tree that is not an update is exactly the tree every one of them was
// written against before `:=` existed.
//
// `also` on its own is *not* an update: it evaluates and discards, which
// changes nothing a later reader can observe. It is only ever written to carry
// updates, but a clause that carries none is as pure as the body it follows.
func HasUpdate(e Expr) bool {
	switch x := e.(type) {
	case *AssignExpr:
		return true
	case *UnaryExpr:
		return HasUpdate(x.X)
	case *BinaryExpr:
		return HasUpdate(x.Left) || HasUpdate(x.Right)
	case *FieldAccess:
		return HasUpdate(x.Target)
	case *CallExpr:
		return slices.ContainsFunc(x.Args, HasUpdate)
	case *CondExpr:
		return HasUpdate(x.Cond) || HasUpdate(x.Then) || HasUpdate(x.Else)
	case *LetExpr:
		return HasUpdate(x.Value) || HasUpdate(x.Body)
	case *AlsoExpr:
		if HasUpdate(x.Body) {
			return true
		}
		return slices.ContainsFunc(x.Clauses, HasUpdate)
	default:
		// Literals, identifiers, and the BlockBody standing in for a
		// sub-pipeline — whose statements are not expressions and cannot carry
		// an update, because `:=` is an expression-layer operator.
		return false
	}
}

// UpdatedNames collects the names e writes to, into names. The resolver uses
// it to find the bindings a statement's expressions update *before* it decides
// how to lower them, which is what lets an updated binding keep a cell instead
// of being folded into a literal.
func UpdatedNames(e Expr, names map[string]bool) {
	switch x := e.(type) {
	case *AssignExpr:
		names[x.Name] = true
		UpdatedNames(x.Value, names)
	case *UnaryExpr:
		UpdatedNames(x.X, names)
	case *BinaryExpr:
		UpdatedNames(x.Left, names)
		UpdatedNames(x.Right, names)
	case *FieldAccess:
		UpdatedNames(x.Target, names)
	case *CallExpr:
		for _, a := range x.Args {
			UpdatedNames(a, names)
		}
	case *CondExpr:
		UpdatedNames(x.Cond, names)
		UpdatedNames(x.Then, names)
		UpdatedNames(x.Else, names)
	case *LetExpr:
		UpdatedNames(x.Value, names)
		// The local shadows an outer name of the same spelling for the whole
		// body, so a write in there is a write to the local and not to the
		// binding outside. Collecting it anyway would only cost that binding
		// its constant folding rather than its correctness, but the shadowing
		// rule is cheap to honor and the name is the whole question here.
		inner := map[string]bool{}
		UpdatedNames(x.Body, inner)
		delete(inner, x.Name)
		for n := range inner {
			names[n] = true
		}
	case *AlsoExpr:
		UpdatedNames(x.Body, names)
		for _, c := range x.Clauses {
			UpdatedNames(c, names)
		}
	}
}

// HasInPlace reports whether any update in e carries the optimizer's in-place
// annotation (see optimizer/linear.go and CallExpr.InPlace).
//
// The primitives that drive a fold ask this to decide whether to clone their
// accumulator on entry. They cannot read the pass's result any other way: a
// node's Eval closure is built at resolve time, before the optimizer runs, and
// the lambda it captures is the only thing the pass and the closure share.
func HasInPlace(e Expr) bool {
	switch x := e.(type) {
	case *CallExpr:
		if x.InPlace {
			return true
		}
		if slices.ContainsFunc(x.Args, HasInPlace) {
			return true
		}
	case *UnaryExpr:
		return HasInPlace(x.X)
	case *BinaryExpr:
		return HasInPlace(x.Left) || HasInPlace(x.Right)
	case *FieldAccess:
		return HasInPlace(x.Target)
	case *CondExpr:
		return HasInPlace(x.Cond) || HasInPlace(x.Then) || HasInPlace(x.Else)
	case *LetExpr:
		return HasInPlace(x.Value) || HasInPlace(x.Body)
	case *AssignExpr:
		return HasInPlace(x.Value)
	case *AlsoExpr:
		if HasInPlace(x.Body) {
			return true
		}
		if slices.ContainsFunc(x.Clauses, HasInPlace) {
			return true
		}
	}
	return false
}

// nondeterministic is every builtin whose value is not decided by its
// arguments.
//
// There are five, and they are the only way an expression in this language can
// give two answers to one question. Everything else is a function of what it
// was handed, which is the assumption almost every optimizer pass rests on:
// that a stage may be reordered, fused, folded or run once instead of twice
// without changing what the program says.
//
// `random` and `randomf` and `pick` draw from the run's stream, so calling one
// twice is two draws. `frame` and `elapsed` read the run's progress, so the
// same call answers differently as the program goes on.
var nondeterministic = map[string]bool{
	"random": true, "randomf": true, "pick": true,
	"frame": true, "elapsed": true,
}

// Nondeterministic reports whether a builtin's value depends on anything but
// its arguments.
func Nondeterministic(name string) bool { return nondeterministic[name] }

// HasNondeterminism reports whether an expression contains such a call.
//
// It is to randomness what HasInPlace is to updates: the question a pass has
// to ask before treating an expression as a function of its inputs. Getting it
// wrong in one direction costs a stage its rewrites; getting it wrong in the
// other folds a die roll into a constant at resolve time, and the program
// plays the same game every time it is run.
func HasNondeterminism(e Expr) bool {
	switch x := e.(type) {
	case *CallExpr:
		if id, ok := x.Fn.(*Ident); ok && nondeterministic[id.Name] {
			return true
		}
		if slices.ContainsFunc(x.Args, HasNondeterminism) {
			return true
		}
	case *UnaryExpr:
		return HasNondeterminism(x.X)
	case *BinaryExpr:
		return HasNondeterminism(x.Left) || HasNondeterminism(x.Right)
	case *FieldAccess:
		return HasNondeterminism(x.Target)
	case *CondExpr:
		return HasNondeterminism(x.Cond) || HasNondeterminism(x.Then) || HasNondeterminism(x.Else)
	case *LetExpr:
		return HasNondeterminism(x.Value) || HasNondeterminism(x.Body)
	case *AssignExpr:
		return HasNondeterminism(x.Value)
	case *AlsoExpr:
		if HasNondeterminism(x.Body) {
			return true
		}
		if slices.ContainsFunc(x.Clauses, HasNondeterminism) {
			return true
		}
	}
	// A record literal is sugar for a `record(...)` call, so the CallExpr case
	// above already covers `{here: random(3)}`.
	return false
}
