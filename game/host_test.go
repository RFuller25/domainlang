package game

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
	"domain/prims"
)

// runReplay runs a Game Dev program against a replay script and returns its
// stdout — the frames, then whatever `Part Ending:` revealed.
//
// It drives the front end itself rather than going through the CLI: this
// package *is* the host, and a test that reached for `domain run` would be
// asserting about argument parsing on the way to asserting about a frame.
func runReplay(src, script string) (string, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return "", fmt.Errorf("lex: %w", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		return "", fmt.Errorf("resolve: %w", err)
	}
	optimizer.Optimize(pipe, true)
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader(script), Stdout: &out}
	_, err = interp.RunScoped(pipe, ctx)
	return out.String(), err
}

// replay is runReplay with test-fatal error handling.
func replay(t *testing.T, src, script string) string {
	t.Helper()
	out, err := runReplay(src, script)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(out, "\n")
}

const counterGame = `Innate Domain: Game Dev

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
`

func TestReplayDrivesAGame(t *testing.T) {
	got := replay(t, counterGame, "frame\ntick\ntick\nframe\nkey a\nframe\nquit\n")
	// Frames are separated by a blank line, and Ending's Reveal follows them.
	want := "|0\n\n|2\n\na|2\n2"
	if got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// A bare `tick` advances to the next timer that is due; `tick N` advances the
// clock by N milliseconds and fires everything that falls due on the way.
func TestReplayClockIsVirtual(t *testing.T) {
	if got, want := replay(t, counterGame, "tick 1000\nframe\nquit\n"), "|10\n10"; got != want {
		t.Errorf("tick 1000: got %q, want %q", got, want)
	}
	if got, want := replay(t, counterGame, "tick\nframe\nquit\n"), "|1\n1"; got != want {
		t.Errorf("bare tick: got %q, want %q", got, want)
	}
}

// Several timers fire in due order, not in declaration order, so a 100ms and
// a 250ms timer over half a second land the way they actually would.
func TestReplayTimersFireInDueOrder(t *testing.T) {
	src := `Innate Domain: Game Dev
Part World:
    Cursed Technique: Apply
        Using: (w) -> {fast: 0, slow: 0}
Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "fast", w.fast + 1)
Part Every 250:
    Cursed Technique: Apply
        Using: (w) -> with(w, "slow", w.slow + 1)
Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.fast) + "/" + totext(w.slow))
`
	if got, want := replay(t, src, "tick 500\nframe\nquit\n"), "5/2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// **The F1 regression, made permanent.** One Shikigami, called from a
// `Part On` — which has `key` in scope — and from a `Part Every`, which does
// not. Role payloads are named bindings for exactly this reason: an ambient
// would have changed every lambda's arity, including inside the inlined
// Shikigami, so the same definition could not serve both.
func TestOneShikigamiServesTwoRoles(t *testing.T) {
	src := `Innate Domain: Game Dev

Part Entity "Bump":
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0, typed: ""}

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "typed", w.typed + key)
    Shikigami: Bump

Part Every 100:
    Shikigami: Bump

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.typed + " n=" + totext(w.n))
`
	if got, want := replay(t, src, "key a\nkey b\ntick\nframe\nquit\n"), "ab n=3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A resize hands the body the new size, under the names its role declared.
func TestResizeBindsWidthAndHeight(t *testing.T) {
	src := `Innate Domain: Game Dev
Part World:
    Cursed Technique: Apply
        Using: (w) -> {size: ""}
Part On "resize":
    Cursed Technique: Apply
        Using: (w) -> with(w, "size", totext(width) + "x" + totext(height))
Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> w
Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.size)
`
	if got, want := replay(t, src, "size 30 10\nframe\nquit\n"), "30x10"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// `Simple Domain: Quit` ends the run where it happens, and the world it
// handed back is the one Ending sees — a game ends *on* a state.
func TestQuitEndsTheRunOnItsWorld(t *testing.T) {
	src := `Innate Domain: Game Dev
Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}
Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)
    Simple Domain: Quit
        Using: (w) -> w.n = 3
Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.n)
Part Ending:
    Cursed Technique: Apply
        Using: (w) -> "ended at " + totext(w.n)
    Reveal: stdout
`
	// The clock is asked for a thousand milliseconds, which is ten ticks. The
	// game stops at three.
	if got, want := replay(t, src, "tick 1000\nframe\nquit\n"), "ended at 3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A specific handler and a catch-all both answer one keystroke, specific
// first: they are two different questions about the same event.
func TestSpecificHandlerRunsBeforeTheCatchAll(t *testing.T) {
	src := `Innate Domain: Game Dev
Part World:
    Cursed Technique: Apply
        Using: (w) -> {log: ""}
Part On "key q":
    Cursed Technique: Apply
        Using: (w) -> with(w, "log", w.log + "Q")
Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "log", w.log + ".")
Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.log)
`
	if got, want := replay(t, src, "key q\nkey z\nframe\nquit\n"), "Q.."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// An empty script still runs the world, draws one frame and ends, so the
// simplest possible game has an output to compare.
func TestEmptyScriptStillDrawsAFrame(t *testing.T) {
	if got, want := replay(t, counterGame, ""), "0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReplayScriptErrors(t *testing.T) {
	for _, c := range []struct{ script, want string }{
		{"wobble\n", "unknown replay command"},
		{"key\n", "needs a key name"},
		{"tick soon\n", "milliseconds"},
		{"size 40\n", "width and a height"},
	} {
		_, err := runReplay(counterGame, c.script)
		if err == nil {
			t.Errorf("script %q was accepted", c.script)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("script %q: error = %v, want it to mention %q", c.script, err, c.want)
		}
	}
}

// A replayed run never opens a real file: Load falls back to Default: unless
// the script says `load <json>`, and — since Part World: and Part Start: run
// before the script is read at all — only a Load fired later, from an On or
// Every handler, can ever see a scripted document. That is the same
// limitation `seed` already has for the same reason.
const loadGame = `Innate Domain: Game Dev
Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: -1}
Part On "key r":
    Cursed Technique: Apply
        Consider save Of
            Domain Expansion: Load
                Path: (w) -> "save.json"
                Into: {n: Int}
                Default: (w) -> {n: 0}
        Using: (w) -> with(w, "n", save.n)
Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.n)
`

func TestLoadUsesDefaultWithNoScriptLine(t *testing.T) {
	if got, want := replay(t, loadGame, "key r\nframe\n"), "0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadReadsAScriptedDocument(t *testing.T) {
	if got, want := replay(t, loadGame, `load {"n": 42}`+"\nkey r\nframe\n"), "42"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A document that does not fit Into: is exactly what a missing save looks
// like — the game starts fresh rather than crashing over a corrupt file.
func TestLoadFallsBackToDefaultOnABadDocument(t *testing.T) {
	if got, want := replay(t, loadGame, `load not-json`+"\nkey r\nframe\n"), "0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadIgnoredBeforeTheScriptIsRead(t *testing.T) {
	src := `Innate Domain: Game Dev
Part World:
    Cursed Technique: Apply
        Consider save Of
            Domain Expansion: Load
                Path: (w) -> "save.json"
                Into: {n: Int}
                Default: (w) -> {n: 0}
        Using: (w) -> {n: save.n}
Part On "key":
    Cursed Technique: Apply
        Using: (w) -> w
Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.n)
`
	// The load line is the very first thing in the script, but Part World:
	// already ran before script() reads any of it.
	if got, want := replay(t, src, `load {"n": 42}`+"\nframe\n"), "0"; got != want {
		t.Errorf("got %q, want %q — a load line cannot reach a Load fired before the script is read", got, want)
	}
}

// A replayed run must never touch a real file: the same script would stop
// giving the same answer on a machine that happened to have one lying
// around.
func TestSaveNeverTouchesDiskDuringReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save.json")
	src := `Innate Domain: Game Dev
Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}
Part On "key s":
    Domain Expansion: Save
        Path: (w) -> "` + strings.ReplaceAll(path, `\`, `\\`) + `"
        Value: (w) -> w.n
Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.n)
`
	if _, err := runReplay(src, "key s\nframe\n"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("Save wrote a real file during replay: %s", path)
	}
}

// TestForEachHandlesAVariableLengthEntityListWithNoCap is the motivating
// case this loop exists for: towerdef.domain's creeps were capped at 6 and
// walked with `Simple Domain: For k in range(6)` plus `if k >= length(...)
// then w else ...` standing in for a real variable-length loop. `For Each`
// needs neither the cap nor the guard — it reads however many entities are
// in the world *this tick*, however many ticks it has grown to.
func TestForEachHandlesAVariableLengthEntityListWithNoCap(t *testing.T) {
	src := `Innate Domain: Game Dev
Part World:
    Cursed Technique: Apply
        Using: (w) -> {creeps: emptylist(0), hits: 0}
Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "creeps", concat(w.creeps, list(1)))
    Simple Domain: For Each c In
        Using: (w) -> w.creeps
        Cursed Technique: Apply
            Using: (w, c, i) -> with(w, "hits", w.hits + 1)
Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(length(w.creeps)) + " creeps, " + totext(w.hits) + " hits")
`
	// Tick 1: one creep spawns, the loop sees it once. Tick 2: a second
	// spawns, and the loop now walks two — never capped, never guarded.
	got := replay(t, src, "tick\nframe\ntick\nframe\ntick\ntick\ntick\ntick\ntick\nframe\n")
	frames := strings.Split(got, "\n\n")
	want := []string{"1 creeps, 1 hits", "2 creeps, 3 hits", "7 creeps, 28 hits"}
	if len(frames) != len(want) {
		t.Fatalf("got %d frames, want %d:\n%s", len(frames), len(want), got)
	}
	for i, w := range want {
		if frames[i] != w {
			t.Errorf("frame %d: got %q, want %q", i, frames[i], w)
		}
	}
}
