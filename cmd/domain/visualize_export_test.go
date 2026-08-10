package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Where the flags open the stepper
// ---------------------------------------------------------------------------

// --go and --expressions used to switch the UI off. Asking to see the emitted
// Go is not asking to stop using the debugger.
func TestOpenAsPutsTheFlagsWhereTheyBelong(t *testing.T) {
	cases := []struct {
		name  string
		opts  visualizeOptions
		check func(t *testing.T, m *visualModel)
	}{
		{"go opens the code screen", visualizeOptions{Go: true}, func(t *testing.T, m *visualModel) {
			if m.screen != screenGo {
				t.Errorf("screen = %v, want the code screen", m.screen)
			}
		}},
		{"expressions opens the pane", visualizeOptions{Exprs: true}, func(t *testing.T, m *visualModel) {
			if m.pane != paneExpr {
				t.Errorf("pane = %v, want the expression pane", m.pane)
			}
		}},
		{"expand-loops opens the frames", visualizeOptions{Expand: true}, func(t *testing.T, m *visualModel) {
			if len(m.expanded) == 0 {
				t.Error("--expand-loops should open every frame")
			}
			for _, n := range m.flat {
				if len(n.Children) > 0 && !m.expanded[n] {
					t.Errorf("frame %q is still closed", n.Label())
				}
			}
		}},
		{"plain opens on the tree", visualizeOptions{}, func(t *testing.T, m *visualModel) {
			if m.screen != screenTree || m.pane != paneValue {
				t.Error("with no flags the stepper opens on the tree and the value pane")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := visModel(t)
			openAs(m, recordSpec{path: m.view.path}, c.opts)
			c.check(t, m)
		})
	}
}

func TestOpenAsStartsTheWatch(t *testing.T) {
	m := visModel(t)
	openAs(m, recordSpec{path: m.view.path}, visualizeOptions{Watch: true, Input: ""})
	if m.watch == nil {
		t.Fatal("--watch should start a watch")
	}
	if len(m.watch.files) != 1 {
		t.Errorf("watching %d files, want just the program", len(m.watch.files))
	}
	if !strings.Contains(watchStatus(m.watch), "re-records") {
		t.Errorf("the watch status should say what it does: %q", watchStatus(m.watch))
	}
}

// ---------------------------------------------------------------------------
// Getting something out
// ---------------------------------------------------------------------------

// w writes the same document --json prints, so the UI and the scripting path
// finally meet.
func TestWriteRecording(t *testing.T) {
	m, prog := visRecordModel(t, visProgram, "1,2,3")
	dir := filepath.Dir(prog)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	m.writeRecording()
	if !strings.HasPrefix(m.status, "wrote ") {
		t.Fatalf("status = %q, want it to name the file", m.status)
	}
	name := strings.TrimPrefix(m.status, "wrote ")
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("what was written is not JSON: %v", err)
	}
	for _, key := range []string{"program", "steps", "rows"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("the document has no %q", key)
		}
	}
}

// The name is stamped so pressing w twice keeps both, and can never land on a
// source file.
func TestRecordingFileName(t *testing.T) {
	got := recordingFileName("/tmp/day1.domain")
	if !strings.HasPrefix(got, "day1-") || !strings.HasSuffix(got, ".json") {
		t.Errorf("name = %q, want a stamped day1 JSON file", got)
	}
	if strings.Contains(got, "/") {
		t.Errorf("name = %q, want it relative to the working directory", got)
	}
}

// y copies the value the recorder kept, and refuses politely where there is
// nothing to copy.
func TestYankValue(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	cmd := m.yankValue()
	if cmd == nil {
		t.Fatal("a step row should have a value to copy")
	}
	if !strings.Contains(m.status, "copied") {
		t.Errorf("status = %q, want it to confirm the copy", m.status)
	}
	// The command carries the value to the terminal.
	if msg := cmd(); msg == nil {
		t.Error("the clipboard command produced nothing")
	}
}

// A frame does hold one value — what its body came to — and that is the one
// worth copying. A frame with no body result has nothing, and says so.
func TestYankValueOnAFrame(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30},
		pressKey("j"), pressKey("j"), pressKey("j"), pressKey("l"), pressKey("j"))
	node := m.selectedNode()
	if !node.IsFrame() {
		t.Skip("no frame row reachable in this recording")
	}
	cmd := m.yankValue()
	if node.Block != nil {
		if cmd == nil {
			t.Error("a frame's body result is a value worth copying")
		}
		if !strings.Contains(m.status, "copied") {
			t.Errorf("status = %q, want it to confirm the copy", m.status)
		}
		return
	}
	if cmd != nil {
		t.Error("a frame with no body result has nothing to copy")
	}
	if !strings.Contains(m.status, "no value") {
		t.Errorf("status = %q, want it to explain", m.status)
	}
}

// ---------------------------------------------------------------------------
// $EDITOR
// ---------------------------------------------------------------------------

func TestEditorArgs(t *testing.T) {
	cases := []struct {
		editor string
		flags  []string
		want   []string
	}{
		{"vim", nil, []string{"+12", "p.domain"}},
		{"nvim", nil, []string{"+12", "p.domain"}},
		{"/usr/bin/vi", nil, []string{"+12", "p.domain"}},
		{"code", []string{"-w"}, []string{"-w", "--goto", "p.domain:12"}},
		{"something-else", nil, []string{"p.domain"}},
	}
	for _, c := range cases {
		t.Run(c.editor, func(t *testing.T) {
			got := editorArgs(c.editor, c.flags, "p.domain", 12)
			if len(got) != len(c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("arg %d = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// A stage inlined from another file has no line in the program on screen, and
// opening the editor at that number would point confidently at the wrong place.
func TestOpenEditorRefusesWhatItCannotPointAt(t *testing.T) {
	m := visModel(t)
	m.view.path = "repl"
	if cmd := m.openEditor(); cmd != nil {
		t.Error("a session recording has no file to open")
	}
	if !strings.Contains(m.status, "not a file") {
		t.Errorf("status = %q, want it to explain", m.status)
	}
}

// ---------------------------------------------------------------------------
// The code screen's own search
// ---------------------------------------------------------------------------

// `/` on the code screen searches the code, not the tree behind it.
func TestGoScreenSearch(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30}, pressKey("c"))
	if m.screen != screenGo {
		t.Skip("this program does not compile to Go")
	}
	src := m.goSrc()
	if len(src) == 0 {
		t.Skip("nothing was emitted")
	}
	m = send(m, pressKey("/"))
	if !m.searchingGo {
		t.Fatal("/ should start a search on the code screen")
	}
	for _, r := range "func" {
		m = send(m, pressKey(string(r)))
	}
	// The tree's filter was left alone: the two searches share nothing.
	if m.filter != "" {
		t.Errorf("the tree filter is %q; the code search should not touch it", m.filter)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.searchingGo {
		t.Error("enter should accept the search")
	}
	// The status names the line it found; the scroll itself stops at the end of
	// the file, so a match near the bottom is on screen without being at the top.
	if !strings.Contains(m.status, "matches") {
		t.Fatalf("status = %q, want it to name the matching line", m.status)
	}
	var line int
	if _, err := fmt.Sscanf(m.status, "line %d", &line); err != nil {
		t.Fatalf("cannot read the line out of %q: %v", m.status, err)
	}
	if !strings.Contains(strings.ToLower(src[line-1]), "func") {
		t.Errorf("line %d is %q, want a match", line, src[line-1])
	}
}

func TestGoScreenSearchWithNoMatch(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30}, pressKey("c"))
	if m.screen != screenGo || len(m.goSrc()) == 0 {
		t.Skip("this program does not compile to Go")
	}
	m.goFind = "zzzzz-not-in-any-program"
	m.findInGo(1)
	if !strings.Contains(m.status, "no line matches") {
		t.Errorf("status = %q, want it to say nothing matched", m.status)
	}
}

// ---------------------------------------------------------------------------
// The REPL overlay
// ---------------------------------------------------------------------------

// The overlay has to tell "the reader pressed q" from "the reader started
// something". It used to find out by *running* the command, which would now run
// an editor or a whole re-recording to answer the question.
func TestStepperQuitDoesNotRunTheCommand(t *testing.T) {
	m := visModel(t)
	ran := false
	cmd := tea.Cmd(func() tea.Msg {
		ran = true
		return nil
	})
	quit, pass := stepperQuit(m, cmd)
	if quit {
		t.Error("a model that is not quitting should not report a quit")
	}
	if ran {
		t.Error("stepperQuit ran the command to inspect it")
	}
	if pass == nil {
		t.Error("the command should be passed along")
	}

	m.quitting = true
	quit, pass = stepperQuit(m, cmd)
	if !quit {
		t.Error("a quitting model should report a quit")
	}
	if pass != nil {
		t.Error("a quit should pass nothing along")
	}
	if ran {
		t.Error("stepperQuit ran the command")
	}
}

func TestQuitKeysSetTheFlag(t *testing.T) {
	for _, key := range []string{"q", "esc"} {
		m := visModel(t)
		m = send(m, pressKey(key))
		if !m.quitting {
			t.Errorf("%q should record that the stepper is quitting", key)
		}
	}
	// esc backs out of a pane first, and only quits from the top.
	m := visModel(t)
	m = send(m, pressKey("t"), tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.quitting {
		t.Error("esc should close the pane before it leaves")
	}
}
