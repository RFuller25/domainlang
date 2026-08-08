package eval

import (
	"testing"

	"domain/ir"
)

// emptylist(v) completes the witness family beside emptyset and emptymap.
// `list()` cannot be the spelling: with no arguments there is nothing to read
// the element type from, and every expression's type is fixed at resolve time.
func TestEmptyListTakesItsTypeFromTheWitness(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"(xs) -> emptylist(0)", "[]"},
		{"(xs) -> length(emptylist(\"\"))", "0"},
		{"(xs) -> concat(emptylist(0), xs)", "[1, 2]"},
		{"(xs) -> sum(emptylist(0))", "0"},
		// The shape the wart showed up in: a record field that is a list of
		// nothing, which used to need take(split(x, \",\"), 0).
		{"(xs) -> record(\"kids\", emptylist(\"\"))", "{kids: []}"},
	} {
		got := ir.FormatValue(mustEval(t, tc.src, intList(1, 2)))
		if got != tc.want {
			t.Errorf("%s = %s, want %s", tc.src, got, tc.want)
		}
	}
}

// The witness is discarded but still *evaluated*, so a failing one fails the
// expression — the rule emptyset already follows, and what keeps the two
// backends agreeing about `emptylist(first(xs))` over an empty list.
func TestEmptyListWitnessIsStillEvaluated(t *testing.T) {
	evalErr(t, "(xs) -> emptylist(item(xs, 5))", "index", intList(1))
}
