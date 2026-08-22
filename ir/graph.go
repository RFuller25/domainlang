package ir

import "slices"

// GraphValue is the explicit graph: a directed, Int-weighted adjacency over
// keyable nodes.
//
// The language already had three ways to describe a graph and no way to *hold*
// one. BFS/Dijkstra/Flood Fill/Connected Components take a Grid, so they only
// reach geometry; Explore takes a seed and a successor lambda, so the graph is
// implicit and cannot be built once and queried repeatedly; Topological Sort
// takes an adjacency Map or an edge list, which has to be rebuilt at every
// stage and has nowhere to put a weight. This is the value those all wanted.
//
// Semantics locked here (both backends and the docs follow them):
//
//   - **Directed.** An undirected graph is one with both arcs, which
//     `Convert To Graph`'s `Mode: Undirected` inserts. One representation, one
//     set of algorithms; a flag on the value would leak into equality,
//     rendering and every primitive that touches it.
//   - **Int-weighted, default 1.** An unweighted graph is one whose weights are
//     all 1, so no algorithm needs a separate unweighted path.
//   - **Insertion-ordered**, like MapValue and SetValue and unlike SparseValue
//     (which sorts, because it is geometry). Determinism is the requirement;
//     insertion order is the one both backends can reproduce without imposing
//     an ordering on the node type.
//   - **A node exists once.** AddEdge brings its endpoints into the graph, so
//     an edge list alone is a complete description; AddNode is for the isolated
//     ones an edge list cannot mention.
//   - **Last write wins on a repeated edge.** Adding a->b twice keeps one arc
//     with the later weight, matching how Put on a Map behaves. Parallel edges
//     would make `weight(g, a, b)` ambiguous, and nothing in the vocabulary
//     asks for them.
//
// Adjacency is stored as indices into nodes rather than as keys: it keeps the
// algorithms cheap (a visited set is a []bool), and it is the representation
// the compiled backend mirrors.
type GraphValue struct {
	nodes []Value        // insertion order
	index map[any]int    // KeyOf(node) -> position in nodes
	adj   [][]GraphEdge  // parallel to nodes
	edges map[[2]int]int // (from, to) -> position in adj[from], so a repeat replaces
}

// GraphEdge is one arc: the destination's index in the owning graph's node
// list, and the arc's weight.
type GraphEdge struct {
	To int
	W  int64
}

func NewGraphValue() *GraphValue {
	return &GraphValue{index: map[any]int{}, edges: map[[2]int]int{}}
}

// NewGraphSized is NewGraphValue with a known node count, so building a graph
// from an edge list sizes its maps once instead of growing through them.
func NewGraphSized(n int) *GraphValue {
	return &GraphValue{
		nodes: make([]Value, 0, n),
		index: make(map[any]int, n),
		adj:   make([][]GraphEdge, 0, n),
		edges: map[[2]int]int{},
	}
}

// AddNode brings a node into the graph if it is not already there, returning
// its index. Adding an existing node is a no-op — it must not reset its arcs.
func (g *GraphValue) AddNode(n Value) int {
	k := KeyOf(n)
	if i, ok := g.index[k]; ok {
		return i
	}
	i := len(g.nodes)
	g.nodes = append(g.nodes, n)
	g.adj = append(g.adj, nil)
	g.index[k] = i
	return i
}

// AddEdge adds (or re-weights) the arc from a to b, bringing both endpoints
// into the graph.
func (g *GraphValue) AddEdge(a, b Value, w int64) {
	i, j := g.AddNode(a), g.AddNode(b)
	if at, ok := g.edges[[2]int{i, j}]; ok {
		g.adj[i][at].W = w // last write wins, keeping the arc's position
		return
	}
	g.edges[[2]int{i, j}] = len(g.adj[i])
	g.adj[i] = append(g.adj[i], GraphEdge{To: j, W: w})
}

// DelEdge removes the arc from a to b if it is there. The nodes stay: deleting
// an arc must not silently drop a node that other arcs still reach.
func (g *GraphValue) DelEdge(a, b Value) {
	i, ok := g.IndexOf(a)
	if !ok {
		return
	}
	j, ok := g.IndexOf(b)
	if !ok {
		return
	}
	at, ok := g.edges[[2]int{i, j}]
	if !ok {
		return
	}
	g.adj[i] = slices.Delete(g.adj[i], at, at+1)
	delete(g.edges, [2]int{i, j})
	// Every arc after the removed one shifted left by one.
	for k := at; k < len(g.adj[i]); k++ {
		g.edges[[2]int{i, g.adj[i][k].To}] = k
	}
}

// IndexOf reports a node's position, and whether it is in the graph at all.
func (g *GraphValue) IndexOf(n Value) (int, bool) {
	i, ok := g.index[KeyOf(n)]
	return i, ok
}

// Has reports whether the node is in the graph.
func (g *GraphValue) Has(n Value) bool {
	_, ok := g.index[KeyOf(n)]
	return ok
}

// Len is the number of nodes.
func (g *GraphValue) Len() int { return len(g.nodes) }

// Nodes returns the nodes in insertion order.
func (g *GraphValue) Nodes() []Value { return g.nodes }

// NodeAt returns the node at an adjacency index.
func (g *GraphValue) NodeAt(i int) Value { return g.nodes[i] }

// AdjOf returns the raw arcs out of a node index, in insertion order.
func (g *GraphValue) AdjOf(i int) []GraphEdge { return g.adj[i] }

// Neighbors returns the destinations of a node's out-arcs, in insertion order.
// An absent node has no neighbours rather than being an error: it is the same
// answer, and it keeps the builtin total.
func (g *GraphValue) Neighbors(n Value) []Value {
	i, ok := g.IndexOf(n)
	if !ok {
		return nil
	}
	out := make([]Value, len(g.adj[i]))
	for k, e := range g.adj[i] {
		out[k] = g.nodes[e.To]
	}
	return out
}

// EdgesOf returns a node's out-arcs as (destination, weight) pairs.
func (g *GraphValue) EdgesOf(n Value) []Value {
	i, ok := g.IndexOf(n)
	if !ok {
		return nil
	}
	out := make([]Value, len(g.adj[i]))
	for k, e := range g.adj[i] {
		out[k] = []Value{g.nodes[e.To], e.W}
	}
	return out
}

// Edges returns every arc as a (from, to, weight) triple, nodes in insertion
// order and each node's arcs in theirs.
func (g *GraphValue) Edges() []Value {
	out := make([]Value, 0, len(g.edges))
	for i, arcs := range g.adj {
		for _, e := range arcs {
			out = append(out, []Value{g.nodes[i], g.nodes[e.To], e.W})
		}
	}
	return out
}

// EdgeCount is the number of arcs.
func (g *GraphValue) EdgeCount() int { return len(g.edges) }

// Weight returns the weight of a->b and whether the arc exists.
func (g *GraphValue) Weight(a, b Value) (int64, bool) {
	i, ok := g.IndexOf(a)
	if !ok {
		return 0, false
	}
	j, ok := g.IndexOf(b)
	if !ok {
		return 0, false
	}
	at, ok := g.edges[[2]int{i, j}]
	if !ok {
		return 0, false
	}
	return g.adj[i][at].W, true
}

// HasEdge reports whether a->b is an arc.
func (g *GraphValue) HasEdge(a, b Value) bool {
	_, ok := g.Weight(a, b)
	return ok
}

// Degree is a node's out-arc count. An absent node has degree 0.
func (g *GraphValue) Degree(n Value) int {
	i, ok := g.IndexOf(n)
	if !ok {
		return 0
	}
	return len(g.adj[i])
}

// Clone returns an independent copy. The expression layer's addnode/addedge/
// deledge are functional — they must not alias the original.
func (g *GraphValue) Clone() *GraphValue {
	out := &GraphValue{
		nodes: make([]Value, len(g.nodes)),
		index: make(map[any]int, len(g.index)),
		adj:   make([][]GraphEdge, len(g.adj)),
		edges: make(map[[2]int]int, len(g.edges)),
	}
	copy(out.nodes, g.nodes)
	for k, v := range g.index {
		out.index[k] = v
	}
	for i, arcs := range g.adj {
		out.adj[i] = slices.Clone(arcs)
	}
	for k, v := range g.edges {
		out.edges[k] = v
	}
	return out
}

// Flipped returns the graph with every arc reversed, nodes keeping their order.
func (g *GraphValue) Flipped() *GraphValue {
	out := NewGraphSized(len(g.nodes))
	for _, n := range g.nodes {
		out.AddNode(n)
	}
	for i, arcs := range g.adj {
		for _, e := range arcs {
			out.AddEdge(g.nodes[e.To], g.nodes[i], e.W)
		}
	}
	return out
}

// Subgraph returns the graph restricted to the given nodes, keeping only arcs
// with both endpoints in the set. Nodes are taken in the order given, and one
// that is not in the graph is skipped rather than invented.
func (g *GraphValue) Subgraph(keep []Value) *GraphValue {
	want := make(map[any]bool, len(keep))
	out := NewGraphSized(len(keep))
	for _, n := range keep {
		if g.Has(n) {
			want[KeyOf(n)] = true
			out.AddNode(n)
		}
	}
	for i, arcs := range g.adj {
		if !want[KeyOf(g.nodes[i])] {
			continue
		}
		for _, e := range arcs {
			if want[KeyOf(g.nodes[e.To])] {
				out.AddEdge(g.nodes[i], g.nodes[e.To], e.W)
			}
		}
	}
	return out
}

// GraphEqual reports whether two graphs have the same nodes and the same arcs,
// **independent of insertion order**.
//
// This is a deliberate divergence from how MapValue and SetValue compare, where
// order is part of the value because it is part of the rendering. Two graphs
// built by reading the same edges in a different order are the same graph, and
// a fixed-point loop over a graph would otherwise never converge. Rendering
// still follows insertion order — equality and rendering answer different
// questions here, which is the reason this is a named function rather than a
// case inside DeepEqual's slice walk.
func GraphEqual(a, b *GraphValue) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a.nodes) != len(b.nodes) || len(a.edges) != len(b.edges) {
		return false
	}
	for k := range a.index {
		if _, ok := b.index[k]; !ok {
			return false
		}
	}
	for i, arcs := range a.adj {
		from := a.nodes[i]
		for _, e := range arcs {
			w, ok := b.Weight(from, a.nodes[e.To])
			if !ok || w != e.W {
				return false
			}
		}
	}
	return true
}

// Roots returns the nodes with no incoming arc, in insertion order.
//
// This is the "who is the top of this" question an edge list parsed out of
// text asks — a `Convert To Graph`ed `parent -> child` listing has exactly one
// root when it is a tree, none when a cycle swallows every node, and several
// when it is a forest. Answering with the whole list rather than a single node
// keeps the method total; the `root` builtin is what turns "not exactly one"
// into an error a reader can act on.
//
// A self-loop counts as an incoming arc, so a node that only points at itself
// is not a root: it is unreachable from anywhere else, which is the property a
// root is being asked about.
func (g *GraphValue) Roots() []Value {
	incoming := make([]bool, len(g.nodes))
	for _, arcs := range g.adj {
		for _, e := range arcs {
			incoming[e.To] = true
		}
	}
	out := make([]Value, 0, 1)
	for i, n := range g.nodes {
		if !incoming[i] {
			out = append(out, n)
		}
	}
	return out
}

// WeightOf is the total weight of a node's out-arcs — Degree with the weights
// counted rather than the arcs. An absent node weighs 0, for the reason it has
// degree 0: the readers are total.
//
// Out-arcs, like Degree, Neighbors and EdgesOf. The in-weight is
// g.Flipped().WeightOf(n), which is the same trick the rest of the vocabulary
// uses to ask a question of the other direction.
func (g *GraphValue) WeightOf(n Value) int64 {
	i, ok := g.IndexOf(n)
	if !ok {
		return 0
	}
	var total int64
	for _, e := range g.adj[i] {
		total += e.W
	}
	return total
}

// Leaves returns the nodes with no outgoing arc, in insertion order — the
// other end of the question Roots asks. A node with neither arc is both a root
// and a leaf, which is the right answer for an isolated one.
func (g *GraphValue) Leaves() []Value {
	out := make([]Value, 0, 1)
	for i, n := range g.nodes {
		if len(g.adj[i]) == 0 {
			out = append(out, n)
		}
	}
	return out
}

// InDegree is a node's incoming-arc count, the mirror of Degree.
//
// It costs a walk of the whole adjacency, because arcs are stored only in the
// direction they point. That is still cheaper than the g.Flipped().Degree(n)
// it replaces, which builds an entire reversed graph to count one node's arcs.
// An absent node has in-degree 0, like Degree.
func (g *GraphValue) InDegree(n Value) int {
	j, ok := g.IndexOf(n)
	if !ok {
		return 0
	}
	count := 0
	for _, arcs := range g.adj {
		for _, e := range arcs {
			if e.To == j {
				count++
			}
		}
	}
	return count
}

// WeightSum is the total weight of every arc in the graph.
func (g *GraphValue) WeightSum() int64 {
	var total int64
	for _, arcs := range g.adj {
		for _, e := range arcs {
			total += e.W
		}
	}
	return total
}

// DelNode returns a copy without the node and without every arc that touched
// it, in either direction. The nodes that remain keep their order.
//
// DelEdge's counterpart: a node could be brought in by AddNode or by an arc's
// endpoint and never leave. It is expressed as a Subgraph because that is
// exactly what it is — everything but one node — and a rebuild is unavoidable
// either way, since the adjacency is held as indices into the node list.
func (g *GraphValue) DelNode(n Value) *GraphValue {
	if !g.Has(n) {
		return g.Clone()
	}
	keep := make([]Value, 0, len(g.nodes)-1)
	k := KeyOf(n)
	for _, node := range g.nodes {
		if KeyOf(node) != k {
			keep = append(keep, node)
		}
	}
	return g.Subgraph(keep)
}

// Reachable returns every node reachable from the start by following arcs, in
// breadth-first discovery order — so the list reads outward from the start,
// the same order graph BFS settles them in.
//
// The start is included: it is reachable from itself by a path of no arcs, the
// same reason Shortest Path from a node to itself is that one node. A node the
// graph does not have reaches nothing, rather than being an error, so the
// builtin over this stays total.
func (g *GraphValue) Reachable(start Value) []Value {
	i, ok := g.IndexOf(start)
	if !ok {
		return []Value{}
	}
	seen := make([]bool, len(g.nodes))
	out := make([]Value, 0, 1)

	var q Queue[int]
	seen[i] = true
	q.Push(i)
	for {
		cur, ok := q.Pop()
		if !ok {
			break
		}
		out = append(out, g.nodes[cur])
		for _, e := range g.adj[cur] {
			if seen[e.To] {
				continue
			}
			seen[e.To] = true
			q.Push(e.To)
		}
	}
	return out
}

// HasCycle reports whether the graph has a directed cycle, by the same measure
// Topological Sort uses: peel nodes with nothing left pointing at them, and a
// cycle is what is left over when nothing can be peeled. A self-loop is a
// cycle — it is a node that can never be ordered before itself.
func (g *GraphValue) HasCycle() bool {
	indeg := make([]int, len(g.nodes))
	for _, arcs := range g.adj {
		for _, e := range arcs {
			indeg[e.To]++
		}
	}
	var q Queue[int]
	for i, d := range indeg {
		if d == 0 {
			q.Push(i)
		}
	}
	ordered := 0
	for {
		cur, ok := q.Pop()
		if !ok {
			break
		}
		ordered++
		for _, e := range g.adj[cur] {
			indeg[e.To]--
			if indeg[e.To] == 0 {
				q.Push(e.To)
			}
		}
	}
	return ordered != len(g.nodes)
}

// Undirected returns the graph with every arc's reverse present, so a walk can
// go either way along it.
//
// An arc that is already there is left alone rather than re-weighted: a->b
// weighing 3 and b->a weighing 5 is a graph that means both, and the mode on
// `Convert To Graph` inserts a pair at one weight only because it is reading
// an edge list where no reverse exists yet. Only the missing direction is
// added, at the weight of the arc it mirrors.
func (g *GraphValue) Undirected() *GraphValue {
	out := g.Clone()
	for i, arcs := range g.adj {
		for _, e := range arcs {
			if !out.HasEdge(g.nodes[e.To], g.nodes[i]) {
				out.AddEdge(g.nodes[e.To], g.nodes[i], e.W)
			}
		}
	}
	return out
}

// MergeGraphs returns the union of two graphs: every node of a in its order,
// then b's new ones, and every arc of both.
//
// An arc both graphs carry takes b's weight, because that is what writing b's
// arcs into a does and AddEdge's rule is last write wins. Neither argument is
// touched.
func MergeGraphs(a, b *GraphValue) *GraphValue {
	out := a.Clone()
	for _, n := range b.nodes {
		out.AddNode(n)
	}
	for i, arcs := range b.adj {
		for _, e := range arcs {
			out.AddEdge(b.nodes[i], b.nodes[e.To], e.W)
		}
	}
	return out
}
