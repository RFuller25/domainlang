package codegen

import (
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/optimizer"
	"domain/pattern"
	"domain/token"
)

// Stream fusion: the general case of what tryFuse does for named shapes.
//
// A run of elementwise stages between a source and a sink compiles to one
// loop. Each element flows through every stage before the next element is
// read, so no stage's output list is ever built:
//
//	source   a List already in hand, a Split of a Text by a literal separator,
//	         or a Range — which is then a counting loop and never a list
//	stages   Map Each, Filter, Convert To Integers / Floats — any number, in
//	         any order
//	sink     Sum, Count, Count Matching, Sum By, Product By, Max, Min,
//	         Product, Any, All, CountDistinct, Join with a literal separator,
//	         or (when nothing else fits) the surviving elements, collected
//
// Interleaving is only invisible when no stage can tell the difference, so
// the run is cut short at the first stage that could:
//
//   - every lambda must be pure (optimizer.Elementwise): a `:=`, a global
//     something writes, or a die roll would see its sibling stages run in a
//     different order;
//   - at most one stage may fail. With one, the first failure is the same
//     element's either way; with two, stage two could fail on element one
//     before stage one fails on element five, and the program would report a
//     different error than the interpreter.
//
// tryFuse runs first and keeps every shape it knows; this takes what is left.
type streamStage struct {
	node *ir.Node
	kind string // "map", "filter", "ints" or "floats"
}

// streamPlan is a run tryStream has decided it can fuse: where it starts, the
// stages it passes through, and the sink that ends it (nil when the surviving
// elements are collected).
type streamPlan struct {
	sep                  string
	fromSplit, fromRange bool
	stages               []streamStage
	sink                 *ir.Node
	consumed             int
}

// planStream finds the longest safe run at the head of nodes without emitting
// anything. ok is false when the run would remove no intermediate list.
func planStream(nodes []*ir.Node, in string) (pl streamPlan, ok bool) {
	i := 0
	if len(nodes) > 0 && nodes[0].Prim == "Split" && !hasMeasured(nodes[0], "sep") {
		if s, _ := nodes[0].Meta["sep"].(string); s != "" && nodes[0].In != nil && nodes[0].In.Kind == ir.KText {
			pl.sep, pl.fromSplit = s, true
			i = 1
		}
	}
	// A Range ignores the value flowing in and counts; fused, the numbers go
	// straight to the stages and the list of them is never built.
	pl.fromRange = !pl.fromSplit && len(nodes) > 0 && nodes[0].Prim == "Range"
	if pl.fromRange {
		i = 1
	}
	if in == "" || (!pl.fromSplit && !pl.fromRange && (len(nodes) == 0 || !isList(nodes[0].In))) {
		return pl, false
	}

	fallible := 0
	for ; i < len(nodes); i++ {
		st, canFail, ok := streamStageOf(nodes[i])
		if !ok || fallible+canFail > 1 {
			break
		}
		fallible += canFail
		pl.stages = append(pl.stages, st)
	}

	if i < len(nodes) && streamSink(nodes[i], fallible, pl.fromSplit) {
		pl.sink = nodes[i]
	}
	// A run that ends in no sink only collects what survives. For the stages
	// that exist to feed a dedicated lowering — a Split Each into a digit grid
	// or a running intersection, a Match Pattern into a keyed extremum —
	// collecting would take the nodes those lowerings start from, and a
	// materialized [][]string is worse than the shape they would have fused to.
	// So the run is only taken for these when it reaches a sink.
	if pl.sink == nil {
		for _, st := range pl.stages {
			if st.kind == "splitEach" || st.kind == "intsEach" || st.kind == "match" {
				return pl, false
			}
		}
	}
	pl.consumed = i
	if pl.sink != nil {
		pl.consumed++
	}
	// Two nodes is the least that removes a list: a source and one stage, or
	// a stage and a sink. A lone Map Each is already one loop.
	return pl, pl.consumed >= 2
}

// streamsParseIntoSink reports whether a `Split` + `Convert To Integers` at the
// head of nodes is the start of a run that streams all the way into a sink.
// tryFuse asks before it takes those two nodes for its own parse-into-a-list
// lowering: that is the right code when something needs the list, and a list
// nobody reads when the run ends in a reduction.
func streamsParseIntoSink(nodes []*ir.Node, in string) bool {
	pl, ok := planStream(nodes, in)
	return ok && pl.fromSplit && pl.sink != nil && pl.consumed >= 3
}

// tryStream fuses the longest safe run at the head of nodes. ok is false when
// the run would remove no intermediate list, which leaves the per-node path
// to emit it unchanged.
func (g *gen) tryStream(nodes []*ir.Node, in string) (int, string, bool, error) {
	pl, ok := planStream(nodes, in)
	if !ok {
		return 0, "", false, nil
	}
	mark := g.main.Len()
	v, err := g.emitStreamRun(pl.sep, pl.fromSplit, pl.fromRange, in, nodes[0], pl.stages, pl.sink)
	if err != nil {
		return 0, "", false, err
	}
	if pl.fromRange {
		// Range reads the incoming value only through a measured bound; with
		// literal bounds nothing mentions it, and Go refuses an unused variable.
		g.keepAlive(in, v, mark)
	}
	return pl.consumed, v, true, nil
}

func isList(t *ir.Type) bool { return t != nil && t.Kind == ir.KList }

// streamStageOf classifies an elementwise stage. canFail is 1 when running it
// can raise an error.
func streamStageOf(n *ir.Node) (st streamStage, canFail int, ok bool) {
	if !isList(n.In) {
		return st, 0, false
	}
	switch n.Prim {
	case "Map Each", "Filter":
		lam, _ := n.Meta["lambda"].(*ast.Lambda)
		pure, total := optimizer.Elementwise(lam, n.In.Elem)
		if !pure {
			return st, 0, false
		}
		kind := "map"
		if n.Prim == "Filter" {
			kind = "filter"
		}
		if total {
			return streamStage{n, kind}, 0, true
		}
		return streamStage{n, kind}, 1, true
	case "Convert To Integers":
		if n.In.Equal(ir.List(ir.Text())) {
			return streamStage{n, "ints"}, 1, true
		}
		if n.In.Equal(ir.List(ir.List(ir.Text()))) {
			return streamStage{n, "intsEach"}, 1, true
		}
	case "Split Each":
		// A literal separator splits each element on its own; the list of
		// split lists is never built. It cannot fail.
		if _, ok := n.Meta["sep"].(string); ok && !hasMeasured(n, "sep") && n.In.Equal(ir.List(ir.Text())) {
			return streamStage{n, "splitEach"}, 0, true
		}
	case "Match Pattern":
		// Each line parsed to its record as the loop reaches it. A line that
		// does not match is the one way it fails, so it is a fallible stage.
		try, _ := n.Meta["try"].(bool)
		scan, _ := n.Meta["scan"].(bool)
		each, _ := n.Meta["each"].(bool)
		cases, _ := n.Meta["cases"].([][2]string)
		tmpl, _ := n.Meta["template"].(string)
		if each && !try && !scan && len(cases) == 0 && tmpl != "" && n.In.Equal(ir.List(ir.Text())) {
			return streamStage{n, "match"}, 1, true
		}
	case "Convert To Floats":
		// From Text a parse, which can fail; from Int a widening, which cannot.
		switch {
		case n.In.Equal(ir.List(ir.Text())):
			return streamStage{n, "floats"}, 1, true
		case n.In.Equal(ir.List(ir.Int())):
			return streamStage{n, "floats"}, 0, true
		}
	}
	return st, 0, false
}

// streamSink reports whether n can consume the stream directly. Count
// Matching's predicate is a stage in all but name, so it counts against the
// one-fallible-stage allowance like one.
//
// Any and All stop at the element that decides them, which skips every stage
// evaluation after it — so they are sinks only when nothing in the run can
// fail, and (fromSplit) not over the split walk, whose loop has no single
// exit to break to.
func streamSink(n *ir.Node, fallible int, fromSplit bool) bool {
	if !isList(n.In) {
		return false
	}
	switch n.Prim {
	case "Sum By", "Product By":
		lam, _ := n.Meta["lambda"].(*ast.Lambda)
		pure, total := optimizer.Elementwise(lam, n.In.Elem)
		return pure && (total || fallible == 0) && n.Out != nil && n.Out.Kind == ir.KInt
	case "Any", "All":
		lam, _ := n.Meta["lambda"].(*ast.Lambda)
		pure, total := optimizer.Elementwise(lam, n.In.Elem)
		return pure && total && fallible == 0 && !fromSplit
	case "CountDistinct":
		return true
	case "Count By", "Group Reduce", "Group By":
		// The key lambda is a stage in all but name, like Count Matching's
		// predicate. Group Reduce sums the element itself or counts it.
		lam, _ := n.Meta["lambda"].(*ast.Lambda)
		pure, total := optimizer.Elementwise(lam, n.In.Elem)
		return pure && (total || fallible == 0) && n.Out != nil && n.Out.Kind == ir.KMap
	case "Fold":
		return foldSinkOK(n, fallible)
	case "Sum":
		return n.Out != nil && (n.Out.Kind == ir.KInt || n.Out.Kind == ir.KFloat)
	case "Count":
		return true
	case "Max", "Min", "Product":
		// Their one failure, an empty list, is decided after the loop — the
		// same point it is decided unfused, once every stage has run.
		return n.Out != nil && (n.Out.Kind == ir.KInt || n.Out.Kind == ir.KFloat)
	case "Join":
		return !hasMeasured(n, "sep") && n.In.Elem != nil && n.In.Elem.Kind == ir.KText
	case "Count Matching":
		lam, _ := n.Meta["lambda"].(*ast.Lambda)
		pure, total := optimizer.Elementwise(lam, n.In.Elem)
		return pure && (total || fallible == 0)
	}
	return false
}

// foldSinkOK reports whether a Fold can consume the stream directly: its
// lambda pure, its seed something that does not need the list that will no
// longer exist, and no second thing in the run able to fail before it.
//
// Fold's lambda is a stage in all but name, so it counts against the
// one-fallible-stage allowance like Count Matching's predicate does. A measured
// seed is a lambda over the *whole list*: it may be streamed past only when it
// does not read it, and is total — it runs before the first element here and
// after the last stage unfused, so a failing seed would otherwise be reported
// ahead of a stage failure that comes first in the unfused program.
func foldSinkOK(n *ir.Node, fallible int) bool {
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil || len(lam.Params) != 2 || !optimizer.PureLambda(lam) || n.Out == nil {
		return false
	}
	if _, lit := n.Meta["seed"]; !lit {
		sl, _ := n.Meta["seedExpr"].(*ast.Lambda)
		if sl == nil || len(sl.Params) != 1 || !optimizer.PureLambda(sl) ||
			optimizer.ExprMentions(sl.Body, sl.Params[0]) || !optimizer.TotalExpr(sl.Body) {
			return false
		}
	}
	return optimizer.TotalExpr(lam.Body) || fallible == 0
}

// emitStreamRun writes the fused loop. first is the chain's first node, whose
// input is the source; each stage binds the element under a fresh name for
// the next.
func (g *gen) emitStreamRun(sep string, fromSplit, fromRange bool, in string, first *ir.Node, stages []streamStage, sink *ir.Node) (string, error) {
	// The element type leaving the chain decides what a collecting sink
	// builds; it is the last stage's output, or the source's element.
	var outElem *ir.Type
	var lo, hi string
	switch {
	case fromSplit:
		outElem = ir.Text()
	case fromRange:
		outElem = ir.Int()
		var err error
		if lo, hi, err = g.emitRangeBounds(first, in); err != nil {
			return "", err
		}
	default:
		outElem = first.In.Elem
	}
	if len(stages) > 0 {
		outElem = stages[len(stages)-1].node.Out.Elem
	}

	// The sink's state is declared before the loop; its per-element step is
	// what the innermost stage runs.
	v := g.fresh("v")
	var step func(x string) error
	finish := func() {}
	// exit is the label an early-exit sink breaks to, when there is one.
	exit := ""
	// A Join writes a mapped `a + b + …` of Text piece by piece rather than
	// building the concatenation first: the pieces go into the same buffer
	// either way, and the concatenation would be one allocation per element.
	// joinBuf is set when the sink is a Join; pendingParts maps the name a
	// last-stage map would have bound to the pieces it stands for.
	joinBuf := ""
	pendingParts := map[string][]string{}
	switch {
	case sink == nil:
		elemGo, err := g.goType(outElem)
		if err != nil {
			return "", err
		}
		// Sized like Filter's output: to the input when it is in hand, since
		// a list that keeps most of what it reads otherwise regrows dozens of
		// times. A Split source has no length until it is walked.
		switch {
		case fromSplit:
			g.wl("var %s []%s", v, elemGo)
		case fromRange:
			g.wl("%s := make([]%s, 0, %s-%s)", v, elemGo, hi, lo)
		default:
			g.wl("%s := make([]%s, 0, len(%s))", v, elemGo, in)
		}
		step = func(x string) error { g.wl("%s = append(%s, %s)", v, v, x); return nil }
	case sink.Prim == "Sum By" || sink.Prim == "Product By":
		identity, op := "0", "+="
		if sink.Prim == "Product By" {
			identity, op = "1", "*="
		}
		g.wl("%s := int64(%s)", v, identity)
		step = func(x string) error {
			lam, err := g.nodeLambda(sink)
			if err != nil {
				return err
			}
			body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: x, typ: sink.In.Elem}})
			if err != nil {
				return unsupported(sink, "lambda: %v", err)
			}
			g.keepElem(x, body)
			g.wl("%s %s %s", v, op, body)
			return nil
		}
	case sink.Prim == "Any" || sink.Prim == "All":
		// The empty stream takes the identity of the connective, and the
		// deciding element is the last one any stage sees.
		universal := sink.Prim == "All"
		exit = g.fresh("decided")
		g.wl("%s := %v", v, universal)
		step = func(x string) error {
			lam, err := g.nodeLambda(sink)
			if err != nil {
				return err
			}
			pred, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: x, typ: sink.In.Elem}})
			if err != nil {
				return unsupported(sink, "lambda: %v", err)
			}
			g.keepElem(x, pred)
			if universal {
				g.wl("if !(%s) {", pred)
			} else {
				g.wl("if %s {", pred)
			}
			g.in()
			g.wl("%s = %v", v, !universal)
			g.wl("break %s", exit)
			g.out()
			g.wl("}")
			return nil
		}
	case sink.Prim == "Group By":
		keyGo, err := g.goType(sink.Out.Key)
		if err != nil {
			return "", unsupported(sink, "%v", err)
		}
		valGo, err := g.goType(sink.Out.Elem)
		if err != nil {
			return "", unsupported(sink, "%v", err)
		}
		g.chelper(sink.Out.Key, "dmMap", declMap)
		g.chelper(sink.Out.Key, "dmAppend", declMapAppend)
		if err := g.wnewMap(v, sink.Out.Key, keyGo, valGo); err != nil {
			return "", unsupported(sink, "%v", err)
		}
		step = func(x string) error {
			lam, err := g.nodeLambda(sink)
			if err != nil {
				return err
			}
			key, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: x, typ: sink.In.Elem}})
			if err != nil {
				return unsupported(sink, "lambda: %v", err)
			}
			g.keepElem(x, key)
			g.wl("%s(&%s, %s, %s)", cname(sink.Out.Key, "dmAppend"), v, key, x)
			return nil
		}
	case sink.Prim == "Count By" || sink.Prim == "Group Reduce":
		keyGo, err := g.goType(sink.Out.Key)
		if err != nil {
			return "", unsupported(sink, "%v", err)
		}
		g.chelper(sink.Out.Key, "dmMap", declMap)
		g.chelper(sink.Out.Key, "dmBump", declMapBump)
		if err := g.wnewMap(v, sink.Out.Key, keyGo, "int64"); err != nil {
			return "", unsupported(sink, "%v", err)
		}
		step = func(x string) error {
			lam, err := g.nodeLambda(sink)
			if err != nil {
				return err
			}
			key, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: x, typ: sink.In.Elem}})
			if err != nil {
				return unsupported(sink, "lambda: %v", err)
			}
			delta := "1"
			if reduce, _ := sink.Meta["reduce"].(string); sink.Prim == "Group Reduce" && reduce == "sum" {
				delta = x
			}
			g.keepElem(x, key+" "+delta)
			g.wl("%s(&%s, %s, %s)", cname(sink.Out.Key, "dmBump"), v, key, delta)
			return nil
		}
	case sink.Prim == "Fold":
		lam, err := g.nodeLambda(sink)
		if err != nil {
			return "", err
		}
		seed, err := g.measuredLit(sink, in, "seed", sink.In, sink.Out, seedLit)
		if err != nil {
			return "", err
		}
		accGo, err := g.goType(sink.Out)
		if err != nil {
			return "", unsupported(sink, "%v", err)
		}
		g.wl("var %s %s = %s", v, accGo, g.ownAccumulator(lam, sink.Out, seed))
		step = func(x string) error {
			body, _, err := g.compileExpr(lam.Body, exprEnv{
				lam.Params[0]: {expr: v, typ: sink.Out},
				lam.Params[1]: {expr: x, typ: sink.In.Elem},
			})
			if err != nil {
				return unsupported(sink, "lambda: %v", err)
			}
			g.keepElem(x, body)
			g.wl("%s = %s", v, body)
			return nil
		}
	case sink.Prim == "CountDistinct":
		elemGo, err := g.goType(outElem)
		if err != nil {
			return "", err
		}
		seen := g.fresh("seen")
		g.wl("%s := make(map[%s]struct{})", seen, elemGo)
		step = func(x string) error { g.wl("%s[%s] = struct{}{}", seen, x); return nil }
		finish = func() { g.wl("%s := int64(len(%s))", v, seen) }
	case sink.Prim == "Sum":
		acc, err := g.goType(sink.Out)
		if err != nil {
			return "", err
		}
		g.wl("var %s %s", v, acc)
		step = func(x string) error { g.wl("%s += %s", v, x); return nil }
	case sink.Prim == "Max" || sink.Prim == "Min" || sink.Prim == "Product":
		acc, err := g.goType(sink.Out)
		if err != nil {
			return "", err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		seen := g.fresh("seen")
		g.wl("var %s %s", v, acc)
		g.wl("%s := false", seen)
		step = func(x string) error {
			g.wl("if !%s {", seen)
			g.in()
			g.wl("%s, %s = %s, true", v, seen, x)
			g.out()
			switch sink.Prim {
			case "Product":
				g.wl("} else {")
				g.in()
				g.wl("%s *= %s", v, x)
				g.out()
			default:
				op := ">"
				if sink.Prim == "Min" {
					op = "<"
				}
				g.wl("} else if %s %s %s {", x, op, v)
				g.in()
				g.wl("%s = %s", v, x)
				g.out()
			}
			g.wl("}")
			return nil
		}
		finish = func() {
			g.wl("if !%s {", seen)
			g.in()
			g.wl("dmFail(%s)", goStr(sink.Prim+" of an empty list is undefined"))
			g.out()
			g.wl("}")
		}
	case sink.Prim == "Count":
		g.wl("var %s int64", v)
		// The element is not needed, only its arrival.
		step = func(x string) error { g.keepElem(x, ""); g.wl("%s++", v); return nil }
	case sink.Prim == "Count Matching":
		g.wl("var %s int64", v)
		step = func(x string) error {
			lam, err := g.nodeLambda(sink)
			if err != nil {
				return err
			}
			pred, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: x, typ: sink.In.Elem}})
			if err != nil {
				return unsupported(sink, "lambda: %v", err)
			}
			g.keepElem(x, pred)
			g.wl("if %s {", pred)
			g.in()
			g.wl("%s++", v)
			g.out()
			g.wl("}")
			return nil
		}
	case sink.Prim == "Join":
		// Built in one buffer and printed by whatever follows, as the unfused
		// Join is: writing straight to stdout would print half an answer
		// before a failing stage stopped the program, where the unfused
		// program prints nothing.
		g.imp("strings")
		sepLit, _ := sink.Meta["sep"].(string)
		b := g.fresh("b")
		g.wl("var %s strings.Builder", b)
		// strings.Join sizes its buffer once; a builder left to grow copies
		// the answer about twice over, which measured slower than the lists
		// fusion saves. From a Split, the text being split is the natural
		// estimate — but a per-line transform that appends even one byte to
		// every line overflows an exact one, and the builder then reallocates
		// and copies the whole answer (measured: 103 ms against 71 ms on a
		// 12 MB `upper(s) + "!"`). A quarter of headroom costs nothing it does
		// not use — untouched capacity is never faulted in — and covers it.
		if fromSplit {
			g.wl("%s.Grow(len(%s) + len(%s)/4 + 64)", b, in, in)
		}
		// The separator goes before every element but the first. Counting
		// elements rather than testing the builder's length matters: the
		// first element may be empty.
		more := ""
		if sepLit != "" {
			more = g.fresh("more")
			g.wl("%s := false", more)
		}
		joinBuf = b
		step = func(x string) error {
			if more != "" {
				g.wl("if %s {", more)
				g.in()
				g.wl("%s.WriteString(%s)", b, goStr(sepLit))
				g.out()
				g.wl("}")
				g.wl("%s = true", more)
			}
			if parts, ok := pendingParts[x]; ok {
				for _, p := range parts {
					g.wl("%s.WriteString(%s)", b, p)
				}
				return nil
			}
			g.wl("%s.WriteString(%s)", b, x)
			return nil
		}
		finish = func() { g.wl("%s := %s.String()", v, b) }
	}

	var run func(k int, x string) error
	run = func(k int, x string) error {
		if k == len(stages) {
			return step(x)
		}
		st := stages[k]
		switch st.kind {
		case "ints":
			g.helper("dmFail", declFail, "fmt", "os")
			g.helper("dmParseInt", declParseInt, "strconv", "strings")
			g.helper("dmParseIntSeg", declParseIntSeg)
			y := g.fresh("n")
			g.wl("%s := dmParseIntSeg(%s)", y, x)
			return run(k+1, y)
		case "splitEach":
			g.imp("strings")
			sep, _ := st.node.Meta["sep"].(string)
			y := g.fresh("row")
			g.wl("%s := strings.Split(%s, %s)", y, x, goStr(sep))
			return run(k+1, y)
		case "intsEach":
			g.helper("dmFail", declFail, "fmt", "os")
			g.helper("dmParseInt", declParseInt, "strconv", "strings")
			g.helper("dmParseIntSeg", declParseIntSeg)
			y, j, f := g.fresh("ns"), g.fresh("j"), g.fresh("f")
			g.wl("%s := make([]int64, len(%s))", y, x)
			g.wl("for %s, %s := range %s {", j, f, x)
			g.in()
			g.wl("%s[%s] = dmParseIntSeg(%s)", y, j, f)
			g.out()
			g.wl("}")
			return run(k+1, y)
		case "match":
			tmplStr, _ := st.node.Meta["template"].(string)
			tmpl, err := pattern.ParseTemplate(tmplStr)
			if err != nil {
				return unsupported(st.node, "template: %v", err)
			}
			elemType := tmpl.OutputType()
			elemGo, err := g.goType(elemType)
			if err != nil {
				return unsupported(st.node, "%v", err)
			}
			fn, err := g.matchParseFunc(tmpl, elemType, elemGo)
			if err != nil {
				return unsupported(st.node, "%v", err)
			}
			g.helper("dmFail", declFail, "fmt", "os")
			y, ok := g.fresh("r"), g.fresh("ok")
			g.wl("%s, %s := %s(%s)", y, ok, fn, x)
			g.wl("if !%s {", ok)
			g.in()
			g.wl(`dmFail("input %%q does not match template %%q", %s, %s)`, x, goStr(tmplStr))
			g.out()
			g.wl("}")
			return run(k+1, y)
		case "floats":
			y := g.fresh("f")
			if st.node.In.Elem.Kind == ir.KInt {
				g.wl("%s := float64(%s)", y, x)
			} else {
				g.helper("dmFail", declFail, "fmt", "os")
				g.helper("dmParseFloat", declParseFloat, "strconv", "strings")
				g.wl("%s := dmParseFloat(%s)", y, x)
			}
			return run(k+1, y)
		case "map":
			lam, err := g.nodeLambda(st.node)
			if err != nil {
				return err
			}
			body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: x, typ: st.node.In.Elem}})
			if err != nil {
				return unsupported(st.node, "lambda: %v", err)
			}
			if k == len(stages)-1 && joinBuf != "" {
				if operands := textConcat(lam.Body, st.node.Out.Elem); len(operands) > 1 {
					var parts []string
					for _, o := range operands {
						p, _, err := g.compileExpr(o, exprEnv{lam.Params[0]: {expr: x, typ: st.node.In.Elem}})
						if err != nil {
							return unsupported(st.node, "lambda: %v", err)
						}
						parts = append(parts, p)
					}
					g.keepElem(x, strings.Join(parts, " "))
					y := g.fresh("e")
					pendingParts[y] = parts
					return run(k+1, y)
				}
			}
			// Declared with its type: a body that is a bare literal is an
			// untyped Go constant, and := would make it an int.
			elemGo, err := g.goType(st.node.Out.Elem)
			if err != nil {
				return unsupported(st.node, "%v", err)
			}
			g.keepElem(x, body)
			y := g.fresh("e")
			g.wl("var %s %s = %s", y, elemGo, body)
			return run(k+1, y)
		default: // filter
			lam, err := g.nodeLambda(st.node)
			if err != nil {
				return err
			}
			pred, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: x, typ: st.node.In.Elem}})
			if err != nil {
				return unsupported(st.node, "lambda: %v", err)
			}
			g.keepElem(x, pred)
			g.wl("if %s {", pred)
			g.in()
			if err := run(k+1, x); err != nil {
				return err
			}
			g.out()
			g.wl("}")
			return nil
		}
	}

	var err error
	if fromSplit {
		g.imp("strings")
		line := g.fresh("line")
		g.emitLines(sep, in, line, func() { err = run(0, line) })
	} else {
		e := g.fresh("e")
		if exit != "" {
			g.wl("%s:", exit)
		}
		if fromRange {
			g.wl("for %s := int64(%s); %s < %s; %s++ {", e, lo, e, hi, e)
		} else {
			g.wl("for _, %s := range %s {", e, in)
		}
		g.in()
		err = run(0, e)
		g.out()
		g.wl("}")
	}
	if err != nil {
		return "", err
	}
	finish()
	return v, nil
}

// textConcat flattens a Text-typed `a + b + …` into its operands, left to
// right; anything else is one operand. Every operand of a `+` whose result is
// Text is itself Text (the typechecker allows no mixed concatenation), so
// each can be written on its own.
func textConcat(e ast.Expr, t *ir.Type) []ast.Expr {
	if t == nil || t.Kind != ir.KText {
		return []ast.Expr{e}
	}
	be, ok := e.(*ast.BinaryExpr)
	if !ok || be.Op != token.PLUS {
		return []ast.Expr{e}
	}
	return append(textConcat(be.Left, t), textConcat(be.Right, t)...)
}

// emitTextFold lowers the optimizer's Text Fold (optimizer.fuseTextFold): the
// seed, then each element's appended parts, written to one builder. The
// accumulator parameter is bound to "" — the pass proved no part reads it.
func (g *gen) emitTextFold(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	seed, _ := n.Meta["seed"].(string)
	g.imp("strings")
	b, e, v := g.fresh("b"), g.fresh("e"), g.fresh("v")
	env := exprEnv{
		lam.Params[0]: {expr: `""`, typ: ir.Text()},
		lam.Params[1]: {expr: e, typ: n.In.Elem},
	}
	var parts []string
	for _, part := range textConcat(lam.Body, ir.Text()) {
		p, _, err := g.compileExpr(part, env)
		if err != nil {
			return "", unsupported(n, "lambda: %v", err)
		}
		parts = append(parts, p)
	}
	g.wl("var %s strings.Builder", b)
	if seed != "" {
		g.wl("%s.WriteString(%s)", b, goStr(seed))
	}
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.keepElem(e, strings.Join(parts, " "))
	for _, p := range parts {
		g.wl("%s.WriteString(%s)", b, p)
	}
	g.out()
	g.wl("}")
	g.wl("%s := %s.String()", v, b)
	return v, nil
}
