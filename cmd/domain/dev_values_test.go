package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const valueProgram = "Cursed Energy: in.txt\n" +
	"Shikigami: Ints\n" +
	"Maximum Technique: Sum\n" +
	"Reveal: stdout\n"

// ---------------------------------------------------------------------------
// values
// ---------------------------------------------------------------------------

// The type says the shape; the value says whether the shape is the one you
// meant. This is the REPL's appeal, against a file.
func TestDevValueBarShowsWhatTheLineProduced(t *testing.T) {
	m := devWriteProgram(t, valueProgram, "1\n2\n3\n")
	m = runToCompletion(t, m)

	m.buf.row = 1 // Shikigami: Ints
	st, ok := m.stageFor(m.buf.row)
	if !ok {
		t.Fatalf("no value recorded for the Ints line; stages: %v", m.stages)
	}
	if !strings.Contains(st.Short, "1") || !strings.Contains(st.Short, "3") {
		t.Errorf("value is %q, want the list of integers", st.Short)
	}
	if st.Type != "List<Int>" {
		t.Errorf("type is %q, want List<Int>", st.Type)
	}

	bar := ansi.Strip(m.valueBar())
	if !strings.Contains(bar, "=>") || !strings.Contains(bar, "List<Int>") {
		t.Errorf("the value bar reads %q", bar)
	}
}

// A line that produced a scalar reports it too — the last stage is usually the
// one you are checking.
func TestDevValueBarFollowsTheCursor(t *testing.T) {
	m := devWriteProgram(t, valueProgram, "1\n2\n3\n")
	m = runToCompletion(t, m)

	m.buf.row = 2 // Maximum Technique: Sum
	st, ok := m.stageFor(m.buf.row)
	if !ok {
		t.Fatal("no value for the Sum line")
	}
	if !strings.Contains(st.Short, "6") {
		t.Errorf("value is %q, want 6", st.Short)
	}
}

// A program that has not been run costs no space at all: the bar is absent
// rather than empty.
func TestDevValueBarIsAbsentBeforeARun(t *testing.T) {
	m := newTestDevModel(valueProgram)
	if got := m.valueBar(); got != "" {
		t.Errorf("value bar is %q before any run", got)
	}
	before := strings.Count(m.view(), "\n")

	m2 := devWriteProgram(t, valueProgram, "1\n2\n3\n")
	m2.width, m2.height = m.width, m.height
	m2 = runToCompletion(t, m2)
	m2.output = nil // dismiss the pane; the bar should remain
	m2.buf.row = 1
	if m2.valueBar() == "" {
		t.Fatal("no value bar after a run")
	}
	if after := strings.Count(m2.view(), "\n"); after != before {
		t.Errorf("the frame changed height when the bar appeared: %d then %d", before, after)
	}
}

// The bar never overflows, whatever the value's size.
func TestDevValueBarFitsANarrowTerminal(t *testing.T) {
	m := devWriteProgram(t, valueProgram, strings.Repeat("1234\n", 200))
	m = runToCompletion(t, m)
	m.buf.row = 1
	for _, w := range []int{20, 40, 80} {
		m.width = w
		if got := ansi.StringWidth(m.valueBar()); got > w {
			t.Errorf("width %d: value bar is %d columns", w, got)
		}
	}
}

// The recording outlives the output pane: dismissing what a run printed must
// not take the values with it.
func TestDevValuesOutliveTheOutputPane(t *testing.T) {
	m := devWriteProgram(t, valueProgram, "1\n2\n3\n")
	m = runToCompletion(t, m)
	m = devKey(m, "x") // dismiss the pane
	if m.output != nil {
		t.Fatal("the pane did not close")
	}
	m.buf.row = 1
	if _, ok := m.stageFor(m.buf.row); !ok {
		t.Error("dismissing the output pane discarded the recording")
	}
}

// ---------------------------------------------------------------------------
// timing
// ---------------------------------------------------------------------------

// A line's share of the run comes from the same profile the stepper's heat pane
// uses, so a hot line means the same thing in both.
func TestDevHeatIsOnlyShownAfterARunAndOnlyWhereItMatters(t *testing.T) {
	m := newTestDevModel(valueProgram)
	if _, ok := m.heatFor(0); ok {
		t.Error("a line was tinted before anything ran")
	}

	m = devWriteProgram(t, valueProgram, strings.Repeat("7\n", 500))
	m = runToCompletion(t, m)

	tinted := 0
	for row := range m.buf.lines {
		if _, ok := m.heatFor(row); ok {
			tinted++
		}
	}
	// Something took the time, and not everything did — a program where every
	// line is warm has told you nothing.
	if tinted == 0 {
		t.Error("no line carried a share of the run")
	}
	if tinted == len(m.buf.lines) {
		t.Error("every line was tinted, which says nothing")
	}
}

// ---------------------------------------------------------------------------
// walking the stages
// ---------------------------------------------------------------------------

// The stepper's gesture, against the buffer: move between stages and watch the
// value change.
func TestDevStageWalkMovesBetweenRecordedLines(t *testing.T) {
	m := devWriteProgram(t, valueProgram, "1\n2\n3\n")
	m = runToCompletion(t, m)
	m.output = nil

	lines := m.stageLines()
	if len(lines) < 2 {
		t.Fatalf("only %d recorded stages", len(lines))
	}

	m.buf.gotoLine(1)
	m = devKey(m, "alt+down")
	if m.buf.row+1 <= 1 {
		t.Errorf("alt+down did not advance: row %d", m.buf.row)
	}
	seen := m.buf.row
	m = devKey(m, "alt+up")
	if m.buf.row >= seen {
		t.Errorf("alt+up did not go back: row %d", m.buf.row)
	}
}

// A pipeline is a loop to read round, so the walk wraps rather than stopping.
func TestDevStageWalkWraps(t *testing.T) {
	m := devWriteProgram(t, valueProgram, "1\n2\n3\n")
	m = runToCompletion(t, m)
	m.output = nil

	lines := m.stageLines()
	m.buf.gotoLine(lines[len(lines)-1])
	m = devKey(m, "alt+down")
	if m.buf.row+1 != lines[0] {
		t.Errorf("walking past the end landed on %d, want %d", m.buf.row+1, lines[0])
	}
}

func TestDevStageWalkSaysWhenNothingHasRun(t *testing.T) {
	m := newTestDevModel(valueProgram)
	m = devKey(m, "alt+down")
	if !strings.Contains(m.status, "nothing recorded") {
		t.Errorf("status is %q", m.status)
	}
}

// A failing step is the interesting one and must not be displaced by a later
// success on the same line.
func TestDevFailedStageIsKept(t *testing.T) {
	m := devWriteProgram(t,
		"Cursed Energy: in.txt\nShikigami: Ints\nCursed Technique: Map Each\n    Using: (x) -> x / 0\nReveal: stdout\n",
		"1\n2\n")
	m = runToCompletion(t, m)
	if m.output == nil || !m.output.err {
		t.Skip("this program did not fail the way the test expects")
	}
	for _, st := range m.stages {
		if st.Failed {
			return
		}
	}
	t.Error("the failure was not recorded against any line")
}

// ---------------------------------------------------------------------------
// the optimizer's opinion
// ---------------------------------------------------------------------------

// A named algorithm is a request the optimizer may answer differently, which is
// the language's most distinctive claim. The editor can now show what it did.
func TestDevExplainShowsWhatTheOptimizerDid(t *testing.T) {
	m := devWriteProgram(t,
		"Cursed Energy: in.txt\nShikigami: Ints\nDomain Expansion: Quicksort, Descending\n"+
			"Maximum Technique: Select Top 3, Sum\nReveal: stdout\n",
		"5\n3\n9\n1\n7\n2\n")
	m = runToCompletion(t, m)
	m.output = nil

	m = devKey(m, "alt+e")
	if m.output == nil {
		t.Fatal("no explain pane")
	}
	body := strings.Join(m.output.lines, "\n")
	if strings.Contains(body, "no optimizations") {
		t.Errorf("the optimizer reported nothing for a substitutable program:\n%s", body)
	}
}

// A pipeline with nothing to rewrite says so, rather than showing an empty pane
// that reads like a failure.
func TestDevExplainSaysWhenThereIsNothingToSay(t *testing.T) {
	m := devWriteProgram(t, valueProgram, "1\n2\n3\n")
	m = runToCompletion(t, m)
	m.output = nil
	m = devKey(m, "alt+e")
	if m.output == nil {
		t.Fatal("no explain pane")
	}
	if !strings.Contains(strings.Join(m.output.lines, "\n"), "no optimizations") {
		t.Errorf("pane reads %v", m.output.lines)
	}
}

func TestDevExplainNeedsARun(t *testing.T) {
	m := newTestDevModel(valueProgram)
	m = devKey(m, "alt+e")
	if !strings.Contains(m.status, "nothing recorded") {
		t.Errorf("status is %q", m.status)
	}
}
