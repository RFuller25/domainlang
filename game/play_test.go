//go:build !js

package game

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
	"domain/prims"
)

// buildPlayModel resolves src and hand-assembles a playModel exactly as
// playGame does, without ever calling tea.NewProgram — so recording can be
// exercised by calling Update/View directly, with no real terminal involved.
//
// The returned Session holds the process-wide interpretation lock
// (interp/session.go) until Close is called — the caller must close it
// before running any other interpretation (runReplay included) in the same
// test, or the second one deadlocks waiting for a lock the first never
// released.
func buildPlayModel(t *testing.T, src string) (*playModel, *interp.Session, *ir.Context, *[]string) {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	optimizer.Optimize(pipe, true)
	g, err := gameOf(pipe)
	if err != nil {
		t.Fatalf("gameOf: %v", err)
	}
	ctx := &ir.Context{}
	sess := interp.NewSession(pipe, ctx)

	var lines []string
	ctx.Record = func(line string) { lines = append(lines, line) }

	m := &playModel{g: g, sess: sess, width: replayCols, height: replayRows,
		box: newRequestBox(), http: httpClient(requestTimeout(g))}
	if _, err := sess.Step(g.setup, nil); err != nil {
		sess.Close()
		t.Fatalf("setup: %v", err)
	}
	w, err := sess.Step(g.world.nodes, ir.NewRecordValue())
	if err != nil {
		sess.Close()
		t.Fatalf("world: %v", err)
	}
	m.world = w
	if g.start != nil {
		if err := m.run(g.start, gameEvent{}); err != nil {
			sess.Close()
			t.Fatalf("start: %v", err)
		}
	}
	return m, sess, ctx, &lines
}

const recordSrc = `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}

Part On "key a":
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.n))
`

// TestRecordCapturesKeysTicksAndFrames checks that Update writes exactly the
// script lines a replay of the same session would need to reproduce it —
// the whole point of --record.
func TestRecordCapturesKeysTicksAndFrames(t *testing.T) {
	m, sess, ctx, linesPtr := buildPlayModel(t, recordSrc)
	defer sess.Close()

	// The seed line is written by playGame itself, before the loop starts —
	// this test bypasses playGame, so write it the same way to check the
	// rest of the sequence in isolation.
	ctx.Record("seed 1")

	m.Update(tickMsg{idx: 0})
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

	lines := *linesPtr
	want := []string{
		"seed 1",
		"tick",
		"frame",
		"key a",
		"text a",
		"frame",
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d\n got: %q\nwant: %q", len(lines), len(want), lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, lines[i], want[i])
		}
	}
}

// TestRecordReplaysToTheSameFrames is the real proof: record a played
// session, close it, then feed the recorded script through the replay half
// and check the frames it draws match the world the played session actually
// reached.
func TestRecordReplaysToTheSameFrames(t *testing.T) {
	m, sess, ctx, linesPtr := buildPlayModel(t, recordSrc)
	ctx.Record("seed 1")

	m.Update(tickMsg{idx: 0})                       // n: 0 -> 1
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}) // n: 1 -> 2
	m.Update(tickMsg{idx: 0})                       // n: 2 -> 3

	// Release the interpretation lock before starting a second one: Session
	// is not reentrant, and runReplay below opens its own.
	sess.Close()

	script := strings.Join(*linesPtr, "\n") + "\n"
	out, err := runReplay(recordSrc, script)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	frames := strings.Split(strings.TrimRight(out, "\n"), "\n\n")
	if len(frames) == 0 {
		t.Fatal("recorded script produced no frames")
	}
	last := frames[len(frames)-1]
	n, err := strconv.Atoi(strings.TrimSpace(last))
	if err != nil {
		t.Fatalf("last frame %q is not a number: %v", last, err)
	}
	if n != 3 {
		t.Errorf("replaying the recorded script ended on n=%d, want 3 (matching the played session)", n)
	}
}

const recordReplySrc = `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {said: ""}

Part On "key enter":
    Domain Expansion: Request
        Url: (w) -> "https://example.test/who"
        As: "who"
        Into: {name: Text}

Part Reply "who":
    Cursed Technique: Apply
        Using: (w) ->
            with(w, "said", if reply.ok then reply.value.name else "(" + reply.error + ")")

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.said)
`

// TestRecordCapturesReplies checks both the success and failure shapes a
// Request's answer can arrive as, and that each recorded line replays back
// to the same world the played session actually reached.
func TestRecordCapturesReplies(t *testing.T) {
	spec := &ir.RequestSpec{Tag: "who", Into: recordReplyInto()}

	m, sess, ctx, linesPtr := buildPlayModel(t, recordReplySrc)
	ctx.Record("seed 1")

	seq := m.box.begin("who")
	m.Update(replyMsg{tag: "who", seq: seq, value: ir.Reply(spec.Into, recordReplyValue("ada"), "")})

	seq = m.box.begin("who")
	m.Update(replyMsg{tag: "who", seq: seq, err: "timed out"})

	sess.Close()

	lines := *linesPtr
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5\n got: %q", len(lines), lines)
	}
	if lines[1] != `reply who {"name":"ada"}` {
		t.Errorf("success reply line: got %q", lines[1])
	}
	if lines[3] != "fail who timed out" {
		t.Errorf("failure reply line: got %q", lines[3])
	}

	script := strings.Join(lines, "\n") + "\n"
	out, err := runReplay(recordReplySrc, script)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	frames := strings.Split(strings.TrimRight(out, "\n"), "\n\n")
	if last := frames[len(frames)-1]; last != "(timed out)" {
		t.Errorf("last frame = %q, want %q", last, "(timed out)")
	}
}

func recordReplyInto() *ir.Type {
	return ir.Record(ir.Field{Name: "name", Type: ir.Text()})
}

func recordReplyValue(name string) ir.Value {
	r := ir.NewRecordValue()
	r.Set("name", name)
	return r
}

const saveLoadSrc = `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Consider save Of
            Domain Expansion: Load
                Path: (w) -> "save.json"
                Into: {highScore: Int}
                Default: (w) -> {highScore: 0}
        Using: (w) -> {highScore: save.highScore}

Part On "key a":
    Cursed Technique: Apply
        Using: (w) -> with(w, "highScore", w.highScore + 1)
    Domain Expansion: Save
        Path: (w) -> "save.json"
        Value: (w) -> {highScore: w.highScore}

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.highScore))
`

// TestPlayedSaveWritesARealFileLoadedByTheNextSession is the real proof for
// --record's counterpart: a played session's Save reaches disk, and the
// next played session's Load (fired from Part World:, which a replayed
// run's `load` line cannot reach — TestLoadIgnoredBeforeTheScriptIsRead)
// reads it back.
func TestPlayedSaveWritesARealFileLoadedByTheNextSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save.json")
	src := strings.Replace(saveLoadSrc, `"save.json"`, strconv.Quote(path), -1)

	m1, sess1, ctx1, _ := buildPlayModel(t, src)
	ctx1.Load = loadFile
	ctx1.Save = func(p, json string) { saveFile(ctx1, p, json) }
	// World already ran (inside buildPlayModel) against whatever Load found
	// before ctx1.Load was wired — redo it now that the hook is in place, the
	// same order playGame itself uses.
	w, err := sess1.Step(m1.g.world.nodes, ir.NewRecordValue())
	if err != nil {
		t.Fatalf("world: %v", err)
	}
	m1.world = w
	if got := viewText(t, m1); got != "0" {
		t.Fatalf("first session's fresh world = %q, want %q", got, "0")
	}
	m1.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if got := viewText(t, m1); got != "1" {
		t.Fatalf("first session after one key = %q, want %q", got, "1")
	}
	sess1.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Save should have written %s: %v", path, err)
	}

	m2, sess2, ctx2, _ := buildPlayModel(t, src)
	defer sess2.Close()
	ctx2.Load = loadFile
	w2, err := sess2.Step(m2.g.world.nodes, ir.NewRecordValue())
	if err != nil {
		t.Fatalf("second session world: %v", err)
	}
	m2.world = w2
	if got := viewText(t, m2); got != "1" {
		t.Errorf("second session's world = %q, want %q (the score the first session saved)", got, "1")
	}
}

// viewText renders a playModel's current Draw output as plain text, for
// tests that only care about the world's value and not a real terminal.
func viewText(t *testing.T, m *playModel) string {
	t.Helper()
	v, err := m.sess.Step(m.g.draw.nodes, m.world)
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	view, ok := v.(*ir.ViewValue)
	if !ok {
		t.Fatalf("Part Draw produced %s rather than a View", ir.DescribeValue(v))
	}
	return ir.RenderViewPlain(view)
}
