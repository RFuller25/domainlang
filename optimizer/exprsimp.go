package optimizer

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"domain/ast"
	"domain/format"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
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
	// env types the lambda's parameters and bindings when the stage makes them
	// knowable (see lambdaEnv), and is nil when it does not. The type-blind
	// rules never read it; the ones that need an element type — "this list is
	// Ints, so its order cannot be observed through a sum" — stand down
	// without it.
	env typecheck.Env
	// scope is what the enclosing `consider` bindings tell the rules (cse.go).
	scope scope
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
			s := &simplifier{env: lambdaEnv(n, lam)}
			body := s.share(s.simplify(lam.Body))
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

// lambdaEnv types a stage lambda's parameters, for the rules that need an
// element type. The answer is nil — and the typed rules stand down — for any
// stage whose parameter types are not stated by its node: guessing one would
// let a rewrite assume Int where the program has Float, where an ordering is
// observable. A name the env does not hold (an enclosing Consider's binding)
// fails typing at the use, which also stands the rule down.
func lambdaEnv(n *ir.Node, lam *ast.Lambda) typecheck.Env {
	var types []*ir.Type
	elem := elemType(n.In)
	switch n.Prim {
	case "Apply":
		types = []*ir.Type{n.In}
	case "Map Each", "Filter", "Count Matching", "Any", "All", "Sum By", "Product By",
		"Find", "Find Index", "Take While", "Drop While", "Partition", "Group By", "Count By",
		"Min By", "Max By", "Sort By":
		if n.In != nil && n.In.Kind == ir.KList {
			types = []*ir.Type{elem}
		}
	case "Fold":
		if n.In != nil && n.In.Kind == ir.KList {
			types = []*ir.Type{n.Out, elem}
		}
	}
	if len(types) != len(lam.Params) {
		return nil
	}
	env := typecheck.Env{}
	if binds, ok := n.Meta[ir.MetaBinds].([]ir.Binding); ok {
		for _, b := range binds {
			env[b.Name()] = b.Type()
		}
	}
	for i, prm := range lam.Params {
		if types[i] == nil {
			return nil
		}
		env[prm] = types[i]
	}
	return env
}

// isEmptyGuard recognizes `length(name) = 0`, the condition rewriteCall puts
// in front of a first/last it has replaced.
func isEmptyGuard(c ast.Expr) bool {
	be, ok := c.(*ast.BinaryExpr)
	if !ok || be.Op != token.EQ {
		return false
	}
	call, ok := be.Left.(*ast.CallExpr)
	lit, isLit := be.Right.(*ast.IntLit)
	return ok && isLit && lit.Value == 0 && builtinName(call) == "length" && len(call.Args) == 1 && cheapPure(call.Args[0])
}

// intList reports whether e is statically a List<Int> under the simplifier's
// environment.
func (s *simplifier) intList(e ast.Expr) bool {
	if s.env == nil {
		return false
	}
	t, err := typecheck.ExprType(e, s.env)
	return err == nil && isIntList(t)
}

// cheapPure reports whether e may be evaluated twice without anyone noticing:
// a name or a field of one.
func cheapPure(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return true
	case *ast.FieldAccess:
		return cheapPure(x.Target)
	}
	return false
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
		then := x.Then
		// The `then` of an empty-list guard is the original expression kept
		// to fail in its own words (see rewriteCall). Simplifying it would
		// guard it again, every round, without end.
		if !isEmptyGuard(x.Cond) {
			then = s.simplify(then)
		}
		e = &ast.CondExpr{
			Cond: s.simplify(x.Cond),
			Then: then,
			Else: s.simplify(x.Else),
			Pos:  x.Pos,
		}
	case *ast.LetExpr:
		// The value is in the outer scope; the body is in the scope the binding
		// makes (see cse.go), restored on the way out.
		val := s.simplify(x.Value)
		outer := s.scope
		s.scope = s.enterLet(x.Name, val)
		body := s.simplify(x.Body)
		s.scope = outer
		e = &ast.LetExpr{Name: x.Name, Value: val, Body: body, Pos: x.Pos}
	}
	// An expression an enclosing `consider` already computed is that binding.
	if name, ok := s.availName(e); ok {
		s.mark("common subexpression")
		return &ast.Ident{Name: name, Pos: exprPos(e)}
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
	case *ast.CallExpr:
		return s.rewriteCall(x)
	case *ast.CondExpr:
		return s.rewriteCond(x)
	}
	return e, false
}

// sharedArmBuilders are the calls whose arguments are all evaluated, left to
// right, before the call itself does anything: constructors and the functional
// collection updates. A call that decides *whether* to evaluate an argument is
// not on the list, since moving a conditional inside it would change what it
// evaluates.
var sharedArmBuilders = map[string]bool{
	"tuple": true, "record": true, "list": true,
	"insert": true, "put": true, "set": true, "setat": true, "concat": true,
}

// rewriteCond merges the two arms of a conditional that build the same thing
// and differ in exactly one argument:
//
//	if c then tuple(A, n + 1, k) else tuple(A, n, k)
//	  → tuple(A, if c then n + 1 else n, k)
//
// Every argument that is shared is evaluated once on either path in both
// forms, and the chosen arm's differing argument is evaluated once; what
// changes is only where the condition falls among them. The original runs c
// first. The rewritten call runs the arguments *before* the differing one
// first, so those must be total — a failure there could otherwise be reported
// ahead of a failing c, or instead of it. The arguments after the differing
// one, and the call itself, still come after c, as they did. The condition
// stays lazy about which differing argument it evaluates, so nothing that was
// skipped is now run.
func (s *simplifier) rewriteCond(x *ast.CondExpr) (ast.Expr, bool) {
	th, ok1 := x.Then.(*ast.CallExpr)
	el, ok2 := x.Else.(*ast.CallExpr)
	if !ok1 || !ok2 || len(th.Args) != len(el.Args) || len(th.Args) == 0 {
		return x, false
	}
	name := builtinName(th)
	if name == "" || name != builtinName(el) || !sharedArmBuilders[name] {
		return x, false
	}
	diff := -1
	for i := range th.Args {
		if format.Expr(th.Args[i]) == format.Expr(el.Args[i]) {
			continue
		}
		if diff >= 0 {
			return x, false // more than one argument differs
		}
		diff = i
	}
	if diff < 0 {
		return x, false
	}
	for _, a := range th.Args[:diff] {
		if !isTotalIn(a, s.env) || ast.HasNondeterminism(a) {
			return x, false
		}
	}
	args := slices.Clone(th.Args)
	args[diff] = &ast.CondExpr{Cond: x.Cond, Then: th.Args[diff], Else: el.Args[diff], Pos: x.Pos}
	s.mark("shared conditional arms")
	return &ast.CallExpr{Fn: th.Fn, Args: args, Pos: th.Pos, InPlace: th.InPlace}, true
}

// rewriteCall replaces a builtin call with a cheaper one that gives the same
// answer: a list built only to be measured or searched, or an idempotent
// transform applied twice. Every rule is exact — both sides are total on
// anything that typechecked, so nothing that could fail is discarded — and
// none depends on element order among Floats.
//
//	contains(keys(m), k)     → haskey(m, k)     a key list scanned → one probe
//	contains(tolist(s), x)   → contains(s, x)   a list scanned → one probe
//	contains(sort(xs), v)    → contains(xs, v)  and unique: a copy, searched
//	length(keys(m))          → size(m)          and values, entries, tolist
//	length(sort(xs))         → length(xs)       and reverse: a copy, measured
//	length(chars(s))         → length(s)        both count runes
//	sort(sort(xs))           → sort(xs)         and unique(unique(xs))
//	reverse(reverse(xs))     → xs
func (s *simplifier) rewriteCall(x *ast.CallExpr) (ast.Expr, bool) {
	fn := builtinName(x)
	if len(x.Args) == 0 {
		return x, false
	}
	if r, ok := s.itemOfMember(x); ok {
		return r, true
	}
	inner, _ := x.Args[0].(*ast.CallExpr)
	if inner == nil || len(inner.Args) != 1 {
		return x, false
	}
	arg := inner.Args[0]
	call := func(name string, args ...ast.Expr) ast.Expr {
		s.mark("cheaper equivalent builtin")
		return &ast.CallExpr{Fn: &ast.Ident{Name: name, Pos: x.Pos}, Args: args, Pos: x.Pos}
	}
	// The typed rules: a reordering placed in front of a reduction that cannot
	// see order, over a list the type says is Ints (a Float sum is order-
	// sensitive in the last bit, and a NaN makes a sort observable).
	//
	//	sum(sort(xs))    → sum(xs)    and reverse; Int addition commutes
	//	min/max(sort(xs)) → min/max(xs)   same empty-list failure, same words
	//	last(sort(xs))   → max(xs)    (first → min) behind a guard, below
	if builtinName(inner) == "sort" || builtinName(inner) == "reverse" {
		switch fn {
		case "sum", "min", "max":
			if len(x.Args) == 1 && s.intList(arg) {
				return call(fn, arg), true
			}
		case "first", "last", "item":
			if fn == "item" {
				// item(sort(xs), 0) is first(sort(xs)) with another failure.
				lit, isLit := x.Args[len(x.Args)-1].(*ast.IntLit)
				if len(x.Args) != 2 || !isLit || lit.Value != 0 {
					break
				}
				fn = "first"
			}
			// first/last of an empty list fail in words min/max do not use, so
			// the answer is not the same expression: guard it. The original
			// still runs on the empty list and says what it always said; the
			// non-empty list is scanned once instead of sorted.
			if builtinName(inner) == "sort" && (len(x.Args) == 1 || builtinName(x) == "item") && cheapPure(arg) && s.intList(arg) {
				pick := "min"
				if fn == "last" {
					pick = "max"
				}
				s.mark("cheaper equivalent builtin")
				return &ast.CondExpr{
					Cond: &ast.BinaryExpr{Op: token.EQ,
						Left:  &ast.CallExpr{Fn: &ast.Ident{Name: "length", Pos: x.Pos}, Args: []ast.Expr{arg}, Pos: x.Pos},
						Right: &ast.IntLit{Value: 0, Pos: x.Pos}, Pos: x.Pos},
					Then: x,
					Else: &ast.CallExpr{Fn: &ast.Ident{Name: pick, Pos: x.Pos}, Args: []ast.Expr{arg}, Pos: x.Pos},
					Pos:  x.Pos,
				}, true
			}
		}
	}
	// The end of a reversed list is the other end of the list — any element
	// type, since position is not order. Guarded for the same reason as above.
	if builtinName(inner) == "reverse" && (fn == "first" || fn == "last") && len(x.Args) == 1 && cheapPure(arg) {
		other := "last"
		if fn == "last" {
			other = "first"
		}
		if t, err := typecheck.ExprType(arg, s.env); s.env != nil && err == nil && t != nil && t.Kind == ir.KList {
			s.mark("cheaper equivalent builtin")
			return &ast.CondExpr{
				Cond: &ast.BinaryExpr{Op: token.EQ,
					Left:  &ast.CallExpr{Fn: &ast.Ident{Name: "length", Pos: x.Pos}, Args: []ast.Expr{arg}, Pos: x.Pos},
					Right: &ast.IntLit{Value: 0, Pos: x.Pos}, Pos: x.Pos},
				Then: x,
				Else: &ast.CallExpr{Fn: &ast.Ident{Name: other, Pos: x.Pos}, Args: []ast.Expr{arg}, Pos: x.Pos},
				Pos:  x.Pos,
			}, true
		}
	}
	switch fn {
	case "contains":
		if len(x.Args) != 2 {
			return x, false
		}
		switch builtinName(inner) {
		case "keys":
			return call("haskey", arg, x.Args[1]), true
		case "tolist":
			return call("contains", arg, x.Args[1]), true
		case "sort", "unique":
			// Membership does not depend on order or multiplicity, and both
			// take only a List (reverse is absent: it also takes a Text, where
			// contains is the substring test and order is the whole question).
			return call("contains", arg, x.Args[1]), true
		}
	case "length":
		switch builtinName(inner) {
		case "keys", "values", "entries", "tolist":
			return call("size", arg), true
		case "sort", "reverse", "chars":
			return call("length", arg), true
		}
	case "sort", "unique":
		if builtinName(inner) == fn {
			s.mark("cheaper equivalent builtin")
			return inner, true
		}
	case "reverse":
		if builtinName(inner) == "reverse" {
			s.mark("cheaper equivalent builtin")
			return arg, true
		}
	}
	return x, false
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

	// A choice between two integer literals, compared with a third: the
	// comparison is decided by which arm was taken, so it *is* the condition
	// (or its negation). This is what a Bool looks like after a program has
	// turned it into a number and then asked the number what it was —
	// `(if a = b then 0 else 1) = 0` is `a = b`. Only the mixed case is
	// rewritten: when both arms answer alike the result is a constant and the
	// condition would be discarded, which is the conditional folding rule's
	// decision to make, with its totality check.
	if cond, lit, op, ok := condVersusLiteral(x); ok {
		tl, tOK := cond.Then.(*ast.IntLit)
		el, eOK := cond.Else.(*ast.IntLit)
		if tOK && eOK {
			thenHolds, ok1 := foldIntCmp(op, tl.Value, lit)
			elseHolds, ok2 := foldIntCmp(op, el.Value, lit)
			if ok1 && ok2 && thenHolds != elseHolds {
				s.mark("boolean round trip")
				if thenHolds {
					return cond.Cond, true
				}
				return &ast.UnaryExpr{Op: token.NOT, X: cond.Cond, Pos: x.Pos}, true
			}
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
// condVersusLiteral recognizes `(if c then … else …) op N` and `N op (if …)`,
// returning the conditional, the literal, and the operator as it reads with
// the conditional on the left.
func condVersusLiteral(x *ast.BinaryExpr) (cond *ast.CondExpr, lit int64, op token.Kind, ok bool) {
	if c, isCond := x.Left.(*ast.CondExpr); isCond {
		if l, isLit := x.Right.(*ast.IntLit); isLit {
			return c, l.Value, x.Op, true
		}
	}
	if c, isCond := x.Right.(*ast.CondExpr); isCond {
		if l, isLit := x.Left.(*ast.IntLit); isLit {
			op := x.Op
			switch op {
			case token.LT:
				op = token.GT
			case token.GT:
				op = token.LT
			case token.LE:
				op = token.GE
			case token.GE:
				op = token.LE
			}
			return c, l.Value, op, true
		}
	}
	return nil, 0, 0, false
}

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
