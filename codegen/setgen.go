package codegen

import (
	"domain/ir"
)

// Map/Set-producing reductions. The emitted dmMap/dmSet types preserve
// insertion order (see runtime.go), and each lowering below reproduces the
// exact ordering of its ir counterpart: Group By keys in first-seen order,
// Intersect in the accumulator's order, Union as left-then-right, Difference
// in the left channel's order.

func (g *gen) emitGroupBy(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	keyGo, err := g.goType(n.Out.Key)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	valGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	m, e, k := g.fresh("m"), g.fresh("e"), g.fresh("k")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmMap", declMap)
	g.helper("dmAppend", declMapAppend)
	g.wl("%s := dmNewMap[%s, %s]()", m, keyGo, valGo)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.wl("%s := %s", k, body)
	g.wl("dmAppend(&%s, %s, %s)", m, k, e)
	g.out()
	g.wl("}")
	return m, nil
}

// emitSplitEachIntersect lowers Split Each("") + Intersect over []string lines
// into a running character-set intersection that never builds the [][]string of
// one-rune substrings nor a per-line hash set. The accumulator is seeded from
// the first line's runes (decoded exactly as strings.Split(line,"") would) and,
// for each subsequent line, filtered by membership: a [256]bool byte table when
// every accumulator element is a single ASCII byte (the common case, exact
// because an ASCII byte is always a standalone rune), else an exact per-line
// rune set. Accumulator order — and thus the rendered set — is preserved.
func (g *gen) emitSplitEachIntersect(in string) string {
	g.helper("dmSet", declSet)
	g.imp("unicode/utf8")
	seed := g.fresh("seed")
	line0, p0, sz0 := g.fresh("line"), g.fresh("p"), g.fresh("sz")
	cur := g.fresh("cur")
	present := g.fresh("present")
	gi, line := g.fresh("gi"), g.fresh("line")
	allB, e := g.fresh("allB"), g.fresh("e")
	w, i := g.fresh("w"), g.fresh("i")
	ls, p, sz := g.fresh("ls"), g.fresh("p"), g.fresh("sz")
	out, e2 := g.fresh("out"), g.fresh("e")

	// Seed the accumulator (deduped, first-line order) once via a dmSet.
	g.wl("%s := dmNewSet[string]()", seed)
	g.wl("if len(%s) > 0 {", in)
	g.in()
	g.wl("%s := %s[0]", line0, in)
	g.wl("for %s := 0; %s < len(%s); {", p0, p0, line0)
	g.in()
	g.wl("_, %s := utf8.DecodeRuneInString(%s[%s:])", sz0, line0, p0)
	g.wl("%s.add(%s[%s:%s+%s])", seed, line0, p0, p0, sz0)
	g.wl("%s += %s", p0, sz0)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	// cur holds the surviving elements in order; it is filtered in place each
	// line, so the running intersection allocates nothing per line.
	g.wl("%s := append([]string(nil), %s.elems...)", cur, seed)
	g.wl("var %s [256]bool", present)
	g.wl("for %s := 1; %s < len(%s); %s++ {", gi, gi, in, gi)
	g.in()
	g.wl("if len(%s) == 0 { break }", cur)
	g.wl("%s := %s[%s]", line, in, gi)
	g.wl("%s := true", allB)
	g.wl("for _, %s := range %s {", e, cur)
	g.in()
	g.wl("if len(%s) != 1 || %s[0] >= 0x80 { %s = false; break }", e, e, allB)
	g.out()
	g.wl("}")
	g.wl("%s := 0", w)
	g.wl("if %s {", allB)
	g.in()
	g.wl("for %s := range %s { %s[%s] = false }", i, present, present, i)
	g.wl("for %s := 0; %s < len(%s); %s++ { %s[%s[%s]] = true }", i, i, line, i, present, line, i)
	g.wl("for _, %s := range %s {", e, cur)
	g.in()
	g.wl("if %s[%s[0]] { %s[%s] = %s; %s++ }", present, e, cur, w, e, w)
	g.out()
	g.wl("}")
	g.out()
	g.wl("} else {")
	g.in()
	g.wl("%s := dmNewSet[string]()", ls)
	g.wl("for %s := 0; %s < len(%s); {", p, p, line)
	g.in()
	g.wl("_, %s := utf8.DecodeRuneInString(%s[%s:])", sz, line, p)
	g.wl("%s.add(%s[%s:%s+%s])", ls, line, p, p, sz)
	g.wl("%s += %s", p, sz)
	g.out()
	g.wl("}")
	g.wl("for _, %s := range %s {", e2, cur)
	g.in()
	g.wl("if %s.contains(%s) { %s[%s] = %s; %s++ }", ls, e2, cur, w, e2, w)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	g.wl("%s = %s[:%s]", cur, cur, w)
	g.out()
	g.wl("}")
	// Rebuild the ordered set for rendering.
	g.wl("%s := dmNewSet[string]()", out)
	g.wl("for _, %s := range %s { %s.add(%s) }", e2, cur, out, e2)
	return out
}

// emitSetReduce lowers Intersect/Union: List<List<T>> -> Set<T>, seeded with
// the first group and combined pairwise, exactly like the interpreter's fold
// over ir.SetIntersect / ir.SetUnion.
func (g *gen) emitSetReduce(n *ir.Node, in string) (string, error) {
	elemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	acc := g.fresh("acc")
	g.helper("dmSet", declSet)
	g.wl("%s := dmNewSet[%s]()", acc, elemGo)
	g.wl("if len(%s) > 0 {", in)
	g.in()
	e0 := g.fresh("e")
	g.wl("for _, %s := range %s[0] {", e0, in)
	g.in()
	g.wl("%s.add(%s)", acc, e0)
	g.out()
	g.wl("}")
	grp := g.fresh("g")
	g.wl("for _, %s := range %s[1:] {", grp, in)
	g.in()
	if n.Prim == "Intersect" {
		// Intersect needs per-group membership: build the group's set, then keep
		// only accumulator elements present in it.
		s, e1 := g.fresh("s"), g.fresh("e")
		g.wl("%s := dmNewSet[%s]()", s, elemGo)
		g.wl("for _, %s := range %s {", e1, grp)
		g.in()
		g.wl("%s.add(%s)", s, e1)
		g.out()
		g.wl("}")
		e2, next := g.fresh("e"), g.fresh("next")
		g.wl("%s := dmNewSet[%s]()", next, elemGo)
		g.wl("for _, %s := range %s.elems {", e2, acc)
		g.in()
		g.wl("if %s.contains(%s) {", s, e2)
		g.in()
		g.wl("%s.add(%s)", next, e2)
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
		g.wl("%s = %s", acc, next)
	} else { // Union
		// Adding each group element straight into the accumulator is identical
		// to unioning a per-group set: acc.add already dedups, and each element's
		// first-occurrence position (which fixes insertion order) is unchanged.
		// No per-group set allocation.
		e1 := g.fresh("e")
		g.wl("for _, %s := range %s {", e1, grp)
		g.in()
		g.wl("%s.add(%s)", acc, e1)
		g.out()
		g.wl("}")
	}
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return acc, nil
}

// emitDifference lowers the Channel consumer a - b over Set-or-List channels.
func (g *gen) emitDifference(n *ir.Node, in string) (string, error) {
	froms, _ := n.Meta["from"].([]string)
	if len(froms) != 2 {
		return "", unsupported(n, "missing channel metadata")
	}
	elemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmSet", declSet)
	sa, err := g.channelAsSet(n, froms[0], elemGo)
	if err != nil {
		return "", err
	}
	sb, err := g.channelAsSet(n, froms[1], elemGo)
	if err != nil {
		return "", err
	}
	v, e := g.fresh("v"), g.fresh("e")
	g.wl("%s := dmNewSet[%s]()", v, elemGo)
	g.wl("for _, %s := range %s.elems {", e, sa)
	g.in()
	g.wl("if !%s.contains(%s) {", sb, e)
	g.in()
	g.wl("%s.add(%s)", v, e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

// channelAsSet returns a dmSet variable for a channel, converting a List
// channel via insertion-order dedup (ir.SetFromList's semantics).
func (g *gen) channelAsSet(n *ir.Node, name, elemGo string) (string, error) {
	cv, ok := g.chans[name]
	if !ok {
		return "", unsupported(n, "channel %q was not compiled", name)
	}
	switch cv.typ.Kind {
	case ir.KSet:
		return cv.v, nil
	case ir.KList:
		s, e := g.fresh("s"), g.fresh("e")
		g.wl("%s := dmNewSet[%s]()", s, elemGo)
		g.wl("for _, %s := range %s {", e, cv.v)
		g.in()
		g.wl("%s.add(%s)", s, e)
		g.out()
		g.wl("}")
		return s, nil
	default:
		return "", unsupported(n, "channel %q is %s, want Set or List", name, cv.typ)
	}
}
