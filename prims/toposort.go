// Domain Expansion: Topological Sort — Map<K, List<K>> -> List<K>.
//
// The graph half of the search vocabulary. BFS/Dijkstra/Flood Fill/Connected
// Components all take a Grid, and Explore answers reachability over an
// implicit state graph; this answers the other standard question about an
// explicit one — "in what order may these be done?" — which AoC asks whenever
// an input is a list of dependencies.
package prims

import (
	"fmt"
	"slices"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

var topologicalSort = &Primitive{
	ID:      "Topological Sort",
	Keyword: "Domain Expansion",
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Topological") || (hasWord(op, "Topo") && hasWord(op, "Sort"))
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		// Two input shapes, like Merge Ranges. An adjacency Map is the
		// classic form; an **edge list** is the one a parse actually lands on,
		// since `Match Pattern "{word} -> {word}" Mode: Each` produces exactly
		// List<List<Text>> with two entries per row.
		node, edges, err := topoInputShape(in, pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim: "Topological Sort", In: in, Out: ir.List(node),
			Display: "Topological Sort", Swappable: true,
			Meta: map[string]any{"edges": edges, "node": node}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				if edges {
					return topoSortEdges(v, pos)
				}
				return topoSort(v, pos)
			},
		}, nil
	},
}

// topoInputShape validates the input and reports the node type plus whether
// the input is an edge list rather than an adjacency map.
func topoInputShape(in *ir.Type, pos token.Position) (*ir.Type, bool, error) {
	bad := func() (*ir.Type, bool, error) {
		return nil, false, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Topological Sort expects Map<K, List<K>> (a node mapped to its successors) "+
				"or a list of edges — List<(K, K)> or two-element List<List<K>>, the shape "+
				"a positional Match Pattern produces — got %s", in)}
	}
	if in == nil {
		return bad()
	}
	switch in.Kind {
	case ir.KMap:
		if in.Elem == nil || in.Elem.Kind != ir.KList || !in.Elem.Elem.Equal(in.Key) {
			return bad()
		}
		if !ir.Keyable(in.Key) {
			return bad()
		}
		return in.Key, false, nil
	case ir.KList:
		e := in.Elem
		if e == nil {
			return bad()
		}
		if e.Kind == ir.KTuple && len(e.Elems) == 2 && e.Elems[0].Equal(e.Elems[1]) &&
			ir.Keyable(e.Elems[0]) {
			return e.Elems[0], true, nil
		}
		if e.Kind == ir.KList && ir.Keyable(e.Elem) {
			// Row length is checked at runtime: the type says List<List<K>>
			// but not how long each row is.
			return e.Elem, true, nil
		}
	}
	return bad()
}

// topoSortEdges runs the same algorithm over a list of (from, to) pairs,
// which is the shape a parsed dependency file lands on.
func topoSortEdges(v ir.Value, pos token.Position) (ir.Value, error) {
	rows, err := ir.AsList(v)
	if err != nil {
		return nil, runtimeErr("Topological Sort", pos, "%v", err)
	}
	m := ir.NewMapValue()
	for i, row := range rows {
		pair, err := ir.AsList(row)
		if err != nil || len(pair) != 2 {
			return nil, runtimeErr("Topological Sort", pos,
				"edge %d is not a (from, to) pair", i)
		}
		prev, _ := m.Get(pair[0])
		var succ []ir.Value
		if prev != nil {
			succ, _ = ir.AsList(prev)
		}
		m.Put(pair[0], append(slices.Clone(succ), pair[1]))
		// The target must exist as a node even with no outgoing edges.
		if !m.Has(pair[1]) {
			m.Put(pair[1], []ir.Value{})
		}
	}
	return topoSort(m, pos)
}

// topoSort is Kahn's algorithm. Ties are broken by first-seen order — keys in
// the map's insertion order, then successors in their list order — so the
// result is deterministic rather than merely valid. Two runs of the same
// program must print the same answer, and the compiled backend must agree
// with the interpreter, which a set-based ready queue would not guarantee.
func topoSort(v ir.Value, pos token.Position) (ir.Value, error) {
	m, ok := v.(*ir.MapValue)
	if !ok {
		return nil, runtimeErr("Topological Sort", pos, "expected a Map, got %s", ir.DescribeValue(v))
	}

	// Nodes in first-seen order: every key, then any node that appears only as
	// a successor (a leaf has no outgoing edges, so it is never a key).
	var order []ir.Value
	index := map[any]int{}
	add := func(n ir.Value) int {
		k := ir.KeyOf(n)
		if i, seen := index[k]; seen {
			return i
		}
		index[k] = len(order)
		order = append(order, n)
		return len(order) - 1
	}
	for _, k := range m.Keys() {
		add(k)
	}
	type edge struct{ from, to int }
	var edges []edge
	for _, k := range m.Keys() {
		from := add(k)
		succ, _ := m.Get(k)
		xs, err := ir.AsList(succ)
		if err != nil {
			return nil, runtimeErr("Topological Sort", pos, "successors of %s: %v", ir.FormatValue(k), err)
		}
		for _, s := range xs {
			edges = append(edges, edge{from, add(s)})
		}
	}

	indeg := make([]int, len(order))
	adj := make([][]int, len(order))
	for _, e := range edges {
		adj[e.from] = append(adj[e.from], e.to)
		indeg[e.to]++
	}

	// A slice used as a queue, scanned in first-seen order, keeps the result
	// deterministic.
	ready := make([]int, 0, len(order))
	for i := range order {
		if indeg[i] == 0 {
			ready = append(ready, i)
		}
	}
	out := make([]ir.Value, 0, len(order))
	for head := 0; head < len(ready); head++ {
		i := ready[head]
		out = append(out, order[i])
		for _, j := range adj[i] {
			if indeg[j]--; indeg[j] == 0 {
				ready = append(ready, j)
			}
		}
	}
	if len(out) != len(order) {
		// Name a node still in the cycle: "there is a cycle" alone leaves the
		// user to find it by hand, and the input is usually large.
		for i, d := range indeg {
			if d > 0 {
				return nil, runtimeErr("Topological Sort", pos,
					"the graph has a cycle (%s is still blocked after %d of %d nodes were ordered)",
					ir.FormatValue(order[i]), len(out), len(order))
			}
		}
	}
	return out, nil
}
