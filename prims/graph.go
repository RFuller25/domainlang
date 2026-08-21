// Channeled Energy: Convert To Graph / Convert To Edges — the doors in and out
// of Graph<K>.
//
// The expression layer can already build a graph (`graph(es)`), but a pipeline
// that has just parsed an edge list should not have to detour through an
// `Apply` to say so, any more than one that has just split a grid has to for
// `Convert To Grid`. And a coercion in needs a coercion out, for the same
// reason `Convert To Rows` exists: anything the graph vocabulary does not cover
// has to be reachable by dropping back to lists.
package prims

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// graphEdgeNode reports the node type of an edge-list input, and whether the
// edges carry a weight. It accepts exactly what the `graph` builtin does, and
// for the same reason: those are the shapes a parse lands on.
func graphEdgeNode(in *ir.Type) (node *ir.Type, weighted bool, ok bool) {
	if in == nil || in.Kind != ir.KList || in.Elem == nil {
		return nil, false, false
	}
	switch e := in.Elem; e.Kind {
	case ir.KTuple:
		if len(e.Elems) != 2 && len(e.Elems) != 3 {
			return nil, false, false
		}
		if !e.Elems[0].Equal(e.Elems[1]) {
			return nil, false, false
		}
		if len(e.Elems) == 3 && !e.Elems[2].Equal(ir.Int()) {
			return nil, false, false
		}
		return e.Elems[0], len(e.Elems) == 3, true
	case ir.KList:
		// The ragged form a positional Match Pattern produces. Its row length
		// is only known at run time, so it is always unweighted.
		return e.Elem, false, true
	}
	return nil, false, false
}

var convertToGraph = &Primitive{
	ID:      "Convert To Graph",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Convert") && hasWord(op, "Graph") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		// Undirected is the one Mode:, and it inserts both arcs rather than
		// making a second kind of value — see ir.GraphValue for why the
		// representation stays single.
		undirected := false
		if name, ok := args.Ident("Mode"); ok {
			switch name {
			case "Undirected":
				undirected = true
			case "Directed":
			default:
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Convert To Graph: unknown Mode %q; write Directed (the default) or Undirected", name)}
			}
		}

		var node *ir.Type
		var weighted, fromMap bool
		switch {
		case in != nil && in.Kind == ir.KMap && in.Elem != nil &&
			in.Elem.Kind == ir.KList && in.Elem.Elem.Equal(in.Key):
			// An adjacency Map<K, List<K>> — the classic written form, and
			// what Topological Sort has always taken.
			node, weighted, fromMap = in.Key, false, true
		default:
			var ok bool
			node, weighted, ok = graphEdgeNode(in)
			if !ok {
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Convert To Graph expects an edge list — List<(K, K)>, List<(K, K, Int)>, "+
						"or the List<List<K>> a positional Match Pattern produces — or an "+
						"adjacency Map<K, List<K>>, got %s", in)}
			}
		}
		if !ir.Keyable(node) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Convert To Graph: nodes must be keyable (Int, Text, or a tuple/record of them), got %s", node)}
		}

		out := ir.Graph(node)
		display := "Convert To Graph"
		if undirected {
			display += ", Undirected"
		}
		return &ir.Node{
			Prim: "Convert To Graph", In: in, Out: out,
			Display:   display,
			Swappable: true,
			Meta:      map[string]any{"undirected": undirected, "weighted": weighted, "fromMap": fromMap, "node": node},
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g := ir.NewGraphValue()
				add := func(a, b ir.Value, w int64) {
					g.AddEdge(a, b, w)
					if undirected {
						g.AddEdge(b, a, w)
					}
				}
				if fromMap {
					m, ok := v.(*ir.MapValue)
					if !ok {
						return nil, runtimeErr("Convert To Graph", pos,
							"expected a Map, got %s", ir.DescribeValue(v))
					}
					for _, k := range m.Keys() {
						g.AddNode(k)
						succ, _ := m.Get(k)
						xs, err := ir.AsList(succ)
						if err != nil {
							return nil, runtimeErr("Convert To Graph", pos, "%v", err)
						}
						for _, s := range xs {
							add(k, s, 1)
						}
					}
					return g, nil
				}
				es, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Convert To Graph", pos, "%v", err)
				}
				for i, e := range es {
					parts, err := ir.AsList(e)
					if err != nil {
						return nil, runtimeErr("Convert To Graph", pos, "edge %d: %v", i, err)
					}
					if len(parts) != 2 && len(parts) != 3 {
						return nil, runtimeErr("Convert To Graph", pos,
							"edge %d has %d parts, want 2 (from, to) or 3 (from, to, weight)", i, len(parts))
					}
					w := int64(1)
					if len(parts) == 3 {
						n, ok := parts[2].(int64)
						if !ok {
							return nil, runtimeErr("Convert To Graph", pos,
								"edge %d weight is not an Int", i)
						}
						w = n
					}
					add(parts[0], parts[1], w)
				}
				return g, nil
			},
		}, nil
	},
}

var convertToEdges = &Primitive{
	ID:      "Convert To Edges",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Convert") && hasWord(op, "Edges") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGraph {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Convert To Edges expects a Graph, got %s", in)}
		}
		out := ir.List(ir.Tuple(in.Elem, in.Elem, ir.Int()))
		return &ir.Node{
			Prim: "Convert To Edges", In: in, Out: out,
			Display: "Convert To Edges", Swappable: true,
			Meta: map[string]any{"node": in.Elem},
			Pos:  pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GraphValue)
				if !ok {
					return nil, runtimeErr("Convert To Edges", pos,
						"expected a Graph, got %s", ir.DescribeValue(v))
				}
				return g.Edges(), nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// The search vocabulary over an explicit graph.
//
// BFS, Dijkstra and Connected Components have always taken a Grid — they were
// the *geometry* half of graph search — and Explore takes a seed plus a
// successor lambda, so its graph is implicit. Neither reaches a graph parsed
// out of text. These are the same three questions asked of a Graph<K>, plus
// the one a weighted graph makes worth asking on its own: the path itself.
//
// The distance results are Map<K, Int> rather than a K-indexed list because a
// map is the shape the rest of the vocabulary already consumes (Convert To
// Entries, Sort By, Map Values), and because an unreachable node is *absent*
// rather than -1: a graph has no "every position" obligation the way a dense
// grid does, so there is no cell to put a sentinel in.
// ---------------------------------------------------------------------------

// graphStart resolves a From:/To: argument to a node index, reporting a clean
// error when the node is not in the graph.
func graphStart(g *ir.GraphValue, m MeasuredValue, prim, role string, v ir.Value, pos token.Position) (int, error) {
	n, err := m.Resolve(v)
	if err != nil {
		return 0, err
	}
	i, ok := g.IndexOf(n)
	if !ok {
		return 0, runtimeErr(prim, pos, "%s: %s is not a node of the graph", role, ir.FormatValue(n))
	}
	return i, nil
}

// graphBFS returns the step distance from start to every reachable node, in
// discovery order — so the map iterates outward from the start.
func graphBFS(g *ir.GraphValue, start int) *ir.MapValue {
	dist := make([]int64, g.Len())
	seen := make([]bool, g.Len())
	out := ir.NewMapSized(g.Len())

	var q ir.Queue[int]
	seen[start] = true
	q.Push(start)
	out.Put(g.NodeAt(start), int64(0))
	for {
		cur, ok := q.Pop()
		if !ok {
			break
		}
		for _, e := range g.AdjOf(cur) {
			if seen[e.To] {
				continue
			}
			seen[e.To] = true
			dist[e.To] = dist[cur] + 1
			out.Put(g.NodeAt(e.To), dist[e.To])
			q.Push(e.To)
		}
	}
	return out
}

// graphDijkstra returns the cheapest total weight from start to every
// reachable node, in *settled* order — the order a min-heap pops them, which
// is by increasing cost. Negative weights are refused: the settled-once
// invariant a priority queue rests on does not hold for them, and silently
// returning a wrong answer is worse than saying so.
func graphDijkstra(g *ir.GraphValue, start int, prim string, pos token.Position) (*ir.MapValue, error) {
	for i := range g.Len() {
		for _, e := range g.AdjOf(i) {
			if e.W < 0 {
				return nil, runtimeErr(prim, pos,
					"negative edge weight %d from %s to %s; Dijkstra needs non-negative weights",
					e.W, ir.FormatValue(g.NodeAt(i)), ir.FormatValue(g.NodeAt(e.To)))
			}
		}
	}
	const none = int64(-1)
	best := make([]int64, g.Len())
	for i := range best {
		best[i] = none
	}
	settled := make([]bool, g.Len())
	out := ir.NewMapSized(g.Len())

	var pq ir.PQ[int]
	best[start] = 0
	pq.Push(start, 0)
	for pq.Len() > 0 {
		cur, d, _ := pq.Pop()
		if settled[cur] {
			continue
		}
		settled[cur] = true
		out.Put(g.NodeAt(cur), d)
		for _, e := range g.AdjOf(cur) {
			nd := d + e.W
			if best[e.To] == none || nd < best[e.To] {
				best[e.To] = nd
				pq.Push(e.To, nd)
			}
		}
	}
	return out, nil
}

// graphComponents counts **weakly** connected components: the arcs are read as
// undirected, which is the question "how many separate pieces is this" almost
// always means. Strong connectivity is a different algorithm and a different
// question, and nothing in the vocabulary asks it yet.
func graphComponents(g *ir.GraphValue) int64 {
	uf := ir.NewUnionFind(g.Len())
	for i := range g.Len() {
		for _, e := range g.AdjOf(i) {
			uf.Union(i, e.To)
		}
	}
	return int64(uf.Count())
}

// graphShortestPath returns the cheapest path from start to goal as a list of
// nodes, or the empty list when the goal is unreachable. The path includes both
// endpoints, so a path from a node to itself is that one node.
func graphShortestPath(g *ir.GraphValue, start, goal int, prim string, pos token.Position) ([]ir.Value, error) {
	for i := range g.Len() {
		for _, e := range g.AdjOf(i) {
			if e.W < 0 {
				return nil, runtimeErr(prim, pos,
					"negative edge weight %d from %s to %s; Shortest Path needs non-negative weights",
					e.W, ir.FormatValue(g.NodeAt(i)), ir.FormatValue(g.NodeAt(e.To)))
			}
		}
	}
	const none = int64(-1)
	best := make([]int64, g.Len())
	prev := make([]int, g.Len())
	for i := range best {
		best[i], prev[i] = none, -1
	}
	settled := make([]bool, g.Len())

	var pq ir.PQ[int]
	best[start] = 0
	pq.Push(start, 0)
	for pq.Len() > 0 {
		cur, d, _ := pq.Pop()
		if settled[cur] {
			continue
		}
		settled[cur] = true
		if cur == goal {
			break
		}
		for _, e := range g.AdjOf(cur) {
			nd := d + e.W
			if best[e.To] == none || nd < best[e.To] {
				best[e.To], prev[e.To] = nd, cur
				pq.Push(e.To, nd)
			}
		}
	}
	if best[goal] == none {
		return []ir.Value{}, nil
	}
	// Walk the predecessors back and reverse, so the path reads start-to-goal.
	var rev []ir.Value
	for at := goal; at != -1; at = prev[at] {
		rev = append(rev, g.NodeAt(at))
	}
	out := make([]ir.Value, len(rev))
	for i, n := range rev {
		out[len(rev)-1-i] = n
	}
	return out, nil
}

var shortestPath = &Primitive{
	ID:      "Shortest Path",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Shortest") && hasWord(op, "Path") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGraph {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Shortest Path expects a Graph, got %s", in)}
		}
		fromM, err := measuredValue(args, "Shortest Path", "Start", in, pos)
		if err != nil {
			return nil, err
		}
		toM, err := measuredValue(args, "Shortest Path", "Goal", in, pos)
		if err != nil {
			return nil, err
		}
		for _, m := range []MeasuredValue{fromM, toM} {
			if !m.Type.Equal(in.Elem) {
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Shortest Path: %s: is %s but the graph's nodes are %s", m.Name, m.Type, in.Elem)}
			}
		}
		meta := map[string]any{"node": in.Elem}
		fromM.Meta(meta, "start")
		toM.Meta(meta, "goal")
		return &ir.Node{
			Prim: "Shortest Path", In: in, Out: ir.List(in.Elem),
			Display:   fmt.Sprintf("Shortest Path from %s to %s", fromM.Describe(), toM.Describe()),
			Swappable: true,
			Meta:      meta,
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GraphValue)
				if !ok {
					return nil, runtimeErr("Shortest Path", pos,
						"expected a Graph, got %s", ir.DescribeValue(v))
				}
				start, err := graphStart(g, fromM, "Shortest Path", "Start", v, pos)
				if err != nil {
					return nil, err
				}
				goal, err := graphStart(g, toM, "Shortest Path", "Goal", v, pos)
				if err != nil {
					return nil, err
				}
				return graphShortestPath(g, start, goal, "Shortest Path", pos)
			},
		}, nil
	},
}

// graphSearchNode builds the BFS and Dijkstra nodes over a Graph. They differ
// only in which distance they compute, so the argument handling, the type and
// the error wording live here once.
func graphSearchNode(prim string, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
	fromM, err := measuredValue(args, prim, "Start", in, pos)
	if err != nil {
		return nil, err
	}
	if !fromM.Type.Equal(in.Elem) {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s: Start: is %s but the graph's nodes are %s", prim, fromM.Type, in.Elem)}
	}
	meta := map[string]any{"graph": true, "node": in.Elem}
	fromM.Meta(meta, "start")
	return &ir.Node{
		Prim: prim, In: in, Out: ir.Map(in.Elem, ir.Int()),
		Display:   fmt.Sprintf("%s from %s", prim, fromM.Describe()),
		Swappable: true,
		Meta:      meta,
		Pos:       pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			g, ok := v.(*ir.GraphValue)
			if !ok {
				return nil, runtimeErr(prim, pos, "expected a Graph, got %s", ir.DescribeValue(v))
			}
			start, err := graphStart(g, fromM, prim, "Start", v, pos)
			if err != nil {
				return nil, err
			}
			if prim == "Dijkstra" {
				return graphDijkstra(g, start, prim, pos)
			}
			return graphBFS(g, start), nil
		},
	}, nil
}

// ---------------------------------------------------------------------------
// The graph vocabulary at the pipeline level.
//
// Each of these is a question a stage can ask of a Graph<K> without dropping
// into an Apply. Root and Convert To Adjacency are the stage forms of things
// the expression layer can already say — worth having because a parsed edge
// list arrives *as a stage*, and detouring through a lambda to ask one
// question of it is the detour Convert To Edges exists to avoid. Minimum
// Spanning Tree and Strongly Connected Components are neither: they are
// algorithms with nowhere else to live, which is what Domain Expansion is for.
// ---------------------------------------------------------------------------

// graphInput is the shared guard: these all take a Graph and nothing else.
func graphInput(prim string, in *ir.Type, pos token.Position) error {
	if in == nil || in.Kind != ir.KGraph {
		return &ResolveError{Pos: pos, Msg: fmt.Sprintf("%s expects a Graph, got %s", prim, in)}
	}
	return nil
}

// graphRootOf is the stage form of the `root` builtin, and says the same three
// things it does — the wording is deliberately identical, so a reader who has
// met one recognizes the other.
func graphRootOf(g *ir.GraphValue, prim string, pos token.Position) (ir.Value, error) {
	roots := g.Roots()
	switch {
	case len(roots) == 1:
		return roots[0], nil
	case g.Len() == 0:
		return nil, runtimeErr(prim, pos, "the graph is empty")
	case len(roots) == 0:
		return nil, runtimeErr(prim, pos, "every node has an incoming arc, so the graph has no root")
	default:
		names := make([]string, 0, 3)
		for i, n := range roots {
			if i == 3 {
				names = append(names, "...")
				break
			}
			names = append(names, ir.FormatValue(n))
		}
		return nil, runtimeErr(prim, pos, "%d nodes have no incoming arc (%s); a rooted graph has exactly one",
			len(roots), strings.Join(names, ", "))
	}
}

var graphRoot = &Primitive{
	ID:      "Root",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Root") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if err := graphInput("Root", in, pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim: "Root", In: in, Out: in.Elem,
			Display: "Root", Swappable: true,
			Meta: map[string]any{"node": in.Elem}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GraphValue)
				if !ok {
					return nil, runtimeErr("Root", pos, "expected a Graph, got %s", ir.DescribeValue(v))
				}
				return graphRootOf(g, "Root", pos)
			},
		}, nil
	},
}

// graphMST is Kruskal over the arcs read as **undirected**: a spanning tree is
// a question about connection, and a directed graph's arcs are what connect it.
//
// Arcs are taken cheapest first, ties broken by the order they were inserted,
// so the tree is the same one on both backends and across runs. A graph in
// several pieces gives a spanning *forest* rather than an error — that is the
// same answer, one tree per piece, and refusing it would make the primitive
// useless on exactly the input that most wants it.
//
// The chosen arcs keep the direction they had. Undirected is a reading, not a
// rewrite; `undirected(...)` is there for a caller who wants both ways.
func graphMST(g *ir.GraphValue) *ir.GraphValue {
	type arc struct {
		w    int64
		from int
		to   int
	}
	var arcs []arc
	for i := range g.Len() {
		for _, e := range g.AdjOf(i) {
			if i == e.To {
				continue // a self-loop connects nothing
			}
			arcs = append(arcs, arc{w: e.W, from: i, to: e.To})
		}
	}
	slices.SortStableFunc(arcs, func(a, b arc) int {
		if a.w != b.w {
			return cmp.Compare(a.w, b.w)
		}
		if a.from != b.from {
			return cmp.Compare(a.from, b.from)
		}
		return cmp.Compare(a.to, b.to)
	})

	out := ir.NewGraphSized(g.Len())
	for _, n := range g.Nodes() {
		out.AddNode(n) // an isolated node is a piece of the forest too
	}
	uf := ir.NewUnionFind(g.Len())
	for _, a := range arcs {
		if uf.Find(a.from) == uf.Find(a.to) {
			continue // both ends already joined; this arc would close a cycle
		}
		uf.Union(a.from, a.to)
		out.AddEdge(g.NodeAt(a.from), g.NodeAt(a.to), a.w)
	}
	return out
}

var minimumSpanningTree = &Primitive{
	ID:      "Minimum Spanning Tree",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Spanning") && hasWord(op, "Tree") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if err := graphInput("Minimum Spanning Tree", in, pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim: "Minimum Spanning Tree", In: in, Out: in,
			Display: "Minimum Spanning Tree", Swappable: true,
			Meta: map[string]any{"node": in.Elem}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GraphValue)
				if !ok {
					return nil, runtimeErr("Minimum Spanning Tree", pos,
						"expected a Graph, got %s", ir.DescribeValue(v))
				}
				return graphMST(g), nil
			},
		}, nil
	},
}

// graphSCC is Kosaraju: one pass over the arcs recording finish order, then a
// pass over the reversed arcs taking each unassigned node's component in
// reverse finish order.
//
// Both walks take adjacency in insertion order and both are iterative, so a
// graph deeper than a stack is a question about memory rather than about
// whether the answer arrives. Each component's nodes come back in the graph's
// insertion order, and the components in the order the second pass finds them,
// which is a topological order of the condensation.
func graphSCC(g *ir.GraphValue) [][]int {
	n := g.Len()
	radj := make([][]int, n)
	for i := range n {
		for _, e := range g.AdjOf(i) {
			radj[e.To] = append(radj[e.To], i)
		}
	}

	// Pass one. `next` is how far into a node's arcs the walk has got, which is
	// what makes the depth-first order the same as a recursive one's.
	seen := make([]bool, n)
	order := make([]int, 0, n)
	next := make([]int, n)
	for start := range n {
		if seen[start] {
			continue
		}
		seen[start] = true
		stack := []int{start}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			arcs := g.AdjOf(cur)
			if next[cur] < len(arcs) {
				to := arcs[next[cur]].To
				next[cur]++
				if !seen[to] {
					seen[to] = true
					stack = append(stack, to)
				}
				continue
			}
			order = append(order, cur)
			stack = stack[:len(stack)-1]
		}
	}

	// Pass two, over the reversed arcs, in reverse finish order.
	comp := make([]int, n)
	for i := range comp {
		comp[i] = -1
	}
	var out [][]int
	for i := len(order) - 1; i >= 0; i-- {
		root := order[i]
		if comp[root] != -1 {
			continue
		}
		id := len(out)
		var members []int
		stack := []int{root}
		comp[root] = id
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			members = append(members, cur)
			for _, from := range radj[cur] {
				if comp[from] == -1 {
					comp[from] = id
					stack = append(stack, from)
				}
			}
		}
		slices.Sort(members) // insertion order within the component
		out = append(out, members)
	}
	return out
}

var stronglyConnectedComponents = &Primitive{
	ID:      "Strongly Connected Components",
	Keyword: "Domain Expansion",
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Strongly") && hasWord(op, "Connected") && hasWord(op, "Components")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if err := graphInput("Strongly Connected Components", in, pos); err != nil {
			return nil, err
		}
		// The components themselves, not a count. Connected Components answers
		// an Int because the pieces of an undirected graph are rarely the
		// question; here they are, and `Count` is one stage away while the
		// members are not recoverable from a number.
		return &ir.Node{
			Prim: "Strongly Connected Components", In: in, Out: ir.List(ir.List(in.Elem)),
			Display: "Strongly Connected Components", Swappable: true,
			Meta: map[string]any{"node": in.Elem}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GraphValue)
				if !ok {
					return nil, runtimeErr("Strongly Connected Components", pos,
						"expected a Graph, got %s", ir.DescribeValue(v))
				}
				comps := graphSCC(g)
				out := make([]ir.Value, len(comps))
				for i, members := range comps {
					nodes := make([]ir.Value, len(members))
					for k, m := range members {
						nodes[k] = g.NodeAt(m)
					}
					out[i] = nodes
				}
				return out, nil
			},
		}, nil
	},
}

var convertToAdjacency = &Primitive{
	ID:      "Convert To Adjacency",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Convert") && hasWord(op, "Adjacency") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if err := graphInput("Convert To Adjacency", in, pos); err != nil {
			return nil, err
		}
		// The inverse of the adjacency-map form Convert To Graph accepts, and
		// it drops the weights: a Map<K, List<K>> has nowhere to put them.
		// Convert To Edges is the way out that keeps them.
		out := ir.Map(in.Elem, ir.List(in.Elem))
		return &ir.Node{
			Prim: "Convert To Adjacency", In: in, Out: out,
			Display: "Convert To Adjacency", Swappable: true,
			Meta: map[string]any{"node": in.Elem}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GraphValue)
				if !ok {
					return nil, runtimeErr("Convert To Adjacency", pos,
						"expected a Graph, got %s", ir.DescribeValue(v))
				}
				m := ir.NewMapSized(g.Len())
				for i := range g.Len() {
					// Every node is a key, isolated ones included: an
					// adjacency listing with a node missing is a different
					// graph, and this is the shape Topological Sort reads.
					succ := make([]ir.Value, len(g.AdjOf(i)))
					for k, e := range g.AdjOf(i) {
						succ[k] = g.NodeAt(e.To)
					}
					m.Put(g.NodeAt(i), succ)
				}
				return m, nil
			},
		}, nil
	},
}
