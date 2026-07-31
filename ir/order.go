package ir

import (
	"cmp"
	"strings"
)

// Ordering over values, for the sorting primitives. v0.4 could only sort by an
// Int key, which left `List<Text>` unsortable and made a two-level sort (sort
// by group, tiebreak by score) inexpressible. An *ordered* type is Int, Float,
// Text, or a Tuple built from ordered types; tuples compare lexicographically,
// which is what makes them usable as compound sort keys.
//
// Ordered is deliberately narrower than Keyable: Float is ordered but not
// keyable (NaN and ±0.0 make float identity treacherous), and a Record is
// keyable but not ordered (its fields have names, not positions, so there is
// no obvious precedence between them).

// Ordered reports whether values of t support < and >.
func Ordered(t *Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case KInt, KFloat, KText:
		return true
	case KTuple:
		for _, e := range t.Elems {
			if !Ordered(e) {
				return false
			}
		}
		return len(t.Elems) > 0
	}
	return false
}

// Compare returns -1, 0, or 1. It assumes both values inhabit the same
// Ordered type, which the resolver checked; a shape it does not recognize
// compares equal so a sort stays stable rather than panicking mid-run.
//
// Floats use plain < and >, so a NaN compares equal to everything. That
// matches the rest of the float story (a float Sort runs exactly as written,
// exempt from the reordering passes) rather than inventing a total order.
func Compare(a, b Value) int {
	switch x := a.(type) {
	case int64:
		if y, ok := b.(int64); ok {
			return cmp.Compare(x, y)
		}
		// Mixed Int/Float compares through the numeric tower's promotion.
		if y, ok := b.(float64); ok {
			return compareFloat(float64(x), y)
		}
	case float64:
		switch y := b.(type) {
		case float64:
			return compareFloat(x, y)
		case int64:
			return compareFloat(x, float64(y))
		}
	case string:
		if y, ok := b.(string); ok {
			return strings.Compare(x, y)
		}
	case []Value:
		// Tuples: lexicographic, first differing element decides. A shorter
		// tuple sorts first, though the resolver makes ragged comparisons
		// impossible — both sides share one static type.
		y, ok := b.([]Value)
		if !ok {
			return 0
		}
		for i := range min(len(x), len(y)) {
			if c := Compare(x[i], y[i]); c != 0 {
				return c
			}
		}
		return cmp.Compare(len(x), len(y))
	}
	return 0
}

func compareFloat(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}
