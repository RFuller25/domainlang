package prims

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/ir"
)

// Fold's `Then:` clause.
//
//	Maximum Technique: Fold
//	    Seed: ...
//	    Using: (acc, x) -> ...
//	    Then: (acc) -> expr
//
// A fold often needs one last expression over the finished accumulator, and
// the only way to write it was to append a sentinel element to the list so the
// final lambda call could compute the answer. `Then:` is that expression: the
// stage's result is Then(final accumulator) and its type is the lambda's.
//
// It is resolved as the stage it is: the Fold node, unchanged, followed by an
// Apply node carrying the Then lambda. Every optimizer pass and the compiler
// already understand both, so none of them can drop the clause — a pass that
// rewrites the Fold sees only the Fold, and the Apply after it still runs. The
// Apply sits inside the same binding scope as the Fold (resolveStatement wraps
// everything this returns), so a `Consider` local the body updated with `:=`
// is read by Then at its final value. An empty list folds to the seed, so Then
// applies to the seed.
//
// Fold and Fold From: take it; Reduce has no seed or accumulator type to
// speak of and keeps its single form.
func (r *resolver) thenStage(stmt *ast.Statement, fold *ir.Node) (*ir.Node, error) {
	if fold.Prim != "Fold" && fold.Prim != "FoldOver" {
		return nil, nil
	}
	var then *ast.Arg
	for _, a := range stmt.Args {
		if a.Name == "Then" {
			then = a
		}
	}
	if then == nil {
		return nil, nil
	}
	then.Used = true
	la, ok := then.Value.(ast.LambdaArg)
	if !ok {
		return nil, &ResolveError{Pos: then.Pos, Msg: "Fold Then: must be a lambda over the finished accumulator, e.g. Then: (acc) -> acc * 2"}
	}
	if want := 1 + ambientDepth(); len(la.Lambda.Params) != want {
		return nil, &ResolveError{Pos: then.Pos, Msg: fmt.Sprintf(
			"Fold Then: lambda must take %d parameter(s) (the finished accumulator), got %d", want, len(la.Lambda.Params))}
	}
	synth := &ast.Statement{
		Keyword: "Cursed Technique",
		Op:      &ast.Operation{Words: []string{"Apply"}, Raw: "Apply"},
		Args:    []*ast.Arg{{Name: "Using", Value: la, Pos: then.Pos}},
		Pos:     stmt.Pos,
	}
	node, err := r.buildPrimitive(apply, synth, fold.Out)
	if err != nil {
		if re, ok := err.(*ResolveError); ok {
			re.Msg = "Fold Then: " + strings.TrimPrefix(re.Msg, "Apply: ")
		}
		return nil, err
	}
	node.Display = "Then"
	return node, nil
}
