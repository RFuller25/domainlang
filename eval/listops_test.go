package eval

import (
	"testing"

	"domain/ir"
)

// The first-order list builtins. Each mirrors the primitive of the same job,
// so the tests that matter are the ones pinning the corners where two
// plausible answers exist: what `chunk` does with a short final block, what
// `windows` does with the same, what `product` gives for the empty list.

func TestFirstOrderListBuiltins(t *testing.T) {
	xs := intList(3, 1, 2, 1)
	for _, tc := range []struct {
		src  string
		args []ir.Value
		want string
	}{
		{"(xs) -> sort(xs)", []ir.Value{xs}, "[1, 1, 2, 3]"},
		{"(xs) -> unique(xs)", []ir.Value{xs}, "[3, 1, 2]"}, // first-seen order
		{"(xs) -> enumerate(xs)", []ir.Value{intList(7, 8)}, "[[0, 7], [1, 8]]"},
		{"(a, b) -> zip(a, b)", []ir.Value{intList(1, 2), intList(9, 8)}, "[[1, 9], [2, 8]]"},
		// Truncated to the shorter, like the Zip consumer.
		{"(a, b) -> zip(a, b)", []ir.Value{intList(1, 2, 3), intList(9)}, "[[1, 9]]"},
		{"(xs) -> flatten(list(xs, xs))", []ir.Value{intList(1, 2)}, "[1, 2, 1, 2]"},
		// chunk keeps a short final block; windows drops a partial one. That
		// is the whole difference between them.
		{"(xs) -> chunk(xs, 3)", []ir.Value{intList(1, 2, 3, 4)}, "[[1, 2, 3], [4]]"},
		{"(xs) -> windows(xs, 3)", []ir.Value{intList(1, 2, 3, 4)}, "[[1, 2, 3], [2, 3, 4]]"},
		{"(xs) -> chunk(xs, 9)", []ir.Value{intList(1, 2)}, "[[1, 2]]"},
		{"(xs) -> windows(xs, 9)", []ir.Value{intList(1, 2)}, "[]"},
		{"(xs) -> transpose(list(xs, xs))", []ir.Value{intList(1, 2)}, "[[1, 1], [2, 2]]"},
	} {
		got := ir.FormatValue(mustEval(t, tc.src, tc.args...))
		if got != tc.want {
			t.Errorf("%s = %s, want %s", tc.src, got, tc.want)
		}
	}
}

// product is to `1` what sum is to `0`: the identity of its operation, so an
// empty list has an answer rather than an error.
func TestProductIdentityAndFloats(t *testing.T) {
	if got := mustEval(t, "(xs) -> product(xs)", intList(2, 3, 4)); got != int64(24) {
		t.Errorf("product = %v, want 24", got)
	}
	if got := mustEval(t, "(xs) -> product(take(xs, 0))", intList(2, 3)); got != int64(1) {
		t.Errorf("product of the empty list = %v, want 1", got)
	}
	floats := []ir.Value{[]ir.Value{0.5, 4.0}}
	if got := mustEval(t, "(xs) -> product(xs)", floats...); got != 2.0 {
		t.Errorf("float product = %v, want 2", got)
	}
}

// sort is the ordering ir.Compare defines, so it reaches Text and tuples and
// agrees with what `<` and the Sort primitive say.
func TestSortReachesEveryOrderedType(t *testing.T) {
	texts := []ir.Value{[]ir.Value{"pear", "fig", "apple"}}
	if got := ir.FormatValue(mustEval(t, "(xs) -> sort(xs)", texts...)); got != "[apple, fig, pear]" {
		t.Errorf("sort over Text = %s", got)
	}
	tuples := []ir.Value{[]ir.Value{
		[]ir.Value{int64(2), int64(0)},
		[]ir.Value{int64(1), int64(9)},
		[]ir.Value{int64(2), int64(0) - 1},
	}}
	if got := ir.FormatValue(mustEval(t, "(xs) -> sort(xs)", tuples...)); got != "[[1, 9], [2, -1], [2, 0]]" {
		t.Errorf("sort over tuples = %s", got)
	}
}

func TestListBuiltinErrors(t *testing.T) {
	wantErr(t, "(xs) -> chunk(xs, 0)", "size must be >= 1", intList(1, 2))
	wantErr(t, "(xs) -> windows(xs, 0)", "size must be >= 1", intList(1, 2))
	// Ragged input has no transpose that keeps every cell, so it is refused
	// with the wording Convert To Grid uses for the same shape problem.
	ragged := []ir.Value{[]ir.Value{
		[]ir.Value{int64(1), int64(2)},
		[]ir.Value{int64(3)},
	}}
	wantErr(t, "(xss) -> transpose(xss)", "not rectangular", ragged...)
}
