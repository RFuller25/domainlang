package ir

// The Meta keys a `Cursed Object` / `Cursed Tool` node carries: the writes it
// performs, and the resolved sub-pipelines behind the `Of` ones.
//
// They live here for the same reason MetaBinds does — every reader is in a
// different package (the compiler backend, the optimizer's node walk, the
// language server) and none of them may depend on the resolver.
const (
	MetaGlobals = "globals"
	// MetaGlobalNodes is [][]*Node, one entry per write whose value is an `Of`
	// operation or body. The optimizer's nodeLists reads it so those stages
	// get the same in-place rewrites as any others; without it a whole
	// sub-pipeline would run unoptimized purely for being written under a
	// declaration.
	MetaGlobalNodes = "globalNodes"
)

// GlobalWrite is one `NAME As/Of …` line on a declaration node: a Binding, plus
// the slot its value lands in.
//
// The value machinery is a Binding's outright, because a declaration's
// right-hand side *is* a binding's — the same three forms with the same two
// prepositions meaning the same two things. Only the destination differs: a
// binding pushes a name into scope for one statement, and this writes a slot
// that outlives every statement. That is the whole difference, and having the
// interface say so keeps the two backends from growing a second copy of the
// three-form logic.
type GlobalWrite interface {
	Binding
	// Slot is the index into the run's global array this write lands in
	// (eval/globals.go), or the package-level variable the compiler gives it.
	Slot() int
}
