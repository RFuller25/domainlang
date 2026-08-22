// Showing a graph as a graph.
//
// Every other type the stepper is named after has a rendering that matches its
// shape: a grid gets row and column gutters, a list one element per line with
// its index, Text its whitespace made visible. A `Graph<K>` had none. It fell
// to the value pane's default branch and arrived as the adjacency map on one
// line — `{a: [(b, 1), (c, 7)], b: [(d, 2), …` — truncated at the pane's width,
// which for six nodes meant three of them were readable and the rest were not
// merely hard to see but unreachable. That is the same defect the pane already
// fixed for lists, in the one type where the *structure* is the whole point.
//
// So a graph is drawn. Nodes are drawn once, at the first place a walk reaches
// them, and the arcs between them as branches:
//
//	a
//	├─[1]─ b
//	│      ├─[2]─ d
//	│      └─[3]─ e
//	└─[7]─ c
//	       └─[1]─ e ↩
//
// This is a spanning tree of the graph with the arcs it could not fit into a
// tree marked rather than dropped: `↩` means "this node is drawn somewhere
// above", which is what a second parent, a cross arc and a cycle all look like
// from here. A drawing that silently dropped those arcs would be a different
// graph, and one that followed them would not terminate.
//
// The walk starts at the roots — the nodes with no arc coming in, which is
// where a `parent -> child` listing parsed out of text wants to be read from —
// and then at whatever it has not reached, in insertion order, so a graph whose
// cycle reaches every node still draws in full.
//
// Nothing new is recorded for any of this. The recorder keeps a *string*, so
// the drawing is parsed back out of the rendering, the way collectionBody
// parses a list; anything that does not parse falls back to the flat line it
// would have shown anyway. A display that guesses wrong should look ordinary,
// not broken.
package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// graphArc is one drawn arc: where it goes, and what it weighs, both as the
// value renderer wrote them.
type graphArc struct {
	to string
	w  string
}

// parsedGraph is a graph recovered from its rendering. Nodes are identified by
// their rendered text, which is what makes the map key and the arc endpoint the
// same node: ir.FormatValue is deterministic, so one node has one spelling.
type parsedGraph struct {
	nodes []string // insertion order, as rendered
	arcs  [][]graphArc
	index map[string]int
}

// parseGraphRendering recovers a graph from `{k: [(k, w), …], …}`.
//
// It reports false for anything that is not that shape — a truncated capture,
// most often, since the recorder's budget cuts a long value mid-string — and
// the caller shows the flat rendering instead.
func parseGraphRendering(body string) (*parsedGraph, bool) {
	body = strings.TrimSpace(body)
	if len(body) < 2 || body[0] != '{' || body[len(body)-1] != '}' {
		return nil, false
	}
	inner := strings.TrimSpace(body[1 : len(body)-1])
	g := &parsedGraph{index: map[string]int{}}
	if inner == "" {
		return g, true // the empty graph renders as {}
	}

	for _, entry := range splitTopLevel(inner, ',') {
		key, list, ok := splitGraphEntry(entry)
		if !ok {
			return nil, false
		}
		from := g.add(key)
		for _, item := range splitTopLevel(strings.TrimSpace(list[1:len(list)-1]), ',') {
			to, w, ok := splitGraphArc(item)
			if !ok {
				return nil, false
			}
			g.arcs[from] = append(g.arcs[from], graphArc{to: to, w: w})
		}
	}
	// An arc may name a node the walk has not met as a key yet only if the
	// rendering is truncated — every node of a graph is a key of its adjacency
	// map. Treat that as unparsed rather than inventing the node.
	for _, arcs := range g.arcs {
		for _, a := range arcs {
			if _, ok := g.index[a.to]; !ok {
				return nil, false
			}
		}
	}
	return g, true
}

// add brings a node in, returning its index.
func (g *parsedGraph) add(n string) int {
	if i, ok := g.index[n]; ok {
		return i
	}
	i := len(g.nodes)
	g.nodes = append(g.nodes, n)
	g.arcs = append(g.arcs, nil)
	g.index[n] = i
	return i
}

// splitGraphEntry breaks `k: [(k, w), …]` at the colon that separates the two.
// The key can hold colons of its own — a record node renders as `{f: 1}` — so
// the split is the first one at depth zero whose remainder is the arc list.
func splitGraphEntry(entry string) (key, list string, ok bool) {
	entry = strings.TrimSpace(entry)
	depth, quoted := 0, false
	for i := 0; i < len(entry); i++ {
		c := entry[i]
		switch {
		case quoted:
			if c == '\\' {
				i++
			} else if c == '"' {
				quoted = false
			}
		case c == '"':
			quoted = true
		case c == '[' || c == '{' || c == '(':
			depth++
		case c == ']' || c == '}' || c == ')':
			depth--
		case c == ':' && depth == 0:
			key = strings.TrimSpace(entry[:i])
			list = strings.TrimSpace(entry[i+1:])
			if key == "" || len(list) < 2 || list[0] != '[' || list[len(list)-1] != ']' {
				return "", "", false
			}
			return key, list, true
		}
	}
	return "", "", false
}

// splitGraphArc breaks `(node, weight)` at its last top-level comma, so a tuple
// node — `((1, 1), 4)` — keeps the commas that belong to it.
func splitGraphArc(item string) (to, w string, ok bool) {
	item = strings.TrimSpace(item)
	if len(item) < 2 || item[0] != '(' || item[len(item)-1] != ')' {
		return "", "", false
	}
	parts := splitTopLevel(strings.TrimSpace(item[1:len(item)-1]), ',')
	if len(parts) < 2 {
		return "", "", false
	}
	// Everything but the last part is the node: a tuple node's own commas were
	// split on too, and rejoining them is exactly how they were written.
	return strings.Join(parts[:len(parts)-1], ", "), parts[len(parts)-1], true
}

// splitTopLevel splits on a separator that is not inside brackets or a quoted
// string. It is splitRendered's inner loop, over an already-unwrapped body,
// because a graph's rendering nests three kinds of bracket rather than one.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start, quoted := 0, 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted:
			if c == '\\' {
				i++
			} else if c == '"' {
				quoted = false
			}
		case c == '"':
			quoted = true
		case c == '[' || c == '{' || c == '(':
			depth++
		case c == ']' || c == '}' || c == ')':
			depth--
		case c == sep && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if s == "" {
		return nil
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// arcCount is the number of arcs in the graph.
func (g *parsedGraph) arcCount() int {
	n := 0
	for _, arcs := range g.arcs {
		n += len(arcs)
	}
	return n
}

// roots are the nodes with no arc coming in, in insertion order — the same
// question the `roots` builtin answers, asked of the rendering.
func (g *parsedGraph) roots() []int {
	incoming := make([]bool, len(g.nodes))
	for _, arcs := range g.arcs {
		for _, a := range arcs {
			incoming[g.index[a.to]] = true
		}
	}
	var out []int
	for i := range g.nodes {
		if !incoming[i] {
			out = append(out, i)
		}
	}
	return out
}

// weighted reports whether any arc weighs something other than 1. An unweighted
// graph draws without weights: every arc saying `[1]` is a column of noise.
func (g *parsedGraph) weighted() bool {
	for _, arcs := range g.arcs {
		for _, a := range arcs {
			if a.w != "1" {
				return true
			}
		}
	}
	return false
}

// hasCycle peels the nodes nothing points at, the same measure Topological Sort
// uses: what cannot be peeled is a cycle.
func (g *parsedGraph) hasCycle() bool {
	indeg := make([]int, len(g.nodes))
	for _, arcs := range g.arcs {
		for _, a := range arcs {
			indeg[g.index[a.to]]++
		}
	}
	var queue []int
	for i, d := range indeg {
		if d == 0 {
			queue = append(queue, i)
		}
	}
	ordered := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		ordered++
		for _, a := range g.arcs[cur] {
			j := g.index[a.to]
			indeg[j]--
			if indeg[j] == 0 {
				queue = append(queue, j)
			}
		}
	}
	return ordered != len(g.nodes)
}

// graphSummary is the line above the drawing: the counts a reader would
// otherwise have to take on trust, and the two facts about the shape that
// decide how to read what is underneath.
func (g *parsedGraph) summary() string {
	parts := []string{plural(len(g.nodes), "node"), plural(g.arcCount(), "arc")}
	if len(g.nodes) > 0 {
		parts = append(parts, plural(len(g.roots()), "root"))
	}
	if g.weighted() {
		parts = append(parts, "weighted")
	}
	if g.hasCycle() {
		parts = append(parts, "has a cycle")
	} else if len(g.nodes) > 0 {
		parts = append(parts, "acyclic")
	}
	return strings.Join(parts, " · ")
}

// maxGraphDrawLines is where the drawing gives up and the listing takes over.
// A drawing is one line per arc, so a dense graph — every node pointing at
// every other — is mostly `↩` references, and past a few hundred lines the
// listing is both shorter and easier to read. The pane's own cap is well above
// this; this is a judgment about legibility, not about memory.
const maxGraphDrawLines = 600

// graphBody renders a Graph value: a summary, then the drawing where one is
// worth having and the adjacency listing where it is not.
func graphBody(body string, w int) []string {
	g, ok := parseGraphRendering(body)
	if !ok {
		// Not the shape we know — most often a capture cut mid-value. Show what
		// the pane would have shown before, and let valueNote say why.
		return []string{"  " + styValue.Render(truncateVis(body, w-2))}
	}
	if len(g.nodes) == 0 {
		return []string{styDim.Render("  (an empty graph — no nodes)")}
	}

	out := []string{styDim.Render("  " + g.summary())}
	if g.arcCount()+len(g.nodes) > maxGraphDrawLines {
		out = append(out, styDim.Render(
			"  (too many arcs to draw legibly — listed by node instead)"), "")
		return append(out, g.listing(w)...)
	}
	out = append(out, "")
	drawing, backEdges := g.draw(w)
	out = append(out, drawing...)
	if backEdges {
		out = append(out, "", styDim.Render("  ↩ a node drawn above — a second parent, a cross arc or a cycle"))
	}
	return out
}

// listing is the fallback: one node per line with its arcs beside it. It is
// what the collection pane does for a list, and it stays readable at any size
// and any width.
func (g *parsedGraph) listing(w int) []string {
	gutter := 0
	for _, n := range g.nodes {
		gutter = max(gutter, ansi.StringWidth(n))
	}
	weighted := g.weighted()
	out := make([]string, 0, len(g.nodes))
	for i, n := range g.nodes {
		var b strings.Builder
		b.WriteString("  " + pad(n, gutter) + " " + styFrame.Render("→"))
		if len(g.arcs[i]) == 0 {
			b.WriteString(styDim.Render(" ·"))
		}
		for _, a := range g.arcs[i] {
			b.WriteString(" " + a.to)
			if weighted {
				b.WriteString(styDim.Render("[" + a.w + "]"))
			}
		}
		out = append(out, truncateVis(b.String(), w))
	}
	return out
}

// draw walks the graph and renders it as a tree with its non-tree arcs marked,
// reporting whether any such arc was drawn (which is what the legend explains).
//
// The walk is iterative: a graph parsed out of a puzzle input can be tens of
// thousands of nodes deep, and a recursive draw would be a stack overflow in a
// debugger, which is the worst possible place for one.
func (g *parsedGraph) draw(w int) ([]string, bool) {
	weighted := g.weighted()
	wWidth := 0
	if weighted {
		for _, arcs := range g.arcs {
			for _, a := range arcs {
				wWidth = max(wWidth, ansi.StringWidth(a.w))
			}
		}
	}

	drawn := make([]bool, len(g.nodes))
	var out []string
	backEdges := false

	// One frame of the walk: the arcs of a node still to be drawn, and the
	// prefix their branches hang under.
	type frame struct {
		node   int
		arc    int
		prefix string
	}

	starts := append(g.roots(), allNodes(len(g.nodes))...)
	for _, start := range starts {
		if drawn[start] {
			continue
		}
		if len(out) > 0 {
			out = append(out, "") // a blank line between the pieces
		}
		drawn[start] = true
		out = append(out, "  "+truncateVis(g.nodes[start], w-2))

		stack := []frame{{node: start}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.arc >= len(g.arcs[top.node]) {
				stack = stack[:len(stack)-1]
				continue
			}
			a := g.arcs[top.node][top.arc]
			last := top.arc == len(g.arcs[top.node])-1
			top.arc++

			branch, cont := graphBranch(last, weighted, a.w, wWidth)
			line := "  " + top.prefix + styFrame.Render(branch) + g.nodes[g.index[a.to]]
			to := g.index[a.to]
			if drawn[to] {
				backEdges = true
				out = append(out, truncateVis(line+styDim.Render(" ↩"), w))
				continue
			}
			drawn[to] = true
			out = append(out, truncateVis(line, w))

			// Descend, unless the indentation has eaten the pane. Stopping is
			// the honest answer: a branch drawn past the right edge is not a
			// drawing of anything.
			next := top.prefix + cont
			if ansi.StringWidth(next) > max(8, w/2) {
				if len(g.arcs[to]) > 0 {
					out = append(out, truncateVis("  "+next+styDim.Render(
						fmt.Sprintf("… %s deeper, past the width", plural(len(g.arcs[to]), "arc"))), w))
				}
				continue
			}
			stack = append(stack, frame{node: to, prefix: next})
		}
	}
	return out, backEdges
}

// graphBranch is one branch and the continuation that hangs under it. The
// weight rides on the arc, where it belongs, padded so a node's children line
// up whatever their weights are.
func graphBranch(last, weighted bool, weight string, wWidth int) (branch, cont string) {
	corner, down := "├", "│"
	if last {
		corner, down = "└", " "
	}
	if !weighted {
		return corner + "── ", down + "   "
	}
	return corner + "─[" + padLeft(weight, wWidth) + "]─ ", down + strings.Repeat(" ", wWidth+5)
}

// padLeft right-aligns a weight in a fixed column.
func padLeft(s string, w int) string {
	if n := w - ansi.StringWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// allNodes is every index in order — where the walk goes after the roots, so a
// graph with no root at all (a cycle reaching everything) still draws.
func allNodes(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}
