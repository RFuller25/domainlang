package prims

import (
	"fmt"
	"slices"

	"domain/ast"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Nested pipeline bodies: a `Using:` lambda written as an indented
// sub-pipeline instead of an expression.
//
//	Cursed Technique: Map Each          # List<List<Int>> -> List<Int>
//	    Domain Expansion: All Pairs
//	        Mode: First
//	        Using: (a, b) -> a + b = 2020
//	    Maximum Technique: Product
//
// The expression layer has no higher-order builtins, so a per-element job that
// needs a *primitive* has no lambda spelling once the element is itself a
// list. A body covers that, and it is one construct rather than twenty-three:
// every primitive that takes a one-parameter `Using:` lambda reaches it through
// requireLambda, so synthesizing a lambda here — whose body is an
// ast.BlockBody, the sub-pipeline as an expression — reaches all of them
// without a single primitive knowing the form exists. The same holds
// downstream: typecheck, eval and the compiler's expression layer each grew one
// case, and every lambda-consuming emitter was left alone.
//
// The arity rule falls out of the same seam. A body turns one value into one
// value, so it can stand in for a one-parameter lambda and nothing else:
// `Fold`, `Reduce`, `Scan` and `All Pairs` take two or more parameters and are
// refused by requireLambda with their arity named.

// blockPipeline is the resolved-body half of an ast.BlockBody. It holds the
// resolver so the body can be lowered late — against the type the primitive
// binds to the lambda's first parameter, which is an element for Map Each, a
// cell for Flood Fill and a state for Explore, and is not known until then.
type blockPipeline struct {
	res   *resolver
	stmts []*ast.Statement
	prim  string
	pos   token.Position
	extra []ast.BlockBind // the lambda's other parameters, in scope inside the body

	// Memoized resolution. A Shikigami body containing a block is re-resolved
	// per call site (requireLambda runs again, building a fresh blockPipeline),
	// so one instance only ever sees one input type in practice; keeping the
	// type it was bound against makes a repeat call cheap and a contradictory
	// one an error rather than a silent re-lowering the compiler would miss.
	bound *ir.Type
	out   *ir.Type
	nodes []*ir.Node
}

// BindBlock resolves the body against its input type, memoizing the result.
func (b *blockPipeline) BindBlock(in *ir.Type) (*ir.Type, error) {
	if b.bound != nil {
		if !b.bound.Equal(in) {
			return nil, fmt.Errorf("%s: body was already resolved for input %s, cannot re-resolve for %s",
				b.pos, b.bound, in)
		}
		return b.out, nil
	}
	if in == nil {
		return nil, fmt.Errorf("%s: body has no input type", b.pos)
	}
	// scopeNested: a body is a run of ordinary stages, like a loop body. A
	// Channel inside it would have nothing to branch from, and a From:
	// consumer would read a channel whose value is not per-invocation.
	nodes, out, err := b.res.resolveSequence(b.stmts, in, scopeNested)
	if err != nil {
		return nil, fmt.Errorf("in the body: %v", err)
	}
	if out == nil {
		return nil, fmt.Errorf("%s: body produced no value", b.pos)
	}
	b.bound, b.out, b.nodes = in, out, nodes
	return out, nil
}

// RunBlock applies the resolved body to one value. The Context comes from the
// evaluation in progress (ir.EvalNode records it): a lambda body has no
// Context parameter, and a `Reveal:` or `Binding Vow` inside a body needs one.
//
// Each application is a trace frame, for the same reason a loop's laps are: a
// body runs once per element, and without a frame its steps land beside the
// stage that ran them — a hundred elements of a three-stage body reading as
// three hundred rows of the enclosing block, with the stage that produced them
// last. Framed, they nest under it and fold into one row that opens.
func (b *blockPipeline) RunBlock(v ir.Value) (ir.Value, error) {
	if b.nodes == nil {
		return nil, fmt.Errorf("%s: body was never resolved", b.pos)
	}
	ctx := ir.CurrentContext()
	if !ctx.Tracing() {
		// Nobody is watching, and this runs once per element: no frame, and no
		// deferred call to close one.
		return runBody(ctx, b.nodes, v)
	}
	// The frame is unnumbered: the primitive calls the body through the lambda
	// layer, which does not say which element this is. The recorder numbers a
	// run of same-named siblings itself, where the count is known.
	var out ir.Value
	ctx.PushFrame(b.prim+" body", b.out)
	defer func() { ctx.PopFrame(out) }()

	out, err := runBody(ctx, b.nodes, v)
	if err != nil {
		out = nil
	}
	return out, err
}

// BlockNodes is the resolved body, for the optimizer and the compiler.
func (b *blockPipeline) BlockNodes() []*ir.Node { return b.nodes }

// blockLambda synthesizes the lambda a nested body stands in for. The
// parameters are named rather than positional because typecheck and eval bind
// lambda parameters by name; nothing else ever reads these names, so they are
// spelled to be unwritable in source.
//
// It takes ambientDepth() trailing parameters like any other lambda in a For
// body, and ignores them: the body's own lambdas pick the ambient values off
// the same stacks they always do, so a `For` variable is in scope inside a body
// without being threaded through it.
func (r *resolver) blockLambda(args ArgSet, stmts []*ast.Statement, arity int, prim string, pos token.Position) (*ast.Lambda, error) {
	names, hasParams := args.Idents("Params")
	switch {
	case arity == 1 && !hasParams:
	case arity == 1 && hasParams:
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s takes a 1-parameter Using: lambda, so its body already has a name for the "+
				"only value there is — drop the Params: line", prim)}
	case !hasParams:
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s takes a %d-parameter Using: lambda, and a body computes one value from one "+
				"value — name the parameters with `Params:` so the body knows which one it is "+
				"over, or write the lambda out", prim, arity)}
	case len(names) != arity:
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s takes %d parameter(s), but Params: names %d", prim, arity, len(names))}
	}

	// A Params: name becomes a binding inside the body, so it is subject to
	// the rules a `Consider` name is: shadowing a builtin would change what a
	// call means for every expression in scope, and two parameters of one name
	// would leave the second with nothing to shadow.
	seen := map[string]bool{}
	for _, n := range names {
		if slices.Contains(typecheck.Builtins, n) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"%q is an expression builtin and cannot be used as a Params: name", n)}
		}
		if seen[n] {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Params: names %q twice", n)}
		}
		seen[n] = true
	}

	params := make([]string, arity+ambientDepth())
	for i := range params {
		params[i] = fmt.Sprintf("$block%d", i)
	}
	// The **last** declared parameter is what the body is over. For a Fold
	// written `Params: acc, row` that makes the body a pipeline over `row`
	// producing the new accumulator, which is the shape the lambda has anyway
	// — `acc` is the value carried in, not the one being transformed.
	//
	// Every declared name is also a binding, the last one included: it is the
	// body's current value *and* readable by name, so a name in a `Params:`
	// line never turns out to be decoration. That is the same thing
	// `Consider … Of Itself` does one layer up.
	inParam := params[arity-1]
	var extra []ast.BlockBind
	if hasParams {
		for i := range arity {
			extra = append(extra, ast.BlockBind{Name: names[i], Param: params[i]})
		}
	}
	return &ast.Lambda{
		Params: params,
		Pos:    pos,
		Body: &ast.BlockBody{
			Param: inParam,
			Extra: extra,
			Stmts: stmts,
			Pipe:  &blockPipeline{res: r, stmts: stmts, prim: prim, pos: pos, extra: extra},
			Pos:   pos,
		},
	}, nil
}
