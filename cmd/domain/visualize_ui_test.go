package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/interp"
)

// plainLines strips styling so a test can assert on what a reader sees.
func plainLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(ansi.Strip(l))
		b.WriteByte('\n')
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Scrolling and focus
// ---------------------------------------------------------------------------

// A pane taller than the terminal can be scrolled. Before this the recorder
// kept up to 64 KiB of a value and the pane showed the lines that fit, with no
// way to reach the rest.
func TestVisualPaneScrolls(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 12}, pressKey("s"))

	first := plainLines(m.detailLines(50))
	if len(m.detailLines(50)) <= m.height-3 {
		t.Skip("the source pane fits on screen; nothing to scroll")
	}
	m = send(m, pressKey("tab"))
	if m.focus != focusDetail {
		t.Fatal("tab should move the focus to the detail pane")
	}
	m = send(m, pressKey("j"), pressKey("j"))
	if m.detailTop != 2 {
		t.Errorf("detailTop = %d after two j, want 2", m.detailTop)
	}
	// The tree cursor did not move: the keys are driving the pane now.
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want the tree left where it was", m.cursor)
	}
	// And what is on screen changed.
	view := m.View().Content
	if strings.Contains(view, first) {
		t.Error("the pane should have scrolled")
	}
	// esc gives the keys back to the tree before it does anything else.
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.focus != focusTree {
		t.Error("esc should return the focus to the tree")
	}
}

// The scroll stops where the last line comes into view, rather than running off
// into a screen of blank.
func TestVisualPaneScrollStopsAtTheEnd(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 12}, pressKey("s"), pressKey("tab"))
	_ = m.View() // renders, which is what measures the pane
	m = send(m, pressKey("G"))
	if want := m.detailBottom(); m.detailTop != want {
		t.Errorf("detailTop = %d after G, want %d", m.detailTop, want)
	}
	m = send(m, pressKey("j"), pressKey("j"), pressKey("j"))
	if want := m.detailBottom(); m.detailTop != want {
		t.Errorf("detailTop = %d, want it pinned at %d", m.detailTop, want)
	}
}

// Moving in the tree resets the pane to the top: a new row is a new thing to
// read, and inheriting the last row's scroll offset shows its middle.
func TestVisualMovingResetsPaneScroll(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 12}, pressKey("s"), pressKey("tab"))
	m = send(m, pressKey("j"), pressKey("j"))
	m = send(m, pressKey("tab"), pressKey("j"))
	if m.detailTop != 0 {
		t.Errorf("detailTop = %d after moving the cursor, want 0", m.detailTop)
	}
}

// ---------------------------------------------------------------------------
// The navigable panes
// ---------------------------------------------------------------------------

// enter in the profile pane takes the tree to that call site.
func TestVisualProfileJumps(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30}, pressKey("t"), pressKey("tab"))
	if len(m.view.times().Hotspots(0)) == 0 {
		t.Skip("nothing took measurable time in this recording")
	}
	before := m.cursor
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.focus != focusTree {
		t.Error("a jump should hand the keys back to the tree")
	}
	if m.status == "" {
		t.Error("a jump should say where it went")
	}
	if m.cursor == before && len(m.rows) > 1 {
		// Not a hard failure — the hottest row can be the one already selected —
		// but the status line has to name it either way.
		t.Logf("cursor unchanged at %d; status = %q", m.cursor, m.status)
	}
}

// enter in the source pane goes to a step on that line, and says so when
// nothing ran there.
func TestVisualSourceJumps(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30}, pressKey("s"), pressKey("tab"))
	m = send(m, pressKey("g")) // line 1: the source stage
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.status, "line 1") {
		t.Errorf("status = %q, want it to name line 1", m.status)
	}
	if m.focus != focusTree {
		t.Error("a jump should hand the keys back to the tree")
	}
}

// ---------------------------------------------------------------------------
// Step numbers and going to one
// ---------------------------------------------------------------------------

// The step index is what --json prints, so the UI has to be able to find one.
func TestVisualJumpToStep(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})

	// Pick a step buried inside the loop, which is what makes the jump worth
	// having: it is inside a collapsed frame.
	var want int
	for _, n := range m.flat {
		if !n.IsFrame() && n.Step.Node.Prim == "Map Each" {
			want = n.Step.Index
		}
	}
	m = send(m, pressKey(":"))
	if !m.jumping {
		t.Fatal(": should start a step jump")
	}
	for _, r := range strings.Split(strconv.Itoa(want), "") {
		m = send(m, pressKey(r))
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	got := m.selectedNode()
	if got == nil || got.IsFrame() || got.Step.Index != want {
		t.Fatalf("cursor is on %v, want step #%d", got, want)
	}
	if !strings.Contains(m.status, "#") {
		t.Errorf("status = %q, want it to name the step", m.status)
	}
}

func TestVisualJumpToMissingStep(t *testing.T) {
	m := visModel(t)
	m = send(m, pressKey(":"), pressKey("9"), pressKey("9"), pressKey("9"),
		tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.status, "no step") {
		t.Errorf("status = %q, want it to say there is no such step", m.status)
	}
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func TestVisualSearchFields(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	// Open everything, so a filter has the whole tree to narrow.
	m.expandAll()

	cases := []struct {
		query string
		want  func(n *interp.TraceNode) bool
		name  string
	}{
		{"prim:sum", func(n *interp.TraceNode) bool {
			return !n.IsFrame() && n.Step.Node.Prim == "Sum"
		}, "by primitive"},
		{"type:List", func(n *interp.TraceNode) bool {
			return !n.IsFrame() && strings.Contains(typeOf(n.Step), "List")
		}, "by out type"},
		{"line:1", func(n *interp.TraceNode) bool {
			return !n.IsFrame() && n.Step.Node.Pos.Line == 1
		}, "by line"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			terms := parseQuery(c.query)
			var matched, wanted int
			for _, n := range m.flat {
				if m.matchesQuery(n, terms) {
					matched++
					if !c.want(n) {
						t.Errorf("%q matched %q, which it should not", c.query, n.Label())
					}
				}
				if c.want(n) {
					wanted++
				}
			}
			if matched == 0 {
				t.Errorf("%q matched nothing, want %d rows", c.query, wanted)
			}
		})
	}
}

// A bare term is still a substring of the label, so nothing anyone already
// types has changed meaning.
func TestVisualSearchPlainTermIsUnchanged(t *testing.T) {
	m := visModel(t)
	terms := parseQuery("sum")
	found := false
	for _, n := range m.flat {
		if m.matchesQuery(n, terms) {
			if !strings.Contains(strings.ToLower(n.Label()), "sum") {
				t.Errorf("a bare term matched %q", n.Label())
			}
			found = true
		}
	}
	if !found {
		t.Error("a bare term should still match by label")
	}
}

// A duration bound finds the slow rows without knowing what they are called.
func TestVisualSearchByTime(t *testing.T) {
	terms := parseQuery(">0ns")
	if len(terms) != 1 || terms[0].cmp != '>' {
		t.Fatalf("parsed %+v, want one comparison term", terms)
	}
	if terms := parseQuery("<1%"); len(terms) != 1 || !terms[0].isPct {
		t.Fatalf("parsed %+v, want a percentage term", terms)
	}
}

// n and N step between matches with the tree intact — a different operation
// from hiding everything else, and the right one when there are only a few.
func TestVisualNextMatch(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.expandAll()
	m.filter = "map"
	m = send(m, pressKey("n"))
	if !strings.Contains(strings.ToLower(m.selectedNode().Label()), "map") {
		t.Errorf("n landed on %q, want a match", m.selectedNode().Label())
	}
	if !strings.Contains(m.status, "match") {
		t.Errorf("status = %q, want it to count the matches", m.status)
	}
	// The tree was not narrowed: every row is still there.
	if len(m.rows) < 4 {
		t.Errorf("n should leave the tree whole, got %d rows", len(m.rows))
	}
}

func TestVisualNextMatchWithoutASearch(t *testing.T) {
	m := visModel(t)
	m = send(m, pressKey("n"))
	if !strings.Contains(m.status, "no search") {
		t.Errorf("status = %q, want it to say there is no search", m.status)
	}
}

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

func TestVisualResizeTree(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	before := m.treeShare
	m = send(m, pressKey(">"))
	if m.treeShare <= before {
		t.Errorf("treeShare = %d after >, want more than %d", m.treeShare, before)
	}
	// It stops rather than letting either half vanish.
	for range 40 {
		m = send(m, pressKey("<"))
	}
	if m.treeShare < 25 {
		t.Errorf("treeShare = %d, want it bounded", m.treeShare)
	}
	if got := len(ansi.Strip(strings.Split(m.View().Content, "\n")[1])); got == 0 {
		t.Error("the body should still render at the narrowest tree")
	}
}

// ---------------------------------------------------------------------------
// Re-recording
// ---------------------------------------------------------------------------

// visRecordModel is a model wired to a real program on disk, so it can be
// recorded again.
func visRecordModel(t *testing.T, src, input string) (*visualModel, string) {
	t.Helper()
	_, prog := writeVisProgram(t, src, input)
	spec := recordSpec{path: prog, optimize: true}
	view, err := spec.record()
	if err != nil {
		t.Fatal(err)
	}
	m := newVisualModel(view)
	m.spec = spec
	m.width, m.height = 120, 30
	return m, prog
}

// The view survives a re-record: the frames you opened stay open and the cursor
// stays on the row it was on, even though every pointer in the tree is new.
func TestVisualRerecordKeepsTheView(t *testing.T) {
	m, _ := visRecordModel(t, visProgram, "1,2,3")

	// Open the loop and land on a row inside it.
	m = send(m, pressKey("j"), pressKey("j"), pressKey("j"), pressKey("l"), pressKey("j"))
	wantLabel := m.selectedNode().Label()
	wantOpen := len(m.expanded)
	m.pane = paneHot

	msg := recordAndWait(t, m)
	m.finishRecording(msg)

	if got := m.selectedNode().Label(); got != wantLabel {
		t.Errorf("cursor is on %q, want %q", got, wantLabel)
	}
	if len(m.expanded) != wantOpen {
		t.Errorf("%d frames open after a re-record, want %d", len(m.expanded), wantOpen)
	}
	if m.pane != paneHot {
		t.Error("the pane a reader was on should survive a re-record")
	}
	if !strings.Contains(m.status, "re-recorded") {
		t.Errorf("status = %q, want it to report the re-record", m.status)
	}
}

// An unchanged program re-records to no changed rows, and says so.
func TestVisualRerecordReportsNoChange(t *testing.T) {
	m, _ := visRecordModel(t, visProgram, "1,2,3")
	m.finishRecording(recordAndWait(t, m))
	if !strings.Contains(m.status, "nothing changed") {
		t.Errorf("status = %q, want it to report no change", m.status)
	}
	for node := range m.changed {
		t.Errorf("row %q was marked changed by an identical run", node.Label())
	}
}

// An edited program re-records to marked rows and a status line naming what
// moved — the part that makes a re-record worth more than running it again.
func TestVisualRerecordMarksWhatChanged(t *testing.T) {
	m, prog := visRecordModel(t, visProgram, "1,2,3")
	before := m.view.revealed

	edited := strings.Replace(visProgram, "x + 1", "x + 100", 1)
	if err := os.WriteFile(prog, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	m.finishRecording(recordAndWait(t, m))

	if len(m.changed) == 0 {
		t.Error("an edited program should mark some rows changed")
	}
	if !strings.Contains(m.status, "revealed") {
		t.Errorf("status = %q, want it to report the new answer", m.status)
	}
	if m.view.revealed == before {
		t.Errorf("the program still reveals %q", before)
	}
}

// A program that no longer parses leaves the recording on screen and puts the
// error in the footer: a file halfway through an edit is the normal state of a
// watched file, not a reason to tear the trace down.
func TestVisualRerecordKeepsTheOldRecordingOnAnError(t *testing.T) {
	m, prog := visRecordModel(t, visProgram, "1,2,3")
	was := m.view
	rows := len(m.rows)

	if err := os.WriteFile(prog, []byte("this is not a domain program at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.finishRecording(recordAndWait(t, m))

	if m.view != was {
		t.Error("a failed re-record should leave the recording alone")
	}
	if len(m.rows) != rows {
		t.Errorf("%d rows after a failed re-record, want the original %d", len(m.rows), rows)
	}
	if !strings.Contains(m.status, "cannot record") {
		t.Errorf("status = %q, want it to report the error", m.status)
	}
}

// A row the edit deleted takes the cursor to its nearest surviving ancestor
// rather than dumping it at the top.
func TestVisualRerecordHandlesADeletedRow(t *testing.T) {
	m, prog := visRecordModel(t, visProgram, "1,2,3")
	// Land on the last stage, which the edit below removes.
	m = send(m, pressKey("G"))

	edited := strings.Replace(visProgram, "Maximum Technique: Sum\n", "", 1)
	if err := os.WriteFile(prog, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	m.finishRecording(recordAndWait(t, m))

	if m.selectedNode() == nil {
		t.Fatal("the cursor should still be on a row")
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		t.Errorf("cursor = %d is outside %d rows", m.cursor, len(m.rows))
	}
}

// recordAndWait runs the re-record command synchronously.
func recordAndWait(t *testing.T, m *visualModel) recordedMsg {
	t.Helper()
	cmd := m.startRecording("r")
	if cmd == nil {
		t.Fatal("startRecording returned no command")
	}
	msg, ok := cmd().(recordedMsg)
	if !ok {
		t.Fatal("the recording command did not return a recording")
	}
	return msg
}

// A recording with no program behind it — the REPL's `:visualize` overlay —
// says so rather than trying.
func TestVisualRerecordWithoutAProgram(t *testing.T) {
	m := visModel(t)
	m.spec = recordSpec{}
	if cmd := m.startRecording("r"); cmd != nil {
		t.Error("a specless model should not start a recording")
	}
	if !strings.Contains(m.status, "no program") {
		t.Errorf("status = %q, want it to say there is nothing to run", m.status)
	}
}

// ---------------------------------------------------------------------------
// Watching
// ---------------------------------------------------------------------------

func TestVisWatchNoticesAChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.domain")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newVisWatch(path, "")
	if _, ok := w.changed(); ok {
		t.Error("an untouched file should not report a change")
	}
	// A different size is a change even within a filesystem's timestamp
	// resolution, which is why both are compared.
	if err := os.WriteFile(path, []byte("one and a half\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := w.changed()
	if !ok || got != path {
		t.Errorf("changed() = (%q, %v), want (%q, true)", got, ok, path)
	}
	if _, ok := w.changed(); ok {
		t.Error("the same change should not be reported twice")
	}
}

// A file that vanishes for an instant — an editor writing through a temporary —
// is not a change, so a watch does not re-record a file that is not there.
func TestVisWatchIgnoresAMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.domain")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newVisWatch(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.changed(); ok {
		t.Error("a file that cannot be read should not count as a change")
	}
}

// A watch tick from a superseded generation is ignored.
func TestVisWatchTickGeneration(t *testing.T) {
	m := visModel(t)
	m.watch = newVisWatch()
	m.watch.gen = 3
	next, cmd := m.Update(visWatchTickMsg{gen: 1})
	if cmd != nil {
		t.Error("a stale tick should do nothing")
	}
	_ = next
}

// ---------------------------------------------------------------------------
// The delta line
// ---------------------------------------------------------------------------

func TestRecordingDeltaReadsAsASentence(t *testing.T) {
	d := recordingDelta{steps: 7, total: -2 * time.Millisecond, wasTotal: 10 * time.Millisecond, rows: 3}
	got := d.String()
	for _, want := range []string{"+7 steps", "% time", "3 rows changed"} {
		if !strings.Contains(got, want) {
			t.Errorf("delta = %q, want it to mention %q", got, want)
		}
	}
	quiet := recordingDelta{wasTotal: time.Millisecond}
	if !strings.Contains(quiet.String(), "nothing changed") {
		t.Errorf("an unchanged delta reads %q", quiet.String())
	}
	failed := recordingDelta{failure: "now fails: boom"}
	if !strings.Contains(failed.String(), "now fails") {
		t.Errorf("a delta that gained a failure reads %q", failed.String())
	}
}

// Row keys survive a re-record: they are what carries the open frames over,
// so two recordings of the same program must agree on them.
func TestNodeKeysAreStableAcrossRecordings(t *testing.T) {
	_, prog := writeVisProgram(t, visProgram, "1,2,3")
	spec := recordSpec{path: prog, optimize: true}
	first, err := spec.record()
	if err != nil {
		t.Fatal(err)
	}
	second, err := spec.record()
	if err != nil {
		t.Fatal(err)
	}
	keysOf := func(v *traceView) map[string]bool {
		out := map[string]bool{}
		for _, k := range nodeKeys(v.rec.Roots()) {
			out[k] = true
		}
		return out
	}
	a, b := keysOf(first), keysOf(second)
	if len(a) != len(b) {
		t.Fatalf("%d keys then %d — two runs of one program should agree", len(a), len(b))
	}
	for k := range a {
		if !b[k] {
			t.Errorf("key %q is missing from the second recording", k)
		}
	}
}
