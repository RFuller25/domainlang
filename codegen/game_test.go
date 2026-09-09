package codegen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/codegen"
	// The Game Dev host, which is what makes a replayed frame exist to be
	// diffed. It registers itself against the scope; see game/host.go.
	_ "domain/game"
	"domain/interp"
	"domain/ir"
)

// The interpreter-versus-binary oracle, for games.
//
// A game's output is a picture, so the thing to diff is a frame — and a
// replayed game produces exactly that: the same script on stdin, the same
// virtual clock, the same seed, a deterministic sequence of frames on stdout.
// That is what makes a game testable at all, and it is why the replay host was
// built before the compiler backend rather than after it.
//
// The rule is the one every other lowering in this package lives under: the
// interpreter is the oracle, and the compiled program must agree with it in
// both optimizer modes. A game has more places to disagree than a puzzle
// solver does — a second render tree, a second clock, a second stream, a
// second event table, a second JSON decoder — so the anchors below are chosen
// to touch each of them.

// replayInterpreter is runInterpreter for a program that is not a chain of
// nodes: the scope's own host drives it. Not a terminal, so it replays.
func replayInterpreter(t *testing.T, pipe *ir.Pipeline, script string) string {
	t.Helper()
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: bytes.NewReader([]byte(script)), Stdout: &out}
	if _, err := interp.RunScoped(pipe, ctx); err != nil {
		t.Fatalf("interpreter: %v", err)
	}
	return out.String()
}

// gameAnchors are the programs the oracle runs. Each one is here because it
// reaches something the others do not.
var gameAnchors = []struct {
	name    string
	program string
	script  string
}{
	// The lifecycle itself: a world, a timer, an input handler, a frame, an
	// ending whose Reveal is the output.
	{"counter", `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0, typed: ""}

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "typed", w.typed + key)

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.typed + "|" + totext(w.n))

Part Ending:
    Cursed Technique: Apply
        Using: (w) -> w.n
    Reveal: stdout
`, "frame\ntick\ntick\nframe\nkey a\nframe\nquit\n"},

	// Two timers at different periods, which must fire in due order rather
	// than in declaration order; a specific handler and a catch-all, which
	// must run specific-first; and Quit with a predicate, which ends the run
	// on the world it was handed.
	{"ordering", `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {fast: 0, slow: 0, log: ""}

Part Every 250:
    Cursed Technique: Apply
        Using: (w) -> with(w, "slow", w.slow + 1)

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "fast", w.fast + 1)
    Simple Domain: Quit
        Using: (w) -> w.fast = 7

Part On "key q":
    Cursed Technique: Apply
        Using: (w) -> with(w, "log", w.log + "Q")

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "log", w.log + ".")

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.fast) + "/" + totext(w.slow) + "/" + w.log)

Part Ending:
    Cursed Technique: Apply
        Using: (w) -> "ended at " + totext(w.fast)
    Reveal: stdout
`, "key q\nkey z\nframe\ntick 1000\nframe\n"},

	// The render tree: every View constructor, laid out and rendered plain.
	// Geometry is the one thing a frame diff cannot forgive, so this is the
	// anchor that would catch a column landing one place over.
	{"view", `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {row: fill(5, "."), x: 2}

Part Every 120:
    Cursed Technique: Apply
        Using: (w) -> with(w, "x", mod(w.x + 1, 5))

Part On "key up":
    Cursed Technique: Apply
        Using: (w) -> with(w, "row", concat(concat(take(w.row, w.x), list("#")), drop(w.row, w.x + 1)))

Part On "resize":
    Cursed Technique: Apply
        Using: (w) -> with(w, "x", mod(width + height, 5))

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> box(stack(
            style(text("frame"), "bold red on blue"),
            align(text(textjoin(w.row, "")), 12, 3, "center"),
            beside(text("f="), text(totext(frame())), text(" t="), text(totext(elapsed()))),
            margin(fit(text("0123456789"), 6, 1), 1, 0),
            blank()))
`, "frame\ntick\nkey up\nframe\nsize 9 7\ntick 500\nkey up\nframe\n"},

	// The seeded stream. Two backends, one splitmix64, one default seed — and
	// a `seed` line that changes it mid-run.
	{"chance", `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {rolls: emptylist(0)}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "rolls", concat(w.rolls, list(random(1000))))

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(tojson(w.rolls))

Part Entity "Unused":
    Cursed Technique: Apply
        Using: (w) -> w
`, "tick 300\nframe\nseed 99\ntick 300\nframe\n"},

	// The asynchronous half: a request with a body, a reply decoded into a
	// declared shape, a failure delivered as {ok: false}, and pending() seeing
	// the box that bounds one firing per tag.
	{"request", `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {typed: "", marks: emptylist(""), score: 0, note: "", waiting: ""}

Part On "text":
    Cursed Technique: Apply
        Using: (w) -> with(w, "typed", w.typed + key)
    Domain Expansion: Request
        Url: (w) -> "https://example.test/guess/" + w.typed
        Method: "POST"
        Body: (w) -> tojson(w.typed)
        As: "guess"
        Into: {marks: List<Text>, score: Int}

Part On "key s":
    Cursed Technique: Apply
        Using: (w) -> with(w, "waiting", w.waiting + (if pending("guess") then "Y" else "N"))

Part Reply "guess":
    Cursed Technique: Apply
        Using: (w) -> with(with(with(w, "marks", reply.value.marks),
            "score", reply.value.score), "note", reply.error)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> stack(
            text(w.typed + " " + textjoin(w.marks, ",") + " " + totext(w.score)),
            text(w.note),
            text(w.waiting))
`, "key s\ntext a\nkey s\nframe\nreply guess {\"marks\": [\"G\",\"Y\"], \"score\": 4}\nkey s\nframe\n" +
		"text b\nfail guess server down\nframe\n"},

	// Reading JSON as a stage, which is the other direction and the one that
	// needs a declared type; and a declaration, which a game may have and
	// which has to run before the world is built.
	{"json", `Innate Domain: Game Dev

Cursed Object: doc As "{\"a\": 1, \"b\": [2, 3], \"c\": {\"k\": true}, \"d\": {\"x\": 5}}"

Part World:
    Cursed Technique: Apply
        Using: (w) -> {got: ""}

Part Start:
    Cursed Technique: Apply
        Using: (w) -> doc
    Channeled Energy: Convert From JSON
        Into: {a: Int, b: List<Int>, c: {k: Bool}, d: Map<Text, Int>}
    Cursed Technique: Apply
        Using: (v) -> {got: totext(v.a) + "/" + totext(sum(v.b)) + "/" + tojson(v.c) + "/" + tojson(v.d)}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> w

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.got)
`, "frame\n"},
}

// TestCompiledGamesMatchInterpreter is the rule this whole milestone exists to
// satisfy: a compiled game and a replayed one produce the same frames.
func TestCompiledGamesMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	for _, a := range gameAnchors {
		for _, mode := range []struct {
			name     string
			optimize bool
		}{{"optimized", true}, {"naive", false}} {
			// Resolution and interpretation stay on this goroutine, as the
			// puzzle-solver oracle explains: prims' scopes are package level.
			pipe := compilePipeline(t, a.program, mode.optimize)
			if pipe.Scope != "Game Dev" {
				t.Fatalf("%s resolved in the %q scope", a.name, pipe.Scope)
			}
			want := replayInterpreter(t, pipe, a.script)
			t.Run(a.name+"/"+mode.name, func(t *testing.T) {
				t.Parallel()
				got := buildAndRun(t, pipe, []byte(a.script), codegen.Options{})
				if got != want {
					t.Errorf("compiled frames diverge from the interpreter\n got: %q\nwant: %q", got, want)
				}
			})
		}
	}
}

// TestGameScopeIsCompilable pins the opt-in: the backend must say it can
// compile a game, or every tool that decides what to *attempt* — the
// documentation harness, `domain build` itself — will keep skipping one.
func TestGameScopeIsCompilable(t *testing.T) {
	if !codegen.Compilable("Game Dev") {
		t.Error("the Game Dev scope has a backend but does not say so")
	}
}

// TestCompiledGameNeedsModules: a game names Bubble Tea, so its build gets the
// repository's requirements and hashes rather than the bare go.mod. This is
// the one regression the milestone accepted, and it is asserted rather than
// merely documented.
func TestCompiledGameNeedsModules(t *testing.T) {
	pipe := compilePipeline(t, gameAnchors[0].program, true)
	src, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(src), []byte("charm.land/bubbletea/v2")) {
		t.Error("a compiled game should import Bubble Tea")
	}
}

// The shipped games, compiled and diffed against the interpreter.
//
// The anchors above are chosen to reach each piece of the machinery; these are
// chosen by somebody trying to write a game. They are the load test the
// anchors are not — a real board, a real loop, a real frame — and a divergence
// here is the one a player would actually see.
func TestCompiledExampleGamesMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	dir := filepath.Join("..", "examples", "games")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".domain") {
			continue
		}
		ran++
		base := strings.TrimSuffix(e.Name(), ".domain")
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		script, err := os.ReadFile(filepath.Join(dir, base+".script"))
		if err != nil {
			t.Fatalf("every game needs a sibling .script: %v", err)
		}
		for _, mode := range []struct {
			name     string
			optimize bool
		}{{"optimized", true}, {"naive", false}} {
			pipe := compilePipeline(t, string(src), mode.optimize)
			want := replayInterpreter(t, pipe, string(script))
			t.Run(base+"/"+mode.name, func(t *testing.T) {
				t.Parallel()
				if got := buildAndRun(t, pipe, script, codegen.Options{}); got != want {
					t.Errorf("compiled frames diverge from the interpreter\n got:\n%s\n\nwant:\n%s", got, want)
				}
			})
		}
	}
	if ran == 0 {
		t.Fatal("no games in examples/games/")
	}
}
