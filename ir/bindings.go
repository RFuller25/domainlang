package ir

import "domain/token"

// The Meta keys a Consider node carries: its bindings, and the resolved
// sub-pipelines behind the `Of` ones. They live here rather than in the
// resolver because every reader is in a different package — the compiler
// backend, the optimizer's node walk, the language server — which is the same
// reason MetaForeign does.
const (
	MetaBinds     = "binds"
	MetaBindNodes = "bindNodes"
)

// Binding is one `Consider x As/Of …` local whose value is computed when its
// scope opens, as carried on a Consider node's Meta[MetaBinds].
//
// It is an interface here, implemented in package prims, for the same reason
// ast.BlockPipeline is an interface in package ast: the resolved form belongs
// to the resolver, but the two backends have to read it, and the packages that
// must not depend on the resolver are exactly the ones both backends share.
//
// The expression-layer members are typed `any` and asserted by the caller
// (they are *ast.Lambda and ast.Expr): ast imports ir, so ir cannot name them.
// Meta already carries lambdas this way, so this adds no new indirection.
//
// Exactly one of Lambda, Expr and BlockNodes is non-nil, matching the three
// ways a binding's value can be written:
//
//	Lambda      `Of (xs) -> …`   applied to the value entering the scope
//	BlockNodes  `Of Sum`, or `Of` + an indented pipeline, run over that value
//	Expr        `As <expression>` that did not fold to a literal
type Binding interface {
	// Name is the name the binding is read by.
	Name() string
	// Type is the type of the value it binds.
	Type() *Type
	// In is the pipeline type the value is computed from.
	In() *Type
	// Lambda is the `Of` lambda over the current value, or nil.
	Lambda() any
	// Expr is the `As` expression, or nil.
	Expr() any
	// BlockNodes is the resolved sub-pipeline behind an `Of` operation or
	// body, or nil.
	BlockNodes() []*Node
	// Pos is where the binding was written, which is the line tooling has to
	// put its answer on.
	Pos() token.Position
}
