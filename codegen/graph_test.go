package codegen_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
)

// Interpreter/binary byte parity for the Graph<K> builtins.
//
// Two things here are two handwritten implementations of one specification and
// are where a divergence would actually appear: the rendering (ir.writeValue
// against codegen's fmtFunc) and the order-insensitive equality (ir.GraphEqual
// against dmGraphEq). Both get their own program.
func TestCompiledGraphBuiltinsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			// The readers, over a weighted graph built from triples.
			name: "readers over a weighted graph",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider g as graph(list(tuple("a", "b", 1), tuple("b", "c", 5), tuple("a", "c", 2)))
        in textjoin(list(
            totext(size(g)),
            totext(length(edges(g))),
            textjoin(nodes(g), "/"),
            textjoin(neighbors(g, "a"), "/"),
            textjoin(neighbors(g, "zzz"), "/"),
            totext(weight(g, "b", "c")),
            totext(weightor(g, "c", "a", 0 - 1)),
            if hasedge(g, "a", "b") then "Y" else "N",
            if hasedge(g, "b", "a") then "Y" else "N",
            totext(degree(g, "a")),
            totext(degree(g, "zzz")),
            if contains(g, "c") then "Y" else "N",
            if contains(g, "zzz") then "Y" else "N"
        ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// The functional updates: the receiver must be untouched, or the
			// two backends diverge the moment a lambda is applied twice.
			name: "updates are functional",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider g as addedge(addedge(emptygraph(""), "a", "b"), "a", "c", 2)
        in consider h as addnode(g, "z")
        in consider k as deledge(h, "a", "b")
        in textjoin(list(
            totext(size(g)), totext(size(h)), totext(size(k)),
            totext(length(edges(g))), totext(length(edges(k))),
            totext(weightor(g, "a", "b", 0 - 1)),
            totext(weightor(k, "a", "b", 0 - 1)),
            totext(length(edges(flipedges(g)))),
            totext(weightor(flipedges(g), "c", "a", 0 - 1)),
            totext(size(subgraph(h, list("a", "b")))),
            totext(length(edges(subgraph(h, list("a", "b")))))
        ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// The rendering, at a Reveal: two handwritten implementations of
			// one format. Isolated nodes, a non-1 weight and a self-loop.
			name: "graph rendering at a reveal",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> addnode(graph(list(tuple("a", "b", 1), tuple("a", "c", 7), tuple("b", "b", 3))), "q")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// Equality ignores insertion order, in both backends. This is the
			// single most likely place the two diverge.
			name: "equality ignores insertion order",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider x as graph(list(tuple("a", "b", 1), tuple("c", "d", 2)))
        in consider y as graph(list(tuple("c", "d", 2), tuple("a", "b", 1)))
        in consider z as graph(list(tuple("a", "b", 9), tuple("c", "d", 2)))
        in textjoin(list(
            if x = y then "same" else "differ",
            if x = z then "same" else "differ",
            if x = addnode(x, "extra") then "same" else "differ"
        ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// The ragged List<List<K>> edge list — the shape a positional
			// Match Pattern lands on, which is why the constructor takes it.
			name: "graph from a positional match pattern",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Cursed Technique: Apply
    Using: (rs) -> graph(rs)
Cursed Technique: Apply
    Using: (g) -> textjoin(list(
        totext(size(g)),
        totext(length(edges(g))),
        textjoin(nodes(g), "/"),
        totext(weightor(g, "a", "b", 0 - 1))
    ), "|")
Reveal: stdout
`,
			input: "a -> b\nb -> c\na -> c",
		},
		{
			// root and weightof: the two readers that ask about a node rather
			// than an arc. The graph is the parent -> child listing they are
			// for, weighted so weightof is not just degree in disguise.
			name: "root and node weight",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider g as graph(list(tuple("a", "b", 3), tuple("a", "c", 4), tuple("b", "d", 5)))
        in textjoin(list(
            root(g),
            totext(weightof(g, "a")),
            totext(weightof(g, "b")),
            totext(weightof(g, "d")),
            totext(weightof(g, "zzz")),
            totext(weightof(flipedges(g), "d")),
            root(addnode(emptygraph(""), "q")),
            totext(degree(g, "a"))
        ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// The node-level and whole-graph vocabulary, in one program: every
			// reader that walks the adjacency, and the three updates that
			// rebuild it. Each is a second handwritten implementation of one
			// specification, which is where a divergence would appear.
			name: "node-level and whole-graph readers",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider g as graph(list(tuple("a", "b", 3), tuple("a", "c", 4), tuple("b", "d", 5)))
        in consider cyc as graph(list(tuple("x", "y"), tuple("y", "x")))
        in textjoin(list(
            textjoin(roots(g), "/"),
            textjoin(leaves(g), "/"),
            textjoin(roots(cyc), "/"),
            totext(indegree(g, "d")),
            totext(indegree(g, "a")),
            totext(indegree(g, "zzz")),
            totext(weightsum(g)),
            if hascycle(g) then "Y" else "N",
            if hascycle(cyc) then "Y" else "N",
            textjoin(reachable(g, "a"), "/"),
            textjoin(reachable(g, "b"), "/"),
            totext(length(reachable(g, "zzz"))),
            textjoin(nodes(delnode(g, "b")), "/"),
            totext(length(edges(delnode(g, "b")))),
            totext(size(delnode(g, "zzz"))),
            totext(length(edges(undirected(g)))),
            totext(weightor(undirected(g), "b", "a", 0 - 1)),
            textjoin(nodes(mergegraphs(g, cyc)), "/"),
            totext(length(edges(mergegraphs(g, cyc)))),
            totext(weightor(mergegraphs(g, graph(list(tuple("a", "b", 99)))), "a", "b", 0 - 1))
        ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// The whole-graph updates at a Reveal: the rendering is where an
			// order divergence between the two backends would show.
			name: "undirected and merged graphs render alike",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider g as graph(list(tuple("a", "b", 3), tuple("b", "a", 5), tuple("b", "c", 7)))
        in mergegraphs(undirected(g), addnode(emptygraph(""), "q"))
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// Tuple nodes: a coordinate graph, which goes through the interned
			// tuple struct rather than a scalar key.
			name: "graph over tuple nodes",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider g as addedge(addedge(emptygraph(point(0, 0)), point(0, 0), point(1, 1), 4), point(1, 1), point(2, 2))
        in textjoin(list(
            totext(size(g)),
            totext(length(edges(g))),
            totext(prow(first(neighbors(g, point(0, 0))))),
            totext(weight(g, point(0, 0), point(1, 1))),
            totext(item(first(edgesof(g, point(0, 0))), 1)),
            totext(prow(root(g))),
            totext(weightof(g, point(0, 0))),
            totext(length(reachable(g, point(0, 0)))),
            totext(indegree(g, point(1, 1))),
            totext(size(delnode(g, point(1, 1))))
        ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// edgesof's tuple result, and a graph accumulated in a Fold — the
			// shape the dead-receiver rewrite is meant to reach.
			name: "graph accumulated in a fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Fold
    Seed: (xs) -> emptygraph("")
    Using: (acc, w) -> addedge(acc, slice(w, 0, 1), slice(w, 2, 3), toint(slice(w, 4, 5)))
Cursed Technique: Apply
    Using: (g) -> textjoin(list(
        totext(size(g)),
        totext(length(edges(g))),
        textjoin(nodes(g), "/"),
        totext(item(first(edgesof(g, "a")), 1)),
        item(first(edgesof(g, "a")), 0)
    ), "|")
Reveal: stdout
`,
			input: "a b 3\nb c 4\na c 5",
		},
		{
			// A graph threaded through Iterate Until Fixed Point: convergence
			// is exactly what the order-insensitive equality is for.
			name: "graph converging in a fixed point loop",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> graph(list(tuple("a", "b", 1)))
Simple Domain: Iterate Until Fixed Point
    Cursed Technique: Apply
        Using: (g) -> addedge(g, "a", "b", 1)
Cursed Technique: Apply
    Using: (g) -> totext(size(g)) + "|" + totext(length(edges(g)))
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// The coercions: an edge list in, triples out. Without these a
			// Graph is only reachable from inside an Apply.
			name: "convert to graph and back to edges",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Channeled Energy: Convert To Graph
Channeled Energy: Convert To Edges
Reveal: stdout
`,
			input: "a -> b\nb -> c\na -> c",
		},
		{
			// Mode: Undirected inserts both arcs rather than making a second
			// kind of value.
			name: "convert to graph undirected",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Channeled Energy: Convert To Graph
    Mode: Undirected
Reveal: stdout
`,
			input: "a -> b\nb -> c",
		},
		{
			// The weighted tuple form, and the adjacency-map form — the two
			// other shapes Convert To Graph accepts.
			name: "convert to graph from weighted tuples and from a map",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> list(tuple("a", "b", 4), tuple("b", "c", 6))
Channeled Energy: Convert To Graph
Cursed Technique: Apply
    Using: (g) -> textjoin(list(
        totext(size(g)), totext(weight(g, "a", "b")), totext(weight(g, "b", "c"))
    ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			name: "convert to graph from an adjacency map",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> tomap(list(tuple("a", list("b", "c")), tuple("b", list("c")), tuple("z", emptylist(""))))
Channeled Energy: Convert To Graph
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// Topological Sort's third input shape. All three go through the
			// same adjacency map, so this must agree with the edge-list form
			// exactly — the next case pins that.
			name: "topological sort over a graph",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: Topological Sort
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "a b\nb c\na c\nd a",
		},
		{
			// BFS counts hops...
			name: "bfs over a graph",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(item(p, 0), item(p, 1), toint(item(p, 2)))
Channeled Energy: Convert To Graph
Domain Expansion: BFS
    Start: "a"
Reveal: stdout
`,
			input: "a b 1\\nb d 10\\na c 2\\nc d 3\\ne f 1",
		},
		{
			// ...and Dijkstra pays the weights. Same input, and the answers
			// differ, or neither test proves anything.
			name: "dijkstra over a graph",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(item(p, 0), item(p, 1), toint(item(p, 2)))
Channeled Energy: Convert To Graph
Domain Expansion: Dijkstra
    Start: "a"
Reveal: stdout
`,
			input: "a b 1\\nb d 10\\na c 2\\nc d 3\\ne f 1",
		},
		{
			name: "connected components over a graph",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: Connected Components
Reveal: stdout
`,
			input: "a b\\nb c\\ne f\\ng g",
		},
		{
			// Shortest Path: the cheap route is the long one in hops.
			name: "shortest path over a weighted graph",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(item(p, 0), item(p, 1), toint(item(p, 2)))
Channeled Energy: Convert To Graph
    Mode: Undirected
Domain Expansion: Shortest Path
    Start: "a"
    Goal: "d"
Maximum Technique: Join with "->"
Reveal: stdout
`,
			input: "a b 1\\nb d 10\\na c 2\\nc d 3\\ne f 1",
		},
		{
			// An unreachable goal is the empty list, not an error.
			name: "shortest path to an unreachable node",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: Shortest Path
    Start: "a"
    Goal: "f"
Maximum Technique: Count
Reveal: stdout
`,
			input: "a b\\nb c\\ne f",
		},
		{
			// A measured Start:, so the node is computed from the value rather
			// than written as a literal — the lambda path through metaValue.
			name: "measured start node",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: BFS
    Start: (g) -> first(nodes(g))
Cursed Technique: Apply
    Using: (m) -> totext(size(m))
Reveal: stdout
`,
			input: "a b\nb c\nc d",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}

// The pipeline-level graph vocabulary, on the same terms: the interpreter and
// the binary have to print the same bytes. Three of these are handwritten
// twice — Kruskal's tie-breaking, Kosaraju's two walks, and the adjacency
// map's key order — which is exactly where an order divergence would hide.
func TestCompiledGraphPrimitivesMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
`
	const weighted = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} {word} {int}"
Channeled Energy: Convert To Graph
    Mode: Undirected
`
	const reveal = "Reveal: stdout\n"
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			name:  "root over a parsed listing",
			src:   head + "Domain Expansion: Root\n" + reveal,
			input: "b c\na b\na d",
		},
		{
			name: "minimum spanning tree",
			src: weighted + "Domain Expansion: Minimum Spanning Tree\n" +
				"Channeled Energy: Convert To Edges\n" + reveal,
			input: "a b 1\nb c 5\na c 3\nc d 2",
		},
		{
			// Equal weights are where the tie-breaking shows: two arcs at the
			// same cost have to be taken in the same order by both backends.
			name:  "minimum spanning forest with ties",
			src:   weighted + "Domain Expansion: Minimum Spanning Tree\n" + reveal,
			input: "a b 2\na c 2\nb c 2\nx y 2\nx z 9",
		},
		{
			name:  "strongly connected components",
			src:   head + "Domain Expansion: Strongly Connected Components\n" + reveal,
			input: "a b\nb c\nc a\nb d\nd e\ne d\nf f",
		},
		{
			name:  "strongly connected components over a dag",
			src:   head + "Domain Expansion: Strongly Connected Components\n" + reveal,
			input: "a b\nb c\na c",
		},
		{
			name:  "convert to adjacency",
			src:   head + "Channeled Energy: Convert To Adjacency\n" + reveal,
			input: "a b\nb c\na c",
		},
		{
			// The round trip back into a Graph, which is what the adjacency
			// shape is for.
			name: "adjacency round trip into a topological order",
			src: head + "Channeled Energy: Convert To Adjacency\n" +
				"Channeled Energy: Convert To Graph\nDomain Expansion: Topological Sort\n" + reveal,
			input: "a b\nb c\na c\nd a",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("interpreter and binary disagree:\n  interpreter: %q\n  binary:      %q", want, got)
				}
			})
		}
	}
}

// weight is the partial one in the group: a missing arc must fail in both
// backends, or the two disagree about whether the program runs at all.
func TestCompiledGraphWeightFailsInBothBackends(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	src := `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> totext(weight(graph(list(tuple("a", "b", 1))), "b", "a"))
Reveal: stdout
`
	pipe := compilePipeline(t, src, false)
	input := []byte("ignored")

	var out bytes.Buffer
	ctx := &ir.Context{Stdin: bytes.NewReader(input), Stdout: &out}
	if _, err := interp.Run(pipe, ctx); err == nil {
		t.Fatal("the interpreter should have failed on a missing edge")
	}
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		t.Fatalf("BuildBinary: %v\n%s", err, goSrc)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		t.Error("the binary exited 0 where the interpreter failed")
	}
}
