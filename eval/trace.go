package eval

import (
	"fmt"
	"slices"

	"domain/ast"
	"domain/ir"
	"domain/typecheck"
)

// Watching lambda applications, and replaying one subexpression at a time.
//
// A `Using:` expression is where a Domain program does its arithmetic, and it
// is the one layer a reader cannot watch: the pipeline trace shows the list
// that went into `Map Each` and the list that came out, and says nothing about
// the expression in between. `domain expansion: visualize` wants that middle,
// and getting it is two halves.
//
// The first half is **watching**: a lambda is applied deep inside a primitive,
// under the node's Eval rather than beside it, so nothing at the pipeline layer
// sees it happen. EvalLambdaTyped is the one door every application goes
// through, so a watcher installed there sees them all — and paying for it costs
// one nil check per application, not per subexpression.
//
// The second half is **replaying**: an application is (lambda, arguments), and
// the expression layer is pure, so re-running one later produces exactly what
// it produced during the run. That is why the recording keeps only the
// arguments and not the whole tree of intermediate values — one small capture
// during the run, and all the detail on demand, for whichever application the
// reader actually asks about.
//
// The exception is a `Using:` written as an indented pipeline (ast.BlockBody).
// A pipeline is not pure — it can Reveal — so replaying one is not free of
// consequence, and TraceLambda refuses rather than running a program twice
// behind the reader's back.

// Applied is called with each lambda application while a watcher is installed.
// The slices are the watcher's to keep: EvalLambdaTyped copies them on the way
// in, for the escape-analysis reason documented there.
type Applied func(l *ast.Lambda, paramTypes []*ir.Type, args []ir.Value)

// watching is the installed watcher, or nil in an ordinary run.
//
// It is package level for the same reason ir's current-Context record is, and
// under the same standing assumption: interp.Run is not called concurrently
// within one process.
var watching Applied

// WatchApplications installs f as the watcher for lambda applications and
// returns a function restoring whatever was installed before. A nil f turns
// watching off.
func WatchApplications(f Applied) (restore func()) {
	prev := watching
	watching = f
	return func() { watching = prev }
}

// ExprNode is one subexpression of a replayed application: what was evaluated,
// what it came to, and the subexpressions it was computed from.
//
// The tree is what *ran*, not what was written, so it tells the truth about the
// two constructs that do not evaluate everything they contain: an `if` has only
// the arm it took, and `and`/`or` only the operand they needed.
type ExprNode struct {
	Expr     ast.Expr
	Value    ir.Value
	Err      error
	Children []*ExprNode

	// Capped is set on the root when the replay hit maxTracedNodes, so a reader
	// is told the tree is partial rather than shown a silently truncated one.
	Capped bool
}

// maxTracedNodes bounds one replay. An expression is written by hand and so is
// small, but a `consider` bound to something enormous inside a deeply nested
// call is still cheap to write, and a display has no use for the tail of it.
const maxTracedNodes = 4096

// tracing is the replay in progress, or nil. Same assumption as watching.
var tracing *exprTrace

type exprTrace struct {
	// stack is the chain of subexpressions currently being evaluated, with a
	// sentinel at the bottom to collect the outermost one.
	stack  []*ExprNode
	budget int
	capped bool
}

// step records one subexpression around its evaluation. Children register
// themselves against it, because evalExprStep recurses through evalExpr, which
// comes straight back here.
func (t *exprTrace) step(e ast.Expr, env Env, types typecheck.Env) (ir.Value, error) {
	if t.budget <= 0 {
		t.capped = true
		return evalExprStep(e, env, types)
	}
	t.budget--

	n := &ExprNode{Expr: e}
	parent := t.stack[len(t.stack)-1]
	parent.Children = append(parent.Children, n)
	t.stack = append(t.stack, n)
	v, err := evalExprStep(e, env, types)
	t.stack = t.stack[:len(t.stack)-1]
	n.Value, n.Err = v, err
	return v, err
}

// TraceLambda replays a lambda application, returning the tree of every
// subexpression it evaluated. The application is the one a watcher saw: pass
// back the lambda, parameter types and arguments it was given.
//
// A failing application still returns its tree — the failure is the reason to
// look, and the node that carries the error is the answer to where it happened.
func TraceLambda(l *ast.Lambda, paramTypes []*ir.Type, args ...ir.Value) (*ExprNode, error) {
	if l == nil {
		return nil, fmt.Errorf("no lambda to trace")
	}
	if _, ok := l.Body.(*ast.BlockBody); ok {
		return nil, fmt.Errorf("this Using: is an indented pipeline, not an expression")
	}
	if tracing != nil {
		return nil, fmt.Errorf("an expression replay is already in progress")
	}
	t := &exprTrace{stack: []*ExprNode{{}}, budget: maxTracedNodes}
	tracing = t
	defer func() { tracing = nil }()

	_, err := EvalLambdaTyped(l, paramTypes, slices.Clone(args)...)
	root := t.stack[0]
	if len(root.Children) == 0 {
		// Nothing was evaluated at all, which only happens when the application
		// was rejected before the body ran (a wrong argument count).
		if err == nil {
			err = fmt.Errorf("the lambda body evaluated nothing")
		}
		return nil, err
	}
	out := root.Children[0]
	out.Capped = t.capped
	return out, err
}
