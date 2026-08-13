package codegen

import (
	"fmt"
	"slices"

	"domain/ir"
)

// Tuning: what a caller knows about the input that the compiler cannot.
//
// The compiler proper is not allowed to know anything about the data a program
// will be run on — that is the whole difference between it and
// `domain expansion: mahoraga`, which measures one input and is allowed to
// exploit everything it finds. This file is the seam between the two. It holds
// *facts*, not transformations: an element count, whether every byte is ASCII,
// how much heap a run actually used. The generator consults them at the points
// where it is already guessing, and generates what it would have generated
// anyway when they are absent.
//
// Three properties make this safe to have in the compiler at all:
//
//   - **The zero value is the compiler's own behaviour.** `Options{}` produces
//     exactly the program `domain build` produces today, so every existing
//     caller is unaffected and a tuning that is dropped somewhere degrades to
//     correct-and-slower rather than to wrong.
//   - **It is serialisable.** Every field is a plain number, string or bool, so
//     it round-trips through a recipe's JSON. A tuned build has to be
//     reproducible from the record of it, or the record is decoration.
//   - **No field can carry an answer.** These describe the *shape* of an input,
//     never its contents, which is what keeps "print the expected output"
//     unreachable rather than merely rejected. See the mahoraga spec.
//
// Each field also records its tier, because that is what a reader of the recipe
// has to be able to check: a capacity hint stays correct on any input and a
// removed UTF-8 decode does not.

// Tuning is the measured shape of one input.
type Tuning struct {
	// ListCapacity is how many elements the program's top-level list actually
	// has. It replaces the generator's own guess where it has one — the
	// fused split-and-parse loop reserves `len(input)/2+1`, which for numeric
	// input of any width over one digit is a considerable over-reservation.
	//
	// A capacity is a hint and nothing else: append still grows a slice that
	// outgrows it, so this stays correct for any input. Guarded tier.
	ListCapacity int

	// ASCIIText says every byte of the input decodes as one rune.
	//
	// Where that holds, a rune position is a byte position and a cell is a
	// one-byte substring, so the grid builder can drop UTF-8 decoding entirely.
	// The binary that results carries no check — it cannot notice a multibyte
	// input and will cut a character in half without saying so — which is why
	// this is pinned tier and why the search verifies it against every byte of
	// the real input before setting it.
	ASCIIText bool

	// GCPercent, when non-zero, is passed to debug.SetGCPercent at startup.
	// -1 switches the collector off.
	//
	// This is the adaptation that makes the least sense as a general
	// optimization and the most sense for one known run: a program that lives
	// for twenty milliseconds and exits never benefits from collecting
	// anything, and every cycle it spends marking is a cycle spent tidying a
	// house that is about to be demolished. The general compiler cannot do this
	// because it does not know the program is short-lived or how much it
	// allocates; the search measures both.
	GCPercent int

	// ASCIIGuarded is ASCIIText with the check kept.
	//
	// The generator emits both paths and a one-pass test per line to choose
	// between them, so the program is correct on any input and takes the fast
	// path on the lines that are plain — including on mixed input, line by line.
	// It is the guarded twin of ASCIIText, and the pair is what makes the tiers
	// mean something concrete: `--tier guarded` buys the fast path and pays for
	// the check, `--tier pinned` drops the check and the generality with it.
	//
	// ASCIIText wins when both are set: a caller who has verified the whole
	// input has no use for a check on every line of it.
	ASCIIGuarded bool

	// ElideNodes names pipeline stages to leave out of the generated program
	// entirely, each as NodeKey renders it.
	//
	// This is the one field that can change what a program computes, and it is
	// the reason the identifiers are positions rather than indices: a caller
	// asking for a stage to be dropped must be naming a stage in the source it
	// measured, not the seventh node of whatever the optimizer produced today.
	// A key that matches nothing is ignored, which makes a stale entry slow
	// rather than wrong.
	//
	// The compiler does not judge whether dropping a stage is safe — it cannot,
	// since that is a question about the data. mahoraga's catalogue establishes
	// it by watching the stage do nothing over an entire real run, and only for
	// primitives where doing nothing to the length means doing nothing at all.
	ElideNodes []string

	// MemoryLimitBytes, when non-zero, is passed to debug.SetMemoryLimit as the
	// backstop under a disabled collector.
	//
	// It is what keeps GCPercent guarded rather than pinned. With a limit set,
	// switching the collector off is a statement about *this* input's heap and
	// not a promise about every input: a larger one crosses the limit, the
	// collector turns itself back on, and the program is merely as fast as it
	// would have been. Without it, a larger input would exhaust memory.
	MemoryLimitBytes int64
}

// Empty reports whether this tuning asks for nothing, in which case the
// generator behaves exactly as `domain build` does.
func (t Tuning) Empty() bool {
	return len(t.ElideNodes) == 0 && t.ListCapacity == 0 && !t.ASCIIText &&
		!t.ASCIIGuarded && t.GCPercent == 0 && t.MemoryLimitBytes == 0
}

// NodeKey identifies a pipeline stage across a reload: its primitive and the
// source position it came from.
//
// Not an index into the node list, because the whole point is to survive one:
// a caller measures a program, the optimizer is asked for a different pass
// schedule, and the seventh node is now something else entirely. A position is
// what the user wrote and does not move.
func NodeKey(n *ir.Node) string {
	if n == nil {
		return ""
	}
	return fmt.Sprintf("%s@%d:%d", n.Prim, n.Pos.Line, n.Pos.Col)
}

// elided reports whether a node was named for removal.
func (g *gen) elided(n *ir.Node) bool {
	if len(g.tuning.ElideNodes) == 0 || n == nil {
		return false
	}
	return slices.Contains(g.tuning.ElideNodes, NodeKey(n))
}

// keepNodes drops the stages a caller asked to remove. The common case — no
// elisions at all — returns the list it was given, so an ordinary build does
// not copy every node list in the program to change nothing.
func (g *gen) keepNodes(nodes []*ir.Node) []*ir.Node {
	if len(g.tuning.ElideNodes) == 0 {
		return nodes
	}
	out := make([]*ir.Node, 0, len(nodes))
	for _, n := range nodes {
		if !g.elided(n) {
			out = append(out, n)
		}
	}
	return out
}

// listCap is the capacity to reserve for the top-level list, given the
// generator's own estimate as a Go expression. A tuning that says nothing
// leaves the estimate in place.
func (g *gen) listCap(estimate string) string {
	if g.tuning.ListCapacity <= 0 {
		return estimate
	}
	return itoa(g.tuning.ListCapacity)
}

// asciiText reports whether the generator may assume one byte per rune with no
// check at all.
func (g *gen) asciiText() bool { return g.tuning.ASCIIText }

// asciiGuarded reports whether the generator should emit both paths and choose
// between them at run time. The unguarded form wins when both are asked for:
// a caller who verified the whole input has no use for a per-line check.
func (g *gen) asciiGuarded() bool { return g.tuning.ASCIIGuarded && !g.tuning.ASCIIText }

// declGCTuning is the startup call a tuned binary carries. It is emitted at the
// top of main, before anything allocates, and only when a tuning asks for it —
// an untuned build has no runtime/debug import at all.
func gcTuningStmt(t Tuning) string {
	if t.GCPercent == 0 && t.MemoryLimitBytes == 0 {
		return ""
	}
	out := ""
	if t.MemoryLimitBytes > 0 {
		// The limit goes first: setting it after switching the collector off
		// would leave a window in which nothing bounds the heap.
		out += "\tdebug.SetMemoryLimit(" + itoa64(t.MemoryLimitBytes) + ")\n"
	}
	if t.GCPercent != 0 {
		out += "\tdebug.SetGCPercent(" + itoa(t.GCPercent) + ")\n"
	}
	return out
}

func itoa(n int) string { return itoa64(int64(n)) }

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	var buf [24]byte
	i := len(buf)
	u := uint64(n)
	if neg {
		u = uint64(-n)
	}
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
