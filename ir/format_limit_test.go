package ir

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// formatCorpus is one value of every kind FormatValue renders, so the tests
// below say something about the renderer rather than about one list of ints.
func formatCorpus(t *testing.T) map[string]Value {
	t.Helper()
	grid := NewGridValue(2, 3)
	for i, cell := range []Value{int64(1), int64(22), int64(3), int64(4), int64(5), int64(6)} {
		grid.Cells[i] = cell
	}
	textGrid := NewGridValue(2, 2)
	for i, cell := range []Value{"a", "b", "c", "d"} {
		textGrid.Cells[i] = cell
	}
	m := NewMapValue()
	m.Put("alpha", int64(1))
	m.Put("beta", int64(2))
	s := NewSetValue()
	s.Add(int64(3))
	s.Add(int64(4))
	sparse := NewSparseValue(int64(0))
	sparse.Put(0, 0, int64(7))
	sparse.Put(2, 1, int64(9))
	rec := NewRecordValue()
	rec.Set("name", "gojo")
	rec.Set("score", int64(120))

	long := make([]Value, 500)
	for i := range long {
		long[i] = int64(i)
	}
	nested := make([]Value, 40)
	for i := range nested {
		nested[i] = []Value{int64(i), "élan", []Value{true, 1.5}}
	}

	return map[string]Value{
		"int":       int64(42),
		"negative":  int64(-7),
		"float":     1.5,
		"text":      "gojo satoru",
		"unicode":   "領域展開 — 無量空処",
		"bool":      true,
		"empty":     []Value{},
		"list":      []Value{int64(1), int64(2), int64(3)},
		"longlist":  long,
		"nested":    nested,
		"record":    rec,
		"map":       m,
		"set":       s,
		"grid":      grid,
		"textgrid":  textGrid,
		"sparse":    sparse,
		"emptytext": "",
	}
}

// An unlimited FormatValueLimit is FormatValue: they are the same walk, and a
// caller reading a recording must see what Reveal would have printed.
func TestFormatValueLimitUnlimitedMatchesFormatValue(t *testing.T) {
	for name, v := range formatCorpus(t) {
		t.Run(name, func(t *testing.T) {
			got, complete := FormatValueLimit(v, 0)
			if !complete {
				t.Errorf("complete = false for an unlimited render")
			}
			if want := FormatValue(v); got != want {
				t.Errorf("FormatValueLimit(v, 0) = %q, want %q", got, want)
			}
		})
	}
}

// Whatever the limit, what comes back is a real prefix of the whole rendering
// and never longer than asked for. That is the property the value pane relies
// on when it says a value was cut rather than mangled.
func TestFormatValueLimitIsAPrefix(t *testing.T) {
	for name, v := range formatCorpus(t) {
		full := FormatValue(v)
		for _, limit := range []int{1, 2, 3, 7, 16, 64, 500, 10000} {
			got, complete := FormatValueLimit(v, limit)
			if len(got) > limit {
				t.Errorf("%s limit %d: got %d bytes", name, limit, len(got))
			}
			if !strings.HasPrefix(full, got) {
				t.Errorf("%s limit %d: %q is not a prefix of %q", name, limit, got, full)
			}
			if complete != (got == full) {
				t.Errorf("%s limit %d: complete = %v but got %q of %q",
					name, limit, complete, got, full)
			}
			if !utf8.ValidString(got) {
				t.Errorf("%s limit %d: cut mid-rune: %q", name, limit, got)
			}
		}
	}
}

// The cut lands on a rune boundary even when the limit falls inside a
// multi-byte character — the case a byte slice gets wrong and a terminal paints
// as a replacement character.
func TestFormatValueLimitCutsOnRuneBoundary(t *testing.T) {
	// "領" is three bytes, so a limit of 1, 2 or 4 all fall inside one.
	v := Value("領域展開")
	for _, limit := range []int{1, 2, 4, 5, 7, 8} {
		got, complete := FormatValueLimit(v, limit)
		if complete {
			t.Fatalf("limit %d: reported complete", limit)
		}
		if !utf8.ValidString(got) {
			t.Errorf("limit %d: %q is not valid UTF-8", limit, got)
		}
		if len(got)%3 != 0 {
			t.Errorf("limit %d: %q does not end on a rune boundary", limit, got)
		}
	}
}

// A limited render must stop *walking*, not merely stop appending — the whole
// reason it exists is to not touch the tail of a huge value.
func TestFormatValueLimitStopsWalking(t *testing.T) {
	const n = 200000
	huge := make([]Value, n)
	for i := range huge {
		huge[i] = int64(i)
	}
	got, complete := FormatValueLimit(huge, 64)
	if complete {
		t.Fatal("a 200k-element list reported complete at a 64-byte limit")
	}
	if len(got) != 64 {
		t.Errorf("got %d bytes, want 64", len(got))
	}
	if !strings.HasPrefix(got, "[0, 1, 2, ") {
		t.Errorf("got %q, want the head of the list", got)
	}
}

// The untyped grid writer and the typed one render the same picture when the
// type says nothing, which is what keeps two grid renderers from drifting.
func TestWriteGridMatchesFormatGridTyped(t *testing.T) {
	for name, v := range formatCorpus(t) {
		g, ok := v.(*GridValue)
		if !ok {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if got, want := FormatValue(g), formatGridTyped(g, nil); got != want {
				t.Errorf("FormatValue = %q, formatGridTyped = %q", got, want)
			}
		})
	}
}
