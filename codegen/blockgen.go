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
	fn, err := g.blockFunc(bb, nodes, argType, outType)
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
		call += ", " + g.bindNames[name].expr
	}
	return call + ")", outType, nil
}

// blockFunc emits the function for one block body and returns its name.
// Memoized per BlockBody, so a body used by a node the compiler visits twice
// (a fusion that recompiles a lambda, say) is emitted once.
func (g *gen) blockFunc(bb *ast.BlockBody, nodes []*ir.Node, in, out *ir.Type) (string, error) {
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
	// the function the body was written in.
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
