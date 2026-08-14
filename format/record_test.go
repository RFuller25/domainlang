package format

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/token"
)

// Record literals survive `domain fmt`.
//
// There are two renderers and both have to agree that a braced call is written
// with braces: the token-level normalizer that `domain fmt` runs (needsSpace),
// and the AST renderer behind Expr (renderBraced), which the REPL and the
// diagnostics reach for. A literal that formatted one way and rendered the
// other would be two spellings of one expression.

func TestRecordLiteralFormatting(t *testing.T) {
	head := "Cursed Energy: x\nCursed Technique: Map Each\n    Using: (n) -> "
	for _, c := range []struct{ name, in, want string }{
		{"braces hug their fields", "{a:1,b:2}", "{a: 1, b: 2}"},
		{"interior spacing normalized", "{ a :  1 ,   b : 2 }", "{a: 1, b: 2}"},
		{"operators inside a field", "{v:n*2+1}", "{v: n * 2 + 1}"},
		{"nested literal", "{q:{z:1}}", "{q: {z: 1}}"},
		{"field access after a literal", "{q:1}.q", "{q: 1}.q"},
		{"literal as a call argument", "totext({q:1}.q)", "totext({q: 1}.q)"},
		// A written record() call is a different spelling of the same node and
		// is given back as written — the flag is what tells them apart.
		{"written record call is untouched", `record("a", 1)`, `record("a", 1)`},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := mustFormat(t, head+c.in+"\nReveal: stdout\n")
			want := head + c.want + "\nReveal: stdout\n"
			if got != want {
				t.Errorf("Format:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestRecordLiteralFormattingIsIdempotent(t *testing.T) {
	src := "Cursed Energy: x\nCursed Technique: Map Each\n    Using: (n) -> { a : 1 , b : {c:n} }\nReveal: stdout\n"
	once := mustFormat(t, src)
	twice := mustFormat(t, once)
	if once != twice {
		t.Errorf("not idempotent:\n once: %q\ntwice: %q", once, twice)
	}
	if !strings.Contains(once, "{a: 1, b: {c: n}}") {
		t.Errorf("unexpected rendering: %q", once)
	}
}

// The AST renderer's half.
func TestExprRendersRecordLiterals(t *testing.T) {
	for _, src := range []string{
		"{a: 1}",
		"{a: 1, b: 2}",
		"{a: r.n + 1}",
		"{a: {b: 1}}",
		"{a: 1}.a",
		`record("a", 1)`,
	} {
		if got := Expr(exprOf(t, src)); got != src {
			t.Errorf("Expr(parse(%q)) = %q", src, got)
		}
	}
}

// Braced is presentation only, so nothing is obliged to maintain it across a
// rebuild. A call carrying the flag but not the shape the syntax requires must
// fall back to record(...) rather than emit source that will not parse —
// formatting has to give back something valid whatever it is handed.
func TestRenderBracedFallsBackWhenTheShapeIsWrong(t *testing.T) {
	rec := func(args ...ast.Expr) *ast.CallExpr {
		return &ast.CallExpr{Fn: &ast.Ident{Name: "record"}, Args: args, Braced: true}
	}
	str := func(s string) ast.Expr { return &ast.StringLit{Value: s} }
	num := func(n int64) ast.Expr { return &ast.IntLit{Value: n} }

	for _, c := range []struct {
		name string
		call *ast.CallExpr
	}{
		{"odd argument count", rec(str("a"))},
		{"no arguments", rec()},
		{"field name is not a literal", rec(&ast.Ident{Name: "a"}, num(1))},
		{"field name is not an identifier", rec(str("has space"), num(1))},
		{"field name is empty", rec(str(""), num(1))},
		{"field name starts with a digit", rec(str("1a"), num(1))},
		{"callee is not record", &ast.CallExpr{
			Fn: &ast.Ident{Name: "tuple"}, Args: []ast.Expr{str("a"), num(1)}, Braced: true,
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Expr(c.call)
			if strings.HasPrefix(got, "{") {
				t.Errorf("rendered as a literal (%q); it cannot be parsed back", got)
			}
		})
	}
}

// needsSpace is shared by every token pair, so the brace rules must not change
// how a parenthesis or a comma is spaced.
func TestBraceSpacingDoesNotDisturbOtherPunctuation(t *testing.T) {
	tok := func(k token.Kind) token.Token { return token.Token{Kind: k} }
	if needsSpace(tok(token.LBRACE), tok(token.IDENT), token.Token{}) {
		t.Error("space after {")
	}
	if needsSpace(tok(token.INT), tok(token.RBRACE), token.Token{}) {
		t.Error("space before }")
	}
	if !needsSpace(tok(token.ARROW), tok(token.LBRACE), token.Token{}) {
		t.Error("no space before { after an arrow")
	}
	if needsSpace(tok(token.RBRACE), tok(token.DOT), token.Token{}) {
		t.Error("space before . after }")
	}
	if needsSpace(tok(token.RBRACE), tok(token.RPAREN), token.Token{}) {
		t.Error("space before ) after }")
	}
}
