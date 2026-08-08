package codegen

import (
	"domain/ast"
	"domain/ir"
)

// emitExplore lowers `Domain Expansion: Explore` — breadth-first search over
// the implicit graph a successor lambda describes, mirroring
// prims/explore.go's runExplore exactly.
//
// The state type is keyable, so its Go representation is comparable and the
// visited set is a plain map — no hashing helper needed. The queue is a slice
// with a head index rather than a dmQueue: the search appends every state at
// most once (that is what the visited set guarantees), so the backing array
// grows to the number of distinct states and no more.
func (g *gen) emitExplore(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	stateT, _ := n.Meta["state"].(*ir.Type)
	if stateT == nil {
		stateT = n.In
	}
	stateGo, err := g.goType(stateT)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	mode, _ := n.Meta["mode"].(string)
	// The weighted modes settle states in cost order and Tally folds the DAG
	// rather than walking it; neither is the plain queue below.
	switch mode {
	case "Cheapest", "Costs":
		return g.emitExploreCheapest(n, in, lam, stateT, stateGo, mode)
	case "Tally":
		return g.emitExploreTally(n, in, lam, stateT, stateGo)
	}

	cur := g.fresh("s")
	succ, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: cur, typ: stateT}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}

	seen, order, q, head, depth := g.fresh("seen"), g.fresh("order"), g.fresh("q"), g.fresh("head"), g.fresh("depth")
	steps, dist := g.fresh("steps"), g.fresh("dist")
	nxt, d := g.fresh("nx"), g.fresh("d")

	g.wl("%s := map[%s]bool{%s: true}", seen, stateGo, in)
	g.wl("%s := []%s{%s}", order, stateGo, in)
	g.wl("%s := []%s{%s}", q, stateGo, in)
	g.wl("%s := []int64{0}", depth)
	g.wl("%s := 0", head)
	// -1 is the "never reached" sentinel, matching Find Index. Only Mode: Steps
	// returns it, so it is blanked for the other modes rather than omitted —
	// the Until: pruning branch below writes to it either way.
	g.wl("%s := int64(-1)", steps)
	g.wl("_ = %s", steps)
	needDist := mode == "Distances"
	if needDist {
		g.helper("dmMap", declMap)
		g.wl("%s := dmNewMap[%s, int64]()", dist, stateGo)
		g.wl("%s.put(%s, 0)", dist, in)
	}

	// The seed can satisfy Until: on its own, in which case the answer is zero
	// steps and nothing is ever expanded.
	hasUntil, seedHit, err := g.exploreUntil(n, in, stateT)
	if err != nil {
		return "", err
	}
	done := g.fresh("done")
	g.wl("%s := false", done)
	if hasUntil {
		g.wl("if %s {", seedHit)
		g.in()
		g.wl("%s = 0", steps)
		g.wl("%s = true", done)
		g.out()
		g.wl("}")
	}

	g.wl("for !%s && %s < len(%s) {", done, head, q)
	g.in()
	g.wl("%s := %s[%s]", cur, q, head)
	g.wl("%s := %s[%s]", d, depth, head)
	g.wl("_ = %s", d)
	g.wl("%s++", head)
	g.wl("for _, %s := range %s {", nxt, succ)
	g.in()
	g.wl("if %s[%s] { continue }", seen, nxt)
	g.wl("%s[%s] = true", seen, nxt)
	g.wl("%s = append(%s, %s)", order, order, nxt)
	if needDist {
		g.wl("%s.put(%s, %s+1)", dist, nxt, d)
	}
	if hasUntil {
		_, hit, err := g.exploreUntil(n, nxt, stateT)
		if err != nil {
			return "", err
		}
		g.wl("if %s {", hit)
		g.in()
		// Until: prunes — the satisfying state is recorded but never expanded.
		g.wl("if %s < 0 { %s = %s + 1 }", steps, steps, d)
		if mode == "Steps" {
			g.wl("%s = true", done)
			g.wl("break")
		} else {
			g.wl("continue")
		}
		g.out()
		g.wl("}")
	}
	g.wl("%s = append(%s, %s)", q, q, nxt)
	g.wl("%s = append(%s, %s+1)", depth, depth, d)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")

	switch mode {
	case "Count":
		v := g.fresh("v")
		g.wl("%s := int64(len(%s))", v, order)
		return v, nil
	case "Steps":
		return steps, nil
	case "Distances":
		return dist, nil
	}
	return order, nil
}

// exploreUntil compiles the Until: predicate against the given state
// expression. It returns ok=false when the node has no Until:.
func (g *gen) exploreUntil(n *ir.Node, state string, stateT *ir.Type) (bool, string, error) {
	lam, ok := n.Meta["until"].(*ast.Lambda)
	if !ok || lam == nil {
		return false, "", nil
	}
	g.bindAmbientParams(lam)
	expr, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: state, typ: stateT}})
	if err != nil {
		return false, "", unsupported(n, "Until: %v", err)
	}
	return true, expr, nil
}

// emitExploreCheapest lowers Mode: Cheapest and Mode: Costs — Dijkstra over
// the implicit graph, mirroring prims/explore.go's cheapest exactly.
//
// The difference from the queue above is where a state is *settled*: on pop,
// in cost order, rather than on first sight. That is why Cost: must be
// non-negative, and why the heap needs the same insertion-order tiebreak
// ir.PQ has — Mode: Costs renders its Map in settle order.
func (g *gen) emitExploreCheapest(n *ir.Node, in string, lam *ast.Lambda,
	stateT *ir.Type, stateGo, mode string) (string, error) {

	cur := g.fresh("s")
	succ, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: cur, typ: stateT}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	settled, best, q := g.fresh("settled"), g.fresh("best"), g.fresh("q")
	costs, out, c := g.fresh("costs"), g.fresh("v"), g.fresh("c")
	nxt, w, nc, prev, ok := g.fresh("nx"), g.fresh("w"), g.fresh("nc"), g.fresh("prev"), g.fresh("ok")
	done := g.fresh("done")

	g.helper("dmPQ", declPQ)
	g.wl("%s := map[%s]bool{}", settled, stateGo)
	g.wl("%s := map[%s]int64{%s: 0}", best, stateGo, in)
	g.wl("var %s dmPQ[%s]", q, stateGo)
	g.wl("%s.push(%s, 0)", q, in)
	needCosts := mode == "Costs"
	if !needCosts {
		// -1 is the "never reached" sentinel Mode: Steps already uses.
		g.wl("%s := int64(-1)", out)
	}
	if needCosts {
		g.helper("dmMap", declMap)
		g.wl("%s := dmNewMap[%s, int64]()", costs, stateGo)
	}
	g.wl("%s := false", done)
	g.wl("for !%s {", done)
	g.in()
	g.wl("%s, %s, %s := %s.pop()", cur, c, ok, q)
	g.wl("if !%s { break }", ok)
	g.wl("if %s[%s] { continue }", settled, cur)
	g.wl("%s[%s] = true", settled, cur)
	if needCosts {
		g.wl("%s.put(%s, %s)", costs, cur, c)
	}
	hasUntil, hit, err := g.exploreUntil(n, cur, stateT)
	if err != nil {
		return "", err
	}
	if hasUntil {
		g.wl("if %s {", hit)
		g.in()
		// States settle in cost order, so the first hit is the cheapest one.
		if mode == "Cheapest" {
			g.wl("%s = %s", out, c)
			g.wl("%s = true", done)
			g.wl("break")
		} else {
			g.wl("continue")
		}
		g.out()
		g.wl("}")
	}
	g.wl("for _, %s := range %s {", nxt, succ)
	g.in()
	g.wl("if %s[%s] { continue }", settled, nxt)
	cost, err := g.exploreCost(n, cur, nxt, stateT)
	if err != nil {
		return "", err
	}
	// Declared rather than inferred: a Cost: of `1` compiles to an untyped Go
	// literal, which := would make an int.
	g.wl("var %s int64 = %s", w, cost)
	g.helper("dmFail", declFail, "fmt", "os")
	g.wl("if %s < 0 {", w)
	g.in()
	g.wl(`dmFail("Cost: returned %%d — a negative cost has no cheapest path to settle on", %s)`, w)
	g.out()
	g.wl("}")
	g.wl("%s := %s + %s", nc, c, w)
	g.wl("if %s, %s := %s[%s]; %s && %s <= %s { continue }", prev, ok, best, nxt, ok, prev, nc)
	g.wl("%s[%s] = %s", best, nxt, nc)
	g.wl("%s.push(%s, %s)", q, nxt, nc)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	if needCosts {
		return costs, nil
	}
	return out, nil
}

// exploreCost compiles the Cost: lambda against the edge being relaxed. The
// 1-parameter form is the cost of entering `to`; the 2-parameter form is the
// cost of the edge, so it binds `from` as well.
func (g *gen) exploreCost(n *ir.Node, from, to string, stateT *ir.Type) (string, error) {
	lam, _ := n.Meta["cost"].(*ast.Lambda)
	if lam == nil {
		return "", unsupported(n, "a weighted Explore has no Cost: lambda")
	}
	arity, _ := n.Meta["costArity"].(int)
	g.bindAmbientParams(lam)
	env := exprEnv{lam.Params[0]: {expr: to, typ: stateT}}
	if arity == 2 {
		env = exprEnv{
			lam.Params[0]: {expr: from, typ: stateT},
			lam.Params[1]: {expr: to, typ: stateT},
		}
	}
	expr, _, err := g.compileExpr(lam.Body, env)
	if err != nil {
		return "", unsupported(n, "Cost: %v", err)
	}
	return expr, nil
}

// emitExploreTally lowers Mode: Tally — a memoized post-order fold over the
// reachable DAG, mirroring prims/explore.go's tally.
//
// The walk is an explicit stack rather than Go recursion for the same reason
// the interpreter's is: a deep DAG should be bounded by the heap, not by how
// much stack a goroutine will grow to.
func (g *gen) emitExploreTally(n *ir.Node, in string, lam *ast.Lambda,
	stateT *ir.Type, stateGo string) (string, error) {

	valueLam, _ := n.Meta["value"].(*ast.Lambda)
	combineLam, _ := n.Meta["combine"].(*ast.Lambda)
	if valueLam == nil || combineLam == nil {
		return "", unsupported(n, "Mode: Tally has no Value:/Combine: lambda")
	}
	valGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	cur := g.fresh("s")
	succ, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: cur, typ: stateT}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.bindAmbientParams(valueLam)
	valExpr, _, err := g.compileExpr(valueLam.Body, exprEnv{valueLam.Params[0]: {expr: cur, typ: stateT}})
	if err != nil {
		return "", unsupported(n, "Value: %v", err)
	}
	acc, other := g.fresh("acc"), g.fresh("b")
	g.bindAmbientParams(combineLam)
	combExpr, _, err := g.compileExpr(combineLam.Body, exprEnv{
		combineLam.Params[0]: {expr: acc, typ: n.Out},
		combineLam.Params[1]: {expr: other, typ: n.Out},
	})
	if err != nil {
		return "", unsupported(n, "Combine: %v", err)
	}

	frame, memo, kids, onStack := g.fresh("frame"), g.fresh("memo"), g.fresh("kids"), g.fresh("onStack")
	stack, f, ok, i, k := g.fresh("stack"), g.fresh("f"), g.fresh("ok"), g.fresh("i"), g.fresh("ks")
	g.helper("dmFail", declFail, "fmt", "os")
	// The cycle message names the state, like Topological Sort's names the
	// blocked node — "there is a cycle" alone leaves it to be found by hand.
	stateFmt, err := g.scalarFmt(cur, stateT)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.wl("type %s struct { v %s; expanded bool }", frame, stateGo)
	g.wl("%s := map[%s]%s{}", memo, stateGo, valGo)
	g.wl("%s := map[%s][]%s{}", kids, stateGo, stateGo)
	g.wl("%s := map[%s]bool{}", onStack, stateGo)
	g.wl("%s := []%s{{v: %s}}", stack, frame, in)
	g.wl("for len(%s) > 0 {", stack)
	g.in()
	g.wl("%s := %s[len(%s)-1]", f, stack, stack)
	g.wl("%s := %s.v", cur, f)
	g.wl("if !%s.expanded {", f)
	g.in()
	g.wl("if _, %s := %s[%s]; %s { %s = %s[:len(%s)-1]; continue }", ok, memo, cur, ok, stack, stack, stack)
	g.wl("if %s[%s] {", onStack, cur)
	g.in()
	g.wl(`dmFail("Mode: Tally needs an acyclic search, but %%s is reachable from itself", %s)`,
		stateFmt)
	g.out()
	g.wl("}")
	g.wl("%s[%s] = true", onStack, cur)
	g.wl("%s[len(%s)-1].expanded = true", stack, stack)
	// Until: marks a leaf: a satisfying state is never expanded, so it
	// contributes its Value: and stops.
	hasUntil, hit, err := g.exploreUntil(n, cur, stateT)
	if err != nil {
		return "", err
	}
	g.wl("var %s []%s", k, stateGo)
	if hasUntil {
		g.wl("if !(%s) { %s = %s }", hit, k, succ)
	} else {
		g.wl("%s = %s", k, succ)
	}
	g.wl("%s[%s] = %s", kids, cur, k)
	g.wl("for %s := len(%s) - 1; %s >= 0; %s-- {", i, k, i, i)
	g.in()
	g.wl("%s = append(%s, %s{v: %s[%s]})", stack, stack, frame, k, i)
	g.out()
	g.wl("}")
	g.wl("continue")
	g.out()
	g.wl("}")
	g.wl("%s = %s[:len(%s)-1]", stack, stack, stack)
	g.wl("%s[%s] = false", onStack, cur)
	g.wl("if _, %s := %s[%s]; %s { continue }", ok, memo, cur, ok)
	g.wl("%s := %s[%s]", k, kids, cur)
	g.wl("if len(%s) == 0 {", k)
	g.in()
	g.wl("%s[%s] = %s", memo, cur, valExpr)
	g.wl("continue")
	g.out()
	g.wl("}")
	g.wl("%s := %s[%s[0]]", acc, memo, k)
	g.wl("for _, %s := range %s[1:] {", i, k)
	g.in()
	g.wl("%s := %s[%s]", other, memo, i)
	g.wl("%s = %s", acc, combExpr)
	g.out()
	g.wl("}")
	g.wl("%s[%s] = %s", memo, cur, acc)
	g.out()
	g.wl("}")
	v := g.fresh("v")
	g.wl("%s := %s[%s]", v, memo, in)
	return v, nil
}
