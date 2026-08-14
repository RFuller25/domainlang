package parser_test

import (
	"strings"
	"testing"

	"domain/ast"
)

// Record literals: `{a: 1, b: x}` parses to the `record("a", 1, "b", x)` call
// it stands for, flagged Braced so the formatter can write it back.
//
// The desugaring is the point of these tests as much as the syntax is. Every
// layer below the parser — typecheck, eval, codegen, the optimizer's walkers,
// prims' Shikigami substitution — sees a CallExpr it has handled since
// record() shipped, so none of them needed a case added for this feature.

// recordCall asserts that a lambda body parsed to a braced `record` call and
// returns it.
func recordCall(t *testing.T, body string) *ast.CallExpr {
	t.Helper()
	lam := lambdaOf(t, body)
	call, ok := lam.Body.(*ast.CallExpr)
	if !ok {
		t.Fatalf("%s: body is %T, want *ast.CallExpr", body, lam.Body)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != "record" {
		t.Fatalf("%s: callee is %v, want record", body, call.Fn)
	}
	if !call.Braced {
		t.Errorf("%s: Braced is false; the formatter would write it back as record(...)", body)
	}
	return call
}

func TestRecordLiteralDesugarsToRecordCall(t *testing.T) {
	call := recordCall(t, `(x) -> {a: 1, b: x}`)
	if len(call.Args) != 4 {
		t.Fatalf("args = %d, want 4 (two name/value pairs)", len(call.Args))
	}
	for i, want := range []string{"a", "b"} {
		lit, ok := call.Args[i*2].(*ast.StringLit)
		if !ok {
			t.Fatalf("arg %d is %T, want *ast.StringLit", i*2, call.Args[i*2])
		}
		if lit.Value != want {
			t.Errorf("field %d = %q, want %q", i, lit.Value, want)
		}
	}
	if _, ok := call.Args[1].(*ast.IntLit); !ok {
		t.Errorf("value 1 is %T, want *ast.IntLit", call.Args[1])
	}
	if id, ok := call.Args[3].(*ast.Ident); !ok || id.Name != "x" {
		t.Errorf("value 2 is %v, want the ident x", call.Args[3])
	}
}

// A hand-written record() call is the same node without the flag, so the
// formatter gives each spelling back as it was written.
func TestHandWrittenRecordCallIsNotBraced(t *testing.T) {
	lam := lambdaOf(t, `(x) -> record("a", 1)`)
	call, ok := lam.Body.(*ast.CallExpr)
	if !ok {
		t.Fatalf("body is %T", lam.Body)
	}
	if call.Braced {
		t.Error("a written record(...) call came back Braced")
	}
}

// Field values are full expressions, parsed at the lowest binding power so an
// operator expression reads whole rather than stopping at the comma.
func TestRecordLiteralFieldValuesAreWholeExpressions(t *testing.T) {
	call := recordCall(t, `(x) -> {sum: 1 + 2 * 3, cmp: x = 1 and x > 0}`)
	if len(call.Args) != 4 {
		t.Fatalf("args = %d, want 4", len(call.Args))
	}
	if _, ok := call.Args[1].(*ast.BinaryExpr); !ok {
		t.Errorf("first value is %T, want the whole *ast.BinaryExpr", call.Args[1])
	}
	if _, ok := call.Args[3].(*ast.BinaryExpr); !ok {
		t.Errorf("second value is %T, want the whole *ast.BinaryExpr", call.Args[3])
	}
}

// parsePrimary is reached through parsePostfix, so `.field` after a literal
// needs no work of its own — but it is exactly the kind of thing that breaks
// silently if the case is ever moved.
func TestRecordLiteralTakesPostfixFieldAccess(t *testing.T) {
	lam := lambdaOf(t, `(x) -> {a: 1}.a`)
	fa, ok := lam.Body.(*ast.FieldAccess)
	if !ok {
		t.Fatalf("body is %T, want *ast.FieldAccess", lam.Body)
	}
	if fa.Field != "a" {
		t.Errorf("field = %q, want a", fa.Field)
	}
	if _, ok := fa.Target.(*ast.CallExpr); !ok {
		t.Errorf("target is %T, want the record call", fa.Target)
	}
}

func TestRecordLiteralNests(t *testing.T) {
	call := recordCall(t, `(x) -> {outer: {inner: 1}}`)
	inner, ok := call.Args[1].(*ast.CallExpr)
	if !ok {
		t.Fatalf("nested value is %T, want *ast.CallExpr", call.Args[1])
	}
	if !inner.Braced {
		t.Error("the nested literal lost its Braced flag")
	}
}

func TestRecordLiteralErrors(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"empty", `(x) -> {}`, "an empty record has no fields"},
		// The reservation for map/set literals: a generic "expected IDENT"
		// would not tell the reader why the obvious spelling is refused.
		{"int key", `(x) -> {1: 2}`, "not (yet) map or set literals"},
		{"string key", `(x) -> {"a": 2}`, "not (yet) map or set literals"},
		{"missing colon", `(x) -> {a 1}`, "needs a colon before its value"},
		{"duplicate field", `(x) -> {a: 1, a: 2}`, `duplicate field "a"`},
		{"trailing comma", `(x) -> {a: 1,}`, "no trailing comma"},
		{"unclosed", `(x) -> {a: 1`, "expected , or }"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := parseErr(t, "X:\n    Using: "+c.body+"\n")
			if !strings.Contains(got, c.want) {
				t.Errorf("error %q does not contain %q", got, c.want)
			}
		})
	}
}

// The duplicate-field message names where the first one was, which the
// typechecker's own duplicate check (recordType) cannot do — it only sees the
// desugared call.
func TestRecordLiteralDuplicateNamesTheFirstField(t *testing.T) {
	got := parseErr(t, "X:\n    Using: (x) -> {a: 1, b: 2, a: 3}\n")
	if !strings.Contains(got, "already given at") {
		t.Errorf("error %q does not point at the first field", got)
	}
}

// A literal may span lines, and does so through the mechanism that was already
// there: an argument written across several lines has its layout tokens spliced
// out before the expression is parsed (joinArgContinuation), which is what lets
// a long record(...) call be broken up today. Braces deliberately do not
// suspend layout in the lexer — they did not need to.
func TestRecordLiteralSpansLinesByArgumentContinuation(t *testing.T) {
	call := recordCall(t, "(x) -> {a: 1,\n        b: 2}")
	if len(call.Args) != 4 {
		t.Fatalf("args = %d, want 4 — the continuation line was not joined", len(call.Args))
	}
	for i, want := range []string{"a", "b"} {
		lit, ok := call.Args[i*2].(*ast.StringLit)
		if !ok || lit.Value != want {
			t.Errorf("field %d = %v, want %q", i, call.Args[i*2], want)
		}
	}
}
