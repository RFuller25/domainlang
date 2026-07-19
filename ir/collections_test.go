package ir

import "testing"

func TestRecordValueOrderAndGet(t *testing.T) {
	r := NewRecordValue()
	r.Set("a", int64(1))
	r.Set("b", int64(2))
	r.Set("a", int64(9)) // overwrite keeps position
	if got := []string{r.Fields[0], r.Fields[1]}; got[0] != "a" || got[1] != "b" || len(r.Fields) != 2 {
		t.Fatalf("field order wrong: %v", r.Fields)
	}
	if v, _ := r.Get("a"); v.(int64) != 9 {
		t.Fatalf("overwrite failed: %v", v)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("missing field should not be found")
	}
}

func TestMapValueInsertionOrder(t *testing.T) {
	m := NewMapValue()
	m.Put("x", int64(1))
	m.Put("y", int64(2))
	m.Put("x", int64(3)) // overwrite, no reorder
	if m.Len() != 2 {
		t.Fatalf("len: got %d", m.Len())
	}
	keys := m.Keys()
	if keys[0] != "x" || keys[1] != "y" {
		t.Fatalf("key order: %v", keys)
	}
	if v, _ := m.Get("x"); v.(int64) != 3 {
		t.Fatalf("overwrite: %v", v)
	}
	if !m.Has("y") || m.Has("z") {
		t.Fatal("Has wrong")
	}
}

func TestSetValueDedupAndOps(t *testing.T) {
	a := SetFromList([]Value{int64(1), int64(2), int64(2), int64(3)})
	if a.Len() != 3 {
		t.Fatalf("dedup failed: %d", a.Len())
	}
	b := SetFromList([]Value{int64(2), int64(3), int64(4)})

	inter := SetIntersect(a, b)
	if inter.Len() != 2 || !inter.Has(int64(2)) || !inter.Has(int64(3)) {
		t.Fatalf("intersect: %v", inter.Elems())
	}
	union := SetUnion(a, b)
	if union.Len() != 4 {
		t.Fatalf("union: %v", union.Elems())
	}
	diff := SetDifference(a, b)
	if diff.Len() != 1 || !diff.Has(int64(1)) {
		t.Fatalf("difference: %v", diff.Elems())
	}
}

func TestGridAccessAndNeighbors(t *testing.T) {
	g := NewGridValue(3, 3)
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			g.SetAt(r, c, int64(r*3+c))
		}
	}
	if v, ok := g.At(1, 1); !ok || v.(int64) != 4 {
		t.Fatalf("center cell: %v %v", v, ok)
	}
	// Out-of-bounds is safe.
	if _, ok := g.At(-1, 0); ok {
		t.Fatal("OOB should return ok=false")
	}
	if _, ok := g.At(3, 3); ok {
		t.Fatal("OOB should return ok=false")
	}

	// Corner has 2 orthogonal neighbors, 3 with diagonals.
	if n := g.Neighbors(0, 0, false); len(n) != 2 {
		t.Fatalf("corner 4-neighbors: %v", n)
	}
	if n := g.Neighbors(0, 0, true); len(n) != 3 {
		t.Fatalf("corner 8-neighbors: %v", n)
	}
	// Center has 4 and 8.
	if n := g.Neighbors(1, 1, false); len(n) != 4 {
		t.Fatalf("center 4-neighbors: %v", n)
	}
	if n := g.Neighbors(1, 1, true); len(n) != 8 {
		t.Fatalf("center 8-neighbors: %v", n)
	}
}

func TestRenderersForCompositeValues(t *testing.T) {
	r := NewRecordValue()
	r.Set("a", int64(2))
	r.Set("b", int64(4))
	if got := FormatValue(r); got != "{a: 2, b: 4}" {
		t.Fatalf("record FormatValue: %q", got)
	}
	if DescribeValue(r) != "Record" {
		t.Fatalf("record describe: %q", DescribeValue(r))
	}

	m := NewMapValue()
	m.Put("x", int64(1))
	if got := FormatValue(m); got != "{x: 1}" {
		t.Fatalf("map FormatValue: %q", got)
	}

	s := SetFromList([]Value{int64(1), int64(2)})
	if got := FormatValue(s); got != "{1, 2}" {
		t.Fatalf("set FormatValue: %q", got)
	}

	g := NewGridValue(2, 2)
	g.SetAt(0, 0, "a")
	g.SetAt(0, 1, "b")
	g.SetAt(1, 0, "c")
	g.SetAt(1, 1, "d")
	if got := FormatValue(g); got != "ab\ncd" {
		t.Fatalf("char grid FormatValue: %q", got)
	}
	if got := FormatShort(g); got != "Grid 2x2" {
		t.Fatalf("grid FormatShort: %q", got)
	}
}

func TestFormatShortTruncatesCollections(t *testing.T) {
	xs := make([]Value, 12)
	for i := range xs {
		xs[i] = int64(i)
	}
	got := FormatShort(xs)
	if got != "[0, 1, 2, 3, 4, 5, 6, 7, …(4 more)]" {
		t.Fatalf("truncation: %q", got)
	}
}
