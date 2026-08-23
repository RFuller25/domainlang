package codegen

import (
	"domain/ir"
)

// The shape-preserving maps. Each mirrors the interpreter's primitive of the
// same name in prims/mapshapes.go — the same iteration order, and the same
// merging when the mapping is not injective, which is where the two backends
// would otherwise disagree about how many things came out.

func (g *gen) emitMapElements(n *ir.Node, in string) (string, error) {
	elemT, _ := n.Meta["elem"].(*ir.Type)
	if elemT == nil {
		elemT = n.In.Elem
	}
	outGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	out, e := g.fresh("st"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: elemT}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmSet", declSet)
	g.wl("%s := dmNewSet[%s]()", out, outGo)
	g.wl("for _, %s := range %s.elems {", e, in)
	g.in()
	// add() drops a repeat, so two elements mapping to one value are one.
	g.wl("%s.add(%s)", out, body)
	g.out()
	g.wl("}")
	return out, nil
}

func (g *gen) emitMapKeys(n *ir.Node, in string) (string, error) {
	keyT, _ := n.Meta["key"].(*ir.Type)
	if keyT == nil {
		keyT = n.In.Key
	}
	keyGo, err := g.goType(n.Out.Key)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	valGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	out, k := g.fresh("m"), g.fresh("k")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: k, typ: keyT}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmMap", declMap)
	g.wl("%s := dmNewMap[%s, %s]()", out, keyGo, valGo)
	g.wl("for _, %s := range %s.keys {", k, in)
	g.in()
	// put() overwrites, so two keys mapping to one keep the later value.
	g.wl("%s.put(%s, %s.vals[%s])", out, body, in, k)
	g.out()
	g.wl("}")
	return out, nil
}

func (g *gen) emitMapNodes(n *ir.Node, in string) (string, error) {
	nodeT, _ := n.Meta["node"].(*ir.Type)
	if nodeT == nil {
		nodeT = n.In.Elem
	}
	outGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	out, renamed, i, e, nd := g.fresh("gr"), g.fresh("rn"), g.fresh("i"), g.fresh("e"), g.fresh("nd")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: nd, typ: nodeT}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmGraph", declGraph)
	// Every node is renamed first, so an isolated one survives and an arc's
	// endpoints agree with the node list.
	g.wl("%s := make([]%s, len(%s.nodes))", renamed, outGo, in)
	g.wl("%s := dmNewGraph[%s]()", out, outGo)
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
	return out, nil
}

func (g *gen) emitMapWeights(n *ir.Node, in string) (string, error) {
	nodeT, _ := n.Meta["node"].(*ir.Type)
	if nodeT == nil {
		nodeT = n.In.Elem
	}
	nodeGo, err := g.goType(nodeT)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	out, i, e := g.fresh("gr"), g.fresh("i"), g.fresh("e")
	from, to, w := g.fresh("f"), g.fresh("t"), g.fresh("w")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: from, typ: nodeT},
		lam.Params[1]: {expr: to, typ: nodeT},
		lam.Params[2]: {expr: w, typ: ir.Int()},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmGraph", declGraph)
	g.wl("%s := dmNewGraph[%s]()", out, nodeGo)
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
	return out, nil
}
