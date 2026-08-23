package prims

import (
	"strings"
	"testing"
)

// Topological Sort's three input shapes all go through one adjacency map, so a
// graph and the edge list it was built from sort identically. The tie-breaking
// is part of the answer — two runs of the same program must print the same
// thing, and so must two spellings of the same graph.
func TestTopologicalSortAgreesAcrossInputShapes(t *testing.T) {
	const head = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(first(p), last(p))
`
	const tail = `Domain Expansion: Topological Sort
Maximum Technique: Join with ","
Reveal: stdout
`
	for _, input := range []string{
		"a b\nb c\na c\nd a",
		"a b\na c\nb d\nc d",
		"z y\ny x",
	} {
		viaEdges, err := runProgramWithInput(t, head+tail, input)
		if err != nil {
			t.Fatalf("edge-list form: %v", err)
		}
		viaGraph, err := runProgramWithInput(t, head+"Channeled Energy: Convert To Graph\n"+tail, input)
		if err != nil {
			t.Fatalf("graph form: %v", err)
		}
		if viaEdges != viaGraph {
			t.Errorf("input %q:\n  via edge list: %q\n  via Graph:     %q", input, viaEdges, viaGraph)
		}
	}
}

// The adjacency-map shape has to agree too, and it is the one that can carry an
// isolated node — which must still appear in the order.
func TestTopologicalSortOverAGraphKeepsIsolatedNodes(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> tomap(list(tuple("a", list("b")), tuple("z", emptylist(""))))
Channeled Energy: Convert To Graph
Domain Expansion: Topological Sort
Maximum Technique: Join with ","
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "z") {
		t.Errorf("isolated node dropped from the order: %q", got)
	}
}

// The pipeline-level graph vocabulary. Each of these is a stage a program can
// reach without an Apply, and each has a property worth pinning beyond "it ran".

const graphHead = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
`

func TestRootPrimitive(t *testing.T) {
	got, err := runProgramWithInput(t, graphHead+"Domain Expansion: Root\nReveal: stdout\n",
		"b c\na b\na d")
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	// The root is the node nothing points at, wherever it was read.
	if strings.TrimSpace(got) != "a" {
		t.Errorf("Root = %q, want a", got)
	}

	// The three failures say which one they are, so a reader knows whether to
	// look for a cycle or for a second tree.
	for _, c := range []struct{ input, want string }{
		{"a b\nc d", "2 nodes have no incoming arc"},
		{"a b\nb a", "every node has an incoming arc"},
	} {
		if _, err := runProgramWithInput(t, graphHead+"Domain Expansion: Root\nReveal: stdout\n", c.input); err == nil {
			t.Errorf("input %q: expected an error containing %q, got none", c.input, c.want)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("input %q: error %q does not contain %q", c.input, err, c.want)
		}
	}
}

// Kruskal's answer is the cheapest spanning set, and it has to be the *same*
// answer every run: the tie-breaking is part of what both backends promise.
func TestMinimumSpanningTree(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} {word} {int}"
Channeled Energy: Convert To Graph
    Mode: Undirected
Domain Expansion: Minimum Spanning Tree
Cursed Technique: Apply
    Using: (g) -> totext(weightsum(g)) + " " + totext(size(g)) + " " + totext(length(edges(g)))
Reveal: stdout
`
	// a-b 1, a-c 3, c-d 2 is the cheapest tree: 6, over 4 nodes and 3 arcs.
	got, err := runProgramWithInput(t, src, "a b 1\nb c 5\na c 3\nc d 2")
	if err != nil {
		t.Fatalf("MST: %v", err)
	}
	if strings.TrimSpace(got) != "6 4 3" {
		t.Errorf("MST = %q, want \"6 4 3\"", got)
	}

	// The arcs' reading order must not change the tree, only its rendering —
	// so the weight is the same whichever way the same graph is described.
	shuffled, err := runProgramWithInput(t, src, "c d 2\na c 3\nb c 5\na b 1")
	if err != nil {
		t.Fatalf("MST, reordered: %v", err)
	}
	if strings.TrimSpace(shuffled) != "6 4 3" {
		t.Errorf("MST over the same graph read in another order = %q, want \"6 4 3\"", shuffled)
	}

	// A graph in pieces gives a forest: 3 nodes, 2 pieces, so 1 arc — not an
	// error, and not a tree that invents a connection.
	forest, err := runProgramWithInput(t, src, "a b 4\nx y 7\nx z 1")
	if err != nil {
		t.Fatalf("MST over a disconnected graph: %v", err)
	}
	if strings.TrimSpace(forest) != "12 5 3" {
		t.Errorf("spanning forest = %q, want \"12 5 3\"", forest)
	}
}

func TestStronglyConnectedComponents(t *testing.T) {
	const src = graphHead + "Domain Expansion: Strongly Connected Components\nReveal: stdout\n"

	// Two cycles joined one way: the groups, in a topological order of the
	// groups, each group in the graph's insertion order.
	got, err := runProgramWithInput(t, src, "a b\nb c\nc a\nb d\nd e\ne d")
	if err != nil {
		t.Fatalf("SCC: %v", err)
	}
	if strings.TrimSpace(got) != "[[a, b, c], [d, e]]" {
		t.Errorf("SCC = %q, want [[a, b, c], [d, e]]", got)
	}

	// An acyclic graph is all singletons — every node appears exactly once,
	// which is the property that makes the result a partition.
	acyclic, err := runProgramWithInput(t, src, "a b\nb c\na c")
	if err != nil {
		t.Fatalf("SCC over a DAG: %v", err)
	}
	if strings.TrimSpace(acyclic) != "[[a], [b], [c]]" {
		t.Errorf("SCC over a DAG = %q, want [[a], [b], [c]]", acyclic)
	}

	// Direction is the whole point: read as undirected these are one piece,
	// which is what Connected Components would say.
	weak, err := runProgramWithInput(t, graphHead+"Domain Expansion: Connected Components\nReveal: stdout\n",
		"a b\nb c\nc a\nb d\nd e\ne d")
	if err != nil {
		t.Fatalf("Connected Components: %v", err)
	}
	if strings.TrimSpace(weak) != "1" {
		t.Errorf("Connected Components = %q, want 1 — the two answer different questions", weak)
	}
}

// Convert To Adjacency is the inverse of the adjacency-map form Convert To
// Graph accepts, so the round trip is the identity on an unweighted graph.
func TestConvertToAdjacency(t *testing.T) {
	got, err := runProgramWithInput(t, graphHead+"Channeled Energy: Convert To Adjacency\nReveal: stdout\n",
		"a b\nb c\na c")
	if err != nil {
		t.Fatalf("Convert To Adjacency: %v", err)
	}
	// Every node is a key, the one with no arcs out included.
	if strings.TrimSpace(got) != "{a: [b, c], b: [c], c: []}" {
		t.Errorf("adjacency = %q, want {a: [b, c], b: [c], c: []}", got)
	}

	direct, err := runProgramWithInput(t, graphHead+"Reveal: stdout\n", "a b\nb c\na c")
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	roundTrip, err := runProgramWithInput(t,
		graphHead+"Channeled Energy: Convert To Adjacency\nChanneled Energy: Convert To Graph\nReveal: stdout\n",
		"a b\nb c\na c")
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if direct != roundTrip {
		t.Errorf("the round trip changed the graph:\n  direct: %q\n  back:   %q", direct, roundTrip)
	}
}

// Accumulate Up is the fold the vocabulary could not express: what everything
// under a node comes to, children first.
func TestAccumulateUp(t *testing.T) {
	const src = graphHead + `Domain Expansion: Accumulate Up
    Using: (n) -> 1
Reveal: stdout
`
	// A tree: each node's subtree size, counting itself.
	got, err := runProgramWithInput(t, src, "a b\na c\nb d")
	if err != nil {
		t.Fatalf("Accumulate Up: %v", err)
	}
	if strings.TrimSpace(got) != "{a: 4, b: 2, c: 1, d: 1}" {
		t.Errorf("subtree sizes = %q, want {a: 4, b: 2, c: 1, d: 1}", got)
	}

	// Not a tree: a node under two parents is folded into both, because it is
	// genuinely under both. a counts d twice — once through b, once through c.
	dag, err := runProgramWithInput(t, src, "a b\na c\nb d\nc d")
	if err != nil {
		t.Fatalf("Accumulate Up over a DAG: %v", err)
	}
	if strings.TrimSpace(dag) != "{a: 5, b: 2, c: 2, d: 1}" {
		t.Errorf("DAG totals = %q, want {a: 5, b: 2, c: 2, d: 1}", dag)
	}

	// A cycle has no "children first" to fold in, and says which node it
	// blocked — the same thing Topological Sort says.
	if _, err := runProgramWithInput(t, src, "x y\ny x\na b"); err == nil {
		t.Error("a cycle should be a runtime error")
	} else if !strings.Contains(err.Error(), "has a cycle") {
		t.Errorf("error %q does not name the cycle", err)
	}
}

// Combine: replaces the fold, and folds a node's children in adjacency order
// so a Combine: that is not commutative still gives one answer.
func TestAccumulateUpCombine(t *testing.T) {
	const src = graphHead + `Domain Expansion: Accumulate Up
    Using: (n) -> length(n)
    Combine: (a, b) -> max(a, b)
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "a bb\na ccc\nbb dddd")
	if err != nil {
		t.Fatalf("Accumulate Up, Combine: %v", err)
	}
	if strings.TrimSpace(got) != "{a: 4, bb: 4, ccc: 3, dddd: 4}" {
		t.Errorf("widest name below each node = %q, want {a: 4, bb: 4, ccc: 3, dddd: 4}", got)
	}

	// Adjacency order is the fold order, so a non-commutative Combine: is
	// still one answer rather than whichever the queue happened to finish.
	const sub = graphHead + `Domain Expansion: Accumulate Up
    Using: (n) -> 10
    Combine: (a, b) -> a - b
Reveal: stdout
`
	first, err := runProgramWithInput(t, sub, "a b\na c")
	if err != nil {
		t.Fatalf("non-commutative Combine: %v", err)
	}
	again, err := runProgramWithInput(t, sub, "a b\na c")
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Errorf("two runs of the same program disagreed: %q then %q", first, again)
	}
	// 10 - 10 - 10 over a's two children, left to right.
	if strings.TrimSpace(first) != "{a: -10, b: 10, c: 10}" {
		t.Errorf("left-to-right fold = %q, want {a: -10, b: 10, c: 10}", first)
	}
}

// The shape-preserving maps keep the type they were given, and say what a
// non-injective mapping does to it.
func TestShapePreservingMaps(t *testing.T) {
	for _, c := range []struct{ name, src, input, want string }{
		{
			// Four elements in, three out: a Set is what merging is for.
			name: "Map Elements merges",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Set
Cursed Technique: Map Elements
    Using: (t) -> lower(t)
Reveal: stdout
`,
			input: "Fire,WATER,fire,Wind",
			want:  "{fire, water, wind}",
		},
		{
			name: "Map Keys keeps the values",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> tomap(list(tuple("a", 1), tuple("b", 2)))
Cursed Technique: Map Keys
    Using: (k) -> upper(k)
Reveal: stdout
`,
			input: "ignored",
			want:  "{A: 1, B: 2}",
		},
		{
			// Two keys mapping to one keep the later value — insert's rule.
			name: "Map Keys collides to the later value",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> tomap(list(tuple(point(0, 0), "a"), tuple(point(0, 5), "b")))
Cursed Technique: Map Keys
    Using: (p) -> prow(p)
Reveal: stdout
`,
			input: "ignored",
			want:  "{0: b}",
		},
		{
			// An isolated node survives, and arcs keep their weights.
			name: "Map Nodes relabels",
			src: graphHead + `Cursed Technique: Map Nodes
    Using: (n) -> slice(n, 0, 1)
Reveal: stdout
`,
			input: "alpha beta\nbeta gamma",
			want:  "{a: [(b, 1)], b: [(g, 1)], g: []}",
		},
		{
			// Two nodes mapping to one become one node holding both arcs.
			name: "Map Nodes merges",
			src: graphHead + `Cursed Technique: Map Nodes
    Using: (n) -> slice(n, 0, 1)
Reveal: stdout
`,
			input: "ax b\nay c",
			want:  "{a: [(b, 1), (c, 1)], b: [], c: []}",
		},
		{
			// The bridge from a node-weighted graph to an arc-weighted one:
			// weigh every arc by what entering its destination costs.
			name: "Map Weights carries a node cost onto in-arcs",
			src: `Cursed Energy: stdin
Cursed Object: cost As tomap(list(tuple("a", 5), tuple("b", 2), tuple("c", 9)))
` + graphHead[len("Cursed Energy: stdin\n"):] + `Cursed Technique: Map Weights
    Using: (f, t, w) -> getor(cost, t, 0)
Domain Expansion: Dijkstra
    Start: "a"
Reveal: stdout
`,
			input: "a b\nb c\na c",
			want:  "{a: 0, b: 2, c: 9}",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := runProgramWithInput(t, c.src, c.input)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if strings.TrimSpace(got) != c.want {
				t.Errorf("got %q, want %q", strings.TrimSpace(got), c.want)
			}
		})
	}
}

// Map Each still flattens every shape to a List — that is what these are the
// counterpart to, not a replacement for.
func TestMapEachStillProducesAList(t *testing.T) {
	got, err := runProgramWithInput(t, `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Set
Cursed Technique: Map Each
    Using: (t) -> lower(t)
Reveal: stdout
`, "Fire,fire,Wind")
	if err != nil {
		t.Fatalf("%v", err)
	}
	// A List, with the duplicate still in it: Map Each does not merge.
	if strings.TrimSpace(got) != "[fire, fire, wind]" {
		t.Errorf("Map Each over a Set = %q, want the flattened list [fire, fire, wind]", got)
	}
}
