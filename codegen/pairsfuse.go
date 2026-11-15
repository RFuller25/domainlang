package codegen

import (
	"domain/ast"
	"domain/ir"
	"domain/optimizer"
)

// emitPairsCount lowers Pairs + Count Matching. It reports ok=false, emitting
// nothing, when the predicate is not pure (a write inside it would have to be
// ordered against the construction of pairs that no longer happens).
func (g *gen) emitPairsCount(pairs, count *ir.Node, in string) (string, bool) {
	lam, err := try1(func() *ast.Lambda { return g.nodeLambda(count) })
	if err != nil {
		return "", false
	}
	if pure, _ := optimizer.Elementwise(lam, count.In.Elem); !pure {
		return "", false
	}
	tupGo := asUnsupported1(pairs, "", func() string { return g.goType(pairs.Out.Elem) })
	v, i, e := g.fresh("v"), g.fresh("i"), g.fresh("e")
	body, _ := asUnsupported2(count, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: count.In.Elem}})
	})
	g.wl("var %s int64", v)
	g.wl("for %s := 0; %s+1 < len(%s); %s++ {", i, i, in, i)
	g.in()
	g.wl("%s := %s{%s[%s], %s[%s+1]}", e, tupGo, in, i, in, i)
	g.wl("if %s {", body)
	g.in()
	g.wl("%s++", v)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, true
}
