package eval

import (
	"strings"
	"testing"

	"domain/ir"
)

// evalSparse runs a lambda with the given arguments, failing the test on error.
func evalSparse(t *testing.T, src string, args ...ir.Value) ir.Value {
	t.Helper()
	lam := parseLambda(t, src)
	v, err := EvalLambda(lam, args...)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return v
}

func TestSparseBuiltinsEval(t *testing.T) {
	sp := ir.NewSparseValue(int64(0))
	sp.Put(2, 3, int64(7))

	if v := evalSparse(t, "(g) -> at(g, 2, 3)", sp); v != int64(7) {
		t.Fatalf("at set = %v", v)
	}
	if v := evalSparse(t, "(g) -> at(g, -9, 9)", sp); v != int64(0) {
		t.Fatalf("at unset = %v (want default)", v)
	}
	if v := evalSparse(t, "(g) -> has(g, 2, 3)", sp); v != true {
		t.Fatalf("has set = %v", v)
	}
	if v := evalSparse(t, "(g) -> has(g, 0, 0)", sp); v != false {
		t.Fatalf("has unset = %v", v)
	}
	if v := evalSparse(t, "(g) -> cells(g)", sp); v != int64(1) {
		t.Fatalf("cells = %v", v)
	}

	// put is functional: the original sparse grid is untouched.
	out := evalSparse(t, "(g) -> put(g, 5, -1, 9)", sp).(*ir.SparseValue)
	if !out.Has(5, -1) || out.Len() != 2 {
		t.Fatal("put did not set the new cell")
	}
	if sp.Has(5, -1) || sp.Len() != 1 {
		t.Fatal("put mutated its input")
	}

	// Bounds accessors over a multi-cell grid.
	if v := evalSparse(t, "(g) -> list(minrow(g), maxrow(g), mincol(g), maxcol(g))", out); ir.FormatValue(v) != "[2, 5, -1, 3]" {
		t.Fatalf("bounds = %s", ir.FormatValue(v))
	}

	// sparse(d) constructs an empty grid whose default is d.
	empty := evalSparse(t, "(d) -> sparse(d)", "#").(*ir.SparseValue)
	if empty.Len() != 0 || empty.Def != "#" {
		t.Fatalf("sparse constructor = %v", ir.FormatValue(empty))
	}
}

func TestToTextEval(t *testing.T) {
	if v := evalSparse(t, "(n) -> totext(n)", int64(-42)); v != "-42" {
		t.Fatalf("totext int = %v", v)
	}
	// Renders like Reveal: shortest round-trip form.
	if v := evalSparse(t, "(f) -> totext(f)", 2.5); v != "2.5" {
		t.Fatalf("totext float = %v", v)
	}
	if v := evalSparse(t, "(f) -> totext(f)", 2.0); v != "2" {
		t.Fatalf("totext whole float = %v", v)
	}
}

func TestSparseBuiltinEvalErrors(t *testing.T) {
	lam := parseLambda(t, "(g) -> minrow(g)")
	_, err := EvalLambda(lam, ir.NewSparseValue(int64(0)))
	if err == nil || !strings.Contains(err.Error(), "minrow of an empty sparse grid is undefined") {
		t.Fatalf("empty minrow error = %v", err)
	}
	lam = parseLambda(t, "(g) -> at(g, 0, 0)")
	if _, err := EvalLambda(lam, int64(3)); err == nil || !strings.Contains(err.Error(), "expected a Grid") {
		t.Fatalf("at on non-grid error = %v", err)
	}
}
