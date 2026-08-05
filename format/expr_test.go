package format

import (
	"testing"

	"domain/ast"
	"domain/lexer"
	"domain/parser"
)

// exprOf parses `Using: <src>` and returns the lambda body, so a case can be
// written as the source it is meant to round-trip.
func exprOf(t *testing.T, src string) ast.Expr {
	t.Helper()
	prog := "Cursed Energy: x\nCursed Technique: Apply\n    Using: (a, b, r, xs) -> " + src + "\n"
	toks, err := lexer.Lex(prog)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	p, err := parser.Parse(prog, toks)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return p.Statements[1].Args[0].Value.(ast.LambdaArg).Lambda.Body
}

func TestExprRoundTrips(t *testing.T) {
	// Each of these is already canonical, so rendering the parse of it must
	// give it back — parentheses where precedence needs them and nowhere else.
	for _, src := range []string{
		"a + b",
		"a + b * 2",
		"(a + b) * 2",
		"a - b - 1",
		"a - (b - 1)",
		"a = b and b < 2",
		"(a = b or b = 2) and a > 0",
		"ikke a = b",
		"-a + 1",
		"-(a + 1)",
		"abs(a - b)",
		"min(list(a, b), 2)",
		"r.n + 1",
		"if a > b then a else b",
		"consider d as abs(a - b) in d * d",
		"sum(take(reverse(xs), 2))",
		"a % 3 = 0",
		"length(xs) - 1",
	} {
		if got := Expr(exprOf(t, src)); got != src {
			t.Errorf("Expr(parse(%q)) = %q", src, got)
		}
	}
}

// Rendering is canonical, not original: parentheses the precedence does not
// need are dropped, because the tree does not record them.
func TestExprDropsRedundantParentheses(t *testing.T) {
	cases := map[string]string{
		"(a) + (b)":     "a + b",
		"a + (b * 2)":   "a + b * 2",
		"((a + b))":     "a + b",
		"abs((a * a))":  "abs(a * a)",
		"(a) = (b + 1)": "a = b + 1",
		// The other direction: `if` and `consider` swallow everything to their
		// right, so one sitting under an operator is always bracketed, even
		// where re-parsing would have recovered it anyway. What a reader of a
		// breakdown row needs is to see where the arm ends.
		"1 + if a > b then a else b": "1 + (if a > b then a else b)",
		"1 + consider d as a in d":   "1 + (consider d as a in d)",
	}
	for src, want := range cases {
		if got := Expr(exprOf(t, src)); got != want {
			t.Errorf("Expr(parse(%q)) = %q, want %q", src, got, want)
		}
	}
}

// The rendering must re-parse to the same thing, which is what "the
// parentheses are where they need to be" actually means.
func TestExprReparses(t *testing.T) {
	for _, src := range []string{
		"(a + b) * (a - b)",
		"consider d as (a + b) * 2 in if d > 3 then d - 1 else 0 - d",
		"ikke (a = b and b = 2)",
		"(if a > b then a else b) + 1",
		"0 - (a - b)",
	} {
		first := Expr(exprOf(t, src))
		second := Expr(exprOf(t, first))
		if first != second {
			t.Errorf("%q rendered to %q, which renders to %q", src, first, second)
		}
	}
}

func TestExprRendersLiterals(t *testing.T) {
	cases := map[string]string{
		`"hi" + "there"`: `"hi" + "there"`,
		"a + 3.25":       "a + 3.25",
		"a + -1":         "a + -1",
	}
	for src, want := range cases {
		if got := Expr(exprOf(t, src)); got != want {
			t.Errorf("Expr(parse(%q)) = %q, want %q", src, got, want)
		}
	}
}

// A `Using:` written as an indented pipeline is not an expression, and says so
// rather than rendering as something that looks like one.
func TestExprOnABlockBody(t *testing.T) {
	got := Expr(&ast.BlockBody{Param: "x"})
	if got != "(an indented pipeline body)" {
		t.Errorf("got %q", got)
	}
}
