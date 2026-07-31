package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/ir"
)

// The editor is tested the way the visualizer is: by driving the model with
// injected messages. Evaluation now happens on a command rather than inside
// Update, so a test has to run the commands the model hands back — which is
// what newTestModel/submit do, standing in for the runtime.

// newTestModel returns a model whose history is isolated from the machine
// running the tests, and whose preview does not make anything wait.
func newTestModel(t *testing.T) replModel {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old := previewDelay
	previewDelay = time.Millisecond
	t.Cleanup(func() { previewDelay = old })
	return newReplModel()
}

// run executes a command the way the runtime would — unwrapping batches,
// feeding every message back into the model — and returns the settled model.
//
// Two kinds of message are dropped rather than delivered. Timer-driven ones
// (the spinner frame, the preview debounce) would make a test sleep to watch
// an animation. Cursor blinks would never end: each blink schedules the next,
// which is exactly right in a running program and a non-terminating loop in a
// driver that runs every command it is handed.
func run(t *testing.T, m replModel, cmd tea.Cmd) replModel {
	t.Helper()
	const budget = 200 // a settled model stops producing work long before this
	queue := expand(cmd)
	for i := 0; len(queue) > 0; i++ {
		if i > budget {
			t.Fatalf("the model never settled: %d messages still queued", len(queue))
		}
		msg := queue[0]
		queue = queue[1:]
		if skipInTests(msg) {
			continue
		}
		next, c := m.Update(msg)
		m = next.(replModel)
		queue = append(queue, expand(c)...)
	}
	return m
}

// skipInTests reports the messages the driver drops; see run.
func skipInTests(msg tea.Msg) bool {
	switch msg.(type) {
	case nil, spinner.TickMsg, previewTickMsg, watchTickMsg:
		// A watch re-arms itself on every tick, so a driver that ran every
		// command it was handed would never stop watching. Tests deliver the
		// tick they mean to test directly.
		return true
	}
	return strings.Contains(fmt.Sprintf("%T", msg), "cursor.")
}

// expand flattens a command into the messages it produces.
func expand(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		out = append(out, expand(c)...)
	}
	return out
}

// printed is what the session wrote, with the color taken back out: an
// interactive session paints its results, and a test should assert on the
// text rather than on the escapes around it.
func printed(m replModel) string { return ansi.Strip(m.buf.String()) }

// press sends one keystroke and settles whatever it started.
func press(t *testing.T, m replModel, msg tea.KeyPressMsg) replModel {
	t.Helper()
	next, cmd := m.Update(msg)
	return run(t, next.(replModel), cmd)
}

// submit types a line and presses enter.
func submit(t *testing.T, m replModel, line string) replModel {
	t.Helper()
	m.ti.SetValue(line)
	return press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestReplTTYCtrlCQuits(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned a nil command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c on an empty line did not return tea.Quit")
	}
}

func TestReplTTYSimpleStatementEvaluates(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("3\n1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")

	if got := m.ti.Value(); got != "" {
		t.Errorf("input not cleared after submit: %q", got)
	}
	if got := m.ti.Prompt; got != promptTop {
		t.Errorf("prompt should stay top-level: %q", got)
	}
	if !strings.Contains(printed(m), `=> "3\n1\n2"`) {
		t.Errorf("missing evaluated result:\n%s", printed(m))
	}
}

func TestReplTTYNeedsBlockAutoIndentsAndCompletes(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, `Cursed Technique: Split Text by "\n"`)
	m = submit(t, m, "Channeled Energy: Convert To Integers")
	m = submit(t, m, "Cursed Technique: Map Each") // needs Using: — continuation mode

	if len(m.core.pending) == 0 {
		t.Fatal("expected a pending block after a statement needing Using:")
	}
	if got := m.ti.Value(); got != "    " {
		t.Errorf("next line not auto-indented: %q", got)
	}
	if got := m.ti.Prompt; got != promptContinue {
		t.Errorf("prompt not switched to continuation: %q", got)
	}

	m = submit(t, m, "    Using: (x) -> x * 10")
	if got := m.ti.Value(); got != "    " {
		t.Errorf("continuation line not re-seeded with the 4-space indent: %q", got)
	}

	m = submit(t, m, "    ") // blank but for the auto-inserted indent — ends the block
	if len(m.core.pending) != 0 {
		t.Error("block did not end on the seeded-but-otherwise-empty line")
	}
	if !strings.Contains(printed(m), "=> [10, 20] : List<Int>") {
		t.Errorf("block statement result missing:\n%s", printed(m))
	}
}

func TestReplTTYForcedContinuationViaCtrlOrAltEnter(t *testing.T) {
	for _, mod := range []tea.KeyMod{tea.ModCtrl, tea.ModAlt} {
		t.Chdir(t.TempDir())
		if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := newTestModel(t)
		m.ti.SetValue("Cursed Energy: nums.txt") // a complete statement on its own
		m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: mod})

		if len(m.core.pending) == 0 {
			t.Fatalf("mod %v: did not force a pending block", mod)
		}
		if len(m.core.stmts) != 0 {
			t.Errorf("mod %v: should not have evaluated the statement on its own", mod)
		}
		if got := m.ti.Value(); got != "    " {
			t.Errorf("mod %v: next line not auto-indented: %q", mod, got)
		}
	}
}

func TestReplTTYCtrlEnterNoOpOnEmptyLine(t *testing.T) {
	m := newTestModel(t)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	m2 := next.(replModel)
	if cmd != nil {
		t.Error("ctrl+enter on an empty line should not print anything")
	}
	if len(m2.core.pending) != 0 || len(m2.hist.entries) != 0 {
		t.Error("ctrl+enter on an empty line should be a total no-op")
	}
}

// A forced block is for statements; a :command has no block to open, and
// forcing one on it used to guarantee a parse error on the next line.
func TestReplTTYForcedBlockSkipsCommands(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue(":help")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})

	if len(m.core.pending) != 0 {
		t.Errorf("alt+enter forced a :command into a block: %#v", m.core.pending)
	}
	if !strings.Contains(printed(m), ":quit") {
		t.Errorf(":help did not run:\n%s", printed(m))
	}
}

// An empty line at the top level closes nothing, so it should leave no trace
// in the scrollback.
func TestReplTTYBlankLineIsSilent(t *testing.T) {
	m := newTestModel(t)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("blank enter printed something: %#v", cmd())
	}
	if got := next.(replModel).buf.String(); got != "" {
		t.Errorf("blank enter wrote to the session: %q", got)
	}
}

func TestReplTTYHistoryRecall(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, ":type")

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.ti.Value(); got != ":type" {
		t.Errorf("up did not recall the most recent line: %q", got)
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.ti.Value(); got != "Cursed Energy: nums.txt" {
		t.Errorf("second up did not recall the earlier line: %q", got)
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.ti.Value(); got != ":type" {
		t.Errorf("down did not step forward in history: %q", got)
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.ti.Value(); got != "" {
		t.Errorf("down past the end should clear the line: %q", got)
	}
}

// Pressing Up parks the line being typed instead of destroying it.
func TestReplTTYHistoryKeepsTheDraft(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")

	m.ti.SetValue("Cursed Techni") // a line in progress
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.ti.Value(); got != "Cursed Energy: nums.txt" {
		t.Fatalf("up did not recall: %q", got)
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.ti.Value(); got != "Cursed Techni" {
		t.Errorf("the draft was not restored: %q", got)
	}
}

func TestReplTTYHistoryPersistsAcrossSessions(t *testing.T) {
	t.Chdir(t.TempDir())
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	first := newReplModel()
	first.hist.add("Cursed Technique: Sum All")
	first.hist.add("Cursed Technique: Sum All") // an immediate repeat is not stored twice
	first.hist.save()

	second := newReplModel()
	if got := second.hist.entries; len(got) != 1 || got[0] != "Cursed Technique: Sum All" {
		t.Errorf("history did not survive the session: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(state, "domain", "repl_history")); err != nil {
		t.Errorf("history file missing: %v", err)
	}
}

// A pasted program arrives as one message with newlines in it. Feeding it to
// the editor as text would collapse those newlines to spaces and silently
// mangle the program, so paste is submitted line by line instead.
func TestReplTTYMultiLinePasteSubmitsEveryLine(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	program := "Cursed Energy: nums.txt\nCursed Technique: Split Text by \"\\n\"\nChanneled Energy: Convert To Integers\nMaximum Technique: Sum\n"

	next, cmd := m.Update(tea.PasteMsg{Content: program})
	m = run(t, next.(replModel), cmd)

	if len(m.core.stmts) != 4 {
		t.Errorf("pasted program did not become 4 statements: %#v", m.core.stmts)
	}
	if !strings.Contains(printed(m), "=> 6 : Int") {
		t.Errorf("pasted program did not run through:\n%s", printed(m))
	}
}

func TestReplTTYSingleLinePasteIsOrdinaryTyping(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue("Cursed ")
	m.ti.CursorEnd()
	next, _ := m.Update(tea.PasteMsg{Content: "Energy: in.txt"})
	m = next.(replModel)

	if got := m.ti.Value(); got != "Cursed Energy: in.txt" {
		t.Errorf("single-line paste did not land in the line: %q", got)
	}
	if len(m.core.stmts) != 0 {
		t.Error("single-line paste should not submit anything")
	}
}

func TestReplTTYTabCompletesUniqueMatch(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue("Cursed T")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})

	if got := m.ti.Value(); got != "Cursed Technique: " {
		t.Fatalf("tab did not complete the keyword: %q", got)
	}
	if !m.completing {
		t.Error("expected completing to be true right after Tab")
	}
}

func TestReplTTYTabCyclesThroughMultipleCandidates(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue("Domain Expansion: Sort")

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	first := m.ti.Value()
	if first != "Domain Expansion: Sort" && first != "Domain Expansion: Sort By" {
		t.Fatalf("unexpected first completion: %q", first)
	}

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	second := m.ti.Value()
	if second == first {
		t.Fatal("second tab did not advance to a different candidate")
	}

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if third := m.ti.Value(); third != first {
		t.Errorf("third tab should wrap back to the first candidate: got %q, want %q", third, first)
	}
}

func TestReplTTYTabCompletionResetsOnOtherKey(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue("Cursed T")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = press(t, m, tea.KeyPressMsg{Text: "x"})

	if m.completing {
		t.Error("typing a character should exit completion cycling")
	}
	if got := m.ti.Value(); got != "Cursed Technique: x" {
		t.Errorf("typed character should append after the accepted completion: %q", got)
	}
}

func TestReplTTYTabNoMatchIsNoOp(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue("zzz")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m2 := next.(replModel)

	if cmd != nil {
		t.Error("no-match tab should not print anything")
	}
	if got := m2.ti.Value(); got != "zzz" {
		t.Errorf("no-match tab should leave the line untouched: %q", got)
	}
	if m2.completing {
		t.Error("no-match tab should not enter completing state")
	}
}

func TestReplTTYTabCompletesReplCommandWithoutDoublingColon(t *testing.T) {
	// Regression: completeToken's :command candidates already include the
	// leading ':', so a tokenStart that also preserves the line's own ':'
	// would splice in a second one ("::load").
	m := newTestModel(t)
	m.ti.SetValue(":lo")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})

	if got := m.ti.Value(); got != ":load" {
		t.Errorf("tab did not complete the :command cleanly: %q", got)
	}
}

// The editor counts the cursor in runes; completion slices bytes. A single
// multi-byte character earlier in the line used to be enough to splice the
// completion into the middle of a word and duplicate the tail.
func TestReplTTYTabCompletionSurvivesNonASCII(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue(`Channel "é": Su`)
	m.ti.CursorEnd()
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})

	got := m.ti.Value()
	if !strings.HasPrefix(got, `Channel "é": `) {
		t.Fatalf("completion mangled the line: %q", got)
	}
	if strings.HasSuffix(got, "u") && !strings.HasSuffix(got, "Su") {
		t.Errorf("completion left the tail of the replaced token behind: %q", got)
	}
	if strings.Count(got, `"`) != 2 {
		t.Errorf("completion damaged the quoted channel name: %q", got)
	}
}

// Ghost text: the editor is fed whole-line suggestions so it can show the top
// completion candidate ahead of the cursor while typing.
func TestReplTTYSuggestionsAreWholeLines(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue("Cursed T")
	m.ti.CursorEnd()
	m.refreshSuggestions()

	got := m.ti.AvailableSuggestions()
	if len(got) == 0 {
		t.Fatal("no suggestions offered")
	}
	for _, s := range got {
		if !strings.HasPrefix(strings.ToLower(s), "cursed t") {
			t.Errorf("suggestion %q does not continue the typed line", s)
		}
	}
}

// The type of the statement being typed, before it is submitted.
func TestReplTTYLiveTypePreview(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")

	m.ti.SetValue(`Cursed Technique: Split Text by "\n"`)
	next, cmd := m.Update(previewTickMsg{gen: m.previewGen})
	m = run(t, next.(replModel), cmd)

	if m.preview != "List<Text>" {
		t.Errorf("preview = %q, want List<Text>", m.preview)
	}
	if !strings.Contains(m.View().Content, "List<Text>") {
		t.Errorf("preview not shown in the view:\n%s", m.View().Content)
	}
	// A stale answer (an earlier keystroke's) must not overwrite a newer one.
	m.previewGen++
	next, _ = m.Update(previewMsg{gen: m.previewGen - 1, typ: "Int", okay: true})
	if got := next.(replModel).preview; got != "List<Text>" {
		t.Errorf("stale preview overwrote the current one: %q", got)
	}
}

// A runaway loop is stopped by Ctrl+C rather than by killing the terminal.
func TestReplTTYCtrlCInterruptsARunawayLoop(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, `Cursed Technique: Split Text by "\n"`)
	m = submit(t, m, "Channeled Energy: Convert To Integers")
	m = submit(t, m, "Maximum Technique: Sum")
	m = submit(t, m, "Simple Domain: While")
	m = submit(t, m, "    Using: (v) -> v > 0")
	m = submit(t, m, "    Cursed Technique: Apply")
	m = submit(t, m, "        Using: (v) -> v + 1")

	// Submit the block-closing line without settling it: the run never ends
	// on its own, so the interrupt has to arrive while it is still going.
	m.ti.SetValue("")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(replModel)
	if !m.evaluating {
		t.Fatal("the loop did not start evaluating")
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- expand(cmd)[0] }()

	// The interrupt goes in while the evaluation is in flight.
	interrupted, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = interrupted.(replModel)
	if !m.interrupting {
		t.Fatal("ctrl+c during evaluation did not ask for an interrupt")
	}

	select {
	case msg := <-done:
		next, cmd := m.Update(msg)
		m = run(t, next.(replModel), cmd)
	case <-time.After(30 * time.Second):
		t.Fatal("the run was not interrupted")
	}

	if m.evaluating {
		t.Error("the model is still evaluating after the interrupt")
	}
	if !strings.Contains(printed(m), "interrupted") {
		t.Errorf("the interrupt was not reported:\n%s", printed(m))
	}
}

// Ctrl+C with a line in progress clears the line; with a block open it drops
// the block. Only an empty prompt with nothing pending quits.
func TestReplTTYCtrlCClearsBeforeItQuits(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue("Cursed Technique: Su")
	m = press(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.ti.Value() != "" {
		t.Errorf("ctrl+c did not clear the line: %q", m.ti.Value())
	}

	m.core.pending = []string{"Cursed Technique: Map Each"}
	m.ti.Prompt = promptContinue
	m = press(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if len(m.core.pending) != 0 {
		t.Error("ctrl+c did not discard the pending block")
	}
	if m.ti.Prompt != promptTop {
		t.Errorf("prompt not restored after discarding the block: %q", m.ti.Prompt)
	}
}

// Ctrl+D closes a session, but not one with a half-typed block in it.
func TestReplTTYCtrlDKeepsAPendingBlock(t *testing.T) {
	m := newTestModel(t)
	m.core.pending = []string{"Cursed Technique: Map Each"}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = next.(replModel)

	if cmd != nil {
		t.Error("ctrl+d with a pending block should not quit")
	}
	if len(m.core.pending) == 0 {
		t.Error("ctrl+d discarded the pending block")
	}
	if m.status == "" {
		t.Error("ctrl+d with a pending block should say why nothing happened")
	}
}

// Unsaved statements turn a quit into a confirmation, once.
func TestReplTTYQuitGuardsUnsavedWork(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")

	if _, ok := guardUnsavedQuit(m, tea.QuitMsg{}).(confirmQuitMsg); !ok {
		t.Fatal("quitting with unsaved statements was not held back")
	}
	next, _ := m.Update(confirmQuitMsg{})
	m = next.(replModel)
	if !m.confirmingQuit || m.status == "" {
		t.Fatal("the confirmation was not surfaced to the user")
	}
	if _, ok := guardUnsavedQuit(m, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Error("the second quit should go through")
	}

	m.core.dirty = false
	if _, ok := guardUnsavedQuit(m, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Error("a saved session should quit without a confirmation")
	}
}

func TestReplTTYWindowSizeAndTitle(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 64, Height: 20})
	m = next.(replModel)

	if m.ti.Width() != 64-len(promptTop)-1 {
		t.Errorf("input width = %d, want %d", m.ti.Width(), 64-len(promptTop)-1)
	}
	if m.core.width != 64 {
		t.Errorf("charts were not told the width: %d", m.core.width)
	}
	if got := m.View().WindowTitle; got != "domain repl" {
		t.Errorf("window title before any value = %q", got)
	}

	m = submit(t, m, "Cursed Energy: nums.txt")
	if got := m.View().WindowTitle; got != "domain repl — Text" {
		t.Errorf("window title = %q, want the current type", got)
	}
}

func TestReplTTYCopyPutsTheProgramOnTheClipboard(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")

	handled, model, cmd := m.terminalCommand(":copy")
	if !handled || cmd == nil {
		t.Fatal(":copy was not handled by the editor")
	}
	// The message tea.SetClipboard produces is internal to bubbletea, so the
	// assertion is on what it carries rather than on its type.
	if got := fmt.Sprint(cmd()); !strings.Contains(got, "Cursed Energy: nums.txt") {
		t.Errorf("clipboard command did not carry the program: %q", got)
	}
	if model.(replModel).status == "" {
		t.Error(":copy said nothing about what it did")
	}
}

func TestReplTTYKeysTogglesHelp(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if !m.showHelp {
		t.Fatal("ctrl+g did not open the key list")
	}
	if !strings.Contains(m.View().Content, "complete") {
		t.Errorf("key list missing from the view:\n%s", m.View().Content)
	}
	m = press(t, m, tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if m.showHelp {
		t.Error("ctrl+g did not close the key list again")
	}
}

// :edit hands the program to $EDITOR and adopts whatever comes back.
func TestReplTTYEditReloadsTheEditedProgram(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")

	handled, model, cmd := m.terminalCommand(":edit")
	if !handled || cmd == nil {
		t.Fatal(":edit was not handled by the editor")
	}
	m = model.(replModel)

	edited := filepath.Join(t.TempDir(), "edited.domain")
	program := "Cursed Energy: nums.txt\nCursed Technique: Split Text by \"\\n\"\nChanneled Energy: Convert To Integers\nMaximum Technique: Sum\n"
	if err := os.WriteFile(edited, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(editDoneMsg{path: edited})
	m = run(t, next.(replModel), cmd)

	if len(m.core.stmts) != 4 {
		t.Errorf("edited program not adopted: %#v", m.core.stmts)
	}
	if !strings.Contains(printed(m), "=> 6 : Int") {
		t.Errorf("edited program was not replayed:\n%s", printed(m))
	}
}

func TestReplTTYSuspend(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+z returned no command")
	}
	if _, ok := cmd().(tea.SuspendMsg); !ok {
		t.Errorf("ctrl+z did not suspend: %#v", cmd())
	}
}

// The confirmation survives the keystrokes that answer it: typing `:quit`
// again used to clear the flag the guard had just set, so a session with
// unsaved work could never be left by typing at all.
func TestReplTTYQuitConfirmationSurvivesTyping(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")

	next, _ := m.Update(confirmQuitMsg{})
	m = next.(replModel)
	for _, r := range ":quit" {
		m = press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !m.confirmingQuit {
		t.Fatal("typing the command again revoked the confirmation")
	}
	if _, ok := guardUnsavedQuit(m, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Error("the retyped quit was held back a second time")
	}
}

// `:quit!` says it outright, and does not need the guard's permission.
func TestReplTTYForceQuitSkipsTheGuard(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")

	// Settled, not merely submitted: finishing the line must not re-arm the
	// guard the line was answering.
	m = submit(t, m, ":quit!")
	if !m.confirmingQuit {
		t.Fatal(":quit! did not disarm the guard")
	}
	if _, ok := guardUnsavedQuit(m, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Error(":quit! was held back by the guard")
	}
}

// New unsaved work re-arms the guard after an earlier confirmation.
func TestReplTTYQuitGuardReArmsOnNewWork(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")
	next, _ := m.Update(confirmQuitMsg{})
	m = next.(replModel)

	m = submit(t, m, `Cursed Technique: Split Text by "\n"`)
	if m.confirmingQuit {
		t.Error("a new statement should put the guard back")
	}
	if _, ok := guardUnsavedQuit(m, tea.QuitMsg{}).(confirmQuitMsg); !ok {
		t.Error("the guard did not hold back a quit after new work")
	}
}

func TestReplTTYCtrlLClearsTheScreen(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+l returned no command")
	}
	if got := fmt.Sprintf("%T", cmd()); !strings.Contains(got, "clearScreen") {
		t.Errorf("ctrl+l did not clear the screen: %s", got)
	}
}

// :paste asks the terminal for its clipboard and runs what comes back; an
// OSC 52 report the session did not ask for is ignored.
func TestReplTTYPasteReadsTheClipboard(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)

	unsolicited, _ := m.Update(tea.ClipboardMsg{Content: "Cursed Energy: nums.txt"})
	if len(unsolicited.(replModel).core.stmts) != 0 {
		t.Error("a clipboard report nobody asked for was run as a program")
	}

	handled, model, cmd := m.terminalCommand(":paste")
	if !handled || cmd == nil {
		t.Fatal(":paste was not handled by the editor")
	}
	m = model.(replModel)
	if !m.awaitingClipboard {
		t.Fatal(":paste did not arm the clipboard reply")
	}
	program := "Cursed Energy: nums.txt\nCursed Technique: Extract Integers\nMaximum Technique: Sum\n"
	next, cmd := m.Update(tea.ClipboardMsg{Content: program})
	m = run(t, next.(replModel), cmd)

	if len(m.core.stmts) != 3 {
		t.Errorf("clipboard program not adopted: %#v", m.core.stmts)
	}
	if !strings.Contains(printed(m), "=> 6 : Int") {
		t.Errorf("clipboard program did not run:\n%s", printed(m))
	}
}

// A pasted program reports once: the statements, anything that went wrong, and
// the value it ended on — not one intermediate value per line.
func TestReplTTYPasteIsReportedAsOneBlock(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	program := "Cursed Energy: nums.txt\nCursed Technique: Extract Integers\nMaximum Technique: Sum\n"

	var printouts []string
	next, cmd := m.Update(tea.PasteMsg{Content: program})
	m = next.(replModel)
	printouts = append(printouts, collectPrintlns(cmd, &m, t)...)

	joined := ansi.Strip(strings.Join(printouts, "\n"))
	if strings.Count(joined, "=> ") != 1 {
		t.Errorf("intermediate values were not folded away:\n%s", joined)
	}
	if !strings.Contains(joined, "=> 6 : Int") {
		t.Errorf("the final value is missing:\n%s", joined)
	}
	if !strings.Contains(joined, "pasted 3 line(s)") {
		t.Errorf("no summary of what was pasted:\n%s", joined)
	}
	for _, want := range []string{"Cursed Energy: nums.txt", "Maximum Technique: Sum"} {
		if !strings.Contains(joined, want) {
			t.Errorf("pasted statement %q missing from the report:\n%s", want, joined)
		}
	}
	if m.pasting {
		t.Error("the paste batch was left open")
	}
}

// An error inside a paste is never folded away.
func TestReplTTYPasteKeepsErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	program := "Cursed Energy: nums.txt\nCursed Tecnique: Split\nCursed Technique: Extract Integers\n"

	next, cmd := m.Update(tea.PasteMsg{Content: program})
	m = next.(replModel)
	joined := ansi.Strip(strings.Join(collectPrintlns(cmd, &m, t), "\n"))

	if !strings.Contains(joined, `unknown keyword "Cursed Tecnique"`) {
		t.Errorf("the error inside the paste was swallowed:\n%s", joined)
	}
	if !strings.Contains(joined, "=> [1, 2] : List<Int>") {
		t.Errorf("the surviving statements did not run:\n%s", joined)
	}
}

// collectPrintlns settles the model like run, keeping what it printed.
func collectPrintlns(cmd tea.Cmd, m *replModel, t *testing.T) []string {
	t.Helper()
	var out []string
	queue := expand(cmd)
	for i := 0; len(queue) > 0 && i < 200; i++ {
		msg := queue[0]
		queue = queue[1:]
		if skipInTests(msg) {
			continue
		}
		if line, ok := printedLine(msg); ok {
			out = append(out, line)
		}
		next, c := m.Update(msg)
		*m = next.(replModel)
		queue = append(queue, expand(c)...)
	}
	return out
}

// printedLine extracts the body of a tea.Println message, whose type is
// internal to bubbletea — hence the string form.
func printedLine(msg tea.Msg) (string, bool) {
	if !strings.Contains(fmt.Sprintf("%T", msg), "printLineMessage") {
		return "", false
	}
	body := fmt.Sprintf("%v", msg)
	return strings.TrimSuffix(strings.TrimPrefix(body, "{"), "}"), true
}

func TestReplTTYEvaluatingHintGrowsWithTime(t *testing.T) {
	m := newTestModel(t)
	m.evaluating = true
	for _, tc := range []struct {
		elapsed time.Duration
		want    string
	}{
		{200 * time.Millisecond, "evaluating… ctrl+c interrupts"},
		{3 * time.Second, "evaluating 3.0s… ctrl+c interrupts"},
		{42 * time.Second, "evaluating 42s… ctrl+c interrupts (an unbounded loop looks like this)"},
	} {
		m.evalStarted = time.Now().Add(-tc.elapsed)
		if got := m.evalHint(); got != tc.want {
			t.Errorf("after %s: hint = %q, want %q", tc.elapsed, got, tc.want)
		}
	}
	m.interrupting = true
	if got := m.evalHint(); got != "interrupting…" {
		t.Errorf("interrupting hint = %q", got)
	}
}

// The cursor's shape says where the session is.
func TestReplTTYCursorShapeFollowsTheMode(t *testing.T) {
	m := newTestModel(t)
	c := m.cursor()
	if c == nil || c.Shape != tea.CursorBar {
		t.Fatalf("top-level cursor = %#v, want a bar", c)
	}

	m.core.pending = []string{"Cursed Technique: Map Each"}
	if c := m.cursor(); c == nil || c.Shape != tea.CursorBlock {
		t.Errorf("in-block cursor = %#v, want a block", c)
	}

	m.evaluating = true
	if c := m.cursor(); c != nil {
		t.Errorf("a running program should hide the cursor, got %#v", c)
	}
}

// Progress: the replay reports how many of the program's stages are done, and
// the terminal's own indicator follows the same number.
func TestReplTTYProgressTracksTheReplay(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, "Cursed Technique: Extract Integers")
	m = submit(t, m, "Maximum Technique: Sum")

	done, total := m.progress.Counts()
	if total != 3 || done != 3 {
		t.Errorf("after a 3-stage replay: %d/%d done, want 3/3", done, total)
	}
	if pct, known := m.progress.Percent(); !known || pct != 100 {
		t.Errorf("finished replay = %d%% (known=%v), want 100%%", pct, known)
	}

	// Idle: no bar in the view, and no indicator on the terminal.
	if bar := m.progressBar(); !strings.Contains(bar, "3/3") {
		t.Errorf("progress bar = %q", bar)
	}
	if m.terminalProgress() != nil {
		t.Error("an idle session should clear the terminal's progress indicator")
	}

	m.evaluating = true
	if got := m.terminalProgress(); got == nil || got.Value != 100 {
		t.Errorf("running progress indicator = %#v", got)
	}
	m.progress.Reset()
	if got := m.terminalProgress(); got == nil || got.State != tea.ProgressBarIndeterminate {
		t.Errorf("before the stage count is known the indicator should be indeterminate, got %#v", got)
	}
}

// Nested work is not progress: a loop is one stage that takes a while, not one
// unit of progress per iteration.
func TestReplTTYProgressCountsTopLevelStagesOnly(t *testing.T) {
	p := &progressCounter{}
	p.SetTotal(2)
	p.Step(ir.StepEvent{Depth: 0})
	for i := 0; i < 50; i++ {
		p.Step(ir.StepEvent{Depth: 1})
	}
	if done, _ := p.Counts(); done != 1 {
		t.Errorf("done = %d, want 1 (nested steps are not stages)", done)
	}
	p.Step(ir.StepEvent{Depth: 0})
	p.Step(ir.StepEvent{Depth: 0}) // a Channel body can report past the count
	if pct, _ := p.Percent(); pct != 100 {
		t.Errorf("pct = %d, want it clamped to 100", pct)
	}
}

// Previews are for a window someone is looking at.
func TestReplTTYPreviewsPauseWhenBlurred(t *testing.T) {
	m := newTestModel(t)
	next, cmd := m.Update(tea.BlurMsg{})
	m = next.(replModel)
	if cmd != nil {
		t.Error("losing focus should not start work")
	}
	m.ti.SetValue("Cursed Technique: Sum All")
	if got := m.schedulePreview(); got != nil {
		t.Error("a blurred session scheduled a preview")
	}

	next, cmd = m.Update(tea.FocusMsg{})
	m = next.(replModel)
	if !m.focused || cmd == nil {
		t.Error("regaining focus should resume previews")
	}
}

// The view asks the terminal for focus reports; without that the messages
// above never arrive.
func TestReplTTYViewRequestsFocusReports(t *testing.T) {
	m := newTestModel(t)
	if !m.View().ReportFocus {
		t.Error("the view does not request focus reporting")
	}
}

// shift+enter is what people try for "and keep going"; it forces a block like
// the other two spellings.
func TestReplTTYShiftEnterForcesABlock(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m.ti.SetValue("Cursed Energy: nums.txt")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})

	if len(m.core.pending) == 0 {
		t.Error("shift+enter did not force a block")
	}
}

// A terminal that reports keyboard enhancements can deliver ctrl+enter, so the
// key list stops advertising the fallback spelling.
func TestReplTTYKeyHelpFollowsTerminalSupport(t *testing.T) {
	m := newTestModel(t)
	if got := m.keys.ForceBlock.Help().Key; got != "alt+enter" {
		t.Errorf("default force-block help = %q, want the spelling that works everywhere", got)
	}
	next, _ := m.Update(tea.KeyboardEnhancementsMsg{})
	m = next.(replModel)
	if got := m.keys.ForceBlock.Help().Key; got != "ctrl+enter" {
		t.Errorf("after enhancements: force-block help = %q", got)
	}
}

// Output taller than the window opens a reader instead of scrolling the
// transcript away; short output still prints.
func TestReplTTYTallOutputOpensAPager(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // settle
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = next.(replModel)

	m = submit(t, m, "Cursed Energy: nums.txt")
	if m.pager != nil {
		t.Fatal("a one-line result should not open a pager")
	}

	m = submit(t, m, ":help") // taller than a ten-line window
	if m.pager == nil {
		t.Fatal(":help did not open a pager in a short window")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), ":stats") {
		t.Errorf("the pager is not showing the help:\n%s", ansi.Strip(m.View().Content))
	}
	if !m.View().AltScreen {
		t.Error("the pager should take the alternate screen, leaving the scrollback alone")
	}

	// While it is open, the pager owns the keyboard.
	m = press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.ti.Value() != "" {
		t.Errorf("a keystroke reached the prompt behind the pager: %q", m.ti.Value())
	}
	m = press(t, m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if m.pager != nil {
		t.Error("q did not close the pager")
	}
}

// A `:stats` profile can be read in program order or worst-first, without
// running the program again.
func TestReplTTYStatsPagerSortsInPlace(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("5\n3\n9\n1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 6})
	m = next.(replModel)
	for _, line := range []string{
		"Cursed Energy: nums.txt",
		"Cursed Technique: Extract Integers",
		"Domain Expansion: Sort",
		"Maximum Technique: Sum",
	} {
		m = submit(t, m, line)
	}
	m = submit(t, m, ":stats")

	if m.pager == nil {
		t.Fatal(":stats did not open a pager in a short window")
	}
	if m.pager.sortable == nil {
		t.Fatal("the profile is not re-orderable")
	}
	inOrder := ansi.Strip(m.pager.vp.GetContent())

	m = press(t, m, tea.KeyPressMsg{Code: 's', Text: "s"})
	sorted := ansi.Strip(m.pager.vp.GetContent())
	if sorted == inOrder {
		t.Error("s did not re-order the profile")
	}
	if !strings.Contains(m.View().Content, "slowest first") {
		t.Error("the pager does not say which order it is showing")
	}
	// Same measurements, re-ordered: every stage is still there.
	for _, stage := range []string{"Read Source", "Sum"} {
		if !strings.Contains(sorted, stage) {
			t.Errorf("sorting lost the %q stage:\n%s", stage, sorted)
		}
	}
}

func TestTooTallToPrint(t *testing.T) {
	content := strings.Repeat("line\n", 20)
	if tooTallToPrint(content, 0) {
		t.Error("a terminal that never reported its height cannot page")
	}
	if !tooTallToPrint(content, 10) {
		t.Error("twenty lines should page in a ten-line window")
	}
	if tooTallToPrint("one line", 10) {
		t.Error("one line should print")
	}
}

// Ctrl+R searches the history rather than walking it, and hands the match to
// the prompt rather than running it.
func TestReplTTYHistorySearch(t *testing.T) {
	m := newTestModel(t)
	for _, line := range []string{
		"Cursed Energy: day1.txt",
		`Cursed Technique: Split Text by "\n"`,
		"Maximum Technique: Sum",
		"Cursed Energy: day2.txt",
	} {
		m.hist.add(line)
	}

	m = press(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.search == nil {
		t.Fatal("ctrl+r did not open the search")
	}
	for _, r := range "day" {
		m = press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.search.current(); got != "Cursed Energy: day2.txt" {
		t.Errorf("first match = %q, want the most recent one", got)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "history search: day") {
		t.Errorf("the search is not on screen:\n%s", ansi.Strip(m.View().Content))
	}

	m = press(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}) // older
	if got := m.search.current(); got != "Cursed Energy: day1.txt" {
		t.Errorf("second match = %q, want the older one", got)
	}

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.search != nil {
		t.Error("enter did not close the search")
	}
	if got := m.ti.Value(); got != "Cursed Energy: day1.txt" {
		t.Errorf("the match was not put on the prompt: %q", got)
	}
	if len(m.core.stmts) != 0 {
		t.Error("the search ran the statement instead of offering it")
	}
}

func TestReplTTYHistorySearchCancels(t *testing.T) {
	m := newTestModel(t)
	m.hist.add("Maximum Technique: Sum")
	m.ti.SetValue("half typed")

	m = press(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search != nil {
		t.Fatal("esc did not close the search")
	}
	if got := m.ti.Value(); got != "half typed" {
		t.Errorf("cancelling the search changed the line: %q", got)
	}
}

func TestHistorySearchBackspaceAndMisses(t *testing.T) {
	h := &history{entries: []string{"Maximum Technique: Sum", "Cursed Energy: in.txt"}}
	s := newHistorySearch(h, "")
	for _, r := range "sumx" {
		s.setQuery(h, s.query+string(r))
	}
	if s.current() != "" {
		t.Errorf("a query that matches nothing showed %q", s.current())
	}
	if !strings.Contains(ansi.Strip(s.view(60)), "(no match)") {
		t.Error("a miss should say so")
	}
	s.setQuery(h, "sum")
	if got := s.current(); got != "Maximum Technique: Sum" {
		t.Errorf("case-insensitive match = %q", got)
	}
}

// A bare :load opens the browser; :load with a path does not.
func TestReplTTYBareLoadOpensThePicker(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("prog.domain", []byte("Cursed Energy: nums.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("sub", 0o755); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(t)
	m = submit(t, m, ":load")
	if m.picker == nil {
		t.Fatal("a bare :load did not open the browser")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "prog.domain") || !strings.Contains(view, "sub/") {
		t.Errorf("the browser is not showing the directory:\n%s", view)
	}

	// Walk to the program and load it.
	for m.picker != nil && m.picker.entries[m.picker.cursor].name != "prog.domain" {
		m = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.picker != nil {
		t.Fatal("choosing a file did not close the browser")
	}
	if len(m.core.stmts) != 1 {
		t.Errorf("the chosen program was not loaded: %#v", m.core.stmts)
	}
}

func TestReplTTYPickerCancelLeavesTheSessionAlone(t *testing.T) {
	t.Chdir(t.TempDir())
	m := newTestModel(t)
	m = submit(t, m, ":load")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.picker != nil {
		t.Fatal("esc did not close the browser")
	}
	if len(m.core.stmts) != 0 || m.status == "" {
		t.Error("cancelling should change nothing, and say so")
	}
}

// :save needs a name that does not exist yet, which is why the browser takes
// one rather than only offering what is already there.
func TestReplTTYPickerSaveTakesATypedName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, ":save")
	if m.picker == nil {
		t.Fatal("a bare :save did not open the browser")
	}
	for _, r := range "day7" {
		m = press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.picker.name; got != "day7" {
		t.Fatalf("typed name = %q", got)
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.picker != nil {
		t.Fatal("saving did not close the browser")
	}
	if _, err := os.Stat(filepath.Join(dir, "day7.domain")); err != nil {
		t.Errorf("the program was not saved under the typed name: %v", err)
	}
	if !strings.Contains(printed(m), "saved 1 statement(s)") {
		t.Errorf("the save was not reported:\n%s", printed(m))
	}
}

func TestPickerListsDirectoriesFirstAndHidesTheRest(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.domain", "a.domain", "notes.txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := newPicker(":load", dir)

	var names []string
	for _, e := range p.entries {
		names = append(names, e.name)
	}
	want := []string{"..", "inputs", "a.domain", "b.domain"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("entries = %v, want %v", names, want)
	}
}

// Bare :doc browses the catalog; typing filters it; Enter puts the statement
// on the prompt rather than running it.
func TestReplTTYDocBrowser(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = next.(replModel)
	m = submit(t, m, ":doc")

	if m.browser == nil {
		t.Fatal("bare :doc did not open the catalog")
	}
	for _, r := range "fold" {
		m = press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	doc, ok := m.browser.selected()
	if !ok || doc.ID != "Fold" {
		t.Fatalf("filtering for fold selected %#v", doc)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Fold") || !strings.Contains(view, doc.Signature) {
		t.Errorf("the browser is not showing the entry:\n%s", view)
	}

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.browser != nil {
		t.Fatal("enter did not close the browser")
	}
	if got := m.ti.Value(); !strings.HasPrefix(got, doc.Keyword+": Fold") {
		t.Errorf("the statement was not put on the prompt: %q", got)
	}
	if len(m.core.stmts) != 0 {
		t.Error("the browser ran the statement instead of offering it")
	}
}

func TestDocBrowserRanksNameMatchesFirst(t *testing.T) {
	b := newDocBrowser()
	if len(b.matches) != len(sortedCatalog()) {
		t.Errorf("an empty filter should show the whole catalog: %d", len(b.matches))
	}
	b.filter("sum")
	if len(b.matches) == 0 {
		t.Fatal("no matches for sum")
	}
	if got := b.matches[0].ID; !strings.EqualFold(got, "Sum") {
		t.Errorf("first match = %q, want the primitive actually called Sum", got)
	}
	b.filter("zzzz")
	if len(b.matches) != 0 {
		t.Errorf("a query matching nothing returned %d entries", len(b.matches))
	}
	if _, ok := b.selected(); ok {
		t.Error("nothing should be selected when nothing matches")
	}
}

// :visualize records the session's own program and opens the stepper over it,
// then hands the prompt back.
func TestReplTTYVisualizeOpensTheStepper(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("3\n1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(replModel)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, "Cursed Technique: Extract Integers")
	m = submit(t, m, "Maximum Technique: Sum")
	m = submit(t, m, ":visualize")

	if m.stepper == nil {
		t.Fatal(":visualize did not open the stepper")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Read Source") {
		t.Errorf("the stepper is not showing the recorded run:\n%s", view)
	}
	if !m.View().AltScreen {
		t.Error("the stepper should take the alternate screen")
	}

	// Its keys are its own while it is open.
	m = press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.ti.Value() != "" {
		t.Errorf("a keystroke reached the prompt behind the stepper: %q", m.ti.Value())
	}

	// q closes the overlay rather than the session.
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(replModel)
	if m.stepper != nil {
		t.Error("q did not close the stepper")
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("q inside the stepper quit the whole session")
		}
	}
	if len(m.core.stmts) != 3 {
		t.Error("the session was disturbed by visualizing it")
	}
}

// :watch replays the program when the file it is watching changes, and stops
// when told to.
func TestReplTTYWatchReplaysOnChange(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := watchInterval
	watchInterval = time.Millisecond
	t.Cleanup(func() { watchInterval = old })

	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, "Cursed Technique: Extract Integers")
	m = submit(t, m, "Maximum Technique: Sum")
	if !strings.Contains(printed(m), "=> 3 : Int") {
		t.Fatalf("the program did not run:\n%s", printed(m))
	}

	m = submit(t, m, ":watch nums.txt")
	if m.watch == nil {
		t.Fatal(":watch did not start watching")
	}
	if !strings.Contains(m.status, "watching") {
		t.Errorf("the watch was not announced: %q", m.status)
	}

	// A tick with the file untouched replays nothing.
	before := printed(m)
	next, cmd := m.Update(watchTickMsg{gen: m.watch.gen})
	m = run(t, next.(replModel), cmd)
	if printed(m) != before {
		t.Error("an unchanged file caused a replay")
	}

	// Change it, and the answer follows.
	if err := os.WriteFile("nums.txt", []byte("10\n20\n30"), 0o644); err != nil {
		t.Fatal(err)
	}
	next, cmd = m.Update(watchTickMsg{gen: m.watch.gen})
	m = run(t, next.(replModel), cmd)
	if !strings.Contains(printed(m), "=> 60 : Int") {
		t.Errorf("the change did not replay the program:\n%s", printed(m))
	}
	if len(m.core.stmts) != 3 {
		t.Errorf("the replay changed the program: %#v", m.core.stmts)
	}

	// A bare :watch stops, and a stale tick from the old watch is ignored.
	stale := m.watch.gen
	m = submit(t, m, ":watch")
	if m.watch != nil {
		t.Fatal("a bare :watch did not stop watching")
	}
	next, _ = m.Update(watchTickMsg{gen: stale})
	if next.(replModel).watch != nil {
		t.Error("a stale tick restarted the watch")
	}
}

func TestReplTTYWatchRejectsAMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	m := newTestModel(t)
	m = submit(t, m, ":watch nope.txt")
	if m.watch != nil {
		t.Error("a file that does not exist should not be watched")
	}
	if !strings.Contains(m.status, "cannot watch") {
		t.Errorf("the failure was not reported: %q", m.status)
	}
}

// A file that vanishes for an instant (an editor writing through a temporary
// file) is not a change to replay on.
func TestWatchIgnoresAMomentarilyMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(path, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &watch{path: path}
	w.modTime, w.size, _ = w.stat()

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if w.changed() {
		t.Error("a missing file was treated as a change")
	}
	if err := os.WriteFile(path, []byte("22"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !w.changed() {
		t.Error("the rewritten file was not noticed")
	}
}

// Ctrl+O edits the whole body of a pending statement at once, and submits it
// as one block.
func TestReplTTYBlockEditor(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	m = next.(replModel)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, "Cursed Technique: Extract Integers")
	m = submit(t, m, "Cursed Technique: Map Each") // opens a block

	if len(m.core.pending) == 0 {
		t.Fatal("no block to edit")
	}
	m = press(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.block == nil {
		t.Fatal("ctrl+o did not open the block editor")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Map Each") {
		t.Errorf("the editor does not show the statement it belongs to:\n%s", ansi.Strip(m.View().Content))
	}

	// Enter is a newline in here, not a submit.
	for _, r := range "Using: (x) -> x * 10" {
		m = press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.block == nil {
		t.Fatal("enter submitted the block instead of adding a line")
	}

	m = press(t, m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if m.block != nil {
		t.Fatal("ctrl+d did not submit the block")
	}
	if len(m.core.pending) != 0 {
		t.Errorf("the block is still open: %#v", m.core.pending)
	}
	if !strings.Contains(printed(m), "=> [10, 20] : List<Int>") {
		t.Errorf("the edited block did not run:\n%s", printed(m))
	}
}

func TestReplTTYBlockEditorCancels(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m = submit(t, m, "Cursed Energy: nums.txt")
	m = submit(t, m, "Cursed Technique: Extract Integers")
	m = submit(t, m, "Cursed Technique: Map Each")
	pending := append([]string(nil), m.core.pending...)

	m = press(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m = press(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.block != nil {
		t.Fatal("esc did not close the editor")
	}
	if strings.Join(m.core.pending, "|") != strings.Join(pending, "|") {
		t.Errorf("cancelling changed the block: %#v", m.core.pending)
	}
}

func TestReplTTYBlockEditorNeedsABlock(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.block != nil {
		t.Error("ctrl+o opened an editor with no block to edit")
	}
	if m.status == "" {
		t.Error("ctrl+o with no block should say why nothing happened")
	}
}

// Lines typed in the editor are indented into the block whether or not the
// typist indented them, and blank lines are dropped rather than closing it.
func TestBlockEditorNormalizesItsLines(t *testing.T) {
	b := newBlockEditor("Cursed Technique: Map Each", nil, "", 80, 24)
	b.ta.SetValue("Using: (x) -> x * 10\n\n        Mode: Strict\n\t Seed: 0  ")
	got := strings.Join(b.lines(), "|")
	want := "    Using: (x) -> x * 10|        Mode: Strict|     Seed: 0"
	if got != want {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

// Ghost text may only suggest what the completion would actually insert: the
// widget matches case-insensitively, and "simple Domain: " under a typed "s"
// is a spelling the parser rejects.
func TestReplTTYSuggestionsKeepTheTypedCase(t *testing.T) {
	m := newTestModel(t)
	m.ti.SetValue("s")
	m.ti.CursorEnd()
	m.refreshSuggestions()

	for _, s := range m.ti.AvailableSuggestions() {
		if !strings.HasPrefix(s, "s") {
			t.Errorf("suggestion %q does not continue the typed text exactly", s)
		}
	}

	m.ti.SetValue("S")
	m.ti.CursorEnd()
	m.refreshSuggestions()
	found := false
	for _, s := range m.ti.AvailableSuggestions() {
		if strings.HasPrefix(s, "Simple Domain") {
			found = true
		}
	}
	if !found {
		t.Error("the correctly-cased prefix lost its suggestion")
	}
}

// A line the session places is the line that was asked for; nothing is dimmed
// onto the end of it.
func TestReplTTYPlacedLinesHaveNoGhostText(t *testing.T) {
	m := newTestModel(t)
	m.hist.add("Maximum Technique: Sum")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})

	if got := m.ti.AvailableSuggestions(); len(got) != 0 {
		t.Errorf("a recalled line offered ghost text: %#v", got)
	}
}
