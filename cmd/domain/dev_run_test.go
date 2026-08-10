package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// runToCompletion starts a run and delivers its result, standing in for the
// event loop. The run itself is a plain command, so this is the whole of what
// the runtime would have done.
func runToCompletion(t *testing.T, m devModel) devModel {
	t.Helper()
	next, cmd := m.runProgram()
	m = next.(devModel)
	if cmd == nil {
		return m // refused to start; the output pane says why
	}
	// The batch is {run, spinner tick}; only the run's message matters here.
	for _, msg := range collectMsgs(cmd) {
		if done, ok := msg.(devRunDoneMsg); ok {
			next, _ := m.Update(done)
			return next.(devModel)
		}
	}
	t.Fatal("the run produced no result")
	return m
}

// collectMsgs unwraps a command, flattening a batch into the messages it
// would have delivered.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// writeProgram puts a program and its input on disk and returns the model
// editing it, since running resolves `Cursed Energy:` against the program's
// own directory.
func devWriteProgram(t *testing.T, prog, input string) devModel {
	t.Helper()
	dir := t.TempDir()
	if input != "" {
		if err := os.WriteFile(filepath.Join(dir, "in.txt"), []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "p.domain")
	if err := os.WriteFile(path, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestDevModel(prog)
	m.path = path
	return m
}

// ---------------------------------------------------------------------------
// running
// ---------------------------------------------------------------------------

func TestDevRunShowsTheProgramsOutput(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n", "1\n2\n3\n")
	m = runToCompletion(t, m)

	if m.output == nil {
		t.Fatal("no output pane")
	}
	if m.output.err {
		t.Errorf("a working program reported an error: %v", m.output.lines)
	}
	if got := strings.Join(m.output.lines, "\n"); !strings.Contains(got, "6") {
		t.Errorf("output is %q, want it to contain 6", got)
	}
	if m.running {
		t.Error("still running after the result arrived")
	}
}

// The run is of the buffer, not of the file — the point of running here.
func TestDevRunUsesTheUnsavedBuffer(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n", "1\n2\n3\n")

	// Change Sum to Count without saving.
	m.buf.lines[2] = "Maximum Technique: Count"
	m = runToCompletion(t, m)

	if got := strings.Join(m.output.lines, "\n"); !strings.Contains(got, "3") {
		t.Errorf("output is %q — the run used the file rather than the buffer", got)
	}
}

// A program that does not resolve says so instead of running.
func TestDevRunReportsAProgramItCannotResolve(t *testing.T) {
	m := devWriteProgram(t, "Cursed Tecnique: Sum\n", "")
	next, cmd := m.runProgram()
	m = next.(devModel)

	if cmd != nil {
		t.Error("a program that cannot resolve should not have started")
	}
	if m.output == nil || !m.output.err {
		t.Fatal("no error reported")
	}
	if !strings.Contains(m.output.title, "cannot run") {
		t.Errorf("title is %q", m.output.title)
	}
}

// A program with nothing to say says that, rather than showing an empty pane
// that looks identical to a failure.
func TestDevRunExplainsAnEmptyResult(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\n", "1\n2\n")
	m = runToCompletion(t, m)
	if got := strings.Join(m.output.lines, "\n"); !strings.Contains(got, "Reveal") {
		t.Errorf("output is %q, want a hint about the missing sink", got)
	}
}

// Ctrl+C during a run stops the run rather than leaving the editor — the whole
// reason the run is on a command instead of inside Update.
func TestDevCtrlCStopsARunRatherThanLeaving(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nReveal: stdout\n", "x")
	next, _ := m.runProgram()
	m = next.(devModel)
	if !m.running {
		t.Fatal("the run did not start")
	}

	next, cmd := m.key(devKeyMsg("ctrl+c"))
	m = next.(devModel)
	if cmd != nil {
		t.Error("ctrl+c during a run should not quit the editor")
	}
	if !m.interrupt.Stopped() {
		t.Error("the run was not asked to stop")
	}
}

// Keystrokes during a run do not reach the program: a buffer that changed
// under a running program would leave the output describing something else.
func TestDevKeystrokesDuringARunDoNotEdit(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nReveal: stdout\n", "x")
	next, _ := m.runProgram()
	m = next.(devModel)
	before := m.buf.text()

	m = devKey(m, "z")
	if m.buf.text() != before {
		t.Errorf("a keystroke reached the buffer during a run: %q", m.buf.text())
	}
}

// A result that arrives after the program changed describes something that is
// no longer on screen.
func TestDevStaleRunResultIsDiscarded(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nReveal: stdout\n", "x")
	m.running = true
	next, _ := m.Update(devRunDoneMsg{result: devRunResult{gen: m.gen - 1, output: "old"}})
	m = next.(devModel)
	if m.output != nil {
		t.Error("a stale run result was shown")
	}
	if m.running {
		t.Error("the run flag was not cleared")
	}
}

// ---------------------------------------------------------------------------
// the output pane
// ---------------------------------------------------------------------------

func TestDevOutputPaneScrollsAndCloses(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	var lines []string
	for i := range 50 {
		lines = append(lines, "line "+string(rune('a'+i%26)))
	}
	m.output = &devOutput{title: "ran", lines: lines}

	m = devKey(m, "down")
	if m.output == nil || m.output.top != 1 {
		t.Fatalf("scrolling did not move the pane")
	}
	m = devKey(m, "x")
	if m.output != nil {
		t.Error("any other key should close the pane")
	}
}

// The pane takes the bottom of the screen and the whole frame still fits.
func TestDevOutputPaneKeepsTheFrameHeight(t *testing.T) {
	m := newTestDevModel(strings.Repeat("Reveal: stdout\n", 40))
	before := strings.Count(m.view(), "\n")
	m.output = &devOutput{title: "ran", lines: []string{"6"}}
	after := strings.Count(m.view(), "\n")
	if before != after {
		t.Errorf("the frame changed height when the pane opened: %d then %d", before, after)
	}
}

func TestDevOutputPaneSaysWhatHappened(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n", "1\n2\n")
	m = runToCompletion(t, m)
	if got := ansi.Strip(strings.Join(m.outputView(), "\n")); !strings.Contains(got, "ran") {
		t.Errorf("the pane does not say what happened: %q", got)
	}
}

// ---------------------------------------------------------------------------
// the stepper
// ---------------------------------------------------------------------------

func TestDevVisualizeOpensTheStepperOverTheBuffer(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n", "1\n2\n3\n")
	m = runToCompletion(t, m)
	next, _ := m.openStepper()
	m = next.(devModel)

	if m.stepper == nil {
		t.Fatal("the stepper did not open")
	}
	// The stepper owns the screen while it is open.
	if !strings.Contains(ansi.Strip(m.view()), "Sum") {
		t.Errorf("the stepper is not being drawn:\n%s", ansi.Strip(m.view()))
	}
}

// The recording carries the buffer's source, so the stepper's source pane
// shows the program on screen rather than whatever is on disk under that name.
func TestDevVisualizeRecordsTheUnsavedBuffer(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n", "1\n2\n3\n")
	m.buf.lines[2] = "Maximum Technique: Count" // unsaved

	m = runToCompletion(t, m)
	next, _ := m.openStepper()
	m = next.(devModel)
	if m.stepper == nil {
		t.Fatal("the stepper did not open")
	}
	src := strings.Join(m.stepper.view.source(), "\n")
	if !strings.Contains(src, "Count") {
		t.Errorf("the source pane shows the file, not the buffer:\n%s", src)
	}
}

// An unsaved buffer has no name, and "(unsaved)" is more honest than an empty
// one in the stepper's header.
func TestDevVisualizeNamesAnUnsavedBuffer(t *testing.T) {
	m := newTestDevModel("Cursed Energy: in.txt\nReveal: stdout")
	if got := m.runPath(); got != "(unsaved)" {
		t.Errorf("runPath is %q", got)
	}
}

func TestDevVisualizeReportsAProgramItCannotResolve(t *testing.T) {
	m := devWriteProgram(t, "Cursed Tecnique: Sum\n", "")
	m = runToCompletion(t, m)
	if m.output == nil || !m.output.err {
		t.Error("nothing explained the refusal")
	}
	next, _ := m.openStepper()
	if next.(devModel).stepper != nil {
		t.Error("the stepper opened on a program that never recorded")
	}
}

// Opening the stepper before anything has run says so rather than running the
// program on your behalf: running is a decision, and taking it when someone
// asked to look at a run is how a key does something surprising.
func TestDevStepperSaysWhenNothingHasRun(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: in.txt\nReveal: stdout\n", "x")
	next, _ := m.openStepper()
	m = next.(devModel)
	if m.stepper != nil {
		t.Error("the stepper opened with no recording")
	}
	if !strings.Contains(m.status, "nothing recorded") {
		t.Errorf("status is %q", m.status)
	}
}

// ---------------------------------------------------------------------------
// choosing the input
// ---------------------------------------------------------------------------

// The input is bound in the program rather than beside it, so the program
// behaves the same here as it does under `domain run`.
func TestDevBindInputRewritesTheSourceStage(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: old.txt\nReveal: stdout\n", "")
	m, ok := m.bindInput(filepath.Join(m.baseDir(), "day7.txt"))
	if !ok {
		t.Fatal("binding failed")
	}
	if got := m.buf.lines[0]; got != "Cursed Energy: day7.txt" {
		t.Errorf("source stage is %q", got)
	}
	if !m.dirty {
		t.Error("rewriting the source stage did not mark the buffer dirty")
	}
}

// A program with no source stage yet gets one, at the top, which is where it
// has to be.
func TestDevBindInputAddsASourceStageWhenThereIsNone(t *testing.T) {
	m := devWriteProgram(t, "Maximum Technique: Sum\nReveal: stdout\n", "")
	m, _ = m.bindInput(filepath.Join(m.baseDir(), "day7.txt"))

	if got := m.buf.lines[0]; got != "Cursed Energy: day7.txt" {
		t.Errorf("first line is %q", got)
	}
	if len(m.buf.lines) != 3 {
		t.Errorf("expected the original lines to survive, got %q", m.buf.text())
	}
}

// Binding is one undo step.
func TestDevBindInputIsOneUndoStep(t *testing.T) {
	m := devWriteProgram(t, "Cursed Energy: old.txt\nReveal: stdout\n", "")
	before := m.buf.text()
	m, _ = m.bindInput(filepath.Join(m.baseDir(), "day7.txt"))
	m = devKey(m, "ctrl+z")
	if m.buf.text() != before {
		t.Errorf("undo gave %q, want %q", m.buf.text(), before)
	}
}

// The input browser lists every file, not only programs: an input is whatever
// the puzzle gave you.
func TestDevInputPickerListsNonProgramFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"day7.txt", "notes.md", "p.domain"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := newPicker(":load", dir)
	p.anyFile = true
	p.setDir(dir)

	var names []string
	for _, e := range p.entries {
		names = append(names, e.name)
	}
	for _, want := range []string{"day7.txt", "notes.md", "p.domain"} {
		if !slicesContains(names, want) {
			t.Errorf("%s missing from %v", want, names)
		}
	}
}

func slicesContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
