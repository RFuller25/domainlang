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

// NewRecordValueSized is NewRecordValue with a known field count, so a record
// built by the expression layer's `record(...)` sizes both halves once.
func NewRecordValueSized(n int) *RecordValue {
	return &RecordValue{Fields: make([]string, 0, n), Vals: make(map[string]Value, n)}
}

// With returns a copy of r with name bound to v — Set, functionally. The field
// must already exist (the expression layer's `with` checks that statically), so
// the copy is exactly the same shape.
func (r *RecordValue) With(name string, v Value) *RecordValue {
	out := &RecordValue{
		Fields: make([]string, len(r.Fields)),
		Vals:   make(map[string]Value, len(r.Vals)),
	}
	copy(out.Fields, r.Fields)
	for k, val := range r.Vals {
		out.Vals[k] = val
	}
	out.Vals[name] = v
	return out
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

// NewMapSized is NewMapValue with a known entry count, so building a map from
// a list of n pairs sizes its map once instead of growing through it.
func NewMapSized(n int) *MapValue {
	return &MapValue{keys: make([]Value, 0, n), vals: make(map[any]Value, n)}
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

// Clone returns an independent copy. The expression layer's insert/del are
// functional — they must not alias the original — and both maps are sized
// exactly once, so building a map in a fold costs one allocation per step
// rather than a growth sequence.
func (m *MapValue) Clone() *MapValue {
	out := &MapValue{
		keys: make([]Value, len(m.keys)),
		vals: make(map[any]Value, len(m.vals)),
	}
	copy(out.keys, m.keys)
	for k, v := range m.vals {
		out.vals[k] = v
	}
	return out
}

// With returns a copy of m with k bound to v — Put, functionally.
func (m *MapValue) With(k, v Value) *MapValue {
	out := m.Clone()
	out.Put(k, v)
	return out
}

// Without returns a copy of m with k removed, keeping insertion order. A key
// that is absent copies as-is rather than scanning twice.
func (m *MapValue) Without(k Value) *MapValue {
	ck := KeyOf(k)
	if _, ok := m.vals[ck]; !ok {
		return m.Clone()
	}
	out := &MapValue{
		keys: make([]Value, 0, len(m.keys)-1),
		vals: make(map[any]Value, len(m.vals)-1),
	}
	for _, key := range m.keys {
		if kk := KeyOf(key); kk != ck {
			out.keys = append(out.keys, key)
			out.vals[kk] = m.vals[kk]
		}
	}
	return out
}

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

// Clone returns an independent copy, sized exactly — see MapValue.Clone.
func (s *SetValue) Clone() *SetValue {
	out := &SetValue{
		elems: make([]Value, len(s.elems)),
		seen:  make(map[any]bool, len(s.seen)),
	}
	copy(out.elems, s.elems)
	for k := range s.seen {
		out.seen[k] = true
	}
	return out
}

// With returns a copy of s with v added — Add, functionally.
func (s *SetValue) With(v Value) *SetValue {
	out := s.Clone()
	out.Add(v)
	return out
}

// Without returns a copy of s with v removed, keeping insertion order.
func (s *SetValue) Without(v Value) *SetValue {
	ck := KeyOf(v)
	if !s.seen[ck] {
		return s.Clone()
	}
	out := &SetValue{
		elems: make([]Value, 0, len(s.elems)-1),
		seen:  make(map[any]bool, len(s.seen)-1),
	}
	for _, e := range s.elems {
		if ek := KeyOf(e); ek != ck {
			out.elems = append(out.elems, e)
			out.seen[ek] = true
		}
	}
	return out
}

// newSetSized is NewSetValue with a known capacity, so building a set from a
// list of n elements sizes its map once instead of growing through it.
func newSetSized(n int) *SetValue {
	return &SetValue{elems: make([]Value, 0, n), seen: make(map[any]bool, n)}
}

// SetFromList builds a set from a list, dropping duplicates.
func SetFromList(xs []Value) *SetValue {
	s := newSetSized(len(xs))
	for _, x := range xs {
		s.Add(x)
	}
	return s
}

// SetIntersect returns elements in both a and b, in a's order.
//
// a is walked rather than the smaller of the two because the result's order is
// part of the contract; the membership tests go to b either way, so the only
// thing size buys is the output's capacity hint.
func SetIntersect(a, b *SetValue) *SetValue {
	out := newSetSized(min(len(a.elems), len(b.elems)))
	for _, e := range a.elems {
		if b.Has(e) {
			out.Add(e)
		}
	}
	return out
}

// SetUnion returns all elements of a then b, deduplicated.
func SetUnion(a, b *SetValue) *SetValue {
	out := &SetValue{
		elems: make([]Value, len(a.elems), len(a.elems)+len(b.elems)),
		seen:  make(map[any]bool, len(a.elems)+len(b.elems)),
	}
	// a is already deduplicated, so its elements go in wholesale and only b's
	// need the membership test.
	copy(out.elems, a.elems)
	for k := range a.seen {
		out.seen[k] = true
	}
	for _, e := range b.elems {
		out.Add(e)
	}
	return out
}

// SetDifference returns elements of a not in b, in a's order.
func SetDifference(a, b *SetValue) *SetValue {
	out := newSetSized(len(a.elems))
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

// With returns a copy of g with cell (r, c) set to v — the functional update
// the expression layer's setat needs. Out of bounds is the caller's to reject:
// the dense grid is finite, so unlike SparseValue.Put there is no cell to
// write. One allocation, sized exactly.
func (g *GridValue) With(r, c int, v Value) *GridValue {
	out := &GridValue{Rows: g.Rows, Cols: g.Cols, Cells: make([]Value, len(g.Cells))}
	copy(out.Cells, g.Cells)
	out.Cells[r*g.Cols+c] = v
	return out
}

// Neighbors returns the in-bounds neighbor coordinates of (r, c). With
// diagonal=false it returns the 4 orthogonal neighbors; with diagonal=true the
// 8 surrounding cells.
func (g *GridValue) Neighbors(r, c int, diagonal bool) [][2]int {
	// A fixed array sliced to length keeps the delta table off the heap: this
	// runs once per cell of every grid traversal.
	deltas := [8][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}, {-1, -1}, {-1, 1}, {1, -1}, {1, 1}}
	n := 4
	if diagonal {
		n = 8
	}
	var out [][2]int
	for _, d := range deltas[:n] {
		nr, nc := r+d[0], c+d[1]
		if g.InBounds(nr, nc) {
			out = append(out, [2]int{nr, nc})
		}
	}
	return out
}
