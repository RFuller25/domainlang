package codegen

import (
	"domain/ast"
	"domain/ir"
)

// Go lowerings for the functional trio in prims/functional.go: Reduce (the
// seedless fold), Scan (the running fold), and Pairs (adjacent tuples). Each
// mirrors its interpreter twin exactly, including the empty-list behaviour —
// Reduce fails, Scan and Pairs return the empty list.

func (g *gen) emitReduce(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	acc, e := g.fresh("acc"), g.fresh("e")
	// The accumulator is an element, so it needs no declared type: it is
	// seeded straight from the list and the lambda maps T x T -> T.
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: acc, typ: n.Out},
		lam.Params[1]: {expr: e, typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.wl("if len(%s) == 0 {", in)
	g.in()
	g.wl(`dmFail("Reduce of an empty list is undefined")`)
	g.out()
	g.wl("}")
	g.wl("%s := %s", acc, g.ownAccumulator(lam, n.Out, in+"[0]"))
	g.wl("for _, %s := range %s[1:] {", e, in)
	g.in()
	g.wl("%s = %s", acc, body)
	g.out()
	g.wl("}")
	return acc, nil
}

func (g *gen) emitScan(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	accGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, acc := g.fresh("v"), g.fresh("acc")
	i, e := g.fresh("i"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: acc, typ: n.Out.Elem},
		lam.Params[1]: {expr: e, typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := make([]%s, len(%s))", v, accGo, in)

	_, hasLit := n.Meta["seed"]
	_, hasExpr := n.Meta["seedExpr"]
	if hasLit || hasExpr {
		seed, err := g.measuredLit(n, in, "seed", n.In, n.Out.Elem, seedLit)
		if err != nil {
			return "", err
		}
		g.wl("var %s %s = %s", acc, accGo, seed)
		g.wl("for %s, %s := range %s {", i, e, in)
		g.in()
		g.wl("%s = %s", acc, body)
		g.wl("%s[%s] = %s", v, i, acc)
		g.out()
		g.wl("}")
		return v, nil
	}

	// Seedless: the first element seeds the scan and is its own first result.
	g.wl("var %s %s", acc, accGo)
	g.wl("for %s, %s := range %s {", i, e, in)
	g.in()
	g.wl("if %s == 0 {", i)
	g.in()
	g.wl("%s = %s", acc, e)
	g.out()
	g.wl("} else {")
	g.in()
	g.wl("%s = %s", acc, body)
	g.out()
	g.wl("}")
	g.wl("%s[%s] = %s", v, i, acc)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitPairs(n *ir.Node, in string) (string, error) {
	tupGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, i := g.fresh("v"), g.fresh("i")
	// n elements give exactly n-1 pairs, so the backing array is sized once
	// here rather than grown a dozen times by append.
	g.wl("%s := make([]%s, 0, len(%s))", v, tupGo, in)
	g.wl("for %s := 0; %s+1 < len(%s); %s++ {", i, i, in, i)
	g.in()
	g.wl("%s = append(%s, %s{%s[%s], %s[%s+1]})", v, v, tupGo, in, i, in, i)
	g.out()
	g.wl("}")
	return v, nil
}

// ownAccumulator emits the one-time clone a fold makes when the optimizer
// marked any update in its lambda as in-place, and returns the expression the
// accumulator should be seeded from. It mirrors prims.ownAccumulator exactly,
// because the two backends have to agree about which programs make the copy.
//
// The analysis proves nothing *inside* the lambda reads the copied-from value
// after an update; it says nothing about who else holds the seed, and a Part
// or a Channel branches from one value. Without a marked update this is the
// identity, so the naive path keeps the allocation profile it had.
func (g *gen) ownAccumulator(lam *ast.Lambda, accT *ir.Type, seed string) string {
	if lam == nil || accT == nil || !ast.HasInPlace(lam.Body) {
		return seed
	}
	switch accT.Kind {
	case ir.KMap:
		g.helper("dmMap", declMap)
		g.helper("dmMapClone", declMapClone)
		return "dmMapClone(" + seed + ")"
	case ir.KSet:
		g.helper("dmSet", declSet)
		g.helper("dmSetClone", declSetClone)
		return "dmSetClone(" + seed + ")"
	case ir.KGrid:
		g.helper("dmGrid", declGrid)
		g.helper("dmGridClone", declGridClone)
		return "dmGridClone(" + seed + ")"
	case ir.KSparse:
		g.helper("dmSparse", declSparse, "slices")
		g.helper("dmSparseClone", declSparseClone)
		return "dmSparseClone(" + seed + ")"
	case ir.KList:
		// A List is a Go slice, so seed and accumulator would share a backing
		// array. `append(nil, …)` is the clone, and it is written inline
		// rather than as a helper because it needs the element type the
		// generator already has.
		elemGo, err := g.goType(accT.Elem)
		if err != nil {
			return seed
		}
		return "append([]" + elemGo + "(nil), " + seed + "...)"
	}
	return seed
}
