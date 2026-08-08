package typecheck

import (
	"testing"

	"domain/ir"
)

// The relational operators reach exactly as far as ir.Ordered — the ordering
// Sort and Sort By already use. Before this they stopped at the numeric types,
// which left `Sort` able to order a List<Text> that no lambda could then
// compare, and made a text tiebreak inside a predicate unwritable.

func TestComparisonOverOrderedTypes(t *testing.T) {
	text, in := ir.Text(), ir.Int()
	pt := ir.Tuple(in, in)
	mixed := ir.Tuple(text, in)

	// The numeric cases, unchanged — including mixing Int with Float, which
	// compares through the tower's promotion rather than through Ordered.
	wantType(t, "(a, b) -> a < b", ir.Bool(), in, in)
	wantType(t, "(a, b) -> a >= b", ir.Bool(), ir.Float(), ir.Float())
	wantType(t, "(a, b) -> a < b", ir.Bool(), in, ir.Float())

	// Text, all four operators.
	wantType(t, "(a, b) -> a < b", ir.Bool(), text, text)
	wantType(t, "(a, b) -> a > b", ir.Bool(), text, text)
	wantType(t, "(a, b) -> a <= b", ir.Bool(), text, text)
	wantType(t, "(a, b) -> a >= b", ir.Bool(), text, text)
	wantType(t, `(w) -> w < "c"`, ir.Bool(), text)

	// Tuples, including the heterogeneous ones a compound sort key is built
	// from. A tuple of ordered elements is ordered; the elements decide.
	wantType(t, "(a, b) -> a < b", ir.Bool(), pt, pt)
	wantType(t, "(a, b) -> a >= b", ir.Bool(), mixed, mixed)
	wantType(t, "(p) -> p < point(3, 4)", ir.Bool(), pt)
}

func TestComparisonRefusesUnorderedTypes(t *testing.T) {
	// Ordered is narrower than Keyable: a Record is keyable but has no
	// ordering, because its fields have names rather than positions.
	rec := ir.Record(ir.Field{Name: "a", Type: ir.Int()})
	for _, tc := range []struct {
		src string
		typ *ir.Type
	}{
		{"(a, b) -> a < b", ir.List(ir.Int())},
		{"(a, b) -> a > b", ir.Set(ir.Int())},
		{"(a, b) -> a <= b", ir.Map(ir.Text(), ir.Int())},
		{"(a, b) -> a >= b", ir.Grid(ir.Int())},
		{"(a, b) -> a < b", ir.Sparse(ir.Int())},
		{"(a, b) -> a < b", ir.Bool()},
		{"(a, b) -> a < b", rec},
		// A tuple is only ordered when every element is.
		{"(a, b) -> a < b", ir.Tuple(ir.Int(), ir.Bool())},
	} {
		wantTypeErr(t, tc.src, "has no ordering", tc.typ, tc.typ)
	}
}

// A mismatch reports the two types and spells the operator the way it was
// written. token.Kind's String is the symbolic name ("LT"), which is right for
// a parser trace and wrong beside the user's own source line.
func TestComparisonMismatchNamesTheOperator(t *testing.T) {
	wantTypeErr(t, "(a, b) -> a < b", "cannot compare Text < Int (different types)",
		ir.Text(), ir.Int())
	wantTypeErr(t, "(a, b) -> a >= b", "cannot compare Text >= Int (different types)",
		ir.Text(), ir.Int())
	wantTypeErr(t, "(a, b) -> a <= b", "<= cannot compare it", ir.Bool(), ir.Bool())
}
