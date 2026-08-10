package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/eval"
	"domain/interp"
	"domain/ir"
	"domain/prims"
)

// The visualizer over a foreign block. Two things are specific to it and worth
// pinning: the block's *body* lines belong to no stage, so the profile must not
// attribute anything to them; and a failing block is the first error in the
// language whose message runs to several lines, which the step table has to
// keep inside its own margins.

func needsPythonVis(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("no python3 on PATH")
		}
	}
}

const visForeignProgram = `Cursed Energy: in.txt
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Domain Expansion: Python : List<Int> -> List<Int>
    import sys
    for line in sys.stdin:
        print(int(line) + 1)
Maximum Technique: Sum
Reveal: stdout
`

const visForeignFailing = `Cursed Energy: in.txt
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Domain Expansion: Python : List<Int> -> Int
    import sys
    raise ValueError("deliberate failure")
Reveal: stdout
`

// visForeignModel records a run of src and returns the navigable model.
func visForeignModel(t *testing.T, src string) *visualModel {
	t.Helper()
	_, prog := writeVisProgram(t, src, "1,2,3")
	pipe, rewrites, err := loadForVisualize(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	rec := interp.NewRecorder(0)
	// The same wiring Visualize does, so the recording carries what the panes
	// under test read.
	defer eval.WatchApplications(rec.Applied)()
	defer prims.WatchForeignRuns(rec.ForeignRan)()
	var revealed strings.Builder
	if _, err := interp.Run(pipe, newVisCtx(t, prog, rec, &revealed)); err != nil &&
		!strings.Contains(err.Error(), "deliberate") {
		t.Fatal(err)
	}
	return newVisualModel(&traceView{path: prog, pipe: pipe, rec: rec, rewrites: rewrites,
		revealed: strings.TrimRight(revealed.String(), "\n")})
}

func TestVisualizeForeignPlain(t *testing.T) {
	needsPythonVis(t)
	_, prog := writeVisProgram(t, visForeignProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true},
		strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	got := out.String()
	// The stage is named for what it is, with the types it declared.
	if !strings.Contains(got, "Python block (List<Int> -> List<Int>)") {
		t.Errorf("the foreign stage is not in the trace:\n%s", got)
	}
	// 2+3+4 = 9 — the block ran, and its result flowed on.
	if !strings.Contains(got, "9") {
		t.Errorf("the block's value did not reach the output:\n%s", got)
	}
}

// A foreign block's body is not Domain, so no line of it is a step. The profile
// must leave those lines blank rather than borrowing a share from the statement
// above them.
func TestVisualizeForeignBodyLinesHaveNoShare(t *testing.T) {
	needsPythonVis(t)
	m := visForeignModel(t, visForeignProgram)
	shares := m.view.lineShares()
	for _, line := range []int{5, 6, 7} { // the three Python lines
		if share, ok := shares[line]; ok {
			t.Errorf("line %d is inside the Python block but claims %v of the run", line, share)
		}
	}
	if _, ok := shares[4]; !ok {
		t.Error("the statement that opened the block claims no share of the run")
	}
}

// The source pane renders the block's body verbatim, since it is what the user
// wrote, and centres on the statement that opened it.
func TestVisualizeForeignSourcePane(t *testing.T) {
	needsPythonVis(t)
	m := visForeignModel(t, visForeignProgram)
	m = send(m, pressKey("j"), pressKey("j"), pressKey("j")) // onto the block
	if !strings.Contains(m.rowLabel(), "Python block") {
		t.Fatalf("cursor is on %q, not the foreign stage", m.rowLabel())
	}
	pane := ansi.Strip(strings.Join(m.sourceLines(80), "\n"))
	for _, want := range []string{"import sys", "for line in sys.stdin:", "print(int(line) + 1)"} {
		if !strings.Contains(pane, want) {
			t.Errorf("the source pane does not show %q:\n%s", want, pane)
		}
	}
}

// The emitted-Go pane maps the stage to the lines it compiled to, which for a
// foreign block are the encode, the run, and the decode.
func TestVisualizeForeignGoSpan(t *testing.T) {
	needsPythonVis(t)
	m := visForeignModel(t, visForeignProgram)
	m = send(m, pressKey("j"), pressKey("j"), pressKey("j"))
	span, ok := m.goSpan()
	if !ok {
		t.Fatal("the foreign stage compiled to nothing the pane could point at")
	}
	src, _, err := m.view.emitted()
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Join(src[span.Start-1:span.End-1], "\n")
	for _, want := range []string{"dmForeignRun", `Lang: "Python"`, "dmForeignLines"} {
		if !strings.Contains(body, want) {
			t.Errorf("the mapped Go does not contain %q:\n%s", want, body)
		}
	}
}

// A stage with no Using: lambda contributes no expression breakdown, rather
// than an empty or invented one.
func TestVisualizeForeignHasNoExpressions(t *testing.T) {
	needsPythonVis(t)
	m := visForeignModel(t, visForeignProgram)
	for _, e := range m.view.expressions() {
		if strings.Contains(e.Prim, "Foreign") || strings.Contains(e.Prim, "Python") {
			t.Errorf("the foreign stage produced an expression breakdown: %+v", e)
		}
	}
}

func TestVisualizeForeignJSON(t *testing.T) {
	needsPythonVis(t)
	_, prog := writeVisProgram(t, visForeignProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, JSON: true},
		strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("the recording is not valid JSON: %v", err)
	}
	if !strings.Contains(out.String(), "Python block") {
		t.Errorf("the foreign stage is missing from the recording:\n%s", out.String())
	}
}

// ---------------------------------------------------------------------------
// A failing block
// ---------------------------------------------------------------------------

// A foreign runtime's report runs to several lines, and the step table is
// columns. Every line of the error has to stay inside the table's margin, or
// the traceback breaks the alignment of everything below it.
func TestVisualizeForeignFailureKeepsTheTableAligned(t *testing.T) {
	needsPythonVis(t)
	_, prog := writeVisProgram(t, visForeignFailing, "1,2,3")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true},
		strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	got := out.String()
	if !strings.Contains(got, "deliberate failure") {
		t.Fatalf("the traceback did not survive into the trace:\n%s", got)
	}
	// Every line of the traceback is indented under the `error:` label; none
	// starts at column zero, where it would read as a new table row.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "Traceback") || strings.Contains(line, "ValueError:") ||
			strings.Contains(line, "File \"") {
			if !strings.HasPrefix(line, " ") {
				t.Errorf("a traceback line breaks out of the table margin: %q", line)
			}
		}
	}
	// The failing stage is still a row, with the stage tag on its first line
	// rather than dangling after somebody else's output.
	if !strings.Contains(got, "the Python block failed with status 1 (in Foreign Block)") {
		t.Errorf("the failure line is malformed:\n%s", got)
	}
}

// The detail pane has room for the whole report, and keeps its line structure:
// a traceback reflowed into a paragraph is not a traceback.
func TestVisualizeForeignFailureDetailPane(t *testing.T) {
	needsPythonVis(t)
	m := visForeignModel(t, visForeignFailing)
	m = send(m, pressKey("G"))
	if !strings.Contains(m.rowLabel(), "Python block") {
		t.Fatalf("the last row is %q, not the failing stage", m.rowLabel())
	}
	pane := ansi.Strip(strings.Join(m.detailLines(80), "\n"))
	for _, want := range []string{"error:", "Traceback (most recent call last)", "ValueError: deliberate failure"} {
		if !strings.Contains(pane, want) {
			t.Errorf("the detail pane is missing %q:\n%s", want, pane)
		}
	}
	// One line each, not one reflowed paragraph.
	if strings.Contains(pane, "Traceback (most recent call last): File") {
		t.Errorf("the traceback was reflowed into a paragraph:\n%s", pane)
	}
}

// Every pane, with the foreign stage selected. A stage with no lambda, no
// expression tree and a body that is not Domain is the shape most likely to
// surprise a pane that assumes otherwise, so each one is rendered rather than
// reasoned about.
func TestVisualizeForeignEveryPaneRenders(t *testing.T) {
	needsPythonVis(t)
	for _, prog := range []struct{ name, src string }{
		{"succeeding", visForeignProgram},
		{"failing", visForeignFailing},
	} {
		t.Run(prog.name, func(t *testing.T) {
			base := visForeignModel(t, prog.src)
			base = send(base, tea.WindowSizeMsg{Width: 100, Height: 30})
			// Onto the foreign stage: it is the fourth row in both programs.
			base = send(base, pressKey("g"), pressKey("j"), pressKey("j"), pressKey("j"))
			if !strings.Contains(base.rowLabel(), "Python block") {
				t.Fatalf("cursor is on %q, not the foreign stage", base.rowLabel())
			}
			// e: optimizer rewrites, t: timings, s: source, x: expressions,
			// c: the emitted Go, ?: the keys, H: help overlay, !: raw values.
			for _, key := range []string{"e", "t", "s", "x", "c", "?", "H", "!"} {
				t.Run(key, func(t *testing.T) {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("pane %q panicked on a foreign stage: %v", key, r)
						}
					}()
					m := send(base, pressKey(key))
					if out := ansi.Strip(m.View().Content); strings.TrimSpace(out) == "" {
						t.Errorf("pane %q rendered nothing", key)
					}
				})
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The inside of a foreign stage
// ---------------------------------------------------------------------------

// The `x` pane asks what a stage did inside. For a foreign stage that is the
// program it ran and the bytes that crossed to and from it, which is the one
// thing neither the value pane nor the source pane can show: the value pane has
// Domain values on both sides, and the translation between them is where a
// foreign stage's mistakes live.
func TestVisualizeForeignPaneShowsTheProgramAndTheWire(t *testing.T) {
	needsPythonVis(t)
	m := visForeignModel(t, visForeignProgram)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = send(m, pressKey("g"), pressKey("j"), pressKey("j"), pressKey("j"), pressKey("x"))
	if !strings.Contains(m.rowLabel(), "Python block") {
		t.Fatalf("cursor is on %q, not the foreign stage", m.rowLabel())
	}
	pane := ansi.Strip(m.View().Content)
	for _, want := range []string{
		"python block",           // the heading, not "expression"
		"for line in sys.stdin:", // the program it ran
		"stdin",                  // what it was given …
		"stdout",                 // … and what it said
		"python3",                // which runtime, resolved from PATH
	} {
		if !strings.Contains(pane, want) {
			t.Errorf("the pane does not show %q:\n%s", want, pane)
		}
	}
	// The dead end it replaces.
	if strings.Contains(pane, "has no Using:") {
		t.Errorf("the pane still reports a foreign stage as having no expression:\n%s", pane)
	}
}

// The bytes shown are the bytes that crossed, not a rendering of the Domain
// value on either side — 1,2,3 doubled by the pipeline arrives as "2\n4\n6\n",
// and that string is what a wire-format question is asked about.
func TestVisualizeForeignCapturesTheActualBytes(t *testing.T) {
	needsPythonVis(t)
	m := visForeignModel(t, visForeignProgram)
	var found *interp.ForeignExec
	for _, s := range m.view.foreignSteps() {
		if exec, ran := foreignOf(s); ran {
			found = exec
		}
	}
	if found == nil {
		t.Fatal("no foreign execution was recorded")
	}
	if got, want := found.Run.Stdin.Text, "1\n2\n3\n"; got != want {
		t.Errorf("stdin captured %q, want %q", got, want)
	}
	if got, want := found.Run.Stdout.Text, "2\n3\n4\n"; got != want {
		t.Errorf("stdout captured %q, want %q", got, want)
	}
	if found.Run.Lang != "Python" || !strings.Contains(found.Run.Command, "python") {
		t.Errorf("the command was not recorded: %+v", found.Run)
	}
	if found.Run.Dur <= 0 {
		t.Error("the run took no measurable time")
	}
}

// A failing block's stderr is the answer to "why", and is captured even though
// the pipeline never sees it.
func TestVisualizeForeignCapturesStderrOnFailure(t *testing.T) {
	needsPythonVis(t)
	m := visForeignModel(t, visForeignFailing)
	steps := m.view.foreignSteps()
	if len(steps) != 1 {
		t.Fatalf("got %d foreign steps, want 1", len(steps))
	}
	exec, ran := foreignOf(steps[0])
	if !ran {
		t.Fatal("the failing execution was not recorded")
	}
	if !strings.Contains(exec.Run.Stderr.Text, "deliberate failure") {
		t.Errorf("stderr was not captured: %q", exec.Run.Stderr.Text)
	}
	if exec.Run.Err == nil {
		t.Error("the recorded run does not know it failed")
	}
	// And the input it was given, which is what you need to reproduce it.
	if exec.Run.Stdin.Text == "" {
		t.Error("the failing block's input was not captured")
	}
}

// Without a watcher installed nothing is captured, and the pane says so rather
// than showing an empty program.
func TestVisualizeForeignUnrecordedSaysSo(t *testing.T) {
	needsPythonVis(t)
	_, prog := writeVisProgram(t, visForeignProgram, "1,2,3")
	pipe, _, err := loadForVisualize(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	rec := interp.NewRecorder(0) // deliberately no WatchForeignRuns
	var revealed strings.Builder
	if _, err := interp.Run(pipe, newVisCtx(t, prog, rec, &revealed)); err != nil {
		t.Fatal(err)
	}
	m := newVisualModel(&traceView{path: prog, pipe: pipe, rec: rec})
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = send(m, pressKey("g"), pressKey("j"), pressKey("j"), pressKey("j"), pressKey("x"))
	pane := ansi.Strip(m.View().Content)
	if !strings.Contains(pane, "did not run in the recording") {
		t.Errorf("an unrecorded stage does not say so:\n%s", pane)
	}
	// The source is on the node, so it is shown either way.
	if !strings.Contains(pane, "for line in sys.stdin:") {
		t.Errorf("the block's source needs no recording to be shown:\n%s", pane)
	}
}

func TestVisualizeForeignExpressionsReport(t *testing.T) {
	needsPythonVis(t)
	_, prog := writeVisProgram(t, visForeignProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true, Exprs: true},
		strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	got := out.String()
	for _, want := range []string{"foreign blocks:", "Python block", "source:", "stdin:", "stdout:"} {
		if !strings.Contains(got, want) {
			t.Errorf("--expressions does not report %q:\n%s", want, got)
		}
	}
}

func TestVisualizeForeignJSONCarriesTheWire(t *testing.T) {
	needsPythonVis(t)
	_, prog := writeVisProgram(t, visForeignProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, JSON: true},
		strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	var doc struct {
		Foreign []struct {
			Lang, Source, Command, Stdin, Stdout string
			Line                                 int
		} `json:"foreign"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(doc.Foreign) != 1 {
		t.Fatalf("got %d foreign entries, want 1", len(doc.Foreign))
	}
	f := doc.Foreign[0]
	if f.Lang != "Python" || f.Line != 4 {
		t.Errorf("entry names the wrong stage: %+v", f)
	}
	if f.Stdin != "1\n2\n3\n" || f.Stdout != "2\n3\n4\n" {
		t.Errorf("the wire bytes are wrong: in %q out %q", f.Stdin, f.Stdout)
	}
	if !strings.Contains(f.Source, "sys.stdin") || f.Command == "" {
		t.Errorf("the program and command are missing: %+v", f)
	}
}

// A stream the recording had no budget left for is not an empty stream.
func TestForeignStreamRendersADroppedCapture(t *testing.T) {
	dropped := ir.Capture{Text: "", Bytes: 4096}
	got := ansi.Strip(strings.Join(streamLines("stdin", dropped, 60), "\n"))
	if !strings.Contains(got, "not captured") {
		t.Errorf("a dropped capture reads as something else:\n%s", got)
	}
	empty := ir.Capture{}
	got = ansi.Strip(strings.Join(streamLines("stdin", empty, 60), "\n"))
	if !strings.Contains(got, "(nothing)") {
		t.Errorf("an empty stream reads as something else:\n%s", got)
	}
}

// Trailing whitespace and a missing final newline are exactly the differences a
// reader comes to this pane to find, and exactly the ones a terminal hides.
func TestForeignCodeBlockShowsInvisibleEnds(t *testing.T) {
	got := ansi.Strip(strings.Join(codeBlock("a  \nb", 40), "\n"))
	if !strings.Contains(got, "a··") {
		t.Errorf("trailing spaces are invisible:\n%s", got)
	}
	if !strings.Contains(got, "no trailing newline") {
		t.Errorf("a missing final newline is not reported:\n%s", got)
	}
	if strings.Contains(ansi.Strip(strings.Join(codeBlock("a\n", 40), "\n")), "no trailing newline") {
		t.Error("a well-terminated stream was reported as missing its newline")
	}
}
