// How far along a replay is.
//
// The REPL re-runs the whole program on every statement, so "is this going to
// take all day" is a question with an answer: the pipeline has a known number
// of top-level stages, and the trace hook reports each one as it finishes.
// Only depth-0 steps count — the same attribution `--stats` uses — because a
// loop with four hundred iterations is one stage that takes a while, not four
// hundred units of progress.
//
// The number goes two places: a bar beside the spinner, drawn with the same
// helper `:stats` draws its profile with, and the terminal's own progress
// indicator (View.ProgressBar), which terminals that support it show on the
// tab or in the taskbar — so a long replay is visible from a window that is
// not on top.
package main

import (
	"sync/atomic"

	"domain/ir"
)

// progressCounter is a tracer that counts finished top-level stages. It is
// read from the event loop while the evaluation goroutine writes it, so both
// fields are atomic.
type progressCounter struct {
	total atomic.Int64
	done  atomic.Int64
	// Inner is another tracer to feed — `:stats` installs its aggregator
	// here, so a profiled run still reports progress.
	Inner ir.Tracer
}

// SetTotal records how many top-level stages the run has, once the program has
// been resolved and the count is known.
func (p *progressCounter) SetTotal(n int) { p.total.Store(int64(n)) }

// Reset clears the counts for a new run.
func (p *progressCounter) Reset() {
	p.total.Store(0)
	p.done.Store(0)
}

// Step counts a finished top-level stage.
func (p *progressCounter) Step(e ir.StepEvent) {
	if p.Inner != nil {
		p.Inner.Step(e)
	}
	if e.Depth == 0 {
		p.done.Add(1)
	}
}

func (p *progressCounter) PushFrame(label string) {
	if p.Inner != nil {
		p.Inner.PushFrame(label)
	}
}

func (p *progressCounter) PopFrame() {
	if p.Inner != nil {
		p.Inner.PopFrame()
	}
}

// Counts reports finished and total stages. total is 0 until the program has
// resolved, which is where a run spends its first moments.
func (p *progressCounter) Counts() (done, total int) {
	return int(p.done.Load()), int(p.total.Load())
}

// Percent is the share of the run's stages that have finished, and whether
// that share means anything yet.
func (p *progressCounter) Percent() (int, bool) {
	done, total := p.Counts()
	if total <= 0 {
		return 0, false
	}
	if done > total {
		done = total // a Channel body can report past the count; never overflow
	}
	return done * 100 / total, true
}
