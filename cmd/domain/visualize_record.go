// Recording again, without leaving the stepper.
//
// The visualizer's model is "run once, then explore", and that is right: a
// finished recording can be walked backwards, and a live trace cannot. But it
// made the edit loop expensive in a way that had nothing to do with the model.
// Change one line of the program and everything you had built up — the frames
// you opened, the row you were on, the pane you were reading — was gone, along
// with the two minutes of navigating that produced it. So people stopped
// re-running, which meant they stopped checking.
//
// `r` records again. `--watch` records again whenever the program or its input
// changes on disk. Neither is worth much on its own; what makes them worth
// having is the two things around them:
//
//   - **The view survives.** A recording is a fresh tree of fresh pointers, so
//     the collapse state and the cursor cannot be carried over as they are.
//     They are carried over by *path* instead (nodeKeys): the row that was
//     `Repeat 3 / iter 2/3 / Map Each` is that row again in the new recording,
//     whatever address it lives at. Open frames stay open, the cursor stays put,
//     and the pane you were reading is the pane you are still reading.
//
//   - **The difference is on screen.** A re-record that just redrew would leave
//     the reader diffing two screens from memory. Instead the new recording is
//     compared against the one it replaced (recordingDelta): rows whose output
//     changed are marked in the tree, and the footer says what moved — steps,
//     time, the program's result, and whether the failure appeared or went away.
//     That comparison is the actual product here. Running a program twice is
//     easy; being told what the second run did differently is the thing.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"domain/interp"
)

// recordedMsg carries a finished re-recording back to the event loop. The run
// happens off the event loop, so a program that takes ten seconds does not
// freeze the UI that is still showing the last one.
type recordedMsg struct {
	view *traceView
	err  error
	why  string // what asked for it: "r" or the file that changed
}

// visWatchTickMsg asks whether a watched file has changed. Same poll-and-compare
// as the REPL's `:watch` (repl_watch.go), for the same reasons documented there.
type visWatchTickMsg struct{ gen int }

// visWatchInterval is how often the watched files are checked. A variable so
// tests do not wait for it.
var visWatchInterval = 500 * time.Millisecond

// visWatch is the set of files a --watch session re-records on.
type visWatch struct {
	files map[string]fileStamp
	gen   int
}

type fileStamp struct {
	modTime time.Time
	size    int64
}

// newVisWatch watches the program and, when there is one, the input file. Both
// matter: an Advent of Code afternoon edits the program in one window and the
// input in another, and only re-running on one of them is a trap.
func newVisWatch(paths ...string) *visWatch {
	w := &visWatch{files: map[string]fileStamp{}}
	for _, p := range paths {
		if p == "" {
			continue
		}
		w.files[p] = stampOf(p)
	}
	return w
}

func stampOf(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{modTime: info.ModTime(), size: info.Size()}
}

// changed reports the first watched file that differs from the last look, and
// records the new shape. A file that cannot be read is not a change: an editor
// writing through a temporary file makes it vanish for an instant, and
// re-recording on that would report a failure the user never made.
func (w *visWatch) changed() (string, bool) {
	for path, was := range w.files {
		now := stampOf(path)
		if now == (fileStamp{}) {
			continue
		}
		if now != was {
			w.files[path] = now
			return path, true
		}
	}
	return "", false
}

// tick schedules the next look.
func (w *visWatch) tick() tea.Cmd {
	if w == nil {
		return nil
	}
	gen := w.gen
	return tea.Tick(visWatchInterval, func(time.Time) tea.Msg { return visWatchTickMsg{gen: gen} })
}

// ---------------------------------------------------------------------------
// Carrying the view across a recording
// ---------------------------------------------------------------------------

// nodeKeys maps every row to a path that identifies it across recordings:
// each ancestor's label, plus which of its identically-labelled siblings it is.
//
// Pointers cannot do this job — a new recording is a new tree — and a row index
// cannot either, because the whole point is that the program changed and the
// indices moved. A path can: the second lap of the outer loop is the second lap
// of the outer loop whatever was edited above it, and when a row genuinely no
// longer exists its key simply finds nothing, which is the honest answer.
func nodeKeys(roots []*interp.TraceNode) map[*interp.TraceNode]string {
	keys := make(map[*interp.TraceNode]string, len(roots))
	var walk func(nodes []*interp.TraceNode, prefix string)
	walk = func(nodes []*interp.TraceNode, prefix string) {
		seen := map[string]int{}
		for _, n := range nodes {
			label := n.Label()
			seen[label]++
			key := fmt.Sprintf("%s/%s#%d", prefix, label, seen[label])
			keys[n] = key
			walk(n.Children, key)
		}
	}
	walk(roots, "")
	return keys
}

// viewState is what a reader has built up that a new recording must not
// destroy. Everything in it is keyed by path or is a plain value, so none of it
// refers to the recording it came from.
type viewState struct {
	expanded map[string]bool
	cursor   string
	pane     detailPane
	screen   screen
	filter   string
	focus    paneFocus
	// outputs is what each row produced, so the next recording can say which
	// rows changed.
	outputs map[string]string
}

// snapshot captures the state of a model, ready to be restored onto the next
// recording of the same program.
func (m *visualModel) snapshot() viewState {
	st := viewState{
		expanded: map[string]bool{},
		pane:     m.pane,
		screen:   m.screen,
		filter:   m.filter,
		focus:    m.focus,
		outputs:  map[string]string{},
	}
	for node, open := range m.expanded {
		if open {
			if key, ok := m.keys[node]; ok {
				st.expanded[key] = true
			}
		}
	}
	if n := m.selectedNode(); n != nil {
		st.cursor = m.keys[n]
	}
	for node, key := range m.keys {
		st.outputs[key] = rowOutput(node)
	}
	return st
}

// rowOutput is the value a row produced, as the comparison sees it: a block
// reports its body's result, everything else its own output, and a failed step
// reports its error — a row that stopped failing has changed as surely as one
// whose number moved.
func rowOutput(n *interp.TraceNode) string {
	if n.Block != nil {
		return n.Block.Short
	}
	if n.IsFrame() {
		return ""
	}
	if n.Step.Err != nil {
		return "error: " + n.Step.Err.Error()
	}
	return n.Step.Short
}

// restore puts a snapshot back onto a freshly recorded model, and works out
// what changed on the way. Rows the new recording does not have are dropped
// silently: the program was edited, and a stage that no longer exists is not an
// error to report.
func (m *visualModel) restore(st viewState) {
	m.pane, m.screen, m.focus = st.pane, st.screen, st.focus
	m.filter = st.filter
	m.changed = map[*interp.TraceNode]bool{}

	var cursorNode *interp.TraceNode
	for node, key := range m.keys {
		if st.expanded[key] {
			m.expanded[node] = true
		}
		if key == st.cursor {
			cursorNode = node
		}
		// A row with no counterpart is new, which counts as changed: it is
		// exactly what a reader wants picked out after an edit.
		was, existed := st.outputs[key]
		if !existed || was != rowOutput(node) {
			m.changed[node] = true
		}
	}
	m.rebuild()
	if cursorNode != nil {
		m.reveal(cursorNode)
		return
	}
	// The row the cursor was on is gone. Landing on row 0 loses the reader's
	// place entirely, so the nearest surviving ancestor is the next best thing.
	for key := trimKey(st.cursor); key != ""; key = trimKey(key) {
		for node, k := range m.keys {
			if k == key {
				m.reveal(node)
				m.status = "the row you were on is no longer in the program"
				return
			}
		}
	}
}

// trimKey drops the last path component of a row key, walking towards the root.
func trimKey(key string) string {
	if i := strings.LastIndex(key, "/"); i > 0 {
		return key[:i]
	}
	return ""
}

// ---------------------------------------------------------------------------
// What changed
// ---------------------------------------------------------------------------

// recordingDelta is the difference between a recording and the one it replaced,
// as a line someone can read without looking anything up.
type recordingDelta struct {
	steps    int           // new minus old
	total    time.Duration // new minus old
	wasTotal time.Duration
	rows     int    // how many rows produced something different
	failure  string // how the run's success or failure moved; "" when it did not
	result   string // how the program's own answer moved; "" when it did not
}

// compare works out what the new recording did differently.
func compare(was, now *traceView, changed map[*interp.TraceNode]bool) recordingDelta {
	d := recordingDelta{
		steps:    now.rec.Steps() - was.rec.Steps(),
		total:    now.times().Overall() - was.times().Overall(),
		wasTotal: was.times().Overall(),
		rows:     len(changed),
	}
	switch {
	case was.runErr == nil && now.runErr != nil:
		d.failure = "now fails: " + firstLine(now.runErr.Error())
	case was.runErr != nil && now.runErr == nil:
		d.failure = "the failure is gone"
	case was.runErr != nil && now.runErr != nil && was.runErr.Error() != now.runErr.Error():
		d.failure = "fails differently: " + firstLine(now.runErr.Error())
	}
	// The program's own answer is what it revealed, when it revealed anything —
	// the one line of a re-record that a reader would otherwise go looking for.
	if was.revealed != now.revealed {
		switch {
		case now.revealed == "":
			d.result = "revealed nothing this time"
		case was.revealed == "":
			d.result = "revealed " + firstLine(now.revealed)
		default:
			d.result = fmt.Sprintf("revealed %s → %s", firstLine(was.revealed), firstLine(now.revealed))
		}
	}
	return d
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + "…"
	}
	return s
}

// String renders the delta as the status line a re-record leaves behind.
func (d recordingDelta) String() string {
	parts := []string{fmt.Sprintf("re-recorded %s", signed(d.steps, "step"))}
	if d.wasTotal > 0 {
		pct := 100 * float64(d.total) / float64(d.wasTotal)
		switch {
		case pct >= 5 || pct <= -5:
			parts = append(parts, fmt.Sprintf("%+.0f%% time", pct))
		default:
			parts = append(parts, "same time")
		}
	}
	if d.rows > 0 {
		parts = append(parts, plural(d.rows, "row")+" changed")
	} else {
		parts = append(parts, "nothing changed")
	}
	if d.result != "" {
		parts = append(parts, d.result)
	}
	if d.failure != "" {
		parts = append(parts, d.failure)
	}
	return strings.Join(parts, " · ")
}

// signed renders a count as a movement — "+7 steps", "-3 steps", "no change" —
// since after a re-record the interesting number is the difference.
func signed(n int, noun string) string {
	if n == 0 {
		return "no change in " + noun + "s"
	}
	return fmt.Sprintf("%+d %s", n, noun+plural1(n))
}

func plural1(n int) string {
	if n == 1 || n == -1 {
		return ""
	}
	return "s"
}

// ---------------------------------------------------------------------------
// Driving it
// ---------------------------------------------------------------------------

// rerecord is the command that runs the program again, off the event loop.
func (m *visualModel) rerecord(why string) tea.Cmd {
	spec := m.spec
	return func() tea.Msg {
		view, err := spec.record()
		return recordedMsg{view: view, err: err, why: why}
	}
}

// startRecording kicks off a re-record unless one is already in flight.
func (m *visualModel) startRecording(why string) tea.Cmd {
	if m.spec.path == "" {
		m.status = "this recording has no program to run again"
		return nil
	}
	if m.recording {
		return nil
	}
	m.recording = true
	m.status = "recording again…"
	return m.rerecord(why)
}

// finishRecording adopts a finished recording, or reports why it could not.
func (m *visualModel) finishRecording(msg recordedMsg) {
	m.recording = false
	if msg.err != nil {
		// A program that no longer resolves is the normal state of a file
		// halfway through being edited. The recording on screen is still good,
		// so it stays, and the error goes in the footer where a syntax error
		// belongs — not over the top of the trace.
		m.status = "cannot record: " + firstLine(msg.err.Error())
		return
	}
	st := m.snapshot()
	was := m.view

	msg.view.expand, msg.view.depth = was.expand, was.depth
	m.adopt(msg.view)
	m.restore(st)

	d := compare(was, msg.view, m.changed)
	if msg.why != "" && msg.why != "r" {
		m.status = fmt.Sprintf("%s changed · %s", msg.why, d)
		return
	}
	m.status = d.String()
}

// watchStatus is the line a --watch session shows before anything has changed,
// so the reader knows the tool is waiting rather than idle.
func watchStatus(w *visWatch) string {
	if w == nil {
		return ""
	}
	names := make([]string, 0, len(w.files))
	for path := range w.files {
		names = append(names, shortPath(path))
	}
	// Sorted, so the line does not reshuffle itself between redraws.
	slicesSort(names)
	return "watching " + strings.Join(names, " and ") + " — every change re-records"
}

// shortPath is a path as a reader thinks of it: the file name, since the
// directory is the one they are sitting in.
func shortPath(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// slicesSort sorts a small string slice in place. (sort.Strings by another
// name, kept local so the import list of this file stays about recording.)
func slicesSort(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// openAs puts the stepper into the state the flags asked for.
//
// The flags that select a view used to switch the UI *off*: `--go` and
// `--expressions` on a terminal printed text and never opened the stepper,
// even though the stepper has a better form of each — a full-screen code view
// and a pane. Asking to see the emitted Go is not asking to stop using the
// debugger, so they now say where to open rather than whether to.
func openAs(m *visualModel, spec recordSpec, opts visualizeOptions) {
	switch {
	case opts.Go:
		m.openGo()
	case opts.Exprs:
		m.pane = paneExpr
	}
	// Likewise --expand-loops, which the text printer read and the UI ignored.
	if opts.Expand {
		m.expandAll()
	}
	if opts.Watch {
		m.watch = newVisWatch(spec.path, opts.Input)
	}
}

// runVisualizeTUI drives the stepper on a real terminal.
func runVisualizeTUI(view *traceView, spec recordSpec, opts visualizeOptions,
	stdin io.Reader, stdout, stderr io.Writer) int {
	m := newVisualModel(view)
	m.spec = spec
	openAs(m, spec, opts)

	teaOpts := []tea.ProgramOption{tea.WithOutput(stdout)}
	if f, ok := stdin.(*os.File); ok {
		teaOpts = append(teaOpts, tea.WithInput(f))
	}
	prog := tea.NewProgram(m, teaOpts...)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	return 0
}
