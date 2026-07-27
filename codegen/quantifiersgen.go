package codegen

import (
	"fmt"

	"domain/ir"
)

// Go lowerings for the early-exit reductions (Any, All, Find, Find Index), the
// keyed arithmetic pair (Sum By, Product By), and the Zip With consumer.
//
// The compiled forms keep the property that motivates the primitives: Any/All
// and the Finds `break` on the element that decides the answer, and Sum By /
// Product By / Zip With accumulate in one loop with no intermediate slice.

func (g *gen) emitQuantifier(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	universal := n.Prim == "All"
	v, i := g.fresh("v"), g.fresh("i")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: in + "[" + i + "]", typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	// The empty list takes the identity of the connective: Any is false, All
	// is true — the same answers the interpreter gives.
	g.wl("%s := %v", v, universal)
	g.wl("for %s := 0; %s < len(%s); %s++ {", i, i, in, i)
	g.in()
	if universal {
		g.wl("if !(%s) {", body)
	} else {
		g.wl("if %s {", body)
	}
	g.in()
	g.wl("%s = %v", v, !universal)
	g.wl("break") // the deciding element is the last one examined
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitFind(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	wantIndex := n.Prim == "Find Index"
	i := g.fresh("i")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: in + "[" + i + "]", typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}

	if wantIndex {
		v := g.fresh("v")
		g.wl("%s := int64(-1)", v) // the "not there" sentinel, as in the interpreter
		g.wl("for %s := 0; %s < len(%s); %s++ {", i, i, in, i)
		g.in()
		g.wl("if %s {", body)
		g.in()
		g.wl("%s = int64(%s)", v, i)
		g.wl("break")
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
		return v, nil
	}

	elemGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	// A Find produced by the optimizer's Filter + Take Item 0 rewrite carries
	// the message the shape it replaced would have reported.
	noMatch, _ := n.Meta["nomatch"].(string)
	if noMatch == "" {
		noMatch = "no element satisfied the Find predicate"
	}
	v, found := g.fresh("v"), g.fresh("found")
	g.helper("dmFail", declFail, "fmt", "os")
	g.wl("var %s %s", v, elemGo)
	g.wl("%s := false", found)
	g.wl("for %s := 0; %s < len(%s); %s++ {", i, i, in, i)
	g.in()
	g.wl("if %s {", body)
	g.in()
	g.wl("%s, %s = %s[%s], true", v, found, in, i)
	g.wl("break") // found: the rest of the list is never touched
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	g.wl("if !%s {", found)
	g.in()
	g.wl("dmFail(%s)", goStr(noMatch))
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitKeyedArithmetic(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	i := g.fresh("i")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: in + "[" + i + "]", typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	identity, op := "0", "+="
	if n.Prim == "Product By" {
		identity, op = "1", "*="
	}
	v := g.fresh("v")
	g.wl("%s := int64(%s)", v, identity) // the empty list folds to the identity
	g.wl("for %s := 0; %s < len(%s); %s++ {", i, i, in, i)
	g.in()
	g.wl("%s %s %s", v, op, body)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitZipWith(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	froms, _ := n.Meta["from"].([]string)
	if len(froms) != 2 {
		return "", unsupported(n, "missing channel metadata")
	}
	av, ok := g.chans[froms[0]]
	if !ok {
		return "", unsupported(n, "channel %q was not compiled", froms[0])
	}
	bv, ok := g.chans[froms[1]]
	if !ok {
		return "", unsupported(n, "channel %q was not compiled", froms[1])
	}
	outGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, m, i := g.fresh("v"), g.fresh("m"), g.fresh("i")

	// Two shapes share this lowering, and neither materializes a []tuple:
	//
	//   Zip With — a two-parameter lambda, one channel element bound to each.
	//   ZipMap   — the optimizer's Zip + Map Each fusion, whose lambda still
	//              takes the tuple, so the pair is built as a loop-local.
	var env exprEnv
	pair, pairLit := "", ""
	switch len(lam.Params) {
	case 2:
		env = exprEnv{
			lam.Params[0]: {expr: fmt.Sprintf("%s[%s]", av.v, i), typ: av.typ.Elem},
			lam.Params[1]: {expr: fmt.Sprintf("%s[%s]", bv.v, i), typ: bv.typ.Elem},
		}
	case 1:
		tupT, _ := n.Meta["tuple"].(*ir.Type)
		if tupT == nil {
			return "", unsupported(n, "missing tuple metadata")
		}
		tupGo, err := g.goType(tupT)
		if err != nil {
			return "", unsupported(n, "%v", err)
		}
		pair = g.fresh("pair")
		pairLit = fmt.Sprintf("%s{%s[%s], %s[%s]}", tupGo, av.v, i, bv.v, i)
		env = exprEnv{lam.Params[0]: {expr: pair, typ: tupT}}
	default:
		return "", unsupported(n, "Zip lambda takes %d parameter(s)", len(lam.Params))
	}

	body, _, err := g.compileExpr(lam.Body, env)
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := len(%s)", m, av.v)
	g.wl("if len(%s) < %s {", bv.v, m)
	g.in()
	g.wl("%s = len(%s)", m, bv.v) // truncated to the shorter list, like Zip
	g.out()
	g.wl("}")
	g.wl("%s := make([]%s, %s)", v, outGo, m)
	g.wl("for %s := 0; %s < %s; %s++ {", i, i, m, i)
	g.in()
	if pair != "" {
		g.wl("%s := %s", pair, pairLit)
		g.wl("_ = %s", pair) // a lambda may ignore the pair it was handed
	}
	g.wl("%s[%s] = %s", v, i, body)
	g.out()
	g.wl("}")
	return v, nil
}
