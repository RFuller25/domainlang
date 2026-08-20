package prims

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// This file implements the Channel mechanism (M4): named sub-pipelines that
// branch from the current value, and From:-consumers that recombine them. This
// is the only place the otherwise-linear IR forms a small dataflow graph.

// resolveChannel lowers a `Channel "name":` statement into a passthrough node
// whose sub-pipeline runs on the current value and stores the result under the
// channel name. The current value is unchanged, so sibling channels all branch
// from the same upstream value.
func (r *resolver) resolveChannel(stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	name := stmt.ChannelName
	if name == "" {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "Channel requires a name"}
	}
	if _, exists := r.channels[name]; exists {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("channel %q is already defined", name)}
	}
	if cur == nil {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("channel %q has no upstream value to branch from", name)}
	}
	if len(stmt.Block) == 0 {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("channel %q has an empty body", name), NeedsBlock: true}
	}

	// Globals are out of reach inside a channel body, in both directions: see
	// resolver.sealedFrom. Saved and restored rather than cleared, so a
	// construct that ever nests inside a channel does not silently unseal.
	wasInChannel := r.inChannel
	r.inChannel = true
	subNodes, subType, err := r.resolveSequence(stmt.Block, cur, scopeChannel)
	r.inChannel = wasInChannel
	if err != nil {
		return nil, err
	}
	r.channels[name] = subType

	node := &ir.Node{
		Prim:    "Channel",
		In:      cur,
		Out:     cur, // passthrough
		Display: fmt.Sprintf("Channel %q", name),
		Meta:    map[string]any{"name": name, "nodes": subNodes},
		Pos:     stmt.Pos,
	}
	node.Eval = func(ctx *ir.Context, in ir.Value) (ir.Value, error) {
		// The result is reported to the tracer on the way out, so a failed
		// body closes its frame as unfinished rather than leaving it open.
		var body ir.Value
		ctx.PushFrame(fmt.Sprintf("Channel %q", name), subType)
		defer func() { ctx.PopFrame(body) }()

		// Read from Meta rather than closing over subNodes directly: a
		// length-changing optimizer rewrite (e.g. fuseUnfoldStream) replaces
		// Meta["nodes"] with a shorter fused slice, and only a read at Eval
		// time picks that up — a captured local would keep running the
		// pre-optimization body forever.
		nodes, _ := node.Meta["nodes"].([]*ir.Node)
		v, err := runBody(ctx, nodes, in)
		if err != nil {
			return nil, err
		}
		body = v
		ctx.SetChannel(name, v)
		return in, nil
	}
	return node, nil
}

// resolveConsumer lowers a From:-consumer (Combine or Difference).
func (r *resolver) resolveConsumer(stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	var rewriteErr error
	node, err := r.consumerNode(stmt, cur, &rewriteErr)
	// A lambda the binding scope could not rewrite is the more specific
	// failure; see resolveOne.
	if rewriteErr != nil {
		return nil, rewriteErr
	}
	return node, err
}

func (r *resolver) consumerNode(stmt *ast.Statement, cur *ir.Type, rewriteErr *error) (*ir.Node, error) {
	args := r.args(stmt, rewriteErr)
	froms, _ := args.Idents("From")
	if len(froms) == 0 {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "From: must name at least one channel"}
	}
	types := make([]*ir.Type, len(froms))
	for i, name := range froms {
		t, ok := r.channels[name]
		if !ok {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("unknown channel %q in From:", name)}
		}
		types[i] = t
	}

	switch {
	case hasWord(stmt.Op, "Combine"):
		return buildCombine(args, froms, types, cur, stmt.Pos)
	case hasWord(stmt.Op, "Difference"):
		return buildDifference(froms, types, cur, stmt.Pos)
	case hasWord(stmt.Op, "Fold"):
		return buildFoldOver(args, froms, types, cur, stmt.Pos)
	case hasWord(stmt.Op, "Zip") && args.Has("Using"):
		return buildZipWith(args, froms, types, cur, stmt.Pos)
	case hasWord(stmt.Op, "Zip"):
		return buildZip(froms, types, cur, stmt.Pos)
	default:
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: "From: is only supported by Combine, Difference, Fold, and Zip"}
	}
}

// buildZip pairs two channel lists element-wise into List<(A, B)>, truncated
// to the shorter list. The main pipeline's current value is ignored, like
// Combine.
func buildZip(froms []string, types []*ir.Type, cur *ir.Type, pos token.Position) (*ir.Node, error) {
	if len(froms) != 2 {
		return nil, &ResolveError{Pos: pos, Msg: "Zip needs exactly two channels (From: a, b)"}
	}
	for i, t := range types {
		if t == nil || t.Kind != ir.KList {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Zip channel %q must hold a List, got %s", froms[i], t)}
		}
	}
	out := ir.List(ir.Tuple(types[0].Elem, types[1].Elem))
	a, b := froms[0], froms[1]
	return &ir.Node{
		Prim:    "Zip",
		In:      cur,
		Out:     out,
		Display: fmt.Sprintf("Zip From: %s, %s", a, b),
		Meta:    map[string]any{"from": []string{a, b}},
		Pos:     pos,
		Eval: func(ctx *ir.Context, _ ir.Value) (ir.Value, error) {
			av, ok := ctx.Channel(a)
			if !ok {
				return nil, runtimeErr("Zip", pos, "channel %q has no value", a)
			}
			bv, ok := ctx.Channel(b)
			if !ok {
				return nil, runtimeErr("Zip", pos, "channel %q has no value", b)
			}
			as, err := ir.AsList(av)
			if err != nil {
				return nil, runtimeErr("Zip", pos, "channel %q: %v", a, err)
			}
			bs, err := ir.AsList(bv)
			if err != nil {
				return nil, runtimeErr("Zip", pos, "channel %q: %v", b, err)
			}
			n := min(len(as), len(bs))
			zipped := make([]ir.Value, n)
			for i := range n {
				zipped[i] = []ir.Value{as[i], bs[i]}
			}
			return zipped, nil
		},
	}, nil
}

// buildZipWith is Zip with a Using: lambda: it combines the two channels
// element-wise directly instead of handing back tuples for a following Map
// Each to take apart. One pass, and no intermediate tuple list — the
// optimizer performs the same rewrite on a naive `Zip` + `Map Each` pair
// (optimizer.fuseZipWith).
func buildZipWith(args ArgSet, froms []string, types []*ir.Type, cur *ir.Type, pos token.Position) (*ir.Node, error) {
	if len(froms) != 2 {
		return nil, &ResolveError{Pos: pos, Msg: "Zip needs exactly two channels (From: a, b)"}
	}
	for i, t := range types {
		if t == nil || t.Kind != ir.KList {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Zip channel %q must hold a List, got %s", froms[i], t)}
		}
	}
	lam, _ := args.Lambda("Using")
	if len(lam.Params) != 2 {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Zip Using: lambda must take 2 parameters (one per channel), got %d", len(lam.Params))}
	}
	elems := []*ir.Type{types[0].Elem, types[1].Elem}
	outElem, err := typecheck.LambdaType(lam, elems...)
	if err != nil {
		return nil, &ResolveError{Pos: pos, Msg: "Zip With: " + err.Error()}
	}
	a, b := froms[0], froms[1]
	return &ir.Node{
		Prim:    "Zip With",
		In:      cur,
		Out:     ir.List(outElem),
		Display: fmt.Sprintf("Zip With From: %s, %s", a, b),
		Meta:    map[string]any{"from": []string{a, b}, "lambda": lam},
		Pos:     pos,
		Eval: func(ctx *ir.Context, _ ir.Value) (ir.Value, error) {
			as, bs, err := zipChannelLists(ctx, "Zip With", a, b, pos)
			if err != nil {
				return nil, err
			}
			n := min(len(as), len(bs)) // truncated to the shorter list, like Zip
			out := make([]ir.Value, n)
			for i := range n {
				r, err := eval.EvalLambdaTyped(lam, elems, as[i], bs[i])
				if err != nil {
					return nil, runtimeErr("Zip With", pos, "element %d: %v", i, err)
				}
				out[i] = r
			}
			return out, nil
		},
	}, nil
}

// zipChannelLists reads two channels and requires both to hold lists.
func zipChannelLists(ctx *ir.Context, prim, a, b string, pos token.Position) ([]ir.Value, []ir.Value, error) {
	get := func(name string) ([]ir.Value, error) {
		v, ok := ctx.Channel(name)
		if !ok {
			return nil, runtimeErr(prim, pos, "channel %q has no value", name)
		}
		xs, err := ir.AsList(v)
		if err != nil {
			return nil, runtimeErr(prim, pos, "channel %q: %v", name, err)
		}
		return xs, nil
	}
	as, err := get(a)
	if err != nil {
		return nil, nil, err
	}
	bs, err := get(b)
	if err != nil {
		return nil, nil, err
	}
	return as, bs, nil
}

// buildFoldOver lowers `Fold` with a `From:` channel: fold over the channel's
// list with the *current pipeline value* as the seed. This is how a state
// value built upstream (e.g. crate stacks) threads through a list that lives
// in a channel (e.g. parsed moves) — the missing piece of the full Day 5
// simulation.
func buildFoldOver(args ArgSet, froms []string, types []*ir.Type, cur *ir.Type, pos token.Position) (*ir.Node, error) {
	if len(froms) != 1 {
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("Fold From: takes exactly one channel, got %d", len(froms))}
	}
	if cur == nil {
		return nil, &ResolveError{Pos: pos, Msg: "Fold From: has no current value to seed with"}
	}
	over := types[0]
	if over == nil || over.Kind != ir.KList {
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("Fold From: channel %q must hold a List, got %s", froms[0], over)}
	}
	lam, ok := args.Lambda("Using")
	if !ok {
		return nil, &ResolveError{Pos: pos, Msg: "Fold requires a Using: lambda", NeedsBlock: true}
	}
	wantArity := 2 + ambientDepth()
	if len(lam.Params) != wantArity {
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("Fold lambda must take %d parameters (acc, item, ...), got %d", wantArity, len(lam.Params))}
	}
	bodyType, err := typecheck.LambdaType(lam, append([]*ir.Type{cur, over.Elem}, ambientTypes()...)...)
	if err != nil {
		return nil, &ResolveError{Pos: pos, Msg: "Fold: " + err.Error()}
	}
	if !bodyType.Equal(cur) {
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("Fold lambda must return the seed type %s, got %s", cur, bodyType)}
	}
	name := froms[0]
	return &ir.Node{
		Prim:    "FoldOver",
		In:      cur,
		Out:     cur,
		Display: "Fold From: " + name,
		Meta:    map[string]any{"from": []string{name}, "lambda": lam},
		Pos:     pos,
		Eval: func(ctx *ir.Context, in ir.Value) (ir.Value, error) {
			cv, ok := ctx.Channel(name)
			if !ok {
				return nil, runtimeErr("FoldOver", pos, "channel %q has no value", name)
			}
			xs, err := ir.AsList(cv)
			if err != nil {
				return nil, runtimeErr("FoldOver", pos, "channel %q: %v", name, err)
			}
			// FoldOver's seed *is* the current pipeline value, which a Part
			// or a sibling Channel may also be holding.
			acc := ownAccumulator(lam, in)
			for i, x := range xs {
				acc, err = eval.EvalLambdaTyped(lam, append([]*ir.Type{cur, over.Elem}, ambientTypes()...), append([]ir.Value{acc, x}, ambientArgs()...)...)
				if err != nil {
					return nil, runtimeErr("FoldOver", pos, "item %d: %v", i, err)
				}
			}
			return acc, nil
		},
	}, nil
}

// buildCombine binds channel values to a Using: lambda's parameters (in From:
// order) and emits the lambda's result.
func buildCombine(args ArgSet, froms []string, types []*ir.Type, cur *ir.Type, pos token.Position) (*ir.Node, error) {
	lam, ok := args.Lambda("Using")
	if !ok {
		return nil, &ResolveError{Pos: pos, Msg: "Combine requires a Using: lambda", NeedsBlock: true}
	}
	wantArity := len(froms) + ambientDepth()
	if len(lam.Params) != wantArity {
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("Combine lambda takes %d parameter(s) but From: names %d channel(s) (plus %d ambient)",
				len(lam.Params), len(froms), ambientDepth())}
	}
	outType, err := typecheck.LambdaType(lam, append(types, ambientTypes()...)...)
	if err != nil {
		return nil, &ResolveError{Pos: pos, Msg: "Combine: " + err.Error()}
	}
	return &ir.Node{
		Prim:    "Combine",
		In:      cur,
		Out:     outType,
		Display: "Combine From: " + strings.Join(froms, ", "),
		Meta:    map[string]any{"from": froms, "lambda": lam},
		Pos:     pos,
		Eval: func(ctx *ir.Context, _ ir.Value) (ir.Value, error) {
			vals := make([]ir.Value, len(froms))
			for i, n := range froms {
				v, ok := ctx.Channel(n)
				if !ok {
					return nil, runtimeErr("Combine", pos, "channel %q was not computed", n)
				}
				vals[i] = v
			}
			r, err := eval.EvalLambdaTyped(lam, append(types, ambientTypes()...), append(vals, ambientArgs()...)...)
			if err != nil {
				return nil, runtimeErr("Combine", pos, "%v", err)
			}
			return r, nil
		},
	}, nil
}

// buildDifference emits the set difference of two channels (a - b). This is the
// home for the binary set op deferred from M2.
func buildDifference(froms []string, types []*ir.Type, cur *ir.Type, pos token.Position) (*ir.Node, error) {
	if len(froms) != 2 {
		return nil, &ResolveError{Pos: pos, Msg: "Difference needs exactly two channels (From: a, b)"}
	}
	elemA, err := setOrListElem(types[0], pos)
	if err != nil {
		return nil, err
	}
	elemB, err := setOrListElem(types[1], pos)
	if err != nil {
		return nil, err
	}
	if !elemA.Equal(elemB) {
		return nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("Difference needs matching element types, got %s and %s", elemA, elemB)}
	}
	if err := requireKeyable(elemA, "Difference", pos); err != nil {
		return nil, err
	}
	a, b := froms[0], froms[1]
	return &ir.Node{
		Prim:    "Difference",
		In:      cur,
		Out:     ir.Set(elemA),
		Display: fmt.Sprintf("Difference From: %s, %s", a, b),
		Meta:    map[string]any{"from": []string{a, b}},
		Pos:     pos,
		Eval: func(ctx *ir.Context, _ ir.Value) (ir.Value, error) {
			sa, err := channelAsSet(ctx, a, pos)
			if err != nil {
				return nil, err
			}
			sb, err := channelAsSet(ctx, b, pos)
			if err != nil {
				return nil, err
			}
			return ir.SetDifference(sa, sb), nil
		},
	}, nil
}

func setOrListElem(t *ir.Type, pos token.Position) (*ir.Type, error) {
	if t != nil && (t.Kind == ir.KSet || t.Kind == ir.KList) {
		return t.Elem, nil
	}
	return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Difference channels must be Set or List, got %s", t)}
}

func channelAsSet(ctx *ir.Context, name string, pos token.Position) (*ir.SetValue, error) {
	v, ok := ctx.Channel(name)
	if !ok {
		return nil, runtimeErr("Difference", pos, "channel %q was not computed", name)
	}
	switch x := v.(type) {
	case *ir.SetValue:
		return x, nil
	case []ir.Value:
		return ir.SetFromList(x), nil
	default:
		return nil, runtimeErr("Difference", pos, "channel %q is not a Set or List (%s)", name, ir.DescribeValue(v))
	}
}

// ---------------------------------------------------------------------------
// Cursed Technique: Take Item N — List<T> -> T (used to pick input sections).
// ---------------------------------------------------------------------------

var takeItem = &Primitive{
	ID:      "Take Item",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Take") && hasWord(op, "Item") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Take Item", pos)
		if err != nil {
			return nil, err
		}
		idxM, err := requireMeasuredInt(op, args, "Take Item", "Index", 0, NoBound, in, pos,
			"an index", "Take Item 0")
		if err != nil {
			return nil, err
		}
		// A literal index keeps its `int` shape in Meta: two optimizer passes
		// match on it (`Filter` + `Take Item 0`, `Sort` + `Take Item k`), and
		// neither can carry a measured one — hasMeasuredArg stands them down.
		meta := map[string]any{}
		if idxM.IsMeasured() {
			idxM.Meta(meta, "index")
		} else {
			meta["index"] = int(idxM.Lit)
		}
		return &ir.Node{
			Prim:    "Take Item",
			In:      in,
			Out:     elem,
			Display: "Take Item " + idxM.Describe(),
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Take Item", pos, "%v", err)
				}
				n, err := idxM.Resolve(v)
				if err != nil {
					return nil, err
				}
				idx := int(n)
				if idx < 0 || idx >= len(items) {
					return nil, runtimeErr("Take Item", pos, "index %d out of range (length %d)", idx, len(items))
				}
				return items[idx], nil
			},
		}, nil
	},
}
