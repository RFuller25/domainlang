package optimizer

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Windows read by position.
//
// `Window size [step]` copies `size` elements into a fresh list for every
// window, so a program that only ever asks a window for its first and last
// element — the shape of "compare each digit with the one halfway round" —
// pays O(n·size) time and allocation for what is a handful of reads per start
// index. fuseWindowPositions replaces the Window with WindowPositions, which
// walks the same start indexes (same size and step, measured or literal, same
// bound checks, same order) and builds, for each, a small tuple holding just
// the elements the next stage's lambda reads. The consumer then runs unchanged
// over those tuples, with its reads rewritten to tuple projections — so which
// element a failure is reported against, what an Any/All stops on, and every
// word of every error stay the consumer's own.

// endsOnly rewrites a body that reads w only as first(w), last(w), item(w, 0)
// or item(w, 1) into one that reads it only as item(w, 0) / item(w, 1), and
// reports false when w is used any other way (its length, a sum, passed whole
// to anything) — or is shadowed, which this does not try to follow.
func endsOnly(e ast.Expr, w string) (ast.Expr, bool) {
	return rewriteWindowReads(e, w, func(r windowRead, pos token.Position) (ast.Expr, bool) {
		end := int64(1)
		if !r.last {
			if r.off != 0 && r.off != 1 {
				return nil, false
			}
			end = r.off
		}
		return &ast.CallExpr{
			Fn:   &ast.Ident{Name: "item", Pos: pos},
			Args: []ast.Expr{&ast.Ident{Name: w, Pos: pos}, &ast.IntLit{Value: end, Pos: pos}},
			Pos:  pos,
		}, true
	})
}

// windowRead is one positional read of a window: first(w) and item(w, 0) are
// offset 0, item(w, c) is offset c, and last(w) is the final element, whose
// offset depends on the window's size.
type windowRead struct {
	last bool
	off  int64
}

// rewriteWindowReads rebuilds e with every positional read of the window w —
// first(w), last(w), item(w, c) for a literal c >= 0 — replaced by what read
// returns, and reports false when w is used any other way (its length, a sum,
// passed whole to anything), is shadowed, or when read declines a read. The
// walk only understands pure expression forms: an update, an `also` or a body
// is left alone and makes the whole rewrite decline.
func rewriteWindowReads(e ast.Expr, w string, read func(windowRead, token.Position) (ast.Expr, bool)) (ast.Expr, bool) {
	isW := func(x ast.Expr) bool {
		id, ok := x.(*ast.Ident)
		return ok && id.Name == w
	}
	ok := true
	var walk func(e ast.Expr) ast.Expr
	walk = func(e ast.Expr) ast.Expr {
		switch x := e.(type) {
		case *ast.Ident:
			if x.Name == w {
				ok = false // the window itself, not one end of it
			}
			return x
		case *ast.IntLit, *ast.StringLit, *ast.BoolLit, *ast.GlobalRef:
			return x
		case *ast.UnaryExpr:
			return &ast.UnaryExpr{Op: x.Op, X: walk(x.X), Pos: x.Pos}
		case *ast.BinaryExpr:
			return &ast.BinaryExpr{Op: x.Op, Left: walk(x.Left), Right: walk(x.Right), Pos: x.Pos}
		case *ast.FieldAccess:
			return &ast.FieldAccess{Target: walk(x.Target), Field: x.Field, Pos: x.Pos}
		case *ast.CondExpr:
			return &ast.CondExpr{Cond: walk(x.Cond), Then: walk(x.Then), Else: walk(x.Else), Pos: x.Pos}
		case *ast.LetExpr:
			if x.Name == w {
				ok = false
				return x
			}
			return &ast.LetExpr{Name: x.Name, Value: walk(x.Value), Body: walk(x.Body), Pos: x.Pos}
		case *ast.CallExpr:
			name := builtinName(x)
			var r windowRead
			isRead := false
			switch {
			case name == "first" && len(x.Args) == 1 && isW(x.Args[0]):
				r, isRead = windowRead{off: 0}, true
			case name == "last" && len(x.Args) == 1 && isW(x.Args[0]):
				r, isRead = windowRead{last: true}, true
			case name == "item" && len(x.Args) == 2 && isW(x.Args[0]):
				if lit, isLit := x.Args[1].(*ast.IntLit); isLit && lit.Value >= 0 {
					r, isRead = windowRead{off: lit.Value}, true
				}
			}
			if isRead {
				if repl, accepted := read(r, x.Pos); accepted {
					return repl
				}
			}
			if _, isIdent := x.Fn.(*ast.Ident); !isIdent {
				ok = false
				return x
			}
			// Anything else — including a read the caller declined — walks its
			// arguments, which reaches the bare `w` and declines the rewrite.
			args := make([]ast.Expr, len(x.Args))
			for i, arg := range x.Args {
				args[i] = walk(arg)
			}
			return &ast.CallExpr{Fn: x.Fn, Args: args, Pos: x.Pos, InPlace: x.InPlace}
		default:
			// An update, an `also`, a body: nothing this rule should rebuild.
			ok = false
			return e
		}
	}
	out := walk(e)
	return out, ok
}

// windowConsumers are the stages that may follow a Window being read by
// position: each one's *output* does not contain the window, so changing what
// its elements are cannot change its type.
func windowConsumer(prim string) bool {
	switch prim {
	case "Map Each", "Count Matching", "Sum By", "Any", "All":
		return true
	}
	return false
}

// fuseWindowPositions turns `Window size [step]` + a consumer that reads each
// window only as first(w), last(w) or item(w, c) into WindowPositions + the
// same consumer over small tuples (or over the bare element when one position
// is read).
//
// Which reads are allowed is decided by whether the naive pipeline could fail
// on them. first(w), last(w) and item(w, 0) cannot: a window holds at least one
// element (size >= 1 is checked wherever the size is resolved). item(w, c) for
// c > 0 can, whenever c >= size, and then fails inside the lambda, after
// whatever the lambda evaluated before it and in the lambda's words with the
// window's length in them. That is not something a tuple can reproduce, so it
// is only rewritten when the size is a literal and c < size — where it cannot
// fail at all. A measured size is only known when the data arrives, so it
// admits the three reads that need no bound.
func fuseWindowPositions(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Window" || !windowConsumer(b.Prim) || a.In == nil || a.In.Kind != ir.KList || a.In.Elem == nil {
			return nil, "", false
		}
		size, sizeOK := readArg(a, "size")
		step, stepOK := readArg(a, "step")
		if !sizeOK || !stepOK {
			return nil, "", false
		}
		if !size.measured() && size.lit < 1 || !step.measured() && step.lit < 1 {
			return nil, "", false
		}
		lam := nodeLambda(b)
		if lam == nil || len(lam.Params) != 1 {
			return nil, "", false
		}
		if _, isBlock := lam.Body.(*ast.BlockBody); isBlock {
			return nil, "", false
		}
		if b.Prim == "Sum By" && (b.Out == nil || b.Out.Kind != ir.KInt) {
			return nil, "", false
		}
		w := lam.Params[0]

		// Pass one: which positions does the lambda read, and may it?
		var slots []int64 // an offset, or -1 for "the last element"
		slotOf := func(r windowRead) (int, bool) {
			off := r.off
			if r.last {
				off = -1
				if !size.measured() {
					off = size.lit - 1 // the same element as item(w, size-1)
				}
			}
			if off > 0 && (size.measured() || off >= size.lit) {
				return 0, false
			}
			for i, s := range slots {
				if s == off {
					return i, true
				}
			}
			slots = append(slots, off)
			return len(slots) - 1, true
		}
		allowed := true
		if _, ok := rewriteWindowReads(lam.Body, w, func(r windowRead, pos token.Position) (ast.Expr, bool) {
			if _, ok := slotOf(r); !ok {
				allowed = false
				return nil, false
			}
			return &ast.IntLit{Value: 0, Pos: pos}, true
		}); !ok || !allowed || len(slots) == 0 {
			return nil, "", false
		}

		// Pass two: the same walk, now spelling each read as its tuple slot.
		arity := len(slots)
		body, ok := rewriteWindowReads(lam.Body, w, func(r windowRead, pos token.Position) (ast.Expr, bool) {
			i, _ := slotOf(r)
			if arity == 1 {
				return &ast.Ident{Name: w, Pos: pos}, true
			}
			return &ast.CallExpr{
				Fn:   &ast.Ident{Name: "item", Pos: pos},
				Args: []ast.Expr{&ast.Ident{Name: w, Pos: pos}, &ast.IntLit{Value: int64(i), Pos: pos}},
				Pos:  pos,
			}, true
		})
		if !ok {
			return nil, "", false
		}

		elem := a.In.Elem
		outElem := elem
		if arity > 1 {
			elems := make([]*ir.Type, arity)
			for i := range elems {
				elems[i] = elem
			}
			outElem = ir.Tuple(elems...)
		}
		positions := newWindowPositionsNode(a, size, step, slots, outElem)
		onTuples := &ast.Lambda{Params: lam.Params, Body: body, Pos: lam.Pos}
		from := &ir.Node{In: positions.Out, Out: b.Out, Pos: b.Pos, Display: b.Display}
		var consumer *ir.Node
		switch b.Prim {
		case "Map Each":
			consumer = newMapEachNode(from, onTuples, b.Display)
		case "Count Matching":
			consumer = newCountMatchingNode(from, onTuples)
		case "Sum By":
			consumer = newSumByNode(from, onTuples)
		default: // Any, All
			consumer = newQuantifierNode(from, b.Prim, onTuples)
		}
		consumer.Display = b.Display
		return []*ir.Node{positions, consumer},
			fmt.Sprintf("Domain rewrote Window %s + %s → Window Positions + %s (each window is read only at %s, so those elements are read straight from the list instead of every window being copied). Guaranteed hit.",
				size.describe(), b.Prim, b.Prim, describeSlots(slots)),
			true
	})
}

// describeSlots names the positions a Window Positions node reads.
func describeSlots(slots []int64) string {
	parts := make([]string, len(slots))
	for i, s := range slots {
		if s < 0 {
			parts[i] = "its last element"
		} else {
			parts[i] = fmt.Sprintf("position %d", s)
		}
	}
	return strings.Join(parts, ", ")
}

// newWindowPositionsNode builds the node. slots holds one offset per element
// of the output tuple, in order — or, with a single slot, per output element —
// where a negative offset means the window's last element. Everything before
// the loop is Window's own: the input is read as a list, then Size, then Step,
// each through the argument's own resolver, so a measured one fails in the
// same words and in the same order.
func newWindowPositionsNode(a *ir.Node, size, step arg, slots []int64, outElem *ir.Type) *ir.Node {
	pos := a.Pos
	meta := map[string]any{"slots": slots}
	size.writeMeta(meta, "size")
	step.writeMeta(meta, "step")
	return &ir.Node{
		Prim:    "WindowPositions",
		In:      a.In,
		Out:     ir.List(outElem),
		Display: fmt.Sprintf("Window Positions (size %s, step %s, reads %s)", size.describe(), step.describe(), describeSlots(slots)),
		Meta:    meta,
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Window", Pos: pos, Msg: err.Error()}
			}
			sz, err := size.value(v)
			if err != nil {
				return nil, err
			}
			st, err := step.value(v)
			if err != nil {
				return nil, err
			}
			at := func(i int64, slot int64) ir.Value {
				if slot < 0 {
					return xs[i+sz-1]
				}
				return xs[i+slot]
			}
			capacity := int64(0)
			if sz <= int64(len(xs)) {
				capacity = (int64(len(xs))-sz)/st + 1
			}
			out := make([]ir.Value, 0, capacity)
			for i := int64(0); i+sz <= int64(len(xs)); i += st {
				if len(slots) == 1 {
					out = append(out, at(i, slots[0]))
					continue
				}
				tup := make([]ir.Value, len(slots))
				for j, s := range slots {
					tup[j] = at(i, s)
				}
				out = append(out, tup)
			}
			return out, nil
		},
	}
}
