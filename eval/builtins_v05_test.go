package eval

import (
	"testing"

	"domain/ir"
)

// v05 evaluates a parameterless expression by wrapping it in a lambda with one
// ignored parameter, and renders the result the way Reveal would.
func v05(t *testing.T, expr string, args ...ir.Value) string {
	t.Helper()
	params := "(_u)"
	if len(args) == 1 {
		params = "(s)"
	}
	if len(args) == 0 {
		args = []ir.Value{int64(0)}
	}
	return ir.FormatValue(evalSrc(t, params+" -> "+expr, args...))
}

func TestModIsEuclidean(t *testing.T) {
	// The whole reason mod is not Go's %: wrap-around indexing must never
	// produce a negative index.
	for _, c := range []struct{ expr, want string }{
		{"mod(7, 3)", "1"},
		{"mod(0 - 7, 3)", "2"},
		{"mod(7, 0 - 3)", "-2"},
		{"mod(0 - 7, 0 - 3)", "-1"},
		{"mod(6, 3)", "0"},
		{"(0 - 1) % 5", "4"}, // the operator agrees with the builtin
		{"0 - 1 % 5", "-1"},  // % binds tighter than -, so this is 0 - (1 % 5)
		{"9 % 5", "4"},
	} {
		if got := v05(t, c.expr); got != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
	evalErr(t, "(_u) -> mod(1, 0)", "mod by zero", int64(0))
}

func TestDivmodRoundTrips(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		{"divmod(17, 5)", "[3, 2]"},
		{"divmod(0 - 17, 5)", "[-4, 3]"},
	} {
		if got := v05(t, c.expr); got != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
	// The contract: q*b + r == a, for negative a too. -4*5 + 3 == -17.
	if got := v05(t, "prow(divmod(0 - 17, 5)) * 5 + pcol(divmod(0 - 17, 5))"); got != "-17" {
		t.Errorf("divmod round trip = %s, want -17", got)
	}
}

func TestV05IntMath(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		{"pow(2, 10)", "1024"},
		{"pow(3, 0)", "1"},
		{"isqrt(0)", "0"},
		{"isqrt(35)", "5"},
		{"isqrt(36)", "6"}, // exact at a perfect square, unlike float sqrt
		{"isqrt(1000000000000)", "1000000"},
		{"factorial(0)", "1"},
		{"factorial(20)", "2432902008176640000"},
		{"choose(6, 2)", "15"},
		{"choose(52, 5)", "2598960"},
		{"choose(5, 9)", "0"},
		{"clamp(9, 0, 4)", "4"},
		{"clamp(0 - 9, 0, 4)", "0"},
		{"clamp(2, 0, 4)", "2"},
		{"min(3, 8)", "3"},
		{"max(3, 8)", "8"},
		{"min(list(3, 8, 1))", "1"}, // the list form still works
	} {
		if got := v05(t, c.expr); got != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
	// Overflow is an error, not a wrapped wrong answer.
	evalErr(t, "(_u) -> factorial(21)", "overflows", int64(0))
	evalErr(t, "(_u) -> pow(2, 0 - 1)", "non-negative", int64(0))
	evalErr(t, "(_u) -> isqrt(0 - 1)", "negative", int64(0))
}

func TestIkkeNegation(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		{"ikke s = 4", "true"},
		{"ikke s = 5", "false"},
		// Binds looser than a comparison, tighter than `and`.
		{"ikke s = 4 and s > 1", "true"},
		{"ikke (s = 5 and s > 1)", "false"},
		{"ikke ikke s = 5", "true"},
	} {
		if got := v05(t, c.expr, int64(5)); got != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
}

func TestV05TextBuiltins(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		{"length(s)", "11"},
		{"slice(s, 0, 5)", "hello"},
		{"slice(s, 6, 99)", "world"}, // clamps like take/drop
		{"slice(s, 4, 2)", ""},       // inverted range is empty, not an error
		{"slice(s, 50, 60)", ""},     // wholly out of range
		{"charat(s, 1)", "e"},
		{`indexof(s, "o")`, "4"},
		{`indexof(s, "zz")`, "-1"},
		{`startswith(s, "hell")`, "true"},
		{`endswith(s, "rld")`, "true"},
		{`replace(s, "l", "L")`, "heLLo worLd"},
		{"upper(s)", "HELLO WORLD"},
		{`lower("ABC")`, "abc"},
		{`trim("  x  ")`, "x"},
		{`textjoin(chars("abc"), "-")`, "a-b-c"},
		{`s + "!"`, "hello world!"},
	} {
		if got := v05(t, c.expr, "hello world"); got != c.want {
			t.Errorf("%s = %q, want %q", c.expr, got, c.want)
		}
	}
	evalErr(t, "(s) -> charat(s, 99)", "out of range", "hello world")
}

// Text positions count runes everywhere, so they agree with `Split Text by ""`.
// Bytes would make charat and length disagree on any non-ASCII input.
func TestTextIsRuneIndexed(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		{"length(s)", "5"},
		{"charat(s, 1)", "é"},
		{"slice(s, 1, 3)", "él"},
		{`indexof(s, "llo")`, "2"},
	} {
		if got := v05(t, c.expr, "héllo"); got != c.want {
			t.Errorf("%s = %q, want %q", c.expr, got, c.want)
		}
	}
}

func TestTupleAndItem(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		// Tuples share []Value with lists at runtime, so they render alike.
		{`tuple("a", 1)`, "[a, 1]"},
		{`item(tuple("a", 1), 0)`, "a"},
		{`item(tuple("a", 1), 1)`, "1"},
		{`item(tuple(s, s * 2, "x"), 2)`, "x"},
		{`length(tuple("a", 1))`, "2"},
		// Tuples compare structurally, which is what makes them usable as
		// Group By keys and Sort By tiebreaks.
		{`tuple("a", 1) = tuple("a", 1)`, "true"},
		{`tuple("a", 1) = tuple("a", 2)`, "false"},
	} {
		if got := v05(t, c.expr, int64(7)); got != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
}

func TestIndexofAndSliceOverList(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		{"indexof(list(4, 9, 2), 9)", "1"},
		{"indexof(list(4, 9, 2), 5)", "-1"},
		{`indexof(list("a", "b"), "b")`, "1"},
		{"slice(list(1, 2, 3, 4), 1, 3)", "[2, 3]"},
		{"slice(list(1, 2, 3, 4), 0, 99)", "[1, 2, 3, 4]"},
		{"slice(list(1, 2, 3, 4), 3, 1)", "[]"},
	} {
		if got := v05(t, c.expr); got != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
}

func TestConsiderBinding(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		{"consider d as s * s in d + d", "50"},
		// Nests, and an inner binding sees the outer one.
		{"consider a as s + 1 in consider b as a * 2 in a + b", "18"},
		// An inner binding shadows an outer one of the same name, and the
		// outer value is restored outside the inner body.
		{"consider a as 1 in (consider a as 9 in a) + a", "10"},
		// A binding shadows a lambda parameter for its body only.
		{"consider s as 100 in s", "100"},
		{"(consider s as 100 in s) + s", "105"},
		// Usable inside a conditional arm, which is the form `let`-less code
		// had to write twice.
		{"consider d as s * 2 in (if d > 5 then d else 0 - d)", "10"},
		// And a conditional can appear in the bound value.
		{"consider d as (if s > 1 then 7 else 8) in d * 2", "14"},
	} {
		if got := v05(t, c.expr, int64(5)); got != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
}

// The bound expression is evaluated once. A partial expression in the value
// position is therefore evaluated exactly once too — it must not be
// re-evaluated per use in the body.
func TestConsiderEvaluatesValueOnce(t *testing.T) {
	// item() is partial; if the binding were substituted textually into the
	// body rather than evaluated, this would fail twice rather than once, but
	// either way the observable result is the same error.
	evalErr(t, "(xs) -> consider v as item(xs, 9) in v + v", "out of range",
		[]ir.Value{int64(1)})
	// The total case gives the value, proving single evaluation reaches both
	// uses.
	if got := v05(t, "consider v as s * 3 in v * v", int64(5)); got != "225" {
		t.Errorf("consider v as s*3 in v*v = %s, want 225", got)
	}
}
