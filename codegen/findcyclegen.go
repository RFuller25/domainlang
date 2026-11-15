package codegen

import (
	"fmt"

	"domain/ast"
	"domain/ir"
)

// emitFindCycleState lowers Find Cycle's step form (prims/findcycle.go): step
// from the start state, keying each state after the step, until one was seen.
func (g *gen) emitFindCycleState(n *ir.Node, in string) string {
	step := g.nodeLambda(n)
	stateGo := asUnsupported1(n, "", func() string { return g.goType(n.In) })
	keyType, _ := n.Meta["keyType"].(*ir.Type)
	if keyType == nil {
		keyType = n.In
	}
	keyGo := asUnsupported1(n, "", func() string { return g.goType(keyType) })
	pt := asUnsupported1(n, "", func() string { return g.pointGo() })
	_, hasLimit := n.Meta["limit"]
	if !hasLimit {
		_, hasLimit = n.Meta["limitExpr"]
	}
	bound := fmt.Sprintf("int64(%d)", maxUnfoldElements)
	if hasLimit {
		bound = g.measuredOperand(n, in, "limit", "Limit", 0)
	}
	v, seen, cur, i, k := g.fresh("v"), g.fresh("seen"), g.fresh("cur"), g.fresh("i"), g.fresh("k")
	stepBody, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(step.Body, exprEnv{step.Params[0]: {expr: cur, typ: n.In}})
	})
	keyBody := cur
	if key, _ := n.Meta["key"].(*ast.Lambda); key != nil {
		g.bindAmbientParams(key)
		keyBody, _ = asUnsupported2(n, "Key: lambda: ", func() (string, *ir.Type) {
			return g.compileExpr(key.Body, exprEnv{key.Params[0]: {expr: cur, typ: n.In}})
		})
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.wl("%s := %s{-1, 0}", v, pt)
	// A key holding a List is not a Go map key; it is keyed by its injective
	// byte encoding instead (keyfn.go), exactly as the list form is.
	hashed, hashedGo := asUnsupported2(n, "", func() (string, string) { return g.hashKey(keyType, k, keyGo) })
	g.wl("%s := map[%s]int64{}", seen, hashedGo)
	g.wl("var %s %s = %s", cur, stateGo, in)
	g.wl("for %s := int64(0); ; %s++ {", i, i)
	g.in()
	g.wl("if %s >= %s {", i, bound)
	g.in()
	if !hasLimit {
		g.wl(`dmFail("Find Cycle: no state repeated within %d steps (non-terminating step? add Limit:)")`, maxUnfoldElements)
	}
	g.wl("break")
	g.out()
	g.wl("}")
	g.wl("%s = %s", cur, stepBody)
	g.wl("%s := %s", k, keyBody)
	g.wl("if at, ok := %s[%s]; ok {", seen, hashed)
	g.in()
	g.wl("%s = %s{at, %s - at}", v, pt, i)
	g.wl("break")
	g.out()
	g.wl("}")
	g.wl("%s[%s] = %s", seen, hashed, i)
	g.out()
	g.wl("}")
	return v
}
