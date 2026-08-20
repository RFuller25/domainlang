package ast

import (
	"domain/ir"
	"domain/token"
)

// GlobalRef is a read of a global declared by `Cursed Object` — the resolved
// form of an identifier that named one.
//
// The parser never builds one. Like BlockBody, it is synthesized during
// resolution: prims' rewriteExpr replaces an *Ident* that names a global (and
// that nothing nearer shadows) with this node, once, while the program is
// lowered. That is the whole performance argument for the feature, and it is
// worth stating where the node is defined rather than only in the design doc.
//
// A global is program-scoped, so if globals were read the way `Consider`
// bindings are — seeded by name into the environment every lambda application
// builds — then *every* lambda in the program would pay for *every* global in
// it, whether or not its body mentioned one. That cost is measured in
// eval/bindings_bench_test.go: one binding in scope makes an application that
// reads nothing 1.8x slower, eight make it 8x slower. Resolving the read to a
// slot index instead means the environment never grows, and evaluating this
// node is a bounds-checked slice load rather than a map probe.
//
// Slot is an index into the run's global array (eval/globals.go), dense and
// assigned in declaration order. Name is kept for errors, tracing and the
// tools that print an expression back to the user; nothing dispatches on it.
// Type is the global's declared type, fixed when it was declared — carried
// here so typecheck can answer without a second slot table to keep in step,
// exactly as BlockBody carries its resolved pipeline.
type GlobalRef struct {
	Slot int
	Name string
	Type *ir.Type
	Pos  token.Position

	// Mutable says something in the program can change this global after it is
	// declared — a `Cursed Tool`, a `:=`, or a `Cursed Object` written
	// somewhere that runs more than once. It is false for the common shape: a
	// global declared once at the top level and only ever read.
	//
	// The optimizer needs it to decide whether a stage reading this name is
	// still a pure function of its input. It rides on the node rather than
	// being computed from the pipeline because every caller of the question
	// has a lambda and nothing else, and because it is a property of the
	// *source* that no rewrite can change: a pass may move or copy a read, but
	// none invents one, and none can make an unwritten global written.
	//
	// It is deliberately an over-approximation. A `:=` is counted by name
	// alone, without asking whether something nearer shadowed the global at
	// that point, because being wrong in that direction only costs a stage its
	// rewrites while being wrong in the other direction is a wrong answer.
	Mutable bool
}

func (*GlobalRef) expr() {}
