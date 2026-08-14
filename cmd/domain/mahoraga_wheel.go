// The wheel — the terminal face of `domain expansion: mahoraga`.
//
// Eight handles, one per turn of the search, arranged at the compass points
// around a hub. A sweep rotates through them continuously; a handle lights when
// its turn adapts something, colored by how much it won. The geometry never
// moves: ASCII rotated frame by frame reads as noise rather than motion, so the
// wheel is a fixed shape lit by a moving light, which is legible at 16 frames a
// second and still says the same thing in a screenshot.
//
// The search knows nothing about any of this. It emits mahoraga.Event and this
// file is one of two things that consume them — the other is the plain reporter
// in mahoraga.go, which is what CI and the tests read. That separation is why
// `--plain` is not a degraded mode: it is the same search, reported differently.
//
// The search runs on its own goroutine because it spends minutes in `go build`
// and in subprocesses, and an event loop that blocks for a build is an event
// loop that cannot repaint, cannot animate, and cannot be interrupted. Events
// cross on an unbuffered channel, which keeps them strictly ordered with the
// completion that follows them: a buffer would let "done" overtake the last
// adaptation and light the verdict before the handle it belongs to.
//
// Tested the way the other TUIs here are (repl_tty.go, visualize_tui.go): by
// driving the model with injected messages rather than a pseudo-terminal.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"domain/codegen"
	"domain/interp"
	"domain/mahoraga"
)

// wheelFrame is how often the wheel repaints. Sixteen frames a second is
// enough for the sweep to read as rotation rather than a blinking light, and
// cheap: a frame is a few hundred styled cells, drawn while the search is
// blocked in a compiler.
const wheelFrame = 60 * time.Millisecond

// wheelSubsteps is how many sweep positions sit between one handle and the
// next. Eight handles at three substeps each gives a 24-position revolution,
// which is fine enough that the trail moves every frame at the slowest speed.
const wheelSubsteps = 3

const wheelPositions = 8 * wheelSubsteps

// ---------------------------------------------------------------------------
// Messages and the bridge from the search
// ---------------------------------------------------------------------------

type wheelTickMsg struct{}

type wheelEventMsg struct{ event mahoraga.Event }

type wheelDoneMsg struct {
	recipe *mahoraga.Recipe
	err    error
}

// wheelBridge carries the search's events onto the event loop.
//
// Report is called on the search goroutine and blocks until the loop takes the
// event, which is the backpressure that keeps the display in step with the
// search rather than showing a queue of things that already happened. The quit
// channel is the release valve: when the reader aborts, the loop stops draining
// and a search blocked mid-Report would never return.
type wheelBridge struct {
	events chan mahoraga.Event
	done   chan wheelDoneMsg
	quit   chan struct{}
	once   sync.Once
}

func newWheelBridge() *wheelBridge {
	return &wheelBridge{
		events: make(chan mahoraga.Event),
		done:   make(chan wheelDoneMsg, 1),
		quit:   make(chan struct{}),
	}
}

func (b *wheelBridge) Report(e mahoraga.Event) {
	select {
	case b.events <- e:
	case <-b.quit:
	}
}

// release unblocks a search that is mid-Report. Safe to call more than once.
func (b *wheelBridge) release() { b.once.Do(func() { close(b.quit) }) }

// next waits for whatever the search says. It is re-issued after every message
// it delivers, which is what keeps a single reader on the channel for the whole
// run.
func (b *wheelBridge) next() tea.Cmd {
	return func() tea.Msg {
		select {
		case e := <-b.events:
			return wheelEventMsg{event: e}
		case d := <-b.done:
			return d
		case <-b.quit:
			return nil
		}
	}
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

// handleState is what one of the eight handles is doing.
type handleState int

const (
	handlePending handleState = iota // not reached yet
	handleTurning                    // the turn in flight
	handleSpent                      // ran, kept nothing
	handleLit                        // ran, kept at least one adaptation
	handleAbsent                     // a turn the catalogue has not reached
	handleSkipped                    // abandoned by the reader
)

// wheelHandle is one turn's handle.
type wheelHandle struct {
	name  string
	state handleState
	tried int
	kept  int
	// best is the largest single effect this turn kept, as a fraction. It sets
	// the handle's color.
	best float64
	// glow counts down the frames a freshly lit handle burns white for.
	glow int
}

// wheelScreen is what fills the terminal.
type wheelScreen int

const (
	screenWheel  wheelScreen = iota
	screenLedger             // everything tried, with the verdict on each
	screenRecipe             // what a recipe written now would say
	screenPasses             // the optimizer passes, on and off
	screenWheelHelp
)

// wheelEntry is one candidate the search finished with.
type wheelEntry struct {
	turn      int
	label     string
	effect    float64
	kept      bool
	reason    string
	mean      time.Duration
	tier      mahoraga.Tier
	failed    bool
	timestamp time.Duration // since the search started
}

// wheelModel is the whole display.
type wheelModel struct {
	width, height int

	program, input string
	tier           mahoraga.Tier
	outPath        string
	recipePath     string

	handles [8]wheelHandle
	// active is the turn in flight, 1..8, or 0 before the first one starts.
	active int

	// sweep is the rotating highlight's position, in substeps around the wheel.
	sweep int
	// frame counts repaints, for everything that free-runs: the hub's heartbeat,
	// the pulse travelling out along the active spoke, the scanner bar.
	frame int
	// flash is the frames remaining of the whole-wheel flash an adaptation sets.
	flash int

	started   time.Time
	elapsedAt time.Duration // frozen once the search finishes

	// candidate is what is being built or measured right now, empty between
	// candidates.
	candidate            string
	candIndex, candTotal int
	candidateSince       time.Time

	baseline time.Duration
	// bestRatio is the champion's cost as a fraction of the baseline's, taken
	// as the running product of the ratios each accepted race measured. The
	// champion's own raw mean is never displayed: it comes from whichever
	// minute its race happened in, and dividing it by a baseline from a
	// different minute is what once drew a 842ms candidate as "best" beside a
	// 713ms baseline, reading 0.85× under a tick.
	bestRatio  float64
	noiseFloor float64

	// spark holds each finished candidate's mean as a multiple of the baseline's
	// speed — above one is faster. It is the shape of the search over time,
	// which the roster of eight handles cannot show.
	spark []float64

	entries []wheelEntry
	tried   int
	kept    int

	// schedule, rounds, flags and tuning are the champion's configuration,
	// carried on the Adapted events so the recipe screen can be live rather
	// than final.
	schedule []string
	rounds   int
	flags    []string
	tuning   codegen.Tuning

	recipe    *mahoraga.Recipe
	err       error
	done      bool
	finishing bool // q was pressed: the search is winding down
	aborted   bool

	paused bool
	// status is a message in place of the key list, and statusFor how many
	// frames it has left. It expires rather than lingering: the footer's job for
	// most of a long search is to say which keys do what, and a note from four
	// minutes ago occupying that line is worse than no note at all.
	status    string
	statusFor int

	screen wheelScreen
	scroll int

	search *mahoraga.Search
	bridge *wheelBridge

	// rate is a smoothed count of candidates finished per second. The sweep
	// turns at a speed set by it, so the wheel is visibly grinding on a turn
	// where every candidate costs a compile and racing on one where they do not.
	rate float64
	// quitting records that the reader left, as distinct from the search ending.
	quitting bool
}

func newWheelModel(prog, input string, opts mahoragaOptions) *wheelModel {
	m := &wheelModel{
		width:      100,
		height:     32,
		program:    prog,
		input:      input,
		tier:       opts.Tier,
		outPath:    mahoragaOut(prog, opts),
		recipePath: mahoragaRecipePath(prog, opts),
		started:    time.Now(),
		bestRatio:  1,
	}
	names := mahoraga.TurnNames()
	for i := range m.handles {
		h := &m.handles[i]
		if i < len(names) {
			h.name = names[i]
		}
		if !mahoraga.TurnBuilt(i + 1) {
			h.state = handleAbsent
		}
	}
	// A --turns cap is a real absence too: the handles past it are never
	// reached, and drawing them as "pending" for the whole run would be a lie
	// the wheel keeps telling.
	if opts.Turns > 0 && opts.Turns < 8 {
		for i := opts.Turns; i < 8; i++ {
			m.handles[i].state = handleAbsent
		}
	}
	return m
}

func (m *wheelModel) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.tick(), m.bridge.next())
}

func (m *wheelModel) tick() tea.Cmd {
	return tea.Tick(wheelFrame, func(time.Time) tea.Msg { return wheelTickMsg{} })
}

// champion is what the champion would cost on the machine the baseline was
// measured on: the baseline scaled by every accepted race's ratio. It is an
// estimate and it is the only honest one available, since the champion's own
// figure and the baseline's were taken minutes apart.
func (m *wheelModel) champion() time.Duration {
	return time.Duration(float64(m.baseline) * m.bestRatio)
}

// elapsed is how long the search has been running, frozen at the end so the
// final screen does not keep counting after there is nothing to count.
func (m *wheelModel) elapsed() time.Duration {
	if m.done {
		return m.elapsedAt
	}
	return time.Since(m.started)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m *wheelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		useTheme(isLightColor(msg.Color))
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case wheelTickMsg:
		m.advance()
		return m, m.tick()

	case wheelEventMsg:
		m.absorb(msg.event)
		return m, m.bridge.next()

	case wheelDoneMsg:
		m.recipe, m.err = msg.recipe, msg.err
		m.done, m.elapsedAt = true, time.Since(m.started)
		m.active = 0
		m.candidate = ""
		// A handle still marked as turning when the search ends never got its
		// TurnEnd — the reader stopped the search mid-turn — and leaving it
		// spinning on a finished search would be the display's own fiction.
		for i := range m.handles {
			if m.handles[i].state == handleTurning {
				m.handles[i].state = m.settled(i)
			}
		}
		// Stopping with `q` is a decision to be done, so the wheel does not sit
		// there waiting to be dismissed; the verdict is printed to the terminal
		// underneath either way.
		if m.finishing {
			m.quitting = true
			return m, tea.Quit
		}
		return m, m.tick()

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// settled is what a handle becomes once its turn is over.
func (m *wheelModel) settled(i int) handleState {
	if m.handles[i].kept > 0 {
		return handleLit
	}
	return handleSpent
}

// advance moves everything that moves on its own.
func (m *wheelModel) advance() {
	m.frame++
	if m.flash > 0 {
		m.flash--
	}
	if m.statusFor > 0 {
		m.statusFor--
		if m.statusFor == 0 {
			m.status = ""
		}
	}
	for i := range m.handles {
		if m.handles[i].glow > 0 {
			m.handles[i].glow--
		}
	}
	if step := m.sweepStep(); step > 0 {
		m.sweep = (m.sweep + step) % wheelPositions
	}
	// The rate decays on its own, so a turn that stops producing candidates
	// slows the wheel down instead of leaving it spinning at the speed of
	// whatever happened last.
	m.rate *= 0.97
}

// sweepStep is how far the highlight moves per frame — the wheel's speed, and
// the one thing on screen that says how fast the search is actually going.
func (m *wheelModel) sweepStep() int {
	switch {
	case m.paused:
		return 0
	case m.done:
		return 1 // coasting to a stop
	case m.rate >= 2:
		return 4
	case m.rate >= 0.8:
		return 3
	case m.rate >= 0.25:
		return 2
	}
	return 1
}

// absorb folds one search event into the display.
func (m *wheelModel) absorb(e mahoraga.Event) {
	switch e.Kind {
	case mahoraga.TurnStart:
		m.active = e.Turn
		if h := m.handle(e.Turn); h != nil {
			h.name = e.TurnName
			if mahoraga.TurnBuilt(e.Turn) {
				h.state = handleTurning
			}
		}

	case mahoraga.CandidateStart:
		m.candidate, m.candIndex, m.candTotal = e.Candidate, e.Index, e.Total
		m.candidateSince = time.Now()

	case mahoraga.CandidateMeasured:
		if e.Turn == 1 {
			m.baseline = e.Measurement.Mean
			if e.Measurement.Mean > 0 {
				m.noiseFloor = float64(e.Measurement.StdErr) / float64(e.Measurement.Mean)
			}
			m.note(fmt.Sprintf("baseline %s over %d runs, ±%.1f%%",
				shortDuration(e.Measurement.Mean), e.Measurement.Runs, m.noiseFloor*100))
		}
		m.pushSpark(e.Measurement.Mean)

	case mahoraga.Adapted:
		if e.Champion.Mean > 0 && e.Measurement.Mean > 0 {
			m.bestRatio *= float64(e.Measurement.Mean) / float64(e.Champion.Mean)
		}
		m.kept++
		m.tried++
		m.rateTick()
		if h := m.handle(e.Turn); h != nil {
			h.kept++
			h.tried++
			h.glow = 12
			if e.Effect > h.best {
				h.best = e.Effect
			}
		}
		m.flash = 10
		m.schedule, m.rounds = e.Schedule.Passes, e.Schedule.MaxRounds
		m.flags, m.tuning = e.Build.Flags, e.Tuning
		m.entries = append(m.entries, wheelEntry{
			turn: e.Turn, label: e.Candidate, effect: e.Effect, kept: true,
			mean: e.Measurement.Mean, tier: e.Tier, timestamp: m.elapsed(),
		})
		m.candidate = ""

	case mahoraga.Rejected:
		m.tried++
		m.rateTick()
		if h := m.handle(e.Turn); h != nil {
			h.tried++
		}
		m.entries = append(m.entries, wheelEntry{
			turn: e.Turn, label: e.Candidate, effect: e.Effect, reason: e.Reason,
			mean: e.Measurement.Mean, tier: e.Tier,
			failed: !e.Measurement.OK(), timestamp: m.elapsed(),
		})
		m.candidate = ""

	case mahoraga.TurnEnd:
		if h := m.handle(e.Turn); h != nil && h.state == handleTurning {
			h.state = m.settled(e.Turn - 1)
		}
		m.candidate = ""

	case mahoraga.SearchDone:
		m.candidate = ""
	}
}

func (m *wheelModel) handle(turn int) *wheelHandle {
	if turn < 1 || turn > len(m.handles) {
		return nil
	}
	return &m.handles[turn-1]
}

// rateTick folds one finished candidate into the smoothed rate the sweep speed
// is read from. An exponential average rather than a window: it needs to react
// within a second or two and it is driving an animation, not a decision.
func (m *wheelModel) rateTick() {
	since := time.Since(m.candidateSince)
	if since <= 0 {
		return
	}
	instant := 1 / since.Seconds()
	m.rate = 0.6*m.rate + 0.4*instant
}

// pushSpark records a candidate's mean as a multiple of the baseline's speed.
func (m *wheelModel) pushSpark(mean time.Duration) {
	if m.baseline <= 0 || mean <= 0 {
		return
	}
	m.spark = append(m.spark, float64(m.baseline)/float64(mean))
	if len(m.spark) > 512 {
		m.spark = m.spark[len(m.spark)-512:]
	}
}

// note puts a message in the footer for a few seconds.
func (m *wheelModel) note(s string) { m.status, m.statusFor = s, statusFrames }

// statusFrames is how long a footer note lives — long enough to read twice.
const statusFrames = 5 * int(time.Second/wheelFrame)

// ---------------------------------------------------------------------------
// Keys
// ---------------------------------------------------------------------------

func (m *wheelModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		// An abort is not a stop: nothing is written, and the search is cut off
		// wherever it is rather than being allowed to finish and re-measure.
		m.aborted, m.quitting = true, true
		m.bridge.release()
		if m.search != nil {
			m.search.Stop()
		}
		return m, tea.Quit
	}
	// Once the search is over the wheel is a report, and any key leaves it.
	if m.done && m.screen == screenWheel {
		m.quitting = true
		return m, tea.Quit
	}
	if m.screen != screenWheel {
		return m.screenKey(key)
	}

	switch key {
	case "q":
		if m.done {
			m.quitting = true
			return m, tea.Quit
		}
		// Finish and keep: the search stops looking, but still re-measures its
		// champion against the baseline and still writes both artifacts. A
		// search with no time limit has to let someone say "that is enough"
		// without throwing away what it found.
		m.finishing = true
		if m.search != nil {
			m.search.Stop()
		}
		m.note("finishing — re-measuring the champion, then writing")
	case "s":
		if m.active == 0 {
			m.note("no turn is running")
			break
		}
		if m.search != nil {
			m.search.SkipTurn(m.active)
		}
		if h := m.handle(m.active); h != nil {
			h.state = handleSkipped
		}
		m.note(fmt.Sprintf("turn %d abandoned — the wheel turns on", m.active))
	case " ":
		m.paused = !m.paused
		if m.paused {
			m.note("animation held — the search is still running")
		} else {
			m.status = ""
		}
	case "a":
		m.open(screenLedger)
	case "r":
		m.open(screenRecipe)
	case "p":
		m.open(screenPasses)
	case "?":
		m.open(screenWheelHelp)
	}
	return m, nil
}

func (m *wheelModel) open(s wheelScreen) {
	m.screen, m.scroll = s, 0
}

// screenKey drives one of the full-screen readers. They all scroll the same way
// and all leave the same way — the key that opened them, esc, or q — because a
// screen you are *in* is somewhere to come back from, not a program to quit.
func (m *wheelModel) screenKey(key string) (tea.Model, tea.Cmd) {
	body := m.screenBody()
	page := max(1, (m.height-4)/2)
	last := max(0, len(body)-max(1, m.height-3))
	switch key {
	case "down", "j":
		m.scroll = min(m.scroll+1, last)
	case "up", "k":
		m.scroll = max(m.scroll-1, 0)
	case "ctrl+d", "pgdown", " ":
		m.scroll = min(m.scroll+page, last)
	case "ctrl+u", "pgup":
		m.scroll = max(m.scroll-page, 0)
	case "g":
		m.scroll = 0
	case "G":
		m.scroll = last
	default:
		m.screen, m.scroll = screenWheel, 0
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Running it
// ---------------------------------------------------------------------------

// runMahoragaWheel drives the search under the wheel and returns the recipe.
//
// It returns rather than reporting, because the verdict belongs on the terminal
// the reader gets back: the wheel takes the alternate screen, and everything
// drawn on it is gone the moment the program exits. What survives is what the
// caller prints afterwards.
func runMahoragaWheel(search *mahoraga.Search, bridge *wheelBridge, prog, input string,
	opts mahoragaOptions, stdin io.Reader, stdout, stderr io.Writer) (*mahoraga.Recipe, int) {

	m := newWheelModel(prog, input, opts)
	m.search, m.bridge = search, bridge

	go func() {
		recipe, err := search.Run()
		bridge.done <- wheelDoneMsg{recipe: recipe, err: err}
	}()

	teaOpts := []tea.ProgramOption{tea.WithOutput(stdout)}
	if f, ok := stdin.(*os.File); ok {
		teaOpts = append(teaOpts, tea.WithInput(f))
	}
	if _, err := tea.NewProgram(m, teaOpts...).Run(); err != nil {
		bridge.release()
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return nil, 1
	}
	bridge.release()

	switch {
	case m.aborted:
		fmt.Fprintln(stderr, "domain: mahoraga was interrupted — nothing was written")
		return nil, 130
	case m.err != nil:
		fmt.Fprintf(stderr, "domain: %v\n", m.err)
		return nil, 1
	case m.recipe == nil:
		// The reader left before the search finished, without asking it to wind
		// down. There is no champion to write and saying so is better than
		// writing a recipe for a search that did not happen.
		fmt.Fprintln(stderr, "domain: the wheel was closed before the search finished — nothing was written")
		return nil, 1
	}
	return m.recipe, 0
}

// shortDuration is interp.FormatDuration, with an em dash for a duration that
// was never measured — the wheel has cells for candidates that failed to build,
// and "0" there would read as "instant".
func shortDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return interp.FormatDuration(d)
}

// clock renders elapsed time as mm:ss, which is what a search is measured in.
func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// trimName shortens a turn name for a narrow roster without losing which turn
// it is: the first word of these names is the distinguishing one.
func trimName(s string, w int) string {
	if len([]rune(s)) <= w {
		return s
	}
	if i := strings.IndexByte(s, ' '); i > 0 && i <= w {
		return s[:i]
	}
	return truncateVis(s, w)
}
