package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

func TestWindow(t *testing.T) {
	src := intsPrelude + "Cursed Technique: Window 3\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5")
	if got := ir.FormatValue(v); got != "[[1, 2, 3], [2, 3, 4], [3, 4, 5]]" {
		t.Fatalf("window 3: got %s", got)
	}
	// 2021 D1 shape: count increasing windows via sums.
	incr := intsPrelude +
		"Cursed Technique: Window 2\n" +
		"Maximum Technique: Count Matching\n" +
		"    Using: (w) -> last(w) > first(w)\n"
	v, _ = runPipeline(t, incr, "199,200,208,210,200,207,240,269,260,263")
	if v.(int64) != 7 {
		t.Fatalf("2021 D1 increases: got %v want 7", v)
	}
}

func TestWindowStepAndBounds(t *testing.T) {
	src := intsPrelude + "Cursed Technique: Window 2 2\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5")
	if got := ir.FormatValue(v); got != "[[1, 2], [3, 4]]" {
		t.Fatalf("window 2 step 2: got %s", got)
	}
	// A window larger than the list yields no windows.
	big := intsPrelude + "Cursed Technique: Window 9\n" + "Maximum Technique: Count\n"
	v, _ = runPipeline(t, big, "1,2")
	if v.(int64) != 0 {
		t.Fatalf("oversized window: got %v want 0", v)
	}
	bad := intsPrelude + "Cursed Technique: Window 0\n"
	if _, err := runErr(t, bad, "1"); err == nil || !strings.Contains(err.Error(), ">= 1") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestFlatten(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Shikigami: Blocks\n" +
		"Cursed Technique: Flatten\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "a\nb\n\nc")
	if v.(int64) != 3 {
		t.Fatalf("flatten count: got %v want 3", v)
	}
}

func TestEnumerate(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Enumerate\n" +
		"Cursed Technique: Map Each\n" +
		"    Using: (p) -> prow(p) * 100 + pcol(p)\n" // index*100 + value
	v, _ := runPipeline(t, src, "7,8,9")
	got, _ := ir.AsIntSlice(v)
	if len(got) != 3 || got[0] != 7 || got[1] != 108 || got[2] != 209 {
		t.Fatalf("enumerate: got %v want [7 108 209]", got)
	}
}

func TestCountBy(t *testing.T) {
	src := intsPrelude +
		"Maximum Technique: Count By\n" +
		"    Using: (n) -> n / 10\n"
	v, _ := runPipeline(t, src, "1,12,15,23,9")
	if got := ir.FormatValue(v); got != "{0: 2, 1: 2, 2: 1}" {
		t.Fatalf("count by: got %s", got)
	}
}

func TestMinByMaxBy(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{w:word} {n:int}\"\n" +
		"Maximum Technique: Max By\n" +
		"    Using: (r) -> r.n\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (r) -> r.w\n"
	v, _ := runPipeline(t, src, "low 1\nhigh 9\nmid 5")
	if v != "high" {
		t.Fatalf("max by: got %v want high", v)
	}
	minSrc := intsPrelude +
		"Maximum Technique: Min By\n" +
		"    Using: (n) -> abs(n - 10)\n"
	v, _ = runPipeline(t, minSrc, "1,8,20")
	if v.(int64) != 8 {
		t.Fatalf("min by: got %v want 8", v)
	}
	empty := intsPrelude +
		"Cursed Technique: Filter\n" +
		"    Using: (n) -> n > 100\n" +
		"Maximum Technique: Min By\n" +
		"    Using: (n) -> n\n"
	if _, err := runErr(t, empty, "1,2"); err == nil ||
		!strings.Contains(err.Error(), "empty list is undefined") {
		t.Fatalf("expected empty-list error, got %v", err)
	}
}

func TestSortByStable(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Domain Expansion: Sort By\n" +
		"    Using: (s) -> occurrences(s, \"a\")\n"
	// Keys: bb=0, aa=2, ba=1, ab=1, cc=0 — stable within equal keys.
	v, _ := runPipeline(t, src, "bb,aa,ba,ab,cc")
	if got := ir.FormatValue(v); got != "[bb, cc, ba, ab, aa]" {
		t.Fatalf("sort by: got %s", got)
	}
	desc := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Domain Expansion: Sort By, Descending\n" +
		"    Using: (n) -> n * n\n"
	v, _ = runPipeline(t, desc, "-3,1,2")
	if got := ir.FormatValue(v); got != "[-3, 2, 1]" {
		t.Fatalf("sort by desc: got %s", got)
	}
}

func TestDifferenceStandalone(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \"\"\n" +
		"Maximum Technique: Difference\n"
	v, _ := runPipeline(t, src, "abcd\nbd\nc")
	if got := ir.FormatValue(v); got != "{a}" {
		t.Fatalf("difference: got %s want {a}", got)
	}
}

func TestZipChannels(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \";\"\n" +
		"\n" +
		"Channel \"a\":\n" +
		"    Cursed Technique: Take Item 0\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"\n" +
		"Channel \"b\":\n" +
		"    Cursed Technique: Take Item 1\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"\n" +
		"Maximum Technique: Zip\n" +
		"    From: a, b\n" +
		"Cursed Technique: Map Each\n" +
		"    Using: (p) -> prow(p) * pcol(p)\n" +
		"Maximum Technique: Sum\n"
	// Zip truncates to the shorter list: (1*4) + (2*5) = 14.
	v, _ := runPipeline(t, src, "1,2,3;4,5")
	if v.(int64) != 14 {
		t.Fatalf("zip dot product: got %v want 14", v)
	}
}

func TestBitBuiltins(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (n) -> band(n, 12) + bor(n, 1) * 100 + bxor(n, 5) * 10000 + shl(n, 2) + shr(n, 1)\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "6")
	// band(6,12)=4, bor(6,1)=7, bxor(6,5)=3, shl(6,2)=24, shr(6,1)=3 → 4+700+30000+24+3.
	if v.(int64) != 30731 {
		t.Fatalf("bit ops: got %v want 30731", v)
	}
	bin := "Cursed Energy: stdin\n" +
		"Shikigami: Lines\n" +
		"Cursed Technique: Map Each\n" +
		"    Using: (s) -> frombin(s)\n" +
		"Maximum Technique: Sum\n"
	v, _ = runPipeline(t, bin, "101\n11")
	if v.(int64) != 8 {
		t.Fatalf("frombin: got %v want 8", v)
	}
}
