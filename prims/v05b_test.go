package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// Range closes the workaround challenges/01_fizzbuzz.domain documented in its
// own header: "Domain has no range generator".
func TestRangeIsHalfOpen(t *testing.T) {
	for _, c := range []struct{ phrase, want string }{
		// Half-open deliberately: `range(N)` in a For header already means
		// 0..N-1, and two meanings of "range" would be worse than `Range 1 16`.
		{"Range 5", "[0, 1, 2, 3, 4]"},
		{"Range 1 6", "[1, 2, 3, 4, 5]"},
		{"Range 3 3", "[]"},
	} {
		src := "Cursed Energy: stdin\nCursed Technique: " + c.phrase + "\n"
		v, _ := runPipeline(t, src, "x")
		if got := ir.FormatValue(v); got != c.want {
			t.Errorf("%s = %s, want %s", c.phrase, got, c.want)
		}
	}
}

func TestRangeRejectsInvertedBounds(t *testing.T) {
	_, err := resolveSrc(t, "Cursed Energy: stdin\nCursed Technique: Range 9 2\n")
	if err == nil || !strings.Contains(err.Error(), "half-open") {
		t.Fatalf("expected an inverted-bounds error, got %v", err)
	}
}

// Reverse over Text: a palindrome check used to round-trip through
// Split Text by "".
func TestReverseText(t *testing.T) {
	src := "Cursed Energy: stdin\nReverse Cursed Technique: Reverse\n"
	v, _ := runPipeline(t, src, "abcde")
	if got := ir.FormatValue(v); got != "edcba" {
		t.Fatalf("Reverse over Text: got %q", got)
	}
	// By rune, like every other text position in the language.
	v, _ = runPipeline(t, src, "héllo")
	if got := ir.FormatValue(v); got != "olléh" {
		t.Fatalf("Reverse should be rune-wise: got %q", got)
	}
}

func TestSubgridCrops(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n" +
		"Cursed Technique: Subgrid 1 1 2 2\n"
	v, _ := runPipeline(t, src, "abcd\nefgh\nijkl")
	if got := ir.FormatValue(v); got != "fg\njk" {
		t.Fatalf("Subgrid: got %q", got)
	}
}

// Out of bounds errors rather than clamping: a crop that silently returned
// fewer rows than asked for would be a wrong answer that looks right.
func TestSubgridOutOfBoundsErrors(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n" +
		"Cursed Technique: Subgrid 1 1 9 9\n"
	_, err := runErr(t, src, "abcd\nefgh")
	if err == nil || !strings.Contains(err.Error(), "does not fit") {
		t.Fatalf("expected a fit error, got %v", err)
	}
}

func TestPadGridAddsBorder(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n" +
		"Cursed Technique: Pad Grid 1\n    Fill: \".\"\n"
	v, _ := runPipeline(t, src, "ab\ncd")
	if got := ir.FormatValue(v); got != "....\n.ab.\n.cd.\n...." {
		t.Fatalf("Pad Grid: got %q", got)
	}
}

func TestPadGridChecksFillType(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n" +
		"Cursed Technique: Pad Grid 1\n    Fill: 0\n"
	_, err := resolveSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "Fill:") {
		t.Fatalf("expected a Fill: type error, got %v", err)
	}
}

// Mode: 4 | 8 on the grid searches. A diagonal chain is three components
// orthogonally and one with diagonals — the cheapest possible witness that
// the connectivity choice is actually threaded through.
func TestConnectedComponentsConnectivity(t *testing.T) {
	base := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n" +
		"Domain Expansion: Connected Components\n"
	v, _ := runPipeline(t, base+"    Using: (c) -> c = \"#\"\n", "#..\n.#.\n..#")
	if v.(int64) != 3 {
		t.Fatalf("default (4-connectivity) should give 3, got %v", v)
	}
	v, _ = runPipeline(t, base+"    Mode: 8\n    Using: (c) -> c = \"#\"\n", "#..\n.#.\n..#")
	if v.(int64) != 1 {
		t.Fatalf("Mode: 8 should join the diagonal into 1, got %v", v)
	}
}

func TestBFSConnectivity(t *testing.T) {
	base := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n" +
		"Domain Expansion: BFS from 0 0\n"
	// (2,2) is unreachable orthogonally through the dots on this board, but
	// two diagonal steps away.
	v, _ := runPipeline(t, base+"    Mode: 8\n    Using: (c) -> c = \".\"\n", "...\n...\n...")
	g := v.(*ir.GridValue)
	d, _ := g.At(2, 2)
	if d != int64(2) {
		t.Fatalf("Mode: 8 should reach (2,2) in 2 steps, got %v", d)
	}
	v, _ = runPipeline(t, base+"    Using: (c) -> c = \".\"\n", "...\n...\n...")
	g = v.(*ir.GridValue)
	d, _ = g.At(2, 2)
	if d != int64(4) {
		t.Fatalf("4-connectivity should reach (2,2) in 4 steps, got %v", d)
	}
}

func TestSearchRejectsUnknownMode(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n" +
		"Domain Expansion: Connected Components\n    Mode: 6\n    Using: (c) -> c = \"#\"\n"
	_, err := resolveSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "4 or 8") {
		t.Fatalf("expected a Mode error naming 4 or 8, got %v", err)
	}
}

// Reveal: stderr keeps debugging output out of the program's answer.
func TestRevealStderrDoesNotTouchStdout(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\nMaximum Technique: Count\nReveal: stderr\n"
	v, _ := runPipeline(t, src, "a\nb\nc")
	// The value passes through unchanged, like any Reveal.
	if v.(int64) != 3 {
		t.Fatalf("Reveal is a passthrough, got %v", v)
	}
}

// A Set used to be a dead end: Map Each has no Set case, so after Convert To
// Set the only moves left were Count, contains and Difference. The list
// primitives now accept one, reading it in insertion order — the order it
// already renders and iterates in.
func TestListPrimitivesAcceptASet(t *testing.T) {
	base := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Set\n"
	for _, c := range []struct{ tail, want string }{
		{"Cursed Technique: Map Each\n    Using: (s) -> upper(s)\n", "[A, B, C]"},
		{"Cursed Technique: Filter\n    Using: (s) -> ikke s = \"b\"\n", "[a, c]"},
		{"Maximum Technique: Count Matching\n    Using: (s) -> ikke s = \"a\"\n", "2"},
		{"Cursed Technique: Take Item 1\n", "b"},
		{"Cursed Technique: Enumerate\n", "[[0, a], [1, b], [2, c]]"},
	} {
		v, _ := runPipeline(t, base+c.tail, "a\nb\na\nc")
		if got := ir.FormatValue(v); got != c.want {
			t.Errorf("%s = %s, want %s", strings.TrimSpace(c.tail), got, c.want)
		}
	}
}

// The result is a List, not a Set: a transform may map two distinct elements
// onto the same value, and silently deduplicating would lose data the program
// asked for.
func TestMapEachOverASetDoesNotDeduplicate(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Set\n" +
		"Cursed Technique: Map Each\n    Using: (s) -> charat(s, 0)\n"
	v, _ := runPipeline(t, src, "ax\nay\nbz")
	if got := ir.FormatValue(v); got != "[a, a, b]" {
		t.Fatalf("expected a List with the duplicate kept, got %s", got)
	}
}

// A Channel body may consume channels declared above it. Before this, a value
// derived from two channels could not itself be named — it had to be
// recomputed at every consumer, or the pipeline restructured around it.
func TestChannelBodyMayConsumeEarlierChannels(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\n" +
		"Channel \"firsts\":\n    Cursed Technique: Map Each\n        Using: (s) -> charat(s, 0)\n" +
		"Channel \"lengths\":\n    Cursed Technique: Map Each\n        Using: (s) -> length(s)\n" +
		"Channel \"labelled\":\n" +
		"    Maximum Technique: Zip\n        From: firsts, lengths\n" +
		"    Cursed Technique: Map Each\n        Using: (p) -> item(p, 0) + totext(item(p, 1))\n" +
		"Maximum Technique: Combine\n    From: labelled\n    Using: (xs) -> textjoin(xs, \",\")\n"
	v, _ := runPipeline(t, src, "apple\nant\nbee")
	if got := ir.FormatValue(v); got != "a5,a3,b3" {
		t.Fatalf("channel composition: got %s", got)
	}
}

// Declaration order gives the dependency DAG for free: a channel enters the
// environment only once its own body has resolved, so a self-reference is
// already an unknown-channel error and no cycle check is needed.
func TestChannelCannotReferenceItselfOrLater(t *testing.T) {
	self := "Cursed Energy: stdin\nShikigami: Lines\n" +
		"Channel \"a\":\n    Maximum Technique: Combine\n        From: a\n        Using: (x) -> x\n"
	if _, err := resolveSrc(t, self); err == nil ||
		!strings.Contains(err.Error(), "unknown channel") {
		t.Fatalf("a self-reference should be an unknown channel, got %v", err)
	}
	forward := "Cursed Energy: stdin\nShikigami: Lines\n" +
		"Channel \"a\":\n    Maximum Technique: Combine\n        From: b\n        Using: (x) -> x\n" +
		"Channel \"b\":\n    Maximum Technique: Count\n"
	if _, err := resolveSrc(t, forward); err == nil ||
		!strings.Contains(err.Error(), "unknown channel") {
		t.Fatalf("a forward reference should be an unknown channel, got %v", err)
	}
}

// Channels still cannot nest, and loop and Shikigami bodies still refuse
// From: consumers — only the Channel-body restriction was lifted.
func TestChannelsStillCannotNest(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\n" +
		"Channel \"outer\":\n    Channel \"inner\":\n        Maximum Technique: Count\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "cannot be nested") {
		t.Fatalf("expected a nesting error, got %v", err)
	}
}

func TestLoopBodyStillRefusesFromConsumers(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Lines\n" +
		"Channel \"c\":\n    Maximum Technique: Count\n" +
		"Simple Domain: Repeat 2\n" +
		"    Maximum Technique: Combine\n        From: c\n        Using: (x) -> x\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "loop or Shikigami body") {
		t.Fatalf("expected a scope error naming loops, got %v", err)
	}
}
