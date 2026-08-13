package codegen

import (
	"domain/ir"
)

// Grid lowerings. A Grid<T> compiles to dmGrid[T] — a rectangular cell slice
// with explicit dimensions, matching ir.GridValue's row-major layout.

func (g *gen) emitConvertToGrid(n *ir.Node, in string) (string, error) {
	if n.In.Kind == ir.KSparse {
		return g.emitSparseDensify(n, in)
	}
	if n.In.Equal(ir.List(ir.Text())) {
		return g.emitGridFromText(n, in)
	}
	return g.emitGridFromRows(n, in)
}

// emitGridFromRows lowers List<List<T>> -> Grid<T>.
func (g *gen) emitGridFromRows(n *ir.Node, in string) (string, error) {
	gridGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	elemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	v, r, row := g.fresh("v"), g.fresh("r"), g.fresh("row")
	g.wl("var %s %s", v, gridGo)
	g.wl("if len(%s) > 0 {", in)
	g.in()
	g.wl("%s.rows, %s.cols = len(%s), len(%s[0])", v, v, in, in)
	g.wl("%s.cells = make([]%s, 0, %s.rows*%s.cols)", v, elemGo, v, v)
	g.wl("for %s, %s := range %s {", r, row, in)
	g.in()
	g.wl("if len(%s) != %s.cols {", row, v)
	g.in()
	g.wl(`dmFail("grid is not rectangular: row %%d has %%d cells, expected %%d", %s, len(%s), %s.cols)`, r, row, v)
	g.out()
	g.wl("}")
	g.wl("%s.cells = append(%s.cells, %s...)", v, v, row)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

// emitGridFromText lowers List<Text> -> Grid<Text>, one cell per rune —
// mirroring the interpreter, which splits rows into single-rune strings.
func (g *gen) emitGridFromText(n *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	v, r, line := g.fresh("v"), g.fresh("r"), g.fresh("line")
	cells, ch := g.fresh("cells"), g.fresh("ch")
	g.wl("var %s dmGrid[string]", v)
	g.helper("dmGrid", declGrid)
	g.wl("for %s, %s := range %s {", r, line, in)
	g.in()
	bi := g.fresh("bi")
	g.wl("%s := make([]string, 0, len(%s))", cells, line)
	asciiLoop := func() {
		g.wl("for %s := 0; %s < len(%s); %s++ {", bi, bi, line, bi)
		g.in()
		g.wl("%s = append(%s, %s[%s:%s+1])", cells, cells, line, bi, bi)
		g.out()
		g.wl("}")
	}
	switch {
	case g.asciiText():
		// The caller has verified that every byte of the input is one rune, so
		// a byte index *is* a rune index and the decode is dead work: one
		// utf8.RuneLen per cell over a grid of twenty thousand of them. The
		// binary carries no check for this — see tuning.go on why it is pinned.
		asciiLoop()
	case g.asciiGuarded():
		// The same fast path with the general one compiled in beside it, chosen
		// per line. The check is a single pass over the line against a decode of
		// every rune in it, so a plain line pays a scan and saves the decode,
		// and a line with a multibyte rune takes the path it always took. This
		// is what "guarded" means concretely: correct on any input, faster on
		// the shape that was observed.
		g.helper("dmASCII", declASCII, "unicode/utf8")
		g.imp("unicode/utf8")
		g.wl("if dmASCII(%s) {", line)
		g.in()
		asciiLoop()
		g.out()
		g.wl("} else {")
		g.in()
		g.emitGridRuneCells(cells, line, bi, ch)
		g.out()
		g.wl("}")
	default:
		g.emitGridRuneCells(cells, line, bi, ch)
	}
	g.wl("if %s == 0 {", r)
	g.in()
	g.wl("%s.rows, %s.cols = len(%s), len(%s)", v, v, in, cells)
	g.wl("%s.cells = make([]string, 0, %s.rows*%s.cols)", v, v, v)
	g.out()
	g.wl("}")
	g.wl("if len(%s) != %s.cols {", cells, v)
	g.in()
	g.wl(`dmFail("grid is not rectangular: row %%d has %%d cells, expected %%d", %s, len(%s), %s.cols)`, r, cells, v)
	g.out()
	g.wl("}")
	g.wl("%s.cells = append(%s.cells, %s...)", v, v, cells)
	g.out()
	g.wl("}")
	return v, nil
}

// emitGridRuneCells is the general path: each cell a one-rune substring
// aliasing the line's backing store (zero allocation for the ASCII grids AoC
// uses), with string(ch) only on the invalid-rune fallback. It is a function of
// its own because the guarded ASCII specialisation compiles it *and* the fast
// path, and a fallback that had drifted from the code it falls back to would be
// worse than no fallback at all.
func (g *gen) emitGridRuneCells(cells, line, bi, ch string) {
	g.imp("unicode/utf8")
	g.wl("for %s, %s := range %s {", bi, ch, line)
	g.in()
	g.wl("if rl := utf8.RuneLen(%s); rl > 0 {", ch)
	g.in()
	g.wl("%s = append(%s, %s[%s:%s+rl])", cells, cells, line, bi, bi)
	g.out()
	g.wl("} else {")
	g.in()
	g.wl("%s = append(%s, string(%s))", cells, cells, ch)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
}

func (g *gen) emitCountCells(n *ir.Node, in string) (string, error) {
	if n.In.Kind == ir.KSparse {
		return g.emitCountCellsSparse(n, in)
	}
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	if positional, _ := n.Meta["positional"].(bool); positional {
		v, r, c := g.fresh("v"), g.fresh("r"), g.fresh("c")
		body, _, err := g.compileExpr(lam.Body, exprEnv{
			lam.Params[0]: {expr: in, typ: n.In},
			lam.Params[1]: {expr: r, typ: ir.Int()},
			lam.Params[2]: {expr: c, typ: ir.Int()},
		})
		if err != nil {
			return "", unsupported(n, "lambda: %v", err)
		}
		g.wl("var %s int64", v)
		g.wl("for %s := int64(0); %s < int64(%s.rows); %s++ {", r, r, in, r)
		g.in()
		g.wl("for %s := int64(0); %s < int64(%s.cols); %s++ {", c, c, in, c)
		g.in()
		g.wl("if %s {", body)
		g.in()
		g.wl("%s++", v)
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
		return v, nil
	}
	v, e := g.fresh("v"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("var %s int64", v)
	g.wl("for _, %s := range %s.cells {", e, in)
	g.in()
	g.wl("if %s {", body)
	g.in()
	g.wl("%s++", v)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitMapCells(n *ir.Node, in string) (string, error) {
	if n.In.Kind == ir.KSparse {
		return g.emitMapCellsSparse(n, in)
	}
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	gridGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	outElemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	if positional, _ := n.Meta["positional"].(bool); positional {
		v, r, c := g.fresh("v"), g.fresh("r"), g.fresh("c")
		body, _, err := g.compileExpr(lam.Body, exprEnv{
			lam.Params[0]: {expr: in, typ: n.In},
			lam.Params[1]: {expr: r, typ: ir.Int()},
			lam.Params[2]: {expr: c, typ: ir.Int()},
		})
		if err != nil {
			return "", unsupported(n, "lambda: %v", err)
		}
		g.wl("%s := %s{rows: %s.rows, cols: %s.cols, cells: make([]%s, len(%s.cells))}",
			v, gridGo, in, in, outElemGo, in)
		g.wl("for %s := int64(0); %s < int64(%s.rows); %s++ {", r, r, in, r)
		g.in()
		g.wl("for %s := int64(0); %s < int64(%s.cols); %s++ {", c, c, in, c)
		g.in()
		g.wl("%s.cells[%s*int64(%s.cols)+%s] = %s", v, r, in, c, body)
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
		return v, nil
	}
	v, i, e := g.fresh("v"), g.fresh("i"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := %s{rows: %s.rows, cols: %s.cols, cells: make([]%s, len(%s.cells))}",
		v, gridGo, in, in, outElemGo, in)
	g.wl("for %s, %s := range %s.cells {", i, e, in)
	g.in()
	g.wl("%s.cells[%s] = %s", v, i, body)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitTranspose(n *ir.Node, in string) (string, error) {
	if n.In != nil && n.In.Kind == ir.KList {
		return g.emitTransposeRows(n, in)
	}
	gridGo, err := g.goType(n.In)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, r, c := g.fresh("v"), g.fresh("r"), g.fresh("c")
	g.wl("%s := %s{rows: %s.cols, cols: %s.rows, cells: make([]%s, len(%s.cells))}",
		v, gridGo, in, in, elemGo, in)
	g.wl("for %s := 0; %s < %s.rows; %s++ {", r, r, in, r)
	g.in()
	g.wl("for %s := 0; %s < %s.cols; %s++ {", c, c, in, c)
	g.in()
	g.wl("%s.cells[%s*%s.cols+%s] = %s.cells[%s*%s.cols+%s]", v, c, v, r, in, r, in, c)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

// emitTransposeRows is Transpose over the List<List<T>> shape: the same swap,
// over a slice of slices rather than a dmGrid. A ragged row aborts with the
// wording the interpreter uses, so the two backends fail on the same input
// with the same message.
func (g *gen) emitTransposeRows(n *ir.Node, in string) (string, error) {
	rowGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	elemGo, err := g.goType(n.In.Elem.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	v, cols, r, c := g.fresh("v"), g.fresh("cols"), g.fresh("r"), g.fresh("c")
	g.wl("%s := 0", cols)
	g.wl("if len(%s) > 0 { %s = len(%s[0]) }", in, cols, in)
	g.wl("for %s := range %s {", r, in)
	g.in()
	g.wl("if len(%s[%s]) != %s {", in, r, cols)
	g.in()
	g.wl(`dmFail("grid is not rectangular: row %%d has %%d cells, expected %%d", %s, len(%s[%s]), %s)`,
		r, in, r, cols)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	g.wl("%s := make([]%s, %s)", v, rowGo, cols)
	g.wl("for %s := range %s {", c, cols)
	g.in()
	g.wl("%s[%s] = make([]%s, len(%s))", v, c, elemGo, in)
	g.wl("for %s := range %s {", r, in)
	g.in()
	g.wl("%s[%s][%s] = %s[%s][%s]", v, c, r, in, r, c)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}
