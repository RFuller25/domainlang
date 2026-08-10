package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// settle runs the analysis the model would have run on idle and feeds the
// result back, standing in for the runtime's timer. Everything the analysis
// does is a pure function of the text, so this is the whole of what the event
// loop would have done.
func settle(t *testing.T, m devModel) devModel {
	t.Helper()
	msg := analyzeCmd(m.gen, m.path, m.buf.text())()
	next, _ := m.Update(msg)
	return next.(devModel)
}

const intelProgram = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Reveal: stdout`

// ---------------------------------------------------------------------------
// type hints
// ---------------------------------------------------------------------------

func TestDevTypeHintsAppearAtTheEndOfEachLine(t *testing.T) {
	m := settle(t, newTestDevModel(intelProgram))

	want := map[int]string{0: ": Text", 1: ": List<Text>", 4: ": List<Int>"}
	for row, label := range want {
		if got := m.hintFor(row); got != label {
			t.Errorf("row %d: hint %q, want %q", row, got, label)
		}
	}

	painted := ansi.Strip(m.view())
	if !strings.Contains(painted, `Cursed Energy: input.txt : Text`) {
		t.Errorf("the hint is not painted after its line:\n%s", painted)
	}
}

// A hint that wrapped would push the program's own text onto a second row,
// which is worse than no hint.
func TestDevTypeHintIsDroppedWhenItWouldNotFit(t *testing.T) {
	m := settle(t, newTestDevModel(intelProgram))
	if m.hintFor(0) == "" {
		t.Fatal("no hint to test with")
	}
	m.width = 30
	painted := ansi.Strip(m.view())
	// No row overflows: with no soft wrap, a long line is clipped rather than
	// left to wrap and push the status line off the bottom.
	for _, line := range strings.Split(painted, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Errorf("a row overflowed a narrow terminal: %q", line)
		}
	}
	if strings.Contains(painted, ": Text") {
		t.Error("a hint was drawn where it does not fit")
	}
}

// The types are of the text in the buffer, not of anything on disk — which is
// the whole point of having them here rather than over a language server.
func TestDevTypeHintsFollowUnsavedEdits(t *testing.T) {
	m := settle(t, newTestDevModel("Cursed Energy: input.txt"))
	if got := m.hintFor(0); got != ": Text" {
		t.Fatalf("hint is %q", got)
	}

	m = devKey(m, "end")
	m = devKey(m, "enter")
	m = devType(m, `Cursed Technique: Split Text by "\n"`)
	m = settle(t, m)

	if got := m.hintFor(1); got != ": List<Text>" {
		t.Errorf("the new line's hint is %q, want %q", got, ": List<Text>")
	}
}

// An analysis that arrives after another edit is dropped rather than shown
// against text it was not computed from.
func TestDevStaleAnalysisIsDiscarded(t *testing.T) {
	m := settle(t, newTestDevModel(intelProgram))
	stale := analyzeCmd(m.gen, m.path, m.buf.text())()

	m = devType(m, "x") // the buffer moves on
	before := m.intel.gen
	next, _ := m.Update(stale)
	if got := next.(devModel).intel.gen; got != before {
		t.Errorf("a stale analysis was accepted: gen %d became %d", before, got)
	}
}

// ---------------------------------------------------------------------------
// diagnostics
// ---------------------------------------------------------------------------

func TestDevDiagnosticsMarkTheGutterAndTheStatusLine(t *testing.T) {
	m := settle(t, newTestDevModel("Cursed Energy: input.txt\nCursed Tecnique: Sum\nReveal: stdout"))

	if m.intel.errs == 0 {
		t.Fatal("the misspelling was not reported")
	}
	if _, ok := m.gutterMark(1); !ok {
		t.Error("the offending line has no gutter mark")
	}
	if _, ok := m.gutterMark(0); ok {
		t.Error("a clean line was marked")
	}

	m.buf.row = 1
	if got := ansi.Strip(m.diagnosticLine()); got == "" {
		t.Error("the status line says nothing about the error under the cursor")
	}
}

// The gutter is the same width with a mark as without, so a program's text
// never moves sideways because a line acquired an error.
func TestDevGutterWidthIsUnchangedByAMark(t *testing.T) {
	m := settle(t, newTestDevModel("Cursed Energy: input.txt\nCursed Tecnique: Sum\nReveal: stdout"))
	lines := strings.Split(ansi.Strip(m.view()), "\n")

	sep := -1
	for i := range 3 {
		l := lines[i]
		// The program text starts at the same column on every row.
		j := strings.Index(l, "Cursed")
		if j < 0 {
			j = strings.Index(l, "Reveal")
		}
		if j < 0 {
			t.Fatalf("no program text on row %d: %q", i, l)
		}
		if sep == -1 {
			sep = j
		} else if j != sep {
			t.Errorf("row %d starts at column %d, want %d — the gutter moved", i, j, sep)
		}
	}
}

func TestDevErrorCountsShowInTheStatusLine(t *testing.T) {
	m := settle(t, newTestDevModel("Cursed Energy: input.txt\nCursed Tecnique: Sum"))
	if got := ansi.Strip(m.statusLine()); !strings.Contains(got, "✗") {
		t.Errorf("no error count in %q", got)
	}
}

// The editor must never rewrite what someone is typing. The diagnostics engine
// repairs as it goes so it can see past the first error; only its findings are
// wanted.
func TestDevAnalysisNeverRewritesTheBuffer(t *testing.T) {
	const broken = "Cursed Energy: input.txt\nCursed Tecnique: Sum"
	m := settle(t, newTestDevModel(broken))
	if m.buf.text() != broken {
		t.Errorf("the analysis edited the program: %q", m.buf.text())
	}
	if m.dirty {
		t.Error("the analysis marked the buffer dirty")
	}
}

// ---------------------------------------------------------------------------
// completion
// ---------------------------------------------------------------------------

func TestDevCompletionOffersKeywords(t *testing.T) {
	m := newTestDevModel("")
	m = devType(m, "Cursed T")
	m = devKey(m, "tab")

	if m.complete == nil {
		t.Fatal("tab did not open the completion popup")
	}
	found := false
	for _, c := range m.complete.candidates {
		if strings.Contains(c, "Technique") {
			found = true
		}
	}
	if !found {
		t.Errorf("no Cursed Technique among %v", m.complete.candidates)
	}
}

func TestDevCompletionInsertsTheChoice(t *testing.T) {
	m := newTestDevModel("")
	m = devType(m, "Cursed T")
	m = devKey(m, "tab")
	if m.complete == nil {
		t.Fatal("no popup")
	}
	pick := m.complete.candidates[0]
	m = devKey(m, "enter")

	if m.complete != nil {
		t.Error("enter should close the popup")
	}
	if got := m.buf.line(); !strings.Contains(got, pick) {
		t.Errorf("line is %q, want it to contain %q", got, pick)
	}
	if !m.dirty {
		t.Error("accepting a completion did not mark the buffer dirty")
	}
}

func TestDevCompletionEscapeLeavesTheLineAlone(t *testing.T) {
	m := newTestDevModel("")
	m = devType(m, "Cursed T")
	before := m.buf.line()
	m = devKey(m, "tab")
	m = devKey(m, "esc")

	if m.complete != nil {
		t.Error("esc should close the popup")
	}
	if m.buf.line() != before {
		t.Errorf("line changed to %q", m.buf.line())
	}
}

// A completion that silently does nothing is worse than none, so where there
// is nothing to offer the key falls through to indenting.
func TestDevTabIndentsWhereThereIsNothingToComplete(t *testing.T) {
	m := newTestDevModel("")
	m = devKey(m, "tab")
	if m.complete != nil {
		t.Fatal("an empty line offered completions")
	}
	if got := m.buf.line(); got != devIndent {
		t.Errorf("tab inserted %q, want an indent", got)
	}
}

// One undo withdraws a whole accepted completion, not a character of it.
func TestDevCompletionIsOneUndoStep(t *testing.T) {
	m, _ := newClockedModel("")
	m = devType(m, "Cursed T")
	m = devKey(m, "tab")
	m = devKey(m, "enter")
	m = devKey(m, "ctrl+z")
	if got := m.buf.line(); got != "Cursed T" {
		t.Errorf("undo gave %q, want %q", got, "Cursed T")
	}
}

// ---------------------------------------------------------------------------
// inspect
// ---------------------------------------------------------------------------

func TestDevInspectDescribesThePrimitiveUnderTheCursor(t *testing.T) {
	m := settle(t, newTestDevModel(intelProgram))
	m.buf.row = 1
	m = devKey(m, "alt+k")

	if m.inspect == nil {
		t.Fatal("ctrl+k showed nothing")
	}
	body := ansi.Strip(strings.Join(m.inspect.lines, "\n"))
	if !strings.Contains(body, "Split") {
		t.Errorf("the panel does not name the primitive:\n%s", body)
	}
	if !strings.Contains(body, "→") {
		t.Errorf("the panel does not show a type step:\n%s", body)
	}

	// The panel is a reference, not a mode: any key closes it.
	m = devKey(m, "x")
	if m.inspect != nil {
		t.Error("a keystroke did not close the panel")
	}
}

func TestDevInspectSaysSoWhenThereIsNothingToSay(t *testing.T) {
	m := settle(t, newTestDevModel("# just a comment"))
	m = devKey(m, "alt+k")
	if m.inspect != nil {
		t.Error("a comment described something")
	}
	if !strings.Contains(m.status, "nothing to inspect") {
		t.Errorf("status is %q", m.status)
	}
}

// ---------------------------------------------------------------------------
// go to definition
// ---------------------------------------------------------------------------

func TestDevGoToDefinitionJumps(t *testing.T) {
	const src = `Shikigami "Double"
    Cursed Technique: Map Each
        Using: (x) -> x * 2
Cursed Energy: input.txt
Channeled Energy: Convert To Integers
Shikigami: Double
Reveal: stdout`
	m := settle(t, newTestDevModel(src))
	m.buf.row = 5 // the call

	m = devKey(m, "ctrl+]")
	if m.buf.row != 0 {
		t.Errorf("cursor on row %d, want 0", m.buf.row)
	}
}

// A prelude name is real and has nowhere to jump to. Saying which it is beats
// saying nothing, which reads as "no such definition".
func TestDevGoToDefinitionExplainsPreludeNames(t *testing.T) {
	m := settle(t, newTestDevModel("Cursed Energy: input.txt\nShikigami: Ints\nReveal: stdout"))
	m.buf.row = 1
	m = devKey(m, "ctrl+]")
	if !strings.Contains(m.status, "prelude") {
		t.Errorf("status is %q", m.status)
	}
	if m.buf.row != 1 {
		t.Error("the cursor moved on a definition it could not reach")
	}
}

// ---------------------------------------------------------------------------
// format
// ---------------------------------------------------------------------------

func TestDevFormatNormalisesTheProgram(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: input.txt\nCursed Technique: Map Each\n  Using: (x) -> x * 2\nReveal: stdout")
	m = devKey(m, "alt+f")

	if !strings.Contains(m.buf.text(), "    Using:") {
		t.Errorf("the argument line was not re-indented:\n%s", m.buf.text())
	}
	if !m.dirty {
		t.Error("formatting did not mark the buffer dirty")
	}
	// And it is one undo step.
	m = devKey(m, "ctrl+z")
	if !strings.Contains(m.buf.text(), "  Using:") {
		t.Errorf("undo did not restore the original layout:\n%s", m.buf.text())
	}
}

func TestDevFormatReportsAProgramItCannotParse(t *testing.T) {
	m, _ := newClockedModel(`Cursed Technique: Split Text by "`)
	before := m.buf.text()
	m = devKey(m, "alt+f")
	if m.buf.text() != before {
		t.Error("an unformattable program was changed anyway")
	}
	if !strings.Contains(m.status, "cannot format") {
		t.Errorf("status is %q", m.status)
	}
}

func TestDevFormatSaysWhenThereIsNothingToDo(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: input.txt\nReveal: stdout")
	m = devKey(m, "alt+f")
	if !strings.Contains(m.status, "already formatted") {
		t.Errorf("status is %q", m.status)
	}
	if m.dirty {
		t.Error("a no-op format marked the buffer dirty")
	}
}

// ---------------------------------------------------------------------------
// the documentation catalog
// ---------------------------------------------------------------------------

func TestDevDocBrowserInsertsAStatement(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: input.txt")
	m = devKey(m, "alt+d")
	if m.browser == nil {
		t.Fatal("ctrl+d did not open the catalog")
	}
	m = devKey(m, "enter")
	if m.browser != nil {
		t.Error("enter should close the catalog")
	}
	if len(m.buf.lines) != 2 {
		t.Fatalf("expected the statement on a new line, got %q", m.buf.text())
	}
	if m.buf.lines[1] == "" {
		t.Error("nothing was inserted")
	}
	if !m.dirty {
		t.Error("inserting did not mark the buffer dirty")
	}
}

// ---------------------------------------------------------------------------
// idle scheduling
// ---------------------------------------------------------------------------

// Editing restarts the idle timer and bumps the generation, so a burst of
// typing analyses once rather than once per keystroke.
func TestDevEditingSchedulesOneAnalysis(t *testing.T) {
	m := newTestDevModel("")
	before := m.gen
	m = devType(m, "Reveal: stdout")
	if m.gen == before {
		t.Error("typing did not bump the generation")
	}

	// An idle message from an earlier generation is ignored.
	next, cmd := m.Update(devIdleMsg{gen: before})
	if cmd != nil {
		t.Error("a superseded idle tick started an analysis")
	}
	// The current one does start work.
	_, cmd = next.(devModel).Update(devIdleMsg{gen: next.(devModel).gen})
	if cmd == nil {
		t.Error("the current idle tick did not start an analysis")
	}
}

// Opening a file analyses it, so its types are there before it is touched.
func TestDevInitAnalysesTheOpenedProgram(t *testing.T) {
	m := newTestDevModel(intelProgram)
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init started no work")
	}
	// The analysis is what Init would have run.
	m = settle(t, m)
	if m.hintFor(0) == "" {
		t.Error("an opened program has no types before it is edited")
	}
}

var _ = tea.Quit

// ---------------------------------------------------------------------------
// following a definition across files
// ---------------------------------------------------------------------------

// withLibrary writes a program and the library it imports, and returns the
// model editing the program.
func withLibrary(t *testing.T) (devModel, string, string) {
	t.Helper()
	dir := t.TempDir()
	lib := filepath.Join(dir, "helpers.domain")
	if err := os.WriteFile(lib, []byte("Shikigami \"Triple\"\n    Cursed Technique: Map Each\n        Using: (x) -> x * 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "main.domain")
	src := "Innate Domain: helpers\nCursed Energy: in.txt\nChanneled Energy: Convert To Integers\nShikigami: Triple\nReveal: stdout"
	if err := os.WriteFile(prog, []byte(src+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestDevModel(src)
	m.path = prog
	return settle(t, m), prog, lib
}

// ctrl+] used to report where an imported definition lived and refuse to go.
// Now it goes.
func TestDevGoToDefinitionFollowsAnImport(t *testing.T) {
	m, _, lib := withLibrary(t)
	m.buf.gotoLine(4) // the call

	m = devKey(m, "ctrl+]")
	if m.path != lib {
		t.Fatalf("still editing %s, want %s (status: %q)", m.path, lib, m.status)
	}
	if m.buf.row != 0 {
		t.Errorf("landed on line %d, want the definition at 1", m.buf.row+1)
	}
	if !strings.Contains(m.buf.text(), "Triple") {
		t.Error("the library's text was not loaded")
	}
}

// And comes back.
func TestDevJumpBackReturnsToTheCaller(t *testing.T) {
	m, prog, _ := withLibrary(t)
	m.buf.gotoLine(4)
	m = devKey(m, "ctrl+]")
	m = devKey(m, "ctrl+[")

	if m.path != prog {
		t.Fatalf("did not return: editing %s (status %q)", m.path, m.status)
	}
	if m.buf.row+1 != 4 {
		t.Errorf("came back to line %d, want 4", m.buf.row+1)
	}
}

// An editor may not lose your program to a navigation key.
func TestDevGoToDefinitionRefusesToAbandonUnsavedWork(t *testing.T) {
	m, prog, _ := withLibrary(t)
	m.buf.gotoLine(4)
	m = devType(m, " ")
	if !m.dirty {
		t.Fatal("the buffer is not dirty")
	}

	m = devKey(m, "ctrl+]")
	if m.path != prog {
		t.Error("an unsaved buffer was abandoned to follow a definition")
	}
	if !strings.Contains(m.status, "save first") {
		t.Errorf("status is %q", m.status)
	}
}

// Opening a different file leaves the previous program's answers behind: folds,
// analysis and recording all describe text that is no longer here.
func TestDevOpeningAnotherFileDropsTheOldProgramsState(t *testing.T) {
	m, _, lib := withLibrary(t)
	m.folded = map[int]bool{2: true}
	m.stages = map[int]devStage{1: {Line: 1, Short: "x"}}

	next, _ := m.open(lib)
	m = next.(devModel)
	if len(m.folded) != 0 {
		t.Error("folds carried into another file")
	}
	if m.stages != nil {
		t.Error("a recording carried into another file")
	}
	if m.intel.analysis != nil {
		t.Error("an analysis carried into another file")
	}
}

func TestDevJumpBackSaysWhenThereIsNowhereToGo(t *testing.T) {
	m := settle(t, newTestDevModel("Reveal: stdout"))
	m = devKey(m, "ctrl+[")
	if !strings.Contains(m.status, "nowhere to go back") {
		t.Errorf("status is %q", m.status)
	}
}

// ---------------------------------------------------------------------------
// the front-end lock
// ---------------------------------------------------------------------------

// Resolution and evaluation write package-level state — the ambient-binding
// stacks in prims, the binding table typecheck.ResetBindings clears — so two of
// them at once corrupt each other. The editor analyses on a command after every
// pause and runs on another, so this is not theoretical: two keystrokes close
// together are two overlapping resolves.
//
// The damage does not look like a crash. It looks like nonsense from the
// linter: `Combine` reported as ignoring its `From:` argument, `Apply` as
// ignoring its `Using:` — statements that plainly read them. Run this with
// -race and it fails without the lock.
func TestDevAnalysisAndRunDoNotRaceEachOther(t *testing.T) {
	prog := "Cursed Energy: in.txt\n" +
		"Shikigami: Lines\n" +
		"Channel \"left\":\n" +
		"    Cursed Technique: Take Item 0\n" +
		"Channel \"right\":\n" +
		"    Cursed Technique: Take Item 1\n" +
		"Maximum Technique: Combine\n" +
		"    From: left, right\n" +
		"Reveal: stdout\n"
	m := devWriteProgram(t, prog, "a\nb\n")

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 15 {
				analyzeCmd(m.gen, m.path, m.buf.text())()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 5 {
			next, cmd := m.runProgram()
			for _, msg := range collectMsgs(cmd) {
				_ = msg
			}
			_ = next
		}
	}()
	wg.Wait()
}

// And the finding itself: a program whose arguments are plainly read must not
// be told it ignores them. This is what the race produced, and what the lock
// has to keep producing correctly under repetition.
func TestDevArgumentsThatAreReadAreNotReportedAsIgnored(t *testing.T) {
	prog := "Cursed Energy: in.txt\n" +
		"Shikigami: Lines\n" +
		"Channel \"left\":\n" +
		"    Cursed Technique: Take Item 0\n" +
		"Channel \"right\":\n" +
		"    Cursed Technique: Take Item 1\n" +
		"Maximum Technique: Combine\n" +
		"    From: left, right\n" +
		"Reveal: stdout"
	m := devWriteProgram(t, prog, "a\nb\n")

	for range 20 {
		m = settle(t, m)
		for line, ds := range m.intel.diags {
			for _, d := range ds {
				if strings.Contains(d.Msg, "ignores the argument") {
					t.Fatalf("L%d: %s — but the statement reads it", line, d.Msg)
				}
			}
		}
	}
}
