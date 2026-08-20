package eval

import "domain/ir"

// The runtime half of `Cursed Object` globals: the run's slot array.
//
// This is deliberately *not* the mechanism bindings use. A `Consider` binding
// is seeded by name into the environment every lambda application builds
// (bindings.go), which is affordable because a binding is in scope for one
// statement. A global is in scope for the whole program, so the same treatment
// would make every lambda in the program pay for every global in it — 1.8x on
// an application that reads nothing at one global, 8x at eight
// (bindings_bench_test.go). Instead the resolver rewrites each read to an
// *ast.GlobalRef carrying its slot, and evaluating one is the slice load
// below: the environment never grows, and an application costs exactly what it
// cost before globals existed.
//
// The slot *is* the cell. A binding written to with `:=` has to be boxed in a
// *Cell because environments are copied on the way into every scope, so a
// write into a copy would be lost; a slot is not copied and not scoped, so a
// write lands where the next reader looks without any boxing.
//
// Like bindings, this is package-level state under the single-threaded,
// one-run-at-a-time assumption the rest of the interpreter makes. It is sized
// and cleared per run from the pipeline's own slot count (interp.Run), rather
// than at resolve time, because the language server and the REPL resolve one
// program while another is still running and their slot numbering has nothing
// to do with each other.
var globals []ir.Value

// ResetGlobals sizes the run's slot array and clears it. n is the number of
// globals the pipeline declares (ir.Pipeline.Globals).
func ResetGlobals(n int) {
	if cap(globals) >= n {
		globals = globals[:n]
		clear(globals)
		return
	}
	globals = make([]ir.Value, n)
}

// Global reads slot i.
func Global(i int) ir.Value { return globals[i] }

// SetGlobal writes slot i.
func SetGlobal(i int, v ir.Value) { globals[i] = v }

// GlobalCount is how many slots the current run has, for the tools that report
// on a run rather than take part in it.
func GlobalCount() int { return len(globals) }

// SnapshotGlobals copies the slot array, and RestoreGlobals puts one back.
//
// A `Part` runs its body against a copy so that writes inside it cannot be
// seen by a sibling Part — the isolation docs/language.md states outright for
// the pipeline value ("Part 1 sorting cannot disturb what Part 2 sees"), which
// a mutable global would otherwise punch straight through. A Part runs twice
// per program rather than once per element, so the copy is free at the scale
// it happens.
func SnapshotGlobals() []ir.Value {
	if len(globals) == 0 {
		return nil
	}
	out := make([]ir.Value, len(globals))
	copy(out, globals)
	return out
}

// RestoreGlobals puts back a slot array taken by SnapshotGlobals.
func RestoreGlobals(saved []ir.Value) {
	if saved == nil {
		clear(globals)
		return
	}
	globals = globals[:len(saved)]
	copy(globals, saved)
}
