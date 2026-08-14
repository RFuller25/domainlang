// The run monitor: what a program is doing while it does it.
//
// `ctrl+r` used to run the program behind the editor — a spinner in the status
// line, and everything the run had to say arriving at the end. That is fine for
// a program that finishes in three milliseconds and no use at all for the one
// you actually want to watch: the `While` loop that has been going for a
// minute, the stage that allocates four gigabytes, the pipeline you are trying
// to make fast. So a run takes the screen and gives it back when you press a
// key.
//
// Where each number comes from, and what it costs:
//
//   - **Where it is.** The trace hook that every node evaluation already passes
//     through (ir.EvalNode) reports the node, its depth and its enclosing
//     frame. devLive rides that chain between the interrupter and the recorder,
//     so the position is the run's own rather than a guess made from outside.
//
//   - **The current value.** Rendering it is the expensive part, so it is not
//     done per step: the event loop raises a flag and the *next* step to report
//     renders one value and lowers it. Ten renderings a second rather than two
//     million, and one atomic load on the hot path — which matters, because
//     BenchmarkTracedVsUntraced is what keeps an untraced `domain run` honest.
//
//   - **Memory.** runtime.ReadMemStats, ten times a second. It costs ~52µs a
//     call and briefly stops the world, where runtime/metrics costs 825ns — but
//     the cheap reading only advances at a GC, which turns a memory curve into
//     a sawtooth of GC points, and the curve is the thing being asked for.
//     0.05% of the run is the right price for it.
//
//   - **CPU.** runtime/metrics, which is the only one of the two that accounts
//     for it: busy CPU-seconds (total minus idle) over the wall time between
//     samples, as a share of one core. Those counters *do* only advance at a
//     GC, so a sample that has learnt nothing new carries the last reading
//     forward rather than drawing a zero the process never spent.
//
// Both are the *process's* numbers, this editor's own painting included, and
// the screen says so rather than implying an isolation the Go runtime cannot
// give: there is no way to ask it what one goroutine's heap is, and a number
// that pretends otherwise is worse than no number.
package main

import (
	"cmp"
	"fmt"
	"runtime"
	"runtime/metrics"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"domain/ir"
)

// devSampleEvery is how often the monitor takes a reading. Ten a second is
// faster than the eye reads a changing number and slow enough that the cost of
// reading one never shows up in what is being measured.
const devSampleEvery = 100 * time.Millisecond

// devMaxSamples is how many readings the history holds before it halves. The
// chart always covers the whole run — a monitor that scrolled off the start
// would answer "when did it get slow" with the last few seconds of a run that
// went wrong a minute ago.
const devMaxSamples = 512

// devStuckAfter is how long without a single node evaluation before the screen
// stops implying the position it is showing is current. A run inside one long
// primitive reports nothing at all until that primitive returns, which is also
// exactly why ctrl+c can look ignored.
const devStuckAfter = 1500 * time.Millisecond

// ---------------------------------------------------------------------------
// what a run reports about itself
// ---------------------------------------------------------------------------

// devSample is one reading of the run.
type devSample struct {
	at        time.Duration // since the run started
	heap      uint64        // bytes of live heap, this process
	allocated uint64        // bytes allocated since the run started
	gc        uint32        // GC cycles since the run started
	pause     time.Duration // time stopped for GC since the run started
	cpu       float64       // percent of one core over the interval before this
	steps     int
}

// devLiveValue is where the run had got to when it was last asked.
//
// It is one snapshot rather than a set of atomic fields because the parts have
// to agree: a line from one step and a value from the next is a reading that
// was never true.
type devLiveValue struct {
	line  int    // 1-based, in the buffer being edited; 0 when the node is not from it
	frame string // the innermost enclosing frame, e.g. `Repeat 4 iter 27/100`
	depth int
	short string
	typ   string
	at    time.Time
}

// devLive is a Tracer that publishes where a run has got to.
//
// It sits between the interrupter and the recorder, so one chain records the
// run, reports it, and can stop it. Everything it does per step is a counter
// increment and an atomic load; the rendering that would actually cost
// something happens only when the event loop has asked for it.
type devLive struct {
	Inner ir.Tracer

	steps atomic.Int64
	want  atomic.Bool
	value atomic.Pointer[devLiveValue]
}

func (l *devLive) Step(e ir.StepEvent) {
	if l.Inner != nil {
		l.Inner.Step(e)
	}
	l.steps.Add(1)
	if !l.want.Load() {
		return
	}
	// CompareAndSwap rather than a plain store: two goroutines never run a
	// program here, but the flag is written from the event loop and cleared
	// here, and the swap is what makes "this snapshot answers that request"
	// true rather than nearly true.
	if !l.want.CompareAndSwap(true, false) {
		return
	}
	v := &devLiveValue{frame: e.Frame, depth: e.Depth, short: ir.FormatShort(e.Out), at: time.Now()}
	if e.Node != nil {
		if line, ok := devLineOf(e.Node); ok {
			v.line = line
		}
		if e.Node.Out != nil {
			v.typ = e.Node.Out.String()
		}
	}
	if e.Err != nil {
		v.short = e.Err.Error()
	}
	l.value.Store(v)
}

func (l *devLive) PushFrame(label string, out *ir.Type) {
	if l.Inner != nil {
		l.Inner.PushFrame(label, out)
	}
}

func (l *devLive) PopFrame(out ir.Value) {
	if l.Inner != nil {
		l.Inner.PopFrame(out)
	}
}

// ask requests a snapshot from the next step to report.
func (l *devLive) ask() { l.want.Store(true) }

// snapshot is the last position published, or nil before the first one.
func (l *devLive) snapshot() *devLiveValue { return l.value.Load() }

// devOutBuf is what a monitored run prints into: a buffer the run's goroutine
// writes and the event loop reads, which is the whole reason it is not the
// strings.Builder a run used to write into. `Reveal:` output now appears while
// it is being printed rather than only once the run is over.
type devOutBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (o *devOutBuf) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.Write(p)
}

func (o *devOutBuf) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

// tail is the last n lines printed, for the pane that shows output as it
// arrives. The end is what a live tail is for: the beginning is still there in
// the output pane when the run is over.
func (o *devOutBuf) tail(n int) []string {
	s := strings.TrimRight(o.String(), "\n")
	if s == "" || n <= 0 {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// devRunSummary is what a finished run leaves behind for the next one to be
// compared against.
type devRunSummary struct {
	outcome  string // "ran", "failed", "stopped"
	failed   bool
	elapsed  time.Duration
	peakHeap uint64
	steps    int
}

// devHotLine is one of the lines that took the most of a finished run.
type devHotLine struct {
	line int
	pct  float64
}

// ---------------------------------------------------------------------------
// the monitor
// ---------------------------------------------------------------------------

// devMonitor is the screen's state: the readings, the position, and what the
// run finally did.
type devMonitor struct {
	// seq identifies the run. Two runs of an unedited buffer share a buffer
	// generation, so the generation cannot be what a stray tick is checked
	// against — this can.
	seq     int
	path    string
	started time.Time
	live    *devLive
	out     *devOutBuf
	prog    *progressCounter

	samples []devSample
	// every is the spacing the history currently holds, which doubles each time
	// it is halved. The time ruler reads it, so a compacted history still says
	// how long the run has been going.
	every time.Duration

	peakHeap  uint64
	baseAlloc uint64 // TotalAlloc when the run began: readings are this run's
	baseGC    uint32
	basePause uint64
	cpu       cpuMeter

	snap     *devLiveValue
	steps    int
	lastStep time.Time // when the step count last moved, for a run inside one primitive

	stopping bool

	// done is nil while the run is going and the summary afterwards, which is
	// the one flag the screen's keys and its footer both turn on.
	done *devRunSummary
	// prev is the previous run's summary, so a program being made faster can be
	// compared against itself without leaving the screen.
	prev *devRunSummary
	hot  []devHotLine
	// truncated says the recording hit its cap: the run carried on and the
	// stepper afterwards will be partial, which is better learnt now.
	truncated bool
}

// newDevMonitor starts a monitor for a run about to begin.
func newDevMonitor(seq int, path string, live *devLive, out *devOutBuf, prog *progressCounter, prev *devRunSummary) *devMonitor {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m := &devMonitor{
		seq: seq, path: path, started: time.Now(), live: live, out: out, prog: prog,
		every:     devSampleEvery,
		baseAlloc: ms.TotalAlloc, baseGC: ms.NumGC, basePause: ms.PauseTotalNs,
		prev: prev,
	}
	m.lastStep = m.started
	m.cpu.reset()
	// One reading straight away, so the screen the run opens has something on
	// it rather than a tenth of a second of empty charts.
	m.sample()
	return m
}

// devSampleMsg asks the monitor for a reading. It names the run, so a tick
// left over from one that has ended cannot append to the next one — or, worse,
// schedule a second stream of ticks alongside it.
type devSampleMsg struct{ seq int }

// devSampleCmd schedules the next reading.
func devSampleCmd(seq int) tea.Cmd {
	return tea.Tick(devSampleEvery, func(time.Time) tea.Msg { return devSampleMsg{seq: seq} })
}

// sample takes one reading and appends it, collecting whatever the live tracer
// has published since the last one.
func (d *devMonitor) sample() {
	now := time.Now()
	elapsed := now.Sub(d.started)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	// The interval is measured rather than assumed: a tick that arrived late
	// covers more time than it was scheduled for, and the CPU share is a rate.
	interval := elapsed
	if n := len(d.samples); n > 0 {
		interval = elapsed - d.samples[n-1].at
	}

	s := devSample{
		at:        elapsed,
		heap:      ms.HeapAlloc,
		allocated: ms.TotalAlloc - d.baseAlloc,
		gc:        ms.NumGC - d.baseGC,
		pause:     time.Duration(ms.PauseTotalNs - d.basePause),
		cpu:       d.cpu.percent(interval),
		steps:     int(d.live.steps.Load()),
	}
	if s.heap > d.peakHeap {
		d.peakHeap = s.heap
	}
	if s.steps != d.steps {
		d.steps, d.lastStep = s.steps, now
	}
	if v := d.live.snapshot(); v != nil && (d.snap == nil || v.at.After(d.snap.at)) {
		d.snap = v
	}
	d.live.ask()

	d.samples = append(d.samples, s)
	d.compact()
}

// compact halves a full history, keeping the heavier of each pair.
//
// Averaging would flatten exactly what the chart is read for — the spike that
// preceded the slowdown — so each pair is represented by the reading that
// actually happened, the one with more heap on it. The spacing doubles, and
// the ruler under the chart says so.
func (d *devMonitor) compact() {
	if len(d.samples) < devMaxSamples {
		return
	}
	kept := d.samples[:0:0]
	for i := 0; i+1 < len(d.samples); i += 2 {
		a, b := d.samples[i], d.samples[i+1]
		if b.heap > a.heap {
			a = b
		}
		kept = append(kept, a)
	}
	if len(d.samples)%2 == 1 {
		kept = append(kept, d.samples[len(d.samples)-1])
	}
	d.samples, d.every = kept, d.every*2
}

// finish closes the monitor over a run that has ended, taking one last reading
// so the screen's numbers describe the whole run rather than stopping a tenth
// of a second short of it.
func (d *devMonitor) finish(res devRunResult, elapsed time.Duration) {
	d.sample()
	last := d.samples[len(d.samples)-1]

	sum := &devRunSummary{
		outcome: "ran", elapsed: elapsed, peakHeap: d.peakHeap, steps: last.steps,
	}
	switch {
	case res.interrupted:
		sum.outcome, sum.failed = "stopped", true
	case res.err != nil:
		sum.outcome, sum.failed = "failed", true
	}
	d.done = sum

	if res.view != nil {
		d.truncated = res.view.rec != nil && res.view.rec.Truncated()
		d.hot = hotLines(res.view.lineShares())
	}
}

// hotLines is the few lines that took the most of the run, worst first. Three,
// because the question a finished run is read for is which line to look at
// next, and a ranking long enough to need reading has not answered it.
func hotLines(shares map[int]float64) []devHotLine {
	var out []devHotLine
	for line, pct := range shares {
		if pct >= devHeatFloor {
			out = append(out, devHotLine{line: line, pct: pct})
		}
	}
	// Worst first, ties broken by line, so the same run always ranks the same
	// way however the shares happened to be walked.
	slices.SortFunc(out, func(a, b devHotLine) int {
		if c := cmp.Compare(b.pct, a.pct); c != 0 {
			return c
		}
		return cmp.Compare(a.line, b.line)
	})
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// stalled reports how long the run has gone without evaluating a node, and
// whether that is long enough to be worth saying. A run inside one long
// primitive publishes nothing — which is the same reason ctrl+c can look
// ignored, and is worth saying out loud in both cases.
func (d *devMonitor) stalled() (time.Duration, bool) {
	if d.done != nil {
		return 0, false
	}
	since := time.Since(d.lastStep)
	return since, since >= devStuckAfter
}

// ---------------------------------------------------------------------------
// the keyboard
// ---------------------------------------------------------------------------

// monitorKey is every key while the monitor is up.
//
// Two modes, and the run itself decides which: while it is going the only key
// that means anything is the one that stops it — there is nothing else to do
// to a running program, and a keystroke that dismissed the screen mid-run
// would hide the thing it was opened to show. Once it has finished the screen
// is a report, and a report is dismissed by any key that is not asking for
// something better: the stepper over the same run, or another run.
func (m devModel) monitorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.monitor
	if d.done == nil {
		if msg.String() == "ctrl+c" {
			m.interrupt.Stop()
			d.stopping = true
			m.status = "stopping…"
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Visualize):
		m.monitor = nil
		return m.openStepper()
	case key.Matches(msg, m.keys.Run):
		return m.runProgram()
	}
	m.monitor = nil
	return m, nil
}

// ---------------------------------------------------------------------------
// CPU
// ---------------------------------------------------------------------------

// cpuMeter reads the Go runtime's own CPU accounting.
//
// The counters are cumulative CPU-seconds across every P, and they advance
// only at a GC — so a reading that has learnt nothing new repeats the last one
// rather than drawing a zero. Reporting a share of *one* core rather than of
// all of them is deliberate: the interpreter is single-threaded, so 100% is
// what a program that is running looks like, on a four-core machine and on a
// sixty-four-core one alike.
type cpuMeter struct {
	samples   [2]metrics.Sample
	lastTotal float64
	lastIdle  float64
	last      float64
	started   bool
}

func (c *cpuMeter) reset() {
	c.samples[0].Name = "/cpu/classes/total:cpu-seconds"
	c.samples[1].Name = "/cpu/classes/idle:cpu-seconds"
	metrics.Read(c.samples[:])
	c.lastTotal, c.lastIdle = c.value(0), c.value(1)
	c.last, c.started = 0, true
}

func (c *cpuMeter) value(i int) float64 {
	if c.samples[i].Value.Kind() != metrics.KindFloat64 {
		return 0
	}
	return c.samples[i].Value.Float64()
}

// percent is the CPU used over the interval, as a share of one core.
func (c *cpuMeter) percent(interval time.Duration) float64 {
	if !c.started || interval <= 0 {
		return 0
	}
	metrics.Read(c.samples[:])
	total, idle := c.value(0), c.value(1)
	dTotal, dIdle := total-c.lastTotal, idle-c.lastIdle
	c.lastTotal, c.lastIdle = total, idle
	if dTotal <= 0 {
		// No GC since the last reading, so the counters have not moved and this
		// interval is unmeasured. The last known figure is a better answer than
		// a zero the process did not spend.
		return c.last
	}
	busy := dTotal - dIdle
	if busy < 0 {
		busy = 0
	}
	c.last = busy / interval.Seconds() * 100
	return c.last
}

// ---------------------------------------------------------------------------
// charts
// ---------------------------------------------------------------------------

// sparkLevels is the ramp a column is drawn with: nine states, from nothing to
// a full cell, which is what a block-drawing font gives without reaching for
// braille (whose two columns per cell are not worth a font that renders them
// as boxes).
var sparkLevels = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// sparkRows draws a series as h rows of blocks, scaled to top. The rows are
// returned top first, so a caller can print them in order.
func sparkRows(vals []float64, h int, top float64) []string {
	if h <= 0 || len(vals) == 0 {
		return nil
	}
	if top <= 0 {
		top = 1
	}
	rows := make([]string, h)
	for r := range h {
		var b strings.Builder
		for _, v := range vals {
			// Eighths of a cell, counted from the bottom of the chart, so a
			// column taller than this row fills it and one shorter than it is
			// empty. A value that is not zero always gets at least one eighth:
			// "barely ran" and "did not run" must not look alike.
			units := int(v / top * float64(h*8))
			if units == 0 && v > 0 {
				units = 1
			}
			level := units - (h-1-r)*8
			b.WriteRune(sparkLevels[max(0, min(8, level))])
		}
		rows[r] = b.String()
	}
	return rows
}

// resample fits a series to w columns, taking the largest reading in each. The
// peak is what a chart of a run is read for, so it is the peak that survives
// being squeezed.
func resample(vals []float64, w int) []float64 {
	if w <= 0 || len(vals) == 0 {
		return nil
	}
	if len(vals) <= w {
		return vals
	}
	out := make([]float64, w)
	for i := range w {
		lo := i * len(vals) / w
		hi := (i + 1) * len(vals) / w
		if hi <= lo {
			hi = lo + 1
		}
		best := vals[lo]
		for _, v := range vals[lo:min(hi, len(vals))] {
			if v > best {
				best = v
			}
		}
		out[i] = best
	}
	return out
}

// maxOf is the largest reading in a series, which is where the top of its
// chart goes.
func maxOf(vals []float64) float64 {
	var top float64
	for _, v := range vals {
		if v > top {
			top = v
		}
	}
	return top
}

func (d *devMonitor) heapSeries() []float64 {
	out := make([]float64, len(d.samples))
	for i, s := range d.samples {
		out[i] = float64(s.heap)
	}
	return out
}

func (d *devMonitor) cpuSeries() []float64 {
	out := make([]float64, len(d.samples))
	for i, s := range d.samples {
		out[i] = s.cpu
	}
	return out
}

// ---------------------------------------------------------------------------
// formatting
// ---------------------------------------------------------------------------

// formatBytes renders a byte count the way a person reads one: three
// significant figures and a unit, rather than eleven digits to count.
func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, exp := float64(n), 0
	for v >= unit && exp < 4 {
		v /= unit
		exp++
	}
	suffix := []string{"B", "KB", "MB", "GB", "TB"}[exp]
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, suffix)
	}
	return fmt.Sprintf("%.1f %s", v, suffix)
}

// formatCount renders a step count with thousands separators: the difference
// between two million and two hundred thousand is the whole point of showing
// it, and it is not visible in a wall of digits.
func formatCount(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// formatRate renders steps per second, in the same units the count is in.
func formatRate(steps int, elapsed time.Duration) string {
	if elapsed <= 0 || steps <= 0 {
		return ""
	}
	rate := float64(steps) / elapsed.Seconds()
	switch {
	case rate >= 1e6:
		return fmt.Sprintf("%.1fM steps/s", rate/1e6)
	case rate >= 1e3:
		return fmt.Sprintf("%.0fK steps/s", rate/1e3)
	default:
		return fmt.Sprintf("%.0f steps/s", rate)
	}
}

// deltaAgainst compares this run with the last one, as a percentage that says
// which way it went. Under a twentieth it says nothing: an interpreter's timing
// wanders by that much between two runs of the same program, and a monitor that
// reports noise as an improvement teaches you to ignore it.
func deltaAgainst(now, prev float64) string {
	if prev <= 0 || now <= 0 {
		return ""
	}
	change := (now - prev) / prev * 100
	switch {
	case change <= -5:
		return fmt.Sprintf("↓%.0f%%", -change)
	case change >= 5:
		return fmt.Sprintf("↑%.0f%%", change)
	}
	return "≈"
}
