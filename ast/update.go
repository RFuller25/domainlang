package ast

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
		for _, a := range x.Args {
			if HasUpdate(a) {
				return true
			}
		}
		return false
	case *CondExpr:
		return HasUpdate(x.Cond) || HasUpdate(x.Then) || HasUpdate(x.Else)
	case *LetExpr:
		return HasUpdate(x.Value) || HasUpdate(x.Body)
	case *AlsoExpr:
		if HasUpdate(x.Body) {
			return true
		}
		for _, c := range x.Clauses {
			if HasUpdate(c) {
				return true
			}
		}
		return false
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
		for _, a := range x.Args {
			if HasInPlace(a) {
				return true
			}
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
		for _, c := range x.Clauses {
			if HasInPlace(c) {
				return true
			}
		}
	}
	return false
}
