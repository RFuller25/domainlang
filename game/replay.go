// Replaying a game: events from a script, frames to stdout.
//
// This is the oracle. A game's output is a picture, and every gate this
// repository runs on compares text — the golden harness, the
// interpreter-versus-binary rule, and the documented examples that must
// execute and match. A replayed game produces exactly that: a deterministic
// sequence of frames on stdout, diffable like any other answer.
//
// It is deterministic because three separate decisions made it so. The clock
// is virtual, advanced only by the script, so a timer fires when the script
// says and never because a machine was slow. The globals freeze once the
// program starts (prims/scope_gamedev.go), so a frame is a function of the
// world alone. And where randomness arrives, its seed is a line of the script
// rather than the time of day.
package game

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"domain/interp"
	"domain/ir"
)

// replayDefaults are the terminal the script gets if it does not say. They
// are a size, not a guess at the user's: a replayed frame must not change
// because it was replayed somewhere else.
const (
	replayCols = 40
	replayRows = 20
	// replaySeed is the stream a script gets when it does not choose one.
	//
	// A fixed constant, not the clock: a replayed run has to give the same
	// answer every time it runs, including a documented example that draws a
	// random number and prints the result. A script that wants a different
	// game says `seed N`.
	replaySeed = 1
)

// replayState is one replayed run.
type replayState struct {
	g     *game
	sess  *interp.Session
	world ir.Value
	out   io.Writer

	nowMS  int64    // the virtual clock
	dueMS  []int64  // when each timer next fires, indexed with g.every
	frames int      // frames written so far, for the separator
	fired  []string // tags a request has gone out under and not yet come back
	specs  map[string]*ir.RequestSpec
	width  int
	height int
	quit   bool
}

// replayGame runs a game from a script on stdin and writes its frames to
// stdout. The value it returns is whatever `Part Ending:` produced, so the
// host's contract is the same as any other run's.
func replayGame(g *game, sess *interp.Session) (ir.Value, error) {
	ctx := sess.Context()
	r := &replayState{
		g: g, sess: sess, out: ctx.Stdout,
		width: replayCols, height: replayRows,
		dueMS: make([]int64, len(g.every)),
		specs: g.specs,
	}
	for i, t := range g.every {
		r.dueMS[i] = t.period
	}

	ctx.Rand = ir.NewRand(replaySeed)
	// A replayed run never touches the network: the script says what came
	// back. That is what makes a multiplayer game testable at all — the two
	// halves of an exchange become two lines of a file — and it is why the
	// examples need no server.
	ctx.Requests = func(call ir.RequestCall) { r.fired = append(r.fired, call.Spec.Tag) }
	ctx.Pending = func(tag string) bool { return slices.Contains(r.fired, tag) }
	if err := r.begin(); err != nil {
		return nil, err
	}
	src := ctx.Stdin
	if src == nil {
		src = strings.NewReader("")
	}
	if err := r.script(src); err != nil {
		return nil, err
	}
	return r.finish()
}

// begin runs the declarations, builds the world and runs Start.
func (r *replayState) begin() error {
	// The declarations first: the world may be built out of one.
	if _, err := r.sess.Step(r.g.setup, nil); err != nil {
		return err
	}
	// The world's body opens with the empty world and says what the world is.
	r.sess.Context().PushFrame(r.g.world.desc, r.g.world.out)
	w, err := r.sess.Step(r.g.world.nodes, ir.NewRecordValue())
	r.sess.Context().PopFrame(w)
	if err != nil {
		return err
	}
	r.world = w
	if r.g.start != nil {
		return r.run(r.g.start)
	}
	return nil
}

// run puts the world through one Part's body and keeps what it produced.
func (r *replayState) run(p *gamePart) error { return r.runWith(p, gameEvent{}) }

// runWith puts the world through one Part's body, with whatever the event
// carried in scope, and keeps what it produced.
func (r *replayState) runWith(p *gamePart, ev gameEvent) error {
	binds, err := bindingsFor(p, ev)
	if err != nil {
		return err
	}
	r.sess.Context().PushFrame(p.desc, p.out)
	w, err := r.sess.StepWith(p.nodes, r.world, binds)
	r.sess.Context().PopFrame(w)
	if err != nil {
		return err
	}
	r.world = w
	// A body that ran `Simple Domain: Quit` is the program saying it is
	// finished. It still handed back a world, and that is the world the
	// ending sees — a game ends *on* a state, not by discarding one.
	if r.sess.Context().Quit {
		r.quit = true
	}
	return nil
}

// finish runs Ending, whose Reveal is the program's output.
func (r *replayState) finish() (ir.Value, error) {
	if r.g.ending == nil {
		return r.world, nil
	}
	r.sess.Context().PushFrame(r.g.ending.desc, r.g.ending.out)
	v, err := r.sess.Step(r.g.ending.nodes, r.world)
	r.sess.Context().PopFrame(v)
	return v, err
}

// frame draws one frame.
//
// Frames are separated by a blank line rather than by a rule, so that a
// one-frame program — which is most documented examples — prints its picture
// and nothing else.
func (r *replayState) frame() error {
	r.sess.Context().PushFrame(r.g.draw.desc, r.g.draw.out)
	v, err := r.sess.Step(r.g.draw.nodes, r.world)
	r.sess.Context().PopFrame(v)
	if err != nil {
		return err
	}
	view, ok := v.(*ir.ViewValue)
	if !ok {
		return fmt.Errorf("Part Draw produced %s rather than a View", ir.DescribeValue(v))
	}
	r.sess.Context().Clock.Drew()
	if r.out == nil {
		r.frames++
		r.sess.Context().Beep = false
		return nil
	}
	if r.frames > 0 {
		fmt.Fprintln(r.out)
	}
	if r.sess.Context().Beep {
		// The bell character itself, with no newline of its own: it rings
		// without displacing a line of the picture, the same way a played
		// terminal would ring it without scrolling.
		fmt.Fprint(r.out, "\a")
		r.sess.Context().Beep = false
	}
	fmt.Fprintln(r.out, ir.RenderViewPlain(view))
	r.frames++
	return nil
}

// event delivers one thing that happened to every handler that answers to it.
func (r *replayState) event(ev gameEvent) error {
	for _, p := range r.g.handlersFor(ev) {
		if err := r.runWith(p, ev); err != nil {
			return err
		}
		if r.quit {
			return nil
		}
	}
	return nil
}

// advance moves the virtual clock forward by ms, firing every timer that
// falls due on the way — in due order, so that a 100ms and a 250ms timer over
// half a second fire in the order they actually would.
func (r *replayState) advance(ms int64) error {
	target := r.nowMS + ms
	for !r.quit {
		next, idx := int64(-1), -1
		for i, due := range r.dueMS {
			if due <= target && (next < 0 || due < next) {
				next, idx = due, i
			}
		}
		if idx < 0 {
			break
		}
		r.sess.Context().Clock.Advance(next - r.nowMS)
		r.nowMS = next
		r.dueMS[idx] = next + r.g.every[idx].period
		if err := r.run(r.g.every[idx]); err != nil {
			return err
		}
	}
	r.sess.Context().Clock.Advance(target - r.nowMS)
	r.nowMS = target
	return nil
}

// tick advances to the next timer that is due, which is what a bare `tick`
// means: let the next thing happen.
func (r *replayState) tick() error {
	if len(r.dueMS) == 0 {
		return nil
	}
	next := r.dueMS[0]
	for _, due := range r.dueMS {
		if due < next {
			next = due
		}
	}
	return r.advance(next - r.nowMS)
}

// script reads the replay script and does what it says.
func (r *replayState) script(src io.Reader) error {
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan() && !r.quit; line++ {
		text := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(text, '#'); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if text == "" {
			continue
		}
		if err := r.command(text); err != nil {
			return fmt.Errorf("replay script line %d: %w", line, err)
		}
	}
	return sc.Err()
}

// command runs one script line.
func (r *replayState) command(text string) error {
	verb, rest, _ := strings.Cut(text, " ")
	rest = strings.TrimSpace(rest)
	switch verb {
	case "frame":
		return r.frame()
	case "quit":
		r.quit = true
		return nil
	case "key":
		if rest == "" {
			return fmt.Errorf("`key` needs a key name, e.g. `key up`")
		}
		return r.event(gameEvent{kind: "key", name: rest})
	case "text":
		if rest == "" {
			return fmt.Errorf("`text` needs something typed, e.g. `text a`")
		}
		return r.event(gameEvent{kind: "text", name: rest})
	case "tick":
		if rest == "" {
			return r.tick()
		}
		ms, err := strconv.ParseInt(rest, 10, 64)
		if err != nil || ms < 0 {
			return fmt.Errorf("`tick` takes a number of milliseconds, got %q", rest)
		}
		return r.advance(ms)
	case "reply", "fail":
		return r.deliverReply(verb == "reply", rest)
	case "seed":
		n, err := strconv.ParseUint(rest, 10, 64)
		if err != nil {
			return fmt.Errorf("`seed` takes a number, got %q", rest)
		}
		// Re-seeding after the world was built is legal and means what it
		// says: everything from here draws from the new stream.
		r.sess.Context().Rand = ir.NewRand(n)
		return nil
	case "load":
		if rest == "" {
			return fmt.Errorf("`load` needs a document, e.g. `load {\"highScore\": 10}`")
		}
		// A replayed run never opens a real file — this is the script
		// standing in for whatever one would have held, on the same
		// terms `reply` stands in for a server. Like `seed`, a line here
		// only reaches a Load fired after it: one before Part World: or
		// Part Start: read it, since both run before the script does.
		r.sess.Context().Load = func(string) (string, bool) { return rest, true }
		return nil
	case "size":
		w, h, err := parseSize(rest)
		if err != nil {
			return err
		}
		r.width, r.height = w, h
		return r.event(gameEvent{kind: "resize", w: w, h: h})
	}
	return fmt.Errorf("unknown replay command %q; the commands are: "+
		"frame, key <name>, text <what>, tick [ms], size <w> <h>, seed <n>, "+
		"reply <tag> <json>, fail <tag> <why>, load <json>, quit", verb)
}

func parseSize(rest string) (int, int, error) {
	f := strings.Fields(rest)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("`size` takes a width and a height, e.g. `size 40 20`")
	}
	w, err1 := strconv.Atoi(f[0])
	h, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("`size` takes two positive numbers, got %q", rest)
	}
	return w, h, nil
}

// deliverReply answers a request the program fired, from the script rather
// than from a server.
//
//	reply guess {"accepted": true, "marks": ["hit"]}
//	fail  guess connection refused
//
// A request the script never answers simply never arrives, which is itself a
// state worth testing: it is what a server going away mid-game looks like, and
// there is no other way to write a test for it.
func (r *replayState) deliverReply(ok bool, rest string) error {
	tag, body, _ := strings.Cut(rest, " ")
	tag, body = strings.TrimSpace(tag), strings.TrimSpace(body)
	if tag == "" {
		return fmt.Errorf("`reply` needs a tag, e.g. `reply scores {\"top\": 3}`")
	}
	part := r.g.replyFor(tag)
	if part == nil {
		return fmt.Errorf("no `Part Reply %q:` in this program", tag)
	}
	spec, ok2 := r.specs[tag]
	if !ok2 {
		return fmt.Errorf("nothing in this program sends a request tagged %q", tag)
	}
	var value ir.Value
	if ok {
		decoded, err := ir.FromJSON(body, spec.Into)
		if err != nil {
			return fmt.Errorf("the reply for %q does not fit %s: %v", tag, spec.Into, err)
		}
		value = ir.Reply(spec.Into, decoded, "")
	} else {
		why := body
		if why == "" {
			why = "the request failed"
		}
		value = ir.Reply(spec.Into, nil, why)
	}
	r.fired = slices.DeleteFunc(r.fired, func(t string) bool { return t == tag })
	return r.runWith(part, gameEvent{kind: "reply", name: tag, reply: value})
}
