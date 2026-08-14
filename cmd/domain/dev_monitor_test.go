package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/interp"
	"domain/ir"
	"domain/token"
)

// errTestFailure stands in for whatever a step failed with.
var errTestFailure = errors.New("boom")

// monitorProgram is small, has a loop to be inside, and prints — so a run of
// it has a position, a value and output to report.
const monitorProgram = "Cursed Energy: in.txt\n" +
	"Shikigami: Ints\n" +
	"Maximum Technique: Sum\n" +
	"Reveal: stdout\n"

// ---------------------------------------------------------------------------
// the screen a run opens
// ---------------------------------------------------------------------------

func TestDevRunOpensTheMonitor(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m = runToMonitor(t, m)

	if m.monitor == nil {
		t.Fatal("running did not open the monitor")
	}
	if m.monitor.done == nil {
		t.Fatal("the finished run left no summary on the monitor")
	}
	if got := m.monitor.done.outcome; got != "ran" {
		t.Errorf("outcome %q, want ran", got)
	}
	if m.monitor.done.steps == 0 {
		t.Error("the run reported no steps")
	}
	if m.monitor.peakHeap == 0 {
		t.Error("the run reported no memory at all")
	}
}

// The screen stays after the program has finished: that is the whole point of
// a report, and a run that vanished when it ended would be one you had to
// catch rather than read.
func TestDevMonitorStaysUntilAKeyIsPressed(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m = runToMonitor(t, m)
	if m.monitor == nil {
		t.Fatal("the monitor closed itself")
	}
	if !strings.Contains(ansi.Strip(m.view()), "run monitor") {
		t.Fatal("the monitor is not what is on screen")
	}

	m = devKey(m, "x")
	if m.monitor != nil {
		t.Fatal("a key did not dismiss the monitor")
	}
	if !strings.Contains(ansi.Strip(m.view()), "Maximum Technique") {
		t.Error("dismissing the monitor did not come back to the program")
	}
}

// Everything a dismissed monitor knew is still true, so it can be looked at
// again — a screen that closes on any key is a screen that closes by accident.
func TestDevMonitorReopens(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m = runToCompletion(t, m)
	if m.monitor != nil {
		t.Fatal("the monitor should be closed")
	}
	m = devKey(m, "esc") // the output pane the run left behind
	m = devKey(m, "alt+m")
	if m.monitor == nil {
		t.Fatal("alt+m did not reopen the monitor")
	}
	if m.monitor.done == nil {
		t.Error("the reopened monitor lost the run it was reporting")
	}
}

func TestDevMonitorSaysWhenThereIsNothingToReopen(t *testing.T) {
	m := newTestDevModel(monitorProgram)
	m = devKey(m, "alt+m")
	if m.monitor != nil {
		t.Fatal("opened a monitor over a program that was never run")
	}
	if !strings.Contains(m.status, "nothing has been run") {
		t.Errorf("status %q says nothing about there being no run", m.status)
	}
}

// The stepper and another run are the two things worth leaving the report for,
// so they are the two keys it does not treat as "dismiss".
func TestDevMonitorHandsOffToTheStepper(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m = runToMonitor(t, m)
	m = devKey(m, "ctrl+t")
	if m.monitor != nil {
		t.Error("the monitor stayed open behind the stepper")
	}
	if m.stepper == nil {
		t.Fatal("ctrl+t did not open the stepper over the run")
	}
}

func TestDevMonitorRunsAgain(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m = runToMonitor(t, m)
	first := m.monitor

	next, _ := m.monitorKey(devKeyMsg("ctrl+r"))
	m = next.(devModel)
	if m.monitor == first {
		t.Fatal("ctrl+r on the monitor did not start a second run")
	}
	if m.monitor == nil || !m.running {
		t.Fatal("the second run did not start")
	}
	if m.monitor.prev == nil {
		t.Error("the second run has nothing to compare itself against")
	}
}

// ---------------------------------------------------------------------------
// stopping
// ---------------------------------------------------------------------------

// ctrl+c on the monitor stops the run rather than dismissing the screen: while
// a program is going there is nothing else that key could usefully mean, and
// dismissing the monitor mid-run would hide what it was opened to show.
func TestDevMonitorCtrlCStopsTheRun(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	next, _ := m.runProgram()
	m = next.(devModel)
	if m.monitor == nil {
		t.Fatal("the run did not open the monitor")
	}

	m = devKey(m, "ctrl+c")
	if m.interrupt == nil || !m.interrupt.Stopped() {
		t.Fatal("ctrl+c did not ask the run to stop")
	}
	if !m.monitor.stopping {
		t.Error("the screen does not know a stop was asked for")
	}
	if m.monitor != nil && m.monitor.done != nil {
		t.Error("the monitor closed the run before it had ended")
	}
}

// A key that is not ctrl+c does nothing at all while the run is going.
func TestDevMonitorIgnoresOtherKeysWhileRunning(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	next, _ := m.runProgram()
	m = next.(devModel)

	m = devKey(m, "x")
	if m.monitor == nil {
		t.Error("a stray key dismissed a running program's monitor")
	}
	if m.dirty {
		t.Error("a key meant for the monitor edited the program")
	}
}

// A run inside one long primitive reports nothing, which is exactly when
// ctrl+c looks ignored. The screen says why rather than looking broken.
func TestDevMonitorSaysWhyAStopHasNotLanded(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	next, _ := m.runProgram()
	m = next.(devModel)
	m = devKey(m, "ctrl+c")
	m.monitor.lastStep = time.Now().Add(-5 * time.Second)

	got := ansi.Strip(m.monitorBanner(m.width))
	if !strings.Contains(got, "a primitive must finish first") {
		t.Errorf("banner %q does not explain the delay", got)
	}
}

// ---------------------------------------------------------------------------
// what the readings say
// ---------------------------------------------------------------------------

func TestDevMonitorSamplesOverTime(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	next, _ := m.runProgram()
	m = next.(devModel)
	seq := m.monitor.seq
	// The monitor takes one reading as it opens, so the screen a run opens is
	// never blank.
	if len(m.monitor.samples) != 1 {
		t.Fatalf("opening the monitor took %d readings, want 1", len(m.monitor.samples))
	}

	for range 3 {
		next, cmd := m.Update(devSampleMsg{seq: seq})
		m = next.(devModel)
		if cmd == nil {
			t.Fatal("a sample of a running program did not schedule the next one")
		}
	}
	if got := len(m.monitor.samples); got != 4 {
		t.Fatalf("took %d readings, want 4 (one on opening, three on ticks)", got)
	}
	for i, s := range m.monitor.samples {
		if i > 0 && s.at < m.monitor.samples[i-1].at {
			t.Error("the readings are not in time order")
		}
	}
}

// A tick from a run that has ended cannot append to the run that replaced it:
// two runs of an unedited buffer share a generation, so the run's own sequence
// number is what separates them.
func TestDevMonitorDropsAStaleTick(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	next, _ := m.runProgram()
	m = next.(devModel)

	before := len(m.monitor.samples)
	next, cmd := m.Update(devSampleMsg{seq: m.monitor.seq - 1})
	m = next.(devModel)
	if cmd != nil {
		t.Error("a stale tick started a second stream of ticks")
	}
	if len(m.monitor.samples) != before {
		t.Error("a stale tick was recorded against the run showing now")
	}
}

// A finished run takes no more readings: its numbers describe the run, and a
// monitor that kept sampling would show the editor's own idling as the
// program's.
func TestDevMonitorStopsSamplingWhenTheRunEnds(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m = runToMonitor(t, m)

	before := len(m.monitor.samples)
	next, cmd := m.Update(devSampleMsg{seq: m.monitor.seq})
	m = next.(devModel)
	if cmd != nil {
		t.Error("a finished run scheduled another reading")
	}
	if len(m.monitor.samples) != before {
		t.Error("a finished run took another reading")
	}
}

// The history covers the whole run however long it goes: when it is full it
// halves, and the reading it keeps from each pair is the heavier one, so the
// peak a chart is read for survives.
func TestDevMonitorHistoryHalvesAndKeepsThePeak(t *testing.T) {
	d := &devMonitor{every: devSampleEvery, live: &devLive{}, prog: &progressCounter{}}
	for i := range devMaxSamples {
		d.samples = append(d.samples, devSample{at: time.Duration(i), heap: uint64(i)})
	}
	peak := d.samples[len(d.samples)-1].heap
	d.compact()

	if got := len(d.samples); got != devMaxSamples/2 {
		t.Fatalf("history is %d readings after halving, want %d", got, devMaxSamples/2)
	}
	if d.every != 2*devSampleEvery {
		t.Errorf("spacing is %s, want %s", d.every, 2*devSampleEvery)
	}
	if got := d.samples[len(d.samples)-1].heap; got != peak {
		t.Errorf("halving lost the peak: %d, want %d", got, peak)
	}
}

// ---------------------------------------------------------------------------
// the live tracer
// ---------------------------------------------------------------------------

// The value is rendered for the step that answers a request and for no other:
// rendering one per step would put the cost of the screen on the run.
func TestDevLiveRendersOnlyWhenAsked(t *testing.T) {
	live := &devLive{}
	node := &ir.Node{Prim: "Sum", Pos: token.Position{Line: 3}}

	live.Step(ir.StepEvent{Node: node, Out: 1})
	if live.snapshot() != nil {
		t.Fatal("a step rendered a value nobody had asked for")
	}

	live.ask()
	live.Step(ir.StepEvent{Node: node, Out: 2, Frame: "Repeat 4 iter 2/4", Depth: 1})
	snap := live.snapshot()
	if snap == nil {
		t.Fatal("the step after the request rendered nothing")
	}
	if snap.line != 3 || snap.frame != "Repeat 4 iter 2/4" || snap.depth != 1 {
		t.Errorf("snapshot %+v does not say where the run was", snap)
	}

	live.Step(ir.StepEvent{Node: node, Out: 3})
	if live.snapshot() != snap {
		t.Error("a later step rendered a value without being asked")
	}
	if got := live.steps.Load(); got != 3 {
		t.Errorf("counted %d steps, want 3", got)
	}
}

// Everything the live tracer sees still reaches the recorder underneath it:
// the monitor watches a run, it does not replace what the run leaves behind.
func TestDevLiveForwardsToTheRecorder(t *testing.T) {
	rec := interp.NewRecorder(0)
	live := &devLive{Inner: rec}
	node := &ir.Node{Prim: "Sum", Pos: token.Position{Line: 1}}

	live.PushFrame("Repeat 2 iter 1/2", nil)
	live.Step(ir.StepEvent{Node: node, Out: 1, Depth: 1})
	live.PopFrame(1)
	live.Step(ir.StepEvent{Node: node, Out: 2})

	if got := rec.Steps(); got != 2 {
		t.Fatalf("the recorder saw %d steps, want 2", got)
	}
	if len(rec.Roots()) == 0 {
		t.Error("the recorder kept nothing of a run it was watching")
	}
}

// A failing step reports its error where the value goes: that is the answer to
// "what has it got" at the moment a run stops having anything.
func TestDevLiveReportsAFailure(t *testing.T) {
	live := &devLive{}
	live.ask()
	live.Step(ir.StepEvent{
		Node: &ir.Node{Prim: "Sum", Pos: token.Position{Line: 2}},
		Err:  errTestFailure,
	})
	if snap := live.snapshot(); snap == nil || !strings.Contains(snap.short, "boom") {
		t.Errorf("snapshot %+v does not carry the failure", snap)
	}
}

// ---------------------------------------------------------------------------
// output, as it is printed
// ---------------------------------------------------------------------------

func TestDevOutBufTailsWhatHasBeenPrinted(t *testing.T) {
	var out devOutBuf
	for _, line := range []string{"one\n", "two\n", "three\n", "four\n"} {
		if _, err := out.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	got := out.tail(2)
	if len(got) != 2 || got[0] != "three" || got[1] != "four" {
		t.Errorf("tail is %q, want the last two lines", got)
	}
	if out.String() != "one\ntwo\nthree\nfour\n" {
		t.Error("tailing the output changed it")
	}
}

// The run's output is on the screen while the run is still going, which is the
// difference the monitor makes to a program that prints as it works.
func TestDevMonitorShowsOutputWhileRunning(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	next, _ := m.runProgram()
	m = next.(devModel)
	m.height = 40 // tall enough for the tail
	if _, err := m.monitor.out.Write([]byte("half an answer\n")); err != nil {
		t.Fatal(err)
	}

	if got := ansi.Strip(m.monitorView()); !strings.Contains(got, "half an answer") {
		t.Error("the monitor is not showing output the run has already printed")
	}
}

// ---------------------------------------------------------------------------
// the screen itself
// ---------------------------------------------------------------------------

// Every row fits: the monitor is a full-screen layout, and a row that overflows
// wraps and pushes the footer off the bottom of the terminal.
func TestDevMonitorViewFitsTheTerminal(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m = runToMonitor(t, m)

	for _, size := range [][2]int{{40, 12}, {60, 20}, {80, 24}, {120, 40}, {200, 60}} {
		m.width, m.height = size[0], size[1]
		lines := strings.Split(m.monitorView(), "\n")
		if len(lines) != m.height {
			t.Errorf("%dx%d: drew %d rows, want %d", size[0], size[1], len(lines), m.height)
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > m.width {
				t.Errorf("%dx%d: row %d is %d columns", size[0], size[1], i, w)
			}
		}
	}
}

// The screen answers the four questions it was opened for, and says whose
// numbers the memory and the CPU are.
func TestDevMonitorViewSaysWhatItKnows(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m.width, m.height = 100, 40
	m = runToMonitor(t, m)

	got := ansi.Strip(m.monitorView())
	for _, want := range []string{
		"heap in use", "cpu, share of one core", "this editor included",
		"where it is", "allocated in total", "ran",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the monitor never mentions %q:\n%s", want, got)
		}
	}
}

// A finished run offers the keys that are worth pressing on it; a running one
// offers the only key that means anything.
func TestDevMonitorFooterFollowsTheRun(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	next, _ := m.runProgram()
	m = next.(devModel)
	if got := ansi.Strip(m.monitorFooter(m.width)); !strings.Contains(got, "ctrl+c") {
		t.Errorf("a running program's footer %q does not say how to stop it", got)
	}

	m = runToMonitor(t, devWriteProgram(t, monitorProgram, "1\n2\n3\n"))
	got := ansi.Strip(m.monitorFooter(m.width))
	for _, want := range []string{"ctrl+t", "ctrl+r", "back to the program"} {
		if !strings.Contains(got, want) {
			t.Errorf("a finished run's footer %q does not offer %q", got, want)
		}
	}
}

// The comparison is against the previous run of the session, which is what
// makes the screen worth keeping open while making a program faster.
func TestDevMonitorComparesWithTheRunBefore(t *testing.T) {
	m := devWriteProgram(t, monitorProgram, "1\n2\n3\n")
	m.width, m.height = 100, 40
	m = runToCompletion(t, m)
	m = runToMonitor(t, m)

	if m.monitor.prev == nil {
		t.Fatal("the second run has nothing to compare against")
	}
	if got := ansi.Strip(strings.Join(m.monitorTotals(m.width), "\n")); !strings.Contains(got, "last run") {
		t.Errorf("the totals %q do not mention the run before", got)
	}
}

// A chart column is never blank for a reading that happened: "barely used any"
// and "did not run" must not look alike.
func TestSparkRowsMarkTheSmallestReading(t *testing.T) {
	rows := sparkRows([]float64{0, 0.0001, 100}, 2, 100)
	if len(rows) != 2 {
		t.Fatalf("drew %d rows, want 2", len(rows))
	}
	bottom := []rune(rows[1])
	if bottom[0] != ' ' {
		t.Errorf("a zero reading drew %q", string(bottom[0]))
	}
	if bottom[1] == ' ' {
		t.Error("a tiny reading drew nothing at all")
	}
	if bottom[2] != '█' {
		t.Errorf("a full reading drew %q", string(bottom[2]))
	}
}

// Squeezing a long run into a narrow terminal keeps the peaks, since the peak
// is the thing the chart is read for.
func TestResampleKeepsThePeaks(t *testing.T) {
	vals := make([]float64, 100)
	vals[42] = 1000
	got := resample(vals, 10)
	if len(got) != 10 {
		t.Fatalf("resampled to %d columns, want 10", len(got))
	}
	if maxOf(got) != 1000 {
		t.Error("resampling lost the peak")
	}
}

func TestFormatBytesReadsLikeASize(t *testing.T) {
	for _, tc := range []struct {
		in   uint64
		want string
	}{
		{512, "512 B"},
		{2048, "2.0 KB"},
		{1 << 20, "1.0 MB"},
		{300 << 20, "300 MB"},
		{3 << 30, "3.0 GB"},
	} {
		if got := formatBytes(tc.in); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatCountGroupsDigits(t *testing.T) {
	if got := formatCount(2481003); got != "2,481,003" {
		t.Errorf("formatCount = %q", got)
	}
	if got := formatCount(7); got != "7" {
		t.Errorf("formatCount = %q", got)
	}
}

// A difference smaller than the noise between two runs of the same program is
// reported as no difference, rather than as an improvement to believe in.
func TestDeltaAgainstIgnoresNoise(t *testing.T) {
	if got := deltaAgainst(1.02, 1.0); got != "≈" {
		t.Errorf("a 2%% change reported %q", got)
	}
	if got := deltaAgainst(0.5, 1.0); got != "↓50%" {
		t.Errorf("halving reported %q", got)
	}
	if got := deltaAgainst(2.0, 1.0); got != "↑100%" {
		t.Errorf("doubling reported %q", got)
	}
}

// A tea.Cmd for a sample really is a delayed message about the run it names.
func TestDevSampleCmdNamesItsRun(t *testing.T) {
	cmd := devSampleCmd(7)
	msg, ok := cmd().(devSampleMsg)
	if !ok {
		t.Fatalf("the sample command produced %T", msg)
	}
	if msg.seq != 7 {
		t.Errorf("the tick names run %d, want 7", msg.seq)
	}
}

var _ tea.Cmd = devSampleCmd(0)
