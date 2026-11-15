package prims

// Node-prim naming helpers and the pipeline walk shared by the resolver's
// callers: which catalog entry a resolved node documents, and every node a
// pipeline holds, nested ones included.

import (
	"domain/ast"
	"domain/ir"
)

// A resolved node's Prim is usually its Primitive.ID, but not always: three
// primitives build a node under a different name, and a handful of language
// *statements* build nodes that are not registry primitives at all.
//
// Both facts matter here and nowhere else. A coverage report divides what a
// folder used by what the catalog holds, so a primitive whose node is named
// differently would be reported as never exercised however often it ran —
// `Fold` was, before this map existed.
var nodePrimAlias = map[string]string{
	"FoldOver":       "Fold",
	"SelectTopK":     "Select Top K",
	"WindowedReduce": "Sliding Reduce",
}

// StructuralPrims are node Prims for statements rather than registry
// primitives: the loop kinds, Channel and its consumers, Part, and Consider.
// They are real, and worth counting, but they are not in the catalog and so
// must not be measured against it — a report that divided by 85 while counting
// them would exceed its own denominator.
var StructuralPrims = map[string]bool{
	"Channel":                     true,
	"Zip":                         true,
	"Combine":                     true,
	"Part":                        true,
	"Consider":                    true,
	"Simple Domain (Repeat)":      true,
	"Simple Domain (While)":       true,
	"Simple Domain (For)":         true,
	"Simple Domain (For Each)":    true,
	"Simple Domain (Fixed Point)": true,
}

// CatalogID maps a resolved node's Prim to the catalog entry documenting it.
// A structural node, or one the optimizer invented (PartialSelect, Stream),
// maps to itself and simply is not in the catalog.
func CatalogID(nodePrim string) string {
	if id, ok := nodePrimAlias[nodePrim]; ok {
		return id
	}
	return nodePrim
}

// WalkNodes calls fn for every node in a pipeline, including the ones inside
// sub-pipelines: Channel bodies, loop bodies, Part bodies, Consider bindings,
// and the resolved pipeline behind a block-form Using:.
//
// It is exported because three different questions need the same walk —
// what vocabulary a program uses, which nodes ran, and which nodes exist to be
// rewritten — and a walk that missed a nesting form would answer all three
// wrongly in the same invisible way. The optimizer keeps its own copy
// (nodeLists) only because it cannot import this package: prims' own tests
// import the optimizer.
func WalkNodes(p *ir.Pipeline, fn func(*ir.Node)) {
	if p == nil {
		return
	}
	// Node lists are collected breadth-first into one growing worklist.
	lists := [][]*ir.Node{p.Nodes}
	for i := 0; i < len(lists); i++ {
		for _, n := range lists[i] {
			if n == nil {
				continue
			}
			fn(n)
			for _, v := range n.Meta {
				switch m := v.(type) {
				case []*ir.Node:
					if m != nil {
						lists = append(lists, m)
					}
				case [][]*ir.Node:
					for _, sub := range m {
						if sub != nil {
							lists = append(lists, sub)
						}
					}
				case *ast.Lambda:
					// A typed nil satisfies the assertion: primitives with
					// optional lambdas store one whether or not it was written.
					if m == nil {
						continue
					}
					if sub := lambdaBlockNodes(m); sub != nil {
						lists = append(lists, sub)
					}
				case []ir.Binding:
					for _, b := range m {
						if b == nil {
							continue
						}
						if lam, ok := b.Lambda().(*ast.Lambda); ok && lam != nil {
							if sub := lambdaBlockNodes(lam); sub != nil {
								lists = append(lists, sub)
							}
						}
						if sub := b.BlockNodes(); sub != nil {
							lists = append(lists, sub)
						}
					}
				}
			}
		}
	}
}

// lambdaBlockNodes is the resolved sub-pipeline behind a Using: written as an
// indented body rather than an expression. It hangs off the lambda rather than
// off Meta["nodes"], so it has to be looked for separately.
func lambdaBlockNodes(lam *ast.Lambda) []*ir.Node {
	bb, ok := lam.Body.(*ast.BlockBody)
	if !ok {
		return nil
	}
	return bb.Pipe.BlockNodes()
}
