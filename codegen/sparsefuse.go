package codegen

import (
	"domain/ast"
	"domain/ir"
	"domain/optimizer"
)

// fuseSparseFilter recognises the tail of every counting automaton:
//
//	Convert To Sparse Grid   (a Map<(Int,Int),V> becomes a plane)
//	Find Cells (pred)        (the points whose value passes, sorted row-major)
//	Convert To Sparse Grid   (those points become marked cells)
//
// and lowers it to one walk over the map. The middle plane and the point list
// exist only to be read back: the result is a set of marked points, and a set
// has no order, so the row-major sort Find Cells pays for is dropped too.
//
// It stands down unless the predicate is pure and total — the fused walk visits
// cells in the map's insertion order, and the unfused one in sorted order, so a
// predicate that could fail or write would report differently — and unless the
// two conversions carry literals rather than measured lambdas, which have a
// value to read that the fused walk no longer builds.
func (g *gen) fuseSparseFilter(nodes []*ir.Node, in string) (int, string, bool) {
	if len(nodes) < 3 {
		return 0, "", false
	}
	conv, find, mark := nodes[0], nodes[1], nodes[2]
	if conv.Prim != "Convert To Sparse Grid" || find.Prim != "Find Cells" || mark.Prim != "Convert To Sparse Grid" {
		return 0, "", false
	}
	if src, _ := conv.Meta["source"].(string); src != "map" {
		return 0, "", false
	}
	if src, _ := mark.Meta["source"].(string); src != "points" {
		return 0, "", false
	}
	if find.In == nil || find.In.Kind != ir.KSparse || mark.In == nil || mark.In.Elem == nil ||
		mark.In.Elem.Kind != ir.KTuple {
		return 0, "", false
	}
	if hasMeasured(conv, "default") || hasMeasured(mark, "default", "mark") {
		return 0, "", false
	}
	lam, err := try1(func() *ast.Lambda { return g.nodeLambda(find) })
	if err != nil {
		return 0, "", false
	}
	if pure, total := optimizer.Elementwise(lam, find.In.Elem); !pure || !total {
		return 0, "", false
	}

	elemGo := asUnsupported1(mark, "", func() string { return g.goType(mark.Out.Elem) })
	def := g.measuredLit(mark, in, "default", mark.In, mark.Out.Elem, sparseLit)
	markLit := g.measuredLit(mark, in, "mark", mark.In, mark.Out.Elem, sparseLit)
	g.helper("dmSparse", declSparse, "slices")

	e, k, ix, v := g.fresh("e"), g.fresh("k"), g.fresh("i"), g.fresh("v")
	body, _ := asUnsupported2(find, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: find.In.Elem}})
	})
	g.wl("%s := dmNewSparse[%s](%s)", v, elemGo, def)
	g.wl("for %s, %s := range %s.keys {", ix, k, in)
	g.in()
	g.wl("%s := %s.vs[%s]", e, in, ix)
	g.wl("if %s {", body)
	g.in()
	g.wl("%s.put(%s.f0, %s.f1, %s)", v, k, k, markLit)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return 3, v, true
}
