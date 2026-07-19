package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

func TestRaggedColumns(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Ragged Columns\n"
	// Lines of lengths 3, 1, 2: column 0 has 3 cells, column 1 has 2, column
	// 2 has 1 — missing cells are skipped, not padded.
	v, _ := runPipeline(t, src, "abc\nd\nef")
	if got := ir.FormatValue(v); got != "[[a, d, e], [b, f], [c]]" {
		t.Fatalf("ragged columns: got %s", got)
	}
}

func TestRaggedColumnsParsesCrateDrawing(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Ragged Columns\n" +
		"Cursed Technique: Take Item 1\n" +
		"Cursed Technique: Filter\n" +
		"    Using: (ch) -> occurrences(\"0123456789 \", ch) = 0\n"
	drawing := "    [D]\n[N] [C]\n[Z] [M] [P]\n 1   2   3 "
	v, _ := runPipeline(t, src, drawing)
	if got := ir.FormatValue(v); got != "[N, Z]" {
		t.Fatalf("stack 1: got %s want [N, Z]", got)
	}
}

func TestJoin(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Maximum Technique: Join\n"
	v, _ := runPipeline(t, src, "C\nM\nZ")
	if v != "CMZ" {
		t.Fatalf("join: got %v want CMZ", v)
	}
	withSep := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Maximum Technique: Join with \", \"\n"
	v, _ = runPipeline(t, withSep, "a\nb")
	if v != "a, b" {
		t.Fatalf("join with sep: got %v", v)
	}
}

func TestFoldOverChannel(t *testing.T) {
	// Seed = the current value (a list built upstream); fold runs over the
	// channel's list. Doubles the seed's single element per channel item.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \";\"\n" +
		"\n" +
		"Channel \"moves\":\n" +
		"    Cursed Technique: Take Item 1\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"\n" +
		"Cursed Technique: Take Item 0\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Maximum Technique: Fold\n" +
		"    From: moves\n" +
		"    Using: (acc, m) -> set(acc, 0, first(acc) + m)\n" +
		"Reveal: stdout\n"
	v, out := runPipeline(t, src, "100,0;1,2,3")
	xs, _ := ir.AsIntSlice(v)
	if len(xs) != 2 || xs[0] != 106 || xs[1] != 0 {
		t.Fatalf("fold over channel: got %v want [106 0]", xs)
	}
	if strings.TrimSpace(out) != "[106, 0]" {
		t.Fatalf("stdout: %q", out)
	}
}

func TestFoldOverErrors(t *testing.T) {
	// Non-list channel.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \";\"\n" +
		"\n" +
		"Channel \"n\":\n" +
		"    Maximum Technique: Count\n" +
		"\n" +
		"Maximum Technique: Fold\n" +
		"    From: n\n" +
		"    Using: (acc, m) -> acc\n"
	_, err := runErr(t, src, "a;b")
	if err == nil || !strings.Contains(err.Error(), "must hold a List") {
		t.Fatalf("expected non-list channel error, got %v", err)
	}
	// Accumulator type mismatch.
	src2 := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \";\"\n" +
		"\n" +
		"Channel \"m\":\n" +
		"    Cursed Technique: Split Each by \",\"\n" +
		"    Cursed Technique: Take Item 0\n" +
		"\n" +
		"Maximum Technique: Fold\n" +
		"    From: m\n" +
		"    Using: (acc, x) -> 42\n"
	_, err = runErr(t, src2, "a;b")
	if err == nil || !strings.Contains(err.Error(), "must return the seed type") {
		t.Fatalf("expected seed-type error, got %v", err)
	}
}

func TestPositionalCountCells(t *testing.T) {
	// Count cells strictly greater than every earlier cell in their row.
	src := digitGrid +
		"Maximum Technique: Count Cells\n" +
		"    Using: (g, r, c) -> c = 0 or max(take(row(g, r), c)) < at(g, r, c)\n"
	v, _ := runPipeline(t, src, "132\n311")
	// Row "132": 1 (edge), 3 (>1), not 2. Row "311": 3 (edge), not 1, not 1.
	if v.(int64) != 3 {
		t.Fatalf("positional count: got %v want 3", v)
	}
}

func TestPositionalMapCells(t *testing.T) {
	src := digitGrid +
		"Cursed Technique: Map Cells\n" +
		"    Using: (g, r, c) -> at(g, r, c) * 100 + r * 10 + c\n"
	v, _ := runPipeline(t, src, "12\n34")
	g := v.(*ir.GridValue)
	want := []int64{100, 201, 310, 411}
	for i, w := range want {
		if g.Cells[i] != w {
			t.Fatalf("cell %d: got %v want %d", i, g.Cells[i], w)
		}
	}
}

func TestCellLambdaArityError(t *testing.T) {
	src := digitGrid +
		"Maximum Technique: Count Cells\n" +
		"    Using: (a, b) -> a > b\n"
	_, err := runErr(t, src, "12\n34")
	if err == nil || !strings.Contains(err.Error(), "1 parameter (the cell) or 3") {
		t.Fatalf("expected arity error, got %v", err)
	}
}

func TestConditionalInPipeline(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (n) -> if n > 2 then n * 10 else n\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1,2,3,4")
	if v.(int64) != 73 { // 1 + 2 + 30 + 40
		t.Fatalf("conditional map: got %v want 73", v)
	}
}
