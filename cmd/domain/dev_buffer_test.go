package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// The load-bearing assumption
// ---------------------------------------------------------------------------

// Per-line highlighting only works if a line of an indentation-sensitive
// language means something on its own. Every line of every program in the
// repository is the test for that — if a continuation line like
// `    Using: (r) -> r.g * r.g` needed the statement above it to lex, the
// editor would be back to whole-file lexing and to one open quote un-painting
// the file while you type the line containing it.
func TestEveryLineOfEveryProgramLexesAlone(t *testing.T) {
	programs := devCorpus(t)
	var checked, blank int
	for _, path := range programs {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for n, line := range strings.Split(string(src), "\n") {
			if strings.TrimSpace(line) == "" {
				blank++
				continue
			}
			faces := facesFor(line)
			if len(faces) != len(line) {
				t.Fatalf("%s:%d face/byte mismatch", path, n+1)
			}
			if devAllPlain(faces) {
				t.Errorf("%s:%d did not lex alone, so it would render unpainted:\n\t%q",
					path, n+1, line)
			}
			checked++
		}
	}
	t.Logf("%d non-blank lines across %d programs, %d blank lines skipped",
		checked, len(programs), blank)
}

// ---------------------------------------------------------------------------
// The property that makes a cursor placeable
// ---------------------------------------------------------------------------

// Painting must add no visible width. This is what a cursor rides on: if a
// highlighted line is exactly as wide as the plain line, a cursor at byte
// offset N lands under the Nth character and no styled string is ever cut. The
// gutter, the end-of-line type hints and the diagnostic markers all stand on
// the same property.
func TestDevHighlightingIsWidthPreservingAcrossTheCorpus(t *testing.T) {
	for _, path := range devCorpus(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for n, line := range strings.Split(string(src), "\n") {
			painted := renderLine(line, -1)
			if got, want := ansi.StringWidth(painted), ansi.StringWidth(line); got != want {
				t.Errorf("%s:%d width changed under paint: got %d want %d\n\t%q",
					path, n+1, got, want, line)
			}
			if plain := ansi.Strip(painted); plain != line {
				t.Errorf("%s:%d text changed under paint:\n got: %q\nwant: %q",
					path, n+1, plain, line)
			}
		}
	}
}

// The cursor is one more face on one more rune, so the property has to hold
// with it switched on — including at end of line, where it is a trailing block
// and is expected to add exactly one cell.
func TestDevCursorIsWidthPreservingAtEveryColumn(t *testing.T) {
	lines := []string{
		`Cursed Energy: input.txt`,
		`Cursed Technique: Split Text by "\n\n"`,
		`    Using: (r) -> r.g * r.g`,
		`Maximum Technique: Sum   # fold it down`,
		`    Using: "{name:word} grade {g:int}"`,
	}
	for _, line := range lines {
		for col := 0; col <= len(line); col++ {
			if col < len(line) && !utf8.RuneStart(line[col]) {
				continue
			}
			painted := renderLine(line, col)
			want := ansi.StringWidth(line)
			if col >= len(line) {
				want++ // the trailing cursor block
			}
			if got := ansi.StringWidth(painted); got != want {
				t.Errorf("cursor at %d changed width: got %d want %d\n\t%q", col, got, want, line)
			}
			if col < len(line) {
				r, w := utf8.DecodeRuneInString(line[col:])
				if got := ansi.Strip(painted)[col : col+w]; got != string(r) {
					t.Errorf("cursor at %d ate the character: got %q want %q", col, got, string(r))
				}
			}
		}
	}
}

// A cursor inside a token has to split it — the case that would be painful
// with ANSI strings and is trivial with faces.
func TestDevCursorSplitsATokenWithoutDisturbingIt(t *testing.T) {
	const line = `Cursed Technique: Sum`
	const col = 11 // the 'n' in "Technique" — mid-word, mid-keyword-phrase

	if f := facesFor(line)[col]; f != faceKeyword {
		t.Fatalf("expected a keyword byte at %d, got face %v", col, f)
	}
	painted := renderLine(line, col)
	if plain := ansi.Strip(painted); plain != line {
		t.Errorf("text changed: got %q want %q", plain, line)
	}
	// The word is now three painted fragments: keyword, cursor, keyword.
	if !strings.Contains(painted, faceStyle(faceCursor).Render("n")) {
		t.Errorf("no cursor fragment in %q", painted)
	}
	if !strings.Contains(painted, faceStyle(faceKeyword).Render("Tech")) ||
		!strings.Contains(painted, faceStyle(faceKeyword).Render("ique")) {
		t.Errorf("the keyword did not survive being split around the cursor: %q", painted)
	}
}

// ---------------------------------------------------------------------------
// The lexer earns its keep
// ---------------------------------------------------------------------------

func TestDevFacesDistinguishKeywordsFromTheirOwnSpelling(t *testing.T) {
	const line = `Cursed Technique: Split Text by "Cursed Technique"`
	faces := facesFor(line)

	if faces[0] != faceKeyword {
		t.Errorf("leading keyword not painted as one (face %v)", faces[0])
	}
	if inString := strings.Index(line, `"Cursed`) + 1; faces[inString] != faceString {
		t.Errorf("a keyword inside a string was painted as face %v, want string", faces[inString])
	}
}

func TestDevFacesTreatAHashInAStringAsText(t *testing.T) {
	const line = `Cursed Technique: Split Text by "#"`
	if h := strings.Index(line, "#"); facesFor(line)[h] != faceString {
		t.Errorf("'#' inside a string painted as face %v, want string", facesFor(line)[h])
	}

	const commented = `Maximum Technique: Sum  # then stop`
	if h := strings.Index(commented, "#"); facesFor(commented)[h] != faceComment {
		t.Errorf("a real comment painted as face %v, want comment", facesFor(commented)[h])
	}
}

func TestDevIndentedArgumentLabelsGetTheirOwnFace(t *testing.T) {
	for _, line := range []string{`    Using: (r) -> r.g`, `    Mode: First`, `    Seed: 0`} {
		label := strings.Index(line, strings.TrimSpace(line))
		if f := facesFor(line)[label]; f != faceArgName {
			t.Errorf("%q: label painted as face %v, want argName", line, f)
		}
	}
}

// The editor and the REPL must agree about what a token is, or the same
// program would look like two different programs in a transcript and in the
// editor. They share faceFor; this is the test that keeps them sharing it.
func TestDevAndReplHighlightingAgree(t *testing.T) {
	for _, line := range []string{
		`Cursed Energy: input.txt`,
		`Cursed Technique: Split Text by "\n\n"`,
		`    Using: (r) -> r.g * 2`,
		`Maximum Technique: Sum  # done`,
	} {
		if got, want := renderLine(line, -1), highlightSource(line, true); got != want {
			t.Errorf("editor and REPL disagree about %q:\n editor: %q\n   repl: %q", line, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// The failure mode this design exists to contain
// ---------------------------------------------------------------------------

// Mid-keystroke a line is often unlexable — an open quote, an unclosed paren.
// The REPL's whole-program highlighter returns the entire source unpainted
// when that happens. Per-line, the damage must stop at the line being typed.
func TestDevABrokenLineDoesNotUnpaintItsNeighbours(t *testing.T) {
	b := newDevBuffer("Cursed Energy: input.txt\nCursed Technique: Split Text by \"\nMaximum Technique: Sum")

	if !devAllPlain(facesFor(b.lines[1])) {
		t.Fatalf("expected the half-typed line to be unpaintable, but it lexed")
	}
	for _, i := range []int{0, 2} {
		if devAllPlain(facesFor(b.lines[i])) {
			t.Errorf("line %d lost its paint because line 2 is mid-edit: %q", i+1, b.lines[i])
		}
	}
	// The contrast that motivated per-line lexing: whole-file, one open quote
	// takes every line with it.
	if highlightSource(b.text(), true) != b.text() {
		t.Errorf("expected the whole-program highlighter to give up on this source")
	}
}

// ---------------------------------------------------------------------------
// Editing
// ---------------------------------------------------------------------------

// Typing a program one keystroke at a time through the model must produce
// exactly that program — the round trip that says the buffer, the cursor
// arithmetic and the message plumbing all agree.
func TestDevTypingAProgramThroughTheModelReproducesIt(t *testing.T) {
	const want = "Cursed Energy: input.txt\nCursed Technique: Map Each\n    Using: (x) -> x * 2\nReveal: stdout"

	m := newDevModel("")
	for _, r := range want {
		if r == '\n' {
			m = devPress(m, "enter")
			continue
		}
		m = devPress(m, string(r))
	}
	// Enter carries indentation forward, so lines typed after an indented one
	// arrive already indented — as they would for a real typist, who then has
	// to remove it.
	got := m.buf.text()
	got = strings.ReplaceAll(got, "\n        Using:", "\n    Using:")
	got = strings.ReplaceAll(got, "\n    Reveal:", "\nReveal:")
	if got != want {
		t.Errorf("round trip failed:\n got: %q\nwant: %q", got, want)
	}
}

func TestDevEditingKeepsTheCursorAndTextConsistent(t *testing.T) {
	m := newDevModel("Cursed Technique: Sum\nReveal: stdout")

	m = devPress(m, "end")
	for range 3 {
		m = devPress(m, "backspace")
	}
	for _, r := range "Count" {
		m = devPress(m, string(r))
	}
	if got, want := m.buf.lines[0], "Cursed Technique: Count"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got, want := m.buf.col, len("Cursed Technique: Count"); got != want {
		t.Errorf("cursor at %d, want %d", got, want)
	}

	m = devPress(m, "down")
	m = devPress(m, "home")
	m = devPress(m, "backspace")
	if len(m.buf.lines) != 1 {
		t.Fatalf("expected the lines to join, got %d lines", len(m.buf.lines))
	}
	if got, want := m.buf.text(), "Cursed Technique: CountReveal: stdout"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// Vertical motion through a short line must not forget the column it started
// in — the bug every hand-rolled editor ships at least once.
func TestDevVerticalMotionRemembersTheGoalColumn(t *testing.T) {
	m := newDevModel("Cursed Technique: Extract Integers\nSum\nMaximum Technique: Count Distinct Values")
	m = devPress(m, "end")
	want := m.buf.col

	m = devPress(m, "down")
	if m.buf.col != len("Sum") {
		t.Errorf("on the short line the cursor should clamp to %d, got %d", len("Sum"), m.buf.col)
	}
	m = devPress(m, "down")
	if m.buf.col != want {
		t.Errorf("column not restored past the short line: got %d want %d", m.buf.col, want)
	}
}

func TestDevEnterCarriesIndentation(t *testing.T) {
	m := newDevModel("Cursed Technique: Map Each\n    Using: (x) -> x")
	m = devPress(m, "down")
	m = devPress(m, "end")
	m = devPress(m, "enter")
	if got, want := m.buf.line(), "    "; got != want {
		t.Errorf("new line started at %q, want %q", got, want)
	}
	if got, want := m.buf.col, 4; got != want {
		t.Errorf("cursor at %d, want %d", got, want)
	}
}

// Opening a file and saving it back must not change it. A trailing newline is
// a line terminator, not an empty last line, and getting that wrong grows a
// file by a blank line every time it is opened.
func TestDevOpenSaveRoundTripsEveryProgram(t *testing.T) {
	for _, path := range devCorpus(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		want := strings.TrimSuffix(string(src), "\n")
		if got := newDevBuffer(string(src)).text(); got != want {
			t.Errorf("%s did not survive an open/save round trip", path)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func devPress(m devModel, key string) devModel {
	var msg tea.KeyPressMsg
	switch key {
	case "left":
		msg = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		msg = tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "home":
		msg = tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		msg = tea.KeyPressMsg{Code: tea.KeyEnd}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	default:
		r, _ := utf8.DecodeRuneInString(key)
		msg = tea.KeyPressMsg{Code: r, Text: key}
	}
	next, _ := m.Update(msg)
	return next.(devModel)
}

func devAllPlain(faces []face) bool {
	for _, f := range faces {
		if f != facePlain {
			return false
		}
	}
	return true
}

// devCorpus is every Domain program in the repository — the examples and the
// challenges — which is what makes the properties above tests rather than
// demonstrations.
func devCorpus(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{"../../examples", "../../challenges"} {
		matches, err := filepath.Glob(filepath.Join(dir, "*.domain"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		out = append(out, matches...)
	}
	if len(out) == 0 {
		t.Fatal("no programs found; the corpus is what gives these tests their teeth")
	}
	return out
}
