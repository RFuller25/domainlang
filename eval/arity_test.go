package eval

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// call builds `name(args...)` directly, so a test can write a call the parser
// would accept but no typed program ever reaches — which is the situation
// constant folding puts the evaluator in.
func call(name string, args ...ast.Expr) *ast.CallExpr {
	pos := token.Position{Line: 1, Col: 1}
	return &ast.CallExpr{Fn: &ast.Ident{Name: name, Pos: pos}, Args: args, Pos: pos}
}

func intLit(v int64) ast.Expr {
	return &ast.IntLit{Value: v, Pos: token.Position{Line: 1, Col: 1}}
}

// TestCallArityIsCheckedBeforeArguments pins the fix for the language server
// crash: `range(5)` reached the evaluator through prims.foldLiteral, before
// anything had counted its arguments, and the builtin read args[1] of a
// one-element slice.
func TestCallArityIsChecked(t *testing.T) {
	cases := []struct {
		expr *ast.CallExpr
		want string
	}{
		{call("range", intLit(5)), "range takes 2 argument(s), got 1"},
		{call("abs"), "abs takes 1 argument(s), got 0"},
		{call("modpow", intLit(1), intLit(2)), "modpow takes 3 argument(s), got 2"},
		{call("length", intLit(1), intLit(2)), "length takes 1 argument(s), got 2"},
		{call("list"), "list takes at least 1 argument(s), got 0"},
		{call("insert", intLit(1)), "insert takes 2 or 3 argument(s), got 1"},
	}
	for _, c := range cases {
		_, err := EvalExpr(c.expr, nil)
		if err == nil {
			t.Errorf("%s: expected an arity error, got none", c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("expected %q, got %v", c.want, err)
		}
	}
}

// TestNoBuiltinPanicsOnAnyArgumentCount sweeps every builtin at every argument
// count it might be written with, over a few value shapes. Each one may fail —
// most will — but none may panic: the evaluator answers an editor asking about
// half-written source, where a wrong call is the normal case, not a bug.
func TestNoBuiltinPanicsOnAnyArgumentCount(t *testing.T) {
	shapes := map[string]ast.Expr{
		"int":  intLit(2),
		"text": &ast.StringLit{Value: "ab", Pos: token.Position{Line: 1, Col: 1}},
		"list": call("list", intLit(1), intLit(2)),
	}
	for _, name := range typecheck.Builtins {
		for shape, arg := range shapes {
			for n := range 5 {
				args := make([]ast.Expr, n)
				for i := range args {
					args[i] = arg
				}
				func() {
					defer func() {
						if p := recover(); p != nil {
							t.Errorf("%s with %d %s argument(s) panicked: %v", name, n, shape, p)
						}
					}()
					// The result is beside the point; not crashing is the test.
					_, _ = EvalExpr(call(name, args...), nil)
				}()
			}
		}
	}
}

// TestCorrectArityStillEvaluates guards the check from being over-eager: the
// calls a program actually writes must go through untouched, variadics
// included.
func TestCorrectArityStillEvaluates(t *testing.T) {
	cases := []struct {
		expr *ast.CallExpr
		want ir.Value
	}{
		{call("abs", intLit(-3)), int64(3)},
		{call("gcd", intLit(12), intLit(18)), int64(6)},
		{call("min", intLit(4), intLit(7)), int64(4)},
		{call("length", call("list", intLit(1), intLit(2), intLit(3))), int64(3)},
	}
	for _, c := range cases {
		got, err := EvalExpr(c.expr, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			continue
		}
		if got != c.want {
			t.Errorf("got %v, want %v", got, c.want)
		}
	}
}
