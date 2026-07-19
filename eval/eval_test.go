package eval

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
)

func parseLambda(t *testing.T, lambdaSrc string) *ast.Lambda {
	t.Helper()
	src := "X:\n    Using: " + lambdaSrc + "\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog.Statements[0].Args[0].Value.(ast.LambdaArg).Lambda
}

// TestExpressionLayer evaluates the canonical 2020 D1 predicate lambda.
func TestExpressionLayer(t *testing.T) {
	lam := parseLambda(t, "(a, b) -> a + b = 2020")

	got, err := EvalLambda(lam, int64(1000), int64(1020))
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Fatalf("1000 + 1020 = 2020 should be true, got %v", got)
	}

	got, err = EvalLambda(lam, int64(1), int64(2))
	if err != nil {
		t.Fatal(err)
	}
	if got != false {
		t.Fatalf("1 + 2 = 2020 should be false, got %v", got)
	}
}

func TestLogicalConnectives(t *testing.T) {
	// Mirrors the 2022 D4 containment predicate shape.
	lam := parseLambda(t, "(a, b, c, d) -> (a <= c and b >= d) or (c <= a and d >= b)")
	mk := func(a, b, c, d int64) ir.Value {
		v, err := EvalLambda(lam, a, b, c, d)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if mk(2, 8, 3, 7) != true { // 2-8 contains 3-7
		t.Fatal("expected containment true")
	}
	if mk(2, 4, 6, 8) != false { // disjoint
		t.Fatal("expected disjoint false")
	}

	// Type error: logical operator on non-Bool operands.
	bad := parseLambda(t, "(x) -> x and x")
	if _, err := EvalLambda(bad, int64(1)); err == nil {
		t.Fatal("expected error for `and` on Int operands")
	}
}

func TestArithmeticAndComparison(t *testing.T) {
	lam := parseLambda(t, "(x) -> x * 3 - 1")
	got, err := EvalLambda(lam, int64(5))
	if err != nil {
		t.Fatal(err)
	}
	if got.(int64) != 14 {
		t.Fatalf("5*3-1 = 14, got %v", got)
	}
}

// TestRuntimeFieldAccess covers the new *RecordValue case (records flow from
// Match Pattern in M3).
func TestRuntimeFieldAccess(t *testing.T) {
	rec := ir.NewRecordValue()
	rec.Set("a", int64(2))
	rec.Set("b", int64(4))

	lam := parseLambda(t, "(r) -> r.a <= r.b")
	got, err := EvalLambda(lam, rec)
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Fatalf("2 <= 4 should be true, got %v", got)
	}

	// Unknown field is a runtime error.
	bad := parseLambda(t, "(r) -> r.missing")
	if _, err := EvalLambda(bad, rec); err == nil {
		t.Fatal("expected error for unknown field")
	}

	// Field access on a non-record value is a runtime error.
	if _, err := EvalLambda(bad, int64(3)); err == nil {
		t.Fatal("expected error for field access on Int")
	}
}

// TestEqualityOnComposites is a regression test: `=` used to only compare
// int64/string/bool and silently fall through to false for every other
// value kind, even when the type checker allows `=` on any pair of
// structurally-equal static types (List/Record/Map/Set/Grid).
func TestEqualityOnComposites(t *testing.T) {
	lam := parseLambda(t, "(a, b) -> a = b")

	t.Run("equal lists", func(t *testing.T) {
		a := []ir.Value{int64(1), int64(2), int64(3)}
		b := []ir.Value{int64(1), int64(2), int64(3)}
		got, err := EvalLambda(lam, a, b)
		if err != nil {
			t.Fatal(err)
		}
		if got != true {
			t.Fatalf("identical lists should be =, got %v", got)
		}
	})

	t.Run("unequal lists", func(t *testing.T) {
		a := []ir.Value{int64(1), int64(2), int64(3)}
		b := []ir.Value{int64(1), int64(2), int64(4)}
		got, err := EvalLambda(lam, a, b)
		if err != nil {
			t.Fatal(err)
		}
		if got != false {
			t.Fatalf("different lists should not be =, got %v", got)
		}
	})

	t.Run("equal records", func(t *testing.T) {
		a := ir.NewRecordValue()
		a.Set("x", int64(1))
		b := ir.NewRecordValue()
		b.Set("x", int64(1))
		got, err := EvalLambda(lam, a, b)
		if err != nil {
			t.Fatal(err)
		}
		if got != true {
			t.Fatalf("identical records should be =, got %v", got)
		}
	})

	t.Run("unequal records", func(t *testing.T) {
		a := ir.NewRecordValue()
		a.Set("x", int64(1))
		b := ir.NewRecordValue()
		b.Set("x", int64(2))
		got, err := EvalLambda(lam, a, b)
		if err != nil {
			t.Fatal(err)
		}
		if got != false {
			t.Fatalf("different records should not be =, got %v", got)
		}
	})

	t.Run("mismatched dynamic types", func(t *testing.T) {
		got, err := EvalLambda(lam, int64(1), "1")
		if err != nil {
			t.Fatal(err)
		}
		if got != false {
			t.Fatalf("Int vs Text should not be =, got %v", got)
		}
	})
}

// TestLogicalShortCircuit is a regression test: `and`/`or` used to always
// evaluate both operands, so a right-hand side that errors (e.g. division by
// zero) would fail even when the left-hand side alone already determines the
// result — breaking the standard guard-clause idiom.
func TestLogicalShortCircuit(t *testing.T) {
	t.Run("or short-circuits on true", func(t *testing.T) {
		lam := parseLambda(t, "(n) -> n = 0 or 10 / n = 5")
		got, err := EvalLambda(lam, int64(0))
		if err != nil {
			t.Fatalf("expected short-circuit to avoid division by zero, got error: %v", err)
		}
		if got != true {
			t.Fatalf("n=0 should make the whole expression true via short-circuit, got %v", got)
		}
	})

	t.Run("or evaluates right when left is false", func(t *testing.T) {
		lam := parseLambda(t, "(n) -> n = 0 or 10 / n = 5")
		got, err := EvalLambda(lam, int64(2))
		if err != nil {
			t.Fatal(err)
		}
		if got != true { // 10/2 = 5
			t.Fatalf("expected true (10/2=5), got %v", got)
		}
	})

	t.Run("and short-circuits on false", func(t *testing.T) {
		lam := parseLambda(t, "(n) -> n > 0 and 10 / n = 5")
		got, err := EvalLambda(lam, int64(0))
		if err != nil {
			t.Fatalf("expected short-circuit to avoid division by zero, got error: %v", err)
		}
		if got != false {
			t.Fatalf("n=0 should make the whole expression false via short-circuit, got %v", got)
		}
	})

	t.Run("and evaluates right when left is true", func(t *testing.T) {
		lam := parseLambda(t, "(n) -> n > 0 and 10 / n = 5")
		got, err := EvalLambda(lam, int64(2))
		if err != nil {
			t.Fatal(err)
		}
		if got != true { // 10/2 = 5
			t.Fatalf("expected true, got %v", got)
		}
		got, err = EvalLambda(lam, int64(3))
		if err != nil {
			t.Fatal(err)
		}
		if got != false { // 10/3 = 3, not 5
			t.Fatalf("expected false, got %v", got)
		}
	})

	t.Run("right-side type error still surfaces when evaluated", func(t *testing.T) {
		lam := parseLambda(t, "(n) -> n > 0 and n")
		if _, err := EvalLambda(lam, int64(1)); err == nil {
			t.Fatal("expected a Bool-type error from the right operand when it is actually evaluated")
		}
	})
}

func TestUnaryMinus(t *testing.T) {
	lam := parseLambda(t, "(x) -> -x = -5")
	got, err := EvalLambda(lam, int64(5))
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Fatalf("-5 = -5 should be true, got %v", got)
	}
}

func TestDivisionByZero(t *testing.T) {
	lam := parseLambda(t, "(x) -> 10 / x")
	if _, err := EvalLambda(lam, int64(0)); err == nil {
		t.Fatal("expected division-by-zero error")
	}
}

func TestUnknownIdentifier(t *testing.T) {
	lam := parseLambda(t, "(x) -> y")
	if _, err := EvalLambda(lam, int64(1)); err == nil {
		t.Fatal("expected unknown-identifier error")
	}
}

func TestLambdaArityMismatch(t *testing.T) {
	lam := parseLambda(t, "(a, b) -> a + b")
	if _, err := EvalLambda(lam, int64(1)); err == nil {
		t.Fatal("expected arity-mismatch error for too few arguments")
	}
	if _, err := EvalLambda(lam, int64(1), int64(2), int64(3)); err == nil {
		t.Fatal("expected arity-mismatch error for too many arguments")
	}
}

func TestComparisonOperators(t *testing.T) {
	cases := []struct {
		expr string
		x, y int64
		want bool
	}{
		{"(a,b) -> a < b", 1, 2, true},
		{"(a,b) -> a < b", 2, 1, false},
		{"(a,b) -> a > b", 2, 1, true},
		{"(a,b) -> a <= b", 2, 2, true},
		{"(a,b) -> a >= b", 2, 3, false},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.expr)
		got, err := EvalLambda(lam, c.x, c.y)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("%s with (%d,%d): got %v want %v", c.expr, c.x, c.y, got, c.want)
		}
	}
}

// ints builds a List value from int64s.
func ints(xs ...int64) []ir.Value {
	out := make([]ir.Value, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// TestBuiltinsEval covers the behavior of every expression-layer builtin
// (typecheck.Builtins) through EvalLambda.
func TestBuiltinsEval(t *testing.T) {
	cases := []struct {
		expr string
		arg  ir.Value
		want ir.Value
	}{
		{"(xs) -> length(xs)", ints(4, 5, 6), int64(3)},
		{"(xs) -> length(xs)", ints(), int64(0)},
		{"(xs) -> item(xs, 1)", ints(4, 5, 6), int64(5)},
		{"(xs) -> sum(take(xs, 2))", ints(4, 5, 6), int64(9)},
		{"(xs) -> sum(take(xs, 9))", ints(4, 5, 6), int64(15)}, // take clamps
		{"(xs) -> sum(take(xs, -1))", ints(4, 5, 6), int64(0)}, // to [0, len]
		{"(xs) -> sum(drop(xs, 1))", ints(4, 5, 6), int64(11)},
		{"(xs) -> sum(drop(xs, 9))", ints(4, 5, 6), int64(0)},
		{"(xs) -> first(reverse(xs))", ints(4, 5, 6), int64(6)},
		{"(xs) -> length(concat(xs, xs))", ints(4, 5), int64(4)},
		{"(xs) -> item(concat(xs, xs), 2)", ints(4, 5), int64(4)},
		{"(xs) -> first(xs)", ints(4, 5, 6), int64(4)},
		{"(xs) -> last(xs)", ints(4, 5, 6), int64(6)},
		{"(xs) -> sum(xs)", ints(), int64(0)}, // sum is total
		{"(xs) -> min(xs)", ints(5, 2, 9), int64(2)},
		{"(xs) -> max(xs)", ints(5, 2, 9), int64(9)},
		{"(xs) -> contains(xs, 5)", ints(4, 5, 6), true},
		{"(xs) -> contains(xs, 7)", ints(4, 5, 6), false},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.expr)
		got, err := EvalLambda(lam, c.arg)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		if !ir.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.expr, got, c.want)
		}
	}
}

func TestBuiltinGetAndAt(t *testing.T) {
	m := ir.NewMapValue()
	m.Put(int64(1), ints(7, 8))
	lam := parseLambda(t, "(m) -> sum(get(m, 1))")
	got, err := EvalLambda(lam, m)
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(15) {
		t.Fatalf("get: got %v want 15", got)
	}

	g := ir.NewGridValue(2, 3)
	g.SetAt(1, 2, int64(42))
	lam = parseLambda(t, "(g) -> at(g, 1, 2)")
	got, err = EvalLambda(lam, g)
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(42) {
		t.Fatalf("at: got %v want 42", got)
	}
}

// TestBuiltinErrors covers the partial builtins' failure paths; wording is
// shared with the compiled backend's dm* helpers.
func TestBuiltinErrors(t *testing.T) {
	cases := []struct {
		expr string
		arg  ir.Value
		want string
	}{
		{"(xs) -> item(xs, 5)", ints(1, 2), "out of range"},
		{"(xs) -> item(xs, -1)", ints(1, 2), "out of range"},
		{"(xs) -> first(xs)", ints(), "first of an empty list"},
		{"(xs) -> last(xs)", ints(), "last of an empty list"},
		{"(xs) -> min(xs)", ints(), "min of an empty list"},
		{"(xs) -> max(xs)", ints(), "max of an empty list"},
		{"(m) -> get(m, 9)", ir.NewMapValue(), "no key 9"},
		{"(g) -> at(g, 5, 0)", ir.NewGridValue(2, 2), "out of range"},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.expr)
		_, err := EvalLambda(lam, c.arg)
		if err == nil {
			t.Errorf("%s: expected an error containing %q", c.expr, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not contain %q", c.expr, err, c.want)
		}
	}
}

// TestSumEmptyListStaticType — sum of a runtime-empty list cannot infer its
// result type from the elements, so the statically inferred element type
// (EvalLambdaTyped) must decide, matching the compiled backend where the
// zero value always carries the slice's element type. Without static types
// the dynamic fallback keeps the historical Int behavior.
func TestSumEmptyListStaticType(t *testing.T) {
	lam := parseLambda(t, "(xs) -> sum(xs)")
	empty := ir.Value([]ir.Value{})

	got, err := EvalLambdaTyped(lam, []*ir.Type{ir.List(ir.Float())}, empty)
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := got.(float64); !ok || f != 0 {
		t.Fatalf("sum of empty List<Float> = %T %v, want float64 0", got, got)
	}

	got, err = EvalLambdaTyped(lam, []*ir.Type{ir.List(ir.Int())}, empty)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := got.(int64); !ok || n != 0 {
		t.Fatalf("sum of empty List<Int> = %T %v, want int64 0", got, got)
	}

	// Untyped fallback: no static information, empty list defaults to Int.
	got, err = EvalLambda(lam, empty)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := got.(int64); !ok || n != 0 {
		t.Fatalf("untyped sum of empty list = %T %v, want int64 0", got, got)
	}

	// Non-empty float lists keep summing as Float on every path.
	got, err = EvalLambdaTyped(lam, []*ir.Type{ir.List(ir.Float())}, ir.Value([]ir.Value{1.5, 2.0}))
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := got.(float64); !ok || f != 3.5 {
		t.Fatalf("typed sum of [1.5, 2.0] = %T %v, want float64 3.5", got, got)
	}
	got, err = EvalLambda(lam, ir.Value([]ir.Value{1.5, 2.0}))
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := got.(float64); !ok || f != 3.5 {
		t.Fatalf("untyped sum of [1.5, 2.0] = %T %v, want float64 3.5", got, got)
	}

	// The static type flows through subexpressions too: an Int literal
	// divided by a Float sum must use float division even when the list is
	// runtime-empty (the observable divergence the compiled backend always
	// had right).
	div := parseLambda(t, "(xs) -> 7 / (sum(xs) + 2)")
	got, err = EvalLambdaTyped(div, []*ir.Type{ir.List(ir.Float())}, empty)
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := got.(float64); !ok || f != 3.5 {
		t.Fatalf("7 / (sum(empty Float) + 2) = %T %v, want float64 3.5", got, got)
	}
}
