package codegen

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/ir"
)

// Simple Domain loops. The body is a resolved node list (stashed in Meta by
// prims/control.go); it is emitted once inside a Go loop that threads a
// single mutable variable. Bodies preserve the value type by construction,
// so the reassignment always typechecks.

// dmMaxLoopIterations mirrors prims.maxLoopIterations' default, and must keep
// mirroring it: a program that runs interpreted and dies compiled (or the
// reverse) is the one failure mode a shared ceiling exists to prevent. The
// guard is emitted only when this is positive, so setting it to zero strips
// the counter comparison out of the binary entirely.
const dmMaxLoopIterations = 1_000_000_000

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
// configured. At a bound of zero the binary carries no counter comparison at
// all.
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
	// A loop whose body writes into the state in place takes its own copy of
	// it first — prims.ownLoopState is the interpreter's half of this, and the
	// two have to agree about which programs make the copy. See
	// optimizer/linear.go for what the analysis does and does not prove.
	g.wl("%s := %s", v, g.ownLoopState(n, body, in))

	emitBody := func() error {
		cur, err := g.emitSequence(body, v)
		if err != nil {
			return err
		}
		// A body of pure passthroughs gives back the variable it was handed,
		// and `v = v` is noise in the emitted source — worth skipping now that
		// a loop body of nothing but `Cursed Tool` writes is an ordinary shape
		// rather than the curiosity an all-vows body was.
		if cur != v {
			g.wl("%s = %s", v, cur)
		}
		return nil
	}

	switch kind {
	case "repeat":
		// Measured before the loop opens: the count is a property of the value
		// entering it, not of each lap's value.
		count, err := g.measuredOperand(n, v, "n", "Times", 0)
		if err != nil {
			return "", err
		}
		i := g.fresh("i")
		g.wl("for %s := int64(0); %s < %s; %s++ {", i, i, count, i)
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

// ownLoopState is the expression a loop seeds its state from: the value it was
// handed, or a copy of it when the optimizer marked an update in the body as
// in-place.
//
// The copy reaches exactly the fields the optimizer says a marked update writes
// through, which is the promise ownableLoopState makes on the analysis side.
//
// Exactly those, and no more. Copying the whole state instead is correct and was
// badly wrong in cost: day 6 of the AoC suite writes a sixteen-element list in an
// inner loop and carries a map beside it that grows to twelve thousand entries,
// so owning the state wholesale cloned that map once per lap of the *outer*
// loop — reintroducing one level up the quadratic this pass exists to remove,
// and making the program 1.8x slower than before the pass could see it at all.
func (g *gen) ownLoopState(n *ir.Node, body []*ir.Node, in string) string {
	if len(body) == 0 || n.In == nil {
		return in
	}
	// Any stage may carry the marks: the pass looks at every body stage that
	// threads the state, not only the first.
	if !anyStageInPlace(body) {
		return in
	}
	owned, err := g.ownValueExpr(in, n.In, "", ownedFields(n))
	if err != nil {
		return in
	}
	return owned
}

// anyStageInPlace reports whether some body stage carries a marked update.
func anyStageInPlace(body []*ir.Node) bool {
	for _, stage := range body {
		if lam, _ := stage.Meta["lambda"].(*ast.Lambda); lam != nil && ast.HasInPlace(lam.Body) {
			return true
		}
	}
	return false
}

// ownedFields reads the paths the optimizer recorded for this loop. A node
// without them predates the record or came from a caller that does not set it,
// and owning everything is the reading that is never wrong.
func ownedFields(n *ir.Node) map[string]bool {
	paths, ok := n.Meta[ir.OwnedFields].([]string)
	if !ok {
		return map[string]bool{ir.OwnsEverything: true}
	}
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[p] = true
	}
	return out
}

// needsOwn reports whether the storage at path has to be copied: because a
// marked update writes it, writes something inside it, or writes a container it
// sits in.
func needsOwn(path string, owned map[string]bool) bool {
	for w := range owned {
		if w == path || pathAncestor(w, path) || pathAncestor(path, w) {
			return true
		}
	}
	return false
}

// pathAncestor reports whether a is a strict ancestor of b. The root path is an
// ancestor of everything, which is the case a plain HasPrefix gets wrong: the
// root is "", so "" + "." is "." and no real path begins with a dot.
func pathAncestor(a, b string) bool {
	if a == b {
		return false
	}
	if a == ir.OwnsEverything {
		return true
	}
	return strings.HasPrefix(b, a+".")
}

// fieldPath extends a projection path with one tuple index.
func fieldPath(path string, i int) string {
	if path == "" {
		return itoa(i)
	}
	return path + "." + itoa(i)
}

// ownValueExpr is the Go expression copying the storage inside a value that a
// marked update reaches, leaving the rest of it shared.
func (g *gen) ownValueExpr(expr string, t *ir.Type, path string, owned map[string]bool) (string, error) {
	if !needsOwn(path, owned) {
		return expr, nil
	}
	switch t.Kind {
	case ir.KList:
		elemGo, err := g.goType(t.Elem)
		if err != nil {
			return "", err
		}
		return "append([]" + elemGo + "(nil), " + expr + "...)", nil
	case ir.KTuple:
		tupGo, err := g.goType(t)
		if err != nil {
			return "", err
		}
		out := tupGo + "{"
		// Elems, not Fields: Fields is the record's and is empty for a tuple,
		// which emits `Tup1{}` — a zero value, and a program that fails with
		// "index 0 out of range (length 0)" on its first lap.
		for i, ft := range t.Elems {
			field := "(" + expr + ").f" + itoa(i)
			copied := field
			if ft != nil && ownableField(ft) {
				if copied, err = g.ownValueExpr(field, ft, fieldPath(path, i), owned); err != nil {
					return "", err
				}
			}
			if i > 0 {
				out += ", "
			}
			out += copied
		}
		return out + "}", nil

	// The rest of the collections a mark can be rooted at through a tuple
	// field. optimizer.projectedCollection lets an update write into any of
	// these when it sits in loop state, so each one has to get storage of its
	// own here — a Map copied by reference into the state tuple would have the
	// loop writing through to whatever the caller still holds. Every clone is
	// one already declared for the functional path.
	case ir.KMap:
		g.helper("dmMap", declMap)
		g.helper("dmMapClone", declMapClone)
		return "dmMapClone(" + expr + ")", nil
	case ir.KSet:
		g.helper("dmSet", declSet)
		g.helper("dmSetClone", declSetClone)
		return "dmSetClone(" + expr + ")", nil
	case ir.KGrid:
		g.helper("dmGrid", declGrid)
		g.helper("dmGridClone", declGridClone)
		return "dmGridClone(" + expr + ")", nil
	case ir.KSparse:
		g.helper("dmSparse", declSparse, "slices")
		g.helper("dmSparseClone", declSparseClone)
		return "dmSparseClone(" + expr + ")", nil
	case ir.KGraph:
		// dmGraph carries its own clone method — the one the functional
		// addnode/addedge call before mutating — so there is no free function
		// to declare here.
		g.helper("dmGraph", declGraph)
		return "(" + expr + ").clone()", nil
	}
	return expr, nil
}

// ownableField reports whether a state field holds storage ownValueExpr can
// copy. It mirrors optimizer.ownableLoopState's field test: what the analysis
// allows a mark to reach, the clone on entry has to be able to own. Whether it
// *does* is needsOwn's question.
func ownableField(t *ir.Type) bool {
	switch t.Kind {
	case ir.KList, ir.KTuple, ir.KMap, ir.KSet, ir.KGrid, ir.KSparse, ir.KGraph:
		return true
	}
	return false
}
