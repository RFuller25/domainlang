package typecheck

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
)

// parseLambda lexes/parses a `Name:` arg line wrapping a lambda and returns it.
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

func TestSumPairPredicateTypesToBool(t *testing.T) {
	// The canonical 2020 D1 lambda: its body is a Bool, given Int params.
	lam := parseLambda(t, "(a, b) -> a + b = 2020")
	got, err := LambdaType(lam, ir.Int(), ir.Int())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(ir.Bool()) {
		t.Fatalf("expected Bool, got %s", got)
	}
}

func TestArithmeticLambdaTypesToInt(t *testing.T) {
	lam := parseLambda(t, "(x) -> x * 2 + 1")
	got, err := LambdaType(lam, ir.Int())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(ir.Int()) {
		t.Fatalf("expected Int, got %s", got)
	}
}

func TestComparisonTypesToBool(t *testing.T) {
	env := Env{"n": ir.Int()}
	for _, src := range []string{"n > 0", "n <= 5", "n = 3"} {
		lam := parseLambda(t, "(n) -> "+src)
		got, err := ExprType(lam.Body, env)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if !got.Equal(ir.Bool()) {
			t.Fatalf("%s: expected Bool, got %s", src, got)
		}
	}
}

func TestLogicalConnectivesType(t *testing.T) {
	rec := ir.Record(
		ir.Field{Name: "a", Type: ir.Int()}, ir.Field{Name: "b", Type: ir.Int()},
		ir.Field{Name: "c", Type: ir.Int()}, ir.Field{Name: "d", Type: ir.Int()},
	)
	lam := parseLambda(t, "(r) -> (r.a <= r.c and r.b >= r.d) or (r.c <= r.a and r.d >= r.b)")
	got, err := LambdaType(lam, rec)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(ir.Bool()) {
		t.Fatalf("expected Bool, got %s", got)
	}

	// `and` on Int operands is a static type error.
	bad := parseLambda(t, "(x) -> x and x")
	if _, err := LambdaType(bad, ir.Int()); err == nil {
		t.Fatal("expected type error for `and` on Int")
	}
}

func TestFieldAccessOnRecord(t *testing.T) {
	rec := ir.Record(ir.Field{Name: "lo", Type: ir.Int()}, ir.Field{Name: "hi", Type: ir.Int()})
	lam := parseLambda(t, "(r) -> r.hi")
	got, err := LambdaType(lam, rec)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(ir.Int()) {
		t.Fatalf("expected Int, got %s", got)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		lambda string
		params []*ir.Type
		want   string
	}{
		{"(a, b) -> a + b", []*ir.Type{ir.Int(), ir.Text()}, "arithmetic needs Int"},
		{"(r) -> r.nope", []*ir.Type{ir.Record(ir.Field{Name: "x", Type: ir.Int()})}, "no field"},
		{"(x) -> x.field", []*ir.Type{ir.Int()}, "non-record"},
		{"(a) -> a = 1", []*ir.Type{ir.Text()}, "different types"},
		{"(a) -> b", []*ir.Type{ir.Int()}, "unknown identifier"},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.lambda)
		_, err := LambdaType(lam, c.params...)
		if err == nil {
			t.Fatalf("%s: expected error", c.lambda)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error %q does not contain %q", c.lambda, err.Error(), c.want)
		}
	}
}

func TestArityMismatch(t *testing.T) {
	lam := parseLambda(t, "(a, b) -> a + b")
	if _, err := LambdaType(lam, ir.Int()); err == nil {
		t.Fatal("expected arity mismatch error")
	}
}

func TestUnaryMinusType(t *testing.T) {
	lam := parseLambda(t, "(x) -> -x")
	got, err := LambdaType(lam, ir.Int())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(ir.Int()) {
		t.Fatalf("got %s want Int", got)
	}
}

func TestUnaryMinusOnNonIntIsAnError(t *testing.T) {
	lam := parseLambda(t, "(x) -> -x")
	if _, err := LambdaType(lam, ir.Text()); err == nil {
		t.Fatal("expected an error for unary minus on Text")
	}
}

func TestLogicalOperatorTypeErrors(t *testing.T) {
	lam := parseLambda(t, "(x) -> x and x")
	if _, err := LambdaType(lam, ir.Int()); err == nil {
		t.Fatal("expected an error for 'and' on non-Bool operands")
	}
}

// TestBuiltinCallTyping covers the happy-path type rule of every entry in
// the Builtins table.
func TestBuiltinCallTyping(t *testing.T) {
	ints := ir.List(ir.Int())
	texts := ir.List(ir.Text())
	nested := ir.List(ir.List(ir.Int()))
	cases := []struct {
		src   string
		param *ir.Type
		want  *ir.Type
	}{
		{"(xs) -> length(xs)", ints, ir.Int()},
		{"(xs) -> length(xs)", nested, ir.Int()},
		{"(xs) -> item(xs, 0)", ints, ir.Int()},
		{"(xs) -> item(xs, 1)", nested, ir.List(ir.Int())},
		{"(xs) -> take(xs, 2)", ints, ints},
		{"(xs) -> drop(xs, 2)", texts, texts},
		{"(xs) -> reverse(xs)", ints, ints},
		{"(xs) -> concat(xs, xs)", ints, ints},
		{"(xs) -> first(xs)", texts, ir.Text()},
		{"(xs) -> last(xs)", ints, ir.Int()},
		{"(xs) -> sum(xs)", ints, ir.Int()},
		{"(xs) -> min(xs)", ints, ir.Int()},
		{"(xs) -> max(xs)", ints, ir.Int()},
		{"(xs) -> contains(xs, 3)", ints, ir.Bool()},
		{"(xs) -> contains(xs, \"a\")", texts, ir.Bool()},
		{"(m) -> get(m, 1)", ir.Map(ir.Int(), ints), ints},
		{"(g) -> at(g, 0, 1)", ir.Grid(ir.Int()), ir.Int()},
		// Builtins compose with each other and with operators.
		{"(xs) -> sum(take(reverse(xs), 2)) + length(xs)", ints, ir.Int()},
		{"(xs) -> item(item(xs, 0), 1) = 5", nested, ir.Bool()},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.src)
		got, err := LambdaType(lam, c.param)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%s: got %s want %s", c.src, got, c.want)
		}
	}
}

// TestBuiltinCallErrors covers the rejection paths: unknown names, arity,
// and argument-type mismatches.
func TestBuiltinCallErrors(t *testing.T) {
	ints := ir.List(ir.Int())
	cases := []struct {
		src   string
		param *ir.Type
		want  string
	}{
		{"(xs) -> frobnicate(xs)", ints, "unknown function"},
		{"(xs) -> length(xs, 1)", ints, "takes 1 argument(s)"},
		{"(x) -> length(x)", ir.Int(), "needs a List"},
		{"(xs) -> item(xs, \"a\")", ints, "must be Int"},
		{"(xs) -> sum(xs)", ir.List(ir.Text()), "needs List<Int>"},
		{"(xs) -> contains(xs, 1)", ir.List(ir.List(ir.Int())), "keyable elements"},
		{"(xs) -> contains(xs, \"a\")", ints, "value must be Int"},
		{"(xs) -> concat(xs, 1)", ints, "same type"},
		{"(m) -> get(m, \"a\")", ir.Map(ir.Int(), ints), "key must be Int"},
		{"(g) -> at(g, 0, 1)", ints, "needs a Grid"},
		{"(xs) -> get(xs, 1)", ints, "needs a Map"},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.src)
		_, err := LambdaType(lam, c.param)
		if err == nil {
			t.Errorf("%s: expected an error containing %q", c.src, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not contain %q", c.src, err, c.want)
		}
	}
}
