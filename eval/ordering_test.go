package eval

import (
	"testing"

	"domain/ir"
)

// `<` `>` `<=` `>=` over the composite half of ir.Ordered. The claim under
// test is not just that they work but that they answer what ir.Compare
// answers — a lambda's `a < b` and a Sort of the same values have to agree
// about what "before" means, or a sort key and the predicate that reads it
// disagree.

func TestTextComparison(t *testing.T) {
	for _, tc := range []struct {
		src  string
		a, b ir.Value
		want bool
	}{
		{"(a, b) -> a < b", "apple", "banana", true},
		{"(a, b) -> a < b", "banana", "apple", false},
		{"(a, b) -> a < b", "apple", "apple", false},
		{"(a, b) -> a <= b", "apple", "apple", true},
		{"(a, b) -> a > b", "banana", "apple", true},
		{"(a, b) -> a >= b", "apple", "apple", true},
		// Prefixes sort first, the same way strings.Compare orders them.
		{"(a, b) -> a < b", "app", "apple", true},
		{"(a, b) -> a < b", "", "a", true},
		// Byte-wise, so uppercase precedes lowercase.
		{"(a, b) -> a < b", "Z", "a", true},
	} {
		if got := mustEval(t, tc.src, tc.a, tc.b); got != tc.want {
			t.Errorf("%s over %q, %q = %v, want %v", tc.src, tc.a, tc.b, got, tc.want)
		}
	}
}

func TestTupleComparisonIsLexicographic(t *testing.T) {
	tup := func(vs ...ir.Value) ir.Value { return []ir.Value(vs) }
	for _, tc := range []struct {
		src  string
		a, b ir.Value
		want bool
	}{
		// The first differing element decides; equal prefixes fall through.
		{"(a, b) -> a < b", tup(int64(1), int64(9)), tup(int64(2), int64(0)), true},
		{"(a, b) -> a < b", tup(int64(2), int64(0)), tup(int64(2), int64(1)), true},
		{"(a, b) -> a < b", tup(int64(2), int64(1)), tup(int64(2), int64(1)), false},
		{"(a, b) -> a <= b", tup(int64(2), int64(1)), tup(int64(2), int64(1)), true},
		{"(a, b) -> a > b", tup(int64(3), int64(0)), tup(int64(2), int64(9)), true},
		// Heterogeneous, which is what a compound sort key looks like.
		{"(a, b) -> a < b", tup("a", int64(2)), tup("a", int64(3)), true},
		{"(a, b) -> a < b", tup("a", int64(9)), tup("b", int64(0)), true},
	} {
		if got := mustEval(t, tc.src, tc.a, tc.b); got != tc.want {
			t.Errorf("%s over %v, %v = %v, want %v", tc.src, tc.a, tc.b, got, tc.want)
		}
	}
}

// The operators and the sorting primitives share ir.Compare, so this is the
// property that matters: whatever `<` says, Compare said first.
func TestComparisonAgreesWithIrCompare(t *testing.T) {
	// One static type per comparison, so the two families stay apart — a Text
	// is never compared against a tuple.
	groups := [][]ir.Value{
		{"", "a", "ab", "b", "Z"},
		{
			[]ir.Value{int64(0), int64(0)},
			[]ir.Value{int64(0), int64(1)},
			[]ir.Value{int64(1), int64(0)},
		},
	}
	ops := []struct {
		src  string
		want func(int) bool
	}{
		{"(a, b) -> a < b", func(c int) bool { return c < 0 }},
		{"(a, b) -> a > b", func(c int) bool { return c > 0 }},
		{"(a, b) -> a <= b", func(c int) bool { return c <= 0 }},
		{"(a, b) -> a >= b", func(c int) bool { return c >= 0 }},
	}
	for _, vals := range groups {
		for _, a := range vals {
			for _, b := range vals {
				c := ir.Compare(a, b)
				for _, op := range ops {
					if got := mustEval(t, op.src, a, b); got != op.want(c) {
						t.Errorf("%s over %v, %v = %v, want %v (ir.Compare = %d)",
							op.src, a, b, got, op.want(c), c)
					}
				}
			}
		}
	}
}

// ir.Compare answers 0 for a shape it does not recognize, so that a sort stays
// stable rather than panicking mid-run. Reporting "equal" is the wrong answer
// for an operator, so eval guards the runtime shapes itself instead of
// trusting the resolver to have.
func TestComparisonRefusesAnUnorderedValue(t *testing.T) {
	wantErr(t, "(a, b) -> a < b", "has no ordering", true, false)
	wantErr(t, "(a, b) -> a >= b", "has no ordering", ir.NewMapValue(), ir.NewMapValue())
}
