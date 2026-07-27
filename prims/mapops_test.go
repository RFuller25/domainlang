package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// A Map could be produced and rendered but never reshaped, so "which key
// occurred most?" — the most common AoC follow-up to Count By — had no
// spelling. These cover the four operations that close that.

func countBySrc(tail string) string {
	return "Cursed Energy: stdin\n" +
		"Shikigami: Lines\n" +
		"Maximum Technique: Count By\n" +
		"    Using: (s) -> charat(s, 0)\n" + tail
}

func TestConvertToEntriesKeepsInsertionOrder(t *testing.T) {
	v, _ := runPipeline(t, countBySrc("Channeled Energy: Convert To Entries\n"),
		"bee\napple\nant\ncow")
	// Insertion order is the order a Map already renders in, so the
	// conversion never silently reshuffles.
	if got := ir.FormatValue(v); got != "[[b, 1], [a, 2], [c, 1]]" {
		t.Fatalf("Convert To Entries: got %s", got)
	}
}

func TestMapValuesKeepsKeysAndOrder(t *testing.T) {
	v, _ := runPipeline(t, countBySrc(
		"Cursed Technique: Map Values\n    Using: (n) -> n * 10\n"),
		"bee\napple\nant")
	if got := ir.FormatValue(v); got != "{b: 10, a: 20}" {
		t.Fatalf("Map Values: got %s", got)
	}
}

func TestFilterEntriesSeesKeyAndValue(t *testing.T) {
	v, _ := runPipeline(t, countBySrc(
		"Cursed Technique: Filter Entries\n    Using: (k, n) -> n > 1 or k = \"c\"\n"),
		"bee\napple\nant\ncow")
	if got := ir.FormatValue(v); got != "{a: 2, c: 1}" {
		t.Fatalf("Filter Entries: got %s", got)
	}
}

func TestConvertToMapRoundTrips(t *testing.T) {
	v, _ := runPipeline(t, countBySrc(
		"Channeled Energy: Convert To Entries\nChanneled Energy: Convert To Map\n"),
		"bee\napple\nant")
	if got := ir.FormatValue(v); got != "{b: 1, a: 2}" {
		t.Fatalf("entries round trip: got %s", got)
	}
}

// The whole point of Convert To Entries: a Map reaches the list vocabulary,
// where Sort By and Select Top K already live.
func TestMostCommonElementIdiom(t *testing.T) {
	v, _ := runPipeline(t, countBySrc(
		"Channeled Energy: Convert To Entries\n"+
			"Domain Expansion: Sort By, Descending\n"+
			"    Using: (e) -> item(e, 1)\n"+
			"Cursed Technique: Take Item 0\n"+
			"Cursed Technique: Apply\n"+
			"    Using: (e) -> item(e, 0)\n"),
		"bee\napple\nant\ncow\navocado")
	if got := ir.FormatValue(v); got != "a" {
		t.Fatalf("most common first letter should be a, got %s", got)
	}
}

func TestConvertToMapRejectsNonPairs(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\nChanneled Energy: Convert To Map\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected a type error for List<Int>")
	}
	if msg := err.Error(); !strings.Contains(msg, "List<(K, V)>") {
		t.Errorf("error should name the expected shape, got: %s", msg)
	}
}
