package ir

import (
	"cmp"
	"slices"
)

// SparseValue is the dedicated nested/sparse grid: an unbounded 2D
// plane addressed by (row, col) int64 coordinates — negative coordinates
// included — where every position holds Def until explicitly written. Only
// written ("set") cells are stored, so memory is proportional to the data,
// not the bounding box.
//
// Semantics locked here (both backends and the docs follow them):
//   - Put never removes: a cell written with the default value is still set
//     (Has reports true; it participates in bounds, Len, and iteration).
//     Bounds are therefore exact and only ever grow.
//   - At is total: any coordinate reads Def when unset. There is no
//     out-of-bounds on an infinite plane.
//   - Iteration order is sorted row-major (by row, then column) — a sparse
//     grid is geometry, not a log, so insertion order is deliberately NOT
//     preserved (unlike MapValue/SetValue).
type SparseValue struct {
	Def                    Value
	cells                  map[[2]int64]Value
	minR, maxR, minC, maxC int64
}

// MaxSparseDense optionally caps the bounding-box area (rows × cols) that
// Convert To Grid will materialize from a sparse grid, in cells.
//
// Zero — the default — means unlimited. It used to be a hard 4,000,000, which
// refused a plot larger than that even when the machine had room for it; a
// limit must never be the reason a correct program cannot run. Two far-apart
// cells still imply a huge dense box, so the failure mode for a genuinely
// unreasonable densify is now memory pressure rather than a clean error. It
// is a var so a caller can opt back into a ceiling; the interpreter and the
// compiled backend read the same value, so the guard stays identical.
var MaxSparseDense = 0

// maxRepresentableCells is not a policy limit but a physical one: a slice
// this long cannot be allocated on any machine, and Go's makeslice panics
// rather than returning an error. Densifying past it is impossible, not
// merely expensive, so it still earns a clean message — removing the tunable
// ceiling above was about not refusing work that *could* succeed.
const maxRepresentableCells = 1 << 40

func NewSparseValue(def Value) *SparseValue {
	return &SparseValue{Def: def, cells: map[[2]int64]Value{}}
}

// Put sets cell (r, c), growing the bounds.
func (s *SparseValue) Put(r, c int64, v Value) {
	if len(s.cells) == 0 {
		s.minR, s.maxR, s.minC, s.maxC = r, r, c, c
	} else {
		if r < s.minR {
			s.minR = r
		}
		if r > s.maxR {
			s.maxR = r
		}
		if c < s.minC {
			s.minC = c
		}
		if c > s.maxC {
			s.maxC = c
		}
	}
	s.cells[[2]int64{r, c}] = v
}

// At reads cell (r, c): the stored value if set, Def otherwise. Total.
func (s *SparseValue) At(r, c int64) Value {
	if v, ok := s.cells[[2]int64{r, c}]; ok {
		return v
	}
	return s.Def
}

// Has reports whether (r, c) was explicitly set.
func (s *SparseValue) Has(r, c int64) bool {
	_, ok := s.cells[[2]int64{r, c}]
	return ok
}

// Len is the number of set cells.
func (s *SparseValue) Len() int { return len(s.cells) }

// Bounds returns the extremes over set cells; ok is false when no cell is set.
func (s *SparseValue) Bounds() (minR, minC, maxR, maxC int64, ok bool) {
	if len(s.cells) == 0 {
		return 0, 0, 0, 0, false
	}
	return s.minR, s.minC, s.maxR, s.maxC, true
}

// Points returns the set-cell coordinates in sorted row-major order — the
// canonical iteration order for rendering, Map/Count/Find Cells, and
// equality-independent determinism.
func (s *SparseValue) Points() [][2]int64 {
	pts := make([][2]int64, 0, len(s.cells))
	for k := range s.cells {
		pts = append(pts, k)
	}
	slices.SortFunc(pts, func(a, b [2]int64) int {
		if a[0] != b[0] {
			return cmp.Compare(a[0], b[0])
		}
		return cmp.Compare(a[1], b[1])
	})
	return pts
}

// Clone returns an independent copy (the expression layer's put is
// functional: it must not alias the original's cell map).
func (s *SparseValue) Clone() *SparseValue {
	out := &SparseValue{
		Def:   s.Def,
		cells: make(map[[2]int64]Value, len(s.cells)),
		minR:  s.minR, maxR: s.maxR, minC: s.minC, maxC: s.maxC,
	}
	for k, v := range s.cells {
		out.cells[k] = v
	}
	return out
}

// ToGrid materializes the bounding box as a dense GridValue, translating so
// (minR, minC) lands at (0, 0) and filling unset positions with Def. An empty
// sparse grid becomes the 0x0 grid. The caller enforces MaxSparseDense.
func (s *SparseValue) ToGrid() *GridValue {
	minR, minC, maxR, maxC, ok := s.Bounds()
	if !ok {
		return NewGridValue(0, 0)
	}
	rows, cols := int(maxR-minR+1), int(maxC-minC+1)
	g := NewGridValue(rows, cols)
	for i := range g.Cells {
		g.Cells[i] = s.Def
	}
	for k, v := range s.cells {
		g.SetAt(int(k[0]-minR), int(k[1]-minC), v)
	}
	return g
}

// TooLargeToDensify reports whether a bounding box of rows x cols cannot be
// materialized: either it exceeds the configured MaxSparseDense ceiling (when
// one is set), or it is beyond what a Go slice can represent at all. The
// per-side checks come first so rows*cols cannot overflow int64.
func TooLargeToDensify(rows, cols int64) bool {
	if rows <= 0 || cols <= 0 {
		return false
	}
	if MaxSparseDense > 0 {
		lim := int64(MaxSparseDense)
		if rows > lim || cols > lim || rows*cols > lim {
			return true
		}
	}
	return rows > maxRepresentableCells || cols > maxRepresentableCells ||
		rows > maxRepresentableCells/cols
}

// DensifyLimit is the cell count TooLargeToDensify compared against, for the
// error message.
func DensifyLimit() int64 {
	if MaxSparseDense > 0 {
		return int64(MaxSparseDense)
	}
	return maxRepresentableCells
}
