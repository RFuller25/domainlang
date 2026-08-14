// Painting the run monitor.
//
// The screen is a column of sections, drawn in the order they answer the
// questions a run raises: is it going, how hard is it working, where is it,
// what has it got, what has it said. Nothing floats and nothing scrolls — a
// number that moved because the layout moved is a number you have to re-find
// every tenth of a second.
//
// Everything the run is doing is drawn at whatever height the terminal has,
// which is what the section sizes below are for: the charts lose rows first,
// the source context and the output tail lose lines after that, and the state
// line and the footer are never dropped, because they are the two rows that
// say what is happening and how to leave.
package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"domain/interp"
)

// monitorView draws the whole screen.
func (m devModel) monitorView() string {
	d := m.monitor
	w := max(20, m.width)
	rows := []string{m.monitorTitle(w), styRule.Render(strings.Repeat("─", w))}

	chartH, context, tailN := monitorSizes(m.height)

	rows = append(rows, m.monitorState(w))
	if banner := m.monitorBanner(w); banner != "" {
		rows = append(rows, banner)
	}
	rows = append(rows, "")
	rows = append(rows, m.monitorCharts(w, chartH)...)
	rows = append(rows, "")
	rows = append(rows, m.monitorTotals(w)...)
	rows = append(rows, "")
	rows = append(rows, m.monitorWhere(w, context)...)
	rows = append(rows, "")
	rows = append(rows, m.monitorValue(w))
	if tailN > 0 {
		if out := d.out.tail(tailN); len(out) > 0 {
			rows = append(rows, "", styHeading.Render(" output ")+styDim.Render("  as it is printed"))
			for _, line := range out {
				rows = append(rows, truncateVis("  "+line, w))
			}
		}
	}
	if d.done != nil {
		rows = append(rows, m.monitorHot(w)...)
	}

	// The footer is pinned to the bottom, so the key that leaves is in the same
	// place whether the run is two lines long or twenty.
	body := max(1, m.height-1)
	if len(rows) > body {
		rows = rows[:body]
	}
	for len(rows) < body {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n") + "\n" + m.monitorFooter(w)
}

// monitorSizes decides what each section gets on a terminal of this height:
// the chart rows, the source lines either side of the current one, and the
// lines of output tail.
func monitorSizes(height int) (chartH, context, tail int) {
	switch {
	case height >= 34:
		return 4, 2, 6
	case height >= 28:
		return 3, 2, 4
	case height >= 22:
		return 2, 1, 3
	case height >= 16:
		return 2, 1, 0
	default:
		return 1, 0, 0
	}
}

// monitorTitle names the screen and the program being watched.
func (m devModel) monitorTitle(w int) string {
	left := styTitle.Render("domain expansion: development") + styDim.Render("  run monitor")
	name := m.monitor.path
	if name == "" {
		name = "(unsaved)"
	} else {
		name = filepath.Base(name)
	}
	right := styDim.Render(name + " ")
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return truncateVis(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// monitorState is the line that says whether the run is going, and how far it
// has got. It is the one row that is always true right now, so it is the one
// row the layout never moves.
func (m devModel) monitorState(w int) string {
	d := m.monitor
	var left string
	if d.done == nil {
		left = m.spin.View() + " " + styHeading.Render("running") + "  " +
			interp.FormatDuration(time.Since(d.started))
	} else {
		style := styHeading
		if d.done.failed {
			style = styErr
		}
		left = "  " + style.Render(d.done.outcome) + "  " + interp.FormatDuration(d.done.elapsed)
	}
	if d.steps > 0 {
		left += styDim.Render("  ·  " + formatCount(d.steps) + " steps")
		elapsed := time.Since(d.started)
		if d.done != nil {
			elapsed = d.done.elapsed
		}
		if rate := formatRate(d.steps, elapsed); rate != "" {
			left += styDim.Render("  ·  " + rate)
		}
	}

	right := ""
	if done, total := d.prog.Counts(); total > 0 {
		if done > total {
			done = total
		}
		pct := float64(done) * 100 / float64(total)
		right = styDim.Render(fmt.Sprintf("stage %d/%d ", done, total)) +
			styHeading.Render(bars(pct, 12)) + " "
	}

	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return truncateVis(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// monitorBanner is the one thing, if any, the screen has to interrupt itself
// to say: that a stop has been asked for and not yet landed, that no node has
// been evaluated for a while, or that the recording has stopped keeping up.
func (m devModel) monitorBanner(w int) string {
	d := m.monitor
	switch {
	case d.stopping && d.done == nil:
		since, stuck := d.stalled()
		msg := "  stopping — a run stops at its next step boundary"
		if stuck {
			// The honest version of "why is ctrl+c doing nothing": one
			// primitive is not interruptible, and it has to finish first.
			msg = fmt.Sprintf("  stopping — nothing has stepped for %s; a primitive must finish first",
				interp.FormatDuration(since))
		}
		return styErr.Render(truncateVis(msg, w))
	case d.done == nil:
		if since, stuck := d.stalled(); stuck {
			return styDim.Render(truncateVis(
				fmt.Sprintf("  no step in %s — one primitive is taking all of it", interp.FormatDuration(since)), w))
		}
	case d.truncated:
		return styDim.Render(truncateVis(
			"  the recording hit its cap — the stepper will show part of this run, not all of it", w))
	}
	return ""
}

// monitorCharts draws memory and CPU over the run, on one shared time axis.
func (m devModel) monitorCharts(w, h int) []string {
	d := m.monitor
	cw := max(8, w-4)

	heap := resample(d.heapSeries(), cw)
	cpu := resample(d.cpuSeries(), cw)
	top := maxOf(heap)

	var last devSample
	if n := len(d.samples); n > 0 {
		last = d.samples[n-1]
	}

	rows := []string{m.monitorChartHead(w, "heap in use",
		formatBytes(last.heap), "peak "+formatBytes(d.peakHeap))}
	for _, r := range sparkRows(heap, h, top) {
		rows = append(rows, "  "+styHeading.Render(r))
	}
	// The CPU chart is always scaled to one core, not to its own peak: a run
	// that used 4% of a core and a run that pinned one look nothing alike, and
	// autoscaling would draw them identically.
	rows = append(rows, m.monitorChartHead(w, "cpu, share of one core",
		fmt.Sprintf("%.0f%%", last.cpu), "process, this editor included"))
	for _, r := range sparkRows(cpu, h, 100) {
		rows = append(rows, "  "+styType.Render(r))
	}
	rows = append(rows, m.monitorRuler(w, cw))
	return rows
}

// monitorChartHead labels a chart: what it is on the left, what it reads right
// now and what to make of it on the right.
func (m devModel) monitorChartHead(w int, label, now, note string) string {
	left := "  " + styLabel.Render(label) + "  " + styValue.Render(now)
	right := styDim.Render(note + " ")
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return truncateVis(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// monitorRuler is the time axis both charts share.
func (m devModel) monitorRuler(w, cw int) string {
	d := m.monitor
	end := time.Since(d.started)
	if d.done != nil {
		end = d.done.elapsed
	}
	right := interp.FormatDuration(end)
	line := "  0s"
	if pad := cw - 2 - len(right); pad > 0 {
		line += strings.Repeat(" ", pad) + right
	}
	return styDim.Render(truncateVis(line, w))
}

// monitorTotals is what the run has spent that a curve cannot show: everything
// it allocated, what the collector did about it, and how the last run compares.
func (m devModel) monitorTotals(w int) []string {
	d := m.monitor
	var last devSample
	if n := len(d.samples); n > 0 {
		last = d.samples[n-1]
	}
	line := fmt.Sprintf("  %s allocated in total  ·  %s  ·  %s paused for collection",
		formatBytes(last.allocated), plural(int(last.gc), "GC cycle"), interp.FormatDuration(last.pause))
	rows := []string{styDim.Render(truncateVis(line, w))}

	if p := d.prev; p != nil {
		elapsed := time.Since(d.started)
		if d.done != nil {
			elapsed = d.done.elapsed
		}
		// The comparison is against the last run of this session, whatever was
		// edited in between — which is the comparison being asked for: it is the
		// edit that is on trial, not the program.
		cmpLine := fmt.Sprintf("  last run  %s %s  ·  peak %s %s  ·  %s steps",
			interp.FormatDuration(p.elapsed), deltaAgainst(elapsed.Seconds(), p.elapsed.Seconds()),
			formatBytes(p.peakHeap), deltaAgainst(float64(d.peakHeap), float64(p.peakHeap)),
			formatCount(p.steps))
		rows = append(rows, styDim.Render(truncateVis(cmpLine, w)))
	}
	return rows
}

// monitorWhere shows the line the run is on, in the program around it.
//
// The position is the last node evaluation to report, not a guess: a run that
// is inside one long primitive keeps showing where that primitive was called
// from, which is the true answer to where it is.
func (m devModel) monitorWhere(w, context int) []string {
	d := m.monitor
	head := "  " + styLabel.Render("where it is")
	if d.snap != nil && d.snap.frame != "" {
		head += styDim.Render("  "+d.snap.frame) +
			styDim.Render(fmt.Sprintf("  ·  depth %d", d.snap.depth))
	}
	rows := []string{truncateVis(head, w)}

	if d.snap == nil || d.snap.line <= 0 {
		note := "  (nothing has reported a position in this program yet)"
		if d.snap != nil {
			// A node with no line here is one inlined from a Shikigami or the
			// prelude: it is real, it is running, and its position is in
			// somebody else's file.
			note = "  (inside a definition from another file)"
		}
		return append(rows, styDim.Render(truncateVis(note, w)))
	}
	if context < 0 {
		return rows
	}

	cur := d.snap.line - 1
	width := len(fmt.Sprint(len(m.buf.lines)))
	for i := max(0, cur-context); i <= min(len(m.buf.lines)-1, cur+context); i++ {
		marker := "   "
		if i == cur {
			marker = styMarker.Render(" → ")
		}
		num := styDim.Render(pad(fmt.Sprint(i+1), width) + " │ ")
		rows = append(rows, truncateVis(marker+num+paintLine(m.buf.lines[i], noDecor()), w))
	}
	return rows
}

// monitorValue is what the last reported step produced — the REPL's own
// `=> value : Type`, against a run that is still going.
func (m devModel) monitorValue(w int) string {
	d := m.monitor
	if d.snap == nil || d.snap.short == "" {
		return styDim.Render("  (no value yet)")
	}
	label := styHeading.Render(" => ")
	tail := ""
	if d.snap.typ != "" {
		tail = styType.Render(" : " + d.snap.typ)
	}
	room := w - ansi.StringWidth(label) - ansi.StringWidth(tail) - 1
	if room < 8 {
		return truncateVis(" "+label+d.snap.short, w)
	}
	return " " + label + truncateVis(d.snap.short, room) + tail
}

// monitorHot ranks the lines the finished run spent itself on. It is drawn
// only at the end, because a share of a run that is still going is a share of
// nothing yet.
func (m devModel) monitorHot(w int) []string {
	d := m.monitor
	if len(d.hot) == 0 {
		return nil
	}
	rows := []string{"", "  " + styLabel.Render("where the time went")}
	width := len(fmt.Sprint(len(m.buf.lines)))
	for _, h := range d.hot {
		text := ""
		if h.line-1 < len(m.buf.lines) {
			text = strings.TrimSpace(m.buf.lines[h.line-1])
		}
		rows = append(rows, truncateVis(fmt.Sprintf("   %s %s %s",
			styDim.Render("line "+pad(fmt.Sprint(h.line), width)),
			heat(h.pct, true).Render(fmt.Sprintf("%5.1f%%", h.pct)),
			styLabel.Render(text)), w))
	}
	return rows
}

// monitorFooter is the key line: what stops a run, and what leaves the screen
// once there is nothing to stop.
func (m devModel) monitorFooter(w int) string {
	if m.monitor.done == nil {
		return truncateVis(styKey.Render("  ctrl+c")+styDim.Render(" stops the run"), w)
	}
	parts := []string{
		styKey.Render("  ctrl+t") + styDim.Render(" stepper"),
		styKey.Render("ctrl+r") + styDim.Render(" run again"),
		styKey.Render("any other key") + styDim.Render(" back to the program"),
	}
	return truncateVis(strings.Join(parts, styDim.Render("  ·  ")), w)
}
