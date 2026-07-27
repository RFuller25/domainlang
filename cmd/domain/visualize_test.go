package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/interp"
	"domain/ir"
)

func TestParseVisualizeArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantPath string
		wantOpts visualizeOptions
		wantErr  bool
	}{
		{"file only", []string{"p.domain"}, "p.domain", visualizeOptions{Optimize: true}, false},
		{"plain", []string{"p.domain", "--plain"}, "p.domain", visualizeOptions{Optimize: true, Plain: true}, false},
		{"input space", []string{"p.domain", "--input", "in.txt"}, "p.domain",
			visualizeOptions{Optimize: true, Input: "in.txt"}, false},
		{"input equals", []string{"p.domain", "--input=in.txt"}, "p.domain",
			visualizeOptions{Optimize: true, Input: "in.txt"}, false},
		{"short input", []string{"-i", "in.txt", "p.domain"}, "p.domain",
			visualizeOptions{Optimize: true, Input: "in.txt"}, false},
		{"max steps", []string{"p.domain", "--max-steps", "50"}, "p.domain",
			visualizeOptions{Optimize: true, MaxSteps: 50}, false},
		{"max steps equals", []string{"p.domain", "--max-steps=50"}, "p.domain",
			visualizeOptions{Optimize: true, MaxSteps: 50}, false},
		{"no optimize", []string{"p.domain", "--no-optimize"}, "p.domain", visualizeOptions{}, false},
		{"no file", []string{"--plain"}, "", visualizeOptions{}, true},
		{"two files", []string{"a.domain", "b.domain"}, "", visualizeOptions{}, true},
		{"unknown flag", []string{"p.domain", "--wat"}, "", visualizeOptions{}, true},
		{"max steps missing value", []string{"p.domain", "--max-steps"}, "", visualizeOptions{}, true},
		{"max steps not a number", []string{"p.domain", "--max-steps", "x"}, "", visualizeOptions{}, true},
		{"max steps zero", []string{"p.domain", "--max-steps", "0"}, "", visualizeOptions{}, true},
		{"input missing value", []string{"p.domain", "--input"}, "", visualizeOptions{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, opts, err := parseVisualizeArgs(c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if path != c.wantPath || opts != c.wantOpts {
				t.Errorf("got (%q, %+v), want (%q, %+v)", path, opts, c.wantPath, c.wantOpts)
			}
		})
	}
}

// writeVisProgram writes a program (and optional in.txt) into a temp dir.
func writeVisProgram(t *testing.T, src, input string) (dir, prog string) {
	t.Helper()
	dir = t.TempDir()
	prog = filepath.Join(dir, "p.domain")
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if input != "" {
		if err := os.WriteFile(filepath.Join(dir, "in.txt"), []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, prog
}

const visProgram = `Cursed Energy: in.txt
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Simple Domain: Repeat 2
    Cursed Technique: Map Each
        Using: (x) -> x + 1
Maximum Technique: Sum
Reveal: stdout
`

func TestVisualizePlainOutput(t *testing.T) {
	_, prog := writeVisProgram(t, visProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	got := out.String()
	for _, want := range []string{
		"Read Source",
		"Split by",
		"Repeat 2",
		"Repeat 2 iter 1/2",
		"Repeat 2 iter 2/2",
		"Map Each",
		"Sum",
		"revealed:",
		"12", // 1,2,3 each incremented twice -> 3+4+5
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plain output missing %q:\n%s", want, got)
		}
	}
}

// The program's own input is found from its `Cursed Energy:` target, so no
// --input is needed and the terminal is never asked to be stdin.
func TestVisualizeFindsTheProgramsOwnInput(t *testing.T) {
	_, prog := writeVisProgram(t, visProgram, "4,5")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "revealed:") {
		t.Errorf("the program should have run:\n%s", out.String())
	}
}

func TestVisualizeWithExplicitInput(t *testing.T) {
	dir, prog := writeVisProgram(t, "Cursed Energy: stdin\n"+
		"Cursed Technique: Split Text by \",\"\nMaximum Technique: Count\nReveal: stdout\n", "")
	inputPath := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(inputPath, []byte("a,b,c,d"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	opts := visualizeOptions{Optimize: true, Plain: true, Input: inputPath}
	if code := Visualize(prog, opts, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "4") {
		t.Errorf("expected the count of 4 elements:\n%s", out.String())
	}
}

func TestVisualizeMissingInputFile(t *testing.T) {
	_, prog := writeVisProgram(t, visProgram, "1")
	var out, errBuf bytes.Buffer
	opts := visualizeOptions{Optimize: true, Plain: true, Input: "no-such-input"}
	if code := Visualize(prog, opts, strings.NewReader(""), &out, &errBuf); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "--input") {
		t.Errorf("stderr should mention --input, got %q", errBuf.String())
	}
}

// A program that needs stdin, with nothing piped in, must say how to fix it
// rather than hanging or running on empty input.
func TestVisualizeRefusesWithoutAnyInput(t *testing.T) {
	_, prog := writeVisProgram(t, "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nMaximum Technique: Count\nReveal: stdout\n", "")
	var out, errBuf bytes.Buffer
	// A *File that is a char device would be a terminal; a bytes.Reader that is
	// empty stands in for "nothing piped".
	code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, bytes.NewReader(nil), &out, &errBuf)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "--input") {
		t.Errorf("the error should name the fix, got %q", errBuf.String())
	}
}

func TestVisualizeBrokenProgram(t *testing.T) {
	_, prog := writeVisProgram(t, "Cursed Energy: in.txt\nMaximum Technique: Frobnicate\n", "1")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, strings.NewReader(""), &out, &errBuf); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if errBuf.Len() == 0 {
		t.Error("expected the resolve error on stderr")
	}
}

// A failing run is still a recording: the trace shows how far it got and what
// went wrong.
func TestVisualizeFailingRunIsStillExplorable(t *testing.T) {
	_, prog := writeVisProgram(t,
		"Cursed Energy: in.txt\nCursed Technique: Split Text by \",\"\n"+
			"Channeled Energy: Convert List to Integers\nReveal: stdout\n", "1,nope")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d (a recorded failure is not a command failure)", code)
	}
	got := out.String()
	if !strings.Contains(got, "run failed:") {
		t.Errorf("the run's failure should be reported:\n%s", got)
	}
	if !strings.Contains(got, "error:") || !strings.Contains(got, "Split by") {
		t.Errorf("the trace should show the steps that ran and the failing one:\n%s", got)
	}
}

// The optimizer's rewrites are shown, which is the visualizer's tie-in to the
// language's thesis.
func TestVisualizeShowsOptimizerRewrites(t *testing.T) {
	_, prog := writeVisProgram(t, "Cursed Energy: in.txt\n"+
		"Cursed Technique: Split Text by \",\"\nChanneled Energy: Convert List to Integers\n"+
		"Domain Expansion: Quicksort, Descending\nMaximum Technique: Select Top 2, Sum\nReveal: stdout\n",
		"5,1,9,7")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	got := out.String()
	if !strings.Contains(got, "optimizer:") || !strings.Contains(got, "Quickselect") {
		t.Errorf("the rewrite should be reported:\n%s", got)
	}
}

func TestVisualizeMaxStepsCap(t *testing.T) {
	_, prog := writeVisProgram(t, visProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	opts := visualizeOptions{Optimize: true, Plain: true, MaxSteps: 3}
	if code := Visualize(prog, opts, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "capped") {
		t.Errorf("the cap should be reported:\n%s", out.String())
	}
}

// --- the TUI model, driven by injected messages (as repl_tty_test.go does) ---

// visModel builds a model over a recorded run of visProgram.
func visModel(t *testing.T) *visualModel {
	t.Helper()
	_, prog := writeVisProgram(t, visProgram, "1,2,3")
	pipe, rewrites, err := loadForVisualize(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	rec := interp.NewRecorder(0)
	var revealed strings.Builder
	ctx := newVisCtx(t, prog, rec, &revealed)
	if _, err := interp.Run(pipe, ctx); err != nil {
		t.Fatal(err)
	}
	return newVisualModel(&traceView{path: prog, rec: rec, rewrites: rewrites})
}

func key(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: firstRune(s), Text: s}
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func send(m *visualModel, msgs ...tea.Msg) *visualModel {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(*visualModel)
	}
	return m
}

func TestVisualModelStartsCollapsed(t *testing.T) {
	m := visModel(t)
	// Top-level stages only: the loop's iterations are hidden until opened.
	for _, r := range m.rows {
		if r.depth != 0 {
			t.Fatalf("row %q is nested but nothing has been expanded", r.node.Label())
		}
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestVisualModelNavigation(t *testing.T) {
	m := visModel(t)
	n := len(m.rows)
	m = send(m, key("j"), key("j"))
	if m.cursor != 2 {
		t.Errorf("cursor after two j = %d, want 2", m.cursor)
	}
	m = send(m, key("k"))
	if m.cursor != 1 {
		t.Errorf("cursor after k = %d, want 1", m.cursor)
	}
	m = send(m, key("G"))
	if m.cursor != n-1 {
		t.Errorf("cursor after G = %d, want %d", m.cursor, n-1)
	}
	m = send(m, key("g"))
	if m.cursor != 0 {
		t.Errorf("cursor after g = %d, want 0", m.cursor)
	}
	// Bounds hold: k at the top and j at the bottom do nothing.
	m = send(m, key("k"))
	if m.cursor != 0 {
		t.Errorf("k at the top moved the cursor to %d", m.cursor)
	}
	m = send(m, key("G"), key("j"))
	if m.cursor != n-1 {
		t.Errorf("j at the bottom moved the cursor to %d", m.cursor)
	}
}

func TestVisualModelExpandAndCollapse(t *testing.T) {
	m := visModel(t)
	// Find the loop row, the only one with children.
	loop := -1
	for i, r := range m.rows {
		if len(r.node.Children) > 0 {
			loop = i
			break
		}
	}
	if loop < 0 {
		t.Fatal("no expandable row in the recording")
	}
	before := len(m.rows)

	m.cursor = loop
	m = send(m, key("l"))
	if len(m.rows) <= before {
		t.Fatalf("expanding added no rows (%d -> %d)", before, len(m.rows))
	}
	if m.rows[loop+1].depth != 1 {
		t.Errorf("the first child should be at depth 1, got %d", m.rows[loop+1].depth)
	}

	// Expanding again steps into the frame rather than doing nothing.
	m = send(m, key("l"))
	if m.cursor != loop+1 {
		t.Errorf("a second l should step into the frame, cursor = %d", m.cursor)
	}

	// h on a child with nothing to close jumps to the parent.
	m = send(m, key("h"))
	if m.cursor != loop {
		t.Errorf("h should move to the parent row, cursor = %d", m.cursor)
	}
	// h again collapses it.
	m = send(m, key("h"))
	if len(m.rows) != before {
		t.Errorf("collapsing did not restore the row count: %d, want %d", len(m.rows), before)
	}
}

// Each pane key opens its pane and closes it again, and the value view is what
// the stepper falls back to.
func TestVisualModelPaneToggles(t *testing.T) {
	cases := []struct {
		key   string
		pane  detailPane
		title string
	}{
		{"e", paneExplain, "optimizer rewrites"},
		{"t", paneHot, "where the time went"},
		{"s", paneSource, "source"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			m := visModel(t)
			if m.pane != paneValue {
				t.Fatal("the stepper should start on the value pane")
			}
			m = send(m, key(c.key))
			if m.pane != c.pane {
				t.Errorf("%q should open its pane, got %v", c.key, m.pane)
			}
			if got := m.View().Content; !strings.Contains(got, c.title) {
				t.Errorf("the pane should be rendered with its heading %q:\n%s", c.title, got)
			}
			m = send(m, key(c.key))
			if m.pane != paneValue {
				t.Errorf("%q should close it again, got %v", c.key, m.pane)
			}
		})
	}
}

func TestVisualModelQuits(t *testing.T) {
	m := visModel(t)
	for _, k := range []string{"q", "esc"} {
		_, cmd := m.Update(key(k))
		if cmd == nil {
			t.Errorf("%q should quit", k)
		}
	}
}

func TestVisualModelViewRendersPanes(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	out := m.View().Content
	if !strings.Contains(out, "│") {
		t.Error("the view should render the pane divider")
	}
	if !strings.Contains(out, "Read Source") {
		t.Error("the tree pane should list the first stage")
	}
	if !strings.Contains(out, "type") || !strings.Contains(out, "out") {
		t.Error("the detail pane should describe the selected step")
	}
	if !strings.Contains(out, "quit") {
		t.Error("the footer should list the keys")
	}
	// Every rendered line must respect the terminal width — measured the way a
	// terminal measures it, since the styled cells carry escape sequences that
	// occupy no columns.
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if n := ansi.StringWidth(line); n > 120 {
			t.Errorf("line %d is %d columns wide, want <= 120:\n%q", i, n, line)
		}
	}
}

// The whole layout rests on styled cells never being counted as text, so the
// width helpers measure printable columns rather than bytes or runes.
func TestWidthHelpersIgnoreStyling(t *testing.T) {
	styled := styErr.Render("boom")
	if len(styled) <= len("boom") {
		t.Skip("styling is disabled in this environment")
	}
	if n := ansi.StringWidth(pad(styled, 10)); n != 10 {
		t.Errorf("pad(styled, 10) is %d columns, want 10", n)
	}
	if n := ansi.StringWidth(truncateVis(styled, 3)); n > 3 {
		t.Errorf("truncateVis(styled, 3) is %d columns, want <= 3", n)
	}
}

// --- timings as shares of the run ---

// visPlain records visProgram and returns the plain output's lines.
func visPlain(t *testing.T) []string {
	t.Helper()
	_, prog := writeVisProgram(t, visProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

// pctOn extracts the `%` column from a plain-output row, which is the second
// of the two right-hand percentage columns to be filled and always present.
func pctOn(t *testing.T, line string) float64 {
	t.Helper()
	for _, f := range strings.Fields(line) {
		if strings.HasSuffix(f, "%") && !strings.HasPrefix(f, "<") {
			if v, err := strconv.ParseFloat(strings.TrimSuffix(f, "%"), 64); err == nil {
				return v
			}
		}
	}
	t.Fatalf("no percentage in row %q", line)
	return 0
}

// The point of the column: every step says what share of the run it took, and
// the top-level rows account for the whole run.
func TestVisualizePlainReportsShares(t *testing.T) {
	lines := visPlain(t)
	if !strings.Contains(lines[0], "total") {
		t.Errorf("the header should name the denominator, got %q", lines[0])
	}
	header := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "step ") && strings.Contains(l, "self%") {
			header = i
			break
		}
	}
	if header < 0 {
		t.Fatalf("no column header in:\n%s", strings.Join(lines, "\n"))
	}

	// Sum the top-level rows: the unindented ones, up to the blank line that
	// ends the table (the optimizer and revealed sections follow it).
	var sum float64
	var rows int
	for _, l := range lines[header+1:] {
		if l == "" {
			break
		}
		if strings.HasPrefix(l, " ") { // a nested row, or a step's error line
			continue
		}
		sum += pctOn(t, l)
		rows++
	}
	if rows < 5 {
		t.Fatalf("expected the pipeline's stages, found %d rows", rows)
	}
	// Each row is rounded to a tenth, so the sum lands near 100 rather than on it.
	if sum < 99 || sum > 101 {
		t.Errorf("top-level shares sum to %.1f%%, want ~100%%:\n%s", sum, strings.Join(lines, "\n"))
	}
}

// The self column exists to keep a loop from reading as a slow primitive, so it
// appears on the loop and nowhere it would just repeat the total.
func TestVisualizePlainSelfColumnOnlyWhereItSaysSomething(t *testing.T) {
	lines := visPlain(t)
	var loop, leaf string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "Repeat 2 "):
			loop = l
		case strings.HasPrefix(l, "Sum "):
			leaf = l
		}
	}
	if loop == "" || leaf == "" {
		t.Fatalf("expected both a loop and a leaf row:\n%s", strings.Join(lines, "\n"))
	}
	if n := strings.Count(loop, "%"); n != 2 {
		t.Errorf("the loop row should carry both a total and a self share, got %d:\n%s", n, loop)
	}
	if n := strings.Count(leaf, "%"); n != 1 {
		t.Errorf("a leaf's self share repeats its total and should be left out, got %d:\n%s", n, leaf)
	}
}

// A frame is not an evaluation, but it does have a cost — that is what makes
// one iteration comparable against its siblings.
func TestVisualizePlainTimesFrames(t *testing.T) {
	for _, l := range visPlain(t) {
		if strings.Contains(l, "Repeat 2 iter 1/2") {
			if !strings.Contains(l, "%") {
				t.Errorf("an iteration frame should carry its share of the run:\n%s", l)
			}
			return
		}
	}
	t.Fatal("no iteration frame in the plain output")
}

func TestVisualModelShowsSharesInBothPanes(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	out := m.View().Content
	if !strings.Contains(out, "total") {
		t.Error("the header should name the run's total time")
	}
	if !strings.Contains(out, "of the run") {
		t.Errorf("the detail pane should give the step's share of the run:\n%s", out)
	}
	if !strings.Contains(out, "%") {
		t.Error("the tree pane should carry a share column")
	}
}

// Selecting a loop shows where its time actually went: mostly its frames', not
// its own.
func TestVisualModelSelfTimeOnANestedRow(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	for i, r := range m.rows {
		if len(r.node.Children) > 0 {
			m.cursor = i
			break
		}
	}
	out := m.View().Content
	if !strings.Contains(out, "self") {
		t.Errorf("a row with frames under it should report its self time:\n%s", out)
	}
}

// A frame row has no value to show, but it does have a cost and a step count —
// better than the placeholder it used to render.
func TestVisualModelFrameDetailShowsItsCost(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	for i, r := range m.rows {
		if len(r.node.Children) > 0 {
			m.cursor = i
			break
		}
	}
	m = send(m, key("l"), key("l")) // open the loop and step into its first frame
	if !m.rows[m.cursor].node.IsFrame() {
		t.Fatalf("expected a frame under the cursor, got %q", m.rows[m.cursor].node.Label())
	}
	out := m.View().Content
	for _, want := range []string{"a frame", "of the run", "steps"} {
		if !strings.Contains(out, want) {
			t.Errorf("the frame detail should contain %q:\n%s", want, out)
		}
	}
}

// --- the profile, the jumps, search, and the source pane ---

// The profile answers a question the tree cannot: on a recording with many
// iterations, the body is invisible row by row and the whole run in aggregate.
func TestVisualModelProfileRanksTheBody(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24}, key("t"))
	out := m.View().Content
	if !strings.Contains(out, "Map Each ×2") {
		t.Errorf("the loop body's two calls should be rolled into one ranked entry:\n%s", out)
	}
	if !strings.Contains(out, "call sites by self time") {
		t.Error("the profile should say what it is ranking by")
	}
}

// H lands on the hottest row and says what it found — including when that row
// was inside a collapsed frame, which is the case that makes the key worth
// having.
func TestVisualModelJumpToHottest(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	want := m.view.times().Hottest()
	if want == nil {
		t.Fatal("the recording should have a hottest row")
	}
	m = send(m, key("H"))
	if got := m.rows[m.cursor].node; got != want {
		t.Errorf("cursor landed on %q, want %q", got.Label(), want.Label())
	}
	if !strings.Contains(m.status, "hottest") {
		t.Errorf("the jump should report what it found, got %q", m.status)
	}
	if !strings.Contains(m.View().Content, "hottest") {
		t.Error("the footer should show the status")
	}
}

// A jump into a collapsed frame has to open the frames above it, or it lands
// somewhere nobody can see.
func TestVisualModelJumpOpensTheFramesAbove(t *testing.T) {
	m := visModel(t)
	// Force the hottest row to be the loop body, inside a collapsed frame.
	var body *interp.TraceNode
	var walk func(nodes []*interp.TraceNode)
	walk = func(nodes []*interp.TraceNode) {
		for _, n := range nodes {
			if !n.IsFrame() && n.Label() == "Map Each" {
				body = n
			}
			walk(n.Children)
		}
	}
	walk(m.view.rec.Roots())
	if body == nil {
		t.Fatal("expected a Map Each inside the loop")
	}
	for _, r := range m.rows {
		if r.node == body {
			t.Fatal("the body should start hidden inside a collapsed frame")
		}
	}

	m.reveal(body)
	if m.rows[m.cursor].node != body {
		t.Errorf("reveal did not put the cursor on the target")
	}
	if m.rows[m.cursor].depth == 0 {
		t.Error("the revealed row should be a nested one")
	}
}

// A failed run is the common reason to open a debugger, and the failing step
// can be thousands of rows in.
func TestVisualModelJumpToFailure(t *testing.T) {
	_, prog := writeVisProgram(t,
		"Cursed Energy: in.txt\nCursed Technique: Split Text by \",\"\n"+
			"Channeled Energy: Convert List to Integers\nReveal: stdout\n", "1,nope")
	pipe, _, err := loadForVisualize(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	rec := interp.NewRecorder(0)
	var revealed strings.Builder
	_, runErr := interp.Run(pipe, newVisCtx(t, prog, rec, &revealed))
	if runErr == nil {
		t.Fatal("this program should fail")
	}
	m := newVisualModel(&traceView{path: prog, rec: rec, runErr: runErr})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24}, key("!"))

	got := m.rows[m.cursor].node
	if got.IsFrame() || got.Step.Err == nil {
		t.Fatalf("! should land on a failing step, got %q", got.Label())
	}
	if !strings.Contains(m.status, "nope") {
		t.Errorf("the jump should report the error, got %q", m.status)
	}
}

// On a clean run the key says so rather than moving the cursor somewhere
// arbitrary.
func TestVisualModelJumpToFailureWithNoFailure(t *testing.T) {
	m := visModel(t)
	before := m.cursor
	m = send(m, key("!"))
	if m.cursor != before {
		t.Errorf("with nothing to find the cursor should stay put, moved to %d", m.cursor)
	}
	if !strings.Contains(m.status, "no failing step") {
		t.Errorf("the key should say it found nothing, got %q", m.status)
	}
}

// Search narrows the tree to matching rows and the paths that reach them, live
// as the query is typed.
func TestVisualModelSearch(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24}, key("/"))
	if !m.searching {
		t.Fatal("/ should start a search")
	}
	m = send(m, key("m"), key("a"), key("p"))
	if m.filter != "map" {
		t.Fatalf("filter = %q, want %q", m.filter, "map")
	}
	for _, r := range m.rows {
		label := strings.ToLower(r.node.Label())
		// Every visible row is either a match or on the path to one.
		if !strings.Contains(label, "map") && len(r.node.Children) == 0 {
			t.Errorf("row %q neither matches nor leads to a match", r.node.Label())
		}
	}
	// The body is inside a collapsed frame, and a search that could not see
	// past that would look like it found nothing.
	var found bool
	for _, r := range m.rows {
		if r.node.Label() == "Map Each" {
			found = true
		}
	}
	if !found {
		t.Error("the search should reach rows inside collapsed frames")
	}

	// Backspace narrows back out; enter accepts and leaves the filter in place.
	m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.filter != "ma" {
		t.Errorf("backspace should shorten the filter, got %q", m.filter)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.searching || m.filter != "ma" {
		t.Errorf("enter should accept: searching = %v, filter = %q", m.searching, m.filter)
	}
	// Escape backs the filter out before it leaves the program.
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.filter != "" {
		t.Errorf("esc should clear the filter, got %q", m.filter)
	}
}

// A filter that matches nothing has to say so, not render a blank pane.
func TestVisualModelSearchWithNoMatch(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24}, key("/"), key("z"), key("z"))
	if len(m.rows) != 0 {
		t.Fatalf("nothing should match %q, got %d rows", m.filter, len(m.rows))
	}
	if out := m.View().Content; !strings.Contains(out, "no row matches") {
		t.Errorf("an empty result should explain itself:\n%s", out)
	}
}

// A collapsed row says what it is hiding, so the 98% row is not a mystery.
func TestVisualModelCollapsedRowsSayWhatTheyHide(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	if out := m.View().Content; !strings.Contains(out, "(2 frames, 2 steps)") {
		t.Errorf("the collapsed loop should report its hidden work:\n%s", out)
	}
	// Opened, the count is gone: the rows themselves are the answer now.
	for i, r := range m.rows {
		if len(r.node.Children) > 0 {
			m.cursor = i
			break
		}
	}
	m = send(m, key("l"))
	if out := m.View().Content; strings.Contains(out, "(2 frames, 2 steps)") {
		t.Errorf("an opened row should not still claim to be hiding its rows:\n%s", out)
	}
}

// The source pane projects the profile back onto the text the user wrote, which
// is where a fix has to happen.
func TestVisualModelSourcePane(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 24}, key("s"))
	out := m.View().Content
	if !strings.Contains(out, "self time by line") {
		t.Error("the source pane should say what the gutter holds")
	}
	if !strings.Contains(out, "Cursed Technique: Split Text") {
		t.Errorf("the pane should render the program:\n%s", out)
	}
	shares := m.view.lineShares()
	if len(shares) == 0 {
		t.Fatal("some line of the program should carry a share of the run")
	}
	src := m.view.source()
	for line := range shares {
		if line < 1 || line > len(src) {
			t.Errorf("line %d is outside the %d-line program", line, len(src))
		}
	}
}

// The detail pane names the line a step came from — and, for a step inlined
// from the prelude or a library, names that source instead of pointing at a
// line of the user's program that has nothing to do with it.
func TestVisualizeSourceAttribution(t *testing.T) {
	_, prog := writeVisProgram(t,
		"Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n", "1\n2")
	pipe, _, err := loadForVisualize(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	rec := interp.NewRecorder(0)
	var revealed strings.Builder
	if _, err := interp.Run(pipe, newVisCtx(t, prog, rec, &revealed)); err != nil {
		t.Fatal(err)
	}
	v := &traceView{path: prog, rec: rec}

	var sawLine, sawForeign bool
	for _, n := range rec.Roots() {
		if n.IsFrame() {
			continue
		}
		switch where := v.where(n.Step.Node); {
		case strings.HasPrefix(where, "line "):
			sawLine = true
		case strings.HasPrefix(where, "inlined from"):
			sawForeign = true
		}
	}
	if !sawLine {
		t.Error("the user's own statements should report their line")
	}
	if !sawForeign {
		t.Error("a step inlined from the prelude should name the prelude, not a line")
	}
	// And nothing inlined leaks into the per-line profile.
	src := v.source()
	for line := range v.lineShares() {
		if line > len(src) {
			t.Errorf("line %d is past the end of the %d-line program", line, len(src))
		}
	}
}

// --- --json ---

func TestVisualizeJSONOutput(t *testing.T) {
	_, prog := writeVisProgram(t, visProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	opts := visualizeOptions{Optimize: true, JSON: true}
	if code := Visualize(prog, opts, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}

	var doc struct {
		Program  string `json:"program"`
		Steps    int    `json:"steps"`
		Total    string `json:"total"`
		Revealed string `json:"revealed"`
		Rows     []struct {
			Kind     string  `json:"kind"`
			Label    string  `json:"label"`
			Line     int     `json:"line"`
			Pct      float64 `json:"pct"`
			Children []struct {
				Kind  string `json:"kind"`
				Label string `json:"label"`
			} `json:"children"`
		} `json:"rows"`
		Hotspots []struct {
			Name  string `json:"name"`
			Calls int    `json:"calls"`
		} `json:"hotspots"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if doc.Program != prog || doc.Steps == 0 || doc.Total == "" {
		t.Errorf("document header = %+v", doc)
	}
	if doc.Revealed != "12" {
		t.Errorf("revealed = %q, want the program's output", doc.Revealed)
	}
	if len(doc.Rows) == 0 {
		t.Fatal("the trace should be in the document")
	}
	loop := doc.Rows[len(doc.Rows)-3]
	if !strings.HasPrefix(loop.Label, "Repeat 2") || len(loop.Children) != 2 {
		t.Errorf("the loop's iterations should be nested: %+v", loop)
	}
	if doc.Rows[0].Line != 1 {
		t.Errorf("the first stage should be line 1, got %d", doc.Rows[0].Line)
	}
	var body bool
	for _, h := range doc.Hotspots {
		if h.Name == "Map Each" && h.Calls == 2 {
			body = true
		}
	}
	if !body {
		t.Errorf("the profile should be in the document: %+v", doc.Hotspots)
	}
}

// --json is asked for explicitly, so it wins over the plain printer rather than
// producing a table nobody can parse.
func TestVisualizeJSONBeatsPlain(t *testing.T) {
	_, prog := writeVisProgram(t, visProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	opts := visualizeOptions{Optimize: true, JSON: true, Plain: true}
	if code := Visualize(prog, opts, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !json.Valid(out.Bytes()) {
		t.Errorf("--json with --plain should still be JSON:\n%s", out.String())
	}
}

// A failed run exports its failure, so the document is usable for the job the
// UI is: finding what broke.
func TestVisualizeJSONCarriesFailure(t *testing.T) {
	_, prog := writeVisProgram(t,
		"Cursed Energy: in.txt\nCursed Technique: Split Text by \",\"\n"+
			"Channeled Energy: Convert List to Integers\nReveal: stdout\n", "1,nope")
	var out, errBuf bytes.Buffer
	opts := visualizeOptions{Optimize: true, JSON: true}
	if code := Visualize(prog, opts, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d (a recorded failure is not a command failure)", code)
	}
	var doc struct {
		Failed string `json:"failed"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Failed, "nope") {
		t.Errorf("failed = %q, want the run's error", doc.Failed)
	}
}

func TestShareBar(t *testing.T) {
	full := shareBar(interp.NodeTiming{TotalPct: 100, SelfPct: 100, Known: true}, 6)
	if full != strings.Repeat("█", 6) {
		t.Errorf("a row that is the whole run should fill the bar, got %q", full)
	}
	// A loop is nearly all frame time: a long light run behind a short solid head.
	loop := shareBar(interp.NodeTiming{TotalPct: 100, SelfPct: 10, Known: true}, 10)
	if !strings.HasPrefix(loop, "█") || !strings.Contains(loop, "░") {
		t.Errorf("a nested row should draw its self time solid and the rest light, got %q", loop)
	}
	// Anything that ran at all is visible.
	if tiny := shareBar(interp.NodeTiming{TotalPct: 0.3, SelfPct: 0.3, Known: true}, 6); !strings.Contains(tiny, "█") {
		t.Errorf("a step that ran should get at least one cell, got %q", tiny)
	}
	// Every bar is exactly its column width, whatever it is drawing.
	for _, nt := range []interp.NodeTiming{
		{TotalPct: 100, SelfPct: 100, Known: true},
		{TotalPct: 0.3, SelfPct: 0, Known: true},
		{}, // an unknown share draws blank rather than guessing
	} {
		if n := len([]rune(shareBar(nt, 6))); n != 6 {
			t.Errorf("bar for %+v is %d runes, want 6", nt, n)
		}
	}
}

// A narrow terminal must still render without panicking.
func TestVisualModelTinyTerminal(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 20, Height: 5})
	if out := m.View().Content; out == "" {
		t.Error("a tiny terminal should still render something")
	}
}

func TestWrapVis(t *testing.T) {
	got := wrapVis("the quick brown fox jumps", 10)
	for _, line := range got {
		if len([]rune(line)) > 10 {
			t.Errorf("line %q exceeds the width", line)
		}
	}
	if strings.Join(got, " ") != "the quick brown fox jumps" {
		t.Errorf("wrapping lost words: %v", got)
	}
}

func TestPadAndTruncateCountRunes(t *testing.T) {
	if got := pad("▾ x", 6); len([]rune(got)) != 6 {
		t.Errorf("pad produced %d runes, want 6", len([]rune(got)))
	}
	if got := truncateVis("▾ abcdef", 4); len([]rune(got)) != 4 {
		t.Errorf("truncateVis produced %d runes, want 4", len([]rune(got)))
	}
	if got := truncateVis("abc", 10); got != "abc" {
		t.Errorf("truncateVis shortened a fitting string: %q", got)
	}
}

// newVisCtx builds the run context the visualizer uses, for tests that drive
// the model directly.
func newVisCtx(t *testing.T, prog string, rec *interp.Recorder, out *strings.Builder) *ir.Context {
	t.Helper()
	return &ir.Context{
		Stdin:   strings.NewReader(""),
		Stdout:  out,
		BaseDir: filepath.Dir(prog),
		Trace:   rec,
	}
}

// Regression: the source stage is found from the resolved pipeline, not by
// reading the file. Scanning the text for the first statement broke as soon as
// a declaration sat above the source — an `Innate Domain:` line, a Shikigami
// definition — and never handled the keyword-less spelling at all.
func TestVisualizeFindsSourceBelowDeclarations(t *testing.T) {
	cases := []struct{ name, src string }{
		{
			"import above the source",
			"Innate Domain: lib\nCursed Energy: in.txt\n" +
				"Cursed Technique: Split Text by \",\"\nMaximum Technique: Count\nReveal: stdout\n",
		},
		{
			"shikigami definition above the source",
			"Shikigami \"Halve\"\n    Cursed Technique: Map Each\n        Using: (x) -> x / 2\n" +
				"Cursed Energy: in.txt\nCursed Technique: Split Text by \",\"\n" +
				"Maximum Technique: Count\nReveal: stdout\n",
		},
		{
			"keyword-less source",
			"in.txt\nSplit Text by \",\"\nCount\nstdout\n",
		},
		{
			"comments above the source",
			"# a header\n\n# and more\nCursed Energy: in.txt\n" +
				"Cursed Technique: Split Text by \",\"\nMaximum Technique: Count\nReveal: stdout\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, prog := writeVisProgram(t, c.src, "a,b,c")
			if strings.Contains(c.src, "Innate Domain") {
				lib := filepath.Join(dir, "lib.domain")
				if err := os.WriteFile(lib, []byte("Shikigami \"Unused\"\n    Maximum Technique: Sum\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var out, errBuf bytes.Buffer
			code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, strings.NewReader(""), &out, &errBuf)
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
			}
			if !strings.Contains(out.String(), "revealed:") {
				t.Errorf("the program's own input should have been found:\n%s", out.String())
			}
		})
	}
}

// A program whose source names a file that does not exist still needs input
// supplied, since the interpreter would fall back to stdin.
func TestVisualizeRefusesWhenTheNamedInputIsMissing(t *testing.T) {
	_, prog := writeVisProgram(t,
		"Cursed Energy: absent.txt\nCursed Technique: Split Text by \",\"\n"+
			"Maximum Technique: Count\nReveal: stdout\n", "")
	var out, errBuf bytes.Buffer
	code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true}, bytes.NewReader(nil), &out, &errBuf)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "--input") {
		t.Errorf("the error should name the fix, got %q", errBuf.String())
	}
}
