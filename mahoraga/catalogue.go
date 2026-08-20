package mahoraga

// The catalogue: templated edits to the emitted Go, each one a measured fact
// about the input plus a place in the generated program where that fact is
// worth something.
//
// This is the closed search space the whole design rests on. Nothing here
// mutates code freely and no entry replaces a computation with its result,
// which is why "print the answer" is unreachable rather than rejected — there
// is no entry that could express it. Adding an entry is adding a row to this
// table and a branch in codegen/tuning.go, and both are reviewable.
//
// Each entry carries three things:
//
//   - **A precondition**, over the measured facts *and* the Go the compiler
//     actually emitted. Both halves are load-bearing: the facts say the input
//     permits it, the source says the program has anywhere to apply it. An
//     entry that only checked the facts would spend a compile and a full
//     measurement establishing that changing nothing changes nothing.
//   - **A tuning**, which is a value in codegen.Tuning and nothing else. The
//     compiler decides what to do with it; the catalogue never writes Go.
//   - **A tier**, which is a claim about correctness and is what `--verify`
//     and `--replay` are checked against. Guarded entries keep a fallback and
//     stay correct on any input. Pinned ones do not, and the binary carries no
//     check — so the search verifies the precondition against every byte of
//     the real input before setting one.

import (
	"fmt"
	"runtime"
	"strings"

	"domain/codegen"
)

// entry is one templated edit.
type entry struct {
	// ID is what the recipe and the report call this. It is a sentence
	// fragment, because it ends up in a line a human reads.
	ID   string
	Tier Tier
	// Why is the one-line explanation the recipe carries when the entry is
	// rejected for not applying.
	Why string
	// Applies reports whether this entry has anything to do here, and returns
	// the label to report it under. The label may name a measured number, so
	// the report says "reserve 2,187 elements" rather than "preallocate".
	//
	// Most entries look at goSrc as well as the facts, because most of them
	// rewrite a specific thing the backend emitted. The ones that do not —
	// the collector settings, which are about the run rather than about the
	// code — are the exception, and say so at their definition.
	Applies func(f Facts, goSrc string) (label string, ok bool)
	// Apply folds the entry into a tuning. Entries are applied greedily on top
	// of one another, so this mutates rather than replaces.
	Apply func(f Facts, t *codegen.Tuning)

	// Pin records what this entry assumes of a future input, for entries whose
	// tier is Pinned. It is what lets `--verify` accept a *different* input
	// that satisfies the same assumption, instead of binding the recipe to one
	// file by hash. Nil for entries that assume nothing.
	Pin func(f Facts, c *Contract)

	// Kind separates a parameter change from a specialisation with a fallback,
	// which is the line between turn 6 and turn 7.
	Kind entryKind
}

// entryKind is what shape of change an entry makes.
type entryKind int

const (
	// kindEdit changes a number the generator was already choosing: a capacity,
	// a collector setting. There is no second code path.
	kindEdit entryKind = iota
	// kindSpecialisation compiles a fast path for the observed shape *and* the
	// general path, choosing between them at run time. This is turn 7's shape,
	// and the pinned tier's version of the same entry is the one that drops the
	// check and the fallback.
	kindSpecialisation
)

// catalogue is every templated edit, in the order they are tried.
//
// The order is deliberate: cheap and safe first. A greedy search keeps whatever
// wins and tries the next entry on top of it, so an entry that lands early is
// in the champion every later entry is measured against.
var catalogue = []entry{
	{
		ID:   "exact list capacity",
		Tier: Guarded,
		Why:  "the program has no guessed list capacity to replace",
		Applies: func(f Facts, goSrc string) (string, bool) {
			// The guess this replaces is the fused split-and-parse loop's
			// `len(input)/2+1` — the smallest a segment could be. It over-
			// reserves by the width of the elements: 2.5× for five-digit
			// numbers. Only worth doing when the split is by newline, since
			// that is what the segment count was measured over.
			if !strings.Contains(goSrc, "/2+1") || !strings.Contains(goSrc, `== '\n'`) {
				return "", false
			}
			if f.Segments <= 0 {
				return "", false
			}
			return fmt.Sprintf("exact list capacity (%d, not len/2+1)", f.Segments), true
		},
		Apply: func(f Facts, t *codegen.Tuning) { t.ListCapacity = f.Segments },
	},
	{
		ID:   "one scheduler thread",
		Tier: General,
		Why:  "the machine has one core, so there is nothing to switch off",
		Applies: func(f Facts, _ string) (string, bool) {
			// Like the collector entries, this is about the run rather than
			// about the emitted code, so there is no shape to look for. A
			// Domain binary is a straight line of loops on one goroutine; the
			// Ps beyond the first exist for the collector's mark workers, and
			// on a run that collects at all they cost coordination the program
			// never asked for.
			if runtime.NumCPU() < 2 || !f.HeapReported || f.NumGC == 0 {
				return "", false
			}
			return fmt.Sprintf("one scheduler thread instead of %d", runtime.NumCPU()), true
		},
		Apply: func(_ Facts, t *codegen.Tuning) { t.MaxProcs = 1 },
	},
	{
		ID:   "collector off for one run",
		Tier: Guarded,
		Why:  "the baseline never collected, so there is nothing to collect less of",
		Applies: func(f Facts, _ string) (string, bool) {
			// This is the entry with no code shape to look for: it is about the
			// run rather than about what the backend emitted, and every program
			// has a heap. The measurement that decides it is NumGC — a program
			// that ran no collections spends nothing on them, so switching the
			// collector off would be a build with no effect.
			if !f.HeapReported || f.NumGC == 0 {
				return "", false
			}
			// A run whose heap is already enormous is the one case where
			// switching the collector off is a bad idea even for a single run:
			// the limit that keeps it safe would have to be larger than most
			// machines have.
			if f.HeapSys > maxHeapForGCOff {
				return "", false
			}
			return fmt.Sprintf("collector off for one run (%d collections in the baseline)",
				f.NumGC), true
		},
		Apply: func(f Facts, t *codegen.Tuning) {
			t.GCPercent = -1
			t.MemoryLimitBytes = memoryLimitFor(f.HeapSys)
		},
	},
	{
		ID:   "collector four times lazier",
		Tier: Guarded,
		Why:  "the baseline collected too little for a lazier collector to save anything",
		Applies: func(f Facts, _ string) (string, bool) {
			// The entry beside this one switches the collector off, which is
			// the right answer for a program whose whole heap fits under the
			// limit that keeps it safe. This is the answer for the other
			// shape: a program that allocates far more than it keeps — a loop
			// rebuilding a list every lap — where the limit is reached however
			// lazy the collector is, and what is available is fewer, larger
			// collections rather than none.
			//
			// Four collections is the floor for asking. Below it there is not
			// enough collection happening for a quarter of it to be worth a
			// build.
			if !f.HeapReported || f.NumGC < 4 {
				return "", false
			}
			if f.HeapSys > maxHeapForGCOff {
				return "", false
			}
			return fmt.Sprintf("collector four times lazier (%d collections in the baseline)",
				f.NumGC), true
		},
		Apply: func(f Facts, t *codegen.Tuning) {
			t.GCPercent = 400
			// A wider backstop than the disabled collector's, because this
			// entry means to let the heap grow: a limit at four times the
			// observed heap would be reached immediately and would turn the
			// collector's own pacing back on, which is the thing being tuned.
			t.MemoryLimitBytes = memoryLimitFor(f.HeapSys * 2)
		},
	},
	{
		ID:   "no UTF-8 decoding",
		Tier: Pinned,
		Why:  "the program decodes no runes, or the input is not all ASCII",
		Applies: func(f Facts, goSrc string) (string, bool) {
			if !f.ASCII || !strings.Contains(goSrc, "utf8.RuneLen") {
				return "", false
			}
			return "no UTF-8 decoding (every byte of the input is one rune)", true
		},
		Apply: func(f Facts, t *codegen.Tuning) { t.ASCIIText = true },
		Pin: func(f Facts, c *Contract) {
			// The assumption is about the encoding, not about this file. Any
			// all-ASCII input satisfies it, and recording it this way is what
			// lets `--verify` say so.
			c.ASCII = true
		},
		Kind: kindSpecialisation,
	},
	{
		ID:   "guarded ASCII fast path",
		Tier: Guarded,
		Why:  "the program decodes no runes",
		Applies: func(f Facts, goSrc string) (string, bool) {
			// Deliberately *not* conditioned on the input being ASCII: the
			// point of the guarded form is that it is correct either way, and a
			// program run on mixed input still takes the fast path for the lines
			// that are plain. What it needs is somewhere to apply it.
			if !strings.Contains(goSrc, "utf8.RuneLen") {
				return "", false
			}
			return "ASCII fast path with the decode kept as a fallback", true
		},
		Apply: func(f Facts, t *codegen.Tuning) { t.ASCIIGuarded = true },
		Kind:  kindSpecialisation,
	},
}

// maxHeapForGCOff is the largest observed heap for which switching the
// collector off is still a sensible thing to do. Past it the memory limit that
// keeps the adaptation guarded would have to be larger than the machine.
const maxHeapForGCOff = 2 << 30 // 2 GiB

// memoryLimitFor is the backstop that keeps a disabled collector guarded
// rather than pinned.
//
// Four times the observed heap, with a floor: a somewhat larger input still
// runs collection-free, and a much larger one crosses the limit and turns the
// collector back on, so the program is merely as fast as it would have been
// rather than out of memory. Without this the entry would be a promise about
// every future input, made from one measurement.
func memoryLimitFor(heapSys uint64) int64 {
	limit := heapSys * 4
	if limit < 64<<20 {
		limit = 64 << 20
	}
	return int64(limit)
}

// catalogueFor returns the entries that apply here, at or below the tier the
// caller allowed, along with the label each should be reported under.
func catalogueFor(f Facts, goSrc string, maxTier Tier) []appliedEntry {
	var out []appliedEntry
	for _, e := range catalogue {
		if e.Tier > maxTier {
			continue
		}
		label, ok := e.Applies(f, goSrc)
		if !ok {
			continue
		}
		out = append(out, appliedEntry{entry: e, Label: label})
	}
	return out
}

// appliedEntry is a catalogue entry that applies, with its measured label.
type appliedEntry struct {
	entry
	Label string
}
