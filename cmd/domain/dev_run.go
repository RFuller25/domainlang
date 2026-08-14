// Running the program you are editing, and stepping through the run.
//
// Two things make this more than "call the interpreter".
//
// The first is Ctrl+C. `While` loops are unbounded by design, and a TUI in raw
// mode has already turned Ctrl+C from a signal into a keystroke nobody is
// reading — so a run inside Update would be a run that cannot be stopped, on a
// language whose loops are the point. The run therefore goes on a tea.Cmd with
// an ir.Interrupter in the trace chain, the event loop keeps painting, and the
// interrupt reaches it from the goroutine that is still listening. This is the
// REPL's arrangement (repl_tty.go), for the REPL's reasons.
//
// The second is where the output goes. A raw-mode terminal cannot take
// interleaved writes from a program that thinks it owns stdout, so `Reveal:`
// is captured rather than printed. That is the same choice `:visualize` makes,
// and it is why the output pane exists at all instead of the program simply
// printing. What is captured is now tailed live on the monitor screen
// (dev_monitor.go), so the capture no longer costs the reader the *timing* of
// the output — only its place on the terminal.
//
// The stepper is the same model `domain expansion: visualize` opens, hosted as
// an overlay — the precedent is repl_visualize.go. It records the *buffer*,
// which may never have been saved, so the recording carries its source with it
// rather than letting the source pane read whatever is on disk.
package main

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"domain/eval"
	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
	"domain/prims"
)

// devRunResult is what a finished run has to report.
type devRunResult struct {
	gen    int
	output string
	err    error
	// view is the recording, which is what the value bar, the timing gutter,
	// the stage walk and the stepper all read.
	view *traceView
	// interrupted distinguishes a run someone stopped from one that failed,
	// which are different things to say about a program.
	interrupted bool
}

type devRunDoneMsg struct{ result devRunResult }

// devOutput is the pane showing what the last run printed.
type devOutput struct {
	lines []string
	top   int
	// title says what happened, since an empty pane after a successful run and
	// an empty pane after a failure look identical.
	title string
	err   bool
}

// runProgram resolves the buffer and runs it off the event loop.
//
// The program is resolved from the buffer text and optimized, exactly as
// `domain run` would: the editor's job is to run the program you would get,
// not a special editor-only version of it.
//
// It records while it runs. The recorder is on the hot path of an instrumented
// run, but an AoC-scale program is microseconds either way, and one recording
// answers three questions at once: what each stage produced, where the time
// went, and what the stepper would walk. Running without recording would mean
// running twice to see any of it.
func (m devModel) runProgram() (tea.Model, tea.Cmd) {
	if m.running {
		return m, nil
	}
	text := m.buf.text()
	pipe, err := devResolveLocked(text, m.path)
	if err != nil {
		m.output = &devOutput{title: "cannot run", lines: wrapLines(err.Error(), m.width-2), err: true}
		return m, nil
	}

	rec := interp.NewRecorder(0)
	m.running = true
	// One trace chain does all four jobs a monitored run needs: the interrupter
	// can stop it, the counter knows which stage it is on, the live tracer
	// publishes where it is, and the recorder keeps it for the stepper
	// afterwards. Ordering matters only in that the interrupter is outermost —
	// a run is stopped after everything watching it has seen the step.
	live := &devLive{Inner: rec}
	prog := &progressCounter{Inner: live}
	prog.SetTotal(len(pipe.Nodes))
	m.interrupt = ir.NewInterrupter(prog)
	m.status = "running… ctrl+c stops it"

	// The output is a synchronized buffer rather than a strings.Builder because
	// the monitor tails it while the run is still writing: `Reveal:` output now
	// appears as it is printed, not only once there is nothing left to print.
	outBuf := &devOutBuf{}
	m.runSeq++
	var prev *devRunSummary
	if m.lastRun != nil {
		prev = m.lastRun.done
	}
	m.monitor = newDevMonitor(m.runSeq, m.path, live, outBuf, prog, prev)
	m.lastRun = m.monitor

	gen, interrupt, dir := m.gen, m.interrupt, m.baseDir()
	rewrites := optimizer.Optimize(clonePipeline(text, m.path), true)
	srcLines := append([]string(nil), m.buf.lines...)
	path := m.runPath()

	run := func() tea.Msg {
		// Evaluation writes the same package-level state resolution does, so a
		// run and a background analysis must not overlap.
		frontEndMu.Lock()
		defer frontEndMu.Unlock()

		defer eval.WatchApplications(rec.Applied)()
		defer prims.WatchForeignRuns(rec.ForeignRan)()

		ctx := &ir.Context{Stdout: outBuf, BaseDir: dir, Trace: interrupt}
		_, runErr := interp.Run(pipe, ctx)
		out := outBuf.String()
		return devRunDoneMsg{result: devRunResult{
			gen:         gen,
			output:      out,
			err:         runErr,
			interrupted: interrupt.Stopped(),
			view: &traceView{
				path: path, pipe: pipe, rec: rec, rewrites: rewrites,
				revealed: strings.TrimRight(out, "\n"),
				runErr:   runErr,
				srcLines: srcLines,
			},
		}}
	}
	return m, tea.Batch(run, m.spin.Tick, devSampleCmd(m.runSeq))
}

// openStepper hands the last recording to the stepper.
//
// Recording happens on every run, so this normally opens instantly over a
// recording that already exists rather than running the program a second time.
// With no recording yet it says so — running is a decision, and taking it on
// someone's behalf when they asked to look at a run is how a key ends up doing
// something surprising.
func (m devModel) openStepper() (tea.Model, tea.Cmd) {
	if m.running {
		return m, nil
	}
	if m.trace == nil {
		m.status = "nothing recorded yet — ctrl+r runs and records"
		return m, nil
	}
	m.stepper = newVisualModel(m.trace)
	return m, nil
}

// devResolveLocked resolves while holding the front-end lock, for callers on
// the event loop that are racing the background analysis.
func devResolveLocked(text, path string) (*ir.Pipeline, error) {
	frontEndMu.Lock()
	defer frontEndMu.Unlock()
	return devResolve(text, path)
}

// devResolve turns buffer text into a runnable pipeline, unoptimized.
func devResolve(text, path string) (*ir.Pipeline, error) {
	toks, err := lexer.Lex(text)
	if err != nil {
		return nil, err
	}
	prog, err := parser.Parse(text, toks)
	if err != nil {
		return nil, err
	}
	return prims.ResolveWith(prog, prims.FileOptions(path))
}

// clonePipeline resolves the same text a second time, so the optimizer has
// something of its own to rewrite and the recording stays the program that
// actually ran.
func clonePipeline(text, path string) *ir.Pipeline {
	pipe, err := devResolve(text, path)
	if err != nil {
		return &ir.Pipeline{}
	}
	return pipe
}

// baseDir is where a `Cursed Energy:` target is resolved from: beside the
// program, as it would be for `domain run`.
func (m devModel) baseDir() string {
	if m.path == "" {
		return "."
	}
	return filepath.Dir(m.path)
}

// runPath is the name a recording carries. An unsaved buffer has none, and
// "(unsaved)" is more honest than an empty string in the stepper's header.
func (m devModel) runPath() string {
	if m.path == "" {
		return "(unsaved)"
	}
	return m.path
}

// finishRun moves a completed run into the output pane, and closes the
// monitor's record of it.
//
// The monitor is finished before anything else is decided, including whether
// the result is still current: it is the report of a run that really happened,
// and a program edited underneath it does not make its measurements untrue.
func (m devModel) finishRun(res devRunResult) (tea.Model, tea.Cmd) {
	m.running = false
	m.interrupt = nil
	m.status = ""
	if m.monitor != nil {
		m.monitor.finish(res, time.Since(m.monitor.started))
	}
	if res.gen != m.gen {
		// The program changed while it ran; its output describes something
		// that is no longer on screen.
		return m, nil
	}

	// The recording outlives the pane: values, timings and the stepper all read
	// it long after the output has been dismissed.
	m.trace = res.view
	m.stages = devStages(res.view)

	out := &devOutput{lines: strings.Split(strings.TrimRight(res.output, "\n"), "\n")}
	switch {
	case res.interrupted:
		out.title, out.err = "stopped", true
	case res.err != nil:
		out.title, out.err = "failed", true
		out.lines = append(out.lines, "", res.err.Error())
	default:
		out.title = "ran"
	}
	if len(out.lines) == 1 && out.lines[0] == "" {
		out.lines = []string{"(no output — is there a `Reveal:` stage?)"}
	}
	m.output = out
	return m, nil
}

// explainPane shows what the optimizer did to the last run's program — the
// `--explain` output, against the program on screen.
//
// It is a list rather than annotations against the lines it changed, because
// `optimizer.Rewrite` carries a message and nothing else: there is no position
// on it to hang an annotation from. Inventing one by matching message text to
// source lines would be a guess presented as a fact, and the whole point of
// this pane is that the optimizer is telling you what it did.
func (m devModel) explainPane() (tea.Model, tea.Cmd) {
	if m.trace == nil {
		m.status = "nothing recorded yet — ctrl+r runs and records"
		return m, nil
	}
	if len(m.trace.rewrites) == 0 {
		m.output = &devOutput{
			title: "optimizer",
			lines: []string{"no optimizations applied — the pipeline runs as written"},
		}
		return m, nil
	}
	lines := make([]string, 0, len(m.trace.rewrites))
	for _, r := range m.trace.rewrites {
		lines = append(lines, "• "+r.Message)
	}
	m.output = &devOutput{title: "optimizer", lines: lines}
	return m, nil
}

// outputKey scrolls the pane, or closes it.
func (m devModel) outputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	h := m.outputHeight()
	last := max(0, len(m.output.lines)-h)
	switch msg.String() {
	case "down", "j":
		m.output.top = min(m.output.top+1, last)
	case "up", "k":
		m.output.top = max(m.output.top-1, 0)
	case "pgdown":
		m.output.top = min(m.output.top+h, last)
	case "pgup":
		m.output.top = max(m.output.top-h, 0)
	default:
		m.output = nil
	}
	return m, nil
}

// outputHeight is how much of the screen the pane takes: a third, so the
// program stays readable behind it.
func (m devModel) outputHeight() int { return max(3, m.height/3) }

// outputView draws the pane along the bottom of the screen.
func (m devModel) outputView() []string {
	o := m.output
	h := m.outputHeight()

	title := styHeading.Render(" " + o.title + " ")
	if o.err {
		title = styErr.Render(" " + o.title + " ")
	}
	hint := styDim.Render("  ↑/↓ scroll · any other key closes")
	rows := []string{truncateVis(title+hint, m.width)}

	for i := o.top; i < min(o.top+h-1, len(o.lines)); i++ {
		rows = append(rows, truncateVis("  "+o.lines[i], m.width))
	}
	if o.top+h-1 < len(o.lines) {
		rows = append(rows, styDim.Render("  ↓ more"))
	}
	return rows
}

// wrapLines breaks a message to the window width, so a long resolver error
// does not run off the side of the pane.
func wrapLines(s string, width int) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		for len(line) > width && width > 0 {
			out = append(out, line[:width])
			line = line[width:]
		}
		out = append(out, line)
	}
	return out
}

// ---------------------------------------------------------------------------
// choosing the input
// ---------------------------------------------------------------------------

// bindInput points the program's `Cursed Energy:` stage at a file, rewriting
// the source line. Binding it in the program rather than holding it beside the
// program is deliberate: the file is part of what the program *is*, and a
// binding that lived only in the editor would make the program behave
// differently here than under `domain run`.
func (m devModel) bindInput(path string) (devModel, bool) {
	rel := path
	if base := m.baseDir(); base != "." {
		if r, err := filepath.Rel(base, path); err == nil {
			rel = r
		}
	}
	m.input = rel

	for i, line := range m.buf.lines {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(strings.ToLower(trimmed), "cursed energy:") {
			continue
		}
		indent := line[:len(line)-len(trimmed)]
		m.undo.record(m.buf, false, m.now())
		m.buf.lines[i] = indent + "Cursed Energy: " + rel
		m.dirty = true
		m.status = "input: " + rel
		return m, true
	}

	// No source stage yet: add one at the top, which is where it has to be.
	m.undo.record(m.buf, false, m.now())
	m.buf.lines = append([]string{"Cursed Energy: " + rel}, m.buf.lines...)
	m.buf.row, m.buf.col = 0, 0
	m.dirty = true
	m.status = "input: " + rel + " (added a source stage)"
	return m, true
}

// inputPickerDir is where the input browser opens: beside the program.
func (m devModel) inputPickerDir() string { return m.baseDir() }
