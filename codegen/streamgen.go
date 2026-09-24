package codegen

import (
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/optimizer"
	"domain/token"
)

// Stream fusion: the general case of what tryFuse does for named shapes.
//
// A run of elementwise stages between a source and a sink compiles to one
// loop. Each element flows through every stage before the next element is
// read, so no stage's output list is ever built:
//
//	source   a List already in hand, or a Split of a Text by a literal separator
//	stages   Map Each, Filter, Convert To Integers / Floats — any number, in
//	         any order
//	sink     Sum, Count, Count Matching, Max, Min, Product, Join with a
//	         literal separator, or (when nothing else fits) the surviving
//	         elements, collected
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

// tryStream fuses the longest safe run at the head of nodes. ok is false when
// the run would remove no intermediate list, which leaves the per-node path
// to emit it unchanged.
func (g *gen) tryStream(nodes []*ir.Node, in string) (int, string, bool, error) {
	i := 0
	var sep string
	fromSplit := false
	if len(nodes) > 0 && nodes[0].Prim == "Split" && !hasMeasured(nodes[0], "sep") {
		if s, _ := nodes[0].Meta["sep"].(string); s != "" && nodes[0].In != nil && nodes[0].In.Kind == ir.KText {
			sep, fromSplit = s, true
			i = 1
		}
	}
	if in == "" || (!fromSplit && (len(nodes) == 0 || !isList(nodes[0].In))) {
		return 0, "", false, nil
	}

	var stages []streamStage
	fallible := 0
	for ; i < len(nodes); i++ {
		st, canFail, ok := streamStageOf(nodes[i])
		if !ok || fallible+canFail > 1 {
			break
		}
		fallible += canFail
		stages = append(stages, st)
	}

	var sink *ir.Node
	if i < len(nodes) && streamSink(nodes[i], fallible) {
		sink = nodes[i]
	}
	consumed := i
	if sink != nil {
		consumed++
	}
	// Two nodes is the least that removes a list: a source and one stage, or
	// a stage and a sink. A lone Map Each is already one loop.
	if consumed < 2 {
		return 0, "", false, nil
	}
	v, err := g.emitStreamRun(sep, fromSplit, in, nodes[0], stages, sink)
	if err != nil {
		return 0, "", false, err
	}
	return consumed, v, true, nil
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
func streamSink(n *ir.Node, fallible int) bool {
	if !isList(n.In) {
		return false
	}
	switch n.Prim {
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

// emitStreamRun writes the fused loop. first is the chain's first node, whose
// input is the source; each stage binds the element under a fresh name for
// the next.
func (g *gen) emitStreamRun(sep string, fromSplit bool, in string, first *ir.Node, stages []streamStage, sink *ir.Node) (string, error) {
	// The element type leaving the chain decides what a collecting sink
	// builds; it is the last stage's output, or the source's element.
	outElem := first.In.Elem
	if fromSplit {
		outElem = ir.Text()
	}
	if len(stages) > 0 {
		outElem = stages[len(stages)-1].node.Out.Elem
	}

	// The sink's state is declared before the loop; its per-element step is
	// what the innermost stage runs.
	v := g.fresh("v")
	var step func(x string) error
	finish := func() {}
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
		if fromSplit {
			g.wl("var %s []%s", v, elemGo)
		} else {
			g.wl("%s := make([]%s, 0, len(%s))", v, elemGo, in)
		}
		step = func(x string) error { g.wl("%s = append(%s, %s)", v, v, x); return nil }
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
		// estimate — a per-line transform rarely changes the total much.
		if fromSplit {
			g.wl("%s.Grow(len(%s))", b, in)
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
		g.wl("for _, %s := range %s {", e, in)
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
