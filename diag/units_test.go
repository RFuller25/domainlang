package diag

import (
	"strings"
	"testing"

	"domain/token"
)

// A column is a rune count — the lexer advances it once per rune and never on
// a UTF-8 continuation byte — so everything that measures from one has to
// count the same way. Measuring the underlined word in bytes, and finding it
// by indexing the line at a rune column, read the wrong text entirely once a
// line held anything outside ASCII.
func TestUnderlineWidthCountsRunes(t *testing.T) {
	cases := []struct {
		name string
		line string
		col  int // 1-based rune column
		want int
	}{
		{"ascii word", "abc foo bar", 9, 3},
		{"word after multi-byte text", "日本語 foo bar", 9, 3},
		{"word after astral text", "🎌🎌 foo bar", 8, 3},
		{"punctuation", `x = "abc")`, 10, 1},
		{"punctuation after multi-byte text", `x = "日本語")`, 10, 1},
		{"word at the end", "日本語 bar", 5, 3},
		{"past the end of the line", "日本語", 9, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Diagnostic{LineText: c.line, Pos: token.Position{Line: 1, Col: c.col}}
			if got := d.Width(); got != c.want {
				r := []rune(c.line)
				at := ""
				if c.col-1 < len(r) {
					at = string(r[c.col-1:])
				}
				t.Errorf("Width() = %d, want %d (from %q)", got, c.want, at)
			}
		})
	}
}

// TestUnderlineNeverRunsPastTheLine keeps the clamp honest for every column of
// every shape of line, including columns no line has.
func TestUnderlineNeverRunsPastTheLine(t *testing.T) {
	for _, line := range []string{"", "a", "日本語", "🎌x🎌", "abc foo bar", "  indented word"} {
		n := len([]rune(line))
		for col := 1; col <= n+3; col++ {
			d := Diagnostic{LineText: line, Pos: token.Position{Line: 1, Col: col}}
			w := d.Width()
			if w < 1 {
				t.Errorf("%q col %d: width %d", line, col, w)
			}
			if col <= n && col-1+w > n {
				t.Errorf("%q col %d: width %d runs past the %d-rune line", line, col, w, n)
			}
		}
	}
}

// TestExplicitEndColumnIsUsedAsGiven covers the other branch: a diagnostic that
// knows its own span is measured in the same rune columns.
func TestExplicitEndColumnIsUsedAsGiven(t *testing.T) {
	d := Diagnostic{LineText: "日本語 foo bar", Pos: token.Position{Line: 1, Col: 5}, EndCol: 8}
	if got := d.Width(); got != 3 {
		t.Errorf("Width() = %d, want 3", got)
	}
}

// TestNoOpFixIsNotOffered is the repair loop's progress rule. enrichDedent
// reconstructs the enclosing indentation widths from the text above, which is
// an approximation of the lexer's indent stack: it includes blocks that have
// since closed, so it can name the very width the lexer just rejected.
// Aligning a line to where it already is repairs nothing, and offering it as a
// confident fix sent the analyzer around its loop until the round cap stopped
// it — re-lexing the same unchanged source thirty times, on every keystroke.
func TestNoOpFixIsNotOffered(t *testing.T) {
	// A dedent to a width that appears above but is not open at that point.
	src := `Cursed Energy: in.txt
Shikigami: Lines
Channel "a":
    Cursed Technique: Take Item 1
Channel "b":
    Cursed Technique: Take Item 1
        Using: (x) -> x
    Cursed Technique: Take Item 1
Maximum Technique: Sum
Reveal: stdout
`
	for i := range len(src) {
		mutated := src[:i] + src[i+1:]
		r := analyze(t, mutated)
		if r.FixedSrc == mutated && r.Applied > 0 {
			t.Fatalf("reported %d fixes but the source is unchanged (deleted byte %d)", r.Applied, i)
		}
		again := analyze(t, r.FixedSrc)
		if again.Applied > 0 && again.FixedSrc == r.FixedSrc {
			t.Fatalf("repaired source still reports %d fixes that change nothing (deleted byte %d)", again.Applied, i)
		}
	}
}

// TestAppliedCountsWhatChanged pins the number the editor puts in front of the
// user: "apply N automatic fix(es)" has to be the number of repairs the edit
// actually contains.
func TestAppliedCountsWhatChanged(t *testing.T) {
	src := strings.Replace(day1, "Cursed Technique: Split Text", "Cursed Tecnique: Split Text", 1)
	r := analyze(t, src)
	if r.Applied != 1 {
		t.Errorf("Applied = %d, want 1", r.Applied)
	}
	if r.FixedSrc != day1 {
		t.Error("the one fix should have produced the original program")
	}

	clean := analyze(t, day1)
	if clean.Applied != 0 {
		t.Errorf("a clean program reported %d fixes", clean.Applied)
	}
}
