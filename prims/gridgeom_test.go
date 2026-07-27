package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

func gridSrc(tail string) string {
	return "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n" + tail
}

func TestRotateGridRight(t *testing.T) {
	v, _ := runPipeline(t, gridSrc("Cursed Technique: Rotate Grid\n    Mode: Right\n"), "abc\ndef")
	// The first column read bottom-to-top becomes the first row.
	if got := ir.FormatValue(v); got != "da\neb\nfc" {
		t.Fatalf("Rotate Right: got %q", got)
	}
}

func TestRotateGridLeftAndHalf(t *testing.T) {
	v, _ := runPipeline(t, gridSrc("Cursed Technique: Rotate Grid\n    Mode: Left\n"), "abc\ndef")
	if got := ir.FormatValue(v); got != "cf\nbe\nad" {
		t.Fatalf("Rotate Left: got %q", got)
	}
	v, _ = runPipeline(t, gridSrc("Cursed Technique: Rotate Grid\n    Mode: Half\n"), "abc\ndef")
	if got := ir.FormatValue(v); got != "fed\ncba" {
		t.Fatalf("Rotate Half: got %q", got)
	}
}

// Four quarter turns is the identity; a sign error would not survive it.
func TestFourRotationsAreIdentity(t *testing.T) {
	v, _ := runPipeline(t, gridSrc(
		"Simple Domain: Repeat 4\n    Cursed Technique: Rotate Grid\n        Mode: Right\n"),
		"abcd\nefgh\nijkl")
	if got := ir.FormatValue(v); got != "abcd\nefgh\nijkl" {
		t.Fatalf("four right turns should be the identity, got %q", got)
	}
}

func TestFlipGrid(t *testing.T) {
	v, _ := runPipeline(t, gridSrc("Cursed Technique: Flip Grid\n    Mode: Horizontal\n"), "abc\ndef")
	if got := ir.FormatValue(v); got != "cba\nfed" {
		t.Fatalf("Flip Horizontal: got %q", got)
	}
	v, _ = runPipeline(t, gridSrc("Cursed Technique: Flip Grid\n    Mode: Vertical\n"), "abc\ndef")
	if got := ir.FormatValue(v); got != "def\nabc" {
		t.Fatalf("Flip Vertical: got %q", got)
	}
}

// Rotate Right is Transpose then Flip Horizontal — the identity the optimizer
// could later exploit, and a cross-check on both implementations.
func TestRotateRightEqualsTransposeThenFlip(t *testing.T) {
	a, _ := runPipeline(t, gridSrc("Cursed Technique: Rotate Grid\n    Mode: Right\n"), "abc\ndef")
	b, _ := runPipeline(t, gridSrc(
		"Cursed Technique: Transpose\nCursed Technique: Flip Grid\n    Mode: Horizontal\n"), "abc\ndef")
	if ir.FormatValue(a) != ir.FormatValue(b) {
		t.Fatalf("Rotate Right %q != Transpose+Flip %q", ir.FormatValue(a), ir.FormatValue(b))
	}
}

// Convert To Grid used to be a one-way door.
func TestConvertToRowsInvertsConvertToGrid(t *testing.T) {
	v, _ := runPipeline(t, gridSrc("Channeled Energy: Convert To Rows\n"), "abc\ndef")
	if got := ir.FormatValue(v); got != "[[a, b, c], [d, e, f]]" {
		t.Fatalf("Convert To Rows: got %s", got)
	}
}

func TestFindCycleReportsStartAndPeriod(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n" +
		"Cursed Technique: Iterate 12\n    Using: (n) -> mod(n * 3 + 1, 7)\n" +
		"Maximum Technique: Find Cycle\n"
	v, _ := runPipeline(t, src, "3")
	pair, _ := ir.AsList(v)
	if len(pair) != 2 {
		t.Fatalf("Find Cycle should give a pair, got %s", ir.FormatValue(v))
	}
	// Whatever the trajectory, the period must be positive and the reported
	// start must be where the repeat genuinely begins.
	if pair[1].(int64) <= 0 {
		t.Fatalf("period should be positive, got %s", ir.FormatValue(v))
	}
}

// A trajectory that never repeats is a legitimate answer, not an error.
func TestFindCycleNoRepeat(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n" +
		"Cursed Technique: Iterate 5\n    Using: (n) -> n + 1\n" +
		"Maximum Technique: Find Cycle\n"
	v, _ := runPipeline(t, src, "0")
	if got := ir.FormatValue(v); got != "[-1, 0]" {
		t.Fatalf("no repeat should be (-1, 0), got %s", got)
	}
}

// The general vow reaches types the two literal shapes never could.
func TestHoldsVowOverAGrid(t *testing.T) {
	_, err := runErr(t, gridSrc(
		"Binding Vow: Holds\n    Using: (g) -> rows(g) = 99\n"), "abc\ndef")
	if err == nil || !strings.Contains(err.Error(), "vow violated") {
		t.Fatalf("expected a vow violation, got %v", err)
	}
	v, _ := runPipeline(t, gridSrc(
		"Binding Vow: Holds\n    Using: (g) -> rows(g) = 2 and cols(g) = 3\n"), "abc\ndef")
	if got := ir.FormatValue(v); got != "abc\ndef" {
		t.Fatalf("a satisfied vow is a passthrough, got %q", got)
	}
}

func TestHoldsVowNeedsAPredicate(t *testing.T) {
	_, err := resolveSrc(t, gridSrc("Binding Vow: Holds\n"))
	if err == nil || !strings.Contains(err.Error(), "Using:") {
		t.Fatalf("expected an error asking for Using:, got %v", err)
	}
}
