package codegen

import (
	"fmt"

	"domain/ir"
)

// Simple Domain loops. The body is a resolved node list (stashed in Meta by
// prims/control.go); it is emitted once inside a Go loop that threads a
// single mutable variable. Bodies preserve the value type by construction,
// so the reassignment always typechecks.

// dmMaxLoopIterations mirrors prims.maxLoopIterations' default: zero, meaning
// unlimited. The guard is emitted only when this is positive, so a compiled
// binary carries no iteration counter at all by default — and, like the
// interpreter, never refuses a long-running but correct loop.
const dmMaxLoopIterations = 0

// comparableScalarElem reports whether t is a list whose element is a scalar
// comparable with Go's != (so convergence can be detected inline).
func comparableScalarElem(t *ir.Type) bool {
	if t == nil || t.Kind != ir.KList || t.Elem == nil {
		return false
	}
	switch t.Elem.Kind {
	case ir.KInt, ir.KFloat, ir.KText, ir.KBool:
		return true
	}
	return false
}

// emitFixedPointMapLoop emits one iteration of a Fixed-Point loop whose body is
// a single Map Each over a comparable-scalar list: it builds the next slice and
// tracks whether any element changed in the same pass, then converges (commit +
// break) when nothing changed — identical semantics to the general
// map-then-deep-equal path, with half the per-iteration element visits. `it` is
// the enclosing loop counter and `v` the loop-carried variable.
func (g *gen) emitFixedPointMapLoop(mapNode *ir.Node, v, it string) error {
	lam, err := g.nodeLambda(mapNode)
	if err != nil {
		return err
	}
	elemGo, err := g.goType(mapNode.Out.Elem)
	if err != nil {
		return unsupported(mapNode, "%v", err)
	}
	next, i, e, nv := g.fresh("next"), g.fresh("i"), g.fresh("e"), g.fresh("nv")
	changed := g.fresh("changed")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: mapNode.In.Elem}})
	if err != nil {
		return unsupported(mapNode, "lambda: %v", err)
	}
	g.wl("%s := make([]%s, len(%s))", next, elemGo, v)
	g.wl("%s := false", changed)
	g.wl("for %s, %s := range %s {", i, e, v)
	g.in()
	g.wl("%s := %s", nv, body)
	g.wl("%s[%s] = %s", next, i, nv)
	g.wl("if %s != %s[%s] { %s = true }", nv, v, i, changed)
	g.out()
	g.wl("}")
	g.wl("if !%s {", changed)
	g.in()
	g.wl("%s = %s", v, next)
	g.wl("break")
	g.out()
	g.wl("}")
	g.emitIterationGuard(it, "did not converge")
	g.wl("%s = %s", v, next)
	return nil
}

// emitIterationGuard emits the runaway-loop check, and only when a bound is
// configured. At the default of zero the binary carries no counter comparison
// at all — a correct but long-running loop is never refused.
func (g *gen) emitIterationGuard(it, what string) {
	if dmMaxLoopIterations <= 0 {
		return
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.wl("if %s >= %d {", it, dmMaxLoopIterations)
	g.in()
	g.wl("dmFail(%s)", goStr(fmt.Sprintf("%s within %d iterations", what, dmMaxLoopIterations)))
	g.out()
	g.wl("}")
}

func (g *gen) emitLoop(n *ir.Node, in string) (string, error) {
	kind, _ := n.Meta["kind"].(string)
	body, _ := n.Meta["nodes"].([]*ir.Node)
	if body == nil {
		return "", unsupported(n, "missing loop body metadata")
	}

	v := g.fresh("v")
	g.wl("%s := %s", v, in)

	emitBody := func() error {
		cur, err := g.emitSequence(body, v)
		if err != nil {
			return err
		}
		g.wl("%s = %s", v, cur)
		return nil
	}

	switch kind {
	case "repeat":
		count, _ := n.Meta["n"].(int64)
		i := g.fresh("i")
		g.wl("for %s := int64(0); %s < %d; %s++ {", i, i, count, i)
		g.in()
		if err := emitBody(); err != nil {
			return "", err
		}
		g.out()
		g.wl("}")
		return v, nil

	case "while":
		lam, err := g.nodeLambda(n)
		if err != nil {
			return "", err
		}
		pred, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: v, typ: n.In}})
		if err != nil {
			return "", unsupported(n, "predicate: %v", err)
		}
		g.helper("dmFail", declFail, "fmt", "os")
		it := g.fresh("it")
		g.wl("for %s := 0; ; %s++ {", it, it)
		g.in()
		g.wl("if !%s {", pred)
		g.in()
		g.wl("break")
		g.out()
		g.wl("}")
		g.emitIterationGuard(it, "loop exceeded")
		if err := emitBody(); err != nil {
			return "", err
		}
		g.out()
		g.wl("}")
		return v, nil

	case "fixedpoint":
		g.helper("dmFail", declFail, "fmt", "os")
		it := g.fresh("it")
		g.wl("for %s := 0; ; %s++ {", it, it)
		g.in()
		// Fast path: a body that is a single Map Each over a comparable-scalar
		// list detects convergence *during* the map (the loop already visits
		// every element), avoiding a second full structural-equality pass.
		if len(body) == 1 && body[0].Prim == "Map Each" && comparableScalarElem(body[0].Out) {
			if err := g.emitFixedPointMapLoop(body[0], v, it); err != nil {
				return "", err
			}
			g.out()
			g.wl("}")
			return v, nil
		}
		// Run the body against v, but keep its result in a fresh name so we
		// can compare old and new before committing.
		cur, err := g.emitSequence(body, v)
		if err != nil {
			return "", err
		}
		if cur == v {
			// A body of pure passthroughs (vows/emits) is already converged
			// after one run, exactly like the interpreter's DeepEqual(v, v).
			g.wl("break")
		} else {
			// Mirror the interpreter's order: converged? then iteration
			// guard, then commit the new value and go around again.
			eq, err := g.eqExpr(cur, v, n.In)
			if err != nil {
				return "", unsupported(n, "convergence test: %v", err)
			}
			g.wl("if %s {", eq)
			g.in()
			g.wl("%s = %s", v, cur)
			g.wl("break")
			g.out()
			g.wl("}")
			g.emitIterationGuard(it, "did not converge")
			g.wl("%s = %s", v, cur)
		}
		g.out()
		g.wl("}")
		return v, nil

	case "for":
		// `For x in <source>` iterates a channel's list (or an inline
		// range(N)), binding each element as an ambient trailing parameter on
		// every Using: lambda in the body. The loop itself is an ordinary Go
		// range; the binding is what needs care, and lives in g.ambient.
		elemT, _ := n.Meta["elem"].(*ir.Type)
		if elemT == nil {
			return "", unsupported(n, "For loop is missing its element type")
		}
		x := g.fresh("x")
		isRange, _ := n.Meta["isRange"].(bool)
		if isRange {
			count, _ := n.Meta["rangeN"].(int64)
			g.wl("for %s := int64(0); %s < %d; %s++ {", x, x, count, x)
		} else {
			name, _ := n.Meta["channel"].(string)
			cv, ok := g.chans[name]
			if !ok {
				return "", unsupported(n, "For source channel %q has no value", name)
			}
			g.wl("for _, %s := range %s {", x, cv.v)
		}
		g.in()
		// Push before emitting the body so the body's lambdas can see it, and
		// pop after — nested For loops stack outermost-first, exactly like the
		// interpreter's ambient stack.
		g.ambient = append(g.ambient, ambientVar{v: x, typ: elemT})
		err := emitBody()
		g.ambient = g.ambient[:len(g.ambient)-1]
		g.ambientNames = nil
		if err != nil {
			return "", err
		}
		g.out()
		g.wl("}")
		return v, nil

	default:
		return "", unsupported(n, "loop kind %q", kind)
	}
}
