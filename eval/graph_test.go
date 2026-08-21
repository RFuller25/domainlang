package eval

import "testing"

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
