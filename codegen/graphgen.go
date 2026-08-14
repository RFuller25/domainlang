package codegen

import (
	"fmt"

	"domain/ast"
	"domain/ir"
)

// The graph builtins whose emitted code depends on the node type: building one
// from an edge list, and the two readers that hand back tuples. Each is
// interned per type through listFn, the way sortFn and pairFn are, because a
// tuple struct is involved and a single generic helper cannot name it.
//
// Everything else in the group is node-type-agnostic and lives as a plain
// generic helper in runtime.go (dmGraphNeighbors, dmGraphAddEdge, …).

// graphOut computes the result type of `graph(edges)`, mirroring the
// typechecker's graphEdgeListNode: pairs and triples come from a tuple element,
// the ragged form from a List<List<K>>.
func (g *gen) graphOut(x *ast.CallExpr, types []*ir.Type) (*ir.Type, error) {
	t := types[0]
	if t == nil || t.Kind != ir.KList || t.Elem == nil {
		return nil, fmt.Errorf("graph needs an edge list, got %s", t)
	}
	switch e := t.Elem; e.Kind {
	case ir.KTuple:
		if len(e.Elems) != 2 && len(e.Elems) != 3 {
			return nil, fmt.Errorf("graph needs 2- or 3-element edges, got %s", e)
		}
		return ir.Graph(e.Elems[0]), nil
	case ir.KList:
		return ir.Graph(e.Elem), nil
	}
	return nil, fmt.Errorf("graph needs an edge list, got %s", t)
}

// graphBuildFn interns `graph(edges)` for one edge-list shape.
//
// The ragged List<List<K>> form checks each row's length at run time and fails
// the way the interpreter does, because the length is not in the type — that is
// the whole reason the shape exists (a positional Match Pattern produces it).
func (g *gen) graphBuildFn(gt, edgeT *ir.Type) (string, error) {
	graphGo, err := g.goType(gt)
	if err != nil {
		return "", err
	}
	edgeGo, err := g.goType(edgeT)
	if err != nil {
		return "", err
	}
	nodeGo, err := g.goType(gt.Elem)
	if err != nil {
		return "", err
	}
	g.helper("dmGraph", declGraph)

	key := "graphbuild:" + canonicalKey(edgeT)
	return g.listFn(key, "GraphOf", func(name string) (string, error) {
		if edgeT.Elem.Kind == ir.KList {
			g.helper("dmFail", declFail, "fmt", "os")
			return fmt.Sprintf(`func %s(es %s) %s {
	out := dmNewGraph[%s]()
	for i, e := range es {
		if len(e) != 2 && len(e) != 3 {
			dmFail("graph: edge %%d has %%d parts, want 2 (from, to) or 3 (from, to, weight)", i, len(e))
		}
		out.addEdge(e[0], e[1], 1)
	}
	return out
}`, name, edgeGo, graphGo, nodeGo), nil
		}
		// A tuple edge: its arity is static, so the weight is decided here.
		w := "1"
		if len(edgeT.Elem.Elems) == 3 {
			w = "e." + tupleField(2)
		}
		return fmt.Sprintf(`func %s(es %s) %s {
	out := dmNewGraph[%s]()
	for _, e := range es {
		out.addEdge(e.%s, e.%s, %s)
	}
	return out
}`, name, edgeGo, graphGo, nodeGo, tupleField(0), tupleField(1), w), nil
	})
}

// graphEdgesFn interns `edges(g)` for one node type: every arc as a
// (from, to, weight) triple, nodes in insertion order and each node's arcs in
// theirs — the same walk ir.GraphValue.Edges does.
func (g *gen) graphEdgesFn(gt *ir.Type) (string, error) {
	graphGo, err := g.goType(gt)
	if err != nil {
		return "", err
	}
	tripleT := ir.Tuple(gt.Elem, gt.Elem, ir.Int())
	tripleGo, err := g.goType(tripleT)
	if err != nil {
		return "", err
	}
	g.helper("dmGraph", declGraph)

	return g.listFn("graphedges:"+canonicalKey(gt), "GraphEdges", func(name string) (string, error) {
		return fmt.Sprintf(`func %s(g %s) []%s {
	out := make([]%s, 0, len(g.edges))
	for i, arcs := range g.adj {
		for _, e := range arcs {
			out = append(out, %s{%s: g.nodes[i], %s: g.nodes[e.to], %s: e.w})
		}
	}
	return out
}`, name, graphGo, tripleGo, tripleGo, tripleGo,
			tupleField(0), tupleField(1), tupleField(2)), nil
	})
}

// graphEdgesOfFn interns `edgesof(g, n)` for one node type: a node's out-arcs
// as (destination, weight) pairs. A node not in the graph reads empty, which is
// what keeps the builtin total.
func (g *gen) graphEdgesOfFn(gt *ir.Type) (string, error) {
	graphGo, err := g.goType(gt)
	if err != nil {
		return "", err
	}
	nodeGo, err := g.goType(gt.Elem)
	if err != nil {
		return "", err
	}
	pairT := ir.Tuple(gt.Elem, ir.Int())
	pairGo, err := g.goType(pairT)
	if err != nil {
		return "", err
	}
	g.helper("dmGraph", declGraph)

	return g.listFn("graphedgesof:"+canonicalKey(gt), "GraphEdgesOf", func(name string) (string, error) {
		return fmt.Sprintf(`func %s(g %s, n %s) []%s {
	i, ok := g.index[n]
	if !ok {
		return []%s{}
	}
	out := make([]%s, len(g.adj[i]))
	for k, e := range g.adj[i] {
		out[k] = %s{%s: g.nodes[e.to], %s: e.w}
	}
	return out
}`, name, graphGo, nodeGo, pairGo, pairGo, pairGo, pairGo,
			tupleField(0), tupleField(1)), nil
	})
}

// emitConvertToGraph lowers the Channeled Energy coercion. The three input
// shapes are told apart at resolve time and recorded in Meta, so the emitted
// loop is the one the input actually has rather than a runtime switch.
func (g *gen) emitConvertToGraph(n *ir.Node, in string) (string, error) {
	nodeGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	undirected, _ := n.Meta["undirected"].(bool)
	weighted, _ := n.Meta["weighted"].(bool)
	fromMap, _ := n.Meta["fromMap"].(bool)

	g.helper("dmGraph", declGraph)
	out, e := g.fresh("gr"), g.fresh("e")
	g.wl("%s := dmNewGraph[%s]()", out, nodeGo)

	// add emits one arc, plus its reverse when the mode asked for it. Both go
	// through addEdge, which brings the endpoints in — so an isolated node can
	// only come from the adjacency-map form, which adds its keys explicitly.
	add := func(a, b, w string) {
		g.wl("%s.addEdge(%s, %s, %s)", out, a, b, w)
		if undirected {
			g.wl("%s.addEdge(%s, %s, %s)", out, b, a, w)
		}
	}

	switch {
	case fromMap:
		g.helper("dmMap", declMap)
		k, s := g.fresh("k"), g.fresh("s")
		g.wl("for _, %s := range %s.keys {", k, in)
		g.in()
		g.wl("%s.addNode(%s)", out, k)
		g.wl("for _, %s := range %s.vals[%s] {", s, in, k)
		g.in()
		add(k, s, "1")
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
	case n.In.Elem.Kind == ir.KList:
		// The ragged form: its row length is not in the type, so it is checked
		// at run time exactly as the interpreter checks it.
		g.helper("dmFail", declFail, "fmt", "os")
		i := g.fresh("i")
		g.wl("for %s, %s := range %s {", i, e, in)
		g.in()
		g.wl("if len(%s) != 2 && len(%s) != 3 {", e, e)
		g.in()
		g.wl(`dmFail("Convert To Graph: edge %%d has %%d parts, want 2 (from, to) or 3 (from, to, weight)", %s, len(%s))`, i, e)
		g.out()
		g.wl("}")
		add(e+"[0]", e+"[1]", "1")
		g.out()
		g.wl("}")
	default:
		w := "1"
		if weighted {
			w = e + "." + tupleField(2)
		}
		g.wl("for _, %s := range %s {", e, in)
		g.in()
		add(e+"."+tupleField(0), e+"."+tupleField(1), w)
		g.out()
		g.wl("}")
	}
	return out, nil
}

// emitConvertToEdges is the way back out: every arc as a (from, to, weight)
// triple, in the same walk ir.GraphValue.Edges does.
func (g *gen) emitConvertToEdges(n *ir.Node, in string) (string, error) {
	tripleGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmGraph", declGraph)
	out, i, arcs, e := g.fresh("es"), g.fresh("i"), g.fresh("arcs"), g.fresh("e")
	g.wl("%s := make([]%s, 0, len(%s.edges))", out, tripleGo, in)
	g.wl("for %s, %s := range %s.adj {", i, arcs, in)
	g.in()
	g.wl("for _, %s := range %s {", e, arcs)
	g.in()
	g.wl("%s = append(%s, %s{%s: %s.nodes[%s], %s: %s.nodes[%s.to], %s: %s.w})",
		out, out, tripleGo, tupleField(0), in, i, tupleField(1), in, e, tupleField(2), e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return out, nil
}

// The graph half of the search vocabulary. Each mirrors the interpreter's
// function of the same job in prims/graph.go — same traversal order, same
// insertion order into the result map, same refusal of a negative weight — so
// the two backends print the same bytes.

// graphSearchStart emits the start-node lookup shared by BFS, Dijkstra and
// Shortest Path: the resolved node, then its index, failing the way the
// interpreter's graphStart does when it is not in the graph.
func (g *gen) graphSearchStart(n *ir.Node, in, prim, role, key string) (string, error) {
	val, err := g.metaValue(n, in, key)
	if err != nil {
		return "", err
	}
	g.helper("dmFail", declFail, "fmt", "os")
	idx, ok := g.fresh("st"), g.fresh("ok")
	g.wl("%s, %s := %s.index[%s]", idx, ok, in, val)
	g.wl("if !%s {", ok)
	g.in()
	g.wl(`dmFail("%s: %s: %%v is not a node of the graph", %s)`, prim, role, val)
	g.out()
	g.wl("}")
	return idx, nil
}

// metaValue renders a Start:/Goal:-style node argument, which is either a
// literal recorded under key or a lambda under key+"Expr". It is
// measuredOperand's value-typed sibling: that one is Int-only, because every
// argument it serves is a count or an index.
func (g *gen) metaValue(n *ir.Node, in, key string) (string, error) {
	if lam, ok := n.Meta[key+"Expr"].(*ast.Lambda); ok {
		g.bindAmbientParams(lam)
		body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: in, typ: n.In}})
		if err != nil {
			return "", unsupported(n, "%s: %v", key, err)
		}
		v := g.fresh("nd")
		g.wl("%s := %s", v, body)
		return v, nil
	}
	switch lit := n.Meta[key].(type) {
	case string:
		return goStr(lit), nil
	case int64:
		return fmt.Sprintf("int64(%d)", lit), nil
	}
	return "", unsupported(n, "missing %s argument", key)
}

func (g *gen) emitGraphBFS(n *ir.Node, in string) (string, error) {
	nodeGo, err := g.goType(n.Out.Key)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	start, err := g.graphSearchStart(n, in, "BFS", "Start", "start")
	if err != nil {
		return "", err
	}
	g.helper("dmGraph", declGraph)
	g.helper("dmMap", declMap)
	g.helper("dmGraphBFS", declGraphBFS)
	v := g.fresh("d")
	g.wl("%s := dmGraphBFS[%s](%s, %s)", v, nodeGo, in, start)
	return v, nil
}

func (g *gen) emitGraphDijkstra(n *ir.Node, in string) (string, error) {
	nodeGo, err := g.goType(n.Out.Key)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	start, err := g.graphSearchStart(n, in, "Dijkstra", "Start", "start")
	if err != nil {
		return "", err
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmGraph", declGraph)
	g.helper("dmMap", declMap)
	g.helper("dmPQ", declPQ)
	g.helper("dmGraphNoNeg", declGraphNoNeg)
	g.helper("dmGraphDijkstra", declGraphDijkstra)
	v := g.fresh("d")
	g.wl("%s := dmGraphDijkstra[%s](%s, %s)", v, nodeGo, in, start)
	return v, nil
}

func (g *gen) emitGraphComponents(n *ir.Node, in string) (string, error) {
	nodeGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmGraph", declGraph)
	g.helper("dmGraphComponents", declGraphComponents)
	v := g.fresh("c")
	g.wl("%s := dmGraphComponents[%s](%s)", v, nodeGo, in)
	return v, nil
}

func (g *gen) emitShortestPath(n *ir.Node, in string) (string, error) {
	nodeGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	start, err := g.graphSearchStart(n, in, "Shortest Path", "Start", "start")
	if err != nil {
		return "", err
	}
	goal, err := g.graphSearchStart(n, in, "Shortest Path", "Goal", "goal")
	if err != nil {
		return "", err
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmGraph", declGraph)
	g.helper("dmPQ", declPQ)
	g.helper("dmGraphNoNeg", declGraphNoNeg)
	g.helper("dmGraphPath", declGraphPath)
	v := g.fresh("p")
	g.wl("%s := dmGraphPath[%s](%s, %s, %s)", v, nodeGo, in, start, goal)
	return v, nil
}
