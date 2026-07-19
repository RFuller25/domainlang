package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// Composite Map/Set keys: points and other tuples/records are
// keyable, so Unique, Convert To Set + contains, Group By, Count By, and get
// work over them — the sparse-grid-as-map shape.

const intsHeader = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\"\n" +
	"Channeled Energy: Convert List to Integers\n"

func TestUniqueOverPoints(t *testing.T) {
	src := intsHeader +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> point(x, x)\n" +
		"Cursed Technique: Unique\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "3\n3\n4\n3")
	if v.(int64) != 2 {
		t.Fatalf("unique points: got %v want 2 ((3,3) and (4,4))", v)
	}
}

func TestSetOfPointsAndContains(t *testing.T) {
	src := intsHeader +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> point(x, 0)\n" +
		"Channeled Energy: Convert To Set\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (s) -> contains(s, point(3, 0))\n"
	v, _ := runPipeline(t, src, "1\n3\n5")
	if v.(bool) != true {
		t.Fatalf("contains((3,0)): got %v want true", v)
	}
	v, _ = runPipeline(t, src, "1\n2\n5")
	if v.(bool) != false {
		t.Fatalf("contains((3,0)) over {1,2,5}: got %v want false", v)
	}
}

func TestCountByPointKey(t *testing.T) {
	// Group 1..5 by (x / 2, 0): (0,0)->1, (1,0)->{2,3}, (2,0)->{4,5}.
	src := intsHeader +
		"Maximum Technique: Count By\n" +
		"    Using: (x) -> point(x / 2, 0)\n"
	v, _ := runPipeline(t, src, "1\n2\n3\n4\n5")
	m := v.(*ir.MapValue)
	if m.Len() != 3 {
		t.Fatalf("count by point: got %d keys want 3 (%s)", m.Len(), ir.FormatValue(m))
	}
	if n, ok := m.Get([]ir.Value{int64(1), int64(0)}); !ok || n.(int64) != 2 {
		t.Fatalf("count for (1,0): got %v, %v want 2", n, ok)
	}
	// Insertion (first-seen) order is preserved in the rendering.
	if got := ir.FormatValue(m); got != "{[0, 0]: 1, [1, 0]: 2, [2, 0]: 2}" {
		t.Fatalf("render: got %q", got)
	}
}

func TestGroupByPointKeyAndGet(t *testing.T) {
	src := intsHeader +
		"Maximum Technique: Group By\n" +
		"    Using: (x) -> point(x / 10, 0)\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (m) -> length(get(m, point(1, 0)))\n"
	v, _ := runPipeline(t, src, "5\n12\n17\n23")
	if v.(int64) != 2 {
		t.Fatalf("group by point + get: got %v want 2 (12 and 17)", v)
	}
}

func TestListElementsStillNotKeyable(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \",\"\n" +
		"Cursed Technique: Unique\n"
	_, err := resolveSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "keyable") {
		t.Fatalf("List<List<Text>> must stay unkeyable, got: %v", err)
	}
}
