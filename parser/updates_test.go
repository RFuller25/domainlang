package parser_test

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/format"
	"domain/lexer"
	"domain/parser"
)

// parse lexes and parses a program that is expected to be well formed.
func parse(t *testing.T, src string) *ast.Program {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

// Parsing `:=` and `also` (docs/expressions.md).

// lambdaOf parses a one-statement program and returns its Using: lambda.
func lambdaOf(t *testing.T, body string) *ast.Lambda {
	t.Helper()
	prog := parse(t, "X:\n    Using: "+body+"\n")
	return prog.Statements[0].Args[0].Value.(ast.LambdaArg).Lambda
}

// parseErr parses a program expected to fail, returning the message.
func parseErr(t *testing.T, src string) string {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		return err.Error()
	}
	if _, err = parser.Parse(src, toks); err == nil {
		t.Fatalf("expected a parse error for %q", src)
	}
	return err.Error()
}

// TestAssignShape pins what `:=` binds against, by rendering the tree back to
// source: the renderer parenthesizes exactly where precedence needs it, so a
// round-trip that keeps the shape is the shape being asserted.
func TestAssignShape(t *testing.T) {
	cases := []struct{ src, want string }{
		// Looser than every operator: the whole arithmetic expression is the
		// value written, not just its first operand.
		{"(v) -> n := a + b * 2", "n := a + b * 2"},
		{"(v) -> n := a = b", "n := a = b"},
		{"(v) -> n := a and b", "n := a and b"},
		// Right-associative, so a chain writes the same value to both names.
		{"(v) -> n := m := 3", "n := m := 3"},
		// An operand needs its parentheses back, since := is looser than +.
		{"(v) -> (n := 3) + 1", "(n := 3) + 1"},
		{"(v) -> 1 + (n := 3)", "1 + (n := 3)"},
		// The right-hand side swallows a conditional and a binding whole,
		// exactly as it swallows arithmetic.
		{"(v) -> n := if v > 1 then 2 else 3", "n := if v > 1 then 2 else 3"},
		{"(v) -> n := consider t as 2 in t * 2", "n := consider t as 2 in t * 2"},
		// …and appears inside them without parentheses, because every arm is
		// parsed from the loosest power.
		{"(v) -> if v > 1 then n := 2 else n := 3", "if v > 1 then n := 2 else n := 3"},
		{"(v) -> consider t as n := 2 in t", "consider t as n := 2 in t"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got := format.Expr(lambdaOf(t, c.src).Body)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestAlsoShape pins the clause list: it extends over commas to the end of the
// expression, and everything written into it keeps its own shape.
func TestAlsoShape(t *testing.T) {
	cases := []struct{ src, want string }{
		{"(v) -> v also n := 1", "v also n := 1"},
		{"(v) -> v also n := 1, m := 2", "v also n := 1, m := 2"},
		{"(v) -> v + 1 also n := 1", "v + 1 also n := 1"},
		// Inside a parenthesis, where the closing paren ends the list — the
		// spelling that puts one in the middle of a larger expression.
		{"(v) -> (v also n := 1) + n", "(v also n := 1) + n"},
		{"(v) -> max((v also n := 1), 2)", "max((v also n := 1), 2)"},
		// A nested one has to be parenthesized, and stays that way.
		{"(v) -> (v also n := 1) also n := 2", "(v also n := 1) also n := 2"},
		// The body is a whole expression, however loose.
		{"(v) -> if v > 1 then 2 else 3 also n := 1", "if v > 1 then 2 else 3 also n := 1"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got := format.Expr(lambdaOf(t, c.src).Body)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestAssignTargetMustBeName rejects a left-hand side that is not a name.
// Domain has no lvalues: `:=` writes to a binding, and nothing else is one.
func TestAssignTargetMustBeName(t *testing.T) {
	for _, src := range []string{
		"X:\n    Using: (v) -> 1 := 2\n",
		"X:\n    Using: (v) -> f(v) := 2\n",
		"X:\n    Using: (v) -> v.a := 2\n",
	} {
		if msg := parseErr(t, src); !strings.Contains(msg, "left side of := must be a name") {
			t.Fatalf("got %q", msg)
		}
	}
}

// TestAssignToParameterRefused is the check that lives in the parser because
// the parameter list is written a few characters from the body that writes to
// it — and because the answer is syntactic.
func TestAssignToParameterRefused(t *testing.T) {
	msg := parseErr(t, "X:\n    Using: (x) -> x := 3\n")
	if !strings.Contains(msg, `"x" is a lambda parameter`) {
		t.Fatalf("got %q", msg)
	}
	// Two parameters deep, and inside a nested expression rather than at the
	// top of the body.
	msg = parseErr(t, "X:\n    Using: (a, b) -> a + (b := 2)\n")
	if !strings.Contains(msg, `"b" is a lambda parameter`) {
		t.Fatalf("got %q", msg)
	}
	// …but a `consider` of the same name shadows the parameter, so the write
	// lands on the local and is perfectly legal.
	lam := lambdaOf(t, "(x) -> consider x as 1 in (x := 2)")
	if got, want := format.Expr(lam.Body), "consider x as 1 in x := 2"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestAlsoAmbiguities are the two places an `also` list cannot be read without
// guessing, and both say so instead.
func TestAlsoAmbiguities(t *testing.T) {
	// Inside a call's arguments the clause commas and the argument commas are
	// the same character.
	msg := parseErr(t, "X:\n    Using: (v) -> max(v also n := 1, 2)\n")
	if !strings.Contains(msg, "ambiguous with the argument commas") {
		t.Fatalf("got %q", msg)
	}
	// A second `also` at the same level: the first list is still open.
	msg = parseErr(t, "X:\n    Using: (v) -> v also n := 1 also n := 2\n")
	if !strings.Contains(msg, "already open") {
		t.Fatalf("got %q", msg)
	}
	// An `also` with nothing after it.
	msg = parseErr(t, "X:\n    Using: (v) -> v also\n")
	if msg == "" {
		t.Fatal("expected an error")
	}
}

// TestAlsoIsNotAReservedWord keeps the contextual-keyword promise: `also`,
// like `and`, `ikke` and `consider`, is only an operator in operator position.
func TestAlsoIsNotAReservedWord(t *testing.T) {
	lam := lambdaOf(t, "(also) -> also + 1")
	if got, want := format.Expr(lam.Body), "also + 1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestAssignInBindingValue: a `Consider x As …` value is one of the three
// positions where an `also` list is unambiguous, and it accepts a write like
// any other expression.
func TestAssignInBindingValue(t *testing.T) {
	prog := parse(t, "X:\n    Consider a As 1\n    Consider b As (a := 2) also a := 3\n    Using: (v) -> v + a\n")
	b := prog.Statements[0].Binds[1]
	if got, want := format.Expr(b.Value), "a := 2 also a := 3"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBindingNameAlso refuses `also` as a binding name, for the reason every
// other expression keyword is refused: the name would read as the operator
// everywhere it was used.
func TestBindingNameAlso(t *testing.T) {
	msg := parseErr(t, "X:\n    Consider also As 1\n    Using: (v) -> v\n")
	if !strings.Contains(msg, "expression keyword") {
		t.Fatalf("got %q", msg)
	}
}
