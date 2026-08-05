package eval

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
)

// traceSrc parses `Using: <src>` out of a one-statement program and replays it
// against args.
func traceSrc(t *testing.T, src string, args ...ir.Value) (*ExprNode, error) {
	t.Helper()
	prog := "Cursed Energy: x\nCursed Technique: Apply\n    Using: " + src + "\n"
	toks, err := lexer.Lex(prog)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	p, err := parser.Parse(prog, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	lam := p.Statements[1].Args[0].Value.(ast.LambdaArg).Lambda
	return TraceLambda(lam, nil, args...)
}

// flatten renders a replayed tree as `depth:expr=value` lines, so a test can
// state the whole shape at once.
func flatten(n *ExprNode, depth int, out []string) []string {
	if n == nil {
		return out
	}
	v := ir.FormatShort(n.Value)
	if n.Err != nil {
		v = "!" + n.Err.Error()
	}
	out = append(out, strings.Repeat(" ", depth)+exprKind(n.Expr)+"="+v)
	for _, c := range n.Children {
		out = flatten(c, depth+1, out)
	}
	return out
}

func exprKind(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.IntLit:
		return "int"
	case *ast.Ident:
		return x.Name
	case *ast.BinaryExpr:
		return "binary"
	case *ast.CallExpr:
		return "call"
	case *ast.CondExpr:
		return "if"
	case *ast.LetExpr:
		return "let"
	}
	return "?"
}

func TestTraceLambdaRecordsEverySubexpression(t *testing.T) {
	root, err := traceSrc(t, "(x) -> abs(x - 10) + 1", int64(1))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(flatten(root, 0, nil), "\n")
	want := strings.Join([]string{
		"binary=10",   // abs(x - 10) + 1
		" call=9",     //   abs(x - 10)
		"  binary=-9", //     x - 10
		"   x=1",
		"   int=10",
		" int=1",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The tree is what ran: an `if` records only the arm it took, which is the
// difference between a trace and a pretty-printed source tree.
func TestTraceLambdaRecordsOnlyTheArmTaken(t *testing.T) {
	root, err := traceSrc(t, "(x) -> if x > 3 then x * 2 else x - 100", int64(1))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(flatten(root, 0, nil), "\n")
	if strings.Contains(got, "=2") {
		t.Errorf("the `then` arm did not run and must not be recorded:\n%s", got)
	}
	if !strings.Contains(got, "binary=-99") {
		t.Errorf("the `else` arm (1 - 100) should be recorded:\n%s", got)
	}
}

// Short-circuiting is visible for the same reason: `or` never evaluated its
// right operand, so there is no node for it.
func TestTraceLambdaShowsShortCircuiting(t *testing.T) {
	root, err := traceSrc(t, "(x) -> x = 0 or 10 / x = 5", int64(0))
	if err != nil {
		t.Fatal(err)
	}
	lines := flatten(root, 0, nil)
	for _, l := range lines {
		if strings.Contains(l, "!") {
			t.Errorf("the division was skipped, so nothing should have failed:\n%s", strings.Join(lines, "\n"))
		}
	}
	if len(lines) != 4 { // `or`, `x = 0`, x, 0
		t.Errorf("got %d rows, want 4 (the right operand was never evaluated):\n%s",
			len(lines), strings.Join(lines, "\n"))
	}
}

// A failing application still yields its tree, with the error on the node that
// raised it — which is the whole reason to look at one.
func TestTraceLambdaKeepsTheTreeOfAFailure(t *testing.T) {
	root, err := traceSrc(t, "(x) -> 10 / (x - 1)", int64(1))
	if err == nil {
		t.Fatal("dividing by zero should fail")
	}
	if root == nil {
		t.Fatal("a failing application should still return what it evaluated")
	}
	got := strings.Join(flatten(root, 0, nil), "\n")
	if !strings.Contains(got, "binary=0") {
		t.Errorf("the divisor that came to 0 should be recorded:\n%s", got)
	}
	if !strings.Contains(got, "!") {
		t.Errorf("the failure should be carried on a node:\n%s", got)
	}
}

// The replay is opt-in and leaves nothing behind: an ordinary evaluation after
// one records nothing and pays nothing.
func TestTracingIsClearedAfterAReplay(t *testing.T) {
	if _, err := traceSrc(t, "(x) -> x + 1", int64(1)); err != nil {
		t.Fatal(err)
	}
	if tracing != nil {
		t.Error("the replay recorder should be cleared when TraceLambda returns")
	}
}

// A watcher sees every application, and sees it before the body runs — so an
// application that fails is still reported.
func TestWatchApplications(t *testing.T) {
	var seen []int
	restore := WatchApplications(func(l *ast.Lambda, types []*ir.Type, args []ir.Value) {
		n, _ := args[0].(int64)
		seen = append(seen, int(n))
	})
	prog := "Cursed Energy: x\nCursed Technique: Apply\n    Using: (x) -> x + 1\n"
	toks, _ := lexer.Lex(prog)
	p, err := parser.Parse(prog, toks)
	if err != nil {
		t.Fatal(err)
	}
	lam := p.Statements[1].Args[0].Value.(ast.LambdaArg).Lambda

	for _, n := range []int64{1, 2, 3} {
		if _, err := EvalLambda(lam, n); err != nil {
			t.Fatal(err)
		}
	}
	restore()
	if _, err := EvalLambda(lam, int64(9)); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 3 || seen[0] != 1 || seen[2] != 3 {
		t.Errorf("watched applications = %v, want [1 2 3] and nothing after restore", seen)
	}
}

// A replay must not report itself to the watcher: the application it is
// replaying is one the watcher already saw.
func TestReplayIsNotWatched(t *testing.T) {
	var count int
	defer WatchApplications(func(*ast.Lambda, []*ir.Type, []ir.Value) { count++ })()
	if _, err := traceSrc(t, "(x) -> x + 1", int64(1)); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("the replay reported %d applications, want 0", count)
	}
}
