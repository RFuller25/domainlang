// `:load` and `:save` with no path — a file browser instead of guessing.
//
// Tab-completing a path works when you know what you are reaching for. When
// you do not, the REPL's answer used to be "cycle through the directory one
// blind candidate at a time". This shows the directory, walks into it, and is
// closed by the same Esc that closes every other overlay here.
//
// It only appears when the command was given no path: `:load day7.domain`
// still loads that file without ceremony, because someone who typed the name
// knows the name.
//
// This is hand-rolled rather than bubbles/filepicker for two reasons. The
// smaller one is a dependency: the bubble pulls in a humanizer for a file-size
// column this does not show. The larger one is `:save`, which needs a name
// that does not exist yet — a file *browser* cannot express "call it this",
// and picking only from what is already there is how a REPL overwrites your
// last session's program.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// picker is the :load/:save file browser.
type picker struct {
	command string // ":load", ":save" or ":save!"
	dir     string
	entries []pickerEntry
	cursor  int
	// name is the filename being typed, for :save. :load leaves it empty and
	// takes the highlighted entry instead.
	name string
	err  string
	keys pickerKeyMap
}

// pickerEntry is one row: a directory to walk into, or a program to pick.
type pickerEntry struct {
	name  string
	isDir bool
	up    bool // the ".." row
}

type pickerKeyMap struct {
	Up, Down, Enter, Parent, Cancel key.Binding
}

func defaultPickerKeys() pickerKeyMap {
	return pickerKeyMap{
		Up:     key.NewBinding(key.WithKeys("up", "ctrl+p")),
		Down:   key.NewBinding(key.WithKeys("down", "ctrl+n")),
		Enter:  key.NewBinding(key.WithKeys("enter")),
		Parent: key.NewBinding(key.WithKeys("left", "alt+up")),
		Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c")),
	}
}

// saving reports whether this picker is choosing a name to write to.
func (p *picker) saving() bool { return p.command == ":save" || p.command == ":save!" }

// newPicker opens a browser rooted at dir for the given command.
func newPicker(command, dir string) *picker {
	if dir == "" {
		dir = "."
	}
	p := &picker{command: command, keys: defaultPickerKeys()}
	p.setDir(dir)
	return p
}

// setDir reads a directory into the browser: sub-directories first, then the
// programs in it, with a way back up unless there is nowhere further to go.
func (p *picker) setDir(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	p.dir, p.cursor, p.err = abs, 0, ""

	entries, err := os.ReadDir(abs)
	if err != nil {
		p.entries = nil
		p.err = err.Error()
		return
	}
	p.entries = nil
	if parent := filepath.Dir(abs); parent != abs {
		p.entries = append(p.entries, pickerEntry{name: "..", isDir: true, up: true})
	}
	var files []pickerEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // a REPL is not a file manager; hidden stays hidden
		}
		if e.IsDir() {
			p.entries = append(p.entries, pickerEntry{name: e.Name(), isDir: true})
			continue
		}
		if strings.HasSuffix(e.Name(), ".domain") {
			files = append(files, pickerEntry{name: e.Name()})
		}
	}
	p.entries = append(p.entries, files...)
}

// update handles one keystroke. It reports whether the browser is still open
// and, when it is not, the path chosen ("" when cancelled).
func (p *picker) update(msg tea.KeyPressMsg) (open bool, path string) {
	switch {
	case key.Matches(msg, p.keys.Cancel):
		return false, ""

	case key.Matches(msg, p.keys.Up):
		if p.cursor > 0 {
			p.cursor--
		}
		return true, ""

	case key.Matches(msg, p.keys.Down):
		if p.cursor < len(p.entries)-1 {
			p.cursor++
		}
		return true, ""

	case key.Matches(msg, p.keys.Parent):
		p.setDir(filepath.Dir(p.dir))
		return true, ""

	case key.Matches(msg, p.keys.Enter):
		return p.enter()

	case p.saving() && msg.String() == "backspace":
		if p.name != "" {
			p.name = p.name[:len(p.name)-1]
		}
		return true, ""
	}

	// Anything printable is the name being typed, but only where a name is
	// what is being asked for.
	if p.saving() && msg.Text != "" {
		p.name += msg.Text
	}
	return true, ""
}

// enter walks into a directory, or settles on a file.
func (p *picker) enter() (open bool, path string) {
	if p.saving() && p.name != "" {
		return false, filepath.Join(p.dir, withDomainExt(p.name))
	}
	if p.cursor >= len(p.entries) {
		return true, ""
	}
	e := p.entries[p.cursor]
	switch {
	case e.up:
		p.setDir(filepath.Dir(p.dir))
		return true, ""
	case e.isDir:
		p.setDir(filepath.Join(p.dir, e.name))
		return true, ""
	case p.saving():
		// Picking an existing file when saving means "this name", typed for
		// you — it still has to be confirmed with Enter, and :save's own
		// overwrite guard still applies.
		p.name = e.name
		return true, ""
	}
	return false, filepath.Join(p.dir, e.name)
}

// withDomainExt supplies the extension when the typed name has none, so
// `:save` into a browser produces the same kind of file `:save foo` does.
func withDomainExt(name string) string {
	if filepath.Ext(name) == "" {
		return name + ".domain"
	}
	return name
}

// view draws the browser: where it is, what is there, and what the keys do.
func (p *picker) view(width, height int) string {
	var b strings.Builder
	what := "load a program"
	if p.saving() {
		what = "save the program as"
	}
	b.WriteString(styTitle.Render("domain "+p.command) + styDim.Render("  "+what) + "\n")
	b.WriteString(styDim.Render(truncateVis(p.dir, max(width, 20))) + "\n")

	switch {
	case p.err != "":
		b.WriteString(styErr.Render(p.err) + "\n")
	case len(p.entries) == 0:
		b.WriteString(styDim.Render("(no .domain files here)") + "\n")
	}

	// Keep the cursor on screen in a directory taller than the window.
	rows := max(height-6, 3)
	start := max(min(p.cursor-rows/2, len(p.entries)-rows), 0)
	end := min(start+rows, len(p.entries))
	for i := start; i < end; i++ {
		e := p.entries[i]
		label := e.name
		if e.isDir {
			label += "/"
		}
		line := "  " + label
		if i == p.cursor {
			line = styCursor.Render("› " + label)
		} else if e.isDir {
			line = "  " + styFrame.Render(label)
		}
		b.WriteString(truncateVis(line, max(width, 20)) + "\n")
	}

	if p.saving() {
		name := p.name
		if name == "" {
			name = styDim.Render("(type a name, or pick a file to reuse its name)")
		}
		b.WriteString("\n" + styHeading.Render("name: ") + name + "\n")
	}
	b.WriteString(styDim.Render(p.hints()))
	return b.String()
}

func (p *picker) hints() string {
	if p.saving() {
		return "↑/↓ move · → / enter open · ← up · type a name · enter save · esc cancel"
	}
	return "↑/↓ move · enter open or load · ← up · esc cancel"
}

// wantsPicker reports whether a command should open the browser: it is one of
// the file commands, and no path was given.
func wantsPicker(line string) (command string, ok bool) {
	name, arg := splitCommand(strings.TrimSpace(line))
	if arg != "" {
		return "", false
	}
	switch name {
	case ":load", ":save", ":save!":
		return name, true
	}
	return "", false
}

// pickedCommand is the line the browser stands in for, once it has chosen.
func pickedCommand(command, path string) string {
	return fmt.Sprintf("%s %s", command, path)
}
