// Package game is the host for `Innate Domain: Game Dev`.
//
// A game is not a chain of nodes, so it cannot be run by walking one: its
// Parts are a world, the events that change it, and a frame drawn from it, and
// something has to decide when each of those happens. That is this file's job.
// It is registered against the scope by name (interp.RegisterHost), because
// interp cannot import prims to ask which host a scope wants — and it lives in
// a package of its own rather than in the CLI because it is language
// machinery. Everything that runs a whole program needs it: the CLI, the
// browser playground, the compiler backend's own oracle. A host reachable only
// from `cmd/domain` would leave every other caller quietly walking a game's
// Parts in source order, which is not an error and not a game.
//
// So it registers itself, and a binary that may run a program imports it for
// that effect — the driver pattern, for the same reason drivers use it.
//
// There are two ways to run a game and they share everything but their edges:
//
//   - **playing**, where events come from a terminal and frames are painted on
//     it (play.go);
//   - **replaying**, where events come from a script and frames are written to
//     stdout as plain text (replay.go).
//
// Replay is not a debugging aid bolted on afterwards; it is what makes a game
// testable at all. A frame diff is to a game what a stdout diff is to a puzzle
// solver, and every gate this repository runs on — the golden harness, the
// interpreter-versus-binary oracle, the documented examples that must execute
// — needs one.
package game

import (
	"fmt"
	"sort"

	"domain/interp"
	"domain/ir"
	"domain/prims"
)

func init() {
	interp.RegisterHost("Game Dev", runGame)
}

// gameRole names the Parts this host knows how to drive. They are the role
// words from prims/scope_gamedev.go; a role added there without a case here
// would be resolved and then silently never run, so parts() refuses one.
const (
	roleWorld  = "World"
	roleStart  = "Start"
	roleOn     = "On"
	roleEvery  = "Every"
	roleReply  = "Reply"
	roleDraw   = "Draw"
	roleEnding = "Ending"
	roleEntity = "Entity"
)

// gamePart is one resolved Part: its role, whatever it was written with, and
// the body to run.
type gamePart struct {
	role   string
	label  string // `Part On "key up":` — the spec
	period int64  // `Part Every 120:` — milliseconds
	nodes  []*ir.Node
	node   *ir.Node
	// desc and out are what a watching tool needs to attribute a stage: the
	// Part as it was written, and what its body produces. A game's Parts are
	// driven by this host rather than reached through the Part node's own
	// Eval, so the frame that node would have pushed has to be pushed here —
	// otherwise every stage of every Part lands in one flat list and a trace
	// cannot say which body a row came from.
	desc string
	out  *ir.Type
	// binds are the values this role's host must put in scope before running
	// the body. The resolver typed the body against exactly these.
	binds []prims.RoleBind
}

// game is a resolved program, indexed by role.
type game struct {
	// setup is every top-level node that is not a Part: the declarations a
	// game is allowed to have (`Cursed Object`, `Cursed Tool`). They run once,
	// before anything else, because a `Part World:` may be built out of one —
	// and a declaration nobody evaluates is a slot the world reads as nothing.
	setup  []*ir.Node
	world  *gamePart
	start  *gamePart
	draw   *gamePart
	ending *gamePart
	on     []*gamePart
	every  []*gamePart
	reply  []*gamePart
	// specs is every request the program can fire, by tag. It is read off the
	// Request nodes wherever they sit — inside a Start, an On, a timer — so
	// the host knows what shape each answer takes without asking the
	// resolver again.
	specs map[string]*ir.RequestSpec
}

// replyFor finds the Part that answers a tag.
func (g *game) replyFor(tag string) *gamePart {
	for _, p := range g.reply {
		if p.label == tag {
			return p
		}
	}
	return nil
}

// gameOf reads the roles off a resolved pipeline.
//
// The Parts are found by walking the node list rather than carried in a field
// of their own: a Part is already a node with its body in Meta, and a second
// place to keep them could disagree with the first. The resolver hoists the
// world, so it is met first here too, but nothing depends on that — this
// indexes by role, not by position.
func gameOf(p *ir.Pipeline) (*game, error) {
	g := &game{specs: map[string]*ir.RequestSpec{}}
	for _, n := range p.Nodes {
		if n == nil {
			continue
		}
		if n.Prim != "Part" {
			g.setup = append(g.setup, n)
			continue
		}
		role, _ := n.Meta["role"].(string)
		part := &gamePart{role: role, node: n, desc: n.Display}
		part.out, _ = n.Meta["bodyType"].(*ir.Type)
		part.label, _ = n.Meta["label"].(string)
		part.nodes, _ = n.Meta["nodes"].([]*ir.Node)
		part.binds, _ = n.Meta["binds"].([]prims.RoleBind)
		if ms, ok := n.Meta["number"].(int64); ok {
			part.period = ms
		}
		switch role {
		case roleWorld:
			g.world = part
		case roleStart:
			g.start = part
		case roleDraw:
			g.draw = part
		case roleEnding:
			g.ending = part
		case roleOn:
			g.on = append(g.on, part)
		case roleEvery:
			g.every = append(g.every, part)
		case roleReply:
			g.reply = append(g.reply, part)
		case roleEntity:
			// A definition, lowered to a Shikigami: it has no node here.
			continue
		default:
			return nil, fmt.Errorf("internal error: this build does not know how to run a Part %s", role)
		}
	}
	if g.world == nil || g.draw == nil {
		// The resolver refuses both of these, so reaching here means a
		// pipeline was built some other way.
		return nil, fmt.Errorf("internal error: a game needs a Part World and a Part Draw")
	}
	ir.CollectRequestSpecs(p.Nodes, g.specs)
	// Timers fire in a fixed order when several fall due at once — earliest
	// period first, then by the order they were written — so that a replayed
	// tick is one answer rather than whichever order a map happened to give.
	sort.SliceStable(g.every, func(i, j int) bool { return g.every[i].period < g.every[j].period })
	return g, nil
}

// handlersFor returns the `Part On` bodies a piece of input matches, in the
// order they were written.
//
// A specific spec and a catch-all both run, specific first: `Part On "key q"`
// and `Part On "key"` are two different questions about one keystroke, and a
// program that asks both means both.
func (g *game) handlersFor(ev gameEvent) []*gamePart {
	var out []*gamePart
	for _, p := range g.on {
		if prims.EventSpecMatches(p.label, ev.kind, ev.name) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return prims.EventSpecPriority(out[i].label) > prims.EventSpecPriority(out[j].label)
	})
	return out
}

// runGame is the host. It picks its mode the way `Read Source` already picks
// between a file and stdin: a terminal means play, anything else means replay.
// So no test and no documented example has to pass a flag, and the browser
// playground — which has neither a terminal nor a filesystem — runs a game
// exactly as the test harness does.
func runGame(p *ir.Pipeline, ctx *ir.Context) (ir.Value, error) {
	g, err := gameOf(p)
	if err != nil {
		return nil, err
	}
	sess := interp.NewSession(p, ctx)
	defer sess.Close()
	if playable(ctx) {
		return playGame(g, sess)
	}
	return replayGame(g, sess)
}

// gameEvent is one thing that happened to the program. What specs may match
// one, and how, is prims' business (prims/gameevent.go): a game's input
// vocabulary is part of the language, not of this host.
type gameEvent struct {
	kind  string   // "key", "text", "resize", "reply"
	name  string   // the key's name, the typed text, or the reply's tag
	w, h  int      // resize only
	reply ir.Value // reply only: {ok, error, value}
}

// bindingsFor pairs a Part's declared bindings with the values this event
// carries. The names and types come from the resolver, so a role that gains a
// binding cannot be forgotten here: the value is looked up by the name the
// body was typed against, and an unknown one is a build-time gap rather than a
// wrong value quietly reaching a lambda.
func bindingsFor(p *gamePart, ev gameEvent) ([]interp.Binding, error) {
	if len(p.binds) == 0 {
		return nil, nil
	}
	out := make([]interp.Binding, 0, len(p.binds))
	for _, b := range p.binds {
		var v ir.Value
		switch b.Name {
		case "key":
			v = ev.name
		case "reply":
			v = ev.reply
		case "width":
			v = int64(ev.w)
		case "height":
			v = int64(ev.h)
		default:
			return nil, fmt.Errorf("internal error: this build has no value for the binding %q on a Part %s", b.Name, p.role)
		}
		out = append(out, interp.Binding{Name: b.Name, Type: b.Type, Value: v})
	}
	return out, nil
}
