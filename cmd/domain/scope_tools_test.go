package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two tools that refuse a game, and the reason they do.
//
// Both are built on one value moving through one chain of stages, and a game
// has no such chain. The failure they avoid is not a crash — the stepper will
// happily render a game's stages as a loop that never finished, and the REPL
// will complain that the one line you typed is missing a `Part World`. Both
// are true and neither is the answer.

const refusalGame = `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.n))
`

func TestVisualizeRefusesAGame(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.domain")
	if err := os.WriteFile(path, []byte(refusalGame), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	code := Visualize(path, visualizeOptions{Optimize: true, InputText: "frame\nquit\n", Plain: true},
		strings.NewReader(""), &out, &errBuf)
	if code == 0 {
		t.Fatalf("visualize accepted a game:\n%s", out.String())
	}
	msg := errBuf.String()
	for _, want := range []string{"not one pipeline", "Game Dev", "domain run"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal should mention %q, got: %s", want, msg)
		}
	}
}

// And it still visualizes an ordinary program, which is the thing the refusal
// must not have broken.
func TestVisualizeStillTakesAPuzzleSolver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.domain")
	src := "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Integers\nMaximum Technique: Sum\nReveal: stdout\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	code := Visualize(path, visualizeOptions{Optimize: true, Plain: true},
		strings.NewReader("1,2,3\n"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("visualize refused a puzzle solver: %s", errBuf.String())
	}
	if !strings.Contains(out.String(), "6") {
		t.Errorf("the trace should show the answer, got:\n%s", out.String())
	}
}

func TestReplRefusesAGameScope(t *testing.T) {
	out := runREPL(t, "Innate Domain: Game Dev\n:quit\n")
	for _, want := range []string{"the REPL", "not one pipeline", "domain run"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal should mention %q, got: %s", want, out)
		}
	}
	// The refusal is about the tool, so it must not be replaced by a
	// diagnostic about the program.
	if strings.Contains(out, "Part World") {
		t.Errorf("the refusal was replaced by a complaint about the program:\n%s", out)
	}
}

// The REPL still takes a scope whose programs *are* one pipeline, written out
// in full — a line nobody needs to type, but one that must not be refused.
func TestReplTakesTheDefaultScopeDeclared(t *testing.T) {
	out := runREPL(t, "Innate Domain: Advent of Code\nCursed Technique: Apply\n:quit\n")
	if strings.Contains(out, "not one pipeline") {
		t.Errorf("the default scope was refused:\n%s", out)
	}
}

func runREPL(t *testing.T, input string) string {
	t.Helper()
	var out bytes.Buffer
	if code := Repl(strings.NewReader(input), &out); code != 0 {
		t.Fatalf("repl exited %d:\n%s", code, out.String())
	}
	return out.String()
}

// And the tools that do *not* care, asserted rather than assumed.
//
// Each of these runs the program through its own host and reads what came out,
// which for a game is its frames. None of them needed a change; this is here
// so that if one of them grows an assumption about a linear pipeline, it fails
// with a sentence rather than with an empty table.

func TestStatsMeasuresAGame(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a binary; skipped in -short mode")
	}
	requireGoToolchain(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "g.domain"), []byte(refusalGame), 0o644); err != nil {
		t.Fatal(err)
	}
	// A game reads its script from stdin. stats runs each program with the
	// input beside it, and a game with none replays an empty script — which
	// still builds the world, draws nothing and ends.
	var out, errBuf bytes.Buffer
	if code := Stats(dir, statsOptions{Runs: 1, Plain: true}, &out, &errBuf); code != 0 {
		t.Fatalf("stats refused a game (%d): %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "g") {
		t.Errorf("the game is missing from the table:\n%s", out.String())
	}
}

func TestCoverageCountsAGamesVocabulary(t *testing.T) {
	dir := t.TempDir()
	src := strings.Replace(refusalGame, "Part Draw:",
		"Part On \"key q\":\n    Simple Domain: Quit\n\nPart Draw:", 1)
	if err := os.WriteFile(filepath.Join(dir, "g.domain"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	if code := Coverage(dir, coverageOptions{Used: true, Plain: true, Only: "prims"}, &out, &errBuf); code != 0 {
		t.Fatalf("coverage refused a game (%d): %s", code, errBuf.String())
	}
	// Quit is a Game Dev primitive. Counting only Core would have left it out
	// of the report in both directions — not in the denominator, and not
	// credited when used.
	if !strings.Contains(out.String(), "Quit") {
		t.Errorf("a scope's own primitive is missing from the report:\n%s", out.String())
	}
}
