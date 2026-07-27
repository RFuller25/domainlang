package codegen

import (
	"domain/ast"
	"domain/ir"
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

func (g *gen) emitPrefixWhile(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	cut, i := g.fresh("cut"), g.fresh("i")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: in + "[" + i + "]", typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
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
		return v, nil
	}
	// The suffix may alias — appending past the end of the original slice
	// reallocates rather than overwriting it.
	g.wl("%s := %s[%s:]", v, in, cut)
	return v, nil
}

func (g *gen) emitChunk(n *ir.Node, in string) (string, error) {
	size, _ := n.Meta["size"].(int64)
	if size < 1 {
		return "", unsupported(n, "missing chunk metadata")
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, i, end := g.fresh("v"), g.fresh("i"), g.fresh("end")
	g.wl("%s := make([][]%s, 0, (len(%s)+%d)/%d)", v, elemGo, in, size-1, size)
	g.wl("for %s := 0; %s < len(%s); %s += %d {", i, i, in, i, size)
	g.in()
	g.wl("%s := %s + %d", end, i, size)
	g.wl("if %s > len(%s) {", end, in)
	g.in()
	g.wl("%s = len(%s)", end, in) // the short final block is kept, not dropped
	g.out()
	g.wl("}")
	g.wl("%s = append(%s, append([]%s{}, %s[%s:%s]...))", v, v, elemGo, in, i, end)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitPartition(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	yes, no, i := g.fresh("yes"), g.fresh("no"), g.fresh("i")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: in + "[" + i + "]", typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := []%s{}", yes, elemGo)
	g.wl("%s := []%s{}", no, elemGo)
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
	return v, nil
}

func (g *gen) emitIterate(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	steps, _ := n.Meta["n"].(int64)
	if steps < 0 {
		return "", unsupported(n, "missing iterate metadata")
	}
	elemGo, err := g.goType(n.In)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, cur, i := g.fresh("v"), g.fresh("cur"), g.fresh("i")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: cur, typ: n.In}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := make([]%s, %d)", v, elemGo, steps)
	g.wl("%s := %s", cur, in)
	g.wl("for %s := 0; %s < %d; %s++ {", i, i, steps, i)
	g.in()
	g.wl("%s = %s", cur, body)
	g.wl("%s[%s] = %s", v, i, cur)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitUnfold(n *ir.Node, in string) (string, error) {
	step, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	cond, _ := n.Meta["while"].(*ast.Lambda)
	if cond == nil || len(cond.Params) != 1 {
		return "", unsupported(n, "missing While: predicate")
	}
	elemGo, err := g.goType(n.In)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, cur := g.fresh("v"), g.fresh("cur")
	env := exprEnv{cond.Params[0]: {expr: cur, typ: n.In}}
	condBody, _, err := g.compileExpr(cond.Body, env)
	if err != nil {
		return "", unsupported(n, "While: lambda: %v", err)
	}
	stepBody, _, err := g.compileExpr(step.Body, exprEnv{step.Params[0]: {expr: cur, typ: n.In}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.wl("%s := []%s{}", v, elemGo)
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
	return v, nil
}

// maxUnfoldElements mirrors prims.maxLoopIterations, the bound the interpreter
// puts on Unfold. The two are separate constants because the interpreter's is
// a var that tests lower; the compiled bound is baked into the binary.
const maxUnfoldElements = 1_000_000
