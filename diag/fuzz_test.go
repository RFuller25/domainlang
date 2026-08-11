package diag

import (
	"strings"
	"testing"
)

// FuzzAnalyze drives the whole front end the way an editor does: arbitrary
// text, analyzed as if it were a program being typed.
//
// The assertion is not "does not panic" — Analyze catches panics by design, so
// that a language server survives one — but that the guard never fires. A
// program is allowed to be wrong in any way at all; what it is not allowed to
// do is reach code that cannot cope with being wrong.
func FuzzAnalyze(f *testing.F) {
	seeds := []string{
		"",
		day1,
		"Reveal: stdout\n",
		"Cursed Energy: in.txt\nMaximum Technique: Sum\nReveal: stdout\n",
		// The crash this file exists for, and its neighbours: a binding whose
		// value is constant-folded before anything has typed it.
		miscountedCall("range(5)"),
		miscountedCall("length(range(0, 100000000))"),
		miscountedCall("fill(1099511627776, 0)"),
		miscountedCall("divisors(9223372036854775783)"),
		miscountedCall("padleft(\"1\", 9223372036854775807, \"0\")"),
		miscountedCall("1 / 0"),
		miscountedCall("item(list(), 0)"),
		miscountedCall("charat(\"\", 5)"),
		"Cursed Energy: in.txt\nMaximum Technique: Count Matching\n    Consider n As -9223372036854775808 / -1\n    Using: (x) -> x > n\nReveal: stdout\n",
		// Shapes that walk the tree deeply rather than widely.
		"Maximum Technique: Count Matching\n    Consider n As ((((((((1))))))))\n    Using: (x) -> x > n\n",
		"Shikigami \"X\" (k: List<List<List<Int>>>)\n    Reveal: stdout\n",
		"Simple Domain: Repeat 2\n    Simple Domain: Repeat 2\n        Maximum Technique: Sum\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		r := Analyze("fuzz.domain", src)
		for _, d := range r.Diags {
			if strings.Contains(d.Msg, "internal error") {
				t.Fatalf("front end crashed on %q: %s", src, d.Msg)
			}
		}
	})
}
