package codegen

import (
	"fmt"
	"math"

	"domain/ir"
)

// Grid geometry and Find Cycle. The grid runtime type is a rows/cols pair over
// a flat row-major slice, so each transform is one nested loop writing into a
// freshly sized grid — the same arithmetic prims/gridgeom.go does.

// emitRange builds the half-open [lo, hi) integer range. Literal bounds make
// the length known, so the slice is allocated exactly once; measured bounds
// are computed first and the capacity comes out of them.
func (g *gen) emitRange(n *ir.Node, in string) (string, error) {
	lo, err := g.measuredOperand(n, in, "lo", "Low", math.MinInt64)
	if err != nil {
		return "", err
	}
	hi, err := g.measuredOperand(n, in, "hi", "High", math.MinInt64)
	if err != nil {
		return "", err
	}
	if hasMeasured(n, "lo", "hi") {
		g.helper("dmFail", declFail, "fmt", "os")
		g.wl("if %s < %s {", hi, lo)
		g.in()
		g.wl(`dmFail("bounds are half-open [%%d, %%d), so the high bound may not be below the low one", %s, %s)`, lo, hi)
		g.out()
		g.wl("}")
	}
	v, i := g.fresh("v"), g.fresh("i")
	g.wl("%s := make([]int64, 0, %s-%s)", v, hi, lo)
	g.wl("for %s := int64(%s); %s < %s; %s++ {", i, lo, i, hi, i)
	g.in()
	g.wl("%s = append(%s, %s)", v, v, i)
	g.out()
	g.wl("}")
	return v, nil
}

// emitSubgrid crops. The bounds are compile-time literals; the fit check is
// not, since the grid's size comes from the input.
func (g *gen) emitSubgrid(n *ir.Node, in string) (string, error) {
	var op [4]string
	for i, k := range [4]string{"row", "col", "height", "width"} {
		name := [4]string{"Row", "Col", "Height", "Width"}[i]
		// Height and width may not be negative; the origin's only constraint
		// is the fit check below, which covers it.
		min := int64(math.MinInt64)
		if i >= 2 {
			min = 0
		}
		s, err := g.measuredOperand(n, in, k, name, min)
		if err != nil {
			return "", err
		}
		op[i] = s
	}
	r0, c0, h, w := op[0], op[1], op[2], op[3]
	gridGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	v, r := g.fresh("v"), g.fresh("r")
	g.wl("if %s < 0 || %s < 0 || %s+%s > int64(%s.rows) || %s+%s > int64(%s.cols) {",
		r0, c0, r0, h, in, c0, w, in)
	g.in()
	g.wl(`dmFail("Subgrid: crop (%%d, %%d) %%dx%%d does not fit a %%dx%%d grid", int64(%s), int64(%s), int64(%s), int64(%s), int64(%s.rows), int64(%s.cols))`,
		r0, c0, h, w, in, in)
	g.out()
	g.wl("}")
	g.wl("var %s %s", v, gridGo)
	g.wl("%s.rows, %s.cols = int(%s), int(%s)", v, v, h, w)
	g.wl("%s.cells = make([]%s, 0, int(%s)*int(%s))", v, elemGo, h, w)
	g.wl("for %s := int64(%s); %s < int64(%s)+int64(%s); %s++ {", r, r0, r, r0, h, r)
	g.in()
	g.wl("%s.cells = append(%s.cells, %s.cells[%s*int64(%s.cols)+int64(%s):%s*int64(%s.cols)+int64(%s)+int64(%s)]...)",
		v, v, in, r, in, c0, r, in, c0, w)
	g.out()
	g.wl("}")
	return v, nil
}

// emitPadGrid surrounds the grid with a border of the Fill: literal.
func (g *gen) emitPadGrid(n *ir.Node, in string) (string, error) {
	pad, err := g.measuredOperand(n, in, "n", "Thickness", 0)
	if err != nil {
		return "", err
	}
	gridGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	fill, err := g.measuredLit(n, in, "fill", n.In, n.Out.Elem, func(f any) (string, error) {
		switch x := f.(type) {
		case string:
			return goStr(x), nil
		case int64:
			return fmt.Sprintf("int64(%d)", x), nil
		}
		return "", unsupported(n, "Pad Grid needs an Int or Text Fill:")
	})
	if err != nil {
		return "", err
	}
	v, i, r, c := g.fresh("v"), g.fresh("i"), g.fresh("r"), g.fresh("c")
	g.wl("var %s %s", v, gridGo)
	g.wl("%s.rows, %s.cols = %s.rows+2*int(%s), %s.cols+2*int(%s)", v, v, in, pad, in, pad)
	g.wl("%s.cells = make([]%s, %s.rows*%s.cols)", v, elemGo, v, v)
	g.wl("for %s := range %s.cells {", i, v)
	g.in()
	g.wl("%s.cells[%s] = %s", v, i, fill)
	g.out()
	g.wl("}")
	g.wl("for %s := 0; %s < %s.rows; %s++ {", r, r, in, r)
	g.in()
	g.wl("for %s := 0; %s < %s.cols; %s++ {", c, c, in, c)
	g.in()
	g.wl("%s.cells[(%s+int(%s))*%s.cols+(%s+int(%s))] = %s.cells[%s*%s.cols+%s]",
		v, r, pad, v, c, pad, in, r, in, c)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitRotateGrid(n *ir.Node, in string) (string, error) {
	mode, _ := n.Meta["mode"].(string)
	gridGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, r, c := g.fresh("v"), g.fresh("r"), g.fresh("c")
	// A quarter turn swaps the dimensions; Half keeps them.
	rows, cols := in+".cols", in+".rows"
	dst := "(" + c + ")*" + in + ".rows + (" + in + ".rows - 1 - " + r + ")"
	switch mode {
	case "Half":
		rows, cols = in+".rows", in+".cols"
		dst = "(" + in + ".rows-1-" + r + ")*" + in + ".cols + (" + in + ".cols - 1 - " + c + ")"
	case "Left":
		dst = "(" + in + ".cols-1-" + c + ")*" + in + ".rows + " + r
	}
	g.wl("var %s %s", v, gridGo)
	g.wl("%s.rows, %s.cols = %s, %s", v, v, rows, cols)
	g.wl("%s.cells = make([]%s, %s*%s)", v, elemGo, rows, cols)
	g.wl("for %s := 0; %s < %s.rows; %s++ {", r, r, in, r)
	g.in()
	g.wl("for %s := 0; %s < %s.cols; %s++ {", c, c, in, c)
	g.in()
	g.wl("%s.cells[%s] = %s.cells[%s*%s.cols+%s]", v, dst, in, r, in, c)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitFlipGrid(n *ir.Node, in string) (string, error) {
	mode, _ := n.Meta["mode"].(string)
	gridGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, r, c := g.fresh("v"), g.fresh("r"), g.fresh("c")
	// Horizontal mirrors left-right, Vertical top-bottom.
	dst := r + "*" + in + ".cols + (" + in + ".cols - 1 - " + c + ")"
	if mode == "Vertical" {
		dst = "(" + in + ".rows-1-" + r + ")*" + in + ".cols + " + c
	}
	g.wl("var %s %s", v, gridGo)
	g.wl("%s.rows, %s.cols = %s.rows, %s.cols", v, v, in, in)
	g.wl("%s.cells = make([]%s, %s.rows*%s.cols)", v, elemGo, in, in)
	g.wl("for %s := 0; %s < %s.rows; %s++ {", r, r, in, r)
	g.in()
	g.wl("for %s := 0; %s < %s.cols; %s++ {", c, c, in, c)
	g.in()
	g.wl("%s.cells[%s] = %s.cells[%s*%s.cols+%s]", v, dst, in, r, in, c)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitConvertToRows(n *ir.Node, in string) (string, error) {
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, r := g.fresh("v"), g.fresh("r")
	g.wl("%s := make([][]%s, %s.rows)", v, elemGo, in)
	g.wl("for %s := 0; %s < %s.rows; %s++ {", r, r, in, r)
	g.in()
	g.wl("%s[%s] = append([]%s(nil), %s.cells[%s*%s.cols:(%s+1)*%s.cols]...)",
		v, r, elemGo, in, r, in, r, in)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitFindCycle(n *ir.Node, in string) (string, error) {
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	pt, err := g.pointGo()
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, seen, i, e := g.fresh("v"), g.fresh("seen"), g.fresh("i"), g.fresh("e")
	g.wl("%s := %s{-1, 0}", v, pt)
	g.wl("%s := map[%s]int64{}", seen, elemGo)
	g.wl("for %s, %s := range %s {", i, e, in)
	g.in()
	g.wl("if at, ok := %s[%s]; ok {", seen, e)
	g.in()
	g.wl("%s = %s{at, int64(%s) - at}", v, pt, i)
	g.wl("break")
	g.out()
	g.wl("}")
	g.wl("%s[%s] = int64(%s)", seen, e, i)
	g.out()
	g.wl("}")
	return v, nil
}

// emitTopologicalSort lowers Kahn's algorithm, mirroring prims/toposort.go
// including its deterministic first-seen tie-breaking: a set-based ready queue
// would produce a valid but different order, and the two backends must print
// the same answer.
func (g *gen) emitTopologicalSort(n *ir.Node, in string) (string, error) {
	node, _ := n.Meta["node"].(*ir.Type)
	if node == nil {
		node = n.Out.Elem
	}
	keyGo, err := g.goType(node)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmMap", declMap)
	// An edge list is folded into the adjacency map first, exactly as the
	// interpreter does, so one algorithm serves both input shapes.
	if edges, _ := n.Meta["edges"].(bool); edges {
		g.helper("dmFail", declFail, "fmt", "os")
		m, e := g.fresh("m"), g.fresh("e")
		tuple := n.In.Elem != nil && n.In.Elem.Kind == ir.KTuple
		from, to := e+".f0", e+".f1"
		if !tuple {
			from, to = e+"[0]", e+"[1]"
		}
		g.wl("%s := dmNewMap[%s, []%s]()", m, keyGo, keyGo)
		g.wl("for _, %s := range %s {", e, in)
		g.in()
		if !tuple {
			g.wl("if len(%s) != 2 {", e)
			g.in()
			g.wl(`dmFail("Topological Sort: an edge is not a (from, to) pair")`)
			g.out()
			g.wl("}")
		}
		g.wl("%s.put(%s, append(%s.vals[%s], %s))", m, from, m, from, to)
		g.wl("if _, ok := %s.vals[%s]; !ok { %s.put(%s, nil) }", m, to, m, to)
		g.out()
		g.wl("}")
		in = m
	}
	g.helper("dmFail", declFail, "fmt", "os")
	// The renderer is only used to name a node in the cycle error. Scalars
	// have no fmtFunc — they are printed directly everywhere else — so the
	// function is built per kind here.
	var fmtFn string
	switch node.Kind {
	case ir.KText:
		fmtFn = "func(s string) string { return s }"
	case ir.KInt:
		g.imp("strconv")
		fmtFn = "func(n int64) string { return strconv.FormatInt(n, 10) }"
	default:
		f, err := g.fmtFunc(node)
		if err != nil {
			return "", unsupported(n, "cannot render %s: %v", node, err)
		}
		fmtFn = f
	}
	g.helper("dmTopoSort", fmt.Sprintf(`func dmTopoSort[K comparable](m dmMap[K, []K], render func(K) string) []K {
	var order []K
	index := map[K]int{}
	add := func(n K) int {
		if i, seen := index[n]; seen {
			return i
		}
		index[n] = len(order)
		order = append(order, n)
		return len(order) - 1
	}
	for _, k := range m.keys {
		add(k)
	}
	adj := map[int][]int{}
	indeg := map[int]int{}
	for _, k := range m.keys {
		from := add(k)
		for _, s := range m.vals[k] {
			to := add(s)
			adj[from] = append(adj[from], to)
			indeg[to]++
		}
	}
	ready := make([]int, 0, len(order))
	for i := range order {
		if indeg[i] == 0 {
			ready = append(ready, i)
		}
	}
	out := make([]K, 0, len(order))
	for head := 0; head < len(ready); head++ {
		i := ready[head]
		out = append(out, order[i])
		for _, j := range adj[i] {
			indeg[j]--
			if indeg[j] == 0 {
				ready = append(ready, j)
			}
		}
	}
	if len(out) != len(order) {
		for i := range order {
			if indeg[i] > 0 {
				dmFail("Topological Sort: the graph has a cycle (%%s is still blocked after %%d of %%d nodes were ordered)", render(order[i]), len(out), len(order))
			}
		}
	}
	return out
}`))
	v := g.fresh("v")
	g.wl("%s := dmTopoSort(%s, %s)", v, in, fmtFn)
	_ = keyGo
	return v, nil
}
