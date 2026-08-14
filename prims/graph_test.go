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
