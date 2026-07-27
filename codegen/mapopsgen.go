package codegen

import (
	"domain/ir"
)

// Map pipeline operations. dmMap is insertion-ordered (a keys slice beside the
// map), so every one of these walks .keys and preserves that order — which is
// what keeps a compiled Map's rendering byte-identical to the interpreter's.

func (g *gen) emitConvertToEntries(n *ir.Node, in string) (string, error) {
	pairGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmMap", declMap)
	v, k := g.fresh("v"), g.fresh("k")
	g.wl("%s := make([]%s, 0, len(%s.keys))", v, pairGo, in)
	g.wl("for _, %s := range %s.keys {", k, in)
	g.in()
	g.wl("%s = append(%s, %s{%s, %s.vals[%s]})", v, v, pairGo, k, in, k)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitConvertToMap(n *ir.Node, in string) (string, error) {
	keyGo, err := g.goType(n.Out.Key)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	valGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmMap", declMap)
	m, e := g.fresh("m"), g.fresh("e")
	g.wl("%s := dmNewMap[%s, %s]()", m, keyGo, valGo)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	// Last write wins, matching the interpreter.
	g.wl("%s.put(%s.f0, %s.f1)", m, e, e)
	g.out()
	g.wl("}")
	return m, nil
}

func (g *gen) emitMapValues(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	keyGo, err := g.goType(n.Out.Key)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	valGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	m, k, e := g.fresh("m"), g.fresh("k"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmMap", declMap)
	g.wl("%s := dmNewMap[%s, %s]()", m, keyGo, valGo)
	g.wl("for _, %s := range %s.keys {", k, in)
	g.in()
	g.wl("%s := %s.vals[%s]", e, in, k)
	g.wl("%s.put(%s, %s)", m, k, body)
	g.out()
	g.wl("}")
	return m, nil
}

func (g *gen) emitFilterEntries(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	keyGo, err := g.goType(n.In.Key)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	valGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	m, k, e := g.fresh("m"), g.fresh("k"), g.fresh("e")
	pred, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: k, typ: n.In.Key},
		lam.Params[1]: {expr: e, typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "predicate: %v", err)
	}
	g.helper("dmMap", declMap)
	g.wl("%s := dmNewMap[%s, %s]()", m, keyGo, valGo)
	g.wl("for _, %s := range %s.keys {", k, in)
	g.in()
	g.wl("%s := %s.vals[%s]", e, in, k)
	g.wl("if %s {", pred)
	g.in()
	g.wl("%s.put(%s, %s)", m, k, e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return m, nil
}
