package codegen

import (
	"bytes"
	"fmt"
	"maps"
	"slices"

	"domain/ast"
	"domain/ir"
)

// A `Using:` written as an indented pipeline body (ast.BlockBody) compiles to
// a top-level Go function, and the expression that stands in for the lambda
// body is a call to it.
//
// That indirection is what makes the form generic on this side. A body is
// statements, and the forty-nine emitters that compile a lambda all want an
// *expression* — many of them build it before opening the loop it is used
// inside, so emitting statements at that moment would put them in the wrong
// place or the wrong scope. Lowering the body to a function once and handing
// back `dmBlockN(x)` gives every one of those sites something it can already
// handle, with no emitter changed and no ordering hazard.
//
// The cost is a call per invocation where the old special-cased Map Each
// inlined the body. Go inlines small ones back, and correctness across every
// lambda-taking primitive is worth more than a hand-inlined loop for one.

// emitBlockCall compiles a block body to a top-level function (once) and
// returns a Go expression applying it to arg.
func (g *gen) emitBlockCall(bb *ast.BlockBody, arg string, argType *ir.Type) (string, *ir.Type, error) {
	nodes := bb.Pipe.BlockNodes()
	if nodes == nil {
		return "", nil, fmt.Errorf("block body was never resolved")
	}
	if argType == nil {
		return "", nil, fmt.Errorf("block body has no input type")
	}
	outType, err := bb.Pipe.BindBlock(argType)
	if err != nil {
		return "", nil, err
	}
	// The bindings this body writes to travel as pointers rather than values,
	// so a `:=` inside it lands on the binding itself — which is what the
	// interpreter's one shared binding stack does. Everything else is still
	// passed by value, so a body that writes nothing emits the Go it always
	// emitted.
	writes := blockUpdates(nodes, g.bindNames)
	fn, err := g.blockFunc(bb, nodes, argType, outType, writes)
	if err != nil {
		return "", nil, err
	}
	// Enclosing For loops' variables and the `Consider` bindings in scope are
	// locals of main, out of scope in a top-level function, so they are passed
	// in — see blockFunc.
	call := fn + "(" + arg
	for _, a := range g.ambient {
		call += ", " + a.v
	}
	for _, name := range g.bindOrder() {
		b := g.bindNames[name]
		if !writes[name] {
			call += ", " + b.expr
			continue
		}
		if b.cell == "" {
			// A binding with no cell is not a variable this package owns, so
			// there is nothing to point at. The resolver refuses a write to
			// every such name, so this is a backstop rather than a path.
			return "", nil, fmt.Errorf("this Using: is an indented pipeline that updates %q "+
				"with `:=`, which is not a binding this backend can address", name)
		}
		call += ", " + b.cell
	}
	return call + ")", outType, nil
}

// blockFunc emits the function for one block body and returns its name.
// Memoized per BlockBody, so a body used by a node the compiler visits twice
// (a fusion that recompiles a lambda, say) is emitted once.
func (g *gen) blockFunc(bb *ast.BlockBody, nodes []*ir.Node, in, out *ir.Type, writes map[string]bool) (string, error) {
	if name, ok := g.blocks[bb]; ok {
		return name, nil
	}
	inGo, err := g.goType(in)
	if err != nil {
		return "", err
	}
	outGo, err := g.goType(out)
	if err != nil {
		return "", err
	}
	name := g.fresh("dmBlock")
	param := g.fresh("bv")

	// The body is emitted into a buffer of its own: g.main is the statements of
	// func main(), and everything the body writes belongs in the function
	// instead. Indentation restarts at the function's body level.
	savedMain, savedIndent := g.main, g.indent
	g.main, g.indent = bytes.Buffer{}, 0

	// Ambient For variables become extra parameters, rebound for the duration
	// so the body's own lambdas compile against the parameter names rather
	// than main's locals.
	savedAmbient := g.ambient
	sig := inGo
	rebound := make([]ambientVar, len(g.ambient))
	for i, a := range savedAmbient {
		p := g.fresh("ba")
		ambGo, terr := g.goType(a.typ)
		if terr != nil {
			g.main, g.indent, g.ambient = savedMain, savedIndent, savedAmbient
			return "", terr
		}
		sig += fmt.Sprintf(", %s %s", p, ambGo)
		rebound[i] = ambientVar{v: p, typ: a.typ}
	}
	g.ambient = rebound

	// The bindings in scope travel the same way, and for the same reason: a
	// lambda inside the body may read one, and the local holding it belongs to
	// the function the body was written in. One in `writes` travels as a
	// pointer instead, so that the body's reads and its writes both reach the
	// caller's variable — the interpreter's bindings are one shared stack, and
	// a copy here would silently disagree with it.
	savedBinds := g.bindNames
	reboundBinds := make(exprEnv, len(savedBinds))
	for _, bname := range g.bindOrder() {
		b := savedBinds[bname]
		p := g.fresh("bb")
		bindGo, terr := g.goType(b.typ)
		if terr != nil {
			g.main, g.indent, g.ambient = savedMain, savedIndent, savedAmbient
			return "", terr
		}
		if writes[bname] {
			sig += fmt.Sprintf(", %s *%s", p, bindGo)
			// The deref is the read *and* the write target, and it is already
			// parenthesized, so it composes wherever a plain variable did.
			reboundBinds[bname] = exprBinding{expr: "(*" + p + ")", typ: b.typ, cell: p}
			continue
		}
		sig += fmt.Sprintf(", %s %s", p, bindGo)
		reboundBinds[bname] = exprBinding{expr: p, typ: b.typ}
	}
	g.bindNames = reboundBinds

	cur, err := g.emitSequence(nodes, param)
	body := g.main.String()

	g.main, g.indent, g.ambient, g.bindNames = savedMain, savedIndent, savedAmbient, savedBinds
	if err != nil {
		return "", err
	}

	var decl bytes.Buffer
	fmt.Fprintf(&decl, "func %s(%s %s) %s {\n", name, param, sig, outGo)
	decl.WriteString(body)
	// A body whose stages all pass the value through (a lone Binding Vow, say)
	// returns the parameter itself, which is already the right expression.
	fmt.Fprintf(&decl, "\treturn %s\n}\n", cur)

	g.decls = append(g.decls, decl.String())
	if g.blocks == nil {
		g.blocks = map[*ast.BlockBody]string{}
	}
	g.blocks[bb] = name
	return name, nil
}

// blockUpdates is the set of enclosing bindings a block body writes to — the
// ones that have to reach its function as pointers rather than copies. A
// binding the body opens *itself* is not one of them: it is a local of the
// block's own function, so a write to it already lands where its readers look,
// and it is not in outer.
//
// A nested body is walked through its resolved nodes rather than through
// ast.UpdatedNames, which stops at a BlockBody because `:=` is an
// expression-layer operator and a sub-pipeline's statements are not
// expressions. Missing one would not miscompile — the inner call would find no
// cell for the name and stop — but it would refuse a program this now
// compiles, so the walk goes all the way down.
func blockUpdates(nodes []*ir.Node, outer exprEnv) map[string]bool {
	if len(outer) == 0 {
		return nil
	}
	found := map[string]bool{}
	var walk func([]*ir.Node)
	walk = func(list []*ir.Node) {
		for _, n := range list {
			if lam, _ := n.Meta["lambda"].(*ast.Lambda); lam != nil {
				if bb, ok := lam.Body.(*ast.BlockBody); ok {
					walk(bb.Pipe.BlockNodes())
				} else {
					names := map[string]bool{}
					ast.UpdatedNames(lam.Body, names)
					for _, p := range lam.Params {
						delete(names, p)
					}
					for name := range names {
						if _, ok := outer[name]; ok {
							found[name] = true
						}
					}
				}
			}
			if sub, _ := n.Meta["nodes"].([]*ir.Node); sub != nil {
				walk(sub)
			}
			if subs, _ := n.Meta[ir.MetaBindNodes].([][]*ir.Node); subs != nil {
				for _, s := range subs {
					walk(s)
				}
			}
		}
	}
	walk(nodes)
	if len(found) == 0 {
		return nil
	}
	return found
}

// bindOrder is the names of the `Consider` bindings in scope, in a fixed order
// so that the parameters a block function declares and the arguments its call
// passes line up. Map iteration order would not: the same body is emitted once
// and called from wherever it appears.
func (g *gen) bindOrder() []string {
	if len(g.bindNames) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(g.bindNames))
}
