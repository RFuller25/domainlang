package lsp

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf16"
)

// The protocol counts columns in UTF-16 code units; the lexer counts them in
// runes. Those agree right up until a character outside the BMP — an emoji is
// one rune and two units — and then every position the server sends on that
// line is short by one per emoji, putting the squiggle under the wrong
// characters and the type hint inside the line instead of after it.

// wantColumn is the correct answer, computed the long way from the line text.
func wantColumn(line string, runeCol int) int {
	r := []rune(line)
	if runeCol-1 > len(r) {
		runeCol = len(r) + 1
	}
	return len(utf16.Encode(r[:runeCol-1]))
}

func TestUTF16ColumnMatchesTheProtocol(t *testing.T) {
	cases := []struct{ name, line string }{
		{"ascii", `    Using: (x) -> x = "abc")`},
		{"bmp", `    Using: (x) -> x = "日本語")`},
		{"astral", `    Using: (x) -> x = "🎌🎌")`},
		{"mixed", `x = "é日🎌" and frobnicate(x)`},
		{"empty", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for col := 1; col <= len([]rune(c.line))+2; col++ {
				if got, want := utf16Column(c.line, col), wantColumn(c.line, col); got != want {
					t.Errorf("column %d: got %d, want %d", col, got, want)
				}
			}
			// A position from an analysis of text the editor has moved on from.
			if got := utf16Column(c.line, 1<<20); got != wantColumn(c.line, 1<<20) {
				t.Errorf("far past the end: got %d", got)
			}
			if got := utf16Column(c.line, -5); got != 0 {
				t.Errorf("before the start: got %d, want 0", got)
			}
		})
	}
}

// diagnosticRange drives a session and returns the range of the first error.
func diagnosticRange(t *testing.T, src string) map[string]any {
	t.Helper()
	results := drive(t, initialize(), didOpen(src))
	for _, d := range findDiagnostics(t, results) {
		m := d.(map[string]any)
		if m["severity"].(float64) == 1 {
			return m["range"].(map[string]any)
		}
	}
	t.Fatalf("no error diagnostic for %q", src)
	return nil
}

func TestDiagnosticRangeIsInCodeUnits(t *testing.T) {
	// The same program three times, differing only in what is inside a string
	// before the error: the stray paren is the error every time, and the range
	// has to land on it every time.
	for _, inside := range []string{"abc", "日本語", "🎌🎌", "é🎌x"} {
		line := `    Using: (x) -> x = "` + inside + `")`
		src := "Cursed Energy: in.txt\nMaximum Technique: Count Matching\n" + line + "\nReveal: stdout\n"
		rng := diagnosticRange(t, src)
		start := int(rng["start"].(map[string]any)["character"].(float64))
		end := int(rng["end"].(map[string]any)["character"].(float64))

		// The paren is the last rune of the line.
		paren := len([]rune(line))
		if want := wantColumn(line, paren); start != want {
			t.Errorf("%q: start = %d, want %d (the paren)", inside, start, want)
		}
		if want := wantColumn(line, paren+1); end != want {
			t.Errorf("%q: end = %d, want %d (just past it)", inside, end, want)
		}
	}
}

func TestInlayHintSitsAfterTheLine(t *testing.T) {
	// A resolving program whose statement line ends in astral characters, so
	// the hint's anchor is a column the two unit systems disagree about.
	src := "Cursed Energy: in.txt\nCursed Technique: Split Text by \"🎌🎌\"\nReveal: stdout\n"
	results := drive(t, initialize(), didOpen(src),
		req(2, "textDocument/inlayHint", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"range": map[string]any{
				"start": map[string]any{"line": 0}, "end": map[string]any{"line": 0}},
		}))
	hints, ok := resultOf(t, results, 2).([]any)
	if !ok || len(hints) == 0 {
		t.Fatal("expected type hints for a program that resolves")
	}
	lines := strings.Split(src, "\n")
	emojiLineHinted := false
	for _, h := range hints {
		pos := h.(map[string]any)["position"].(map[string]any)
		line := lines[int(pos["line"].(float64))]
		want := wantColumn(line, len([]rune(line))+1)
		if got := int(pos["character"].(float64)); got != want {
			t.Errorf("hint at character %d, want %d (the end of %q)", got, want, line)
		}
		if strings.Contains(line, "🎌") {
			emojiLineHinted = true
		}
	}
	if !emojiLineHinted {
		t.Error("the line with the astral characters got no hint, so nothing was proved")
	}
}

// TestCodeActionCoversExactlyTheDocument pins the whole-document edit range.
// Naming a line the document does not have leaves the edit depending on the
// client to clamp it back.
func TestCodeActionCoversExactlyTheDocument(t *testing.T) {
	src := "Cursed Energy: in.txt\nCursed Tecnique: Split Text by \"\\n\"\nReveal: stdout\n"
	results := drive(t, initialize(), didOpen(src),
		req(2, "textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"range": map[string]any{
				"start": map[string]any{"line": 0, "character": 0},
				"end":   map[string]any{"line": 0, "character": 0}},
		}))
	actions, ok := resultOf(t, results, 2).([]any)
	if !ok || len(actions) == 0 {
		t.Fatal("expected a quick fix for the misspelled keyword")
	}
	edit := actions[0].(map[string]any)["edit"].(map[string]any)
	changes := edit["changes"].(map[string]any)[uri].([]any)
	rng := changes[0].(map[string]any)["range"].(map[string]any)
	end := rng["end"].(map[string]any)

	lines := strings.Split(src, "\n")
	lastLine := len(lines) - 1
	if got := int(end["line"].(float64)); got != lastLine {
		t.Errorf("edit ends on line %d, but the document's last line is %d", got, lastLine)
	}
	if got := int(end["character"].(float64)); got != wantColumn(lines[lastLine], len([]rune(lines[lastLine]))+1) {
		t.Errorf("edit ends at character %d, want the end of the last line", got)
	}
}

// TestEveryPositionSentIsInsideTheDocument sweeps the requests that answer with
// a position, over documents that are wrong in the ways an editor sees, and
// checks each one names a place the document has.
func TestEveryPositionSentIsInsideTheDocument(t *testing.T) {
	for _, c := range hostile {
		if len(c.text) > 1<<16 {
			continue // the deep-nesting documents are about the parser, not positions
		}
		t.Run(c.name, func(t *testing.T) {
			results := drive(t, initialize(), didOpen(c.text),
				req(2, "textDocument/inlayHint", map[string]any{
					"textDocument": map[string]any{"uri": uri},
					"range": map[string]any{
						"start": map[string]any{"line": 0}, "end": map[string]any{"line": 0}},
				}))
			lines := strings.Split(c.text, "\n")
			var check func(v any)
			check = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					if l, ok := x["line"].(float64); ok {
						if c, ok := x["character"].(float64); ok {
							if int(l) < 0 || int(l) >= len(lines) {
								t.Errorf("position on line %v of a %d-line document", l, len(lines))
								return
							}
							if units := wantColumn(lines[int(l)], len([]rune(lines[int(l)]))+1); int(c) > units {
								t.Errorf("position at character %v of a %d-unit line %q", c, units, lines[int(l)])
							}
						}
					}
					for _, vv := range x {
						check(vv)
					}
				case []any:
					for _, vv := range x {
						check(vv)
					}
				}
			}
			for _, r := range results {
				var m map[string]any
				b, _ := json.Marshal(r)
				_ = json.Unmarshal(b, &m)
				check(m)
			}
		})
	}
}
