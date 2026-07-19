package ir

import "testing"

func TestSparseValueBasics(t *testing.T) {
	s := NewSparseValue(int64(0))
	if s.Len() != 0 {
		t.Fatalf("empty sparse Len = %d", s.Len())
	}
	if _, _, _, _, ok := s.Bounds(); ok {
		t.Fatal("empty sparse reported bounds")
	}
	if got := s.At(5, -3); got != int64(0) {
		t.Fatalf("At on empty = %v, want default 0", got)
	}

	s.Put(2, 3, int64(7))
	s.Put(-1, 10, int64(9))
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
	minR, minC, maxR, maxC, ok := s.Bounds()
	if !ok || minR != -1 || minC != 3 || maxR != 2 || maxC != 10 {
		t.Fatalf("Bounds = %d %d %d %d %v", minR, minC, maxR, maxC, ok)
	}
	if !s.Has(2, 3) || s.Has(0, 0) {
		t.Fatal("Has wrong")
	}
	if got := s.At(2, 3); got != int64(7) {
		t.Fatalf("At(2,3) = %v", got)
	}
	if got := s.At(100, 100); got != int64(0) {
		t.Fatalf("At unset = %v, want default", got)
	}
}

// A cell explicitly written with the default value is still set.
func TestSparsePutDefaultIsSet(t *testing.T) {
	s := NewSparseValue(int64(0))
	s.Put(4, 4, int64(0))
	if !s.Has(4, 4) || s.Len() != 1 {
		t.Fatal("cell set to the default value must stay set")
	}
}

func TestSparsePointsSortedRowMajor(t *testing.T) {
	s := NewSparseValue("")
	// Insert deliberately out of order.
	s.Put(1, 0, "c")
	s.Put(0, 5, "b")
	s.Put(0, -2, "a")
	s.Put(1, 1, "d")
	want := [][2]int64{{0, -2}, {0, 5}, {1, 0}, {1, 1}}
	got := s.Points()
	if len(got) != len(want) {
		t.Fatalf("Points len = %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Points[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestSparseFormatValue(t *testing.T) {
	s := NewSparseValue(".")
	if got := FormatValue(s); got != "{}" {
		t.Fatalf("empty sparse renders %q", got)
	}
	s.Put(1, 2, "#")
	s.Put(0, 0, "#")
	if got := FormatValue(s); got != "{[0, 0]: #, [1, 2]: #}" {
		t.Fatalf("sparse renders %q", got)
	}
	if got := FormatShort(s); got != "Sparse 2x3 (2 set)" {
		t.Fatalf("FormatShort = %q", got)
	}
}

func TestSparseDeepEqual(t *testing.T) {
	a := NewSparseValue(int64(0))
	b := NewSparseValue(int64(0))
	a.Put(1, 1, int64(5))
	b.Put(1, 1, int64(5))
	if !DeepEqual(a, b) {
		t.Fatal("equal sparse grids reported unequal")
	}
	b.Put(2, 2, int64(0)) // set-but-default still distinguishes
	if DeepEqual(a, b) {
		t.Fatal("different set-cell counts reported equal")
	}
	c := NewSparseValue(int64(1))
	c.Put(1, 1, int64(5))
	if DeepEqual(a, c) {
		t.Fatal("different defaults reported equal")
	}
}

func TestSparseCloneIndependent(t *testing.T) {
	a := NewSparseValue(int64(0))
	a.Put(0, 0, int64(1))
	b := a.Clone()
	b.Put(9, 9, int64(2))
	if a.Has(9, 9) || a.Len() != 1 {
		t.Fatal("Clone aliases the original")
	}
	if !DeepEqual(a, a.Clone()) {
		t.Fatal("Clone not equal to original")
	}
}

func TestSparseToGrid(t *testing.T) {
	s := NewSparseValue(".")
	g := s.ToGrid()
	if g.Rows != 0 || g.Cols != 0 {
		t.Fatalf("empty sparse ToGrid = %dx%d", g.Rows, g.Cols)
	}
	// Negative coordinates translate so (minR, minC) lands at (0, 0).
	s.Put(-1, -1, "#")
	s.Put(1, 2, "#")
	g = s.ToGrid()
	if g.Rows != 3 || g.Cols != 4 {
		t.Fatalf("ToGrid = %dx%d, want 3x4", g.Rows, g.Cols)
	}
	if v, _ := g.At(0, 0); v != "#" {
		t.Fatalf("translated corner = %v", v)
	}
	if v, _ := g.At(2, 3); v != "#" {
		t.Fatalf("translated far cell = %v", v)
	}
	if v, _ := g.At(1, 1); v != "." {
		t.Fatalf("unset cell = %v, want default", v)
	}
	if got := FormatValue(g); got != "#...\n....\n...#" {
		t.Fatalf("dense render = %q", got)
	}
}

func TestSparseTypeModel(t *testing.T) {
	st := Sparse(Int())
	if st.String() != "Sparse<Int>" {
		t.Fatalf("String = %q", st.String())
	}
	if !st.Equal(Sparse(Int())) || st.Equal(Sparse(Text())) || st.Equal(Grid(Int())) {
		t.Fatal("Sparse type equality wrong")
	}
	if Keyable(st) {
		t.Fatal("Sparse must not be keyable")
	}
	if DescribeValue(NewSparseValue(int64(0))) != "Sparse" {
		t.Fatal("DescribeValue wrong")
	}
}
