package codegen

import (
	"fmt"
	"strconv"

	"domain/ir"
)

// Sparse grid lowerings. A Sparse<T> compiles to dmSparse[T] — a
// map of set cells keyed by (row, col) with a default value and exact
// bounds, matching ir.SparseValue. Iteration is always through pts()
// (sorted row-major), the interpreter's canonical order, so rendering and
// lambda-error order stay byte-identical.

// sparseLit renders a Default:/Mark: literal (int64 or string, fixed at
// resolve time) as a typed Go expression.
func sparseLit(v any) (string, error) {
	switch x := v.(type) {
	case int64:
		return "int64(" + strconv.FormatInt(x, 10) + ")", nil
	case string:
		return goStr(x), nil
	default:
		return "", fmt.Errorf("unsupported sparse literal %T", v)
	}
}

func (g *gen) emitConvertToSparseGrid(n *ir.Node, in string) (string, error) {
	source, _ := n.Meta["source"].(string)
	def, err := g.measuredLit(n, in, "default", n.In, n.Out.Elem, sparseLit)
	if err != nil {
		return "", err
	}
	elemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmSparse", declSparse, "sort")
	v := g.fresh("v")
	g.wl("%s := dmNewSparse[%s](%s)", v, elemGo, def)

	switch source {
	case "grid":
		r, c, cell := g.fresh("r"), g.fresh("c"), g.fresh("cell")
		g.wl("for %s := 0; %s < %s.rows; %s++ {", r, r, in, r)
		g.in()
		g.wl("for %s := 0; %s < %s.cols; %s++ {", c, c, in, c)
		g.in()
		g.wl("%s := %s.cells[%s*%s.cols+%s]", cell, in, r, in, c)
		g.wl("if %s != %s {", cell, def)
		g.in()
		g.wl("%s.put(int64(%s), int64(%s), %s)", v, r, c, cell)
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
	case "map":
		k := g.fresh("k")
		g.wl("for _, %s := range %s.keys {", k, in)
		g.in()
		g.wl("%s.put(%s.f0, %s.f1, %s.vals[%s])", v, k, k, in, k)
		g.out()
		g.wl("}")
	case "points":
		mark, err := g.measuredLit(n, in, "mark", n.In, n.Out.Elem, sparseLit)
		if err != nil {
			return "", unsupported(n, "%v", err)
		}
		p := g.fresh("p")
		if n.In.Elem.Kind == ir.KTuple {
			g.wl("for _, %s := range %s {", p, in)
			g.in()
			g.wl("%s.put(%s.f0, %s.f1, %s)", v, p, p, mark)
			g.out()
			g.wl("}")
			break
		}
		// List<List<Int>> rows: the row length is a runtime property.
		g.helper("dmFail", declFail, "fmt", "os")
		i := g.fresh("i")
		g.wl("for %s, %s := range %s {", i, p, in)
		g.in()
		g.wl("if len(%s) != 2 {", p)
		g.in()
		g.wl(`dmFail("item %%d is not a point (need exactly two integers)", %s)`, i)
		g.out()
		g.wl("}")
		g.wl("%s.put(%s[0], %s[1], %s)", v, p, p, mark)
		g.out()
		g.wl("}")
	default:
		return "", unsupported(n, "source %q", source)
	}
	return v, nil
}

// emitSparseDensify lowers Convert To Grid over a Sparse<T>: the bounding
// box translated to (0, 0), default-filled, guarded by ir.MaxSparseDense.
func (g *gen) emitSparseDensify(n *ir.Node, in string) (string, error) {
	gridGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	elemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	v, rows, cols := g.fresh("v"), g.fresh("rows"), g.fresh("cols")
	i, k, e := g.fresh("i"), g.fresh("k"), g.fresh("e")
	lim := strconv.FormatInt(ir.DensifyLimit(), 10)
	g.wl("var %s %s", v, gridGo)
	g.wl("if len(%s.cells) > 0 {", in)
	g.in()
	g.wl("%s := %s.maxR - %s.minR + 1", rows, in, in)
	g.wl("%s := %s.maxC - %s.minC + 1", cols, in, in)
	// The tunable ceiling is gone, but a box Go cannot represent still gets a
	// clean message instead of a makeslice panic — DensifyLimit answers with
	// the configured ceiling when there is one and the physical cap otherwise.
	{
		g.wl("if %s > %s || %s > %s || %s/1 > %s/%s {", rows, lim, cols, lim, rows, lim, cols)
		g.in()
		g.wl(`dmFail("sparse grid too large to densify (%%dx%%d, limit %%d cells)", %s, %s, int64(%s))`,
			rows, cols, lim)
		g.out()
		g.wl("}")
	}
	g.wl("%s.rows, %s.cols = int(%s), int(%s)", v, v, rows, cols)
	g.wl("%s.cells = make([]%s, %s*%s)", v, elemGo, rows, cols)
	g.wl("for %s := range %s.cells {", i, v)
	g.in()
	g.wl("%s.cells[%s] = %s.def", v, i, in)
	g.out()
	g.wl("}")
	g.wl("for %s, %s := range %s.cells {", k, e, in)
	g.in()
	g.wl("%s.cells[(%s.r-%s.minR)*%s+(%s.c-%s.minC)] = %s", v, k, in, cols, k, in, e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

// emitMapCellsSparse: the lambda maps the default first (the whole plane is
// transformed), then every set cell in sorted row-major order.
func (g *gen) emitMapCellsSparse(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	outElemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	e := g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmSparse", declSparse, "sort")
	v, p := g.fresh("v"), g.fresh("p")
	g.wl("%s := %s.def", e, in)
	g.wl("%s := dmNewSparse[%s](%s)", v, outElemGo, body)
	g.wl("for _, %s := range %s.pts() {", p, in)
	g.in()
	g.wl("%s = %s.cells[%s]", e, in, p)
	g.wl("%s.put(%s.r, %s.c, %s)", v, p, p, body)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitCountCellsSparse(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	e := g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	v, p := g.fresh("v"), g.fresh("p")
	g.wl("var %s int64", v)
	g.wl("for _, %s := range %s.pts() {", p, in)
	g.in()
	g.wl("%s := %s.cells[%s]", e, in, p)
	g.wl("if %s {", body)
	g.in()
	g.wl("%s++", v)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitFindCellsSparse(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	pt, err := g.pointGo()
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	e := g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	v, p := g.fresh("v"), g.fresh("p")
	g.wl("%s := []%s{}", v, pt)
	g.wl("for _, %s := range %s.pts() {", p, in)
	g.in()
	g.wl("%s := %s.cells[%s]", e, in, p)
	g.wl("if %s {", body)
	g.in()
	g.wl("%s = append(%s, %s{%s.r, %s.c})", v, v, pt, p, p)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}
