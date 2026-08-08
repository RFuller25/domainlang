package codegen

import (
	"domain/ast"
	"domain/ir"
)

// Compiling `Consider x As/Of …` bindings (prims/locals.go).
//
// Only the bindings whose values are known at runtime reach the backend at
// all: a constant was substituted into the lambdas that read it while the
// program was resolved, and a function binding was inlined at its call sites,
// so both are already part of the expressions compiled here. What is left is a
// Consider node — a value computed once when the scope opens, and the nodes
// that scope covers.
//
// The value becomes an ordinary Go local, and the name is registered in
// g.bindNames so that compileExpr resolves it the way it resolves an enclosing
// For loop's variable: as a fallback behind the lambda's own parameters, which
// keeps a parameter of the same name shadowing the binding exactly as it does
// in the interpreter.

func (g *gen) emitConsider(n *ir.Node, in string) (string, error) {
	binds, _ := n.Meta[ir.MetaBinds].([]ir.Binding)
	body, _ := n.Meta["nodes"].([]*ir.Node)
	if binds == nil || body == nil {
		return "", unsupported(n, "missing binding metadata")
	}

	// Shadow in a copy: the bindings go out of scope with the nodes they
	// cover, and a sibling statement must not see them.
	saved := g.bindNames
	scoped := make(exprEnv, len(saved)+len(binds))
	for k, v := range saved {
		scoped[k] = v
	}
	g.bindNames = scoped
	defer func() { g.bindNames = saved }()

	for _, b := range binds {
		expr, err := g.bindValue(n, b, in)
		if err != nil {
			return "", err
		}
		goType, err := g.goType(b.Type())
		if err != nil {
			return "", err
		}
		v := g.fresh("dmBind")
		g.wl("var %s %s = %s", v, goType, expr)
		g.wl("_ = %s", v)
		// Registered after its own value is compiled, so a binding written in
		// terms of an earlier one sees that one and never itself.
		scoped[b.Name()] = exprBinding{expr: v, typ: b.Type(), cell: "&" + v}
	}
	return g.emitSequence(body, in)
}

// bindValue compiles one binding's value against the value entering the scope.
func (g *gen) bindValue(n *ir.Node, b ir.Binding, in string) (string, error) {
	switch {
	case b.Lambda() != nil:
		lam, ok := b.Lambda().(*ast.Lambda)
		if !ok || len(lam.Params) == 0 {
			return "", unsupported(n, "binding %q has a malformed lambda", b.Name())
		}
		// The lambda's own parameter is the current value; any parameters
		// after it are the enclosing For loops' ambient ones.
		env := exprEnv{lam.Params[0]: {expr: in, typ: b.In()}}
		saved := g.ambientNames
		g.ambientNames = g.ambientEnv(lam.Params[1:])
		expr, _, err := g.compileExpr(lam.Body, env)
		g.ambientNames = saved
		return expr, err

	case b.BlockNodes() != nil:
		// An operation or a whole sub-pipeline: emitted inline, so its stages
		// are statements of the function this scope lives in and its result is
		// the value bound.
		return g.emitSequence(b.BlockNodes(), in)

	default:
		e, ok := b.Expr().(ast.Expr)
		if !ok {
			return "", unsupported(n, "binding %q has no value", b.Name())
		}
		// An `As` expression cannot see the pipeline value, only the bindings
		// above it — which are already in g.bindNames.
		expr, _, err := g.compileExpr(e, exprEnv{})
		return expr, err
	}
}

// ambientEnv maps a lambda's trailing ambient parameter names onto the
// enclosing For loops' variables, innermost last.
func (g *gen) ambientEnv(trailing []string) exprEnv {
	env := make(exprEnv, len(trailing))
	for i, p := range trailing {
		if i < len(g.ambient) {
			env[p] = exprBinding{expr: g.ambient[i].v, typ: g.ambient[i].typ}
		}
	}
	return env
}
