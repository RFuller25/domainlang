package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const untidy = `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Enumerate
Maximum Technique: Fold
    Seed: 0
    Using: (acc, p) -> consider i as item(p, 0) in consider x as item(p, 1) in acc + i * x
Reveal:   stdout`

const tidied = `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Enumerate
Maximum Technique: Fold
    Seed: 0
    Using: (acc, (i, x)) -> acc + i * x
Reveal: stdout`

// ctrl+s tidies and saves: what is written is what is on screen.
func TestDevSaveTidiesTheProgram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.domain")
	m, _ := newClockedModel(untidy)
	m.path, m.dirty = path, true
	m = devKey(m, "ctrl+s")

	if got := m.buf.text(); got != tidied {
		t.Fatalf("buffer is\n%s", got)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != tidied+"\n" {
		t.Errorf("file is\n%s", b)
	}
	if m.dirty {
		t.Error("still dirty after saving")
	}
	if !strings.Contains(m.status, "destructured") || !strings.Contains(m.status, "saved p.domain") {
		t.Errorf("status is %q", m.status)
	}
}

// Undoing the tidy is how it is declined: the next save writes the program as
// it was typed.
func TestDevSaveAfterUndoSavesAsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.domain")
	m, _ := newClockedModel(untidy)
	m.path, m.dirty = path, true
	m = devKey(m, "ctrl+s")
	m = devKey(m, "ctrl+z")
	if m.buf.text() != untidy {
		t.Fatalf("undo gave\n%s", m.buf.text())
	}
	if !m.dirty {
		t.Error("undoing a saved tidy should leave unsaved changes")
	}
	m = devKey(m, "ctrl+s")
	if m.buf.text() != untidy {
		t.Errorf("the declined tidy was applied again:\n%s", m.buf.text())
	}
	if b, _ := os.ReadFile(path); string(b) != untidy+"\n" {
		t.Errorf("file is\n%s", b)
	}
	// Asking by name is a new request.
	m = devKey(m, "alt+o")
	if m.buf.text() == untidy {
		t.Error("alt+o did nothing after a declined save")
	}
}

// A program that does not check is saved exactly as typed.
func TestDevSaveOfABrokenProgramJustSaves(t *testing.T) {
	const broken = "Cursed Energy: input.txt\nCursed Tecnique: Sum   \nReveal stdout"
	path := filepath.Join(t.TempDir(), "p.domain")
	m, _ := newClockedModel(broken)
	m.path, m.dirty = path, true
	m = devKey(m, "ctrl+s")
	if m.buf.text() != broken {
		t.Errorf("buffer changed:\n%s", m.buf.text())
	}
	if b, _ := os.ReadFile(path); string(b) != broken+"\n" {
		t.Errorf("file is %q", b)
	}
	if m.dirty || m.status != "saved p.domain" {
		t.Errorf("dirty=%v status=%q", m.dirty, m.status)
	}
}

// alt+o shows the rewrites without writing the file.
func TestDevTidyNowDoesNotSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.domain")
	m, _ := newClockedModel(untidy)
	m.path = path
	m = devKey(m, "alt+o")
	if !strings.Contains(m.buf.text(), "(acc, (i, x))") {
		t.Fatalf("not tidied:\n%s", m.buf.text())
	}
	if !m.dirty {
		t.Error("a tidy is a change")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("alt+o wrote the file")
	}
	m = devKey(m, "ctrl+z")
	if m.buf.text() != untidy {
		t.Error("one undo should withdraw the whole tidy")
	}
}

// alt+v runs the parse and offers what holds of it; nothing is inserted
// without being chosen.
func TestDevVowsAreOfferedAndInserted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("3\n1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const src = "Cursed Energy: input.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout"
	m, _ := newClockedModel(src)
	m.path = filepath.Join(dir, "p.domain")
	m.buf.row = 1

	next, cmd := m.Update(devKeyMsg("alt+v"))
	m = next.(devModel)
	if cmd == nil {
		t.Fatalf("no run started; status %q", m.status)
	}
	m = devSend(m, cmd())
	if m.vows == nil {
		t.Fatalf("no overlay; status %q", m.status)
	}
	if m.buf.text() != src {
		t.Fatal("the offer changed the program")
	}
	view := ansi.Strip(m.view())
	if !strings.Contains(view, "Count Equals 3") || !strings.Contains(view, "All Values > 0") {
		t.Errorf("overlay is\n%s", view)
	}

	m = devKey(m, "enter")
	want := "Cursed Energy: input.txt\nShikigami: Ints\nBinding Vow: Count Equals 3\nMaximum Technique: Sum\nReveal: stdout"
	if m.buf.text() != want {
		t.Errorf("buffer is\n%s", m.buf.text())
	}
	if m.vows != nil || !m.dirty {
		t.Errorf("overlay=%v dirty=%v", m.vows != nil, m.dirty)
	}
}

func TestDevVowsEscLeavesTheProgramAlone(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("3\n1\n2\n"), 0o644)
	const src = "Cursed Energy: input.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout"
	m, _ := newClockedModel(src)
	m.path = filepath.Join(dir, "p.domain")
	next, cmd := m.Update(devKeyMsg("alt+v"))
	m = devSend(next.(devModel), cmd())
	m = devKey(m, "esc")
	if m.vows != nil || m.buf.text() != src || m.dirty {
		t.Error("esc did not leave the program alone")
	}
}

// With no input to run on there is nothing to observe, and it says so.
func TestDevVowsExplainAMissingInput(t *testing.T) {
	const src = "Cursed Energy: nowhere.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout"
	m, _ := newClockedModel(src)
	m.path = filepath.Join(t.TempDir(), "p.domain")
	next, cmd := m.Update(devKeyMsg("alt+v"))
	m = devSend(next.(devModel), cmd())
	if m.vows != nil || !strings.Contains(m.status, "no vows") {
		t.Errorf("status is %q", m.status)
	}
}
