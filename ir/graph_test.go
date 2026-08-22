package ir

import "testing"

// g builds a graph from (from, to, weight) triples, for brevity below.
func g(edges ...[3]any) *GraphValue {
	gv := NewGraphValue()
	for _, e := range edges {
		gv.AddEdge(e[0], e[1], e[2].(int64))
	}
	return gv
}

func TestGraphInsertionOrder(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"c", "a", int64(2)})
	gv.AddNode("z")

	want := []string{"a", "b", "c", "z"}
	got := gv.Nodes()
	if len(got) != len(want) {
		t.Fatalf("nodes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("node %d = %v, want %s — an edge brings its endpoints in, in the order written", i, got[i], want[i])
		}
	}
	if gv.Len() != 4 || gv.EdgeCount() != 2 {
		t.Errorf("Len/EdgeCount = %d/%d, want 4/2", gv.Len(), gv.EdgeCount())
	}
}

// Adding a node that is already there must not reset its arcs — AddEdge calls
// AddNode on both endpoints, so this happens on every edge after the first.
func TestGraphAddNodeIsIdempotent(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)})
	gv.AddNode("a")
	if gv.Degree("a") != 1 {
		t.Errorf("re-adding a node dropped its arcs: degree = %d, want 1", gv.Degree("a"))
	}
	if gv.Len() != 2 {
		t.Errorf("re-adding a node duplicated it: %d nodes", gv.Len())
	}
}

// Last write wins on a repeated edge, and the arc keeps its position — so the
// rendering does not shuffle when a weight is updated.
func TestGraphRepeatedEdgeReweights(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(5)}, [3]any{"a", "b", int64(9)})
	if gv.EdgeCount() != 2 {
		t.Errorf("EdgeCount = %d, want 2 — a repeat re-weights rather than duplicating", gv.EdgeCount())
	}
	if w, _ := gv.Weight("a", "b"); w != 9 {
		t.Errorf("weight = %d, want 9 (last write wins)", w)
	}
	ns := gv.Neighbors("a")
	if len(ns) != 2 || ns[0] != "b" || ns[1] != "c" {
		t.Errorf("neighbours = %v, want [b c] — a re-weight must not move the arc", ns)
	}
}

func TestGraphSelfLoop(t *testing.T) {
	gv := g([3]any{"a", "a", int64(3)})
	if !gv.HasEdge("a", "a") {
		t.Error("self-loop missing")
	}
	if gv.Len() != 1 || gv.Degree("a") != 1 {
		t.Errorf("self-loop: %d nodes, degree %d, want 1/1", gv.Len(), gv.Degree("a"))
	}
}

// Directed: an arc one way is not an arc the other.
func TestGraphIsDirected(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)})
	if !gv.HasEdge("a", "b") {
		t.Error("a->b missing")
	}
	if gv.HasEdge("b", "a") {
		t.Error("a->b implied b->a; the graph is directed")
	}
}

// Absent nodes read as empty rather than erroring, which is what keeps
// neighbors/edgesof/degree total.
func TestGraphAbsentNodeReadsEmpty(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)})
	if ns := gv.Neighbors("nope"); len(ns) != 0 {
		t.Errorf("neighbours of an absent node = %v, want none", ns)
	}
	if gv.Degree("nope") != 0 {
		t.Error("degree of an absent node is not 0")
	}
	if _, ok := gv.Weight("nope", "b"); ok {
		t.Error("weight of an absent arc reported present")
	}
	if gv.Has("nope") {
		t.Error("Has reported an absent node")
	}
}

func TestGraphDelEdge(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(2)}, [3]any{"a", "d", int64(3)})
	gv.DelEdge("a", "c")

	if gv.HasEdge("a", "c") {
		t.Error("deleted arc still present")
	}
	if gv.EdgeCount() != 2 {
		t.Errorf("EdgeCount = %d, want 2", gv.EdgeCount())
	}
	// The nodes stay: an arc's removal must not drop a node other arcs may
	// still reach, and c is still a node of the graph.
	if !gv.Has("c") || gv.Len() != 4 {
		t.Errorf("deleting an arc dropped a node: %d nodes", gv.Len())
	}
	// The surviving arcs keep their order and their weights, which is the part
	// the index bookkeeping can get wrong.
	ns := gv.Neighbors("a")
	if len(ns) != 2 || ns[0] != "b" || ns[1] != "d" {
		t.Fatalf("neighbours after delete = %v, want [b d]", ns)
	}
	if w, _ := gv.Weight("a", "d"); w != 3 {
		t.Errorf("weight of the shifted arc = %d, want 3", w)
	}
	// Deleting something absent is a no-op, not a corruption.
	gv.DelEdge("a", "zzz")
	gv.DelEdge("zzz", "a")
	if gv.EdgeCount() != 2 {
		t.Errorf("deleting an absent arc changed the graph: EdgeCount = %d", gv.EdgeCount())
	}
}

func TestGraphCloneIsIndependent(t *testing.T) {
	orig := g([3]any{"a", "b", int64(1)})
	cp := orig.Clone()
	cp.AddEdge("a", "c", 2)
	cp.AddEdge("a", "b", 99)

	if orig.EdgeCount() != 1 {
		t.Errorf("the original gained an arc: EdgeCount = %d", orig.EdgeCount())
	}
	if w, _ := orig.Weight("a", "b"); w != 1 {
		t.Errorf("the original was re-weighted through its copy: %d", w)
	}
	if orig.Has("c") {
		t.Error("the original gained a node through its copy")
	}
}

func TestGraphFlipped(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"b", "c", int64(2)})
	f := gv.Flipped()

	if !f.HasEdge("b", "a") || !f.HasEdge("c", "b") {
		t.Error("arcs were not reversed")
	}
	if f.HasEdge("a", "b") {
		t.Error("the original direction survived")
	}
	if w, _ := f.Weight("b", "a"); w != 1 {
		t.Errorf("reversed arc lost its weight: %d", w)
	}
	// Node order is preserved, so the flipped graph renders predictably.
	if f.Len() != 3 || f.Nodes()[0] != "a" {
		t.Errorf("flipping reordered the nodes: %v", f.Nodes())
	}
}

func TestGraphSubgraph(t *testing.T) {
	gv := g(
		[3]any{"a", "b", int64(1)},
		[3]any{"b", "c", int64(2)},
		[3]any{"c", "a", int64(3)},
	)
	s := gv.Subgraph([]Value{"a", "b", "zzz"})

	if s.Len() != 2 {
		t.Errorf("%d nodes, want 2 — a node not in the graph is skipped, not invented", s.Len())
	}
	if !s.HasEdge("a", "b") {
		t.Error("an arc with both endpoints kept was dropped")
	}
	if s.HasEdge("b", "c") || s.Has("c") {
		t.Error("an arc leaving the kept set survived")
	}
}

// Equality ignores insertion order; rendering does not. The two answer
// different questions, which is the whole reason GraphEqual exists.
func TestGraphEqualIgnoresInsertionOrder(t *testing.T) {
	x := g([3]any{"a", "b", int64(1)}, [3]any{"c", "d", int64(2)})
	y := g([3]any{"c", "d", int64(2)}, [3]any{"a", "b", int64(1)})

	if !GraphEqual(x, y) {
		t.Error("graphs built in different orders compared unequal")
	}
	if !DeepEqual(x, y) {
		t.Error("DeepEqual disagreed with GraphEqual")
	}
	if FormatValue(x) == FormatValue(y) {
		t.Error("both orders rendered the same; the test cannot tell them apart")
	}
}

func TestGraphEqualDistinguishes(t *testing.T) {
	base := g([3]any{"a", "b", int64(1)})
	for _, c := range []struct {
		name  string
		other *GraphValue
	}{
		{"different weight", g([3]any{"a", "b", int64(2)})},
		{"different direction", g([3]any{"b", "a", int64(1)})},
		{"extra arc", g([3]any{"a", "b", int64(1)}, [3]any{"b", "a", int64(1)})},
		{"extra isolated node", func() *GraphValue {
			x := g([3]any{"a", "b", int64(1)})
			x.AddNode("c")
			return x
		}()},
		{"empty", NewGraphValue()},
	} {
		t.Run(c.name, func(t *testing.T) {
			if GraphEqual(base, c.other) {
				t.Errorf("%s compared equal to the base graph", c.name)
			}
		})
	}
}

func TestGraphRendering(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(2)}, [3]any{"b", "c", int64(1)})
	gv.AddNode("z")

	// Deliberately the shape a Map<K, List<(K, Int)>> renders as, and weights
	// are always shown — hiding them when they are all 1 would make the output
	// depend on the data.
	want := "{a: [(b, 1), (c, 2)], b: [(c, 1)], c: [], z: []}"
	if got := FormatValue(gv); got != want {
		t.Errorf("FormatValue =\n %s\nwant\n %s", got, want)
	}
	// The typed renderer must agree with the untyped one.
	if got := FormatValueTyped(gv, Graph(Text())); got != want {
		t.Errorf("FormatValueTyped =\n %s\nwant\n %s", got, want)
	}
	if got := FormatValue(NewGraphValue()); got != "{}" {
		t.Errorf("empty graph rendered %q, want {}", got)
	}
}

// The bounded writer must stop inside a graph like it does inside every other
// composite, rather than building the whole rendering and slicing it.
func TestGraphRenderingRespectsTheLimit(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(2)})
	full := FormatValue(gv)
	for n := 1; n < len(full); n++ {
		got, complete := FormatValueLimit(gv, n)
		if complete {
			t.Fatalf("limit %d reported complete for a %d-byte rendering", n, len(full))
		}
		if len(got) > n {
			t.Fatalf("limit %d produced %d bytes", n, len(got))
		}
		if full[:len(got)] != got {
			t.Fatalf("limit %d gave %q, which is not a prefix of %q", n, got, full)
		}
	}
}

func TestGraphDescribeValue(t *testing.T) {
	if got := DescribeValue(NewGraphValue()); got != "Graph" {
		t.Errorf("DescribeValue = %q, want Graph", got)
	}
}

// A graph is neither keyable nor ordered, so it can never reach a Map key, a
// Set element or a Sort. Both predicates are default-deny, which is what makes
// that true without a case of its own — pinned so a later edit cannot open it.
func TestGraphIsNeitherKeyableNorOrdered(t *testing.T) {
	gt := Graph(Text())
	if Keyable(gt) {
		t.Error("Graph is keyable")
	}
	if Ordered(gt) {
		t.Error("Graph is ordered")
	}
	if Keyable(Tuple(Int(), gt)) {
		t.Error("a tuple holding a Graph is keyable")
	}
	if Ordered(Tuple(Int(), gt)) {
		t.Error("a tuple holding a Graph is ordered")
	}
}

// Roots is the "who is the top of this" question, and it is asked of an edge
// list parsed out of text: a tree has one root, a forest several, a graph whose
// cycle swallows every node none at all.
func TestGraphRoots(t *testing.T) {
	names := func(vs []Value) []string {
		out := make([]string, len(vs))
		for i, v := range vs {
			out[i] = v.(string)
		}
		return out
	}
	equal := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	tree := g([3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(1)}, [3]any{"b", "d", int64(1)})
	if got := names(tree.Roots()); !equal(got, "a") {
		t.Errorf("tree roots = %v, want [a]", got)
	}

	// A forest keeps both roots, in insertion order.
	forest := g([3]any{"a", "b", int64(1)}, [3]any{"c", "d", int64(1)})
	if got := names(forest.Roots()); !equal(got, "a", "c") {
		t.Errorf("forest roots = %v, want [a c]", got)
	}

	// A cycle through every node leaves none.
	cycle := g([3]any{"a", "b", int64(1)}, [3]any{"b", "a", int64(1)})
	if got := cycle.Roots(); len(got) != 0 {
		t.Errorf("cycle roots = %v, want none", names(got))
	}

	// An isolated node is a root: nothing points at it.
	lone := NewGraphValue()
	lone.AddNode("q")
	if got := names(lone.Roots()); !equal(got, "q") {
		t.Errorf("isolated node roots = %v, want [q]", got)
	}

	// A self-loop is an incoming arc, so a node that only points at itself is
	// not a root — nothing else can reach it either.
	self := g([3]any{"a", "a", int64(1)})
	if got := self.Roots(); len(got) != 0 {
		t.Errorf("self-loop roots = %v, want none", names(got))
	}

	if got := NewGraphValue().Roots(); len(got) != 0 {
		t.Errorf("empty graph roots = %v, want none", names(got))
	}
}

// WeightOf is Degree with the weights counted: out-arcs only, and 0 for a node
// the graph does not have, because the readers are total.
func TestGraphWeightOf(t *testing.T) {
	gv := g([3]any{"a", "b", int64(3)}, [3]any{"a", "c", int64(4)}, [3]any{"b", "c", int64(5)})
	gv.AddNode("q")

	for _, c := range []struct {
		node string
		want int64
	}{
		{"a", 7},
		{"b", 5},
		{"c", 0}, // arcs come in, none go out
		{"q", 0}, // isolated
		{"zzz", 0},
	} {
		if got := gv.WeightOf(c.node); got != c.want {
			t.Errorf("WeightOf(%q) = %d, want %d", c.node, got, c.want)
		}
	}

	// The in-weight is the out-weight of the flipped graph, which is how the
	// rest of the vocabulary asks a question of the other direction.
	if got := gv.Flipped().WeightOf("c"); got != 9 {
		t.Errorf("in-weight of c = %d, want 9", got)
	}

	// A re-weighted arc is counted once, at its later weight.
	gv.AddEdge("a", "b", 10)
	if got := gv.WeightOf("a"); got != 14 {
		t.Errorf("WeightOf(a) after re-weighting = %d, want 14", got)
	}
}

// Leaves is Roots' mirror, and an isolated node is both.
func TestGraphLeaves(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(1)})
	gv.AddNode("q")
	got := gv.Leaves()
	if len(got) != 3 || got[0] != "b" || got[1] != "c" || got[2] != "q" {
		t.Errorf("Leaves = %v, want [b c q]", got)
	}
	if roots := gv.Roots(); len(roots) != 2 || roots[0] != "a" || roots[1] != "q" {
		t.Errorf("Roots = %v, want [a q] — an isolated node is both", roots)
	}
}

// InDegree counts the arcs coming in without building the reversed graph the
// vocabulary used to make people build.
func TestGraphInDegree(t *testing.T) {
	gv := g([3]any{"a", "c", int64(1)}, [3]any{"b", "c", int64(1)}, [3]any{"c", "d", int64(1)})
	for _, c := range []struct {
		node string
		want int
	}{{"c", 2}, {"a", 0}, {"d", 1}, {"zzz", 0}} {
		if got := gv.InDegree(c.node); got != c.want {
			t.Errorf("InDegree(%q) = %d, want %d", c.node, got, c.want)
		}
	}
	// It has to agree with the flipedges spelling it replaces.
	f := gv.Flipped()
	for _, n := range gv.Nodes() {
		if a, b := gv.InDegree(n), f.Degree(n); a != b {
			t.Errorf("InDegree(%v) = %d but Flipped().Degree = %d", n, a, b)
		}
	}
	// A self-loop is an arc in as well as out.
	self := g([3]any{"x", "x", int64(1)})
	if got := self.InDegree("x"); got != 1 {
		t.Errorf("InDegree of a self-loop = %d, want 1", got)
	}
}

// DelNode is DelEdge's counterpart: the node goes, and so does every arc that
// touched it in either direction.
func TestGraphDelNode(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"b", "c", int64(2)}, [3]any{"c", "a", int64(3)})
	out := gv.DelNode("b")

	if out.Has("b") {
		t.Error("the deleted node is still there")
	}
	if out.HasEdge("a", "b") || out.HasEdge("b", "c") {
		t.Error("an arc touching the deleted node survived")
	}
	if !out.HasEdge("c", "a") {
		t.Error("an arc between two surviving nodes was dropped")
	}
	if w, _ := out.Weight("c", "a"); w != 3 {
		t.Errorf("a surviving arc lost its weight: %d", w)
	}
	// The original is untouched, and the survivors keep their order.
	if gv.Len() != 3 || !gv.Has("b") {
		t.Error("DelNode mutated its receiver")
	}
	if ns := out.Nodes(); len(ns) != 2 || ns[0] != "a" || ns[1] != "c" {
		t.Errorf("surviving nodes = %v, want [a c]", ns)
	}
	// Deleting something absent is a copy, not a corruption.
	if same := gv.DelNode("zzz"); same.Len() != 3 || same.EdgeCount() != 3 {
		t.Error("deleting an absent node changed the graph")
	}
}

// Reachable is a breadth-first walk that includes its start.
func TestGraphReachable(t *testing.T) {
	gv := g(
		[3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(1)},
		[3]any{"b", "d", int64(1)}, [3]any{"e", "f", int64(1)},
	)
	got := gv.Reachable("a")
	if len(got) != 4 || got[0] != "a" || got[3] != "d" {
		t.Errorf("Reachable(a) = %v, want [a b c d] in breadth-first order", got)
	}
	if got := gv.Reachable("d"); len(got) != 1 || got[0] != "d" {
		t.Errorf("Reachable(d) = %v, want [d] — a node reaches itself", got)
	}
	// Arcs are directed, so a piece of the graph the start cannot walk to is
	// not reachable from it.
	if got := gv.Reachable("a"); len(got) != 4 {
		t.Errorf("Reachable(a) = %v, want not to include the e/f component", got)
	}
	if got := gv.Reachable("zzz"); len(got) != 0 {
		t.Errorf("Reachable of an absent node = %v, want none", got)
	}
	// A cycle terminates rather than walking forever.
	cyc := g([3]any{"x", "y", int64(1)}, [3]any{"y", "x", int64(1)})
	if got := cyc.Reachable("x"); len(got) != 2 {
		t.Errorf("Reachable over a cycle = %v, want both nodes once", got)
	}
}

func TestGraphHasCycle(t *testing.T) {
	for _, c := range []struct {
		name string
		gv   *GraphValue
		want bool
	}{
		{"a tree", g([3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(1)}), false},
		{"a diamond", g(
			[3]any{"a", "b", int64(1)}, [3]any{"a", "c", int64(1)},
			[3]any{"b", "d", int64(1)}, [3]any{"c", "d", int64(1)}), false},
		{"a cycle", g([3]any{"a", "b", int64(1)}, [3]any{"b", "a", int64(1)}), true},
		{"a self-loop", g([3]any{"a", "a", int64(1)}), true},
		{"a cycle off to one side", g(
			[3]any{"a", "b", int64(1)}, [3]any{"x", "y", int64(1)}, [3]any{"y", "x", int64(1)}), true},
		{"empty", NewGraphValue(), false},
	} {
		if got := c.gv.HasCycle(); got != c.want {
			t.Errorf("%s: HasCycle = %v, want %v", c.name, got, c.want)
		}
	}
}

// Undirected fills in the missing reverse of each arc and leaves an arc that
// is already there alone — including its weight.
func TestGraphUndirected(t *testing.T) {
	gv := g([3]any{"a", "b", int64(3)}, [3]any{"b", "a", int64(5)}, [3]any{"b", "c", int64(7)})
	out := gv.Undirected()

	if w, _ := out.Weight("a", "b"); w != 3 {
		t.Errorf("an existing arc was re-weighted: a->b = %d, want 3", w)
	}
	if w, _ := out.Weight("b", "a"); w != 5 {
		t.Errorf("the other existing direction was re-weighted: b->a = %d, want 5", w)
	}
	if w, ok := out.Weight("c", "b"); !ok || w != 7 {
		t.Errorf("the missing reverse was not added at its arc's weight: %d %v", w, ok)
	}
	if out.EdgeCount() != 4 {
		t.Errorf("EdgeCount = %d, want 4 — one reverse added, two already there", out.EdgeCount())
	}
	if gv.EdgeCount() != 3 {
		t.Error("Undirected mutated its receiver")
	}
	// Every arc now has its reverse, which is the property the name promises.
	for _, e := range out.Edges() {
		p := e.([]Value)
		if !out.HasEdge(p[1], p[0]) {
			t.Errorf("arc %v -> %v has no reverse", p[0], p[1])
		}
	}
}

// MergeGraphs is a union, with the second graph's weight winning a repeat —
// AddEdge's rule, since that is what writing b's arcs into a does.
func TestGraphMerge(t *testing.T) {
	a := g([3]any{"a", "b", int64(1)}, [3]any{"b", "c", int64(2)})
	b := g([3]any{"b", "c", int64(9)}, [3]any{"c", "d", int64(3)})
	out := MergeGraphs(a, b)

	if out.Len() != 4 {
		t.Errorf("%d nodes, want 4", out.Len())
	}
	if ns := out.Nodes(); ns[0] != "a" || ns[3] != "d" {
		t.Errorf("node order = %v, want a's order then b's new ones", ns)
	}
	if w, _ := out.Weight("b", "c"); w != 9 {
		t.Errorf("the shared arc = %d, want 9 — the second graph writes last", w)
	}
	if out.EdgeCount() != 3 {
		t.Errorf("EdgeCount = %d, want 3 — the shared arc is one arc", out.EdgeCount())
	}
	if a.EdgeCount() != 2 || b.EdgeCount() != 2 {
		t.Error("MergeGraphs mutated one of its arguments")
	}
	// An isolated node in either graph survives the merge.
	lone := NewGraphValue()
	lone.AddNode("q")
	if !MergeGraphs(a, lone).Has("q") {
		t.Error("an isolated node was lost in the merge")
	}
}

func TestGraphWeightSum(t *testing.T) {
	gv := g([3]any{"a", "b", int64(3)}, [3]any{"a", "c", int64(4)}, [3]any{"b", "c", int64(5)})
	if got := gv.WeightSum(); got != 12 {
		t.Errorf("WeightSum = %d, want 12", got)
	}
	// A re-weighted arc counts once, at its later weight.
	gv.AddEdge("a", "b", 10)
	if got := gv.WeightSum(); got != 19 {
		t.Errorf("WeightSum after re-weighting = %d, want 19", got)
	}
	if got := NewGraphValue().WeightSum(); got != 0 {
		t.Errorf("WeightSum of an empty graph = %d, want 0", got)
	}
}

// A graph's short form and its size are what a trace, a --json export and the
// visualizer's columns show. Without a case of its own a Graph fell to
// FormatShort's default and rendered as Go's view of the struct — `&{[a b c]
// map[a:0 b:1…` — which is the representation leaking, not the value.
func TestGraphShortFormAndSize(t *testing.T) {
	gv := g([3]any{"a", "b", int64(1)}, [3]any{"b", "c", int64(2)})
	gv.AddNode("q")

	if got := FormatShort(gv); got != "Graph 4 nodes (2 arcs)" {
		t.Errorf("FormatShort = %q, want %q", got, "Graph 4 nodes (2 arcs)")
	}
	// The node count, which is what size(g) answers in the language.
	if n, ok := SizeOf(gv); !ok || n != 4 {
		t.Errorf("SizeOf = %d, %v; want 4, true", n, ok)
	}
	if got := FormatShort(NewGraphValue()); got != "Graph 0 nodes (0 arcs)" {
		t.Errorf("empty graph FormatShort = %q", got)
	}
	// The long form is still the adjacency listing: the short one is a summary,
	// not a replacement.
	if got := FormatValue(gv); got != "{a: [(b, 1)], b: [(c, 2)], c: [], q: []}" {
		t.Errorf("FormatValue = %q", got)
	}
}
