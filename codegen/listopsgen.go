package codegen

import (
	"domain/ast"
	"domain/ir"
	"domain/optimizer"
)

// Go lowerings for the list-shaping and generating primitives in
// prims/listops.go and prims/generate.go: Take While, Drop While, Chunk,
// Partition, Iterate and Unfold. Each mirrors its interpreter twin, down to
// the early exits — the compiled Take While stops testing at the boundary just
// as the interpreted one does.
//
// The predicate-driven ones bind the lambda parameter to an indexed expression
// (`xs[i]`) rather than a range variable. That keeps the generated loop valid
// even when the predicate ignores its parameter, which a range variable would
// leave declared and unused.

func (g *gen) emitPrefixWhile(n *ir.Node, in string) string {
	elemGo := asUnsupported1(n, "", func() string { return g.goType(n.In.Elem) })
	// optimizer.elideConstEarlyExits: a predicate that is constantly false at
	// the boundary leaves the empty list, and no lambda.
	if empty, _ := n.Meta["empty"].(bool); empty {
		v := g.fresh("v")
		g.wl("%s := []%s{}", v, elemGo)
		return v
	}
	lam := g.nodeLambda(n)
	cut, i := g.fresh("cut"), g.fresh("i")
	body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{
			lam.Params[0]: {expr: in + "[" + i + "]", typ: n.In.Elem},
		})
	})
	g.wl("%s := len(%s)", cut, in)
	g.wl("for %s := 0; %s < len(%s); %s++ {", i, i, in, i)
	g.in()
	g.wl("if !(%s) {", body)
	g.in()
	g.wl("%s = %s", cut, i)
	g.wl("break") // the boundary decides both halves; stop testing
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")

	v := g.fresh("v")
	if n.Prim == "Take While" {
		// Copied: a later append onto a shared backing array would otherwise
		// overwrite the elements this slice stops short of.
		g.wl("%s := append([]%s{}, %s[:%s]...)", v, elemGo, in, cut)
		return v
	}
	// The suffix may alias — appending past the end of the original slice
	// reallocates rather than overwriting it.
	g.wl("%s := %s[%s:]", v, in, cut)
	return v
}

func (g *gen) emitChunk(n *ir.Node, in string) string {
	elemGo := asUnsupported1(n, "", func() string { return g.goType(n.In.Elem) })
	size := g.measuredOperand(n, in, "size", "Size", 1)
	// The loop counts in int, so a measured size — an int64 variable rather
	// than an untyped constant — needs one conversion, bound once beside it.
	sizeI := size
	if hasMeasured(n, "size") {
		sizeI = g.fresh("sz")
		g.wl("%s := int(%s)", sizeI, size)
	}
	v, i, end := g.fresh("v"), g.fresh("i"), g.fresh("end")
	g.wl("%s := make([][]%s, 0, (len(%s)+%s-1)/%s)", v, elemGo, in, sizeI, sizeI)
	g.wl("for %s := 0; %s < len(%s); %s += %s {", i, i, in, i, sizeI)
	g.in()
	g.wl("%s := %s + %s", end, i, sizeI)
	g.wl("if %s > len(%s) {", end, in)
	g.in()
	g.wl("%s = len(%s)", end, in) // the short final block is kept, not dropped
	g.out()
	g.wl("}")
	g.wl("%s = append(%s, append([]%s{}, %s[%s:%s]...))", v, v, elemGo, in, i, end)
	g.out()
	g.wl("}")
	return v
}

func (g *gen) emitPartition(n *ir.Node, in string) string {
	lam := g.nodeLambda(n)
	elemGo := asUnsupported1(n, "", func() string { return g.goType(n.In.Elem) })
	yes, no, i := g.fresh("yes"), g.fresh("no"), g.fresh("i")
	body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{
			lam.Params[0]: {expr: in + "[" + i + "]", typ: n.In.Elem},
		})
	})
	// Either half can hold the whole input, and a cap that guesses the split
	// (half each, say) pays a full reallocation and copy the moment the data
	// is lopsided — which is the normal case for a predicate worth writing.
	// Reserving the length for both trades transient memory for never
	// regrowing; they cannot share one array, since a later append onto one
	// half would then overwrite the other.
	g.wl("%s := make([]%s, 0, len(%s))", yes, elemGo, in)
	g.wl("%s := make([]%s, 0, len(%s))", no, elemGo, in)
	g.wl("for %s := 0; %s < len(%s); %s++ {", i, i, in, i)
	g.in()
	g.wl("if %s {", body)
	g.in()
	g.wl("%s = append(%s, %s[%s])", yes, yes, in, i)
	g.out()
	g.wl("} else {")
	g.in()
	g.wl("%s = append(%s, %s[%s])", no, no, in, i)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	v := g.fresh("v")
	g.wl("%s := [][]%s{%s, %s}", v, elemGo, yes, no)
	return v
}

// emitPartitionHalf lowers Partition + Take Item 0/1: the half that is asked
// for is collected and the other is never built. The predicate still runs on
// every element, in order, so a predicate that fails fails on the same one.
func (g *gen) emitPartitionHalf(part *ir.Node, in string, keep bool) string {
	lam := g.nodeLambda(part)
	elemGo := asUnsupported1(part, "", func() string { return g.goType(part.In.Elem) })
	out, i := g.fresh("v"), g.fresh("i")
	body, _ := asUnsupported2(part, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{
			lam.Params[0]: {expr: in + "[" + i + "]", typ: part.In.Elem},
		})
	})
	g.wl("%s := make([]%s, 0, len(%s))", out, elemGo, in)
	g.wl("for %s := 0; %s < len(%s); %s++ {", i, i, in, i)
	g.in()
	if keep {
		g.wl("if %s {", body)
	} else {
		g.wl("if !%s {", body)
	}
	g.in()
	g.wl("%s = append(%s, %s[%s])", out, out, in, i)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return out
}

func (g *gen) emitIterate(n *ir.Node, in string) string {
	lam := g.nodeLambda(n)
	steps := g.measuredOperand(n, in, "n", "Times", 0)
	elemGo := asUnsupported1(n, "", func() string { return g.goType(n.In) })
	v, cur, i := g.fresh("v"), g.fresh("cur"), g.fresh("i")
	body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: cur, typ: n.In}})
	})
	if stop, _ := n.Meta[optimizer.MetaUntilRepeat].(bool); stop {
		// optimizer.stopIterateAtRepeat: the trajectory is only searched for
		// its first repeat, so it ends at the first state equal to an earlier
		// one. Brent's cycle detection, the loop newIterateUntilRepeat runs:
		// one comparison per step against a mark that jumps forward at powers
		// of two.
		mark, gap, power := g.fresh("mark"), g.fresh("gap"), g.fresh("power")
		same := asUnsupported1(n, "", func() string { return g.eqExpr(v+"["+mark+"]", cur, n.In) })
		g.wl("%s := make([]%s, 0, min(%s, 1024))", v, elemGo, steps)
		g.wl("%s := %s", cur, in)
		g.wl("%s, %s, %s := int64(0), int64(0), int64(1)", mark, gap, power)
		g.wl("for %s := int64(0); %s < %s; %s++ {", i, i, steps, i)
		g.in()
		g.wl("%s = %s", cur, body)
		g.wl("%s = append(%s, %s)", v, v, cur)
		g.wl("if %s == 0 {", i)
		g.in()
		g.wl("continue")
		g.out()
		g.wl("}")
		g.wl("%s++", gap)
		g.wl("if %s {", same)
		g.in()
		g.wl("break")
		g.out()
		g.wl("}")
		g.wl("if %s == %s {", gap, power)
		g.in()
		g.wl("%s, %s, %s = %s, 0, %s*2", mark, gap, power, i, power)
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
		return v
	}
	g.wl("%s := make([]%s, %s)", v, elemGo, steps)
	g.wl("%s := %s", cur, in)
	g.wl("for %s := int64(0); %s < %s; %s++ {", i, i, steps, i)
	g.in()
	g.wl("%s = %s", cur, body)
	g.wl("%s[%s] = %s", v, i, cur)
	g.out()
	g.wl("}")
	return v
}

func (g *gen) emitUnfold(n *ir.Node, in string) string {
	step := g.nodeLambda(n)
	cond, _ := n.Meta["while"].(*ast.Lambda)
	if cond == nil || len(cond.Params) != 1 {
		panic(unsupported(n, "missing While: predicate"))
	}
	elemGo := asUnsupported1(n, "", func() string { return g.goType(n.In) })
	v, cur := g.fresh("v"), g.fresh("cur")
	env := exprEnv{cond.Params[0]: {expr: cur, typ: n.In}}
	condBody, _ := asUnsupported2(n, "While: lambda: ", func() (string, *ir.Type) { return g.compileExpr(cond.Body, env) })
	stepBody, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(step.Body, exprEnv{step.Params[0]: {expr: cur, typ: n.In}})
	})
	g.helper("dmFail", declFail, "fmt", "os")
	// No estimate exists here: how many elements an Unfold produces is a
	// property of its own predicate, not of anything in scope. A caller who
	// has measured it says so through Tuning.ListCapacities.
	g.wl("%s", g.accumDecl(n, v, elemGo))
	g.wl("%s := %s", cur, in)
	g.wl("for %s {", condBody)
	g.in()
	g.wl("%s = append(%s, %s)", v, v, cur)
	// Bounded like the interpreter: a step that never falsifies the predicate
	// fails loudly instead of running out of memory.
	g.wl("if len(%s) > %d {", v, maxUnfoldElements)
	g.in()
	g.wl(`dmFail("Unfold produced more than %d elements (non-terminating While:?)")`, maxUnfoldElements)
	g.out()
	g.wl("}")
	g.wl("%s = %s", cur, stepBody)
	g.out()
	g.wl("}")
	g.probeLen(n, v)
	return v
}

// emitStream lowers the optimizer's fused Unfold + Map Each/Filter (+ take)
// chain (optimizer/streamfuse.go) to one Go loop — the compiled-backend twin
// of that pass's interpreter Eval. Every fused Map Each/Filter lambda was
// proven total when the fusion fired, so — unlike emitMapEach/emitFilter —
// none of the inlined steps need error handling; only Unfold's own bound
// check does, exactly as in emitUnfold.
func (g *gen) emitStream(n *ir.Node, in string) string {
	unfoldNode, _ := n.Meta["unfold"].(*ir.Node)
	steps, _ := n.Meta["steps"].([]*ir.Node)
	earlyExit, _ := n.Meta["earlyExit"].(bool)
	takeN, _ := n.Meta["take"].(int64)
	if unfoldNode == nil {
		panic(unsupported(n, "missing Stream metadata"))
	}
	step := g.nodeLambda(unfoldNode)
	cond, _ := unfoldNode.Meta["while"].(*ast.Lambda)
	if cond == nil || len(cond.Params) != 1 {
		panic(unsupported(n, "missing While: predicate"))
	}
	outElemGo := asUnsupported1(n, "", func() string { return g.goType(n.Out.Elem) })

	v, cur, raw := g.fresh("v"), g.fresh("cur"), g.fresh("raw")
	condBody, _ := asUnsupported2(n, "While: lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(cond.Body, exprEnv{cond.Params[0]: {expr: cur, typ: unfoldNode.In}})
	})
	stepBody, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
		return g.compileExpr(step.Body, exprEnv{step.Params[0]: {expr: cur, typ: unfoldNode.In}})
	})

	g.helper("dmFail", declFail, "fmt", "os")
	// The fused stream has no estimate either, and here the absence is
	// expensive: a `take` limit bounds the list from above but says nothing
	// about how many elements the filters will let through, so the generator
	// starts from nothing and the slice grows by doubling all the way up.
	g.wl("%s", g.accumDecl(n, v, outElemGo))
	g.wl("%s := %s", cur, in)
	g.wl("%s := 0", raw)
	g.wl("for %s {", condBody)
	g.in()

	// The fused elementwise run: each Map Each rebinds val to a fresh
	// variable; each Filter opens an `if` block that everything after it
	// (including the final append) sits inside, closed once the run is done.
	val, valType := cur, unfoldNode.In
	openBlocks := 0
	for _, s := range steps {
		lam := g.nodeLambda(s)
		body, _ := asUnsupported2(n, "lambda: ", func() (string, *ir.Type) {
			return g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: val, typ: valType}})
		})
		switch s.Prim {
		case "Map Each":
			m := g.fresh("m")
			g.wl("%s := %s", m, body)
			val, valType = m, s.Out.Elem
		case "Filter":
			g.wl("if %s {", body)
			g.in()
			openBlocks++
		}
	}
	g.wl("%s = append(%s, %s)", v, v, val)
	if earlyExit {
		g.wl("if len(%s) >= %d {", v, takeN)
		g.in()
		g.wl("break")
		g.out()
		g.wl("}")
	}
	for range openBlocks {
		g.out()
		g.wl("}")
	}

	// Bounded like the interpreter: a step that never falsifies the predicate
	// fails loudly instead of running out of memory. Checked against the raw
	// generation count, not len(v) — a filter that discards almost
	// everything must not be able to defeat the bound.
	g.wl("%s++", raw)
	g.wl("if %s > %d {", raw, maxUnfoldElements)
	g.in()
	g.wl(`dmFail("Unfold produced more than %d elements (non-terminating While:?)")`, maxUnfoldElements)
	g.out()
	g.wl("}")
	g.wl("%s = %s", cur, stepBody)
	g.out()
	g.wl("}")
	g.probeLen(n, v)
	return v
}

// maxUnfoldElements is the bound the interpreter puts on Unfold, which is the
// same bound it puts on a loop (prims.maxLoopIterations) — an unfold that
// never falsifies its predicate is a non-terminating loop that also eats
// memory. Defined in terms of dmMaxLoopIterations rather than repeated, so the
// two cannot drift apart: this constant sat at 1,000,000 after the loop
// ceiling was lifted, which failed any compiled Unfold past a million elements
// while the interpreter ran it happily.
const maxUnfoldElements = dmMaxLoopIterations
