package codegen

import "domain/ir"

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
	g.wl("%s := %s[0]", acc, in)
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

	seed, seeded := n.Meta["seed"]
	if seeded {
		switch s := seed.(type) {
		case int64:
			g.wl("%s := int64(%d)", acc, s)
		case string:
			g.wl("%s := %s", acc, goStr(s))
		default:
			return "", unsupported(n, "seed of type %T", s)
		}
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
	g.wl("%s := []%s{}", v, tupGo)
	g.wl("for %s := 0; %s+1 < len(%s); %s++ {", i, i, in, i)
	g.in()
	g.wl("%s = append(%s, %s{%s[%s], %s[%s+1]})", v, v, tupGo, in, i, in, i)
	g.out()
	g.wl("}")
	return v, nil
}
