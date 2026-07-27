package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// Topological Sort takes two shapes, like Merge Ranges: an adjacency Map, and
// the edge list a parse actually lands on.

func edgeSrc(tail string) string {
	return "Cursed Energy: stdin\nShikigami: Lines\n" +
		"Cursed Technique: Match Pattern\n    Mode: Each\n    Using: \"{word} -> {word}\"\n" + tail
}

func TestTopologicalSortOverEdgeList(t *testing.T) {
	v, _ := runPipeline(t, edgeSrc("Domain Expansion: Topological Sort\n"),
		"a -> b\na -> c\nb -> d\nc -> d")
	if got := ir.FormatValue(v); got != "[a, b, c, d]" {
		t.Fatalf("topological order: got %s", got)
	}
}

// Ties break by first-seen order, so the answer is deterministic rather than
// merely valid — two runs, and the two backends, must agree.
func TestTopologicalSortIsDeterministic(t *testing.T) {
	src := edgeSrc("Domain Expansion: Topological Sort\n")
	first, _ := runPipeline(t, src, "b -> z\na -> z\nc -> z")
	for i := 0; i < 5; i++ {
		again, _ := runPipeline(t, src, "b -> z\na -> z\nc -> z")
		if ir.FormatValue(first) != ir.FormatValue(again) {
			t.Fatalf("order is not deterministic: %s vs %s",
				ir.FormatValue(first), ir.FormatValue(again))
		}
	}
	if got := ir.FormatValue(first); got != "[b, a, c, z]" {
		t.Fatalf("expected first-seen tie-breaking, got %s", got)
	}
}

// A node that only ever appears as a target still belongs in the output.
func TestTopologicalSortIncludesLeaves(t *testing.T) {
	v, _ := runPipeline(t, edgeSrc("Maximum Technique: Count\n"), "a -> b")
	if v.(int64) != 1 {
		t.Fatalf("sanity: one edge, got %v", v)
	}
	v, _ = runPipeline(t, edgeSrc("Domain Expansion: Topological Sort\nMaximum Technique: Count\n"), "a -> b")
	if v.(int64) != 2 {
		t.Fatalf("both endpoints should be ordered, got %v", v)
	}
}

func TestTopologicalSortOverAdjacencyMap(t *testing.T) {
	// Group By over the source column gives Map<Text, List<Text>> once the
	// values are the targets themselves.
	src := "Cursed Energy: stdin\nShikigami: Lines\n" +
		"Cursed Technique: Match Pattern\n    Mode: Each\n    Using: \"{a:word} -> {b:word}\"\n" +
		"Maximum Technique: Group By\n    Using: (r) -> r.a\n" +
		"Cursed Technique: Map Values\n    Using: (rs) -> list(item(rs, 0).b)\n" +
		"Domain Expansion: Topological Sort\n"
	v, _ := runPipeline(t, src, "a -> b\nb -> c")
	if got := ir.FormatValue(v); got != "[a, b, c]" {
		t.Fatalf("adjacency-map form: got %s", got)
	}
}

// A cycle names a blocked node: "there is a cycle" alone leaves the user to
// find it by hand in a large input.
func TestTopologicalSortCycleNamesANode(t *testing.T) {
	_, err := runErr(t, edgeSrc("Domain Expansion: Topological Sort\n"), "a -> b\nb -> c\nc -> a")
	if err == nil {
		t.Fatal("expected a cycle error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cycle") || !strings.Contains(msg, "blocked") {
		t.Fatalf("error should name a blocked node, got: %s", msg)
	}
}

func TestTopologicalSortRejectsWrongShape(t *testing.T) {
	_, err := resolveSrc(t, "Cursed Energy: stdin\nShikigami: Ints\nDomain Expansion: Topological Sort\n")
	if err == nil {
		t.Fatal("expected a shape error for List<Int>")
	}
	if msg := err.Error(); !strings.Contains(msg, "Map<K, List<K>>") {
		t.Fatalf("error should name the accepted shapes, got: %s", msg)
	}
}
