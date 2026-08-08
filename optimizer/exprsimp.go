package optimizer

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Expression-layer passes: constant folding, algebraic identities, and
// boolean short-circuit simplification inside Using: lambda bodies. The three
// share one bottom-up rewriter so a fold can cascade (2 < 3 folds to true,
// which then collapses `true and p` to p).
//
// Bodies are rewritten *in place* (lam.Body is reassigned on the shared
// *ast.Lambda): every consumer of the lambda — the node's captured Eval
// closure, the codegen switch reading Meta — holds the same pointer, so both
// backends see the simplified body without any node surgery. That also makes
// the pass safe inside Channel and loop sub-pipelines.

// simplifier rewrites one expression tree, remembering which rule families
// fired for the --explain message.
type simplifier struct {
	kinds map[string]bool
}

func (s *simplifier) mark(kind string) {
	if s.kinds == nil {
		s.kinds = map[string]bool{}
	}
	s.kinds[kind] = true
}

// simplifyLambdaBodies applies the expression-layer rules to every Using:
// lambda in the pipeline, including lambdas inside Channel bodies and loop
// bodies (While predicates included — they live in Meta["lambda"] too).
func simplifyLambdaBodies(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	seen := map[*ast.Lambda]bool{}
	for _, list := range nodeLists(p) {
		for _, n := range list {
			lam, _ := n.Meta["lambda"].(*ast.Lambda)
			if lam == nil || seen[lam] {
				continue
			}
			// Read from Meta directly rather than through nodeLambda, because
			// this pass wants the lambda even when no other pass may have it —
			// so the stand-down for an updating body is stated here too. The
			// rules below drop and duplicate subexpressions, which is exactly
			// what a write must not have done to it.
			if effectful(lam) {
				continue
			}
			// Float-typed nodes are exempt: the integer identities below
			// (x*0 → 0, x+0 → x, constant folding to IntLit) are wrong or
			// type-changing under IEEE arithmetic, and the simplifier has no
			// type information inside the body.
			if typeHasFloat(n.In) || typeHasFloat(n.Out) {
				continue
			}
			seen[lam] = true
			s := &simplifier{}
			body := s.simplify(lam.Body)
			if len(s.kinds) == 0 {
				continue
			}
			lam.Body = body
			kinds := slices.Sorted(maps.Keys(s.kinds))
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain simplified the Using: lambda of %s (%s). Guaranteed hit.",
				n.Prim, strings.Join(kinds, ", "))})
		}
	}
	return rewrites
}

// simplify rewrites bottom-up: children first, then local rules at this node
// until they stop applying.
func (s *simplifier) simplify(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		e = &ast.UnaryExpr{Op: x.Op, X: s.simplify(x.X), Pos: x.Pos}
	case *ast.BinaryExpr:
		e = &ast.BinaryExpr{Op: x.Op, Left: s.simplify(x.Left), Right: s.simplify(x.Right), Pos: x.Pos}
	case *ast.FieldAccess:
		e = &ast.FieldAccess{Target: s.simplify(x.Target), Field: x.Field, Pos: x.Pos}
	case *ast.CallExpr:
		args := make([]ast.Expr, len(x.Args))
		for i, a := range x.Args {
			args[i] = s.simplify(a)
		}
		e = &ast.CallExpr{Fn: x.Fn, Args: args, Pos: x.Pos, InPlace: x.InPlace}
	case *ast.CondExpr:
		e = &ast.CondExpr{
			Cond: s.simplify(x.Cond),
			Then: s.simplify(x.Then),
			Else: s.simplify(x.Else),
			Pos:  x.Pos,
		}
	case *ast.LetExpr:
		e = &ast.LetExpr{
			Name:  x.Name,
			Value: s.simplify(x.Value),
			Body:  s.simplify(x.Body),
			Pos:   x.Pos,
		}
	}
	for {
		next, changed := s.rewriteLocal(e)
		if !changed {
			return next
		}
		e = next
	}
}

// rewriteLocal applies one rule at the root of e. All arithmetic mirrors
// eval's semantics exactly (Go int64 wrap-around, truncated division), and no
// rule may discard a subexpression that could fail at runtime — see isTotal.
func (s *simplifier) rewriteLocal(e ast.Expr) (ast.Expr, bool) {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if lit, ok := x.X.(*ast.IntLit); ok && x.Op == token.MINUS {
			s.mark("constant folding")
			return &ast.IntLit{Value: -lit.Value, Pos: x.Pos}, true
		}
	case *ast.BinaryExpr:
		return s.rewriteBinary(x)
	}
	return e, false
}

func (s *simplifier) rewriteBinary(x *ast.BinaryExpr) (ast.Expr, bool) {
	li, lIsInt := x.Left.(*ast.IntLit)
	ri, rIsInt := x.Right.(*ast.IntLit)

	// Constant folding over two integer literals.
	if lIsInt && rIsInt {
		if v, ok := foldIntOp(x.Op, li.Value, ri.Value); ok {
			s.mark("constant folding")
			return &ast.IntLit{Value: v, Pos: x.Pos}, true
		}
		if b, ok := foldIntCmp(x.Op, li.Value, ri.Value); ok {
			s.mark("constant folding")
			return &ast.BoolLit{Value: b, Pos: x.Pos}, true
		}
	}

	// Text orders lexicographically, so two string literals fold the same way
	// two integer ones do. Byte-wise, matching ir.Compare and Go's own `<`.
	if ls, ok := x.Left.(*ast.StringLit); ok {
		if rs, ok := x.Right.(*ast.StringLit); ok {
			if b, ok := foldCmp(x.Op, strings.Compare(ls.Value, rs.Value)); ok {
				s.mark("constant folding")
				return &ast.BoolLit{Value: b, Pos: x.Pos}, true
			}
		}
	}

	// Constant folding of `=` over matching literals.
	if x.Op == token.EQ {
		if ls, ok := x.Left.(*ast.StringLit); ok {
			if rs, ok := x.Right.(*ast.StringLit); ok {
				s.mark("constant folding")
				return &ast.BoolLit{Value: ls.Value == rs.Value, Pos: x.Pos}, true
			}
		}
		if lb, ok := x.Left.(*ast.BoolLit); ok {
			if rb, ok := x.Right.(*ast.BoolLit); ok {
				s.mark("constant folding")
				return &ast.BoolLit{Value: lb.Value == rb.Value, Pos: x.Pos}, true
			}
		}
	}

	// Reflexive comparisons on the same identifier: x = x, x <= x are always
	// true; x < x, x > x always false (values are pure, equality structural).
	if lid, ok := x.Left.(*ast.Ident); ok {
		if rid, ok := x.Right.(*ast.Ident); ok && lid.Name == rid.Name {
			switch x.Op {
			case token.EQ, token.LE, token.GE:
				s.mark("algebraic identity")
				return &ast.BoolLit{Value: true, Pos: x.Pos}, true
			case token.LT, token.GT:
				s.mark("algebraic identity")
				return &ast.BoolLit{Value: false, Pos: x.Pos}, true
			case token.MINUS:
				s.mark("algebraic identity")
				return &ast.IntLit{Value: 0, Pos: x.Pos}, true
			}
		}
	}

	// Boolean short-circuit collapsing.
	if x.Op == token.AND || x.Op == token.OR {
		return s.rewriteLogical(x)
	}

	// Algebraic identities. The typechecker already proved both operands Int
	// for these operators, so replacing the expression by the other operand
	// preserves the type.
	switch x.Op {
	case token.PLUS:
		if lIsInt && li.Value == 0 {
			s.mark("algebraic identity")
			return x.Right, true
		}
		if rIsInt && ri.Value == 0 {
			s.mark("algebraic identity")
			return x.Left, true
		}
	case token.MINUS:
		if rIsInt && ri.Value == 0 {
			s.mark("algebraic identity")
			return x.Left, true
		}
	case token.STAR:
		if lIsInt && li.Value == 1 {
			s.mark("algebraic identity")
			return x.Right, true
		}
		if rIsInt && ri.Value == 1 {
			s.mark("algebraic identity")
			return x.Left, true
		}
		// x*0 → 0 only when x can be discarded without losing an error.
		if lIsInt && li.Value == 0 && isTotal(x.Right) {
			s.mark("algebraic identity")
			return &ast.IntLit{Value: 0, Pos: x.Pos}, true
		}
		if rIsInt && ri.Value == 0 && isTotal(x.Left) {
			s.mark("algebraic identity")
			return &ast.IntLit{Value: 0, Pos: x.Pos}, true
		}
	case token.SLASH:
		if rIsInt && ri.Value == 1 {
			s.mark("algebraic identity")
			return x.Left, true
		}
	}
	return x, false
}

// rewriteLogical collapses and/or around boolean literals, preserving eval's
// short-circuit order: a left-hand literal always collapses; a right-hand
// literal may only erase the left operand when that operand is total.
func (s *simplifier) rewriteLogical(x *ast.BinaryExpr) (ast.Expr, bool) {
	if lb, ok := x.Left.(*ast.BoolLit); ok {
		s.mark("boolean short-circuit")
		switch {
		case x.Op == token.AND && lb.Value:
			return x.Right, true
		case x.Op == token.AND: // false and e → false, e never evaluated anyway
			return &ast.BoolLit{Value: false, Pos: x.Pos}, true
		case x.Op == token.OR && lb.Value:
			return &ast.BoolLit{Value: true, Pos: x.Pos}, true
		default: // false or e → e
			return x.Right, true
		}
	}
	if rb, ok := x.Right.(*ast.BoolLit); ok {
		if x.Op == token.AND && rb.Value {
			s.mark("boolean short-circuit")
			return x.Left, true
		}
		if x.Op == token.OR && !rb.Value {
			s.mark("boolean short-circuit")
			return x.Left, true
		}
		// e and false / e or true: the result is constant but e still runs
		// first in the naive pipeline — only fold when e cannot fail.
		if isTotal(x.Left) {
			s.mark("boolean short-circuit")
			return &ast.BoolLit{Value: x.Op == token.OR, Pos: x.Pos}, true
		}
	}
	return x, false
}

// foldIntOp folds an arithmetic operator over two int64 literals, mirroring
// eval exactly: Go wrap-around, truncated division, and ÷0 left in place so
// the runtime error survives.
func foldIntOp(op token.Kind, a, b int64) (int64, bool) {
	switch op {
	case token.PLUS:
		return a + b, true
	case token.MINUS:
		return a - b, true
	case token.STAR:
		return a * b, true
	case token.SLASH:
		if b == 0 {
			return 0, false
		}
		return a / b, true
	}
	return 0, false
}

func foldIntCmp(op token.Kind, a, b int64) (bool, bool) {
	if op == token.EQ {
		return a == b, true
	}
	return foldCmp(op, cmp.Compare(a, b))
}

// foldCmp turns a three-way comparison into the answer a relational operator
// would give. `=` is deliberately absent: two values can compare 0 without
// being equal (a Float NaN does, matching ir.Compare), so equality folds
// through its own literal cases rather than through an ordering.
func foldCmp(op token.Kind, c int) (bool, bool) {
	switch op {
	case token.LT:
		return c < 0, true
	case token.GT:
		return c > 0, true
	case token.LE:
		return c <= 0, true
	case token.GE:
		return c >= 0, true
	}
	return false, false
}
