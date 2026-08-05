package prims

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Local bindings: `Consider x As …` and `Consider x Of …` (see ast.Binding).
//
// A binding is in scope for every expression on the statement it is written
// under — each of its lambda-valued arguments — and for every statement nested
// beneath it, where an inner binding of the same name shadows it and a lambda
// parameter of that name shadows it too.
//
// Three kinds, and which one a binding is decides how far down the stack it
// travels:
//
//   - a *constant* (`As` an expression that folds) is substituted into lambda
//     bodies as a literal, exactly as a Shikigami's scalar parameter is. It
//     costs nothing at runtime and — the reason it is worth folding rather
//     than binding — leaves the optimizer's body patterns intact, so a stage
//     that gained a binding does not quietly lose its algorithm substitution.
//   - a *function* (`As` a lambda) is inlined at each call site by
//     beta-reduction into `consider … in …`. There are no function values
//     anywhere in the type model, so this is not an implementation shortcut:
//     it is what makes the form expressible at all.
//   - a *runtime value* (`Of` anything, or `As` an expression that does not
//     fold) is computed once when its scope opens, by the Bind node this file
//     builds, and read by name out of the environment that eval and typecheck
//     seed every lambda with (eval/bindings.go, typecheck/bindings.go). The
//     lambda that reads one is left exactly as written.

// localBind is one binding in scope during resolution. Exactly one of lit,
// lam and rt is set.
type localBind struct {
	name string
	src  *ast.Binding
	lit  ast.Expr     // a constant: substituted as a literal
	lam  *ast.Lambda  // a function: inlined at its call sites
	rt   *runtimeBind // a value computed when the scope opens
}

// runtimeBind is a binding whose value is only known once data arrives.
// Exactly one of expr, lam and body is set.
type runtimeBind struct {
	name string
	typ  *ir.Type
	expr ast.Expr       // `As` an expression that did not fold
	lam  *ast.Lambda    // `Of` a lambda, applied to the current value
	body *blockPipeline // `Of` an operation or an indented sub-pipeline
	in   *ir.Type       // the pipeline type the value is computed from
	pos  token.Position
}

// value computes the binding from the value entering its scope. An `As`
// expression is evaluated against the bindings already computed in this scope
// (eval.BindingEnv), which is what lets one binding be written in terms of an
// earlier one.
func (b *runtimeBind) value(v ir.Value) (ir.Value, error) {
	switch {
	case b.lam != nil:
		r, err := eval.EvalLambdaTyped(b.lam,
			append([]*ir.Type{b.in}, ambientTypes()...),
			append([]ir.Value{v}, ambientArgs()...)...)
		if err != nil {
			return nil, runtimeErr("Consider "+b.name, b.pos, "%v", err)
		}
		return r, nil
	case b.body != nil:
		r, err := b.body.RunBlock(v)
		if err != nil {
			return nil, err
		}
		return r, nil
	default:
		env, types := eval.BindingEnv()
		r, err := eval.EvalExprTyped(b.expr, env, types)
		if err != nil {
			return nil, runtimeErr("Consider "+b.name, b.pos, "%v", err)
		}
		return r, nil
	}
}

// lookup finds the innermost binding with the given name.
func (r *resolver) lookupLocal(name string) *localBind {
	for i := len(r.locals) - 1; i >= 0; i-- {
		if r.locals[i].name == name {
			return &r.locals[i]
		}
	}
	return nil
}

// pushBinds resolves a run of bindings against the current pipeline type and
// brings them into scope, innermost last. It returns the ones whose values are
// computed at runtime — the Bind node's payload — and the function that takes
// them all back out of scope again.
//
// Bindings are resolved in written order and each sees the ones above it, so
// `Consider half As accum / 2` is legal and a cycle is not expressible.
func (r *resolver) pushBinds(binds []*ast.Binding, cur *ir.Type) ([]*runtimeBind, func(), error) {
	pushed, typePushed := 0, 0
	pop := func() {
		r.locals = r.locals[:len(r.locals)-pushed]
		typecheck.PopBindings(typePushed)
	}

	var rts []*runtimeBind
	seen := map[string]bool{}
	for _, b := range binds {
		if err := checkBindName(b, seen); err != nil {
			pop()
			return nil, nil, err
		}
		seen[b.Name] = true

		lb, rt, err := r.resolveBind(b, cur)
		if err != nil {
			pop()
			return nil, nil, err
		}
		r.locals = append(r.locals, lb)
		pushed++
		if rt != nil {
			// The type goes into scope now, so the lambdas resolved after this
			// point — including a later binding's — can read the name.
			typecheck.PushBinding(rt.name, rt.typ)
			typePushed++
			rts = append(rts, rt)
		}
	}
	return rts, pop, nil
}

// checkBindName rejects the names a binding may not take.
func checkBindName(b *ast.Binding, seen map[string]bool) error {
	if seen[b.Name] {
		return &ResolveError{Pos: b.Pos, Msg: fmt.Sprintf(
			"%q is bound twice in the same block; a second Consider of the same name has nothing to shadow", b.Name)}
	}
	// Shadowing a builtin would change what a call means for every expression
	// in scope, including ones written before the binding was added, so the
	// name is refused rather than allowed to win.
	if slices.Contains(typecheck.Builtins, b.Name) {
		return &ResolveError{Pos: b.Pos, Msg: fmt.Sprintf(
			"%q is an expression builtin and cannot be used as a binding name", b.Name)}
	}
	return nil
}

// resolveBind lowers one binding, returning its resolved form and — when its
// value is only known at runtime — the payload for the Bind node.
func (r *resolver) resolveBind(b *ast.Binding, cur *ir.Type) (localBind, *runtimeBind, error) {
	fail := func(format string, a ...any) (localBind, *runtimeBind, error) {
		return localBind{}, nil, &ResolveError{Pos: b.Pos, Msg: fmt.Sprintf(format, a...)}
	}

	switch {
	// `Of` an operation phrase or an indented sub-pipeline.
	case b.Of && len(b.Body) > 0:
		if cur == nil {
			return fail("`Consider %s Of` has no current value to work from", b.Name)
		}
		body := &blockPipeline{res: r, stmts: b.Body, prim: "Consider " + b.Name, pos: b.Pos}
		out, err := body.BindBlock(cur)
		if err != nil {
			return fail("`Consider %s Of`: %v", b.Name, err)
		}
		rt := &runtimeBind{name: b.Name, typ: out, body: body, in: cur, pos: b.Pos}
		return localBind{name: b.Name, src: b, rt: rt}, rt, nil

	// `Of` a lambda: applied to the current value, like a measured argument.
	case b.Of:
		if cur == nil {
			return fail("`Consider %s Of` has no current value to work from", b.Name)
		}
		lam, err := r.rewriteLambda(b.Lambda)
		if err != nil {
			return localBind{}, nil, err
		}
		want := 1 + ambientDepth()
		if len(lam.Params) != want {
			return fail("`Consider %s Of` takes a %d-parameter lambda over the current value, got %d",
				b.Name, want, len(lam.Params))
		}
		typ, err := typecheck.LambdaType(lam, append([]*ir.Type{cur}, ambientTypes()...)...)
		if err != nil {
			return fail("`Consider %s Of`: %v", b.Name, err)
		}
		rt := &runtimeBind{name: b.Name, typ: typ, lam: lam, in: cur, pos: b.Pos}
		return localBind{name: b.Name, src: b, rt: rt}, rt, nil

	// `As` a lambda: a function, inlined at its call sites.
	case b.Lambda != nil:
		lam, err := r.rewriteLambda(b.Lambda)
		if err != nil {
			return localBind{}, nil, err
		}
		if len(lam.Params) == 0 {
			return fail("`Consider %s As` a lambda needs at least one parameter; write the value itself if it takes none", b.Name)
		}
		return localBind{name: b.Name, src: b, lam: lam}, nil, nil

	// `As` an expression: a constant when it folds, a runtime value otherwise.
	default:
		e, err := r.rewriteExpr(b.Value, nil)
		if err != nil {
			return localBind{}, nil, err
		}
		if lit, ok := foldLiteral(e); ok {
			return localBind{name: b.Name, src: b, lit: lit}, nil, nil
		}
		typ, err := typecheck.ExprType(e, typecheck.BindingEnv())
		if err != nil {
			return fail("`Consider %s As`: %v", b.Name, err)
		}
		rt := &runtimeBind{name: b.Name, typ: typ, expr: e, in: cur, pos: b.Pos}
		return localBind{name: b.Name, src: b, rt: rt}, rt, nil
	}
}

// foldLiteral evaluates a closed expression to a literal. An expression that
// reads a runtime binding, or that fails (`item(list(), 0)`), does not fold and
// becomes a runtime binding instead — where it fails the way it would have
// anywhere else in the language, at the moment it runs.
//
// Only the scalars have a literal form. A binding of a list or a map is
// perfectly legal, it just cannot be substituted, so it takes the runtime path
// and is computed once when its scope opens.
func foldLiteral(e ast.Expr) (ast.Expr, bool) {
	switch e.(type) {
	case *ast.IntLit, *ast.FloatLit, *ast.BoolLit, *ast.StringLit:
		return e, true
	}
	v, err := eval.EvalExpr(e, nil)
	if err != nil {
		return nil, false
	}
	return literalOf(v, exprPos(e))
}

func literalOf(v ir.Value, pos token.Position) (ast.Expr, bool) {
	switch x := v.(type) {
	case int64:
		return &ast.IntLit{Value: x, Pos: pos}, true
	case float64:
		return &ast.FloatLit{Value: x, Pos: pos}, true
	case bool:
		return &ast.BoolLit{Value: x, Pos: pos}, true
	case string:
		return &ast.StringLit{Value: x, Pos: pos}, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Rewriting expressions against the bindings in scope
// ---------------------------------------------------------------------------

// rewriteLambda rewrites a lambda body against the bindings in scope. The
// parameters shadow bindings of the same name, so a lambda that was correct
// before a binding was added stays correct after.
func (r *resolver) rewriteLambda(lam *ast.Lambda) (*ast.Lambda, error) {
	if lam == nil || len(r.locals) == 0 {
		return lam, nil
	}
	shadowed := make(map[string]bool, len(lam.Params))
	for _, p := range lam.Params {
		shadowed[p] = true
	}
	body, err := r.rewriteExpr(lam.Body, shadowed)
	if err != nil {
		return nil, err
	}
	if body == lam.Body {
		return lam, nil
	}
	return &ast.Lambda{Params: lam.Params, Body: body, Pos: lam.Pos}, nil
}

// rewriteExpr substitutes constants, inlines calls to function bindings, and
// marks every binding it finds as read. An expression that uses no binding is
// returned unchanged (pointer-identical), which is what keeps a program
// without bindings byte-for-byte the program it was.
func (r *resolver) rewriteExpr(e ast.Expr, shadowed map[string]bool) (ast.Expr, error) {
	switch x := e.(type) {
	case *ast.Ident:
		if shadowed[x.Name] {
			return x, nil
		}
		b := r.lookupLocal(x.Name)
		if b == nil {
			return x, nil
		}
		b.src.Used = true
		if b.lam != nil {
			return nil, &ResolveError{Pos: x.Pos, Msg: fmt.Sprintf(
				"%q is a function binding, so it has to be called: write %s(…). "+
					"Domain has no function values, which is why a bare name cannot stand for one", x.Name, x.Name)}
		}
		if b.lit != nil {
			return withPos(b.lit, x.Pos), nil
		}
		return x, nil // a runtime binding resolves by name when the lambda runs

	case *ast.CallExpr:
		args, changed, err := r.rewriteArgs(x.Args, shadowed)
		if err != nil {
			return nil, err
		}
		if id, ok := x.Fn.(*ast.Ident); ok && !shadowed[id.Name] {
			if b := r.lookupLocal(id.Name); b != nil {
				b.src.Used = true
				if b.lam == nil {
					return nil, &ResolveError{Pos: x.Pos, Msg: fmt.Sprintf(
						"%q is a value, not a function, so it cannot be called", id.Name)}
				}
				return inlineCall(b, args, x.Pos)
			}
		}
		if !changed {
			return x, nil
		}
		return &ast.CallExpr{Fn: x.Fn, Args: args, Pos: x.Pos}, nil

	case *ast.UnaryExpr:
		v, err := r.rewriteExpr(x.X, shadowed)
		if err != nil || v == x.X {
			return x, err
		}
		return &ast.UnaryExpr{Op: x.Op, X: v, Pos: x.Pos}, nil

	case *ast.BinaryExpr:
		l, err := r.rewriteExpr(x.Left, shadowed)
		if err != nil {
			return nil, err
		}
		rr, err := r.rewriteExpr(x.Right, shadowed)
		if err != nil {
			return nil, err
		}
		if l == x.Left && rr == x.Right {
			return x, nil
		}
		return &ast.BinaryExpr{Op: x.Op, Left: l, Right: rr, Pos: x.Pos}, nil

	case *ast.FieldAccess:
		t, err := r.rewriteExpr(x.Target, shadowed)
		if err != nil || t == x.Target {
			return x, err
		}
		return &ast.FieldAccess{Target: t, Field: x.Field, Pos: x.Pos}, nil

	case *ast.CondExpr:
		c, err := r.rewriteExpr(x.Cond, shadowed)
		if err != nil {
			return nil, err
		}
		t, err := r.rewriteExpr(x.Then, shadowed)
		if err != nil {
			return nil, err
		}
		el, err := r.rewriteExpr(x.Else, shadowed)
		if err != nil {
			return nil, err
		}
		if c == x.Cond && t == x.Then && el == x.Else {
			return x, nil
		}
		return &ast.CondExpr{Cond: c, Then: t, Else: el, Pos: x.Pos}, nil

	case *ast.LetExpr:
		v, err := r.rewriteExpr(x.Value, shadowed)
		if err != nil {
			return nil, err
		}
		// The bound name shadows a binding for the body, the same way a lambda
		// parameter does.
		inner := shadowed
		if !shadowed[x.Name] {
			inner = maps.Clone(shadowed)
			if inner == nil {
				inner = map[string]bool{}
			}
			inner[x.Name] = true
		}
		body, err := r.rewriteExpr(x.Body, inner)
		if err != nil {
			return nil, err
		}
		if v == x.Value && body == x.Body {
			return x, nil
		}
		return &ast.LetExpr{Name: x.Name, Value: v, Body: body, Pos: x.Pos}, nil

	default:
		// Literals, and the BlockBody standing in for a sub-pipeline: a body's
		// statements are resolved through the resolver, which has the same
		// scope in hand, so there is nothing to rewrite here.
		return e, nil
	}
}

func (r *resolver) rewriteArgs(args []ast.Expr, shadowed map[string]bool) ([]ast.Expr, bool, error) {
	if len(args) == 0 {
		return args, false, nil
	}
	out := make([]ast.Expr, len(args))
	changed := false
	for i, a := range args {
		v, err := r.rewriteExpr(a, shadowed)
		if err != nil {
			return nil, false, err
		}
		out[i] = v
		changed = changed || v != a
	}
	if !changed {
		return args, false, nil
	}
	return out, true, nil
}

// inlineCall beta-reduces a call to a function binding: each argument is bound
// to the matching parameter with `consider … in`, so it is evaluated once
// whether the body reads it once, twice or not at all.
//
// The parameters are renamed when an argument mentions one of them. Bindings
// nest, so the naive expansion would otherwise capture: with `f As (a, b) -> …`
// the call `f(b, a)` binds a to the caller's b and then binds b to what is by
// then no longer the caller's a.
func inlineCall(b *localBind, args []ast.Expr, pos token.Position) (ast.Expr, error) {
	lam := b.lam
	if len(args) != len(lam.Params) {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s takes %d argument(s), got %d", b.name, len(lam.Params), len(args))}
	}

	params := lam.Params
	body := lam.Body
	if capturing(params, args) {
		used := map[string]bool{}
		for _, a := range args {
			collectIdents(a, used)
		}
		collectIdents(body, used)
		renamed := make([]string, len(params))
		sub := map[string]string{}
		for i, p := range params {
			renamed[i] = freshName(p, used)
			used[renamed[i]] = true
			sub[p] = renamed[i]
		}
		params = renamed
		body = renameIdents(body, sub, map[string]bool{})
	}

	out := body
	for i := len(params) - 1; i >= 0; i-- {
		out = &ast.LetExpr{Name: params[i], Value: args[i], Body: out, Pos: pos}
	}
	return out, nil
}

// capturing reports whether binding the parameters in order would capture a
// name one of the arguments depends on.
func capturing(params []string, args []ast.Expr) bool {
	if len(params) < 2 {
		return false
	}
	names := map[string]bool{}
	for i, a := range args {
		if i > 0 {
			free := map[string]bool{}
			collectIdents(a, free)
			for n := range free {
				if names[n] {
					return true
				}
			}
		}
		names[params[i]] = true
	}
	return false
}

// freshName picks a name for a renamed parameter that no expression involved
// is using. The `$` cannot be written in Domain source, so the only names it
// has to avoid are the ones the compiler backend would mangle it into
// colliding with — which is why the check is against the mangled spelling too.
func freshName(base string, used map[string]bool) string {
	for i := 0; ; i++ {
		cand := fmt.Sprintf("$%s%d", base, i)
		if !used[cand] && !used[mangleLike(cand)] {
			return cand
		}
	}
}

// mangleLike is the name the compiler backend's fieldName would produce for a
// generated binding, used only to keep freshName away from it.
func mangleLike(name string) string {
	out := []rune(name)
	for i, r := range out {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			out[i] = '_'
		}
	}
	return string(out)
}

func collectIdents(e ast.Expr, into map[string]bool) {
	switch x := e.(type) {
	case *ast.Ident:
		into[x.Name] = true
	case *ast.UnaryExpr:
		collectIdents(x.X, into)
	case *ast.BinaryExpr:
		collectIdents(x.Left, into)
		collectIdents(x.Right, into)
	case *ast.FieldAccess:
		collectIdents(x.Target, into)
	case *ast.CallExpr:
		collectIdents(x.Fn, into)
		for _, a := range x.Args {
			collectIdents(a, into)
		}
	case *ast.CondExpr:
		collectIdents(x.Cond, into)
		collectIdents(x.Then, into)
		collectIdents(x.Else, into)
	case *ast.LetExpr:
		into[x.Name] = true
		collectIdents(x.Value, into)
		collectIdents(x.Body, into)
	}
}

// renameIdents applies a renaming to the free identifiers of an expression,
// leaving names rebound inside it alone.
func renameIdents(e ast.Expr, sub map[string]string, shadowed map[string]bool) ast.Expr {
	switch x := e.(type) {
	case *ast.Ident:
		if !shadowed[x.Name] {
			if to, ok := sub[x.Name]; ok {
				return &ast.Ident{Name: to, Pos: x.Pos}
			}
		}
		return x
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: x.Op, X: renameIdents(x.X, sub, shadowed), Pos: x.Pos}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{Op: x.Op,
			Left:  renameIdents(x.Left, sub, shadowed),
			Right: renameIdents(x.Right, sub, shadowed), Pos: x.Pos}
	case *ast.FieldAccess:
		return &ast.FieldAccess{Target: renameIdents(x.Target, sub, shadowed), Field: x.Field, Pos: x.Pos}
	case *ast.CallExpr:
		args := make([]ast.Expr, len(x.Args))
		for i, a := range x.Args {
			args[i] = renameIdents(a, sub, shadowed)
		}
		return &ast.CallExpr{Fn: x.Fn, Args: args, Pos: x.Pos}
	case *ast.CondExpr:
		return &ast.CondExpr{
			Cond: renameIdents(x.Cond, sub, shadowed),
			Then: renameIdents(x.Then, sub, shadowed),
			Else: renameIdents(x.Else, sub, shadowed), Pos: x.Pos}
	case *ast.LetExpr:
		inner := shadowed
		if !shadowed[x.Name] {
			inner = maps.Clone(shadowed)
			if inner == nil {
				inner = map[string]bool{}
			}
			inner[x.Name] = true
		}
		return &ast.LetExpr{Name: x.Name,
			Value: renameIdents(x.Value, sub, shadowed),
			Body:  renameIdents(x.Body, sub, inner), Pos: x.Pos}
	default:
		return e
	}
}

// withPos copies a literal, carrying the position of the identifier it stands
// in for so an error inside a rewritten lambda still points at the source the
// user wrote.
func withPos(lit ast.Expr, pos token.Position) ast.Expr {
	switch x := lit.(type) {
	case *ast.IntLit:
		return &ast.IntLit{Value: x.Value, Pos: pos}
	case *ast.FloatLit:
		return &ast.FloatLit{Value: x.Value, Pos: pos}
	case *ast.BoolLit:
		return &ast.BoolLit{Value: x.Value, Pos: pos}
	case *ast.StringLit:
		return &ast.StringLit{Value: x.Value, Pos: pos}
	}
	return lit
}

func exprPos(e ast.Expr) token.Position {
	switch x := e.(type) {
	case *ast.IntLit:
		return x.Pos
	case *ast.FloatLit:
		return x.Pos
	case *ast.BoolLit:
		return x.Pos
	case *ast.StringLit:
		return x.Pos
	case *ast.Ident:
		return x.Pos
	case *ast.UnaryExpr:
		return x.Pos
	case *ast.BinaryExpr:
		return x.Pos
	case *ast.FieldAccess:
		return x.Pos
	case *ast.CallExpr:
		return x.Pos
	case *ast.CondExpr:
		return x.Pos
	case *ast.LetExpr:
		return x.Pos
	}
	return token.Position{}
}

// ---------------------------------------------------------------------------
// The Bind node
// ---------------------------------------------------------------------------

// bindNode wraps the nodes a binding's scope covers. Computing the values here
// rather than inside each wrapped node is what gives the scope its shape: they
// are computed once, from the value entering the scope, and taken back out
// again on the way out however the body ended.
func bindNode(rts []*runtimeBind, body []*ir.Node, in, out *ir.Type, pos token.Position) *ir.Node {
	names := make([]string, len(rts))
	binds := make([]ir.Binding, len(rts))
	sub := make([][]*ir.Node, 0, len(rts))
	for i, rt := range rts {
		names[i] = rt.name
		binds[i] = rt
		if rt.body != nil {
			sub = append(sub, rt.body.BlockNodes())
		}
	}
	return &ir.Node{
		Prim:    "Consider",
		In:      in,
		Out:     out,
		Display: "Consider " + strings.Join(names, ", "),
		Pos:     pos,
		Meta: map[string]any{
			ir.MetaBinds:     binds,
			ir.MetaBindNodes: sub,
			"nodes":          body,
		},
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			pushed := 0
			defer func() { eval.PopBindings(pushed) }()
			for _, rt := range rts {
				val, err := rt.value(v)
				if err != nil {
					return nil, err
				}
				eval.PushBinding(rt.name, val, rt.typ)
				pushed++
			}
			return runBody(ctx, body, v)
		},
	}
}

// runtimeBind implements ir.Binding, the view of a binding the compiler
// backend reads off the node (the interpreter uses the fields directly).

// Name is the name the binding is read by.
func (b *runtimeBind) Name() string { return b.name }

// Type is the type of the value it binds.
func (b *runtimeBind) Type() *ir.Type { return b.typ }

// In is the pipeline type the value is computed from.
func (b *runtimeBind) In() *ir.Type { return b.in }

// Lambda is the `Of` lambda applied to the current value, or nil. The
// untyped nil matters: a typed nil in an interface is not nil, and the
// backend decides which of the three forms this is by asking.
func (b *runtimeBind) Lambda() any {
	if b.lam == nil {
		return nil
	}
	return b.lam
}

// Expr is the `As` expression computed when the scope opens, or nil.
func (b *runtimeBind) Expr() any {
	if b.expr == nil {
		return nil
	}
	return b.expr
}

// Pos is where the binding was written.
func (b *runtimeBind) Pos() token.Position { return b.pos }

// BlockNodes is the resolved sub-pipeline behind an `Of` operation or body,
// or nil.
func (b *runtimeBind) BlockNodes() []*ir.Node {
	if b.body == nil {
		return nil
	}
	return b.body.BlockNodes()
}
