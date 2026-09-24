// Cursed Technique: Map Elements / Map Keys / Map Nodes / Map Weights — the
// shape-preserving half of Map Each.
//
// `Map Each` takes a List, a Set or a Map and always hands back a **List**.
// That is right for what it is — a mapping that may change the element type
// and imposes an order — but it means the only way to map a Set and still have
// a Set is `Map Each` followed by `Convert To Set`, and the only way to map a
// Map's keys is to take it apart into entries and build it again. Two
// primitives already close that gap for two of the types: `Map Cells` keeps a
// Grid (and a Sparse) and `Map Values` keeps a Map. These are the rest of the
// row.
//
// Each of them answers "change every X, keep the shape":
//
//	Map Elements   Set<T>    x (T -> U)          -> Set<U>
//	Map Keys       Map<K,V>  x (K -> J)          -> Map<J,V>
//	Map Nodes      Graph<K>  x (K -> J)          -> Graph<J>
//	Map Weights    Graph<K>  x ((K, K, Int) -> Int) -> Graph<K>
//
// The last two are the graph's answer to the pair Map Keys and Map Values are
// for a Map: one changes what the things are called, the other changes what
// the arcs between them weigh.
//
// **A mapping that is not injective merges.** Two set elements that map to one
// value are one element; two keys that map to one key keep the last one
// written, which is `insert`'s rule; two nodes that map to one node become one
// node holding both their arcs. That is what these types mean, and a primitive
// that refused it would be refusing an ordinary use — folding case, rounding a
// coordinate, interning a long name — rather than catching a mistake. Each one
// says so where the result can be smaller than the input.
package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
)

// oneArgLambda reads the Using: lambda of a one-parameter shape-preserving map
// and reports what it returns, wording the error with the primitive's name.
func oneArgLambda(args ArgSet, prim string, param *ir.Type, pos token.Position) (*ast.Lambda, *ir.Type, error) {
	lam, err := requireLambda(args, 1, prim, pos)
	if err != nil {
		return nil, nil, err
	}
	out, err := lambdaType(lam, param)
	if err != nil {
		return nil, nil, &ResolveError{Pos: pos, Msg: prim + ": " + err.Error()}
	}
	return lam, out, nil
}

// applyOne runs a one-parameter mapping lambda over one value.
func applyOne(lam *ast.Lambda, param *ir.Type, v ir.Value, prim string, pos token.Position) (ir.Value, error) {
	r, err := evalLambda(lam, []*ir.Type{param}, v)
	if err != nil {
		return nil, runtimeErr(prim, pos, "%v", err)
	}
	return r, nil
}

var mapElements = &Primitive{
	ID:      "Map Elements",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Map") && hasWord(op, "Elements") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KSet {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Map Elements expects a Set, got %s (Map Each maps any collection into a List)", in)}
		}
		lam, out, err := oneArgLambda(args, "Map Elements", in.Elem, pos)
		if err != nil {
			return nil, err
		}
		if !ir.Keyable(out) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Map Elements: a Set's elements must be keyable (Int, Text, or a tuple/record of them), got %s", out)}
		}
		return &ir.Node{
			Prim: "Map Elements", In: in, Out: ir.Set(out),
			Display: "Map Elements", Meta: map[string]any{"lambda": lam, "elem": in.Elem}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				s, ok := v.(*ir.SetValue)
				if !ok {
					return nil, runtimeErr("Map Elements", pos, "expected a Set, got %s", ir.DescribeValue(v))
				}
				res := ir.NewSetValue()
				for _, e := range s.Elems() {
					nv, err := applyOne(lam, in.Elem, e, "Map Elements", pos)
					if err != nil {
						return nil, err
					}
					res.Add(nv) // two elements mapping to one value are one element
				}
				return res, nil
			},
		}, nil
	},
}

var mapKeys = &Primitive{
	ID:      "Map Keys",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Map") && hasWord(op, "Keys") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KMap {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Map Keys expects a Map, got %s", in)}
		}
		lam, out, err := oneArgLambda(args, "Map Keys", in.Key, pos)
		if err != nil {
			return nil, err
		}
		if !ir.Keyable(out) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Map Keys: a Map's keys must be keyable (Int, Text, or a tuple/record of them), got %s", out)}
		}
		return &ir.Node{
			Prim: "Map Keys", In: in, Out: ir.Map(out, in.Elem),
			Display: "Map Keys", Meta: map[string]any{"lambda": lam, "key": in.Key}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				m, ok := v.(*ir.MapValue)
				if !ok {
					return nil, runtimeErr("Map Keys", pos, "expected a Map, got %s", ir.DescribeValue(v))
				}
				res := ir.NewMapSized(m.Len())
				for _, k := range m.Keys() {
					nk, err := applyOne(lam, in.Key, k, "Map Keys", pos)
					if err != nil {
						return nil, err
					}
					val, _ := m.Get(k)
					res.Put(nk, val) // two keys mapping to one: the later write wins
				}
				return res, nil
			},
		}, nil
	},
}

var mapNodes = &Primitive{
	ID:      "Map Nodes",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Map") && hasWord(op, "Nodes") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGraph {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Map Nodes expects a Graph, got %s", in)}
		}
		lam, out, err := oneArgLambda(args, "Map Nodes", in.Elem, pos)
		if err != nil {
			return nil, err
		}
		if !ir.Keyable(out) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Map Nodes: a Graph's nodes must be keyable (Int, Text, or a tuple/record of them), got %s", out)}
		}
		return &ir.Node{
			Prim: "Map Nodes", In: in, Out: ir.Graph(out),
			Display: "Map Nodes", Meta: map[string]any{"lambda": lam, "node": in.Elem}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GraphValue)
				if !ok {
					return nil, runtimeErr("Map Nodes", pos, "expected a Graph, got %s", ir.DescribeValue(v))
				}
				// Every node is renamed first, so an isolated one survives and
				// an arc's endpoints agree with the node list.
				renamed := make([]ir.Value, g.Len())
				res := ir.NewGraphSized(g.Len())
				for i := range g.Len() {
					nv, err := applyOne(lam, in.Elem, g.NodeAt(i), "Map Nodes", pos)
					if err != nil {
						return nil, err
					}
					renamed[i] = nv
					res.AddNode(nv)
				}
				for i := range g.Len() {
					for _, e := range g.AdjOf(i) {
						res.AddEdge(renamed[i], renamed[e.To], e.W)
					}
				}
				return res, nil
			},
		}, nil
	},
}

var mapWeights = &Primitive{
	ID:      "Map Weights",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Map") && hasWord(op, "Weights") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KGraph {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Map Weights expects a Graph, got %s", in)}
		}
		// Three parameters, because an arc's new weight is almost never a
		// function of its old one alone: it is the destination that carries
		// the cost in a node-weighted graph, and the pair that carries it in a
		// distance one.
		lam, err := requireLambda(args, 3, "Map Weights", pos)
		if err != nil {
			return nil, err
		}
		params := []*ir.Type{in.Elem, in.Elem, ir.Int()}
		out, err := lambdaType(lam, params...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Map Weights: " + err.Error()}
		}
		if !out.Equal(ir.Int()) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Map Weights: Using: must return Int — an arc's weight — got %s", out)}
		}
		return &ir.Node{
			Prim: "Map Weights", In: in, Out: in,
			Display: "Map Weights", Meta: map[string]any{"lambda": lam, "node": in.Elem}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				g, ok := v.(*ir.GraphValue)
				if !ok {
					return nil, runtimeErr("Map Weights", pos, "expected a Graph, got %s", ir.DescribeValue(v))
				}
				res := ir.NewGraphSized(g.Len())
				for i := range g.Len() {
					res.AddNode(g.NodeAt(i))
				}
				for i := range g.Len() {
					for _, e := range g.AdjOf(i) {
						from, to := g.NodeAt(i), g.NodeAt(e.To)
						r, err := eval.EvalLambdaTyped(lam, append(params, ambientTypes()...),
							append([]ir.Value{from, to, e.W}, ambientArgs()...)...)
						if err != nil {
							return nil, runtimeErr("Map Weights", pos, "%v", err)
						}
						w, ok := r.(int64)
						if !ok {
							return nil, runtimeErr("Map Weights", pos, "Using: did not return an Int")
						}
						res.AddEdge(from, to, w)
					}
				}
				return res, nil
			},
		}, nil
	},
}
