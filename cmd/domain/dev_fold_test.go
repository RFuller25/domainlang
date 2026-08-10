package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const foldProgram = "Cursed Energy: in.txt\n" +
	"Shikigami: Ints\n" +
	"Channel \"top\":\n" +
	"    Domain Expansion: Quicksort, Descending\n" +
	"    Maximum Technique: Select Top 3\n" +
	"\n" +
	"    Maximum Technique: Sum\n" +
	"Maximum Technique: Count\n" +
	"Reveal: stdout"

// ---------------------------------------------------------------------------
// folding
// ---------------------------------------------------------------------------

func TestDevFoldHidesABlocksBody(t *testing.T) {
	m := settle(t, newTestDevModel(foldProgram))
	if len(m.blocks) == 0 {
		t.Fatalf("no blocks found in:\n%s", foldProgram)
	}

	m.buf.gotoLine(3) // the Channel header
	m, ok := m.toggleFold()
	if !ok {
		t.Fatal("the Channel header did not fold")
	}

	painted := ansi.Strip(m.view())
	if strings.Contains(painted, "Quicksort") {
		t.Errorf("the body is still drawn:\n%s", painted)
	}
	if !strings.Contains(painted, "Channel") {
		t.Error("the header disappeared with its body")
	}
	if !strings.Contains(painted, "folded") {
		t.Errorf("no marker stands for the hidden body:\n%s", painted)
	}
	// Lines outside the block are untouched.
	if !strings.Contains(painted, "Maximum Technique: Count") {
		t.Error("a line after the block was hidden too")
	}
}

// A blank line inside a body looks like the end of it and is not. This is why
// the extent comes from the parse rather than from the indentation.
func TestDevFoldSpansABlankLineInsideTheBody(t *testing.T) {
	m := settle(t, newTestDevModel(foldProgram))
	b, ok := m.blocks[3]
	if !ok {
		t.Fatal("the Channel was not recognised as a block")
	}
	if b.Last < 7 {
		t.Errorf("the block ends at line %d, want 7 — the blank line cut it short", b.Last)
	}
}

// Folding from inside the body folds the body you are in, and brings the cursor
// out to the header — a cursor on a line that is not drawn is one nobody can
// see.
func TestDevFoldFromInsideBringsTheCursorOut(t *testing.T) {
	m := settle(t, newTestDevModel(foldProgram))
	m.buf.gotoLine(5)
	m, ok := m.toggleFold()
	if !ok {
		t.Fatal("folding from inside the body did nothing")
	}
	if m.buf.row+1 != 3 {
		t.Errorf("cursor is on line %d, want the header at 3", m.buf.row+1)
	}
	if m.hidden(m.buf.row + 1) {
		t.Error("the cursor is inside the fold it just made")
	}
}

// Vertical motion steps over a fold rather than into it.
func TestDevMotionSkipsAFoldedBlock(t *testing.T) {
	m := settle(t, newTestDevModel(foldProgram))
	m.buf.gotoLine(3)
	m, _ = m.toggleFold()

	m.buf.gotoLine(2)
	m = devKey(m, "down") // onto the header
	m = devKey(m, "down") // over the whole body
	if m.hidden(m.buf.row + 1) {
		t.Errorf("the cursor landed inside the fold, on line %d", m.buf.row+1)
	}
	if m.buf.row+1 <= 7 {
		t.Errorf("cursor on line %d, want past the folded body", m.buf.row+1)
	}
}

func TestDevUnfoldRestoresTheBody(t *testing.T) {
	m := settle(t, newTestDevModel(foldProgram))
	m.buf.gotoLine(3)
	m, _ = m.toggleFold()
	m, _ = m.toggleFold()
	if strings.Contains(ansi.Strip(m.view()), "folded") {
		t.Error("the block is still folded")
	}
	if !strings.Contains(ansi.Strip(m.view()), "Quicksort") {
		t.Error("the body did not come back")
	}
}

func TestDevUnfoldAllOpensEverything(t *testing.T) {
	m := settle(t, newTestDevModel(foldProgram))
	m.buf.gotoLine(3)
	m, _ = m.toggleFold()
	m = devKey(m, "alt+shift+z")
	if len(m.folded) != 0 {
		t.Errorf("%d blocks still folded", len(m.folded))
	}
}

// Folding never changes the program, only what is drawn.
func TestDevFoldingDoesNotEditTheBuffer(t *testing.T) {
	m := settle(t, newTestDevModel(foldProgram))
	m.buf.gotoLine(3)
	m, _ = m.toggleFold()
	if m.buf.text() != foldProgram {
		t.Errorf("folding changed the program:\n%s", m.buf.text())
	}
	if m.dirty {
		t.Error("folding marked the buffer dirty")
	}
}

// A line with no block around it says so rather than silently doing nothing.
func TestDevFoldExplainsWhenThereIsNoBlock(t *testing.T) {
	m := settle(t, newTestDevModel("Cursed Energy: in.txt\nReveal: stdout"))
	m = devKey(m, "alt+z")
	if !strings.Contains(m.status, "no block") {
		t.Errorf("status is %q", m.status)
	}
}

// The frame is still exactly as tall with a fold in it.
func TestDevFoldKeepsTheFrameHeight(t *testing.T) {
	m := settle(t, newTestDevModel(foldProgram))
	before := strings.Count(m.view(), "\n")
	m.buf.gotoLine(3)
	m, _ = m.toggleFold()
	if after := strings.Count(m.view(), "\n"); after != before {
		t.Errorf("the frame changed height when a block folded: %d then %d", before, after)
	}
}

// ---------------------------------------------------------------------------
// auto-indent
// ---------------------------------------------------------------------------

// The four structural keywords always take a body, so Enter after one indents.
func TestDevEnterIndentsAfterABlockOpener(t *testing.T) {
	for _, opener := range []string{
		`Channel "top":`,
		`Simple Domain: Repeat 3`,
		`Part "1":`,
		`Shikigami "Double"`,
	} {
		m, _ := newClockedModel(opener)
		m = devKey(m, "end")
		m = devKey(m, "enter")
		if got := m.buf.line(); got != devIndent {
			t.Errorf("%q: new line is %q, want one indent", opener, got)
		}
	}
}

// `Shikigami "X"` defines and takes a body; `Shikigami: X` calls and does not.
// One character apart, opposite answers.
func TestDevShikigamiCallDoesNotIndent(t *testing.T) {
	m, _ := newClockedModel("Shikigami: Ints")
	m = devKey(m, "end")
	m = devKey(m, "enter")
	if got := m.buf.line(); got != "" {
		t.Errorf("a Shikigami *call* indented: %q", got)
	}
}

// An ordinary stage does not, and neither does auto-indent try to guess which
// operations want a `Using:` — nothing in the vocabulary declares that.
func TestDevEnterDoesNotGuessAtArgumentLines(t *testing.T) {
	m, _ := newClockedModel("Cursed Technique: Map Each")
	m = devKey(m, "end")
	m = devKey(m, "enter")
	if got := m.buf.line(); got != "" {
		t.Errorf("new line is %q, want no indent", got)
	}
}

// Indentation already present is still carried, and a block opener adds to it.
func TestDevAutoIndentBuildsOnExistingIndentation(t *testing.T) {
	if got := autoIndentFor(`    Channel "inner":`); got != devIndent+devIndent {
		t.Errorf("got %q, want two levels", got)
	}
	if got := autoIndentFor("    Maximum Technique: Sum"); got != devIndent {
		t.Errorf("got %q, want one level carried", got)
	}
}
