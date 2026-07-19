package ir

// Runtime representations of the v0.2 composite value kinds. These structs are
// suffixed *Value to distinguish them from the same-named static type
// constructors (Record, Map, Set, Grid). Tuples reuse []Value (a fixed-length
// list), so they need no dedicated struct — the distinction lives only in the
// static type. All collections preserve insertion order so output is
// deterministic.

// ---------------------------------------------------------------------------
// RecordValue — named fields, declaration order preserved for rendering.
// ---------------------------------------------------------------------------

type RecordValue struct {
	Fields []string // field names in declared order
	Vals   map[string]Value
}

func NewRecordValue() *RecordValue {
	return &RecordValue{Vals: map[string]Value{}}
}

// Set adds or replaces a field, preserving first-seen order.
func (r *RecordValue) Set(name string, v Value) {
	if _, ok := r.Vals[name]; !ok {
		r.Fields = append(r.Fields, name)
	}
	r.Vals[name] = v
}

func (r *RecordValue) Get(name string) (Value, bool) {
	v, ok := r.Vals[name]
	return v, ok
}

// ---------------------------------------------------------------------------
// MapValue — keys are any keyable value (Int/Text scalars, or Tuples/Records
// of keyable values via KeyOf), iteration in insertion order.
// ---------------------------------------------------------------------------

type MapValue struct {
	keys []Value
	vals map[any]Value
}

func NewMapValue() *MapValue {
	return &MapValue{vals: map[any]Value{}}
}

func (m *MapValue) Put(k, v Value) {
	ck := KeyOf(k)
	if _, ok := m.vals[ck]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[ck] = v
}

func (m *MapValue) Get(k Value) (Value, bool) {
	v, ok := m.vals[KeyOf(k)]
	return v, ok
}

func (m *MapValue) Has(k Value) bool {
	_, ok := m.vals[KeyOf(k)]
	return ok
}

func (m *MapValue) Len() int { return len(m.keys) }

// Keys returns the keys in insertion order.
func (m *MapValue) Keys() []Value { return m.keys }

// ---------------------------------------------------------------------------
// SetValue — keyable elements (see KeyOf), insertion order preserved.
// ---------------------------------------------------------------------------

type SetValue struct {
	elems []Value
	seen  map[any]bool
}

func NewSetValue() *SetValue {
	return &SetValue{seen: map[any]bool{}}
}

// Add inserts v, reporting whether it was newly added.
func (s *SetValue) Add(v Value) bool {
	ck := KeyOf(v)
	if s.seen[ck] {
		return false
	}
	s.seen[ck] = true
	s.elems = append(s.elems, v)
	return true
}

func (s *SetValue) Has(v Value) bool { return s.seen[KeyOf(v)] }
func (s *SetValue) Len() int         { return len(s.elems) }
func (s *SetValue) Elems() []Value   { return s.elems }

// SetFromList builds a set from a list, dropping duplicates.
func SetFromList(xs []Value) *SetValue {
	s := NewSetValue()
	for _, x := range xs {
		s.Add(x)
	}
	return s
}

// SetIntersect returns elements in both a and b, in a's order.
func SetIntersect(a, b *SetValue) *SetValue {
	out := NewSetValue()
	for _, e := range a.elems {
		if b.Has(e) {
			out.Add(e)
		}
	}
	return out
}

// SetUnion returns all elements of a then b, deduplicated.
func SetUnion(a, b *SetValue) *SetValue {
	out := NewSetValue()
	for _, e := range a.elems {
		out.Add(e)
	}
	for _, e := range b.elems {
		out.Add(e)
	}
	return out
}

// SetDifference returns elements of a not in b, in a's order.
func SetDifference(a, b *SetValue) *SetValue {
	out := NewSetValue()
	for _, e := range a.elems {
		if !b.Has(e) {
			out.Add(e)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// GridValue — 2D, row-major, 0-based (row, col). Out-of-bounds access is safe.
// ---------------------------------------------------------------------------

type GridValue struct {
	Rows  int
	Cols  int
	Cells []Value // row-major: index = row*Cols + col
}

func NewGridValue(rows, cols int) *GridValue {
	return &GridValue{Rows: rows, Cols: cols, Cells: make([]Value, rows*cols)}
}

func (g *GridValue) InBounds(r, c int) bool {
	return r >= 0 && r < g.Rows && c >= 0 && c < g.Cols
}

// At returns the cell at (r, c). Out-of-bounds returns (nil, false) — the safe
// policy from docs/data-model.md, so neighbor walks need no bounds checks.
func (g *GridValue) At(r, c int) (Value, bool) {
	if !g.InBounds(r, c) {
		return nil, false
	}
	return g.Cells[r*g.Cols+c], true
}

func (g *GridValue) SetAt(r, c int, v Value) {
	if g.InBounds(r, c) {
		g.Cells[r*g.Cols+c] = v
	}
}

// Neighbors returns the in-bounds neighbor coordinates of (r, c). With
// diagonal=false it returns the 4 orthogonal neighbors; with diagonal=true the
// 8 surrounding cells.
func (g *GridValue) Neighbors(r, c int, diagonal bool) [][2]int {
	deltas := [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	if diagonal {
		deltas = append(deltas, [2]int{-1, -1}, [2]int{-1, 1}, [2]int{1, -1}, [2]int{1, 1})
	}
	var out [][2]int
	for _, d := range deltas {
		nr, nc := r+d[0], c+d[1]
		if g.InBounds(nr, nc) {
			out = append(out, [2]int{nr, nc})
		}
	}
	return out
}
