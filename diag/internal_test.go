package diag

import (
	"strings"
	"testing"
)

// miscountedCall is the language server crash, reduced: a `Consider … As`
// binding is constant-folded before it is typed, so a call written with the
// wrong number of arguments used to reach the evaluator with nothing having
// counted them, and take the whole process down with it. An editor asks for
// this on every keystroke — `range(5)` is what `range(5, 10)` looks like
// halfway through being typed.
func miscountedCall(expr string) string {
	return `Cursed Energy: input.txt
Shikigami: Lines
Channeled Energy: Convert List to Integers
Maximum Technique: Count Matching
    Consider n As ` + expr + `
    Using: (x) -> x > n
Reveal: stdout
`
}

func TestMiscountedCallInBindingIsReported(t *testing.T) {
	r := analyze(t, miscountedCall("range(5)"))
	d := diagWith(r, Error, "range takes 2 argument(s), got 1")
	if d == nil {
		t.Fatalf("no arity diagnostic in %v", r.Diags)
	}
	if d.Pos.Line != 5 {
		t.Errorf("diagnostic on line %d, want 5", d.Pos.Line)
	}
}

// TestMiscountedCallShapes walks the same path with the other ways a call
// comes out miscounted — too few, too many, and both kinds of variadic bound —
// and asks that each is an ordinary reported error rather than a crash the
// guard in Analyze had to catch.
func TestMiscountedCallShapes(t *testing.T) {
	for _, expr := range []string{
		"range(5)", "range(1, 2, 3)", "abs()", "modpow(2, 3)",
		"list()", "insert(1)", "point(1)", "tuple(1)", "solve2x2(1, 2)",
	} {
		r := analyze(t, miscountedCall(expr))
		if len(r.Diags) == 0 {
			t.Errorf("%s: expected a diagnostic, got none", expr)
		}
		for _, d := range r.Diags {
			if strings.Contains(d.Msg, "internal error") {
				t.Errorf("%s: %s", expr, d.Msg)
			}
		}
	}
}
