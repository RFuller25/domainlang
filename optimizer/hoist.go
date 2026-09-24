package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/format"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Loop-invariant code motion for stage lambdas.
//
// A stage's Using: lambda runs once per element. A part of its body that
// mentions none of the lambda's parameters gives the same answer every time,
// and when that part is real work — sorting a list, building a set, reading a
// map's keys — the stage does it n times to use it once:
//
//	Consider targets Of Itself
//	...
//	Filter Using: (x) -> contains(sort(targets), x)     sort runs per element
//
// hoistInvariants computes such a subexpression once, when the stage starts,
// by wrapping the stage in a Consider node that binds it, and rewrites the
// body to read the binding. It is the Consider the program could have
// written, synthesized; both backends already run and compile that shape.
//
// Membership gets one step more. `contains(xs, v)` over a List scans the whole
// list for every element; over a Set it is one lookup, and the answer is the
// same — contains requires a keyable element type, which is exactly what a Set
// holds. So an invariant List on the left of a per-element contains is bound
// as toset(xs) instead: O(n·m) becomes O(n + m).
//
// What may move is narrow, and each rule is a way the program could otherwise
// tell that it moved:
//
//   - the expression must be total. A stage over an empty list never evaluated
//     it; hoisted, it runs once regardless, and an expression that can fail
//     would fail where the program used not to. Totality also covers the lazy
//     `if` arms and `and`/`or`: a total expression evaluated one extra time is
//     only slower, never wrong;
//   - it must not draw randomness or read a name anything writes: a moved
//     draw consumes the stream in a different place, and a moved read of a
//     `:=` target reads it at a different moment;
//   - the stage's lambda must be held by that stage alone. The binding is
//     wrapped around this one node, so a second node reading the same body
//     would read a name nothing binds;
//   - its type must be known here — the backends need it to bind the value.
//
// What could not move for one of these reasons is reported by
// InvariantStandDowns, so the program can be told what it is paying.
func hoistInvariants(p *ir.Pipeline) []Rewrite {
	h := newHoister(p)
	var rewrites []Rewrite
	h.walk(p.Nodes, typecheck.Env{}, func(list []*ir.Node, i int, lam *ast.Lambda, found []invariant, env typecheck.Env) {
		n := list[i]
		var binds []*hoisted
		var hoistedDesc []string
		for _, f := range found {
			if f.reason != "" {
				continue
			}
			name := fmt.Sprintf("hoisted#%d", h.next)
			h.next++
			value := f.expr
			if f.set {
				value = &ast.CallExpr{Fn: &ast.Ident{Name: "toset", Pos: f.pos}, Args: []ast.Expr{f.expr}, Pos: f.pos}
			}
			binds = append(binds, &hoisted{name: name, expr: value, typ: f.typ, in: n.In, pos: f.pos})
			*f.slot = &ast.Ident{Name: name, Pos: f.pos}
			if f.set {
				hoistedDesc = append(hoistedDesc, fmt.Sprintf(
					"Domain made `%s` a set once for %s's `contains`: each lookup is one hash probe instead of a scan of the list. Guaranteed hit.",
					format.Expr(f.expr), n.Prim))
			} else {
				hoistedDesc = append(hoistedDesc, fmt.Sprintf(
					"Domain hoisted `%s` out of %s's lambda: it does not depend on the element, so it is computed once for the stage instead of once per element. Guaranteed hit.",
					format.Expr(f.expr), n.Prim))
			}
		}
		if len(binds) == 0 {
			return
		}
		list[i] = hoistNode(n, binds)
		for _, d := range hoistedDesc {
			rewrites = append(rewrites, Rewrite{Message: d})
		}
	})
	return rewrites
}

// InvariantStandDown is invariant work a stage repeats per element that
// hoistInvariants could not move, and why.
type InvariantStandDown struct {
	Prim   string
	Pos    token.Position
	Expr   string // the subexpression, as Domain source
	Set    bool   // a List membership that would have become a Set
	Reason string // "can-fail", "untyped", "shared-lambda"
}

// InvariantStandDowns reports the per-element invariant work the pass left in
// place. It runs the pass's own analysis without rewriting anything, so what
// it says cannot drift from what the pass does.
func InvariantStandDowns(p *ir.Pipeline) []InvariantStandDown {
	h := newHoister(p)
	var out []InvariantStandDown
	h.walk(p.Nodes, typecheck.Env{}, func(list []*ir.Node, i int, lam *ast.Lambda, found []invariant, env typecheck.Env) {
		for _, f := range found {
			if f.reason == "" {
				continue
			}
			out = append(out, InvariantStandDown{
				Prim: list[i].Prim, Pos: f.pos, Expr: format.Expr(f.expr), Set: f.set, Reason: f.reason,
			})
		}
	})
	return out
}

// hoistCost is the builtins worth moving: each is linear or worse in the size
// of what it reads. Cheap lookups (length of a list, item, get) are not, and
// moving them would only add a binding.
var hoistCost = map[string]bool{
	"sort": true, "unique": true, "toset": true, "tomap": true, "reverse": true,
	"sum": true, "product": true, "min": true, "max": true,
	"keys": true, "values": true, "entries": true, "tolist": true,
	"flatten": true, "transpose": true, "concat": true, "zip": true, "enumerate": true,
	"chunk": true, "windows": true, "range": true, "fill": true,
	"chars": true, "split": true, "words": true, "textjoin": true, "replace": true,
	"occurrences": true, "repeat": true, "upper": true, "lower": true,
	"indexof": true, "contains": true,
	"graph": true, "nodes": true, "edges": true, "roots": true, "leaves": true,
	"reachable": true, "hascycle": true, "flipedges": true, "undirected": true,
	"subgraph": true, "mergegraphs": true, "weightsum": true, "cellpoints": true,
	"row": true, "col": true,
}

// invariant is one maximal subexpression of a lambda body that does not
// depend on the lambda's parameters.
type invariant struct {
	expr   ast.Expr
	slot   *ast.Expr // where it sits, so it can be replaced in place
	pos    token.Position
	typ    *ir.Type
	set    bool   // bind toset(expr) and keep contains reading the Set
	reason string // why it cannot move; "" when it can
}

type hoisted struct {
	name string
	expr ast.Expr
	typ  *ir.Type
	in   *ir.Type
	pos  token.Position
}

type hoister struct {
	written map[string]bool     // every name a `:=` anywhere writes
	uses    map[*ast.Lambda]int // how many node Meta slots hold each lambda
	next    int
}

func newHoister(p *ir.Pipeline) *hoister {
	h := &hoister{written: map[string]bool{}, uses: map[*ast.Lambda]int{}}
	for _, list := range nodeLists(p) {
		for _, n := range list {
			for _, v := range n.Meta {
				if lam, ok := v.(*ast.Lambda); ok && lam != nil {
					h.uses[lam]++
					ast.UpdatedNames(lam.Body, h.written)
				}
			}
		}
	}
	return h
}

// walk visits every stage lambda that runs once per element, with the types
// of the Consider bindings in scope at it.
func (h *hoister) walk(list []*ir.Node, env typecheck.Env, visit func([]*ir.Node, int, *ast.Lambda, []invariant, typecheck.Env)) {
	for i := 0; i < len(list); i++ {
		n := list[i]
		inner := env
		if binds, ok := n.Meta[ir.MetaBinds].([]ir.Binding); ok {
			inner = make(typecheck.Env, len(env)+len(binds))
			for k, t := range env {
				inner[k] = t
			}
			for _, b := range binds {
				inner[b.Name()] = b.Type()
			}
		}
		if sub, _ := n.Meta["nodes"].([]*ir.Node); sub != nil {
			h.walk(sub, inner, visit)
		}
		// Apply's lambda runs once over the whole value, not per element: it
		// has nothing to hoist out of.
		if n.Prim == "Apply" {
			continue
		}
		lam, _ := n.Meta["lambda"].(*ast.Lambda)
		if lam == nil || len(lam.Params) == 0 || effectful(lam) {
			continue
		}
		if _, isBlock := lam.Body.(*ast.BlockBody); isBlock {
			continue
		}
		varying := map[string]bool{}
		for _, prm := range lam.Params {
			varying[prm] = true
		}
		var found []invariant
		h.collect(&lam.Body, varying, env, &found)
		if len(found) == 0 {
			continue
		}
		if h.uses[lam] != 1 {
			for j := range found {
				if found[j].reason == "" {
					found[j].reason = "shared-lambda"
				}
			}
		}
		visit(list, i, lam, found, env)
	}
}

// collect finds the maximal invariant subexpressions worth moving under slot.
func (h *hoister) collect(slot *ast.Expr, varying map[string]bool, env typecheck.Env, out *[]invariant) {
	e := *slot
	// `contains(xs, v)` with an invariant List xs and a varying v: bind the
	// List as a Set.
	if c, ok := e.(*ast.CallExpr); ok && builtinName(c) == "contains" && len(c.Args) == 2 &&
		h.invariant(c.Args[0], varying) && !h.invariant(c.Args[1], varying) {
		if t, err := typecheck.ExprType(c.Args[0], env); err == nil && t != nil && t.Kind == ir.KList && ir.Keyable(t.Elem) {
			inv := h.candidate(&c.Args[0], env)
			inv.set, inv.typ = true, ir.Set(t.Elem)
			*out = append(*out, inv)
			h.collect(&c.Args[1], varying, env, out)
			return
		}
	}
	if h.invariant(e, varying) && costly(e) {
		*out = append(*out, h.candidate(slot, env))
		return
	}
	switch x := e.(type) {
	case *ast.UnaryExpr:
		h.collect(&x.X, varying, env, out)
	case *ast.BinaryExpr:
		h.collect(&x.Left, varying, env, out)
		h.collect(&x.Right, varying, env, out)
	case *ast.FieldAccess:
		h.collect(&x.Target, varying, env, out)
	case *ast.CallExpr:
		for i := range x.Args {
			h.collect(&x.Args[i], varying, env, out)
		}
	case *ast.CondExpr:
		h.collect(&x.Cond, varying, env, out)
		h.collect(&x.Then, varying, env, out)
		h.collect(&x.Else, varying, env, out)
	case *ast.LetExpr:
		h.collect(&x.Value, varying, env, out)
		inner := make(map[string]bool, len(varying)+1)
		for k := range varying {
			inner[k] = true
		}
		inner[x.Name] = true
		h.collect(&x.Body, inner, env, out)
	}
}

// candidate types an invariant subexpression and decides whether it can move.
func (h *hoister) candidate(slot *ast.Expr, env typecheck.Env) invariant {
	e := *slot
	inv := invariant{expr: e, slot: slot, pos: exprPos(e)}
	t, err := typecheck.ExprType(e, env)
	switch {
	case !isTotal(e):
		inv.reason = "can-fail"
	case err != nil || t == nil:
		inv.reason = "untyped"
	default:
		inv.typ = t
	}
	return inv
}

// invariant reports whether e reads none of the varying names, draws no
// randomness, and reads nothing a `:=` writes.
func (h *hoister) invariant(e ast.Expr, varying map[string]bool) bool {
	switch x := e.(type) {
	case *ast.IntLit, *ast.FloatLit, *ast.BoolLit, *ast.StringLit:
		return true
	case *ast.Ident:
		return !varying[x.Name] && !h.written[x.Name]
	case *ast.GlobalRef:
		return !x.Mutable
	case *ast.UnaryExpr:
		return h.invariant(x.X, varying)
	case *ast.BinaryExpr:
		return h.invariant(x.Left, varying) && h.invariant(x.Right, varying)
	case *ast.FieldAccess:
		return h.invariant(x.Target, varying)
	case *ast.CallExpr:
		if ast.HasNondeterminism(x) {
			return false
		}
		if _, ok := x.Fn.(*ast.Ident); !ok {
			return false
		}
		for _, a := range x.Args {
			if !h.invariant(a, varying) {
				return false
			}
		}
		return true
	case *ast.CondExpr:
		return h.invariant(x.Cond, varying) && h.invariant(x.Then, varying) && h.invariant(x.Else, varying)
	}
	// A consider, an update, or a node kind this walk does not know: not
	// provably invariant, so it stays where it was written.
	return false
}

// costly reports whether e calls a builtin worth computing only once.
func costly(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.CallExpr:
		if hoistCost[builtinName(x)] {
			return true
		}
		for _, a := range x.Args {
			if costly(a) {
				return true
			}
		}
	case *ast.UnaryExpr:
		return costly(x.X)
	case *ast.BinaryExpr:
		return costly(x.Left) || costly(x.Right)
	case *ast.FieldAccess:
		return costly(x.Target)
	case *ast.CondExpr:
		return costly(x.Cond) || costly(x.Then) || costly(x.Else)
	}
	return false
}

func builtinName(c *ast.CallExpr) string {
	if id, ok := c.Fn.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func exprPos(e ast.Expr) token.Position {
	switch x := e.(type) {
	case *ast.CallExpr:
		// A call's own position is its parenthesis; a reader looks for the
		// name in front of it.
		if id, ok := x.Fn.(*ast.Ident); ok && id.Pos.Line != 0 {
			return id.Pos
		}
		return x.Pos
	case *ast.Ident:
		return x.Pos
	case *ast.BinaryExpr:
		return x.Pos
	case *ast.UnaryExpr:
		return x.Pos
	case *ast.FieldAccess:
		return x.Pos
	case *ast.CondExpr:
		return x.Pos
	case *ast.GlobalRef:
		return x.Pos
	}
	return token.Position{}
}

// hoistNode wraps a stage in the Consider its hoisted values are bound by.
// The values are computed each time the stage starts — once per run at the
// top level, once per lap inside a loop body — from the bindings in scope
// there, exactly as a written `Consider x As …` is.
func hoistNode(n *ir.Node, binds []*hoisted) *ir.Node {
	ibinds := make([]ir.Binding, len(binds))
	for i, b := range binds {
		ibinds[i] = b
	}
	return &ir.Node{
		Prim:    "Consider",
		In:      n.In,
		Out:     n.Out,
		Display: "Consider (hoisted for " + n.Prim + ")",
		Pos:     n.Pos,
		Meta: map[string]any{
			ir.MetaBinds:     ibinds,
			ir.MetaBindNodes: [][]*ir.Node{},
			"nodes":          []*ir.Node{n},
		},
		Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			pushed := 0
			defer func() { eval.PopBindings(pushed) }()
			for _, b := range binds {
				env, types := eval.BindingEnv()
				val, err := eval.EvalExprTyped(b.expr, env, types)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: n.Prim, Pos: b.pos, Msg: err.Error()}
				}
				eval.PushBinding(b.name, val, b.typ)
				pushed++
			}
			return ir.EvalNode(ctx, n, v)
		},
	}
}

// hoisted implements ir.Binding: an `As` binding whose value is an expression.
func (b *hoisted) Name() string           { return b.name }
func (b *hoisted) Type() *ir.Type         { return b.typ }
func (b *hoisted) In() *ir.Type           { return b.in }
func (b *hoisted) Lambda() any            { return nil }
func (b *hoisted) Expr() any              { return b.expr }
func (b *hoisted) BlockNodes() []*ir.Node { return nil }
func (b *hoisted) Pos() token.Position    { return b.pos }
