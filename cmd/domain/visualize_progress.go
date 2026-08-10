// Saying that a long program is still running.
//
// `domain expansion: visualize` runs the program to completion before it shows
// anything. That is the model and it is the right one — a recording you can
// walk backwards through beats a live trace that has already scrolled past —
// but it has a cost the tool used to make the reader pay silently: a program
// that takes twenty seconds leaves a blank terminal for twenty seconds, which
// is indistinguishable from a hang.
//
// So the recorder reports how far it has got (interp.Recorder.OnProgress) and
// this prints it, on one line that rewrites itself and is erased before the UI
// takes the screen. It goes to *stderr*, so `--json` and `--plain` stay
// pipeable, and it prints nothing at all when stderr is not a terminal: a CI
// log does not want three hundred progress lines, and a file does not want
// carriage returns in it.
package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"domain/interp"
)

// progressAfter is how long a run has to have been going before anything is
// printed. Nearly every program finishes inside it, and a line that appears and
// vanishes within a frame is worse than no line.
const progressAfter = 400 * time.Millisecond

// progressEvery is how often the line is rewritten once it is showing.
const progressRedraw = 100 * time.Millisecond

// progressPrinter draws the "still running" line.
type progressPrinter struct {
	w       io.Writer
	enabled bool
	shown   bool
	last    time.Time
	width   int // how wide the last line was, so it can be erased exactly
}

// newProgressPrinter returns a printer that draws to w when w is a terminal,
// and one that does nothing otherwise.
func newProgressPrinter(w io.Writer) *progressPrinter {
	return &progressPrinter{w: w, enabled: isColorTerminal(w)}
}

// report is the interp.Recorder progress hook.
func (p *progressPrinter) report(pr interp.Progress) {
	if !p.enabled || pr.Elapsed < progressAfter {
		return
	}
	if p.shown && time.Since(p.last) < progressRedraw {
		return
	}
	p.last = time.Now()
	p.shown = true

	line := fmt.Sprintf("  recording %s · %s elapsed",
		plural(pr.Steps, "step"), interp.FormatDuration(pr.Elapsed))
	if pr.Capped {
		// Only failures are being kept now, and the reader is about to be shown
		// a partial recording; better they learn it while there is still time
		// to stop and re-run than from the header afterwards.
		line += " · capped, still running"
	}
	// Pad to the previous width so a line that shrinks does not leave the tail
	// of the last one behind it.
	if n := len([]rune(line)); n < p.width {
		line += strings.Repeat(" ", p.width-n)
	} else {
		p.width = n
	}
	fmt.Fprintf(p.w, "\r%s", line)
}

// done erases the line, leaving the terminal as it was found. It is safe to
// call whether or not anything was ever drawn.
func (p *progressPrinter) done() {
	if !p.shown {
		return
	}
	fmt.Fprintf(p.w, "\r%s\r", strings.Repeat(" ", p.width))
	p.shown, p.width = false, 0
}
