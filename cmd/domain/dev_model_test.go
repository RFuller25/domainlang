package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// newTestDevModel returns a model sized like a real terminal, since almost
// everything about the view depends on how much of it there is.
func newTestDevModel(text string) devModel {
	m := newDevModel(text)
	m.width, m.height = 80, 24
	return m
}

func devSend(m devModel, msg tea.Msg) devModel {
	next, _ := m.Update(msg)
	return next.(devModel)
}

func devKey(m devModel, s string) devModel {
	return devSend(m, devKeyMsg(s))
}

// devKeyMsg spells a key the way the runtime would deliver it. Named keys
// carry a Code; anything else is text, which is exactly the distinction the
// editor relies on to keep function keys out of the program.
func devKeyMsg(s string) tea.KeyPressMsg {
	named := map[string]rune{
		"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"enter": tea.KeyEnter, "backspace": tea.KeyBackspace, "tab": tea.KeyTab,
		"esc": tea.KeyEscape, "space": tea.KeySpace,
	}
	// Modifiers peel off first so they compose with named keys: "ctrl+right" is
	// ctrl plus the right arrow, not ctrl plus the letter r.
	var mod tea.KeyMod
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			mod, s = mod|tea.ModCtrl, strings.TrimPrefix(s, "ctrl+")
		case strings.HasPrefix(s, "alt+"):
			mod, s = mod|tea.ModAlt, strings.TrimPrefix(s, "alt+")
		case strings.HasPrefix(s, "shift+"):
			mod, s = mod|tea.ModShift, strings.TrimPrefix(s, "shift+")
		default:
			if code, ok := named[s]; ok {
				return tea.KeyPressMsg{Code: code, Mod: mod}
			}
			if mod != 0 {
				return tea.KeyPressMsg{Code: rune(s[0]), Mod: mod}
			}
			return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
		}
	}
}

// ---------------------------------------------------------------------------
// arguments
// ---------------------------------------------------------------------------

func TestParseDevelopmentArgs(t *testing.T) {
	cases := []struct {
		args      []string
		path      string
		input     string
		shouldErr bool
	}{
		{args: nil},
		{args: []string{"day7.domain"}, path: "day7.domain"},
		{args: []string{"day7.domain", "--input", "day7.txt"}, path: "day7.domain", input: "day7.txt"},
		{args: []string{"--input=day7.txt", "day7.domain"}, path: "day7.domain", input: "day7.txt"},
		{args: []string{"-i", "in.txt"}, input: "in.txt"},
		{args: []string{"--input"}, shouldErr: true},
		{args: []string{"--nope"}, shouldErr: true},
		{args: []string{"a.domain", "b.domain"}, shouldErr: true},
	}
	for _, c := range cases {
		path, opts, err := parseDevelopmentArgs(c.args)
		if c.shouldErr {
			if err == nil {
				t.Errorf("%v: expected an error", c.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v: %v", c.args, err)
			continue
		}
		if path != c.path || opts.Input != c.input {
			t.Errorf("%v: got (%q, %q), want (%q, %q)", c.args, path, opts.Input, c.path, c.input)
		}
	}
}

// An editor without a terminal has nothing to do, and says so rather than
// failing obscurely once it is already painting.
func TestDevelopmentRefusesAPipe(t *testing.T) {
	var out, errOut strings.Builder
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	defer r.Close()

	if code := Development("", devOptions{}, r, &out, &errOut); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "needs a terminal") {
		t.Errorf("unhelpful refusal: %q", errOut.String())
	}
}

// The command family has to recognize it, in both spellings.
func TestDevelopmentIsAnExpansionCommand(t *testing.T) {
	for _, args := range [][]string{
		{"expansion:", "development", "day7.domain"},
		{"expansion: development", "day7.domain"},
	} {
		cmd, rest, ok := expansionInvocation(args)
		if !ok || len(cmd) != 1 || cmd[0] != "development" {
			t.Fatalf("%v: got cmd %v ok %v", args, cmd, ok)
		}
		if len(rest) != 1 || rest[0] != "day7.domain" {
			t.Errorf("%v: got rest %v", args, rest)
		}
	}
}

// ---------------------------------------------------------------------------
// files
// ---------------------------------------------------------------------------

func TestDevSaveWritesAndClearsDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.domain")

	m := newTestDevModel("")
	for _, r := range "Reveal: stdout" {
		m = devKey(m, string(r))
	}
	if !m.dirty {
		t.Fatal("typing did not mark the buffer dirty")
	}

	next, _ := m.save(path)
	m = next.(devModel)
	if m.dirty {
		t.Error("still dirty after a save")
	}
	if m.path != path {
		t.Errorf("path is %q, want %q", m.path, path)
	}

	// The file is terminated, and reading it back gives the same program.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "Reveal: stdout\n"; got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
	if got := newDevBuffer(string(b)).text(); got != "Reveal: stdout" {
		t.Errorf("round trip gave %q", got)
	}
}

// Saving with no name is not an error and not a silent no-op: it asks.
func TestDevSaveWithoutANameOpensThePicker(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	next, _ := m.save("")
	if m := next.(devModel); m.picker == nil || !m.picker.saving() {
		t.Error("save with no path should open the picker in save mode")
	}
}

func TestDevOpenReplacesTheBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.domain")
	if err := os.WriteFile(path, []byte("Cursed Energy: in.txt\nReveal: stdout\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestDevModel("something else")
	m.dirty = true
	next, _ := m.open(path)
	m = next.(devModel)

	if got, want := m.buf.text(), "Cursed Energy: in.txt\nReveal: stdout"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if m.dirty {
		t.Error("a freshly opened file is not dirty")
	}
	if m.path != path {
		t.Errorf("path is %q, want %q", m.path, path)
	}
}

func TestDevOpenReportsAFailureWithoutLosingTheBuffer(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	next, _ := m.open(filepath.Join(t.TempDir(), "nope.domain"))
	m = next.(devModel)
	if m.buf.text() != "Reveal: stdout" {
		t.Error("a failed open discarded the buffer")
	}
	if !strings.Contains(m.status, "could not open") {
		t.Errorf("no explanation in the status line: %q", m.status)
	}
}

// ---------------------------------------------------------------------------
// the quit guard
// ---------------------------------------------------------------------------

func TestDevQuitGuardOnlyStopsUnsavedWork(t *testing.T) {
	clean := newTestDevModel("Reveal: stdout")
	if _, ok := guardUnsavedDevQuit(clean, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Error("a saved buffer should quit without argument")
	}

	dirty := clean
	dirty.dirty = true
	if _, ok := guardUnsavedDevQuit(dirty, tea.QuitMsg{}).(devConfirmQuitMsg); !ok {
		t.Error("an unsaved buffer should be asked about")
	}

	// Asked once, it lets go: the second quit is the answer.
	asked := dirty
	asked.confirmingQuit = true
	if _, ok := guardUnsavedDevQuit(asked, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Error("the second quit should be allowed through")
	}
}

// Carrying on editing answers the question, so the warning does not linger
// over a buffer that is being worked on.
func TestDevEditingWithdrawsTheQuitConfirmation(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	m.dirty = true
	m = devSend(m, devConfirmQuitMsg{})
	if !m.confirmingQuit || m.status == "" {
		t.Fatal("expected a confirmation and an explanation")
	}
	m = devKey(m, "x")
	if m.confirmingQuit {
		t.Error("typing should withdraw the confirmation")
	}
	if m.status != "" {
		t.Errorf("the warning outlived the question: %q", m.status)
	}
}

// Saving is the other answer, and it must clear the pending question too.
func TestDevSavingClearsTheQuitConfirmation(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	m.dirty, m.confirmingQuit = true, true
	next, _ := m.save(filepath.Join(t.TempDir(), "p.domain"))
	if next.(devModel).confirmingQuit {
		t.Error("a save should settle the unsaved-work question")
	}
}

// ---------------------------------------------------------------------------
// scrolling and the view
// ---------------------------------------------------------------------------

// Only what is on screen is painted — the property the whole render budget
// depends on.
func TestDevViewPaintsOnlyTheVisibleWindow(t *testing.T) {
	var lines []string
	for i := range 200 {
		lines = append(lines, "Cursed Technique: Map Each  # line "+strconv.Itoa(i))
	}
	m := newTestDevModel(strings.Join(lines, "\n"))
	m.scrollToCursor()

	painted := ansi.Strip(m.view())
	if strings.Contains(painted, "# line 100") {
		t.Error("a line far below the window was painted")
	}
	if !strings.Contains(painted, "# line 0") {
		t.Error("the first line was not painted")
	}
	// One line per row, plus the status line.
	if got, want := strings.Count(painted, "\n"), m.height-1; got != want {
		t.Errorf("painted %d newlines, want %d", got, want)
	}
}

func TestDevScrollingFollowsTheCursor(t *testing.T) {
	var lines []string
	for i := range 100 {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	m := newTestDevModel(strings.Join(lines, "\n"))

	for range 40 {
		m = devKey(m, "down")
	}
	if m.buf.row != 40 {
		t.Fatalf("cursor on row %d, want 40", m.buf.row)
	}
	if m.buf.row < m.top || m.buf.row >= m.top+m.textHeight() {
		t.Errorf("cursor at %d is outside the window [%d,%d)", m.buf.row, m.top, m.top+m.textHeight())
	}

	// Back to the top, and the window comes with it.
	for range 40 {
		m = devKey(m, "up")
	}
	if m.top != 0 {
		t.Errorf("window did not follow the cursor home: top is %d", m.top)
	}
}

// A program shorter than the window is never scrolled.
func TestDevShortProgramIsNotScrolled(t *testing.T) {
	m := newTestDevModel("Cursed Energy: in.txt\nReveal: stdout")
	m.top = 5 // as a deletion might have left it
	m.scrollToCursor()
	if m.top != 0 {
		t.Errorf("top is %d, want 0", m.top)
	}
}

// The gutter's width comes from the line count, so a program does not shift
// its own text sideways as it grows past ten lines.
func TestDevGutterWidthIsStable(t *testing.T) {
	var lines []string
	for range 120 {
		lines = append(lines, "Reveal: stdout")
	}
	m := newTestDevModel(strings.Join(lines, "\n"))
	painted := strings.Split(ansi.Strip(m.view()), "\n")

	width := -1
	for _, l := range painted[:m.textHeight()] {
		i := strings.Index(l, "│")
		if i < 0 {
			t.Fatalf("no gutter separator in %q", l)
		}
		if width == -1 {
			width = i
		} else if i != width {
			t.Errorf("gutter width moved from %d to %d on %q", width, i, l)
		}
	}
}

func TestDevStatusLineNamesTheFileAndMarksItDirty(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	m.path = "/tmp/day7.domain"

	if got := ansi.Strip(m.statusLine()); !strings.Contains(got, "day7.domain") || strings.Contains(got, "*") {
		t.Errorf("clean status line is wrong: %q", got)
	}
	m.dirty = true
	if got := ansi.Strip(m.statusLine()); !strings.Contains(got, "day7.domain*") {
		t.Errorf("dirty marker missing: %q", got)
	}
	// An unnamed buffer says so rather than showing an empty name.
	m.path = ""
	if got := ansi.Strip(m.statusLine()); !strings.Contains(got, "(unnamed)") {
		t.Errorf("unnamed buffer is not labelled: %q", got)
	}
}

// The status line reports the column in characters, not bytes: a byte offset
// is right for the lexer and wrong for a person counting across a line.
func TestDevStatusLineCountsColumnsInCharacters(t *testing.T) {
	m := newTestDevModel(`Cursed Technique: Split Text by "—"`)
	m = devKey(m, "end")
	got := ansi.Strip(m.statusLine())
	want := len([]rune(m.buf.line())) + 1
	if !strings.Contains(got, "1:"+strconv.Itoa(want)) {
		t.Errorf("status line %q does not report column %d", got, want)
	}
}

// The status line never wraps, whatever it has to say.
func TestDevStatusLineFitsANarrowTerminal(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	m.path = "/tmp/a-rather-long-program-name.domain"
	m.status = "could not save: permission denied on a very long path indeed"
	for _, w := range []int{20, 40, 80, 200} {
		m.width = w
		if got := ansi.StringWidth(m.statusLine()); got > w {
			t.Errorf("width %d: status line is %d columns", w, got)
		}
	}
}

// ---------------------------------------------------------------------------
// the key list
// ---------------------------------------------------------------------------

func TestDevHelpOpensScrollsAndCloses(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	m = devKey(m, "ctrl+g")
	if !m.showHelp {
		t.Fatal("ctrl+g did not open the key list")
	}
	if !strings.Contains(ansi.Strip(m.view()), "ctrl+s") {
		t.Error("the key list does not mention saving")
	}

	m.height = 6 // short enough that it has to scroll
	m = devKey(m, "down")
	if m.helpTop != 1 {
		t.Errorf("helpTop is %d, want 1", m.helpTop)
	}
	m = devKey(m, "x")
	if m.showHelp {
		t.Error("a key that is not a scroll should close the reference")
	}
}

// Typing while the key list is up must not reach the program.
func TestDevHelpSwallowsItsKeystrokes(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	m = devKey(m, "ctrl+g")
	m = devKey(m, "z")
	if m.buf.text() != "Reveal: stdout" {
		t.Errorf("a keystroke leaked into the buffer: %q", m.buf.text())
	}
}

// ---------------------------------------------------------------------------
// dirty tracking
// ---------------------------------------------------------------------------

// Moving through a file must never mark it unsaved — the reason the dirty flag
// is decided by whether the text changed rather than by which key was pressed.
func TestDevMotionDoesNotDirtyTheBuffer(t *testing.T) {
	m := newTestDevModel("Cursed Energy: in.txt\nCursed Technique: Sum\nReveal: stdout")
	for _, k := range []string{"down", "right", "end", "up", "home", "pgdown", "pgup", "left"} {
		m = devKey(m, k)
	}
	if m.dirty {
		t.Error("moving the cursor marked the buffer dirty")
	}
}

// ---------------------------------------------------------------------------
// horizontal scrolling
// ---------------------------------------------------------------------------

// The cursor is visible at every column of a line wider than the window.
//
// This is the test the original clipping lacked: rows never overflowed, which
// was checked, but a cursor past the right edge was clipped away with the rest
// of the line and simply could not be seen. A long `Using:` lambda reaches that
// in one statement.
func TestDevCursorStaysVisiblePastTheRightEdge(t *testing.T) {
	const long = `    Using: (r) -> if r.kind = "swap" then padd(point(r.a, r.b), origin) else r.pos`
	m := newTestDevModel(long)
	m.width = 40

	for col := 0; col <= len(long); col++ {
		m.buf.col = col
		m.scrollToCursor()
		painted := ansi.Strip(m.view())
		first := strings.SplitN(painted, "\n", 2)[0]

		// The cursor's column, in cells, must fall inside the window.
		want := ansi.StringWidth(long[:col]) - m.leftCol + m.gutterWidth()
		if want < m.gutterWidth() || want >= m.width {
			t.Fatalf("col %d: cursor at screen column %d, outside [%d,%d)",
				col, want, m.gutterWidth(), m.width)
		}
		if ansi.StringWidth(first) > m.width {
			t.Fatalf("col %d: row overflowed the window: %q", col, first)
		}
	}
}

// Scrolling right and back again returns to the start, rather than leaving the
// view stranded away from column zero.
func TestDevHorizontalScrollReturnsHome(t *testing.T) {
	m := newTestDevModel(strings.Repeat("x", 200))
	m.width = 40

	m = devKey(m, "end")
	if m.leftCol == 0 {
		t.Fatal("the view did not scroll right at all")
	}
	m = devKey(m, "home")
	if m.leftCol != 0 {
		t.Errorf("leftCol is %d after home, want 0", m.leftCol)
	}
}

// A line that fits is never scrolled, so short programs never shift sideways.
func TestDevShortLinesAreNotScrolledHorizontally(t *testing.T) {
	m := newTestDevModel("Cursed Energy: in.txt\nReveal: stdout")
	m.width = 80
	m = devKey(m, "end")
	if m.leftCol != 0 {
		t.Errorf("leftCol is %d on a line that fits", m.leftCol)
	}
}

// The gutter does not scroll with the text: line numbers stay where they are.
func TestDevGutterDoesNotScrollWithTheText(t *testing.T) {
	m := newTestDevModel(strings.Repeat("x", 200) + "\nshort")
	m.width = 40
	m = devKey(m, "end")

	for _, line := range strings.Split(ansi.Strip(m.view()), "\n")[:2] {
		if !strings.Contains(line, "│") {
			t.Errorf("the gutter scrolled away: %q", line)
		}
	}
}
