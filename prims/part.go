package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
)

// Part blocks: the two-answers-per-input shape.
//
// A Part branches from the current value exactly like a Channel — it is a
// passthrough, so sibling Parts all see the same upstream value and the parse
// above them happens once. The difference is where its result goes: a Channel
// stores it under a name for a From: consumer, while a Part labels whatever its
// body Reveals.
//
// Output is explicit: a Part prints only what its body Reveals, which keeps
// Reveal the single output sink and keeps the linter's "never Reveals" check
// honest. A Part whose body never Reveals is a lint warning, not an error.

// resolvePart lowers a `Part "label":` statement into a passthrough node whose
// sub-pipeline runs on the current value with the Part's label in scope.
func (r *resolver) resolvePart(stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	label := stmt.PartName
	if label == "" {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: "Part requires a label, e.g. Part \"1\":"}
	}
	if _, exists := r.parts[label]; exists {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("Part %q is already defined", label)}
	}
	if cur == nil {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("Part %q has no upstream value to branch from", label)}
	}
	if len(stmt.Block) == 0 {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("Part %q has an empty body", label), NeedsBlock: true}
	}

	// scopePart: a Part may consume channels defined above it with From:, but
	// may not define channels of its own (they cannot nest) or hold Parts.
	subNodes, subType, err := r.resolveSequence(stmt.Block, cur, scopePart)
	if err != nil {
		return nil, err
	}
	r.parts[label] = true

	return &ir.Node{
		Prim:    "Part",
		In:      cur,
		Out:     cur, // passthrough
		Display: fmt.Sprintf("Part %q", label),
		// The "nodes" key is what puts this body in optimizer.nodeLists, so
		// in-place passes (expression simplification, algorithm substitution)
		// fire inside a Part exactly as they do inside a Channel or a loop.
		Meta: map[string]any{"label": label, "nodes": subNodes},
		Pos:  stmt.Pos,
		Eval: func(ctx *ir.Context, in ir.Value) (ir.Value, error) {
			// Save and restore rather than clear, so the label of an enclosing
			// scope survives if Parts are ever allowed to nest.
			prev := ctx.PartLabel
			ctx.PartLabel = label
			defer func() { ctx.PartLabel = prev }()

			// Globals are restored on the way out for the same reason the
			// pipeline value is passed through: sibling Parts branch from one
			// state, and "Part 1 sorting cannot disturb what Part 2 sees"
			// (docs/language.md) is a guarantee about everything a Part can
			// reach, not only about the value. Without this a `Cursed Tool`
			// inside one Part would silently change what the next one reads.
			//
			// A Part runs once per program rather than once per element, so
			// the copy costs nothing at the scale it happens.
			savedGlobals := eval.SnapshotGlobals()
			defer eval.RestoreGlobals(savedGlobals)

			// The body's result is what the Part actually computed; the node
			// itself passes its input through. A body that failed reports nil.
			var body ir.Value
			ctx.PushFrame(fmt.Sprintf("Part %q", label), subType)
			defer func() { ctx.PopFrame(body) }()

			v, err := runBody(ctx, subNodes, in)
			if err != nil {
				return nil, err
			}
			body = v
			return in, nil
		},
	}, nil
}
