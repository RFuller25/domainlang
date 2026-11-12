package codegen

import (
	"regexp"
	"strings"

	"domain/ir"
)

// The list-keyed Graph and Topological Sort runtime.
//
// dmGraph indexes its nodes with Go's own map, so its node type must be
// comparable. A Graph whose node type holds a List (ir.ContainsList) is the
// different Go type dmLGraph: the same fields the rest of the generator reads
// (nodes, adj, edges) and the same methods (addNode, addEdge, delEdge, weight,
// clone), with the node index a map[string]int keyed by the injective encoding
// of codegen/keyfn.go. The encoder travels with the value (kf), as it does in
// dmLSet and dmLMap, and every constructor copies it from the graph it derives
// from.
//
// Every helper beyond the type itself (neighbors, reachable, Kruskal, Kosaraju,
// …) is the plain helper's source with its names and its three kinds of hash
// access rewritten by lgraphDecl, so the two forms cannot drift apart: the
// plain declaration is the single source of the algorithm.
//
// Programs whose graph nodes have no List never mention any of this, and their
// generated code is unchanged.

const declLGraph = `type dmLGraphEdge struct {
	to int
	w  int64
}

type dmLGraph[K any] struct {
	nodes []K
	index map[string]int
	adj   [][]dmLGraphEdge
	edges map[[2]int]int
	kf    func(K) string
}

func dmNewLGraph[K any](kf func(K) string) dmLGraph[K] {
	return dmLGraph[K]{index: map[string]int{}, edges: map[[2]int]int{}, kf: kf}
}

func (g dmLGraph[K]) lookup(n K) (int, bool) {
	if len(g.nodes) == 0 {
		return 0, false
	}
	i, ok := g.index[g.kf(n)]
	return i, ok
}

func (g *dmLGraph[K]) addNode(n K) int {
	k := g.kf(n)
	if i, ok := g.index[k]; ok {
		return i
	}
	i := len(g.nodes)
	g.nodes = append(g.nodes, n)
	g.adj = append(g.adj, nil)
	g.index[k] = i
	return i
}

func (g *dmLGraph[K]) addEdge(a, b K, w int64) {
	i, j := g.addNode(a), g.addNode(b)
	if at, ok := g.edges[[2]int{i, j}]; ok {
		g.adj[i][at].w = w
		return
	}
	g.edges[[2]int{i, j}] = len(g.adj[i])
	g.adj[i] = append(g.adj[i], dmLGraphEdge{to: j, w: w})
}

func (g *dmLGraph[K]) delEdge(a, b K) {
	i, ok := g.lookup(a)
	if !ok {
		return
	}
	j, ok := g.lookup(b)
	if !ok {
		return
	}
	at, ok := g.edges[[2]int{i, j}]
	if !ok {
		return
	}
	g.adj[i] = append(g.adj[i][:at], g.adj[i][at+1:]...)
	delete(g.edges, [2]int{i, j})
	for k := at; k < len(g.adj[i]); k++ {
		g.edges[[2]int{i, g.adj[i][k].to}] = k
	}
}

func (g dmLGraph[K]) weight(a, b K) (int64, bool) {
	i, ok := g.lookup(a)
	if !ok {
		return 0, false
	}
	j, ok := g.lookup(b)
	if !ok {
		return 0, false
	}
	at, ok := g.edges[[2]int{i, j}]
	if !ok {
		return 0, false
	}
	return g.adj[i][at].w, true
}

func (g dmLGraph[K]) clone() dmLGraph[K] {
	out := dmLGraph[K]{
		nodes: append([]K(nil), g.nodes...),
		index: make(map[string]int, len(g.index)),
		adj:   make([][]dmLGraphEdge, len(g.adj)),
		edges: make(map[[2]int]int, len(g.edges)),
		kf:    g.kf,
	}
	for k, v := range g.index {
		out.index[k] = v
	}
	for i, arcs := range g.adj {
		out.adj[i] = append([]dmLGraphEdge(nil), arcs...)
	}
	for k, v := range g.edges {
		out.edges[k] = v
	}
	return out
}`

// The three helpers whose plain form compares or hashes a node itself rather
// than going through the index, so the rewrite cannot reach them.
const declLGraphSub = `func dmLGraphSub[K any](g dmLGraph[K], keep []K) dmLGraph[K] {
	want := make(map[string]bool, len(keep))
	out := dmNewLGraph[K](g.kf)
	for _, n := range keep {
		if _, ok := g.lookup(n); ok {
			want[g.kf(n)] = true
			out.addNode(n)
		}
	}
	for i, arcs := range g.adj {
		if !want[g.kf(g.nodes[i])] {
			continue
		}
		for _, e := range arcs {
			if want[g.kf(g.nodes[e.to])] {
				out.addEdge(g.nodes[i], g.nodes[e.to], e.w)
			}
		}
	}
	return out
}`

const declLGraphDelNode = `func dmLGraphDelNode[K any](g dmLGraph[K], n K) dmLGraph[K] {
	if _, ok := g.lookup(n); !ok {
		return g.clone()
	}
	nk := g.kf(n)
	keep := make([]K, 0, len(g.nodes)-1)
	for _, node := range g.nodes {
		if g.kf(node) != nk {
			keep = append(keep, node)
		}
	}
	return dmLGraphSub(g, keep)
}`

const declLGraphEq = `func dmLGraphEq[K any](a, b dmLGraph[K]) bool {
	if len(a.nodes) != len(b.nodes) || len(a.edges) != len(b.edges) {
		return false
	}
	for k := range a.index {
		if _, ok := b.index[k]; !ok {
			return false
		}
	}
	for i, arcs := range a.adj {
		for _, e := range arcs {
			w, ok := b.weight(a.nodes[i], a.nodes[e.to])
			if !ok || w != e.w {
				return false
			}
		}
	}
	return true
}`

const declLTopo = `type dmLTopo[K any] struct {
	order []K
	index map[string]int
	adj   [][]int
	indeg []int
	kf    func(K) string
}

func dmNewLTopo[K any](kf func(K) string, hint int) *dmLTopo[K] {
	return &dmLTopo[K]{index: make(map[string]int, hint), kf: kf}
}

func (t *dmLTopo[K]) id(n K) int {
	k := t.kf(n)
	if i, ok := t.index[k]; ok {
		return i
	}
	i := len(t.order)
	t.index[k] = i
	t.order = append(t.order, n)
	t.adj = append(t.adj, nil)
	t.indeg = append(t.indeg, 0)
	return i
}`

var (
	reLGraphName  = regexp.MustCompile(`\bdm(Graph|Topo)`)
	reLGraphIndex = regexp.MustCompile(`\b(\w+)\.index\[(\w+)\]`)
)

// lgraphDecl derives the list-keyed form of a plain graph helper.
func lgraphDecl(src string) string {
	s := reLGraphName.ReplaceAllString(src, "dmL$1")
	s = strings.ReplaceAll(s, "[K comparable]", "[K any]")
	s = strings.ReplaceAll(s, "dmNewGraph[K]()", "dmNewLGraph[K](g.kf)")
	s = strings.ReplaceAll(s, "dmNewMap[K, int64]()", "dmNewLMap[K, int64](g.kf)")
	s = strings.ReplaceAll(s, "dmMap[K,", "dmLMap[K,")
	s = strings.ReplaceAll(s, "dmNewTopo[K](len(m.keys))", "dmNewLTopo[K](m.kf, len(m.keys))")
	s = reLGraphIndex.ReplaceAllString(s, "$1.lookup($2)")
	return s
}

func init() {
	// The plain helper's name, its source, and the hand-written source of the
	// list-keyed form where the rewrite would not be sound.
	plain := []struct{ base, decl, hand string }{
		{"dmGraphNeighbors", declGraphNeighbors, ""},
		{"dmGraphDegree", declGraphDegree, ""},
		{"dmGraphWeightOf", declGraphWeightOf, ""},
		{"dmGraphRoot", declGraphRoot, ""},
		{"dmGraphWeight", declGraphWeightAt, ""},
		{"dmGraphWeightOr", declGraphWeightOr, ""},
		{"dmGraphRoots", declGraphRoots, ""},
		{"dmGraphLeaves", declGraphLeaves, ""},
		{"dmGraphInDegree", declGraphInDegree, ""},
		{"dmGraphWeightSum", declGraphWeightSum, ""},
		{"dmGraphDelNode", declGraphDelNode, declLGraphDelNode},
		{"dmGraphReachable", declGraphReachable, ""},
		{"dmGraphHasCycle", declGraphHasCycle, ""},
		{"dmGraphUndirected", declGraphUndirected, ""},
		{"dmGraphMerge", declGraphMerge, ""},
		{"dmGraphMST", declGraphMST, ""},
		{"dmGraphSCC", declGraphSCC, ""},
		{"dmGraphAddNode", declGraphAddNode, ""},
		{"dmGraphAddEdge", declGraphAddEdge, ""},
		{"dmGraphDelEdge", declGraphDelEdge, ""},
		{"dmGraphAddNodeIn", declGraphAddNodeIn, ""},
		{"dmGraphAddEdgeIn", declGraphAddEdgeIn, ""},
		{"dmGraphFlip", declGraphFlip, ""},
		{"dmGraphSub", declGraphSub, declLGraphSub},
		{"dmGraphEq", declGraphEq, declLGraphEq},
		{"dmGraphBFS", declGraphBFS, ""},
		{"dmGraphNoNeg", declGraphNoNeg, ""},
		{"dmGraphDijkstra", declGraphDijkstra, ""},
		{"dmGraphComponents", declGraphComponents, ""},
		{"dmGraphPath", declGraphPath, ""},
		{"dmTopoSort", declTopoSort, ""},
	}
	lTable["dmGraph"] = lEntry{"dmLGraph", declLGraph}
	lTable["dmTopo"] = lEntry{"dmLTopo", declLTopo + "\n\n" + lgraphTopoTail()}
	for _, p := range plain {
		decl := p.hand
		if decl == "" {
			decl = lgraphDecl(p.decl)
		}
		lTable[p.base] = lEntry{reLGraphName.ReplaceAllString(p.base, "dmL$1"), decl}
	}
}

// lgraphTopoTail is the part of dmTopo the list-keyed form shares verbatim:
// everything after the id method.
func lgraphTopoTail() string {
	i := strings.Index(declTopo, "func (t *dmTopo[K]) edge")
	return lgraphDecl(declTopo[i:])
}

// newGraphExpr is the Go expression for an empty Graph of node type node.
func (g *gen) newGraphExpr(node *ir.Type, nodeGo string) (string, error) {
	if !lKeyed(node) {
		g.helper("dmGraph", declGraph)
		return "dmNewGraph[" + nodeGo + "]()", nil
	}
	kf, err := g.keyFn(node)
	if err != nil {
		return "", err
	}
	g.helper("dmLGraph", declLGraph)
	return "dmNewLGraph[" + nodeGo + "](" + kf + ")", nil
}

// nodeIndex is the Go expression (a value and an ok) that finds node n in the
// graph held by the expression gr: the plain index map, or the list-keyed
// form's lookup.
func nodeIndex(node *ir.Type, gr, n string) string {
	if lKeyed(node) {
		return gr + ".lookup(" + n + ")"
	}
	return gr + ".index[" + n + "]"
}
