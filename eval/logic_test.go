package eval

import (
	"testing"

	"domain/ir"
)

// The bitwise reducers, and the logical connectives in function form.

func TestBitwiseReducers(t *testing.T) {
	xs := intList(12, 10, 6)
	for _, tc := range []struct{ src, want string }{
		{"(xs) -> bxorall(xs)", "0"}, // 12^10^6
		{"(xs) -> borall(xs)", "14"}, // 12|10|6
		{"(xs) -> bandall(xs)", "0"}, // 12&10&6
		{"(xs) -> bxorall(list(7))", "7"},
		// The seed is each operator's identity, so the empty list is the value
		// that leaves a later fold unchanged — sum(0) and product(1)'s rule.
		// For `and` that is all bits set, which is -1 in two's complement.
		{"(xs) -> bxorall(emptylist(0))", "0"},
		{"(xs) -> borall(emptylist(0))", "0"},
		{"(xs) -> bandall(emptylist(0))", "-1"},
	} {
		if got := ir.FormatValue(mustEval(t, tc.src, xs)); got != tc.want {
			t.Errorf("%s = %s, want %s", tc.src, got, tc.want)
		}
	}
}

// bandall's identity is the one a reader is most likely to expect to be 0, so
// it gets its own check against the property that defines it.
func TestBandallIdentityLeavesAFoldUnchanged(t *testing.T) {
	got := ir.FormatValue(mustEval(t, "(xs) -> band(bandall(emptylist(0)), 12)", intList(1)))
	if got != "12" {
		t.Errorf("band(bandall([]), 12) = %s, want 12 — the empty AND is not the identity", got)
	}
}

func TestLogicalConnectivesAsFunctions(t *testing.T) {
	xs := intList(1, 0)
	for _, tc := range []struct{ src, want string }{
		{"(xs) -> and(first(xs) > 0, last(xs) > 0)", "false"},
		{"(xs) -> and(first(xs) > 0, last(xs) >= 0)", "true"},
		{"(xs) -> or(first(xs) < 0, last(xs) >= 0)", "true"},
		{"(xs) -> or(first(xs) < 0, last(xs) > 0)", "false"},
		// xor is the one with no infix spelling at all.
		{"(xs) -> xor(first(xs) > 0, last(xs) > 0)", "true"},
		{"(xs) -> xor(first(xs) > 0, last(xs) >= 0)", "false"},
		{"(xs) -> not(first(xs) < 0)", "true"},
	} {
		if got := ir.FormatValue(mustEval(t, tc.src, xs)); got != tc.want {
			t.Errorf("%s = %s, want %s", tc.src, got, tc.want)
		}
	}
}

// The function forms do **not** short-circuit: every argument is evaluated
// before the call dispatches, which is how every other builtin behaves. Infix
// `and`/`or` do short-circuit. The difference is observable, so it is pinned
// rather than left to be discovered.
func TestFunctionFormsDoNotShortCircuit(t *testing.T) {
	// The right operand is an expression that *fails*. Under infix `and` a
	// false left operand means it never runs; under and(...) it does, and the
	// failure reaches the program.
	if got := ir.FormatValue(mustEval(t, "(xs) -> first(xs) > 99 and item(xs, 5) > 0", intList(1))); got != "false" {
		t.Errorf("infix `and` should short-circuit past the failing operand; got %s", got)
	}
	evalErr(t, "(xs) -> and(first(xs) > 99, item(xs, 5) > 0)", "index", intList(1))

	// Same for `or` with a true left operand.
	if got := ir.FormatValue(mustEval(t, "(xs) -> first(xs) > 0 or item(xs, 5) > 0", intList(1))); got != "true" {
		t.Errorf("infix `or` should short-circuit past the failing operand; got %s", got)
	}
	evalErr(t, "(xs) -> or(first(xs) > 0, item(xs, 5) > 0)", "index", intList(1))
}

func TestLogicRefusals(t *testing.T) {
	evalErr(t, "(xs) -> and(first(xs), first(xs) > 0)", "expected Bool", intList(1))
	evalErr(t, "(xs) -> not(first(xs))", "expected Bool", intList(1))
}
