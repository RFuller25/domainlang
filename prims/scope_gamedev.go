package prims

import (
	"fmt"
	"slices"

	"domain/ast"
	"domain/ir"
)

// `Innate Domain: Game Dev` — a terminal game.
//
// A Game Dev program is **Parts and declarations only**: there is no top-level
// pipeline, because there is no single value flowing through one. That is the
// same rule a library file already lives under (prims/imports.go: "a library
// is a bag of Shikigami, not a program"), and it is the whole of the "use
// strict" effect — no new syntax, one flag.
//
// The Parts are roles, and **every lifecycle role name answers *when***:
// World is first, Start is once before the first frame, On is on input, Every
// is on a timer, Reply is when a request completes, Draw is each frame, Ending
// is last. Entity is the one exception and the one role that is not in the
// lifecycle at all — it is a definition, which is why it reads differently.
//
// What the scope does *not* own is the loop. That belongs to the host
// (interp.RegisterHost), which reads these Parts off the pipeline and drives
// them; the resolver's whole job is to say what each body may be.

// gameDev is the scope.
var gameDev = &Scope{
	Name:    "Game Dev",
	Aliases: []string{"Game"},
	Summary: "A terminal game: a world, events that change it, and a frame drawn from it.",
	// Parts and declarations only.
	TopLevelPipeline: false,
	// Ordered specific-matcher-first, as Core is: "Convert From JSON" and
	// "Convert To JSON" are told apart by their preposition.
	Prims: []*Primitive{quit, beep, request, load, save, fromJSON, toJSON},
	PartRoles: []PartRole{
		{
			Name: "World", Arg: ArgNone, Min: 1, Max: 1,
			DefinesWorld: true,
			Globals:      GlobalsReadWrite,
			// The body opens with the **empty world** — a Record with no
			// fields — and says what the world actually is.
			//
			// The alternative was to open with no value at all, which reads
			// better ("built from nothing") and is what `Cursed Energy` wants
			// as its precondition. It loses, because only `Read Source`
			// accepts a nil input: every other stage, `Apply` included, has
			// nothing to act on, and a game that does not read a file could
			// not state its world at all. Starting from `{}` costs one
			// ignored lambda parameter and makes the common case writable.
			In:  func(*ir.Type) *ir.Type { return ir.Record() },
			Doc: "The world: its shape and its starting value. Runs once, first.",
		},
		{
			Name: "Start", Arg: ArgNone, Min: 0, Max: 1,
			Globals: GlobalsReadWrite,
			In:      worldIn, Out: worldOut,
			Doc: "One-shot setup with the world in hand, before the first frame.",
		},
		{
			Name: "On", Arg: ArgLabel, Min: 0, Max: -1,
			Globals: GlobalsReadOnly,
			In:      worldIn, Out: worldOut,
			// An input handler knows what arrived: the key's name, or the
			// text typed. `key` is a Text either way, so one name covers
			// both spellings of the question.
			Binds: func(arg *ast.PartArg, _ ProgramFacts) ([]RoleBind, error) {
				if arg != nil && arg.Text == "resize" {
					return []RoleBind{
						{Name: "width", Type: ir.Int()},
						{Name: "height", Type: ir.Int()},
					}, nil
				}
				return []RoleBind{{Name: "key", Type: ir.Text()}}, nil
			},
			CheckLabel: func(spec string) error {
				if ValidEventSpec(spec) {
					return nil
				}
				return fmt.Errorf("%q is not something that can happen; the forms are: %s", spec, EventSpecList())
			},
			Doc: "What an input does to the world.",
		},
		{
			Name: "Every", Arg: ArgInt, Min: 0, Max: -1,
			Globals: GlobalsReadOnly,
			In:      worldIn, Out: worldOut,
			Doc: "What the passing of time does to the world, every N milliseconds.",
		},
		{
			Name: "Reply", Arg: ArgLabel, Min: 0, Max: -1,
			Globals: GlobalsReadOnly,
			In:      worldIn, Out: worldOut,
			// The answer is in scope as `reply`, shaped
			// {ok: Bool, error: Text, value: T} — one shape for success and
			// for every way it can fail, so a game has one thing to branch on
			// and "the server is down" is a state it can draw.
			Binds: replyBinds,
			Doc:   "What a completed request does to the world.",
		},
		{
			Name: "Draw", Arg: ArgNone, Min: 1, Max: 1,
			Globals: GlobalsReadOnly,
			In:      worldIn,
			Out:     func(*ir.Type) *ir.Type { return ir.View() },
			Doc:     "The frame, drawn from the world. Runs once per frame and changes nothing.",
		},
		{
			Name: "Ending", Arg: ArgNone, Min: 0, Max: 1,
			Globals: GlobalsReadOnly,
			In:      worldIn,
			Doc:     "What the game leaves behind: whatever this Reveals is the program's output.",
		},
		{
			Name: "Entity", Arg: ArgLabel, Min: 0, Max: -1,
			Globals: GlobalsReadOnly,
			In:      worldIn,
			// An Entity is not run: it is lowered to a Shikigami and called by
			// name. See entityDefs.
			Definition: true,
			Doc:        "A named thing the world is made of, built from the world and called by its name.",
		},
	},
	Shape: gameShape,
}

// replyBinds gives a `Part Reply "<tag>":` body the answer it was written for.
//
// The type comes from the Request that fires the tag, which the resolver
// recorded on its way past — so the two halves of an asynchronous exchange are
// checked against each other rather than merely hoped about. A Reply whose tag
// nothing fires is refused before this is reached.
func replyBinds(arg *ast.PartArg, facts ProgramFacts) ([]RoleBind, error) {
	if arg == nil {
		return nil, nil
	}
	spec, ok := facts.Requests[arg.Text]
	if !ok {
		return nil, fmt.Errorf("nothing sends a request tagged %q — a reply with no question is a Part that never runs", arg.Text)
	}
	return []RoleBind{{Name: "reply", Type: spec.Reply}}, nil
}

// worldIn and worldOut are the contract almost every role shares: given the
// world, produce the world. They are named rather than written out eight times
// because "this role transforms the world" is one idea.
func worldIn(world *ir.Type) *ir.Type  { return world }
func worldOut(world *ir.Type) *ir.Type { return world }

// gameShape is the whole-program check: what a game must have to be one.
//
// It runs after every statement has resolved, so cardinality is already
// settled (PartRole.Min/Max). What is left is the question no single line can
// answer: whether the program does anything.
func gameShape(facts ProgramFacts) error {
	// A request nobody answers is a question asked into the void: the reply
	// arrives and there is no Part for it, so the program silently never
	// reacts. Checked both ways — replyBinds refuses the other direction.
	for tag := range facts.Requests {
		if !slices.Contains(facts.PartLabels["Reply"], tag) {
			return fmt.Errorf("a Request is tagged %q but nothing answers it — "+
				"add `Part Reply %q:`, or the answer arrives and the game never sees it", tag, tag)
		}
	}
	counts := facts.PartCounts
	if counts["On"]+counts["Every"]+counts["Reply"] == 0 {
		return fmt.Errorf("a Game Dev program needs something that changes the world — " +
			"a `Part On \"…\":` for input, a `Part Every N:` for time, or a `Part Reply \"…\":` " +
			"for an answer from a server. With none of them the world is drawn once and never moves")
	}
	return nil
}

// entityDefs converts a program's `Part Entity "Name":` blocks into Shikigami
// definitions, and reports which statements they were.
//
// An Entity lowers to an ordinary Shikigami — its label becomes the name, its
// body becomes the definition — and from there it is called by name, inlined
// at the call site, and optimized through, with no new call machinery, no new
// inlining and no new reserved-name rule. `checkShikigamiName` already refuses
// a label that names a built-in.
//
// The Part itself produces no node: it is a definition, not a stage, so it is
// removed from the statement list rather than resolved.
func entityDefs(sc *Scope, stmts []*ast.Statement) ([]*ast.ShikigamiDef, map[*ast.Statement]bool, error) {
	var defs []*ast.ShikigamiDef
	var taken map[*ast.Statement]bool
	for _, stmt := range stmts {
		if stmt == nil || stmt.Keyword != "Part" || stmt.PartRole == "" {
			continue
		}
		role, ok := sc.roleNamed(stmt.PartRole)
		if !ok || !role.Definition {
			continue
		}
		if stmt.PartName == "" {
			return nil, nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
				"Part %s needs a name, e.g. `Part %s \"Creep\":`", stmt.PartRole, stmt.PartRole)}
		}
		if len(stmt.Block) == 0 {
			return nil, nil, &ResolveError{Pos: stmt.Pos, NeedsBlock: true, Msg: fmt.Sprintf(
				"Part %s %q has an empty body", stmt.PartRole, stmt.PartName)}
		}
		defs = append(defs, &ast.ShikigamiDef{
			Name:  stmt.PartName,
			Body:  stmt.Block,
			Binds: stmt.Binds,
			Pos:   stmt.Pos,
		})
		if taken == nil {
			taken = map[*ast.Statement]bool{}
		}
		taken[stmt] = true
	}
	return defs, taken, nil
}
