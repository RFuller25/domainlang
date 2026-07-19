package codegen

import (
	"domain/ir"
)

// Lowerings for the graph-search Domain Expansions (B.f5): BFS, Dijkstra,
// Flood Fill, Connected Components. The Using: predicate is inlined into a
// []bool mask loop at the call site (no closures), and the traversal itself
// is a self-contained dm* helper mirroring prims/search.go — including the
// dmFail wording for bad starts and negative costs.

// gridSearchFusable reports whether a search node's mask can be built straight
// from input lines (skipping a materialized string grid).
func gridSearchFusable(n *ir.Node) bool {
	return n.Prim == "BFS" || n.Prim == "Connected Components" || n.Prim == "Flood Fill"
}

// emitGridSearchFromLines fuses "Convert To Grid (from text) -> BFS/Connected
// Components": it builds the []bool mask and the row/col dimensions directly
// from the lines, applying the search predicate to a zero-alloc one-rune
// substring, so the intermediate dmGrid[string] is never materialized.
func (g *gen) emitGridSearchFromLines(gridNode, searchNode *ir.Node, lines string) (string, error) {
	lam, err := g.nodeLambda(searchNode)
	if err != nil {
		return "", err
	}
	cellT := ir.Text()
	if searchNode.In != nil && searchNode.In.Elem != nil {
		cellT = searchNode.In.Elem
	}
	mask, rows, cols, rc := g.fresh("mask"), g.fresh("rows"), g.fresh("cols"), g.fresh("rc")
	r, line, bi, ch, cell := g.fresh("r"), g.fresh("line"), g.fresh("bi"), g.fresh("ch"), g.fresh("cell")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: cell, typ: cellT}})
	if err != nil {
		return "", unsupported(searchNode, "lambda: %v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.imp("unicode/utf8")
	g.wl("%s := len(%s)", rows, lines)
	g.wl("%s := 0", cols)
	g.wl("%s := make([]bool, 0, %s*%s)", mask, rows, cols) // grows; cap is a hint
	g.wl("for %s, %s := range %s {", r, line, lines)
	g.in()
	g.wl("%s := 0", rc)
	g.wl("for %s, %s := range %s {", bi, ch, line)
	g.in()
	g.wl("var %s string", cell)
	g.wl("if rl := utf8.RuneLen(%s); rl > 0 {", ch)
	g.in()
	g.wl("%s = %s[%s:%s+rl]", cell, line, bi, bi)
	g.out()
	g.wl("} else {")
	g.in()
	g.wl("%s = string(%s)", cell, ch)
	g.out()
	g.wl("}")
	g.wl("%s = append(%s, %s)", mask, mask, body)
	g.wl("%s++", rc)
	g.out()
	g.wl("}")
	g.wl("if %s == 0 {", r)
	g.in()
	g.wl("%s = %s", cols, rc)
	g.out()
	g.wl("} else if %s != %s {", rc, cols)
	g.in()
	g.wl(`dmFail("grid is not rectangular: row %%d has %%d cells, expected %%d", %s, %s, %s)`, r, rc, cols)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")

	g.helper("dmGrid", declGrid)
	v := g.fresh("v")
	switch searchNode.Prim {
	case "BFS":
		sr, sc, err := startCoords(searchNode)
		if err != nil {
			return "", err
		}
		g.helper("dmCheckStart", declCheckStart)
		g.helper("dmDistGrid", declDistGrid)
		g.helper("dmBFS", declBFS)
		g.wl("%s := dmBFS(%s, %s, %s, %d, %d)", v, rows, cols, mask, sr, sc)
	case "Connected Components":
		g.helper("dmComponents", declComponents)
		g.wl("%s := dmComponents(%s, %s, %s)", v, rows, cols, mask)
	case "Flood Fill":
		sr, sc, err := startCoords(searchNode)
		if err != nil {
			return "", err
		}
		g.helper("dmCheckStart", declCheckStart)
		g.helper("dmFloodFill", declFloodFill)
		g.wl("%s := dmFloodFill(%s, %s, %s, %d, %d)", v, rows, cols, mask, sr, sc)
	default:
		return "", unsupported(searchNode, "grid-search fusion")
	}
	return v, nil
}

// emitCellMask inlines the node's cell predicate over every grid cell.
func (g *gen) emitCellMask(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	mask, i, e := g.fresh("mask"), g.fresh("i"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := make([]bool, len(%s.cells))", mask, in)
	g.wl("for %s, %s := range %s.cells {", i, e, in)
	g.in()
	g.wl("%s[%s] = %s", mask, i, body)
	g.out()
	g.wl("}")
	return mask, nil
}

// startCoords reads the "from R C" metadata the resolver stashed.
func startCoords(n *ir.Node) (int64, int64, error) {
	r, ok1 := n.Meta["row"].(int64)
	c, ok2 := n.Meta["col"].(int64)
	if !ok1 || !ok2 {
		return 0, 0, unsupported(n, "missing start coordinates metadata")
	}
	return r, c, nil
}

const declCheckStart = `func dmCheckStart(rows, cols int, sr, sc int64) {
	if sr < 0 || sr >= int64(rows) || sc < 0 || sc >= int64(cols) {
		dmFail("start (%d, %d) is out of bounds (grid %dx%d)", sr, sc, rows, cols)
	}
}`

const declDistGrid = `func dmDistGrid(rows, cols int) dmGrid[int64] {
	out := dmGrid[int64]{rows: rows, cols: cols, cells: make([]int64, rows*cols)}
	for i := range out.cells {
		out.cells[i] = -1
	}
	return out
}`

const declBFS = `func dmBFS(rows, cols int, mask []bool, sr, sc int64) dmGrid[int64] {
	dmCheckStart(rows, cols, sr, sc)
	if !mask[sr*int64(cols)+sc] {
		dmFail("start (%d, %d) is not walkable", sr, sc)
	}
	out := dmDistGrid(rows, cols)
	out.cells[sr*int64(cols)+sc] = 0
	queue := [][2]int64{{sr, sc}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		d := out.cells[cur[0]*int64(cols)+cur[1]]
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur[0]+dl[0], cur[1]+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= int64(cols) {
				continue
			}
			i := nr*int64(cols) + nc
			if !mask[i] || out.cells[i] != -1 {
				continue
			}
			out.cells[i] = d + 1
			queue = append(queue, [2]int64{nr, nc})
		}
	}
	return out
}`

func (g *gen) emitBFS(n *ir.Node, in string) (string, error) {
	mask, err := g.emitCellMask(n, in)
	if err != nil {
		return "", err
	}
	r, c, err := startCoords(n)
	if err != nil {
		return "", err
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmGrid", declGrid)
	g.helper("dmCheckStart", declCheckStart)
	g.helper("dmDistGrid", declDistGrid)
	g.helper("dmBFS", declBFS)
	v := g.fresh("v")
	g.wl("%s := dmBFS(%s.rows, %s.cols, %s, %d, %d)", v, in, in, mask, r, c)
	return v, nil
}

const declFloodFill = `func dmFloodFill(rows, cols int, mask []bool, sr, sc int64) dmGrid[int64] {
	dmCheckStart(rows, cols, sr, sc)
	if !mask[sr*int64(cols)+sc] {
		dmFail("start (%d, %d) is not in the region (its predicate is false there)", sr, sc)
	}
	out := dmGrid[int64]{rows: rows, cols: cols, cells: make([]int64, rows*cols)}
	out.cells[sr*int64(cols)+sc] = 1
	stack := [][2]int64{{sr, sc}}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur[0]+dl[0], cur[1]+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= int64(cols) {
				continue
			}
			i := nr*int64(cols) + nc
			if !mask[i] || out.cells[i] == 1 {
				continue
			}
			out.cells[i] = 1
			stack = append(stack, [2]int64{nr, nc})
		}
	}
	return out
}`

func (g *gen) emitFloodFill(n *ir.Node, in string) (string, error) {
	mask, err := g.emitCellMask(n, in)
	if err != nil {
		return "", err
	}
	r, c, err := startCoords(n)
	if err != nil {
		return "", err
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmGrid", declGrid)
	g.helper("dmCheckStart", declCheckStart)
	g.helper("dmFloodFill", declFloodFill)
	v := g.fresh("v")
	g.wl("%s := dmFloodFill(%s.rows, %s.cols, %s, %d, %d)", v, in, in, mask, r, c)
	return v, nil
}

// declDijkstra hand-rolls a binary min-heap on (dist, row, col) — ties settle
// in arbitrary heap order, which cannot change the resulting distances. A
// tentative-distance array prunes pushes to strict improvements (the standard
// lazy-deletion Dijkstra), so the heap only ever holds relaxations that beat
// the best distance seen for a cell — far fewer heap operations than pushing
// every unsettled neighbour, with identical final distances.
const declDijkstra = `func dmDijkstra(rows, cols int, costs []int64, sr, sc int64) dmGrid[int64] {
	for i, c := range costs {
		if c < 0 {
			dmFail("cell %d has a negative or non-Int cost (%d)", i, c)
		}
	}
	dmCheckStart(rows, cols, sr, sc)
	out := dmDistGrid(rows, cols)
	const inf = int64(^uint64(0) >> 1)
	tent := make([]int64, len(costs))
	for i := range tent {
		tent[i] = inf
	}
	type item struct{ d, r, c int64 }
	h := []item{{0, sr, sc}}
	tent[sr*int64(cols)+sc] = 0
	push := func(it item) {
		h = append(h, it)
		for i := len(h) - 1; i > 0; {
			p := (i - 1) / 2
			if h[p].d <= h[i].d {
				break
			}
			h[p], h[i] = h[i], h[p]
			i = p
		}
	}
	pop := func() item {
		top := h[0]
		n := len(h) - 1
		h[0] = h[n]
		h = h[:n]
		for i := 0; ; {
			l, r, m := 2*i+1, 2*i+2, i
			if l < n && h[l].d < h[m].d {
				m = l
			}
			if r < n && h[r].d < h[m].d {
				m = r
			}
			if m == i {
				break
			}
			h[i], h[m] = h[m], h[i]
			i = m
		}
		return top
	}
	for len(h) > 0 {
		cur := pop()
		i := cur.r*int64(cols) + cur.c
		if out.cells[i] != -1 {
			continue
		}
		out.cells[i] = cur.d
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur.r+dl[0], cur.c+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= int64(cols) {
				continue
			}
			j := nr*int64(cols) + nc
			if out.cells[j] != -1 {
				continue
			}
			nd := cur.d + costs[j]
			if nd < tent[j] {
				tent[j] = nd
				push(item{nd, nr, nc})
			}
		}
	}
	return out
}`

func (g *gen) emitDijkstra(n *ir.Node, in string) (string, error) {
	r, c, err := startCoords(n)
	if err != nil {
		return "", err
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmGrid", declGrid)
	g.helper("dmCheckStart", declCheckStart)
	g.helper("dmDistGrid", declDistGrid)
	g.helper("dmDijkstra", declDijkstra)
	v := g.fresh("v")
	g.wl("%s := dmDijkstra(%s.rows, %s.cols, %s.cells, %d, %d)", v, in, in, in, r, c)
	return v, nil
}

// declBFSTarget / declDijkstraTarget lower the optimizer's early-exit fusion
// of a search with a single at(target) read (optimizer.fuseSearchTarget):
// same validations and wording as the full helpers plus at()'s bounds check,
// returning the moment the target settles.
const declBFSTarget = `func dmBFSTarget(rows, cols int, mask []bool, sr, sc, tr, tc int64) int64 {
	dmCheckStart(rows, cols, sr, sc)
	if !mask[sr*int64(cols)+sc] {
		dmFail("start (%d, %d) is not walkable", sr, sc)
	}
	if tr < 0 || tr >= int64(rows) || tc < 0 || tc >= int64(cols) {
		dmFail("at: position (%d, %d) out of range (grid %dx%d)", tr, tc, rows, cols)
	}
	w := int64(cols)
	target := tr*w + tc
	if sr*w+sc == target {
		return 0
	}
	dist := make([]int64, rows*cols)
	for i := range dist {
		dist[i] = -1
	}
	dist[sr*w+sc] = 0
	queue := [][2]int64{{sr, sc}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		d := dist[cur[0]*w+cur[1]]
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur[0]+dl[0], cur[1]+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= w {
				continue
			}
			i := nr*w + nc
			if !mask[i] || dist[i] != -1 {
				continue
			}
			dist[i] = d + 1
			if i == target {
				return d + 1
			}
			queue = append(queue, [2]int64{nr, nc})
		}
	}
	return -1
}`

const declDijkstraTarget = `func dmDijkstraTarget(rows, cols int, costs []int64, sr, sc, tr, tc int64) int64 {
	for i, c := range costs {
		if c < 0 {
			dmFail("cell %d has a negative or non-Int cost (%d)", i, c)
		}
	}
	dmCheckStart(rows, cols, sr, sc)
	if tr < 0 || tr >= int64(rows) || tc < 0 || tc >= int64(cols) {
		dmFail("at: position (%d, %d) out of range (grid %dx%d)", tr, tc, rows, cols)
	}
	w := int64(cols)
	target := tr*w + tc
	dist := make([]int64, rows*cols)
	for i := range dist {
		dist[i] = -1
	}
	const inf = int64(^uint64(0) >> 1)
	tent := make([]int64, rows*cols)
	for i := range tent {
		tent[i] = inf
	}
	type item struct{ d, r, c int64 }
	h := []item{{0, sr, sc}}
	tent[sr*w+sc] = 0
	push := func(it item) {
		h = append(h, it)
		for i := len(h) - 1; i > 0; {
			p := (i - 1) / 2
			if h[p].d <= h[i].d {
				break
			}
			h[p], h[i] = h[i], h[p]
			i = p
		}
	}
	pop := func() item {
		top := h[0]
		n := len(h) - 1
		h[0] = h[n]
		h = h[:n]
		for i := 0; ; {
			l, r, m := 2*i+1, 2*i+2, i
			if l < n && h[l].d < h[m].d {
				m = l
			}
			if r < n && h[r].d < h[m].d {
				m = r
			}
			if m == i {
				break
			}
			h[i], h[m] = h[m], h[i]
			i = m
		}
		return top
	}
	for len(h) > 0 {
		cur := pop()
		i := cur.r*w + cur.c
		if dist[i] != -1 {
			continue
		}
		dist[i] = cur.d
		if i == target {
			return cur.d
		}
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur.r+dl[0], cur.c+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= w {
				continue
			}
			j := nr*w + nc
			if dist[j] != -1 {
				continue
			}
			nd := cur.d + costs[j]
			if nd < tent[j] {
				tent[j] = nd
				push(item{nd, nr, nc})
			}
		}
	}
	return -1
}`

func (g *gen) emitSearchTarget(n *ir.Node, in string) (string, error) {
	kind, _ := n.Meta["kind"].(string)
	r, c, err := startCoords(n)
	if err != nil {
		return "", err
	}
	tr, ok1 := n.Meta["trow"].(int64)
	tc, ok2 := n.Meta["tcol"].(int64)
	if !ok1 || !ok2 {
		return "", unsupported(n, "missing target coordinates metadata")
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmGrid", declGrid)
	g.helper("dmCheckStart", declCheckStart)
	v := g.fresh("v")
	switch kind {
	case "BFS":
		mask, err := g.emitCellMask(n, in)
		if err != nil {
			return "", err
		}
		g.helper("dmBFSTarget", declBFSTarget)
		g.wl("%s := dmBFSTarget(%s.rows, %s.cols, %s, %d, %d, %d, %d)", v, in, in, mask, r, c, tr, tc)
		return v, nil
	case "Dijkstra":
		g.helper("dmDijkstraTarget", declDijkstraTarget)
		g.wl("%s := dmDijkstraTarget(%s.rows, %s.cols, %s.cells, %d, %d, %d, %d)", v, in, in, in, r, c, tr, tc)
		return v, nil
	}
	return "", unsupported(n, "unknown search kind %q", kind)
}

const declComponents = `func dmComponents(rows, cols int, mask []bool) int64 {
	parent := make([]int, rows*cols)
	size := make([]int, rows*cols)
	for i := range parent {
		parent[i] = i
		size[i] = 1
	}
	find := func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if size[ra] < size[rb] {
			ra, rb = rb, ra
		}
		parent[rb] = ra
		size[ra] += size[rb]
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if !mask[i] {
				continue
			}
			if c+1 < cols && mask[i+1] {
				union(i, i+1)
			}
			if r+1 < rows && mask[i+cols] {
				union(i, i+cols)
			}
		}
	}
	var n int64
	for i, m := range mask {
		if m && find(i) == i {
			n++
		}
	}
	return n
}`

func (g *gen) emitConnectedComponents(n *ir.Node, in string) (string, error) {
	mask, err := g.emitCellMask(n, in)
	if err != nil {
		return "", err
	}
	g.helper("dmComponents", declComponents)
	v := g.fresh("v")
	g.wl("%s := dmComponents(%s.rows, %s.cols, %s)", v, in, in, mask)
	return v, nil
}
