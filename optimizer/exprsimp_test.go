package optimizer

import (
	"testing"

	"domain/ast"
	"domain/eval"
	"domain/token"
)

func intL(v int64) *ast.IntLit                        { return &ast.IntLit{Value: v} }
func ident(n string) *ast.Ident                       { return &ast.Ident{Name: n} }
func bin(op token.Kind, l, r ast.Expr) *ast.BinaryExpr { return &ast.BinaryExpr{Op: op, Left: l, Right: r} }

// simplifyOnce runs the rewriter over an expression tree.
func simplifyOnce(e ast.Expr) (ast.Expr, bool) {
	s := &simplifier{}
	out := s.simplify(e)
	return out, len(s.kinds) > 0
}

// TestSimplifyShapes pins the shape each rule family produces.
func TestSimplifyShapes(t *testing.T) {
	cases := []struct {
		name string
		in   ast.Expr
		want func(e ast.Expr) bool
	}{
		{"fold add", bin(token.PLUS, intL(2), intL(3)),
			func(e ast.Expr) bool { l, ok := e.(*ast.IntLit); return ok && l.Value == 5 }},
		{"fold truncated division", bin(token.SLASH, intL(7), intL(2)),
			func(e ast.Expr) bool { l, ok := e.(*ast.IntLit); return ok && l.Value == 3 }},
		{"fold negative division like eval", bin(token.SLASH, intL(-7), intL(2)),
			func(e ast.Expr) bool { l, ok := e.(*ast.IntLit); return ok && l.Value == -3 }},
		{"fold comparison to bool", bin(token.LT, intL(2), intL(3)),
			func(e ast.Expr) bool { b, ok := e.(*ast.BoolLit); return ok && b.Value }},
		{"fold string equality", bin(token.EQ, &ast.StringLit{Value: "a"}, &ast.StringLit{Value: "b"}),
			func(e ast.Expr) bool { b, ok := e.(*ast.BoolLit); return ok && !b.Value }},
		{"fold unary minus", &ast.UnaryExpr{Op: token.MINUS, X: intL(4)},
			func(e ast.Expr) bool { l, ok := e.(*ast.IntLit); return ok && l.Value == -4 }},
		{"x plus zero", bin(token.PLUS, ident("x"), intL(0)),
			func(e ast.Expr) bool { i, ok := e.(*ast.Ident); return ok && i.Name == "x" }},
		{"zero plus x", bin(token.PLUS, intL(0), ident("x")),
			func(e ast.Expr) bool { _, ok := e.(*ast.Ident); return ok }},
		{"x times one", bin(token.STAR, ident("x"), intL(1)),
			func(e ast.Expr) bool { _, ok := e.(*ast.Ident); return ok }},
		{"x div one", bin(token.SLASH, ident("x"), intL(1)),
			func(e ast.Expr) bool { _, ok := e.(*ast.Ident); return ok }},
		{"x times zero (total operand)", bin(token.STAR, ident("x"), intL(0)),
			func(e ast.Expr) bool { l, ok := e.(*ast.IntLit); return ok && l.Value == 0 }},
		{"x minus x", bin(token.MINUS, ident("x"), ident("x")),
			func(e ast.Expr) bool { l, ok := e.(*ast.IntLit); return ok && l.Value == 0 }},
		{"x equals x", bin(token.EQ, ident("x"), ident("x")),
			func(e ast.Expr) bool { b, ok := e.(*ast.BoolLit); return ok && b.Value }},
		{"x less than x", bin(token.LT, ident("x"), ident("x")),
			func(e ast.Expr) bool { b, ok := e.(*ast.BoolLit); return ok && !b.Value }},
		{"true and p", bin(token.AND, bin(token.LT, intL(1), intL(2)), bin(token.GT, ident("x"), intL(3))),
			func(e ast.Expr) bool { b, ok := e.(*ast.BinaryExpr); return ok && b.Op == token.GT }},
		{"p and false with total p", bin(token.AND, bin(token.GT, ident("x"), intL(3)), bin(token.EQ, intL(1), intL(2))),
			func(e ast.Expr) bool { b, ok := e.(*ast.BoolLit); return ok && !b.Value }},
		{"false or p", bin(token.OR, bin(token.GT, intL(1), intL(2)), bin(token.GT, ident("x"), intL(3))),
			func(e ast.Expr) bool { b, ok := e.(*ast.BinaryExpr); return ok && b.Op == token.GT }},
		{"cascade to constant predicate",
			// (x*0 = 1) or (2 < 1)  →  (0 = 1) or false  →  false
			bin(token.OR,
				bin(token.EQ, bin(token.STAR, ident("x"), intL(0)), intL(1)),
				bin(token.LT, intL(2), intL(1))),
			func(e ast.Expr) bool { b, ok := e.(*ast.BoolLit); return ok && !b.Value }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, changed := simplifyOnce(c.in)
			if !changed {
				t.Fatalf("expected a rewrite, got none (%#v)", out)
			}
			if !c.want(out) {
				t.Fatalf("unexpected shape: %#v", out)
			}
		})
	}
}

// TestSimplifyRefusals pins the cases the rules must NOT touch: folding them
// would erase a runtime error the naive pipeline reports.
func TestSimplifyRefusals(t *testing.T) {
	cases := []struct {
		name string
		in   ast.Expr
	}{
		{"division by literal zero", bin(token.SLASH, intL(7), intL(0))},
		{"times zero with failable operand", bin(token.STAR, bin(token.SLASH, intL(10), ident("x")), intL(0))},
		{"and false with failable left", bin(token.AND,
			bin(token.EQ, bin(token.SLASH, intL(10), ident("x")), intL(5)),
			bin(token.EQ, intL(1), intL(2)))},
		{"or true with failable left", bin(token.OR,
			bin(token.EQ, bin(token.SLASH, intL(10), ident("x")), intL(5)),
			bin(token.LT, intL(1), intL(2)))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, _ := simplifyOnce(c.in)
			switch c.name {
			case "division by literal zero":
				if _, isLit := out.(*ast.IntLit); isLit {
					t.Fatalf("÷0 must not fold, got %#v", out)
				}
			default:
				if _, isBool := out.(*ast.BoolLit); isBool {
					t.Fatalf("failable operand must not be erased, got %#v", out)
				}
				if _, isInt := out.(*ast.IntLit); isInt {
					t.Fatalf("failable operand must not be erased, got %#v", out)
				}
			}
		})
	}
}

// TestSimplifyPreservesSemantics evaluates original vs simplified bodies over
// a sweep of environments: values must agree, and a simplification may never
// turn an error into a success or vice versa.
func TestSimplifyPreservesSemantics(t *testing.T) {
	exprs := []ast.Expr{
		bin(token.PLUS, bin(token.STAR, ident("x"), intL(1)), intL(0)),
		bin(token.AND, bin(token.LE, intL(2), intL(2)), bin(token.GT, ident("x"), intL(0))),
		bin(token.OR, bin(token.EQ, ident("x"), ident("x")), bin(token.GT, bin(token.SLASH, intL(1), ident("x")), intL(0))),
		bin(token.EQ, bin(token.MINUS, ident("x"), ident("x")), intL(0)),
		bin(token.STAR, bin(token.PLUS, ident("x"), intL(0)), intL(0)),
		bin(token.SLASH, bin(token.STAR, ident("x"), intL(0)), intL(0)), // (x*0)/0 → 0/0: must still error
		bin(token.AND, bin(token.GT, ident("x"), intL(0)), bin(token.EQ, bin(token.SLASH, intL(6), ident("x")), intL(2))),
		bin(token.PLUS, bin(token.SLASH, intL(9), intL(3)), bin(token.MINUS, intL(1), intL(1))),
	}
	for i, e := range exprs {
		simplified, _ := simplifyOnce(e)
		for x := int64(-4); x <= 4; x++ {
			env := eval.Env{"x": x}
			gotV, gotErr := eval.EvalExpr(simplified, env)
			wantV, wantErr := eval.EvalExpr(e, env)
			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("expr %d at x=%d: error divergence: simplified err=%v, original err=%v", i, x, gotErr, wantErr)
			}
			if wantErr == nil && gotV != wantV {
				t.Fatalf("expr %d at x=%d: value divergence: simplified %v, original %v", i, x, gotV, wantV)
			}
		}
	}
}
