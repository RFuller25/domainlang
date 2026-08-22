package typecheck

import (
	"testing"

	"domain/ir"
)

// The graph builtins' typing rules. `graph` is the interesting one: it accepts
// the two edge-list shapes a parse actually lands on, the same pair Topological
// Sort already takes.

func graphT(node *ir.Type) *ir.Type { return ir.Graph(node) }

func TestGraphBuiltinTypes(t *testing.T) {
	gt := graphT(ir.Text())
	gi := graphT(ir.Int())
	pairs := ir.List(ir.Tuple(ir.Text(), ir.Text()))
	triples := ir.List(ir.Tuple(ir.Text(), ir.Text(), ir.Int()))
	ragged := ir.List(ir.List(ir.Text()))

	// Construction, from each shape.
	wantType(t, "(es) -> graph(es)", gt, pairs)
	wantType(t, "(es) -> graph(es)", gt, triples)
	wantType(t, "(es) -> graph(es)", gt, ragged)
	wantType(t, `(g) -> emptygraph("")`, gt, gt)
	wantType(t, "(g) -> emptygraph(0)", gi, gt)

	// Functional updates return the same graph type.
	wantType(t, `(g) -> addnode(g, "a")`, gt, gt)
	wantType(t, `(g) -> addedge(g, "a", "b")`, gt, gt)
	wantType(t, `(g) -> addedge(g, "a", "b", 5)`, gt, gt)
	wantType(t, `(g) -> deledge(g, "a", "b")`, gt, gt)
	wantType(t, "(g) -> flipedges(g)", gt, gt)
	wantType(t, `(g) -> subgraph(g, list("a"))`, gt, gt)

	// Readers.
	wantType(t, "(g) -> nodes(g)", ir.List(ir.Text()), gt)
	wantType(t, "(g) -> edges(g)", ir.List(ir.Tuple(ir.Text(), ir.Text(), ir.Int())), gt)
	wantType(t, `(g) -> neighbors(g, "a")`, ir.List(ir.Text()), gt)
	wantType(t, `(g) -> edgesof(g, "a")`, ir.List(ir.Tuple(ir.Text(), ir.Int())), gt)
	wantType(t, `(g) -> hasedge(g, "a", "b")`, ir.Bool(), gt)
	wantType(t, `(g) -> weight(g, "a", "b")`, ir.Int(), gt)
	wantType(t, `(g) -> weightor(g, "a", "b", 0)`, ir.Int(), gt)
	wantType(t, `(g) -> degree(g, "a")`, ir.Int(), gt)
	wantType(t, `(g) -> weightof(g, "a")`, ir.Int(), gt)
	// root answers a node, so its type is the graph's node type — the one
	// reader whose result is not a list, a count or a Bool.
	wantType(t, "(g) -> root(g)", ir.Text(), gt)
	wantType(t, "(g) -> root(g)", ir.Int(), gi)
	wantType(t, "(g) -> roots(g)", ir.List(ir.Text()), gt)
	wantType(t, "(g) -> leaves(g)", ir.List(ir.Text()), gt)
	wantType(t, `(g) -> indegree(g, "a")`, ir.Int(), gt)
	wantType(t, `(g) -> reachable(g, "a")`, ir.List(ir.Text()), gt)
	wantType(t, "(g) -> hascycle(g)", ir.Bool(), gt)
	wantType(t, "(g) -> weightsum(g)", ir.Int(), gt)
	// The whole-graph updates return the same graph type, like the arc ones.
	wantType(t, `(g) -> delnode(g, "a")`, gt, gt)
	wantType(t, "(g) -> undirected(g)", gt, gt)
	wantType(t, "(g) -> mergegraphs(g, g)", gt, gt)

	// size and contains are extended rather than duplicated under new names.
	wantType(t, "(g) -> size(g)", ir.Int(), gt)
	wantType(t, `(g) -> contains(g, "a")`, ir.Bool(), gt)

	// A tuple-node graph, which is what a coordinate graph looks like.
	pt := ir.Tuple(ir.Int(), ir.Int())
	gp := graphT(pt)
	wantType(t, "(g) -> nodes(g)", ir.List(pt), gp)
	wantType(t, "(g) -> root(g)", pt, gp)
	wantType(t, "(g) -> addedge(g, point(0, 0), point(1, 1))", gp, gp)
}

func TestGraphBuiltinTypeErrors(t *testing.T) {
	gt := graphT(ir.Text())
	gi := graphT(ir.Int())

	for _, c := range []struct {
		name, src string
		params    []*ir.Type
		sub       string
	}{
		{"not a graph", "(xs) -> nodes(xs)", []*ir.Type{ir.List(ir.Int())}, "nodes needs a Graph argument"},
		{"wrong node type", `(g) -> addnode(g, "a")`, []*ir.Type{gi}, "addnode value must be Int"},
		{"wrong endpoint", `(g) -> addedge(g, "a", 1)`, []*ir.Type{gt}, "addedge endpoint 2 must be Text"},
		{"non-Int weight", `(g) -> addedge(g, "a", "b", "c")`, []*ir.Type{gt}, "must be Int"},
		{"weightor default", `(g) -> weightor(g, "a", "b", "z")`, []*ir.Type{gt}, "must be Int"},
		{"subgraph wants a list", `(g) -> subgraph(g, "a")`, []*ir.Type{gt}, "subgraph needs List<Text>"},
		{"subgraph element type", "(g) -> subgraph(g, list(1))", []*ir.Type{gt}, "subgraph needs List<Text>"},
		{"neighbors node type", "(g) -> neighbors(g, 1)", []*ir.Type{gt}, "neighbors node must be Text"},
		{"weightof node type", "(g) -> weightof(g, 1)", []*ir.Type{gt}, "weightof node must be Text"},
		{"root of a list", "(xs) -> root(xs)", []*ir.Type{ir.List(ir.Int())}, "root needs a Graph argument"},
		{"roots of a list", "(xs) -> roots(xs)", []*ir.Type{ir.List(ir.Int())}, "roots needs a Graph argument"},
		{"indegree node type", "(g) -> indegree(g, 1)", []*ir.Type{gt}, "indegree node must be Text"},
		{"reachable node type", "(g) -> reachable(g, 1)", []*ir.Type{gt}, "reachable node must be Text"},
		{"delnode value type", "(g) -> delnode(g, 1)", []*ir.Type{gt}, "delnode value must be Text"},
		{"mergegraphs needs a second graph", `(g) -> mergegraphs(g, "a")`, []*ir.Type{gt},
			"mergegraphs needs two graphs of the same node type"},
		{"mergegraphs node types differ", "(g, h) -> mergegraphs(g, h)", []*ir.Type{gt, gi},
			"mergegraphs needs two graphs of the same node type"},
		// Construction shapes.
		{"graph of scalars", "(xs) -> graph(xs)", []*ir.Type{ir.List(ir.Int())}, "graph needs an edge list"},
		{"graph of 1-tuples", "(xs) -> graph(xs)",
			[]*ir.Type{ir.List(ir.Tuple(ir.Text()))}, "graph needs an edge list"},
		{"graph endpoints differ", "(xs) -> graph(xs)",
			[]*ir.Type{ir.List(ir.Tuple(ir.Text(), ir.Int()))}, "endpoints must have the same type"},
		{"graph weight not Int", "(xs) -> graph(xs)",
			[]*ir.Type{ir.List(ir.Tuple(ir.Text(), ir.Text(), ir.Text()))}, "weight must be Int"},
		{"graph node unkeyable", "(xs) -> graph(xs)",
			[]*ir.Type{ir.List(ir.Tuple(ir.Float(), ir.Float()))}, "keyable"},
		{"emptygraph unkeyable", "(f) -> emptygraph(f)", []*ir.Type{ir.Float()}, "keyable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			wantTypeErr(t, c.src, c.sub, c.params...)
		})
	}
}

// A Graph is neither keyable nor ordered, so it cannot reach a Set, a Map key
// or a Sort. The predicates are default-deny; this pins the consequence at the
// place a user would actually hit it.
func TestGraphCannotBeSortedOrKeyed(t *testing.T) {
	gt := graphT(ir.Text())
	wantTypeErr(t, "(gs) -> sort(gs)", "ordered", ir.List(gt))
	wantTypeErr(t, "(gs) -> toset(gs)", "keyable", ir.List(gt))
	wantTypeErr(t, "(g) -> g < g", "", gt) // any error will do; it must not typecheck
}

// addedge is the one variadic here: 3 arguments weigh the arc 1, 4 name a
// weight, and anything else is an arity error rather than a type error.
func TestAddEdgeArity(t *testing.T) {
	gt := graphT(ir.Text())
	wantTypeErr(t, `(g) -> addedge(g, "a")`, "argument", gt)
	wantTypeErr(t, `(g) -> addedge(g, "a", "b", 1, 2)`, "argument", gt)
}
