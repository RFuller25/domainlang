package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// ---------------------------------------------------------------------------
// Take While / Drop While
// ---------------------------------------------------------------------------

func TestTakeWhileStopsAtTheBoundary(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Take While\n" +
		"    Using: (x) -> x < 4\n"
	// The 3 after the boundary is *not* taken — this is what separates Take
	// While from Filter, which would have kept it.
	v, _ := runPipeline(t, src, "1,2,9,3")
	if got := ir.FormatValue(v); got != "[1, 2]" {
		t.Fatalf("take while: got %s want [1, 2]", got)
	}
}

func TestDropWhileIsTheComplement(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Drop While\n" +
		"    Using: (x) -> x < 4\n"
	v, _ := runPipeline(t, src, "1,2,9,3")
	if got := ir.FormatValue(v); got != "[9, 3]" {
		t.Fatalf("drop while: got %s want [9, 3]", got)
	}
}

func TestPrefixWhileEdgeCases(t *testing.T) {
	cases := []struct {
		name, prim, input, want string
	}{
		{"take everything", "Take While", "1,2,3", "[1, 2, 3]"},
		{"take nothing", "Take While", "9,1,2", "[]"},
		{"drop everything", "Drop While", "1,2,3", "[]"},
		{"drop nothing", "Drop While", "9,1,2", "[9, 1, 2]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := intsPrelude +
				"Cursed Technique: " + c.prim + "\n" +
				"    Using: (x) -> x < 4\n"
			v, _ := runPipeline(t, src, c.input)
			if got := ir.FormatValue(v); got != c.want {
				t.Fatalf("%s over %s: got %s want %s", c.prim, c.input, got, c.want)
			}
		})
	}
}

func TestTakeWhilePrefixIsCopied(t *testing.T) {
	// The prefix must not alias the input: concat appending onto a shared
	// backing array would otherwise overwrite the element just past the cut.
	src := intsPrelude +
		"Cursed Technique: Take While\n" +
		"    Using: (x) -> x < 4\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (xs) -> sum(concat(xs, list(100)))\n"
	v, _ := runPipeline(t, src, "1,2,9,3")
	if v.(int64) != 103 {
		t.Fatalf("concat onto a Take While result: got %v want 103", v)
	}
}

func TestPrefixWhileRejectsNonPredicates(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Take While\n" +
		"    Using: (x) -> x + 1\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "must return Bool") {
		t.Fatalf("expected a predicate-type error, got %v", err)
	}
}

func TestTakeWhileIsNotTheWhileLoop(t *testing.T) {
	// `While` is a Simple Domain loop kind, and keyword inference checks loop
	// shapes before the registry — Take/Drop While must survive that.
	src := "stdin\n" +
		"Split Text by \",\"\n" +
		"Convert List to Integers\n" +
		"Take While\n" +
		"    Using: (x) -> x < 4\n" +
		"Drop While\n" +
		"    Using: (x) -> x < 2\n"
	v, _ := runPipeline(t, src, "1,2,9,3")
	if got := ir.FormatValue(v); got != "[2]" {
		t.Fatalf("keyword-free Take/Drop While: got %s want [2]", got)
	}
}

// ---------------------------------------------------------------------------
// Chunk
// ---------------------------------------------------------------------------

func TestChunkKeepsTheShortFinalBlock(t *testing.T) {
	src := intsPrelude + "Cursed Technique: Chunk 2\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5")
	if got := ir.FormatValue(v); got != "[[1, 2], [3, 4], [5]]" {
		t.Fatalf("chunk 2: got %s", got)
	}
}

func TestChunkVersusWindowStepping(t *testing.T) {
	// The difference that motivates Chunk: `Window 2 2` drops the trailing 5.
	windowed, _ := runPipeline(t, intsPrelude+"Cursed Technique: Window 2 2\n", "1,2,3,4,5")
	chunked, _ := runPipeline(t, intsPrelude+"Cursed Technique: Chunk 2\n", "1,2,3,4,5")
	if ir.FormatValue(windowed) != "[[1, 2], [3, 4]]" {
		t.Fatalf("window 2 2: got %s", ir.FormatValue(windowed))
	}
	if ir.FormatValue(chunked) != "[[1, 2], [3, 4], [5]]" {
		t.Fatalf("chunk 2: got %s", ir.FormatValue(chunked))
	}
}

func TestChunkEdgeCases(t *testing.T) {
	big, _ := runPipeline(t, intsPrelude+"Cursed Technique: Chunk 9\n", "1,2")
	if got := ir.FormatValue(big); got != "[[1, 2]]" {
		t.Fatalf("oversized chunk: got %s want [[1, 2]]", got)
	}
	one, _ := runPipeline(t, intsPrelude+"Cursed Technique: Chunk 1\n", "1,2")
	if got := ir.FormatValue(one); got != "[[1], [2]]" {
		t.Fatalf("chunk 1: got %s", got)
	}
	if _, err := resolveSrc(t, intsPrelude+"Cursed Technique: Chunk 0\n"); err == nil ||
		!strings.Contains(err.Error(), ">= 1") {
		t.Fatalf("expected a size error, got %v", err)
	}
	if _, err := resolveSrc(t, intsPrelude+"Cursed Technique: Chunk\n"); err == nil ||
		!strings.Contains(err.Error(), "requires a size") {
		t.Fatalf("expected a missing-size error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Partition
// ---------------------------------------------------------------------------

func TestPartitionSplitsInOrder(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Partition\n" +
		"    Using: (x) -> x > 2\n"
	v, _ := runPipeline(t, src, "1,5,2,4,3")
	if got := ir.FormatValue(v); got != "[[5, 4, 3], [1, 2]]" {
		t.Fatalf("partition: got %s want [[5, 4, 3], [1, 2]]", got)
	}
}

func TestPartitionHalvesAreReachable(t *testing.T) {
	base := intsPrelude +
		"Cursed Technique: Partition\n" +
		"    Using: (x) -> x > 2\n"
	matched, _ := runPipeline(t, base+"Cursed Technique: Take Item 0\nMaximum Technique: Sum\n", "1,5,2,4,3")
	if matched.(int64) != 12 {
		t.Fatalf("Take Item 0 (matches): got %v want 12", matched)
	}
	rest, _ := runPipeline(t, base+"Cursed Technique: Apply\n    Using: (p) -> sum(last(p))\n", "1,5,2,4,3")
	if rest.(int64) != 3 {
		t.Fatalf("last(p) (rest): got %v want 3", rest)
	}
}

func TestPartitionAgreesWithTwoFilters(t *testing.T) {
	const input = "4,8,15,16,23,42"
	part, _ := runPipeline(t, intsPrelude+
		"Cursed Technique: Partition\n"+
		"    Using: (x) -> x > 15\n"+
		"Cursed Technique: Take Item 0\n", input)
	filt, _ := runPipeline(t, intsPrelude+
		"Cursed Technique: Filter\n"+
		"    Using: (x) -> x > 15\n", input)
	if ir.FormatValue(part) != ir.FormatValue(filt) {
		t.Fatalf("Partition half %s != Filter %s", ir.FormatValue(part), ir.FormatValue(filt))
	}
}

func TestPartitionOfEmptyGivesTwoEmptyHalves(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Cursed Technique: Filter\n" +
		"    Using: (x) -> x > 100\n" +
		"Cursed Technique: Partition\n" +
		"    Using: (x) -> x > 2\n"
	v, err := runErr(t, src, "1,2")
	if err != nil {
		t.Fatalf("partition of an empty list must not error: %v", err)
	}
	if got := ir.FormatValue(v); got != "[[], []]" {
		t.Fatalf("partition of empty: got %s want [[], []]", got)
	}
}
