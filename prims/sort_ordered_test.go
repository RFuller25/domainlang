package prims

import (
	"testing"

	"domain/ir"
)

// Sort over Text and Sort By over any ordered key. v0.4 could sort only
// List<Int> (plus the Float arm) and key only by Int, which left alphabetical
// sorting and multi-level tiebreaks inexpressible.
func TestSortOverText(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Shikigami: Lines\n" +
		"Domain Expansion: Sort\n"
	v, _ := runPipeline(t, src, "pear\napple\nfig\nBanana")
	// Capital B sorts before the lowercase run: this is byte order, the same
	// order Go's < gives, which is what keeps both backends identical.
	if got := ir.FormatValue(v); got != "[Banana, apple, fig, pear]" {
		t.Fatalf("Sort over Text: got %s", got)
	}
}

func TestSortOverTextDescending(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Shikigami: Lines\n" +
		"Domain Expansion: Sort, Descending\n"
	v, _ := runPipeline(t, src, "pear\napple\nfig")
	if got := ir.FormatValue(v); got != "[pear, fig, apple]" {
		t.Fatalf("Sort Descending over Text: got %s", got)
	}
}

func TestSortByTupleKeyIsLexicographic(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Shikigami: Lines\n" +
		"Domain Expansion: Sort By\n" +
		"    Using: (s) -> tuple(length(s), s)\n"
	v, _ := runPipeline(t, src, "pear\nfig\napple\nkiwi\nbanana")
	if got := ir.FormatValue(v); got != "[fig, kiwi, pear, apple, banana]" {
		t.Fatalf("Sort By tuple key: got %s", got)
	}
}

// Equal keys keep input order — the property that makes a tuple tiebreak
// composable with a following sort.
func TestSortByIsStable(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Shikigami: Lines\n" +
		"Domain Expansion: Sort By\n" +
		"    Using: (s) -> length(s)\n"
	v, _ := runPipeline(t, src, "bb\naa\ncc\na")
	if got := ir.FormatValue(v); got != "[a, bb, aa, cc]" {
		t.Fatalf("Sort By stability: got %s", got)
	}
}

func TestSortByTextKey(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Shikigami: Lines\n" +
		"Domain Expansion: Sort By\n" +
		"    Using: (s) -> charat(s, 1)\n"
	v, _ := runPipeline(t, src, "xc\nya\nzb")
	if got := ir.FormatValue(v); got != "[ya, zb, xc]" {
		t.Fatalf("Sort By Text key: got %s", got)
	}
}

// A key type with no ordering is still refused, and the message names the
// types that are allowed rather than only saying "Int".
func TestSortByRejectsUnorderedKey(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Shikigami: Lines\n" +
		"Domain Expansion: Sort By\n" +
		"    Using: (s) -> startswith(s, \"a\")\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected a resolve error for a Bool sort key")
	}
	if got := err.Error(); !contains(got, "ordered type") {
		t.Fatalf("error should name the ordered types, got: %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
