package eval

import (
	"strings"
	"testing"
	"time"
)

// TestFoldBudgetRefusesLargeBuilds pins what a constant fold will not do. Every
// one of these is a legal expression with a well-defined answer; what none of
// them may do is spend an editor's memory computing it while someone is still
// typing the line. `fill(1 << 40, 0)` is the sharp one: it asks for sixteen
// terabytes, and Go answers an allocation it cannot serve by killing the
// process outright — no recover, no diagnostic, just a language server that
// stopped existing.
func TestFoldBudgetRefusesLargeBuilds(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"fill(1099511627776, 0)", "more than can be built"},
		{"length(range(0, 100000000))", "more than can be built"},
		{"length(repeat(\"ab\", 100000000))", "more than can be built"},
		{"length(padleft(\"1\", 100000000, \"0\"))", "more than can be built"},
		{"length(divisors(9223372036854775783))", "too large to factor"},
	}
	for _, c := range cases {
		lam := parseLambda(t, "(q) -> "+c.expr)
		start := time.Now()
		_, err := EvalConst(lam.Body)
		if err == nil {
			t.Errorf("%s: folded instead of declining", c.expr)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %v, want %q", c.expr, err, c.want)
		}
		// The budget is about cost, so the refusal has to be cheap: an editor
		// that stalls for ten seconds per keystroke is the thing being fixed.
		if d := time.Since(start); d > time.Second {
			t.Errorf("%s: took %v to decline", c.expr, d)
		}
	}
}

// TestFoldBudgetKeepsOrdinaryConstants covers the other side: the constants
// worth folding are all small, and every one of them still folds.
func TestFoldBudgetKeepsOrdinaryConstants(t *testing.T) {
	cases := []struct {
		expr string
		want any
	}{
		{"5", int64(5)},
		{"2 + 3 * 4", int64(14)},
		{"length(range(0, 1000))", int64(1000)},
		{"sum(range(1, 101))", int64(5050)},
		{"length(fill(100, 0))", int64(100)},
		{"repeat(\"ab\", 3)", "ababab"},
		{"padleft(\"7\", 4, \"0\")", "0007"},
		{"length(divisors(1000000))", int64(49)},
		{"toint(\"42\") * 2", int64(84)},
	}
	for _, c := range cases {
		lam := parseLambda(t, "(q) -> "+c.expr)
		got, err := EvalConst(lam.Body)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %v, want %v", c.expr, got, c.want)
		}
	}
}

// TestRunTimeKeepsTheFullLimits is the promise the budget rests on: it applies
// to folding only. The same expression, evaluated where a program actually
// runs, builds what it was asked for.
func TestRunTimeKeepsTheFullLimits(t *testing.T) {
	lam := parseLambda(t, "(q) -> length(range(0, 200000))")
	got, err := EvalExpr(lam.Body, nil)
	if err != nil {
		t.Fatalf("run-time evaluation refused a build the budget only bounds while folding: %v", err)
	}
	if got != int64(200000) {
		t.Fatalf("got %v, want 200000", got)
	}
}

// TestFoldBudgetIsReleased pins that the mode is scoped to the fold: a fold
// that fails, or one that panics on its way out, must not leave the evaluator
// budgeted for the run that follows.
func TestFoldBudgetIsReleased(t *testing.T) {
	lam := parseLambda(t, "(q) -> fill(1099511627776, 0)")
	if _, err := EvalConst(lam.Body); err == nil {
		t.Fatal("expected the budget to decline this")
	}
	if folding {
		t.Fatal("still folding after EvalConst returned")
	}
	func() {
		defer func() { _ = recover() }()
		defer func() {
			if folding {
				t.Error("still folding after a panic unwound through EvalConst")
			}
		}()
		_, _ = EvalConst(nil) // an expression of no kind at all
	}()
}
