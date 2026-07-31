// `:visualize` — the step-by-step run visualizer, over the session's own
// program, without leaving the session.
//
// `domain expansion: visualize <file>` records a run and opens a stepper over
// the recording. Everything that costs is already in this binary: the recorder
// (interp), the tree/value/timing panes (visualize_tui.go). What the REPL was
// missing is the handoff — a session's program is not a file, so the command
// could not be pointed at it.
//
// So the session records its own program and hands the recording to the same
// model, as an overlay. Step through it, press q, and the prompt is where it
// was. A piped session gets the plain text trace instead, which is exactly
// what `--plain` prints, for the same reason: there is nothing to drive.
//
// The recording is of the program *as the session runs it* — unoptimized —
// because that is the program being built, statement by statement, and a
// stepper showing fused stages the user never typed would be answering a
// different question. The rewrites the optimizer *would* apply are collected
// separately, so the explain pane still has something to say.
package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"domain/interp"
	"domain/ir"
	"domain/optimizer"
)

// visualize records the session's program and leaves the recording where the
// editor can find it. Without a terminal to drive, it prints the trace.
func (r *repl) visualize() {
	if len(r.stmts) == 0 {
		fmt.Fprintln(r.out, "(empty domain)")
		return
	}
	pipe, src, err := r.frontEnd(r.stmts)
	if err != nil {
		r.reportError(src, err)
		return
	}
	if len(pipe.Nodes) == 0 {
		fmt.Fprintln(r.out, "(no value yet)")
		return
	}

	rec := interp.NewRecorder(0)
	ctx := r.context()
	// The program's own Reveal output is captured rather than printed: a
	// raw-mode terminal cannot take interleaved writes, and the trace is the
	// point.
	var revealed strings.Builder
	ctx.Stdout = &revealed
	// Keep the session's interrupter in the chain, so recording a runaway loop
	// can still be stopped.
	if it, ok := r.trace.(*ir.Interrupter); ok && it != nil {
		prev := it.Inner
		it.Inner = rec
		defer func() { it.Inner = prev }()
		ctx.Trace = it
	} else {
		ctx.Trace = rec
	}

	_, runErr := interp.Run(pipe, ctx)

	r.lastTrace = &traceView{
		path: "repl",
		// The session's own pipeline, so the code pane can compile it: the
		// program being built is exactly the one worth asking that about.
		pipe:     pipe,
		rec:      rec,
		rewrites: r.rewritesFor(),
		revealed: strings.TrimRight(revealed.String(), "\n"),
		runErr:   runErr,
	}
	if !r.interactive {
		r.lastTrace.writePlain(r.out)
		r.lastTrace = nil
	}
}

// rewritesFor is what the optimizer would do to this program, for the explain
// pane. It optimizes a *separate* resolve, so the recording above stays the
// program the session actually runs.
func (r *repl) rewritesFor() []optimizer.Rewrite {
	pipe, _, err := r.frontEnd(r.stmts)
	if err != nil {
		return nil
	}
	return optimizer.Optimize(pipe, true)
}

// takeTrace hands the editor the recording just made, once.
func (r *repl) takeTrace() *traceView {
	tv := r.lastTrace
	r.lastTrace = nil
	return tv
}

// stepperQuit converts the embedded stepper's "quit" into "close the overlay".
//
// The stepper does not know it is embedded: q and Esc-at-the-top return
// tea.Quit, which in a session would end the session. Every command it returns
// is one of those quits — it starts no work of its own — so running one here
// to look at it is safe, and anything that is not a quit is passed along
// untouched.
func stepperQuit(cmd tea.Cmd) (quit bool, passthrough tea.Cmd) {
	if cmd == nil {
		return false, nil
	}
	msg := cmd()
	if _, isQuit := msg.(tea.QuitMsg); isQuit {
		return true, nil
	}
	return false, func() tea.Msg { return msg }
}
