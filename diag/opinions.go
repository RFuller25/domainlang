package diag

import (
	"fmt"

	"domain/ast"
	"domain/format"
	"domain/ir"
	"domain/optimizer"
	"domain/token"
)

// The compiler's opinions: performance costs it found in the program and
// could not remove on its own.
//
// Everything the optimizer can rewrite, it rewrites, silently — `--explain`
// lists what it did. What is reported here is the rest: a known-slow spelling
// whose faster twin gives a different answer in some corner (a different
// error message on an empty list, say), or a rewrite the optimizer recognised
// and had to stand down. Each one says what the program is paying, what to
// write instead, and why the compiler did not make the change itself — the
// last part matters, because "the compiler could have done this" is the first
// thing a reader of a performance warning will wonder.
//
// These carry Code "perf". `domain run` and `domain build` print them; the
// rest of lint's findings are left to `domain expansion: lint`.

// lintOpinions reports the per-element costs the optimizer left in place. The
// pipeline must already have been through the optimizer (lintDeclinedInPlace
// runs it), so what is left is exactly what it could not move.
func lintOpinions(pipe *ir.Pipeline, add func(Diagnostic)) {
	if pipe == nil {
		return
	}
	for _, d := range optimizer.InvariantStandDowns(pipe) {
		add(invariantOpinion(d))
	}
	for _, n := range allNodes(pipe.Nodes) {
		for _, v := range n.Meta {
			if lam, ok := v.(*ast.Lambda); ok && lam != nil {
				walkExpr(lam.Body, func(e ast.Expr) { expressionOpinion(e, add) })
			}
		}
		scanOpinion(n, add)
	}
}

// invariantOpinion words one piece of per-element work the hoisting pass had
// to leave where it was.
func invariantOpinion(d optimizer.InvariantStandDown) Diagnostic {
	why := map[string]string{
		"can-fail": "the compiler moves work like this out of the loop itself only when it cannot fail. " +
			"This can — on an empty list, for one — and computed up front it would fail even when " +
			"the stage has no elements to run on",
		"shared-lambda": "the compiler moves work like this out of the loop itself, but this lambda is " +
			"shared with another stage (a Shikigami summoned twice, say), and one binding cannot cover both",
		"untyped": "the compiler moves work like this out of the loop itself, but from here it cannot see " +
			"the type of everything this reads",
	}[d.Reason]
	if d.Set {
		name := bindingName(d.Expr, "Set")
		return Diagnostic{
			Severity: Warning, Code: "perf", Pos: d.Pos, EndCol: d.Pos.Col + len(d.Expr),
			Msg: fmt.Sprintf("`contains` scans all of `%s` for every element of this %s — "+
				"O(n·m) where a set makes it O(n + m)", d.Expr, d.Prim),
			Help: fmt.Sprintf("build the set once for the stage — `Consider %s As toset(%s)` — "+
				"and ask `contains(%s, …)`: one lookup instead of a scan", name, d.Expr, name),
			Notes: []string{why},
		}
	}
	name := bindingName(d.Expr, "Once")
	return Diagnostic{
		Severity: Warning, Code: "perf", Pos: d.Pos, EndCol: d.Pos.Col + len(d.Expr),
		Msg: fmt.Sprintf("`%s` does not depend on the element, but this %s computes it again "+
			"for every one", d.Expr, d.Prim),
		Help: fmt.Sprintf("compute it once for the stage — `Consider %s As %s` — and use `%s`",
			name, d.Expr, name),
		Notes: []string{why},
	}
}

// bindingName suggests a name for a hoisted value: the list's own name with a
// suffix when there is one, else something neutral.
func bindingName(expr, suffix string) string {
	if isIdentText(expr) {
		return expr + suffix
	}
	if suffix == "Set" {
		return "members"
	}
	return "once"
}

func isIdentText(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// expressionOpinion flags a spelling that does more work than the answer
// needs and whose cheaper twin fails differently, so the compiler cannot
// swap one for the other on its own.
func expressionOpinion(e ast.Expr, add func(Diagnostic)) {
	outer, ok := e.(*ast.CallExpr)
	if !ok || len(outer.Args) == 0 {
		return
	}
	inner, ok := outer.Args[0].(*ast.CallExpr)
	if !ok || len(inner.Args) != 1 {
		return
	}
	xs := format.Expr(inner.Args[0])
	pos := outer.Pos
	if id, ok := outer.Fn.(*ast.Ident); ok && id.Pos.Line != 0 {
		pos = id.Pos
	}
	first := callName(outer) == "first" ||
		(callName(outer) == "item" && len(outer.Args) == 2 && isIntLit(outer.Args[1], 0))
	last := callName(outer) == "last"
	var better, what string
	switch callName(inner) {
	case "sort":
		switch {
		case first:
			better, what = "min("+xs+")", "the smallest element"
		case last:
			better, what = "max("+xs+")", "the largest element"
		}
		if better != "" {
			add(Diagnostic{
				Severity: Warning, Code: "perf", Pos: pos,
				EndCol: pos.Col + len(format.Expr(outer)),
				Msg: fmt.Sprintf("`%s` sorts the whole list to read %s — O(n log n) for an O(n) question",
					format.Expr(outer), what),
				Help: fmt.Sprintf("for a list of numbers, write `%s`", better),
				Notes: []string{"the compiler leaves this as written: on an empty list the two report " +
					"different errors, and it does not change what a program says when it fails"},
			})
		}
	case "reverse":
		switch {
		case first:
			better = "last(" + xs + ")"
		case last:
			better = "first(" + xs + ")"
		}
		if better != "" {
			add(Diagnostic{
				Severity: Warning, Code: "perf", Pos: pos,
				EndCol: pos.Col + len(format.Expr(outer)),
				Msg: fmt.Sprintf("`%s` copies the whole list, reversed, to read one end of it",
					format.Expr(outer)),
				Help: fmt.Sprintf("write `%s`", better),
				Notes: []string{"the compiler leaves this as written: on an empty list the two report " +
					"different errors, and it does not change what a program says when it fails"},
			})
		}
	}
}

// scanOpinion flags a pair or triple scan left at O(n²) or O(n³) only because
// its target is not a literal. The optimizer's complement scans are specialised
// on the number itself, so `(a, b) -> a + b = target` reads the same as
// `a + b = 2020` to a person and not to the pass.
func scanOpinion(n *ir.Node, add func(Diagnostic)) {
	if n.Prim != "All Pairs" && n.Prim != "Combinations" {
		return
	}
	if mode, _ := n.Meta["mode"].(string); mode != "First" && mode != "Count" {
		return
	}
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil || len(lam.Params) < 2 || len(lam.Params) > 3 {
		return
	}
	eq, ok := lam.Body.(*ast.BinaryExpr)
	if !ok || eq.Op != token.EQ {
		return
	}
	target := eq.Right
	if !sumOfParams(eq.Left, lam.Params) {
		target = eq.Left
		if !sumOfParams(eq.Right, lam.Params) {
			return
		}
	}
	if _, isLit := target.(*ast.IntLit); isLit || mentionsAny(target, lam.Params) {
		return
	}
	cost, fast := "O(n²)", "O(n)"
	if len(lam.Params) == 3 {
		cost, fast = "O(n³)", "O(n²)"
	}
	add(Diagnostic{
		Severity: Warning, Code: "perf", Pos: n.Pos,
		Msg: fmt.Sprintf("this %s tries every combination — %s — because its target `%s` is not a number "+
			"written in the program", n.Prim, cost, format.Expr(target)),
		Help: fmt.Sprintf("with the target written as an integer literal, the compiler runs this as an %s "+
			"hash scan instead; if the target is a constant, write it in", fast),
		Notes: []string{"the hash scans are specialised on the target at compile time, " +
			"so one that is only known when the program runs keeps the scan as written"},
	})
}

// sumOfParams reports whether e is a `+` chain whose leaves are exactly the
// given parameters, each once.
func sumOfParams(e ast.Expr, params []string) bool {
	var leaves []string
	var walk func(ast.Expr) bool
	walk = func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.BinaryExpr:
			return x.Op == token.PLUS && walk(x.Left) && walk(x.Right)
		case *ast.Ident:
			leaves = append(leaves, x.Name)
			return true
		}
		return false
	}
	if !walk(e) || len(leaves) != len(params) {
		return false
	}
	seen := map[string]bool{}
	for _, l := range leaves {
		seen[l] = true
	}
	for _, p := range params {
		if !seen[p] {
			return false
		}
	}
	return len(seen) == len(params)
}

func mentionsAny(e ast.Expr, names []string) bool {
	found := false
	walkExpr(e, func(x ast.Expr) {
		if id, ok := x.(*ast.Ident); ok {
			for _, n := range names {
				found = found || id.Name == n
			}
		}
	})
	return found
}

func callName(c *ast.CallExpr) string {
	if id, ok := c.Fn.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func isIntLit(e ast.Expr, v int64) bool {
	lit, ok := e.(*ast.IntLit)
	return ok && lit.Value == v
}

// walkExpr visits e and every subexpression of it.
func walkExpr(e ast.Expr, visit func(ast.Expr)) {
	if e == nil {
		return
	}
	visit(e)
	switch x := e.(type) {
	case *ast.UnaryExpr:
		walkExpr(x.X, visit)
	case *ast.BinaryExpr:
		walkExpr(x.Left, visit)
		walkExpr(x.Right, visit)
	case *ast.FieldAccess:
		walkExpr(x.Target, visit)
	case *ast.CallExpr:
		for _, a := range x.Args {
			walkExpr(a, visit)
		}
	case *ast.CondExpr:
		walkExpr(x.Cond, visit)
		walkExpr(x.Then, visit)
		walkExpr(x.Else, visit)
	case *ast.LetExpr:
		walkExpr(x.Value, visit)
		walkExpr(x.Body, visit)
	case *ast.AssignExpr:
		walkExpr(x.Value, visit)
	case *ast.AlsoExpr:
		walkExpr(x.Body, visit)
		for _, c := range x.Clauses {
			walkExpr(c, visit)
		}
	}
}

// allNodes flattens every node list in a pipeline: the top level, Channel,
// loop and Part bodies, and the sub-pipelines behind bindings.
func allNodes(nodes []*ir.Node) []*ir.Node {
	var out []*ir.Node
	var visit func([]*ir.Node)
	visit = func(list []*ir.Node) {
		for _, n := range list {
			out = append(out, n)
			if sub, _ := n.Meta["nodes"].([]*ir.Node); sub != nil {
				visit(sub)
			}
			for _, key := range []string{ir.MetaBindNodes, ir.MetaGlobalNodes} {
				if subs, _ := n.Meta[key].([][]*ir.Node); subs != nil {
					for _, s := range subs {
						visit(s)
					}
				}
			}
		}
	}
	visit(nodes)
	return out
}
