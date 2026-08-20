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

	// Constants pins `Consider x As/Of …` bindings to the values they were
	// measured holding, keyed as ConsiderKey renders them.
	//
	// A binding is a value computed once when a scope opens and read by every
	// lambda under it: a list's length, a modulus, a limit. The compiler emits
	// it as a Go local because it cannot know what it will hold. A caller who
	// has *watched* it hold 16 on every one of fifty thousand laps can say so,
	// and what that buys is not the arithmetic — it is what the Go compiler
	// does next with a value it can see: `% l` becomes a mask rather than a
	// division, comparisons fold, and bounds checks against it disappear.
	//
	// This is the field that can be wrong on a different input, and wrong
	// silently: a binary built with `l = 16` on a sixteen-bank input will
	// happily wrap modulo 16 on a twenty-bank one. Pinned tier, and the search
	// records what it measured in the recipe's contract.
	Constants map[string]int64

	// ListCapacities reserves a measured number of elements for the list
	// accumulators the generator has no estimate for at all, keyed as
	// ListSiteKey renders them.
	//
	// ListCapacity above replaces a *guess* — the split-and-parse loop's
	// len/2+1. These sites have no guess to replace: a generator loop, an
	// unfold, a stream with a take limit, all start from `[]T{}` because the
	// length is not a function of anything in scope. What that costs is
	// growth: a list that reaches five million elements is reallocated and
	// copied twenty-two times on the way, and a profile of one such program
	// spends a third of its time in `growslice`.
	//
	// A capacity is a hint and stays correct on any input — append grows a
	// slice that outgrows it — so this is guarded tier, like the capacity
	// above. What it can be is *wasteful*: reserving five million elements for
	// a run that produces ten is 40MB nobody uses, which is why the compiler
	// does not guess it and why a measurement is what licenses it.
	//
	// Keys are positions for the same reason ElideNodes' are: a caller that
	// measured one schedule may be building another, and a key naming a site
	// this build does not have is ignored. A stale entry makes a binary slow,
	// never wrong.
	ListCapacities map[string]int

	// ProbeConstants makes the generated program report what its bindings
	// actually held, rather than changing what it computes.
	//
	// It is the reconnaissance half of Constants *and* of ListCapacities: one
	// build, one run, and a file of `key first max calls varies` lines to read
	// both off. It belongs here rather than in a separate generator because
	// the *only* reliable account of what a binding held, or of how far a list
	// grew, is the one the program itself gives — re-deriving either from the
	// source would be re-implementing the program.
	//
	// A probe build is never a measured build. It is compiled, run once
	// untimed, and thrown away.
	ProbeConstants bool

	// MaxProcs, when non-zero, is passed to runtime.GOMAXPROCS at startup.
	//
	// A Domain binary runs on one goroutine — codegen emits no `go` statement
	// anywhere, and Channels and Parts are no exception; they compile to
	// straight-line code that runs one branch after another. So the Ps beyond
	// the first exist for the collector's mark workers, and setting this to 1
	// changes nothing about what the program computes on any input, which is
	// what makes it general tier despite reading like a machine setting.
	//
	// It is *not* a compiler default, and the reason is measured rather than
	// assumed. Over the 37 programs in bench/testdata, `GOMAXPROCS(1)` is a
	// consistent win on ten (read_length 0.78x, sparse_life 0.80x, while_halve
	// 0.83x, explore_states 0.82x) and a consistent loss on six
	// (count_by_entries 1.21x, pipeline_body 1.20x, float_sum 1.14x), both
	// reproducing across independent runs on an idle box; the geometric mean is
	// 0.98x and total wall clock 0.97x. Concurrent marking on the spare cores
	// genuinely pays for itself when a program allocates hard enough, and which
	// side of that a program falls on is a fact about its allocation rate — a
	// runtime property the compiler cannot read off the source. That is why this
	// stays where a measurement can license it, per program, in the search.
	MaxProcs int

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
		!t.ASCIIGuarded && t.GCPercent == 0 && t.MemoryLimitBytes == 0 &&
		len(t.Constants) == 0 && !t.ProbeConstants && t.MaxProcs == 0 &&
		len(t.ListCapacities) == 0
}

// ListSiteKey identifies a list accumulator across a reload: the node whose
// loop fills it, and where the user wrote it. The prefix keeps the key space
// apart from ConsiderKey's, since one probe report carries both.
func ListSiteKey(n *ir.Node) string { return "list:" + NodeKey(n) }

// ConsiderKey identifies one `Consider` binding across a reload: the node's
// primitive and position, and the name bound there.
//
// The same reasoning as NodeKey, one level finer. A `Consider` node may bind
// several names and a caller pinning one of them has to name it — and has to
// name it in terms of what the *user wrote*, not of which local the generator
// happened to allocate, since a different pass schedule allocates different
// ones.
func ConsiderKey(n *ir.Node, name string) string {
	return NodeKey(n) + "#" + name
}

// constFor returns the literal a binding was pinned to, if it was.
//
// Only Int bindings are pinnable. A pinned Text would put input-derived bytes
// into the generated program, which is the one thing the tuning boundary
// exists to prevent — see the mahoraga spec on why "print the answer" has to
// be unreachable rather than merely rejected, and Facts on why every measured
// quantity here is a count and never a content.
func (g *gen) constFor(key string, t *ir.Type) (string, bool) {
	if len(g.tuning.Constants) == 0 || t == nil || t.Kind != ir.KInt {
		return "", false
	}
	v, ok := g.tuning.Constants[key]
	if !ok {
		return "", false
	}
	// Written as a conversion rather than a bare literal so the constant
	// carries its type wherever it is substituted. It is still a constant to
	// the Go compiler — a conversion of an untyped constant is one — so
	// nothing is lost, and an inferred `int` in a context expecting an Int is
	// avoided.
	return "int64(" + itoa64(v) + ")", true
}

// probing reports whether this build is a reconnaissance build that reports
// what its bindings held.
func (g *gen) probing() bool { return g.tuning.ProbeConstants }

// accumDecl is the declaration of a list accumulator the generator has no
// estimate for: `v := []T{}` ordinarily, and a reservation when a caller has
// measured how long it grows.
func (g *gen) accumDecl(n *ir.Node, v, elemGo string) string {
	if c := g.tuning.ListCapacities[ListSiteKey(n)]; c > 0 && n != nil {
		return v + " := make([]" + elemGo + ", 0, " + itoa(c) + ")"
	}
	return v + " := []" + elemGo + "{}"
}

// probeLen reports how long an accumulator grew, on a probe build. It is
// emitted after the loop that fills it, where the length is final.
func (g *gen) probeLen(n *ir.Node, v string) {
	if !g.probing() || n == nil {
		return
	}
	g.wl("dmProbe(%q, int64(len(%s)))", ListSiteKey(n), v)
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

// procsTuningStmt is the scheduler setting a tuned binary carries, emitted
// beside the collector settings at the top of main and for the same reason:
// after the first allocation it would be resizing a pool that already exists.
func procsTuningStmt(t Tuning) string {
	if t.MaxProcs <= 0 {
		return ""
	}
	return "\truntime.GOMAXPROCS(" + itoa(t.MaxProcs) + ")\n"
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
