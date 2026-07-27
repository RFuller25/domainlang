// Map pipeline operations. Group By and Count By are two of the most
// reachable primitives in the language and both produce a Map — but a Map
// could only be rendered, never reshaped, so the single most common AoC
// follow-up ("which key occurred most?") had no spelling at all.
//
// Convert To Entries is the important one: it drops a Map back into the list
// vocabulary, where Sort By and Select Top K already live. Count By ->
// Convert To Entries -> Sort By Descending -> Select Top 1 is the whole
// idiom, and the existing quickselect fusion fires on it for free.
package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// ---------------------------------------------------------------------------
// Channeled Energy: Convert To Entries — Map<K,V> -> List<(K, V)>.
// ---------------------------------------------------------------------------

var convertToEntries = &Primitive{
	ID:      "Convert To Entries",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Entries") && !hasWord(op, "Filter") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KMap {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Convert To Entries expects a Map, got %s", in)}
		}
		out := ir.List(ir.Tuple(in.Key, in.Elem))
		return &ir.Node{
			Prim: "Convert To Entries", In: in, Out: out,
			Display: "Convert To Entries", Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				m, ok := v.(*ir.MapValue)
				if !ok {
					return nil, runtimeErr("Convert To Entries", pos, "expected a Map, got %s", ir.DescribeValue(v))
				}
				// Insertion order, the order a Map already renders in, so the
				// conversion never silently reshuffles.
				entries := make([]ir.Value, 0, m.Len())
				for _, k := range m.Keys() {
					val, _ := m.Get(k)
					entries = append(entries, []ir.Value{k, val})
				}
				return entries, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Channeled Energy: Convert To Map — List<(K, V)> -> Map<K, V>.
// ---------------------------------------------------------------------------

var convertToMap = &Primitive{
	ID:      "Convert To Map",
	Keyword: "Channeled Energy",
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Convert") && hasWord(op, "Map") && !hasWord(op, "Each")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KList || in.Elem == nil || in.Elem.Kind != ir.KTuple ||
			len(in.Elem.Elems) != 2 {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Convert To Map expects List<(K, V)> — the shape Convert To Entries produces — got %s", in)}
		}
		key, val := in.Elem.Elems[0], in.Elem.Elems[1]
		if !ir.Keyable(key) {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Convert To Map needs a keyable key type, got %s", key)}
		}
		return &ir.Node{
			Prim: "Convert To Map", In: in, Out: ir.Map(key, val),
			Display: "Convert To Map", Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Convert To Map", pos, "%v", err)
				}
				m := ir.NewMapValue()
				for i, e := range xs {
					pair, err := ir.AsList(e)
					if err != nil || len(pair) != 2 {
						return nil, runtimeErr("Convert To Map", pos, "entry %d is not a (key, value) pair", i)
					}
					// Last write wins, the same rule a repeated literal key
					// would follow in any language with map literals.
					m.Put(pair[0], pair[1])
				}
				return m, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Map Values — Map<K,V> x (V -> W) -> Map<K,W>.
// ---------------------------------------------------------------------------

var mapValues = &Primitive{
	ID:      "Map Values",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Map") && hasWord(op, "Values") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KMap {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Map Values expects a Map, got %s", in)}
		}
		lam, err := requireLambda(args, 1, "Map Values", pos)
		if err != nil {
			return nil, err
		}
		outElem, err := typecheck.LambdaType(lam, append([]*ir.Type{in.Elem}, ambientTypes()...)...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Map Values: " + err.Error()}
		}
		return &ir.Node{
			Prim: "Map Values", In: in, Out: ir.Map(in.Key, outElem),
			Display: "Map Values", Meta: map[string]any{"lambda": lam}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				m, ok := v.(*ir.MapValue)
				if !ok {
					return nil, runtimeErr("Map Values", pos, "expected a Map, got %s", ir.DescribeValue(v))
				}
				out := ir.NewMapValue()
				for _, k := range m.Keys() {
					val, _ := m.Get(k)
					nv, err := eval.EvalLambdaTyped(lam,
						append([]*ir.Type{in.Elem}, ambientTypes()...),
						append([]ir.Value{val}, ambientArgs()...)...)
					if err != nil {
						return nil, runtimeErr("Map Values", pos, "%v", err)
					}
					out.Put(k, nv)
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Filter Entries — Map<K,V> x ((K, V) -> Bool) -> Map<K,V>.
// ---------------------------------------------------------------------------

var filterEntries = &Primitive{
	ID:      "Filter Entries",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Filter") && hasWord(op, "Entries") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KMap {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Filter Entries expects a Map, got %s", in)}
		}
		lam, err := requireLambda(args, 2, "Filter Entries", pos)
		if err != nil {
			return nil, err
		}
		// Two parameters, key then value — the shape that reads naturally, and
		// the one a Map's own rendering suggests.
		bt, err := typecheck.LambdaType(lam, append([]*ir.Type{in.Key, in.Elem}, ambientTypes()...)...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Filter Entries: " + err.Error()}
		}
		if !bt.Equal(ir.Bool()) {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Filter Entries predicate must return Bool, got %s", bt)}
		}
		return &ir.Node{
			Prim: "Filter Entries", In: in, Out: in,
			Display: "Filter Entries", Meta: map[string]any{"lambda": lam}, Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				m, ok := v.(*ir.MapValue)
				if !ok {
					return nil, runtimeErr("Filter Entries", pos, "expected a Map, got %s", ir.DescribeValue(v))
				}
				out := ir.NewMapValue()
				for _, k := range m.Keys() {
					val, _ := m.Get(k)
					keep, err := eval.EvalLambdaTyped(lam,
						append([]*ir.Type{in.Key, in.Elem}, ambientTypes()...),
						append([]ir.Value{k, val}, ambientArgs()...)...)
					if err != nil {
						return nil, runtimeErr("Filter Entries", pos, "%v", err)
					}
					b, ok := keep.(bool)
					if !ok {
						return nil, runtimeErr("Filter Entries", pos, "predicate did not return a Bool")
					}
					if b {
						out.Put(k, val)
					}
				}
				return out, nil
			},
		}, nil
	},
}
