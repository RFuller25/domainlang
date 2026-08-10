// Getting something out of the stepper.
//
// Everything the visualizer knows was, until now, only ever on the screen. The
// data path existed — `--json` prints the whole recording, and the docs pitch
// it for CI — but it lived on the other side of quitting, re-running, and
// finding the row again. So the two paths never met: you explored in the UI,
// and if you wanted to keep anything you started over in a pipe.
//
// Three keys close that. `w` writes the recording as the same JSON document
// `--json` prints, and says where it went. `y` copies the selected value to the
// system clipboard, which is where a value goes when it is about to be pasted
// into a test. `o` opens the program at the selected step's line in $EDITOR,
// which is where a reader goes the moment they know what is wrong.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// writeRecording writes the recording beside the program as JSON, the same
// document `--json` prints. It is written rather than piped for the obvious
// reason: the UI owns stdout.
func (m *visualModel) writeRecording() {
	name := recordingFileName(m.view.path)
	f, err := os.Create(name)
	if err != nil {
		m.status = fmt.Sprintf("cannot write %s: %v", name, err)
		return
	}
	defer f.Close()
	// Expressions and the emitted Go are included: someone writing the
	// recording out wants what they were looking at, and the cost is paid once
	// at the moment they ask rather than on every recording.
	if err := m.view.writeJSON(f, true, true); err != nil {
		m.status = fmt.Sprintf("cannot write %s: %v", name, err)
		return
	}
	m.status = "wrote " + name
}

// recordingFileName is where `w` writes: beside the program, stamped, so
// pressing it twice keeps both and neither overwrites a source file.
func recordingFileName(program string) string {
	base := strings.TrimSuffix(filepath.Base(program), filepath.Ext(program))
	if base == "" {
		base = "recording"
	}
	return fmt.Sprintf("%s-%s.json", base, time.Now().Format("150405"))
}

// yankValue copies the selected row's value to the system clipboard — the full
// rendering where the recorder kept one, since the short form is the thing you
// would have retyped anyway.
func (m *visualModel) yankValue() tea.Cmd {
	node := m.selectedNode()
	if node == nil {
		m.status = "no row to copy"
		return nil
	}
	var v recordedValue
	switch {
	case node.Block != nil:
		v = recordedOf(node.Block)
	case node.IsFrame():
		m.status = "a frame has no value of its own"
		return nil
	default:
		v = stepValue(node.Step)
	}
	body := v.text()
	if body == "" {
		m.status = "nothing was captured for this row"
		return nil
	}
	m.status = fmt.Sprintf("copied %s to the clipboard", plural(len(body), "byte"))
	if !v.fullOK {
		m.status += " (the part that was captured)"
	}
	return tea.SetClipboard(body)
}

// openEditor opens the program at the selected step's line.
//
// The line is the one the value pane reports, which means an inlined stage —
// whose position belongs to another file — opens nothing rather than opening
// the user's program at a line number that means something there and nothing
// here. That is the same rule traceView.where follows, for the same reason.
func (m *visualModel) openEditor() tea.Cmd {
	s := m.selected()
	if s == nil {
		m.status = "a frame has no line of its own"
		return nil
	}
	if from, foreign := s.Node.Foreign(); foreign {
		m.status = "this stage was inlined from " + from
		return nil
	}
	line := s.Node.Pos.Line
	if line <= 0 {
		m.status = "this stage has no line in the program"
		return nil
	}
	if m.view.path == "" || m.view.path == "repl" {
		m.status = "this recording is of a session, not a file"
		return nil
	}

	editor := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	// A shell-less exec keeps a path with spaces working; the editor command
	// itself may still be `code -w`-style, so it is split on spaces — the same
	// handling the REPL's `:edit` uses.
	fields := strings.Fields(editor)
	name, args := fields[0], editorArgs(fields[0], fields[1:], m.view.path, line)
	cmd := exec.Command(name, args...) //nolint:gosec // the user's own $EDITOR
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err: err} })
}

// editorDoneMsg reports that $EDITOR exited. Nothing is reloaded — the
// recording is of the program as it was — but `r` re-records it, which is the
// natural next keystroke and what the status line says.
type editorDoneMsg struct{ err error }

// editorArgs builds the command line that opens a file at a line. The two
// spellings cover every editor anyone is likely to have set; one that
// understands neither simply opens the file, which is still most of the point.
func editorArgs(editor string, flags []string, path string, line int) []string {
	args := append([]string{}, flags...)
	switch filepath.Base(editor) {
	case "code", "code-insiders", "codium":
		return append(args, "--goto", fmt.Sprintf("%s:%d", path, line))
	case "vi", "vim", "nvim", "nano", "emacs", "emacsclient", "kak", "hx", "helix":
		return append(args, fmt.Sprintf("+%d", line), path)
	}
	return append(args, path)
}
