package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/mahoraga"
)

// newTestWheel builds a wheel with a bridge but no search behind it, which is
// what lets every test here drive the model by hand.
func newTestWheel(t *testing.T) *wheelModel {
	t.Helper()
	m := newWheelModel("day15.domain", "day15.txt", mahoragaOptions{Tier: mahoraga.Pinned})
	m.bridge = newWheelBridge()
	m.width, m.height = 120, 40
	return m
}

// send pushes messages through Update the way the event loop would.
func (m *wheelModel) send(msgs ...tea.Msg) {
	for _, msg := range msgs {
		m.Update(msg)
	}
}

func (m *wheelModel) event(e mahoraga.Event) { m.send(wheelEventMsg{event: e}) }

// plain strips the styling so a test can assert on what a reader sees.
func plainView(m *wheelModel) string { return ansi.Strip(m.View().Content) }

// The wheel's arms have to reach their tips without leaving the canvas and
// without two arms landing on the same cell — the geometry is the one part of
// this display that cannot be checked by looking at it in CI.
func TestWheelGeometry(t *testing.T) {
	seen := map[[2]int]int{}
	for k := range 8 {
		arm := wheelArm(k)
		if len(arm) == 0 {
			t.Fatalf("arm %d is empty", k)
		}
		for _, p := range arm {
			if p.x < 0 || p.x >= wheelW || p.y < 0 || p.y >= wheelH {
				t.Errorf("arm %d has a cell off the canvas at (%d,%d)", k, p.x, p.y)
			}
			if prev, ok := seen[[2]int{p.x, p.y}]; ok {
				t.Errorf("arms %d and %d both claim (%d,%d)", prev, k, p.x, p.y)
			}
			seen[[2]int{p.x, p.y}] = k
		}
		// The hub is drawn separately, so no arm may sit on it.
		if p := arm[0]; p.x == wheelCX && p.y == wheelCY {
			t.Errorf("arm %d starts on the hub", k)
		}
		// The digit has to land on the canvas too, or a handle loses its number.
		tip, off := arm[len(arm)-1], wheelDigitOffset[k]
		dx, dy := tip.x+off[0], tip.y+off[1]
		if dx < 0 || dx >= wheelW || dy < 0 || dy >= wheelH {
			t.Errorf("handle %d's digit is off the canvas at (%d,%d)", k+1, dx, dy)
		}
	}
}

// Every painted row must be exactly the canvas width, or the arms will not line
// up with the hub and the roster beside it will wander.
func TestWheelRowsAreRectangular(t *testing.T) {
	m := newTestWheel(t)
	m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 4, TurnName: "pass ablation"})
	for _, line := range m.wheelLines() {
		if got := ansi.StringWidth(line); got != wheelW {
			t.Errorf("a wheel row is %d columns wide, want %d: %q", got, wheelW, ansi.Strip(line))
		}
	}
}

// All eight handles are drawn from the first frame, before the search has
// reached any of them: the wheel's shape is the claim that there are eight
// turns, and it has to be true while seven of them are still pending.
func TestWheelDrawsEightHandlesFromTheStart(t *testing.T) {
	m := newTestWheel(t)
	art := ansi.Strip(strings.Join(m.wheelLines(), "\n"))
	for d := '1'; d <= '8'; d++ {
		if !strings.ContainsRune(art, d) {
			t.Errorf("handle %c is missing from the wheel:\n%s", d, art)
		}
	}
	if !strings.ContainsRune(art, '◉') {
		t.Errorf("the hub is missing:\n%s", art)
	}
}

// A turn the catalogue has not reached is drawn hollow rather than left off, so
// "found nothing" and "did not look" stay distinguishable.
//
// Written against mahoraga.TurnBuilt rather than against a turn number, because
// the answer changes as the catalogue fills in — all eight are built today, and
// this still has to hold the moment a ninth kind of turn is stubbed out.
func TestWheelMarksUnbuiltTurns(t *testing.T) {
	m := newTestWheel(t)
	for k := range 8 {
		mark, _ := m.handleMark(k)
		want := '·'
		if !mahoraga.TurnBuilt(k + 1) {
			want = '◌'
		}
		if mark != want {
			t.Errorf("turn %d's handle is %q, want %q (built=%v)",
				k+1, mark, want, mahoraga.TurnBuilt(k+1))
		}
	}

	// And the mechanism itself, independent of which turns happen to be built:
	// a handle marked absent is hollow and the roster says why.
	m.handles[1].state = handleAbsent
	if got, _ := m.handleMark(1); got != '◌' {
		t.Errorf("an absent handle is %q, want the hollow mark", got)
	}
	// The roster explains the absence either way: a turn that does not exist
	// says so, and one cut off by --turns says that instead.
	right, _ := m.rosterRight(1)
	if !strings.Contains(right, "not built") && !strings.Contains(right, "--turns") {
		t.Errorf("the roster does not explain an absent handle: %q", right)
	}
}

// A --turns cap is an absence too: handles past it are never reached, and
// drawing them as merely pending would be a lie the wheel keeps telling.
func TestWheelMarksTurnsBeyondTheCap(t *testing.T) {
	m := newWheelModel("p.domain", "in.txt", mahoragaOptions{Turns: 3})
	m.bridge = newWheelBridge()
	for k := 3; k < 8; k++ {
		if m.handles[k].state != handleAbsent {
			t.Errorf("handle %d is %v with --turns=3, want absent", k+1, m.handles[k].state)
		}
	}
	if got, _ := m.rosterRight(4); !strings.Contains(got, "--turns") {
		t.Errorf("the roster does not explain a handle cut off by --turns: %q", got)
	}
}

// An adaptation lights its handle, flashes the wheel, and moves the numbers.
func TestWheelLightsAHandleOnAdaptation(t *testing.T) {
	m := newTestWheel(t)
	m.event(mahoraga.Event{Kind: mahoraga.CandidateMeasured, Turn: 1,
		Measurement: mahoraga.Measurement{Mean: 20 * time.Millisecond, StdErr: 200 * time.Microsecond, Runs: 9}})
	m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 4, TurnName: "pass ablation"})
	m.event(mahoraga.Event{Kind: mahoraga.CandidateStart, Turn: 4, Candidate: "without loop fusion", Index: 3, Total: 29})
	m.event(mahoraga.Event{Kind: mahoraga.Adapted, Turn: 4, Candidate: "without loop fusion",
		Effect: 0.08, Tier: mahoraga.General,
		Champion:    mahoraga.Measurement{Mean: 20 * time.Millisecond, Runs: 9, Correct: true},
		Measurement: mahoraga.Measurement{Mean: 18400 * time.Microsecond, Runs: 9, Correct: true}})

	h := m.handles[3]
	if h.kept != 1 || h.best != 0.08 {
		t.Errorf("the handle did not record the win: %+v", h)
	}
	if m.flash == 0 {
		t.Error("an adaptation did not flash the wheel")
	}
	if got := m.champion(); got != 18400*time.Microsecond {
		t.Errorf("the champion is %v", got)
	}
	body := plainView(m)
	if !strings.Contains(body, "without loop fusion") {
		t.Errorf("the adaptation is not in the log:\n%s", body)
	}
	if !strings.Contains(body, "1 kept") {
		t.Errorf("the roster does not report the win:\n%s", body)
	}
}

// The turn in flight has to be identifiable on the wheel itself, not only in
// the text beside it.
func TestWheelMarksTheTurnInFlight(t *testing.T) {
	m := newTestWheel(t)
	m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 5, TurnName: "pass ordering"})
	if m.handles[4].state != handleTurning {
		t.Fatalf("turn 5 is %v, want turning", m.handles[4].state)
	}
	mark, _ := m.handleMark(4)
	if !strings.ContainsRune("◇◈◆", mark) {
		t.Errorf("the turning handle is %q", mark)
	}
	m.event(mahoraga.Event{Kind: mahoraga.TurnEnd, Turn: 5, TurnName: "pass ordering"})
	if m.handles[4].state != handleSpent {
		t.Errorf("a turn that ended with nothing kept is %v, want spent", m.handles[4].state)
	}
}

// The sweep turns, and turns faster when candidates are finishing faster. That
// is the one thing on screen encoding the search's actual pace.
func TestWheelSweepSpeedFollowsTheSearch(t *testing.T) {
	m := newTestWheel(t)
	before := m.sweep
	m.send(wheelTickMsg{})
	if m.sweep == before {
		t.Error("the sweep did not move on a tick")
	}
	slow := m.sweepStep()
	m.rate = 5
	if fast := m.sweepStep(); fast <= slow {
		t.Errorf("a busy search does not turn the wheel faster: %d then %d", slow, fast)
	}
	m.paused = true
	if m.sweepStep() != 0 {
		t.Error("a held wheel is still turning")
	}
}

// `q` is "finish and keep", not "quit": the search stops looking but still
// re-measures and still writes. Nothing about that is worth losing to a
// keystroke that reads like an abort.
func TestWheelQFinishesRatherThanQuits(t *testing.T) {
	m := newTestWheel(t)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil {
		t.Error("q quit the program instead of asking the search to finish")
	}
	if !m.finishing {
		t.Error("q did not ask the search to finish")
	}
	if !strings.Contains(m.status, "re-measuring") {
		t.Errorf("q did not say what it was doing: %q", m.status)
	}

	// When the search then reports, the wheel leaves on its own: the reader has
	// already said they are done.
	_, cmd = m.Update(wheelDoneMsg{recipe: &mahoraga.Recipe{Version: mahoraga.RecipeVersion}})
	if cmd == nil || !m.quitting {
		t.Error("the wheel did not close once the finish it was asked for arrived")
	}
}

// ctrl+c is the abort, and it has to release a search blocked mid-Report or the
// process would not exit.
func TestWheelCtrlCAborts(t *testing.T) {
	m := newTestWheel(t)
	_, cmd := m.Update(tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'c'})
	if cmd == nil {
		t.Fatal("ctrl+c did not quit")
	}
	if !m.aborted {
		t.Error("ctrl+c was not recorded as an abort")
	}
	select {
	case <-m.bridge.quit:
	default:
		t.Error("ctrl+c left the search blocked on a channel nobody is reading")
	}
}

// A turn abandoned with `s` is marked as abandoned rather than as having found
// nothing — they are different findings.
func TestWheelSkipMarksTheTurnAbandoned(t *testing.T) {
	m := newTestWheel(t)
	m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 4, TurnName: "pass ablation"})
	m.send(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.handles[3].state != handleSkipped {
		t.Errorf("turn 4 is %v after s, want skipped", m.handles[3].state)
	}
	if got, _ := m.handleMark(3); got != '⊘' {
		t.Errorf("an abandoned handle is %q", got)
	}
}

// A search that finished with a live handle must not be left spinning: without
// a TurnEnd the display would keep animating a turn that is over.
func TestWheelSettlesHandlesWhenTheSearchEnds(t *testing.T) {
	m := newTestWheel(t)
	m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 4, TurnName: "pass ablation"})
	m.send(wheelDoneMsg{recipe: &mahoraga.Recipe{Version: mahoraga.RecipeVersion}})
	if m.handles[3].state == handleTurning {
		t.Error("a handle is still turning after the search ended")
	}
	if !m.done || m.active != 0 {
		t.Errorf("the wheel did not settle: done=%v active=%d", m.done, m.active)
	}
}

// The verdict has to be able to say "nothing" — a tuner that always reports a
// win is not measuring.
func TestWheelVerdictSaysBaselineUnbeaten(t *testing.T) {
	m := newTestWheel(t)
	m.send(wheelDoneMsg{recipe: &mahoraga.Recipe{Version: mahoraga.RecipeVersion, Speedup: 1.01}})
	if got := ansi.Strip(m.verdictLine()); !strings.Contains(got, "BASELINE UNBEATEN") {
		t.Errorf("a search that kept nothing reported %q", got)
	}

	won := &mahoraga.Recipe{Version: mahoraga.RecipeVersion, Speedup: 1.3,
		Adaptations: []mahoraga.Adaptation{{Turn: 4, ID: "x", Kept: true}}}
	m2 := newTestWheel(t)
	m2.send(wheelDoneMsg{recipe: won})
	if got := ansi.Strip(m2.verdictLine()); !strings.Contains(got, "1.30× faster") {
		t.Errorf("a search that won reported %q", got)
	}
}

// The screens are readers, not modes: they scroll, and anything else returns.
func TestWheelScreens(t *testing.T) {
	m := newTestWheel(t)
	m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 4, TurnName: "pass ablation"})
	m.event(mahoraga.Event{Kind: mahoraga.Rejected, Turn: 4, Candidate: "without const fold",
		Effect: 0.001, Reason: "0.1% — below the 2.0% worth recording",
		Measurement: mahoraga.Measurement{Mean: 20 * time.Millisecond, Runs: 9, Correct: true}})

	for _, tc := range []struct{ key, want string }{
		{"a", "without const fold"},
		{"r", "no adaptation has been kept"},
		{"p", "passes"},
		{"?", "abandon the turn in flight"},
	} {
		m.send(tea.KeyPressMsg{Code: rune(tc.key[0]), Text: tc.key})
		got := plainView(m)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%q screen is missing %q:\n%s", tc.key, tc.want, got)
		}
		m.send(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.screen != screenWheel {
			t.Errorf("esc did not leave the %q screen", tc.key)
		}
	}
}

// The passes screen has to show a pass the search switched off — that is the
// entire content of what turn 4 finds.
func TestWheelPassesScreenShowsAblatedPasses(t *testing.T) {
	m := newTestWheel(t)
	m.schedule = []string{"a-pass-that-survived"}
	m.open(screenPasses)
	got := ansi.Strip(strings.Join(m.passesBody(), "\n"))
	if !strings.Contains(got, "off ") {
		t.Errorf("no pass is shown as switched off:\n%s", got)
	}
	if !strings.Contains(got, "switched off for this program and this input") {
		t.Errorf("the screen does not explain what an off pass means:\n%s", got)
	}
}

// Every frame the wheel paints must fit the terminal it was given, on a wide
// screen and on a cramped one. A row that overflows wraps, and a wrapped frame
// walks the whole display down the screen.
func TestWheelViewFitsTheTerminal(t *testing.T) {
	sizes := [][2]int{{120, 40}, {100, 30}, {80, 24}, {60, 18}, {40, 12}}
	for _, size := range sizes {
		m := newTestWheel(t)
		m.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.event(mahoraga.Event{Kind: mahoraga.CandidateMeasured, Turn: 1,
			Measurement: mahoraga.Measurement{Mean: 20 * time.Millisecond, StdErr: 200 * time.Microsecond, Runs: 9}})
		m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 4, TurnName: "pass ablation"})
		m.event(mahoraga.Event{Kind: mahoraga.CandidateStart, Turn: 4,
			Candidate: "without a pass with a rather long name", Index: 17, Total: 29})
		m.send(wheelTickMsg{}, wheelTickMsg{})

		lines := strings.Split(m.View().Content, "\n")
		for i, line := range lines {
			if got := ansi.StringWidth(line); got > size[0] {
				t.Errorf("%dx%d: line %d is %d columns wide:\n%s",
					size[0], size[1], i, got, ansi.Strip(line))
			}
		}
		if len(lines) > size[1] {
			t.Errorf("%dx%d: the view is %d rows tall", size[0], size[1], len(lines))
		}
	}
}

// The bridge is the whole contract between the search's goroutine and the event
// loop: events arrive in order, and a release unblocks a search nobody is
// reading any more.
func TestWheelBridge(t *testing.T) {
	b := newWheelBridge()
	go func() {
		b.Report(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 1})
		b.Report(mahoraga.Event{Kind: mahoraga.Adapted, Turn: 3, Effect: 0.1})
		b.done <- wheelDoneMsg{recipe: &mahoraga.Recipe{}}
	}()
	kinds := []mahoraga.EventKind{mahoraga.TurnStart, mahoraga.Adapted}
	for _, want := range kinds {
		msg := b.next()()
		e, ok := msg.(wheelEventMsg)
		if !ok {
			t.Fatalf("got %T, want an event", msg)
		}
		if e.event.Kind != want {
			t.Errorf("events arrived out of order: got %v, want %v", e.event.Kind, want)
		}
	}
	if _, ok := b.next()().(wheelDoneMsg); !ok {
		t.Error("the completion did not arrive after the events")
	}

	// A released bridge must not leave Report blocked forever.
	b2 := newWheelBridge()
	b2.release()
	done := make(chan struct{})
	go func() {
		b2.Report(mahoraga.Event{Kind: mahoraga.TurnStart})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Report blocked on a released bridge")
	}
}

// A real search through a real bridge into a real model. Every other test here
// injects events by hand, which proves the display and nothing about the wiring
// — that the search's Reporter is the bridge, that its events survive the
// crossing in order, and that the model ends up describing what actually
// happened.
func TestWheelDrivesARealSearch(t *testing.T) {
	requireGoToolchain(t)
	prog, input, expected := mahoragaArena(t)
	dir := filepath.Dir(prog)

	bridge := newWheelBridge()
	search, err := mahoraga.NewSearch(mahoraga.Options{
		Program: prog, Input: input, Expected: expected,
		BaselineRuns: 3, ScreenRuns: 2, Turns: 1,
		Out:    filepath.Join(dir, "adapted"),
		Recipe: filepath.Join(dir, "r.json"),
	}, bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer search.Close()

	m := newWheelModel(prog, input, mahoragaOptions{Turns: 1})
	m.bridge, m.search = bridge, search
	m.width, m.height = 110, 36

	done := make(chan struct{})
	go func() {
		recipe, err := search.Run()
		bridge.done <- wheelDoneMsg{recipe: recipe, err: err}
		close(done)
	}()

	// Drain the way the event loop does: one message at a time, until the
	// completion arrives.
	deadline := time.After(3 * time.Minute)
	for !m.done {
		select {
		case <-deadline:
			bridge.release()
			t.Fatal("the search did not finish")
		default:
		}
		m.send(bridge.next()())
	}
	<-done

	if m.err != nil {
		t.Fatalf("the search failed: %v", m.err)
	}
	if m.baseline <= 0 {
		t.Error("the wheel never learned the baseline")
	}
	if m.handles[0].state != handleSpent && m.handles[0].state != handleLit {
		t.Errorf("turn 1's handle is %v after a finished search", m.handles[0].state)
	}
	if got := ansi.Strip(m.verdictLine()); got == "" {
		t.Error("the wheel produced no verdict")
	}
	// The recipe screen has to describe the search that just ran, not a blank.
	m.open(screenRecipe)
	if body := ansi.Strip(strings.Join(m.recipeBody(), "\n")); !strings.Contains(body, prog) {
		t.Errorf("the recipe screen does not name the program:\n%s", body)
	}
}

// The wheel is for terminals. Anything being captured — a pipe, --plain, --json
// — gets the line-per-event reporter instead, because a search whose output is
// being read must not emit a stream of escape sequences into it.
func TestWantsWheelNeedsBothEnds(t *testing.T) {
	var sb strings.Builder
	if wantsWheel(mahoragaOptions{}, strings.NewReader(""), &sb) {
		t.Error("the wheel was chosen for a non-terminal")
	}
	if wantsWheel(mahoragaOptions{Plain: true}, nil, nil) {
		t.Error("--plain got the wheel")
	}
	if wantsWheel(mahoragaOptions{JSON: true}, nil, nil) {
		t.Error("--json got the wheel")
	}
}

// The display bug a real recipe caught, and the reason Event carries the
// champion's own figure.
//
// A search on a drifting machine accepted a candidate at 842ms because the
// champion, raced alongside it in the same minute, measured 871ms — a genuine
// 3.3% edge. The wheel then drew "best 842ms" beside a baseline of 713ms
// measured three minutes earlier, which reads 0.85× with a tick next to it: the
// display was dividing two numbers from two different machines.
//
// The fix is to never show the champion's raw mean. What is shown is the
// baseline scaled by the product of each accepted race's ratio, which is the
// only quantity in the search immune to drift.
func TestWheelShowsTheChampionRelativeToTheBaseline(t *testing.T) {
	m := newTestWheel(t)
	m.event(mahoraga.Event{Kind: mahoraga.CandidateMeasured, Turn: 1,
		Measurement: mahoraga.Measurement{Mean: 713 * time.Millisecond, StdErr: 5 * time.Millisecond, Runs: 9}})

	// The machine drifts 22% slower, and a candidate wins by 3.3% inside that.
	m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 4, TurnName: "pass ablation"})
	m.event(mahoraga.Event{Kind: mahoraga.Adapted, Turn: 4, Candidate: "without fuseLinearMapExtremum",
		Effect: 0.033, Tier: mahoraga.General,
		Champion:    mahoraga.Measurement{Mean: 871 * time.Millisecond, Runs: 9, Correct: true},
		Measurement: mahoraga.Measurement{Mean: 842 * time.Millisecond, Runs: 9, Correct: true}})

	// 713ms × (842/871) = 689ms. Faster than the baseline, which is what a 3.3%
	// win means — and never the raw 842ms.
	got := m.champion()
	if got > 700*time.Millisecond || got < 680*time.Millisecond {
		t.Errorf("best = %v, want ~689ms (the baseline scaled by 842/871)", got)
	}
	if got > m.baseline {
		t.Errorf("best (%v) is slower than the baseline (%v) after a win — this is the "+
			"bug: the champion's raw mean was measured on a drifted machine and the "+
			"baseline was not", got, m.baseline)
	}

	line := ansi.Strip(m.numbersLine())
	if strings.Contains(line, "842") {
		t.Errorf("the champion's raw mean is on screen: %q", line)
	}
	if !strings.Contains(line, "1.03") {
		t.Errorf("the speedup does not reflect the race's own ratio: %q", line)
	}
}

// With nothing kept, "best" is the baseline and the speedup is exactly one.
// A search that has adapted nothing has no other honest answer.
func TestWheelBestIsTheBaselineUntilSomethingIsKept(t *testing.T) {
	m := newTestWheel(t)
	m.event(mahoraga.Event{Kind: mahoraga.CandidateMeasured, Turn: 1,
		Measurement: mahoraga.Measurement{Mean: 713 * time.Millisecond, StdErr: 5 * time.Millisecond, Runs: 9}})
	m.event(mahoraga.Event{Kind: mahoraga.TurnStart, Turn: 4, TurnName: "pass ablation"})
	m.event(mahoraga.Event{Kind: mahoraga.Rejected, Turn: 4, Candidate: "without x",
		Reason: "slower by 5.6%", Effect: -0.056,
		Champion:    mahoraga.Measurement{Mean: 871 * time.Millisecond, Runs: 9, Correct: true},
		Measurement: mahoraga.Measurement{Mean: 920 * time.Millisecond, Runs: 9, Correct: true}})

	if m.champion() != m.baseline {
		t.Errorf("best = %v with nothing kept, want the baseline %v", m.champion(), m.baseline)
	}
	if line := ansi.Strip(m.numbersLine()); !strings.Contains(line, "1.000×") {
		t.Errorf("a search that kept nothing is not reporting 1.000×: %q", line)
	}
}
