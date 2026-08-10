package main

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"

	"github.com/charmbracelet/x/ansi"
)

// devClock drives the undo coalescing without sleeping.
type devClock struct{ t time.Time }

func (c *devClock) now() time.Time       { return c.t }
func (c *devClock) tick(d time.Duration) { c.t = c.t.Add(d) }

// newClockedModel returns a model whose sense of time the test controls, which
// is what makes the pause rule testable at all.
func newClockedModel(text string) (devModel, *devClock) {
	c := &devClock{t: time.Unix(1_700_000_000, 0)}
	m := newTestDevModel(text)
	m.now = c.now
	return m, c
}

func devType(m devModel, s string) devModel {
	for _, r := range s {
		m = devKey(m, string(r))
	}
	return m
}

// ---------------------------------------------------------------------------
// undo
// ---------------------------------------------------------------------------

// The decision: a run of typing is one undo step, and the run ends when the
// typing does. Undoing a sentence should not be twenty-one keypresses.
func TestDevUndoCoalescesARunOfTyping(t *testing.T) {
	m, c := newClockedModel("")
	for _, r := range "Maximum Technique: Sum" {
		m = devKey(m, string(r))
		c.tick(30 * time.Millisecond) // a fast typist, well inside the pause
	}
	if m.buf.text() != "Maximum Technique: Sum" {
		t.Fatalf("typed %q", m.buf.text())
	}

	m = devKey(m, "ctrl+z")
	if m.buf.text() != "" {
		t.Errorf("one undo should withdraw the whole run, got %q", m.buf.text())
	}
}

// And the other half: a pause ends the run, so two thoughts are two steps.
func TestDevUndoBreaksAfterAPause(t *testing.T) {
	m, c := newClockedModel("")
	m = devType(m, "Sum")
	c.tick(2 * devUndoPause)
	m = devType(m, " Each")

	if m.buf.text() != "Sum Each" {
		t.Fatalf("typed %q", m.buf.text())
	}
	m = devKey(m, "ctrl+z")
	if got := m.buf.text(); got != "Sum" {
		t.Errorf("first undo should stop at the pause, got %q", got)
	}
	m = devKey(m, "ctrl+z")
	if got := m.buf.text(); got != "" {
		t.Errorf("second undo should reach the start, got %q", got)
	}
}

// A discrete action is its own step whatever the timing: it was one decision
// when it was made and should be one when it is withdrawn.
func TestDevUndoTreatsEnterAsItsOwnStep(t *testing.T) {
	m, _ := newClockedModel("")
	m = devType(m, "Sum")
	m = devKey(m, "enter")
	m = devType(m, "Reveal: stdout")

	m = devKey(m, "ctrl+z")
	if got := m.buf.text(); got != "Sum\n" {
		t.Errorf("undo should withdraw the second line's text, got %q", got)
	}
	m = devKey(m, "ctrl+z")
	if got := m.buf.text(); got != "Sum" {
		t.Errorf("undo should withdraw the line break, got %q", got)
	}
}

func TestDevRedoReturnsWhatUndoTook(t *testing.T) {
	m, _ := newClockedModel("")
	m = devType(m, "Sum")
	m = devKey(m, "ctrl+z")
	if m.buf.text() != "" {
		t.Fatalf("undo left %q", m.buf.text())
	}
	m = devKey(m, "ctrl+y")
	if got := m.buf.text(); got != "Sum" {
		t.Errorf("redo gave %q, want %q", got, "Sum")
	}
}

// Typing after an undo must not merge into the step that was just withdrawn,
// or continuing to type would silently undo the undo.
func TestDevTypingAfterUndoStartsANewStep(t *testing.T) {
	m, c := newClockedModel("")
	m = devType(m, "Sum")
	m = devKey(m, "ctrl+z")
	c.tick(10 * time.Millisecond) // well inside the pause window
	m = devType(m, "Count")

	if got := m.buf.text(); got != "Count" {
		t.Fatalf("got %q", got)
	}
	m = devKey(m, "ctrl+z")
	if got := m.buf.text(); got != "" {
		t.Errorf("the new run should undo on its own, got %q", got)
	}
}

// A new edit abandons the redo branch: there is no longer one future.
func TestDevEditingAfterUndoDropsRedo(t *testing.T) {
	m, c := newClockedModel("")
	m = devType(m, "Sum")
	m = devKey(m, "ctrl+z")
	c.tick(2 * devUndoPause)
	m = devType(m, "Count")
	m = devKey(m, "ctrl+y")
	if got := m.buf.text(); got != "Count" {
		t.Errorf("redo should have been abandoned, got %q", got)
	}
}

func TestDevUndoRestoresTheCursorToWhereTheEditWas(t *testing.T) {
	m, c := newClockedModel("Cursed Energy: in.txt\nReveal: stdout")
	m = devKey(m, "down")
	m = devKey(m, "end")
	c.tick(2 * devUndoPause)
	m = devType(m, "!")

	m = devKey(m, "up") // wander away
	m = devKey(m, "home")
	m = devKey(m, "ctrl+z")

	if m.buf.row != 1 || m.buf.col != len("Reveal: stdout") {
		t.Errorf("undo left the cursor at %d:%d, want 1:%d", m.buf.row, m.buf.col, len("Reveal: stdout"))
	}
}

func TestDevUndoSaysWhenThereIsNothingToUndo(t *testing.T) {
	m, _ := newClockedModel("Reveal: stdout")
	m = devKey(m, "ctrl+z")
	if !strings.Contains(m.status, "nothing to undo") {
		t.Errorf("status is %q", m.status)
	}
}

// ---------------------------------------------------------------------------
// selection
// ---------------------------------------------------------------------------

func TestDevShiftedMotionSelectsAndPlainMotionDrops(t *testing.T) {
	m, _ := newClockedModel("Cursed Technique: Sum")
	for range 6 {
		m = devKey(m, "shift+right")
	}
	if got := m.buf.selectedText(); got != "Cursed" {
		t.Errorf("selected %q, want %q", got, "Cursed")
	}
	m = devKey(m, "right")
	if _, _, ok := m.buf.selection(); ok {
		t.Error("an unshifted motion should drop the selection")
	}
}

func TestDevSelectionSpansLines(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: in.txt\nCursed Technique: Sum\nReveal: stdout")
	m = devKey(m, "end")
	m = devKey(m, "shift+down")
	m = devKey(m, "shift+end")

	want := "\nCursed Technique: Sum"
	if got := m.buf.selectedText(); got != want {
		t.Errorf("selected %q, want %q", got, want)
	}
}

func TestDevSelectAllTakesTheProgram(t *testing.T) {
	const src = "Cursed Energy: in.txt\nReveal: stdout"
	m, _ := newClockedModel(src)
	m = devKey(m, "ctrl+a")
	if got := m.buf.selectedText(); got != src {
		t.Errorf("selected %q", got)
	}
}

// Typing over a selection replaces it — the thing that makes select-then-type
// work at all.
func TestDevTypingReplacesTheSelection(t *testing.T) {
	m, _ := newClockedModel("Cursed Technique: Sum")
	m = devKey(m, "end")
	for range 3 {
		m = devKey(m, "shift+left")
	}
	m = devType(m, "Count")
	if got, want := m.buf.text(), "Cursed Technique: Count"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestDevBackspaceDeletesTheSelectionRatherThanOneCharacter(t *testing.T) {
	m, _ := newClockedModel("Cursed Technique: Sum")
	m = devKey(m, "end")
	for range 3 {
		m = devKey(m, "shift+left")
	}
	m = devKey(m, "backspace")
	if got, want := m.buf.text(), "Cursed Technique: "; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// The selection is painted, and painting it does not change the text or its
// width — the same property the cursor rides on.
func TestDevSelectionIsPaintedWithoutDisturbingTheLine(t *testing.T) {
	m, _ := newClockedModel("Cursed Technique: Sum")
	for range 6 {
		m = devKey(m, "shift+right")
	}
	const line = "Cursed Technique: Sum"
	painted := paintLine(line, m.decorFor(0))
	if plain := ansi.Strip(painted); plain != line {
		t.Errorf("selection changed the text: %q", plain)
	}
	if !strings.Contains(painted, faceStyle(faceSelect).Render("Cursed")) {
		t.Errorf("the selected run is not painted as selected: %q", painted)
	}
}

// ---------------------------------------------------------------------------
// word motion and indentation
// ---------------------------------------------------------------------------

func TestDevWordMotion(t *testing.T) {
	m, _ := newClockedModel("Cursed Technique: Split Text")
	m = devKey(m, "ctrl+right")
	if m.buf.col != len("Cursed ") {
		t.Errorf("after one word: col %d, want %d", m.buf.col, len("Cursed "))
	}
	m = devKey(m, "ctrl+right")
	if m.buf.col != len("Cursed Technique: ") {
		t.Errorf("after two words: col %d, want %d", m.buf.col, len("Cursed Technique: "))
	}
	m = devKey(m, "ctrl+left")
	if m.buf.col != len("Cursed ") {
		t.Errorf("back one word: col %d, want %d", m.buf.col, len("Cursed "))
	}
}

func TestDevIndentAndDedentALine(t *testing.T) {
	m, _ := newClockedModel("Using: (x) -> x")
	m = devKey(m, "tab")
	if got, want := m.buf.text(), "    Using: (x) -> x"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	m = devKey(m, "shift+tab")
	if got, want := m.buf.text(), "Using: (x) -> x"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// Dedenting a line with no indentation is a no-op, not a bite out of it.
	m = devKey(m, "shift+tab")
	if got, want := m.buf.text(), "Using: (x) -> x"; got != want {
		t.Errorf("dedent ate text: %q", got)
	}
}

func TestDevIndentASelection(t *testing.T) {
	m, _ := newClockedModel("Using: a\nUsing: b\nUsing: c")
	m = devKey(m, "shift+down")
	m = devKey(m, "shift+down")
	m = devKey(m, "tab")

	want := "    Using: a\n    Using: b\nUsing: c"
	if got := m.buf.text(); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// Tab indents where there is nothing to complete, so one key does both jobs.
// Phase 3 gave Tab a second meaning — it completes after a word — and the rule
// that separates them is whether anything but whitespace precedes the cursor.
func TestDevTabIndentsInsideLeadingWhitespace(t *testing.T) {
	m, _ := newClockedModel("    Sum")
	m = devKey(m, "home")
	m = devKey(m, "tab")
	if got, want := m.buf.text(), "        Sum"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if m.complete != nil {
		t.Error("tab in the indentation offered completions")
	}
}

// ---------------------------------------------------------------------------
// find
// ---------------------------------------------------------------------------

func TestDevFindIsIncrementalAndWrapping(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: in.txt\nMaximum Technique: Sum\nCursed Technique: Sum Each")
	m = devKey(m, "ctrl+f")
	if m.search == nil {
		t.Fatal("ctrl+f did not open the search")
	}
	m = devType(m, "Sum")

	if n := len(m.search.matches); n != 2 {
		t.Fatalf("found %d matches, want 2", n)
	}
	// The first match at or after the cursor, not the top of the file.
	if m.buf.row != 1 {
		t.Errorf("cursor on row %d, want 1", m.buf.row)
	}
	m = devKey(m, "down")
	if m.buf.row != 2 {
		t.Errorf("next match is on row %d, want 2", m.buf.row)
	}
	m = devKey(m, "down")
	if m.buf.row != 1 {
		t.Errorf("search did not wrap: row %d, want 1", m.buf.row)
	}
}

func TestDevFindIsCaseInsensitive(t *testing.T) {
	m, _ := newClockedModel("Maximum Technique: Sum")
	m = devKey(m, "ctrl+f")
	m = devType(m, "sum")
	if len(m.search.matches) != 1 {
		t.Errorf("found %d matches, want 1", len(m.search.matches))
	}
}

// Cancelling puts the cursor back; a search that moved you and then gave up
// would be worse than no search.
func TestDevFindEscapeRestoresTheCursor(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: in.txt\nMaximum Technique: Sum")
	m = devKey(m, "ctrl+f")
	m = devType(m, "Sum")
	if m.buf.row != 1 {
		t.Fatalf("search did not move the cursor")
	}
	m = devKey(m, "esc")
	if m.search != nil {
		t.Error("esc should close the search")
	}
	if m.buf.row != 0 || m.buf.col != 0 {
		t.Errorf("cursor left at %d:%d, want 0:0", m.buf.row, m.buf.col)
	}
}

// Enter keeps you where the search got to.
func TestDevFindEnterKeepsTheMatch(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: in.txt\nMaximum Technique: Sum")
	m = devKey(m, "ctrl+f")
	m = devType(m, "Sum")
	m = devKey(m, "enter")
	if m.search != nil {
		t.Error("enter should close the search")
	}
	if m.buf.row != 1 {
		t.Errorf("cursor left row %d, want 1", m.buf.row)
	}
}

// Typing into the search must not reach the program.
func TestDevFindDoesNotEditTheBuffer(t *testing.T) {
	const src = "Maximum Technique: Sum"
	m, _ := newClockedModel(src)
	m = devKey(m, "ctrl+f")
	m = devType(m, "Sum")
	if m.buf.text() != src {
		t.Errorf("the search edited the program: %q", m.buf.text())
	}
	if m.dirty {
		t.Error("searching marked the buffer dirty")
	}
}

func TestDevFindPromptReportsTheCount(t *testing.T) {
	m, _ := newClockedModel("Sum\nSum\nSum")
	m = devKey(m, "ctrl+f")
	m = devType(m, "Sum")
	if got := ansi.Strip(m.search.prompt()); !strings.Contains(got, "1 of 3") {
		t.Errorf("prompt is %q", got)
	}
	m = devType(m, "X")
	if got := ansi.Strip(m.search.prompt()); !strings.Contains(got, "no matches") {
		t.Errorf("prompt is %q", got)
	}
}

// ---------------------------------------------------------------------------
// go to line
// ---------------------------------------------------------------------------

func TestDevGotoLine(t *testing.T) {
	var lines []string
	for i := range 50 {
		lines = append(lines, "line "+string(rune('a'+i%26)))
	}
	m, _ := newClockedModel(strings.Join(lines, "\n"))

	m = devKey(m, "ctrl+l")
	if m.gotoLine == nil {
		t.Fatal("ctrl+l did not open the prompt")
	}
	m = devType(m, "42")
	m = devKey(m, "enter")

	if m.gotoLine != nil {
		t.Error("enter should close the prompt")
	}
	if m.buf.row != 41 {
		t.Errorf("cursor on row %d, want 41", m.buf.row)
	}
	if m.buf.row < m.top || m.buf.row >= m.top+m.textHeight() {
		t.Errorf("row %d is outside the window [%d,%d)", m.buf.row, m.top, m.top+m.textHeight())
	}
}

// A line number beyond the program clamps rather than failing.
func TestDevGotoLineClamps(t *testing.T) {
	m, _ := newClockedModel("a\nb\nc")
	m = devKey(m, "ctrl+l")
	m = devType(m, "999")
	m = devKey(m, "enter")
	if m.buf.row != 2 {
		t.Errorf("row %d, want 2", m.buf.row)
	}
}

// The prompt takes digits only: accepting letters and refusing them at Enter
// would waste the typing.
func TestDevGotoLineIgnoresNonDigits(t *testing.T) {
	m, _ := newClockedModel("a\nb\nc")
	m = devKey(m, "ctrl+l")
	m = devType(m, "x2y")
	if *m.gotoLine != "2" {
		t.Errorf("prompt holds %q, want %q", *m.gotoLine, "2")
	}
}

// ---------------------------------------------------------------------------
// the bottom line
// ---------------------------------------------------------------------------

// A prompt owns the bottom row while it is open, so two rows never compete for
// it.
func TestDevPromptTakesTheBottomLine(t *testing.T) {
	m, _ := newClockedModel("Maximum Technique: Sum")
	m.path = "/tmp/p.domain"

	if got := ansi.Strip(m.bottomLine()); !strings.Contains(got, "p.domain") {
		t.Errorf("expected the status line, got %q", got)
	}
	m = devKey(m, "ctrl+f")
	if got := ansi.Strip(m.bottomLine()); !strings.Contains(got, "find") {
		t.Errorf("expected the find prompt, got %q", got)
	}
}

// Whatever is on it, the bottom line fits.
func TestDevBottomLineFitsANarrowTerminal(t *testing.T) {
	m, _ := newClockedModel("Maximum Technique: Sum")
	m = devKey(m, "ctrl+f")
	m = devType(m, "a very long search query indeed that will not fit")
	for _, w := range []int{20, 40, 80} {
		m.width = w
		if got := ansi.StringWidth(m.bottomLine()); got > w {
			t.Errorf("width %d: bottom line is %d columns", w, got)
		}
	}
}

// ctrl+c copies and does not leave. Binding it to quit as well would mean a
// buffer could be abandoned by the reflex that copies, which is the one
// keystroke mistake an editor must not make cheap.
func TestDevCtrlCCopiesRatherThanLeaving(t *testing.T) {
	m, _ := newClockedModel("Cursed Technique: Sum")
	for range 6 {
		m = devKey(m, "shift+right")
	}
	next, cmd := m.key(devKeyMsg("ctrl+c"))
	m = next.(devModel)
	if cmd != nil {
		t.Error("ctrl+c returned a command — it must not quit")
	}
	// Either it copied or there is no clipboard here; both are fine, and
	// neither is leaving.
	if m.status == "" {
		t.Error("ctrl+c did nothing at all")
	}
}

// Leaving is ctrl+q alone.
func TestDevOnlyCtrlQLeaves(t *testing.T) {
	m, _ := newClockedModel("Reveal: stdout")
	_, cmd := m.key(devKeyMsg("ctrl+q"))
	if cmd == nil {
		t.Error("ctrl+q did not quit")
	}
}

// Every binding the key list describes is one the editor actually has, and
// every binding it has is described. A key that works and is undocumented is
// as bad as one documented and missing.
func TestDevKeyListMatchesTheBindings(t *testing.T) {
	body := strings.Join(devHelpBody(), "\n")
	keys := defaultDevKeys()
	for name, binding := range map[string]key.Binding{
		"Quit": keys.Quit, "Save": keys.Save, "Open": keys.Open, "Help": keys.Help,
		"Undo": keys.Undo, "Redo": keys.Redo, "Find": keys.Find, "Goto": keys.Goto,
		"SelectAll": keys.SelectAll, "Copy": keys.Copy, "Cut": keys.Cut, "Paste": keys.Paste,
		"Inspect": keys.Inspect, "Definition": keys.Definition, "Format": keys.Format,
		"Docs": keys.Docs, "Run": keys.Run, "Visualize": keys.Visualize,
		"Input": keys.Input, "Suggest": keys.Suggest,
	} {
		first := binding.Keys()[0]
		if !strings.Contains(body, first) {
			t.Errorf("%s is bound to %q but the key list does not mention it", name, first)
		}
	}
}

// No two bindings may share a key. A shared key means whichever case the
// dispatcher checks first silently wins, and the other binding is dead in a way
// nothing else would report.
func TestDevNoTwoBindingsShareAKey(t *testing.T) {
	k := defaultDevKeys()
	owner := map[string]string{}
	for name, b := range map[string]key.Binding{
		"Quit": k.Quit, "Save": k.Save, "SaveAs": k.SaveAs, "Open": k.Open, "Help": k.Help,
		"Undo": k.Undo, "Redo": k.Redo, "Find": k.Find, "Goto": k.Goto,
		"SelectAll": k.SelectAll, "Copy": k.Copy, "Cut": k.Cut, "Paste": k.Paste,
		"Complete": k.Complete, "Inspect": k.Inspect, "Definition": k.Definition,
		"Format": k.Format, "Docs": k.Docs, "Run": k.Run, "Visualize": k.Visualize,
		"Input": k.Input, "Suggest": k.Suggest,
		"StageNext": k.StageNext, "StagePrev": k.StagePrev,
	} {
		for _, spelling := range b.Keys() {
			if prev, taken := owner[spelling]; taken {
				t.Errorf("%q is bound to both %s and %s", spelling, prev, name)
			}
			owner[spelling] = name
		}
	}
}
