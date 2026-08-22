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
