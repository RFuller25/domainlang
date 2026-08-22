package eval

import (
	"testing"

	"domain/ir"
)

// `root` is the one graph reader that is partial, so its three failures are
// the part worth pinning: each one is a fact about the data a caller has to
// hear, and each is worded to say which fact it is.
func TestGraphRootBuiltin(t *testing.T) {
	tree := `graph(list(tuple("a", "b"), tuple("a", "c"), tuple("b", "d")))`
	if got := mustEval(t, "(s) -> root("+tree+")", ""); got != "a" {
		t.Errorf("root of a tree = %v, want a", got)
	}
	// An isolated node counts: nothing points at it.
	if got := mustEval(t, `(s) -> root(addnode(emptygraph(""), "q"))`, ""); got != "q" {
		t.Errorf("root of a one-node graph = %v, want q", got)
	}
	// The root is a node, not a name, so a tuple-node graph answers a tuple.
	pts := "graph(list(tuple(point(0, 0), point(1, 1))))"
	if got := mustEval(t, "(s) -> prow(root("+pts+"))", ""); got != int64(0) {
		t.Errorf("root of a coordinate graph = %v, want row 0", got)
	}

	wantErr(t, `(s) -> root(emptygraph(""))`, "the graph is empty", "")
	wantErr(t, `(s) -> root(graph(list(tuple("a", "b"), tuple("b", "a"))))`,
		"every node has an incoming arc", "")
	wantErr(t, `(s) -> root(graph(list(tuple("a", "b"), tuple("c", "d"))))`,
		"2 nodes have no incoming arc (a, c)", "")
	// A self-loop is an incoming arc, so the node it loops on is not a root.
	wantErr(t, `(s) -> root(graph(list(tuple("a", "a"))))`, "no root", "")
}

// `weightof` is `degree` with the weights counted, and total the same way.
func TestGraphWeightOfBuiltin(t *testing.T) {
	g := `graph(list(tuple("a", "b", 3), tuple("a", "c", 4), tuple("b", "c", 5)))`
	for _, c := range []struct {
		src  string
		want int64
	}{
		{`weightof(` + g + `, "a")`, 7},
		{`weightof(` + g + `, "b")`, 5},
		{`weightof(` + g + `, "c")`, 0},   // arcs come in, none go out
		{`weightof(` + g + `, "zzz")`, 0}, // absent, like degree
		{`weightof(flipedges(` + g + `), "c")`, 9},
		// An unweighted graph's arcs weigh 1, so weightof degenerates to
		// degree — which is what makes the default weight worth having.
		{`weightof(graph(list(tuple("a", "b"), tuple("a", "c"))), "a")`, 2},
	} {
		if got := mustEval(t, "(s) -> "+c.src, ""); got != c.want {
			t.Errorf("%s = %v, want %d", c.src, got, c.want)
		}
	}
}

// The node-level readers over the interpreter. `roots` is the total twin the
// vocabulary was missing: where `root` errors, this is what it errors about.
func TestGraphRootsAndLeavesBuiltins(t *testing.T) {
	forest := `graph(list(tuple("a", "b"), tuple("c", "d")))`
	if got := mustEval(t, `(s) -> textjoin(roots(`+forest+`), "/")`, ""); got != "a/c" {
		t.Errorf("roots of a forest = %v, want a/c", got)
	}
	if got := mustEval(t, `(s) -> textjoin(leaves(`+forest+`), "/")`, ""); got != "b/d" {
		t.Errorf("leaves of a forest = %v, want b/d", got)
	}
	// Where root errors, roots is what it errors about — and it is empty for
	// the cycle rather than failing.
	cyc := `graph(list(tuple("x", "y"), tuple("y", "x")))`
	if got := mustEval(t, `(s) -> length(roots(`+cyc+`))`, ""); got != int64(0) {
		t.Errorf("roots of a cycle = %v, want none", got)
	}
	if got := mustEval(t, `(s) -> length(roots(emptygraph("")))`, ""); got != int64(0) {
		t.Errorf("roots of an empty graph = %v, want none", got)
	}
}

func TestGraphNodeLevelBuiltins(t *testing.T) {
	g := `graph(list(tuple("a", "b", 3), tuple("a", "c", 4), tuple("b", "d", 5)))`
	for _, c := range []struct {
		src  string
		want ir.Value
	}{
		{`indegree(` + g + `, "d")`, int64(1)},
		{`indegree(` + g + `, "a")`, int64(0)},
		{`indegree(` + g + `, "zzz")`, int64(0)},
		// indegree agrees with the flipedges spelling it replaces.
		{`indegree(` + g + `, "b") = degree(flipedges(` + g + `), "b")`, true},
		{`weightsum(` + g + `)`, int64(12)},
		{`hascycle(` + g + `)`, false},
		{`hascycle(graph(list(tuple("x", "y"), tuple("y", "x"))))`, true},
		{`hascycle(graph(list(tuple("x", "x"))))`, true},
		// reachable includes its start and stops at the arcs' direction.
		{`textjoin(reachable(` + g + `, "a"), "/")`, "a/b/c/d"},
		{`textjoin(reachable(` + g + `, "b"), "/")`, "b/d"},
		{`length(reachable(` + g + `, "zzz"))`, int64(0)},
		// delnode takes the arcs that touched it, in both directions.
		{`textjoin(nodes(delnode(` + g + `, "b")), "/")`, "a/c/d"},
		{`length(edges(delnode(` + g + `, "b")))`, int64(1)},
		{`size(delnode(` + g + `, "zzz"))`, int64(4)},
		// undirected adds the missing reverses at their arc's weight.
		{`length(edges(undirected(` + g + `)))`, int64(6)},
		{`weight(undirected(` + g + `), "b", "a")`, int64(3)},
		{`hasedge(undirected(` + g + `), "d", "b")`, true},
		// mergegraphs unions, and the second graph's weight wins a repeat.
		{`size(mergegraphs(` + g + `, graph(list(tuple("d", "e")))))`, int64(5)},
		{`weight(mergegraphs(` + g + `, graph(list(tuple("a", "b", 99)))), "a", "b")`, int64(99)},
	} {
		if got := mustEval(t, "(s) -> "+c.src, ""); got != c.want {
			t.Errorf("%s = %v, want %v", c.src, got, c.want)
		}
	}
}

// The updates are functional here too: applying a lambda twice to the same
// graph must not let the second application see the first one's work, which is
// exactly what constant folding does.
func TestGraphNodeUpdatesAreFunctional(t *testing.T) {
	base := ir.NewGraphValue()
	base.AddEdge("a", "b", 1)
	base.AddEdge("b", "c", 2)

	for _, src := range []string{
		`(g) -> size(delnode(g, "b"))`,
		`(g) -> length(edges(undirected(g)))`,
		`(g) -> length(edges(mergegraphs(g, g)))`,
	} {
		first := mustEval(t, src, base)
		if second := mustEval(t, src, base); first != second {
			t.Errorf("%s: %v then %v — the update was not functional", src, first, second)
		}
	}
	if base.Len() != 3 || base.EdgeCount() != 2 {
		t.Errorf("the receiver was mutated: %d nodes, %d arcs", base.Len(), base.EdgeCount())
	}
}
