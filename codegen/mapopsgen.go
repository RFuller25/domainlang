package codegen

import (
	"domain/ir"
)

// Map pipeline operations. dmMap is insertion-ordered (a keys slice beside the
// map), so every one of these walks .keys and preserves that order — which is
// what keeps a compiled Map's rendering byte-identical to the interpreter's.

func (g *gen) emitConvertToEntries(n *ir.Node, in string) string {
	pairGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Elem) })
	g.chelper(n.In.Key, "dmMap", declMap)
	v, k, ix := g.fresh("v"), g.fresh("k"), g.fresh("i")
	g.wl("%s := make([]%s, 0, len(%s.keys))", v, pairGo, in)
	g.wl("for %s, %s := range %s.keys {", ix, k, in)
	g.in()
	g.wl("%s = append(%s, %s{%s, %s.vs[%s]})", v, v, pairGo, k, in, ix)
	g.out()
	g.wl("}")
	return v
}

func (g *gen) emitConvertToMap(n *ir.Node, in string) string {
	keyGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Key) })
	valGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Elem) })
	m, e := g.fresh("m"), g.fresh("e")
	asUnsupported0(n, "", func() { g.wnewMap(m, n.Out.Key, keyGo, valGo) })
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	// Last write wins, matching the interpreter.
	g.wl("%s.put(%s.f0, %s.f1)", m, e, e)
	g.out()
	g.wl("}")
	return m
}

func (g *gen) emitMapValues(n *ir.Node, in string) string {
	lam := g.nodeLambda(n)
	keyGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Key) })
	valGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Elem) })
	m, k, e := g.fresh("m"), g.fresh("k"), g.fresh("e")
	body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	})
	asUnsupported0(n, "", func() { g.wnewMap(m, n.Out.Key, keyGo, valGo) })
	ix := g.fresh("i")
	g.wl("for %s, %s := range %s.keys {", ix, k, in)
	g.in()
	g.wl("%s := %s.vs[%s]", e, in, ix)
	g.wl("%s.put(%s, %s)", m, k, body)
	g.out()
	g.wl("}")
	return m
}

func (g *gen) emitFilterEntries(n *ir.Node, in string) string {
	lam := g.nodeLambda(n)
	keyGo := asUnsupported1(n, "", func() string { return g.goType(n.In.Key) })
	valGo := asUnsupported1(n, "", func() string { return g.goType(n.In.Elem) })
	m, k, e := g.fresh("m"), g.fresh("k"), g.fresh("e")
	pred, _ := asUnsupported2(n, "predicate: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{
			lam.Params[0]: {expr: k, typ: n.In.Key},
			lam.Params[1]: {expr: e, typ: n.In.Elem},
		})
	})
	asUnsupported0(n, "", func() { g.wnewMap(m, n.In.Key, keyGo, valGo) })
	ix := g.fresh("i")
	g.wl("for %s, %s := range %s.keys {", ix, k, in)
	g.in()
	g.wl("%s := %s.vs[%s]", e, in, ix)
	g.wl("if %s {", pred)
	g.in()
	g.wl("%s.put(%s, %s)", m, k, e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return m
}

// mapEntries materializes a Map as a slice of entry tuples, in key order, for
// the sequence primitives that accept a Map wherever they accept a List (see
// seqConsumers). It is emitConvertToEntries' lowering without a node of its
// own: the entry type comes from the *input* Map rather than from an output
// type, because here the conversion is implicit.
func (g *gen) mapEntries(n *ir.Node, in string) string {
	pairGo := asUnsupported1(n, "", func() string { return g.goType(ir.Tuple(n.In.Key, n.In.Elem)) })
	g.chelper(n.In.Key, "dmMap", declMap)
	v, k, ix := g.fresh("v"), g.fresh("k"), g.fresh("i")
	g.wl("%s := make([]%s, 0, len(%s.keys))", v, pairGo, in)
	g.wl("for %s, %s := range %s.keys {", ix, k, in)
	g.in()
	g.wl("%s = append(%s, %s{%s, %s.vs[%s]})", v, v, pairGo, k, in, ix)
	g.out()
	g.wl("}")
	return v
}
