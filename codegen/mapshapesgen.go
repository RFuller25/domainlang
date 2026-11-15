package codegen

import (
	"domain/ir"
)

// The shape-preserving maps. Each mirrors the interpreter's primitive of the
// same name in prims/mapshapes.go — the same iteration order, and the same
// merging when the mapping is not injective, which is where the two backends
// would otherwise disagree about how many things came out.

func (g *gen) emitMapElements(n *ir.Node, in string) string {
	elemT, _ := n.Meta["elem"].(*ir.Type)
	if elemT == nil {
		elemT = n.In.Elem
	}
	outGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Elem) })
	lam := g.nodeLambda(n)
	out, e := g.fresh("st"), g.fresh("e")
	body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: elemT}})
	})
	asUnsupported0(n, "", func() { g.wnewSet(out, n.Out.Elem, outGo) })
	g.wl("for _, %s := range %s.elems {", e, in)
	g.in()
	// add() drops a repeat, so two elements mapping to one value are one.
	g.wl("%s.add(%s)", out, body)
	g.out()
	g.wl("}")
	return out
}

func (g *gen) emitMapKeys(n *ir.Node, in string) string {
	keyT, _ := n.Meta["key"].(*ir.Type)
	if keyT == nil {
		keyT = n.In.Key
	}
	keyGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Key) })
	valGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Elem) })
	lam := g.nodeLambda(n)
	out, k := g.fresh("m"), g.fresh("k")
	body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: k, typ: keyT}})
	})
	asUnsupported0(n, "", func() { g.wnewMap(out, n.Out.Key, keyGo, valGo) })
	ix := g.fresh("i")
	g.wl("for %s, %s := range %s.keys {", ix, k, in)
	g.in()
	// put() overwrites, so two keys mapping to one keep the later value.
	g.wl("%s.put(%s, %s.vs[%s])", out, body, in, ix)
	g.out()
	g.wl("}")
	return out
}

func (g *gen) emitMapNodes(n *ir.Node, in string) string {
	nodeT, _ := n.Meta["node"].(*ir.Type)
	if nodeT == nil {
		nodeT = n.In.Elem
	}
	outGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Elem) })
	lam := g.nodeLambda(n)
	out, renamed, i, e, nd := g.fresh("gr"), g.fresh("rn"), g.fresh("i"), g.fresh("e"), g.fresh("nd")
	body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: nd, typ: nodeT}})
	})
	g.chelper(n.In.Elem, "dmGraph", declGraph)
	newG := asUnsupported1(n, "", func() string { return g.newGraphExpr(n.Out.Elem, outGo) })
	// Every node is renamed first, so an isolated one survives and an arc's
	// endpoints agree with the node list.
	g.wl("%s := make([]%s, len(%s.nodes))", renamed, outGo, in)
	g.wl("%s := %s", out, newG)
	g.wl("for %s := range %s.nodes {", i, in)
	g.in()
	g.wl("%s := %s.nodes[%s]", nd, in, i)
	g.wl("%s[%s] = %s", renamed, i, body)
	g.wl("%s.addNode(%s[%s])", out, renamed, i)
	g.out()
	g.wl("}")
	g.wl("for %s := range %s.nodes {", i, in)
	g.in()
	g.wl("for _, %s := range %s.adj[%s] {", e, in, i)
	g.in()
	g.wl("%s.addEdge(%s[%s], %s[%s.to], %s.w)", out, renamed, i, renamed, e, e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return out
}

func (g *gen) emitMapWeights(n *ir.Node, in string) string {
	nodeT, _ := n.Meta["node"].(*ir.Type)
	if nodeT == nil {
		nodeT = n.In.Elem
	}
	nodeGo := asUnsupported1(n, "", func() string { return g.goType(nodeT) })
	lam := g.nodeLambda(n)
	out, i, e := g.fresh("gr"), g.fresh("i"), g.fresh("e")
	from, to, w := g.fresh("f"), g.fresh("t"), g.fresh("w")
	body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{
			lam.Params[0]: {expr: from, typ: nodeT},
			lam.Params[1]: {expr: to, typ: nodeT},
			lam.Params[2]: {expr: w, typ: ir.Int()},
		})
	})
	g.chelper(n.In.Elem, "dmGraph", declGraph)
	newG := asUnsupported1(n, "", func() string { return g.newGraphExpr(n.Out.Elem, nodeGo) })
	g.wl("%s := %s", out, newG)
	g.wl("for %s := range %s.nodes {", i, in)
	g.in()
	g.wl("%s.addNode(%s.nodes[%s])", out, in, i)
	g.out()
	g.wl("}")
	g.wl("for %s := range %s.nodes {", i, in)
	g.in()
	g.wl("for _, %s := range %s.adj[%s] {", e, in, i)
	g.in()
	g.wl("%s := %s.nodes[%s]", from, in, i)
	g.wl("%s := %s.nodes[%s.to]", to, in, e)
	g.wl("%s := %s.w", w, e)
	g.wl("_, _, _ = %s, %s, %s", from, to, w)
	g.wl("%s.addEdge(%s, %s, %s)", out, from, to, body)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return out
}
