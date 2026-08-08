package ast

import (
	"domain/ir"
	"domain/token"
)

// A lambda body written as an indented sub-pipeline.
//
// `Using:` lambdas are the expression layer, which cannot iterate: there are
// no higher-order builtins, so a per-element job that needs a *primitive* —
// search the element, sort it, fold across it — has no lambda spelling at all
// once the element is itself a list. Writing the body as a pipeline instead
// covers that, and the two forms have the same signature: a lambda `(x) -> e`
// and a sub-pipeline both turn one value into one value.
//
// Making it an Expr is what makes it general. Every lambda-consuming
// primitive reaches its lambda through prims.requireLambda and then asks
// typecheck.LambdaType for the result type and eval for the value; every
// compiler emitter reaches it through nodeLambda and compiles the body with
// compileExpr. A block that *is* a lambda body therefore travels all of those
// paths unchanged, and no primitive needs to know it exists — which is the
// difference between one construct and twenty-three special cases.
//
// The parser never builds one: it parses an indented run of statements into
// Statement.Block, and the resolver synthesizes the lambda when the statement's
// primitive turns out to want one.

// BlockPipeline is the resolved-body side of a BlockBody, implemented in
// package prims (which owns the vocabulary and the resolver). The interface
// keeps ast free of the pipeline layer's machinery: this package holds the
// syntax node, prims holds what it means.
type BlockPipeline interface {
	// BindBlock resolves the body against the type its input is bound to and
	// returns the body's result type. Which type that is depends on the
	// primitive — an element for Map Each, a cell for Flood Fill, a state for
	// Explore — so it is not known until the lambda's parameter is bound.
	// Callers may repeat it; the resolution is memoized.
	BindBlock(in *ir.Type) (*ir.Type, error)
	// RunBlock applies the resolved body to one value.
	RunBlock(in ir.Value) (ir.Value, error)
	// BlockNodes is the resolved body, for the optimizer (which rewrites nodes
	// inside it in place) and the compiler (which emits it). Nil before
	// BindBlock has run.
	BlockNodes() []*ir.Node
}

// BlockBind is one of a block body's extra parameters: the user's name for it,
// and the synthesized lambda parameter it takes its value from.
//
// A body computes one value from one value, so a lambda of two or more
// parameters has exactly one it can be the body *of* — and the rest have to
// arrive some other way. They arrive as bindings, which is the mechanism
// `Consider` already uses: typecheck pushes their types, eval pushes their
// values, and the compiler threads them into the block's function beside the
// bindings that were already in scope. Nothing downstream learns a new shape.
type BlockBind struct {
	Name  string // what the body's expressions call it
	Param string // the synthesized lambda parameter holding its value
}

// BlockBody is a lambda body that is a sub-pipeline rather than an expression.
type BlockBody struct {
	// Param is the name of the synthesized lambda parameter the body's input
	// comes from. typecheck and eval look it up in their environments, so a
	// block works in whichever parameter slot the primitive binds first.
	Param string
	// Extra are the lambda's other parameters, named by a `Params:` argument
	// and in scope for every expression in the body. Empty for the
	// one-parameter case, which is every body written before this existed.
	Extra []BlockBind
	// Stmts is the body as written, kept so tooling that walks the program
	// sees the same statements the source does.
	Stmts []*Statement
	Pipe  BlockPipeline
	Pos   token.Position
}

func (*BlockBody) expr() {}
