package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
)

// Graphs whose nodes are Lists: the compiled dmLGraph / dmLTopo against the
// interpreter's GraphValue. Each program runs four ways (optimized/naive
// interpreter, optimized/naive binary) and must print the same bytes.
func graphParity(t *testing.T, head string, cases []struct{ name, body string }, inputs []string) {
	t.Helper()
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	for _, tc := range cases {
		for _, input := range inputs {
			name := tc.name + "/" + strings.ReplaceAll(input, " ", "_")
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				src := head + tc.body + "Reveal: stdout\n"
				pipe, err := frontEnd(src, true)
				if err != nil {
					t.Fatalf("front end: %v", err)
				}
				want := runInterpreter(t, pipe, []byte(input))
				naive := runInterpreter(t, compilePipeline(t, src, false), []byte(input))
				if want != naive {
					t.Fatalf("optimized interpreter %q, naive %q", want, naive)
				}
				if got := buildAndRun(t, pipe, []byte(input), codegen.Options{}); got != want {
					t.Errorf("interpreter %q, binary %q", want, got)
				}
				if got := buildAndRun(t, compilePipeline(t, src, false), []byte(input), codegen.Options{}); got != want {
					t.Errorf("interpreter %q, unoptimized binary %q", want, got)
				}
			})
		}
	}
}

// Nodes are the pair (x / 100, x % 100), so 123 is [1, 23] and 1203 is [12, 3]:
// two nodes that print alike once their parts are run together.
const lgNode = "list(%s / 100, %s % 100)"

func lgn(x string) string { return strings.ReplaceAll(lgNode, "%s", x) }

var lgEdges = "Cursed Energy: stdin\nCursed Technique: Extract Integers\nCursed Technique: Chunk 2\n" +
	"Cursed Technique: Map Each\n    Using: (p) -> tuple(" + lgn("item(p, 0)") + ", " + lgn("item(p, 1)") + ", item(p, 0) % 4 + 1)\n"

var lgInputs = []string{"", "5 6", "123 1203 1203 123 7 123", "101 202 202 303 303 101 404 202", "1 2 2 3 1 3 3 4"}
var lgNonEmpty = lgInputs[1:]
var lgDAG = []string{"", "1 2 2 3 1 3 3 4 5 3", "123 1203 1203 4000"}

func TestCompiledListGraphConversions(t *testing.T) {
	cases := []struct{ name, body string }{
		{"convert to graph", "Channeled Energy: Convert To Graph\n"},
		{"convert to graph undirected", "Channeled Energy: Convert To Graph\n    Mode: Undirected\n"},
		{"unweighted pairs", "Cursed Technique: Map Each\n    Using: (e) -> tuple(item(e, 0), item(e, 1))\nChanneled Energy: Convert To Graph\n"},
		{"convert to edges", "Channeled Energy: Convert To Graph\nChanneled Energy: Convert To Edges\n"},
		{"convert to adjacency", "Channeled Energy: Convert To Graph\nChanneled Energy: Convert To Adjacency\n"},
		{"adjacency round trip", "Channeled Energy: Convert To Graph\nChanneled Energy: Convert To Adjacency\nChanneled Energy: Convert To Graph\n"},
		{"adjacency map built by hand", "Cursed Technique: Apply\n    Using: (es) -> tomap(list(tuple(list(1, 23), list(list(12, 3))), tuple(list(12, 3), list(list(1, 23))), tuple(list(5), emptylist(list(0)))))\nChanneled Energy: Convert To Graph\n"},
		{"ragged edge list of Int nodes stays Int", "Cursed Technique: Map Each\n    Using: (e) -> list(first(item(e, 0)), first(item(e, 1)))\nChanneled Energy: Convert To Graph\n"},
	}
	graphParity(t, lgEdges, cases, lgInputs)
}

func TestCompiledListGraphPrimitives(t *testing.T) {
	const g = "Channeled Energy: Convert To Graph\n"
	cases := []struct{ name, body string }{
		{"minimum spanning tree", g + "Domain Expansion: Minimum Spanning Tree\n"},
		{"minimum spanning tree as edges", g + "Domain Expansion: Minimum Spanning Tree\nChanneled Energy: Convert To Edges\n"},
		{"strongly connected components", g + "Domain Expansion: Strongly Connected Components\n"},
		{"connected components", g + "Domain Expansion: Connected Components\n"},
		{"map nodes merges", g + "Cursed Technique: Map Nodes\n    Using: (n) -> take(n, 1)\n"},
		{"map nodes to int nodes", g + "Cursed Technique: Map Nodes\n    Using: (n) -> sum(n)\n"},
		{"map weights from the old weight", g + "Cursed Technique: Map Weights\n    Using: (f, t, w) -> w * 2 + length(f)\n"},
		{"map weights then dijkstra", g + "Cursed Technique: Map Weights\n    Using: (f, t, w) -> w + sum(t) % 3\nDomain Expansion: Dijkstra\n    Start: (g) -> first(nodes(g))\n"},
		{"bfs", g + "Domain Expansion: BFS\n    Start: (g) -> first(nodes(g))\n"},
		{"dijkstra", g + "Domain Expansion: Dijkstra\n    Start: (g) -> first(nodes(g))\n"},
		{"dijkstra undirected", g[:len(g)-1] + "\n    Mode: Undirected\nDomain Expansion: Dijkstra\n    Start: (g) -> first(nodes(g))\n"},
		{"shortest path", g[:len(g)-1] + "\n    Mode: Undirected\nDomain Expansion: Shortest Path\n    Start: (g) -> first(nodes(g))\n    Goal: (g) -> last(nodes(g))\n"},
		{"shortest path unreachable", g + "Domain Expansion: Shortest Path\n    Start: (g) -> last(nodes(g))\n    Goal: (g) -> first(nodes(g))\n"},
	}
	graphParity(t, lgEdges, cases, lgNonEmpty)
	// The empty graph, for the primitives that do not need a start node.
	graphParity(t, lgEdges, cases[:6], []string{""})
}

func TestCompiledListGraphDagPrimitives(t *testing.T) {
	const g = "Channeled Energy: Convert To Graph\n"
	cases := []struct{ name, body string }{
		{"topological sort over a graph", g + "Domain Expansion: Topological Sort\n"},
		{"topological sort over an edge list", "Cursed Technique: Map Each\n    Using: (e) -> tuple(item(e, 0), item(e, 1))\nDomain Expansion: Topological Sort\n"},
		{"topological sort over an adjacency map", g + "Channeled Energy: Convert To Adjacency\nDomain Expansion: Topological Sort\n"},
		{"accumulate up", g + "Domain Expansion: Accumulate Up\n    Using: (n) -> sum(n) + 1\n"},
		{"accumulate up with combine", g + "Domain Expansion: Accumulate Up\n    Using: (n) -> 10\n    Combine: (a, b) -> a - b\n"},
		{"hascycle weightsum", g + "Cursed Technique: Apply\n    Using: (g) -> tuple(hascycle(g), weightsum(g))\n"},
	}
	graphParity(t, lgEdges, cases, lgDAG)
}

func TestCompiledListGraphRoot(t *testing.T) {
	const g = "Channeled Energy: Convert To Graph\n"
	cases := []struct{ name, body string }{
		{"root stage", g + "Domain Expansion: Root\n"},
		{"root builtin", g + "Cursed Technique: Apply\n    Using: (g) -> tuple(root(g), roots(g), leaves(g))\n"},
	}
	graphParity(t, lgEdges, cases, []string{"1 2 1 3 2 4", "5 6", "123 1203 1203 4000"})
}

// The builtins, each over a graph built from the input. n0 and n1 are its first
// and last node (a pair of zeros when the graph is empty), so an empty input
// exercises the absent-node paths.
func TestCompiledListGraphBuiltins(t *testing.T) {
	const pre = "Channeled Energy: Convert To Graph\nCursed Technique: Apply\n    Using: (g) ->\n" +
		"        consider n0 as (if size(g) > 0 then first(nodes(g)) else list(0, 0))\n" +
		"        in consider n1 as (if size(g) > 0 then last(nodes(g)) else list(0, 0))\n" +
		"        in consider zz as list(99, 99)\n        in "
	cases := []struct{ name, body string }{
		{"size nodes edges", "tuple(size(g), nodes(g), edges(g))\n"},
		{"neighbors and edgesof", "tuple(neighbors(g, n0), edgesof(g, n0), neighbors(g, zz), edgesof(g, zz))\n"},
		{"hasedge weightor", "tuple(hasedge(g, n0, n1), hasedge(g, n1, n0), weightor(g, n0, n1, 0 - 1), weightor(g, n1, n0, 0 - 1))\n"},
		{"weight of an existing arc", "(if length(edges(g)) > 0 then weight(g, item(first(edges(g)), 0), item(first(edges(g)), 1)) else 0)\n"},
		{"degrees", "tuple(degree(g, n0), degree(g, zz), indegree(g, n1), indegree(g, zz), weightof(g, n0), weightof(g, zz))\n"},
		{"roots leaves", "tuple(roots(g), leaves(g))\n"},
		{"reachable", "tuple(reachable(g, n0), reachable(g, zz))\n"},
		{"contains", "tuple(contains(g, n0), contains(g, zz), contains(g, list(1, 23)), contains(g, list(12, 3)))\n"},
		{"delnode", "tuple(delnode(g, n0), delnode(g, zz), delnode(g, n1))\n"},
		{"hascycle weightsum", "tuple(hascycle(g), weightsum(g))\n"},
		{"undirected merge flip", "tuple(undirected(g), mergegraphs(g, flipedges(g)), flipedges(g))\n"},
		{"subgraph", "tuple(subgraph(g, take(nodes(g), 2)), subgraph(g, list(zz, n0)))\n"},
		{"updates are functional", "tuple(addnode(g, list(1, 23)), addedge(g, list(1, 23), list(12, 3)), addedge(g, n0, list(12, 3), 7), deledge(g, n0, n1), g)\n"},
		{"print-alike nodes stay apart", "consider h as addedge(addedge(g, list(1, 23), list(12, 3)), list(12, 3), list(123))\n        in tuple(size(h), neighbors(h, list(1, 23)), neighbors(h, list(12, 3)), hasedge(h, list(12, 3), list(1, 23)), nodes(h))\n"},
		{"equality ignores insertion order", "tuple(g = graph(reverse(edges(g))), g = addnode(g, zz), g = g)\n"},
		{"graph from an edge list", "tuple(graph(edges(g)), graph(reverse(edges(g))))\n"},
		{"emptygraph witness", "addedge(addedge(emptygraph(list(0)), list(1, 23), list(12, 3)), list(12, 3), list(1, 23), 4)\n"},
	}
	graphParity(t, lgEdges, cases2(pre, cases), lgInputs)
}

func cases2(pre string, cs []struct{ name, body string }) []struct{ name, body string } {
	out := make([]struct{ name, body string }, len(cs))
	for i, c := range cs {
		out[i].name, out[i].body = c.name, pre+c.body
	}
	return out
}

// A graph threaded through a fold and through a fixed-point loop.
func TestCompiledListGraphLoops(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\nCursed Technique: Chunk 2\n"
	cases := []struct{ name, body string }{
		{"graph accumulated in a fold", "Maximum Technique: Fold\n    Seed: (xs) -> emptygraph(list(0))\n    Using: (acc, p) -> addedge(acc, " + lgn("item(p, 0)") + ", " + lgn("item(p, 1)") + ", 1)\nCursed Technique: Apply\n    Using: (g) -> tuple(size(g), nodes(g), edges(g))\n"},
		{"graph converging in a fixed point loop", "Cursed Technique: Map Each\n    Using: (p) -> tuple(" + lgn("item(p, 0)") + ", " + lgn("item(p, 1)") + ")\nChanneled Energy: Convert To Graph\nSimple Domain: Iterate Until Fixed Point\n    Cursed Technique: Apply\n        Using: (g) -> mergegraphs(g, undirected(g))\n"},
	}
	graphParity(t, head, cases, lgInputs)
}
