package prims

// What vocabulary a program actually uses: which primitives it runs, which
// expression-layer builtins it calls, and which themed keywords it speaks
// under.
//
// This is the shared measurement behind `domain expansion: coverage` (which of
// the catalog has this folder never exercised?), `stats` (what does a year of
// solutions draw on?) and `golf` (is this stage still reachable at all?).
//
// Two properties decide whether the numbers mean anything, and both are
// choices rather than accidents:
//
// **It must be given the unoptimized pipeline.** fuseMapMap turns two Map Each
// nodes into one and elideRedundantSort deletes a Sort outright, so counting
// after optimization would report that a program which visibly uses Sort does
// not. Callers resolve, call Used, and only then optimize — or optimize a
// separate copy.
//
// **Builtins are counted statically, primitives structurally.** A builtin is
// not a node: `gcd` is evaluated inside eval.Eval and never reaches the trace
// hook, so the only place to see it is the expression tree. That means a
// builtin inside a branch that never runs still counts as used. Callers that
// care about the difference (coverage --dynamic) trace primitives separately
// and say in their header which half is which.

import (
	"domain/ast"
	"domain/ir"
	"domain/typecheck"
)

// Usage is the primitive and builtin vocabulary one program exercises. Each
// map holds occurrence counts, not just membership: "how many times" is what
// separates a program that reaches for Fold once from one built on it.
type Usage struct {
	Prims    map[string]int // Primitive.ID → nodes carrying it
	Builtins map[string]int // expression-layer function name → call sites
	Keywords map[string]int // themed keyword → nodes resolved under it
}

func newUsage() *Usage {
	return &Usage{
		Prims:    map[string]int{},
		Builtins: map[string]int{},
		Keywords: map[string]int{},
	}
}

// Merge folds other into u, summing counts. Used to total a folder.
func (u *Usage) Merge(other *Usage) {
	if other == nil {
		return
	}
	for k, v := range other.Prims {
		u.Prims[k] += v
	}
	for k, v := range other.Builtins {
		u.Builtins[k] += v
	}
	for k, v := range other.Keywords {
		u.Keywords[k] += v
	}
}

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

// builtinSet is the expression-layer table as a set, so an ordinary local
// name or a Shikigami call is not mistaken for a builtin.
var builtinSet = func() map[string]bool {
	m := make(map[string]bool, len(typecheck.Builtins))
	for _, b := range typecheck.Builtins {
		m[b] = true
	}
	return m
}()

// AllPrims is every primitive ID the registry knows, for the "out of how
// many" half of a coverage report.
func AllPrims() []string {
	out := make([]string, 0, len(Registry))
	for _, p := range Registry {
		out = append(out, p.ID)
	}
	return out
}

// AllBuiltins is every expression-layer function name.
func AllBuiltins() []string {
	return append([]string(nil), typecheck.Builtins...)
}

// AllKeywords is every themed keyword that heads a statement class.
func AllKeywords() []string {
	out := make([]string, 0, len(keywordPages))
	for k := range keywordPages {
		out = append(out, k)
	}
	return out
}

// Used walks a resolved pipeline and reports the vocabulary it draws on.
//
// The walk covers every node list, not just the top level: Channel bodies,
// loop bodies, Consider bindings' sub-pipelines and the resolved pipeline
// behind a block-form Using: are all places a primitive hides, and a coverage
// report that missed them would credit a program with less than it wrote.
func Used(p *ir.Pipeline) *Usage {
	u := newUsage()
	seenLambda := map[*ast.Lambda]bool{}
	WalkNodes(p, func(n *ir.Node) {
		id := CatalogID(n.Prim)
		u.Prims[id]++
		if doc, ok := Catalog[id]; ok && doc.Keyword != "" {
			u.Keywords[doc.Keyword]++
		}
		// The expression layer is walked here rather than in WalkNodes,
		// because it is this function's question: a builtin is not a node, so
		// a node walk cannot see one.
		for _, v := range n.Meta {
			switch m := v.(type) {
			case *ast.Lambda:
				if m == nil || seenLambda[m] {
					continue
				}
				seenLambda[m] = true
				u.walkExpr(m.Body)
			case ast.Expr:
				u.walkExpr(m)
			case []ir.Binding:
				for _, b := range m {
					if b == nil {
						continue
					}
					if lam, ok := b.Lambda().(*ast.Lambda); ok && lam != nil && !seenLambda[lam] {
						seenLambda[lam] = true
						u.walkExpr(lam.Body)
					}
					if e, ok := b.Expr().(ast.Expr); ok && e != nil {
						u.walkExpr(e)
					}
				}
			}
		}
	})
	return u
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

// walkExpr counts every builtin call in one expression tree.
//
// Only a call whose callee is a plain identifier in the builtin table counts.
// A Shikigami call and a local bound to a lambda both parse as CallExpr too,
// and neither is a builtin — crediting them would inflate the coverage number
// with names the catalog has never heard of.
func (u *Usage) walkExpr(e ast.Expr) {
	switch x := e.(type) {
	case nil:
		return
	case *ast.CallExpr:
		if id, ok := x.Fn.(*ast.Ident); ok && builtinSet[id.Name] {
			u.Builtins[id.Name]++
		} else {
			u.walkExpr(x.Fn)
		}
		for _, a := range x.Args {
			u.walkExpr(a)
		}
	case *ast.BinaryExpr:
		u.walkExpr(x.Left)
		u.walkExpr(x.Right)
	case *ast.UnaryExpr:
		u.walkExpr(x.X)
	case *ast.FieldAccess:
		u.walkExpr(x.Target)
	case *ast.CondExpr:
		u.walkExpr(x.Cond)
		u.walkExpr(x.Then)
		u.walkExpr(x.Else)
	case *ast.LetExpr:
		u.walkExpr(x.Value)
		u.walkExpr(x.Body)
	case *ast.AssignExpr:
		u.walkExpr(x.Value)
	case *ast.AlsoExpr:
		u.walkExpr(x.Body)
		for _, c := range x.Clauses {
			u.walkExpr(c)
		}
	}
}
