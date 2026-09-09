//go:build !js

// Playing a game: events from the terminal, frames painted on it.
//
// The interactive half of the host. It shares everything that matters with
// the replayed half — the same Parts, run in the same order, against the same
// world — and differs only at its edges: where the events come from and where
// the frames go. Bubble Tea supplies both.
//
// Two things it does *not* share, and both are deliberate. The clock is real
// here, so `Part Every 120:` means every 120 milliseconds rather than every
// script line. And Draw runs after every message, because Bubble Tea calls
// View after every Update, where a replay draws only when its script says
// `frame`. So a played session and a replayed one are the same game but not
// the same frame sequence, and only the replayed one is ever compared.
package game

import (
	"fmt"
	"net/http"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"domain/interp"
	"domain/ir"
)

// playable reports whether this run has a terminal to play on. It is the same
// question `Read Source` asks about a file before falling back to stdin, and
// it is asked for the same reason: the program should do the obvious thing
// without being told which mode to use.
func playable(ctx *ir.Context) bool {
	in, ok := ctx.Stdin.(*os.File)
	if !ok || !term.IsTerminal(in.Fd()) {
		return false
	}
	out, ok := ctx.Stdout.(*os.File)
	return ok && term.IsTerminal(out.Fd())
}

// tickMsg is one timer coming due. The index says which, so several timers at
// different periods stay separate.
type tickMsg struct {
	idx int
	at  time.Time
}

// playModel is the Bubble Tea model: the world, and the Parts that change it.
type playModel struct {
	g    *game
	sess *interp.Session

	world ir.Value
	err   error
	done  bool

	// box bounds requests to one in flight per tag; fired queues the ones a
	// body asked for while it was running, because a Part's Eval cannot
	// return a tea.Cmd — it does not know it is inside one.
	box   *requestBox
	http  *http.Client
	fired []ir.RequestCall

	width, height int
}

func playGame(g *game, sess *interp.Session) (ir.Value, error) {
	// Played, the stream starts from the clock, so a game is a different game
	// every time somebody sits down to it. Replayed it starts from a constant
	// or from the script, which is what makes a test reproducible.
	seed := uint64(time.Now().UnixNano())
	sess.Context().Rand = ir.NewRand(seed)
	m := &playModel{g: g, sess: sess, width: replayCols, height: replayRows,
		box: newRequestBox(), http: httpClient(requestTimeout(g))}
	// A recording script must fix the seed a live session drew from the
	// clock — the one thing about this run a replay could not otherwise
	// reproduce — as its first line, before anything else happens.
	m.record("seed %d", seed)
	// A request fired from inside a body is queued here and turned into a
	// command by whichever Update was running: the pipeline has no way to
	// hand one back, and this is the seam that does not need it to.
	sess.Context().Requests = func(call ir.RequestCall) { m.fired = append(m.fired, call) }
	sess.Context().Pending = m.box.pending
	// Playing is the one mode that may touch a real file: a replayed run's
	// state is the script, never the machine it happens to run on.
	sess.Context().Load = loadFile
	sess.Context().Save = func(path, json string) { saveFile(sess.Context(), path, json) }
	// The declarations first, for the same reason the replayed half runs them
	// first: the world may be built out of one.
	if _, err := sess.Step(g.setup, nil); err != nil {
		return nil, err
	}
	w, err := sess.Step(g.world.nodes, ir.NewRecordValue())
	if err != nil {
		return nil, err
	}
	m.world = w
	if g.start != nil {
		if err := m.run(g.start, gameEvent{}); err != nil {
			return nil, err
		}
	}

	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	fm, _ := final.(*playModel)
	if fm == nil {
		fm = m
	}
	if fm.err != nil {
		return nil, fm.err
	}
	// Ending runs after the loop, on the world the game stopped at — outside
	// the alternate screen, so its Reveal lands in the scrollback the player
	// gets back rather than being wiped with the frame.
	if g.ending == nil {
		return fm.world, nil
	}
	return sess.Step(g.ending.nodes, fm.world)
}

// run puts the world through one Part's body with the event's values in
// scope, and records a failure rather than propagating it: a game that fails
// mid-frame has to leave the terminal as it found it, which means unwinding
// through Bubble Tea rather than panicking out of it.
func (m *playModel) run(p *gamePart, ev gameEvent) error {
	binds, err := bindingsFor(p, ev)
	if err != nil {
		return err
	}
	w, err := m.sess.StepWith(p.nodes, m.world, binds)
	if err != nil {
		return err
	}
	m.world = w
	if m.sess.Context().Quit {
		m.done = true
	}
	return nil
}

func (m *playModel) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.g.every)+1)
	for i, t := range m.g.every {
		cmds = append(cmds, timerCmd(i, t.period))
	}
	// `Part World:` and `Part Start:` ran before the loop began, and either
	// may have fired a request — fetching the room, loading a level. Those
	// were queued with nothing to turn them into commands, so this is where
	// they go out.
	if c := m.drainRequests(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// timerCmd schedules one timer's next firing. Each timer reschedules itself
// when it fires, so their periods stay independent.
func timerCmd(idx int, periodMS int64) tea.Cmd {
	d := time.Duration(periodMS) * time.Millisecond
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg{idx: idx, at: t} })
}

func (m *playModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.err != nil || m.done {
		return m, tea.Quit
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.record("size %d %d", msg.Width, msg.Height)
		cmd := m.deliver(gameEvent{kind: "resize", w: msg.Width, h: msg.Height})
		m.record("frame")
		return m, cmd

	case tickMsg:
		if msg.idx < len(m.g.every) {
			m.record("tick")
			m.sess.Context().Clock.Advance(m.g.every[msg.idx].period)
			if err := m.run(m.g.every[msg.idx], gameEvent{}); err != nil {
				m.err = err
				return m, tea.Quit
			}
			m.record("frame")
			if m.done {
				return m, tea.Quit
			}
			return m, tea.Batch(m.drainRequests(), timerCmd(msg.idx, m.g.every[msg.idx].period))
		}

	case replyMsg:
		// A superseded firing's answer arrives late and is dropped: the
		// program asked for newer data and already has a newer question out.
		if !m.box.accept(msg.tag, msg.seq) {
			return m, nil
		}
		part := m.g.replyFor(msg.tag)
		if part == nil {
			return m, nil
		}
		m.recordReply(msg)
		cmd := m.deliver(gameEvent{kind: "reply", name: msg.tag, reply: msg.value})
		m.record("frame")
		if m.done || m.err != nil {
			return m, tea.Quit
		}
		return m, cmd

	case tea.KeyPressMsg:
		name := msg.String()
		// Ctrl+C always leaves, unless the program said what it means by it.
		// A game with no quit handler is the first game anybody writes, and
		// an unkillable one is a bad first impression. A recording still has
		// to say so explicitly: replaying `key ctrl+c` alone would do
		// nothing against a program with no handler for it, since only the
		// replay script's own `quit` verb stops a run early.
		if name == "ctrl+c" && len(m.g.handlersFor(gameEvent{kind: "key", name: name})) == 0 {
			m.record("key %s", name)
			m.record("quit")
			return m, tea.Quit
		}
		m.record("key %s", name)
		cmd := m.deliver(gameEvent{kind: "key", name: name})
		// A printable keystroke is also text, which is what a game asking the
		// player to type reads. Both handlers see it, because they are two
		// different questions about one keypress.
		if !m.done && m.err == nil && msg.Text != "" {
			m.record("text %s", msg.Text)
			if c := m.deliver(gameEvent{kind: "text", name: msg.Text}); c != nil {
				cmd = tea.Batch(cmd, c)
			}
		}
		m.record("frame")
		if m.done || m.err != nil {
			return m, tea.Quit
		}
		return m, cmd
	}
	return m, nil
}

// record writes one replay-script line, if something is listening for them.
func (m *playModel) record(format string, a ...any) {
	if rec := m.sess.Context().Record; rec != nil {
		rec(fmt.Sprintf(format, a...))
	}
}

// recordReply writes the `reply`/`fail` line a request's answer replays as.
// msg.value is already the wrapped {ok, error, value} a Part Reply reads;
// this is that wrapping's inverse, the same shape deliverReply decodes from
// a script line in replay.go.
func (m *playModel) recordReply(msg replyMsg) {
	if msg.err != "" {
		m.record("fail %s %s", msg.tag, msg.err)
		return
	}
	rec, ok := msg.value.(*ir.RecordValue)
	if !ok {
		return
	}
	v, ok := rec.Get("value")
	if !ok {
		return
	}
	j, err := ir.ToJSON(v)
	if err != nil {
		return
	}
	m.record("reply %s %s", msg.tag, j)
}

// deliver runs every handler that answers to an event, and turns whatever
// they asked for into commands.
func (m *playModel) deliver(ev gameEvent) tea.Cmd {
	if ev.kind == "reply" {
		if p := m.g.replyFor(ev.name); p != nil {
			if err := m.run(p, ev); err != nil {
				m.err = err
				return tea.Quit
			}
			if m.done {
				return tea.Quit
			}
		}
		return m.drainRequests()
	}
	for _, p := range m.g.handlersFor(ev) {
		if err := m.run(p, ev); err != nil {
			m.err = err
			return tea.Quit
		}
		if m.done {
			return tea.Quit
		}
	}
	return m.drainRequests()
}

// drainRequests turns everything the bodies just fired into commands.
func (m *playModel) drainRequests() tea.Cmd {
	if len(m.fired) == 0 {
		return nil
	}
	calls := m.fired
	m.fired = nil
	cmds := make([]tea.Cmd, 0, len(calls))
	for _, call := range calls {
		seq := m.box.begin(call.Spec.Tag)
		cmds = append(cmds, func() tea.Msg { return sendRequest(m.http, call, seq) })
	}
	return tea.Batch(cmds...)
}

// loadFile is Load's disk-reading half for a played session: the file's raw
// bytes, or "no save" for anything that stops it being read — missing,
// unreadable, whatever went wrong. Load's own Default: is what a program
// shows either way; there is no reason to tell the two apart from inside the
// game, only from a person's own filesystem.
func loadFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// saveFile is Save's disk-writing half. A failure has nowhere else to go —
// there is no Reply to carry it back to the program, unlike a Request — so
// it is noted on stderr and otherwise swallowed: a game that cannot save
// should keep playing, not crash over it.
func saveFile(ctx *ir.Context, path, json string) {
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		stderr := ctx.Stderr
		if stderr == nil {
			stderr = os.Stderr
		}
		fmt.Fprintf(stderr, "save %s: %v\n", path, err)
	}
}

// requestTimeout is the longest any of a program's requests declared, which is
// what the shared client is given; a shorter one is enforced per call by the
// server answering or not.
func requestTimeout(g *game) int64 {
	longest := int64(5000)
	for _, spec := range g.specs {
		if spec.TimeoutMS > longest {
			longest = spec.TimeoutMS
		}
	}
	return longest
}

func (m *playModel) View() tea.View {
	v, err := m.sess.Step(m.g.draw.nodes, m.world)
	if err != nil {
		// Painting is not the place to fail loudly: the frame says what went
		// wrong and the next Update leaves.
		m.err = err
		return tea.NewView(err.Error())
	}
	view, ok := v.(*ir.ViewValue)
	if !ok {
		m.err = fmt.Errorf("Part Draw produced %s rather than a View", ir.DescribeValue(v))
		return tea.NewView(m.err.Error())
	}
	m.sess.Context().Clock.Drew()
	s := renderViewStyled(view)
	if m.sess.Context().Beep {
		s = "\a" + s
		m.sess.Context().Beep = false
	}
	out := tea.NewView(s)
	out.AltScreen = true
	return out
}
