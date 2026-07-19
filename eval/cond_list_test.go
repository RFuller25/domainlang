package eval

import (
	"testing"

	"domain/ir"
)

func TestConditionalSelectsArm(t *testing.T) {
	src := "(n) -> if n > 0 then n * 2 else 0 - n"
	if got := evalSrc(t, src, int64(5)); got != int64(10) {
		t.Fatalf("then arm: got %v want 10", got)
	}
	if got := evalSrc(t, src, int64(-4)); got != int64(4) {
		t.Fatalf("else arm: got %v want 4", got)
	}
}

func TestConditionalArmsAreLazy(t *testing.T) {
	// first(xs) on the empty list errors — but the then-arm must shield it.
	src := "(xs) -> if length(xs) = 0 then -1 else first(xs)"
	if got := evalSrc(t, src, []ir.Value{}); got != int64(-1) {
		t.Fatalf("guarded empty list: got %v want -1", got)
	}
	if got := evalSrc(t, src, []ir.Value{int64(7)}); got != int64(7) {
		t.Fatalf("non-empty list: got %v want 7", got)
	}
}

func TestConditionalNests(t *testing.T) {
	src := "(n) -> if n < 0 then \"neg\" else if n = 0 then \"zero\" else \"pos\""
	for n, want := range map[int64]string{-3: "neg", 0: "zero", 9: "pos"} {
		if got := evalSrc(t, src, n); got != want {
			t.Errorf("n=%d: got %v want %q", n, got, want)
		}
	}
}

func TestListBuiltin(t *testing.T) {
	got := evalSrc(t, "(a, b) -> list(a, b, a + b)", int64(2), int64(3))
	if !ir.DeepEqual(got, []ir.Value{int64(2), int64(3), int64(5)}) {
		t.Fatalf("list = %v", got)
	}
}

func TestSetBuiltin(t *testing.T) {
	xs := []ir.Value{int64(1), int64(2), int64(3)}
	got := evalSrc(t, "(xs) -> set(xs, 1, 99)", xs)
	if !ir.DeepEqual(got, []ir.Value{int64(1), int64(99), int64(3)}) {
		t.Fatalf("set = %v", got)
	}
	// Functional: the input list is unchanged.
	if xs[1] != int64(2) {
		t.Fatal("set must not mutate its input")
	}
	evalErr(t, "(xs) -> set(xs, 5, 0)", "index 5 out of range", xs)
}

func TestRowColRowsColsBuiltins(t *testing.T) {
	g := ir.NewGridValue(2, 3)
	for i := range g.Cells {
		g.Cells[i] = int64(i) // 0 1 2 / 3 4 5
	}
	if got := evalSrc(t, "(g) -> row(g, 1)", g); !ir.DeepEqual(got,
		[]ir.Value{int64(3), int64(4), int64(5)}) {
		t.Fatalf("row = %v", got)
	}
	if got := evalSrc(t, "(g) -> col(g, 2)", g); !ir.DeepEqual(got,
		[]ir.Value{int64(2), int64(5)}) {
		t.Fatalf("col = %v", got)
	}
	if got := evalSrc(t, "(g) -> rows(g) * 10 + cols(g)", g); got != int64(23) {
		t.Fatalf("rows/cols = %v want 23", got)
	}
	evalErr(t, "(g) -> row(g, 2)", "row 2 out of range", g)
	evalErr(t, "(g) -> col(g, 3)", "column 3 out of range", g)
}
