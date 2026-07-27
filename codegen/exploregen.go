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
