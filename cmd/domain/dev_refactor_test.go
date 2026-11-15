package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"domain/aoc"
)

const refactorable = `Cursed Energy: input.txt
Shikigami: Ints
Cursed Object: bump As 10
Cursed Technique: Map Each
    Using: (x) -> x + bump
Maximum Technique: Sum
Reveal: stdout`

// f2 renames the name under the cursor, and one undo takes it back.
func TestDevRename(t *testing.T) {
	m, _ := newClockedModel(refactorable)
	m.buf.row, m.buf.col = 4, 22 // on `bump` inside the lambda
	m = devKey(m, "f2")
	if m.ask == nil {
		t.Fatalf("no prompt; status %q", m.status)
	}
	if line := ansi.Strip(m.bottomLine()); !strings.Contains(line, "rename bump to") || !strings.Contains(line, "global") {
		t.Errorf("prompt is %q", line)
	}
	m = devType(m, "extra")
	m = devKey(m, "enter")
	if got := m.buf.text(); !strings.Contains(got, "Cursed Object: extra As 10") || !strings.Contains(got, "x + extra") {
		t.Fatalf("buffer is\n%s", got)
	}
	if !m.dirty || !strings.Contains(m.status, "renamed bump to extra in 2 places") {
		t.Errorf("dirty=%v status=%q", m.dirty, m.status)
	}
	m = devKey(m, "ctrl+z")
	if m.buf.text() != refactorable {
		t.Error("one undo should withdraw the rename")
	}
}

// A rename that would capture another name is refused and changes nothing.
func TestDevRenameRefusesCapture(t *testing.T) {
	m, _ := newClockedModel(refactorable)
	m.buf.row, m.buf.col = 2, 15
	m = devKey(m, "f2")
	m = devType(m, "x")
	m = devKey(m, "enter")
	if m.buf.text() != refactorable || !strings.Contains(m.status, "not changed") {
		t.Errorf("status %q\n%s", m.status, m.buf.text())
	}
	// Esc cancels the question outright.
	m = devKey(m, "f2")
	m = devType(m, "zz")
	m = devKey(m, "esc")
	if m.ask != nil || m.buf.text() != refactorable {
		t.Error("esc did not cancel the rename")
	}
}

// alt+r lists what the program can be asked to become, and the naming entry
// asks one question per field nobody has named.
func TestDevRefactorNamesTupleFields(t *testing.T) {
	const src = `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Apply
    Using: (js) -> tuple(js, 0, 7)
Cursed Technique: Apply
    Using: (st) -> item(st, 2) + length(item(st, 0))
Reveal: stdout`
	m, _ := newClockedModel(src)
	m.buf.row = 5
	m = devKey(m, "alt+r")
	if m.refactor == nil {
		t.Fatalf("no menu; status %q", m.status)
	}
	view := ansi.Strip(m.view())
	if !strings.Contains(view, "name the fields of st") || !strings.Contains(view, "(List<Int>, Int, Int)") {
		t.Fatalf("menu is\n%s", view)
	}
	m = devKey(m, "enter")
	if m.ask == nil || len(m.ask.questions) != 2 {
		t.Fatalf("questions: %+v", m.ask)
	}
	m = devType(m, "js")
	m = devKey(m, "enter")
	m = devType(m, "limit")
	m = devKey(m, "enter")
	if !strings.Contains(m.buf.text(), "Using: ((js, _, limit)) -> limit + length(js)") {
		t.Errorf("buffer is\n%s", m.buf.text())
	}
}

func TestDevRefactorCarriesACounter(t *testing.T) {
	const src = `Cursed Energy: input.txt
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Object: laps As 0
Simple Domain: While
    Using: (v) -> v > 1
    Cursed Technique: Apply
        Using: (n) -> (n / 2) also laps := laps + 1
Cursed Technique: Apply
    Using: (v) -> laps
Reveal: stdout`
	m, _ := newClockedModel(src)
	m = devKey(m, "alt+r")
	if m.refactor == nil || !strings.Contains(ansi.Strip(m.view()), "carry laps in the loop on line 5") {
		t.Fatalf("menu: %q\n%s", m.status, ansi.Strip(m.view()))
	}
	m = devKey(m, "enter")
	got := m.buf.text()
	for _, want := range []string{"Using: ((v, laps)) -> v > 1", "tuple((n / 2), laps + 1)", "Cursed Object: laps Of ((_, c)) -> c"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestDevRefactorHasNothingToOfferACleanProgram(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: input.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout")
	m = devKey(m, "alt+r")
	if m.refactor != nil || !strings.Contains(m.status, "nothing to refactor") {
		t.Errorf("status %q", m.status)
	}
}

// alt+t goes between the two files a puzzle is worked against.
func TestDevToggleInput(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "day5.input"), []byte("1\n2\n3\n4\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "day5.example"), []byte("5\n7\n"), 0o644)
	m, _ := newClockedModel("Cursed Energy: day5.input\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout")
	m.path = filepath.Join(dir, "p.domain")

	m = devKey(m, "alt+t")
	if m.buf.lines[0] != "Cursed Energy: day5.example" {
		t.Fatalf("line 1 is %q (status %q)", m.buf.lines[0], m.status)
	}
	m = devKey(m, "alt+t")
	if m.buf.lines[0] != "Cursed Energy: day5.input" {
		t.Errorf("line 1 is %q", m.buf.lines[0])
	}

	// With no pair it says what one would be called.
	os.Remove(filepath.Join(dir, "day5.example"))
	m = devKey(m, "alt+t")
	if m.buf.lines[0] != "Cursed Energy: day5.input" || !strings.Contains(m.status, "day5.example") {
		t.Errorf("status %q", m.status)
	}
}

// With a second input beside the first, each vow says whether it holds there.
func TestDevVowsAreComparedWithTheOtherInput(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "day5.input"), []byte("1\n2\n3\n4\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "day5.example"), []byte("5\n7\n"), 0o644)
	m, _ := newClockedModel("Cursed Energy: day5.input\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout")
	m.path = filepath.Join(dir, "p.domain")
	m.buf.row = 1
	next, cmd := m.Update(devKeyMsg("alt+v"))
	m = devSend(next.(devModel), cmd())
	if m.vows == nil {
		t.Fatalf("status %q", m.status)
	}
	view := ansi.Strip(m.view())
	if !strings.Contains(view, "holds on day5.example too") || !strings.Contains(view, "does not hold on day5.example") {
		t.Fatalf("overlay is\n%s", view)
	}
	// `a` takes the ones that are true of the puzzle and leaves the count.
	m = devKey(m, "a")
	if got := m.buf.text(); strings.Contains(got, "Count Equals") || !strings.Contains(got, "All Values > 0") {
		t.Errorf("buffer is\n%s", got)
	}
}

// After a run that died, alt+v is about the value that broke it.
func TestDevVowsAfterAFailedRun(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("3\n\n2\n"), 0o644)
	const src = "Cursed Energy: input.txt\nCursed Technique: Split Text by \"\\n\"\nChanneled Energy: Convert To Integers\nMaximum Technique: Sum\nReveal: stdout"
	m, _ := newClockedModel(src)
	m.path = filepath.Join(dir, "p.domain")
	m.trace = &traceView{runErr: errors.New("3:1: cannot convert"), srcLines: strings.Split(src, "\n")}
	m.stages = map[int]devStage{3: {Line: 3, Failed: true}}

	next, cmd := m.Update(devKeyMsg("alt+v"))
	m = devSend(next.(devModel), cmd())
	if m.vows == nil || !m.vows.offer.Broken {
		t.Fatalf("status %q", m.status)
	}
	if view := ansi.Strip(m.view()); !strings.Contains(view, "false on this run") || !strings.Contains(view, "item 2 of 3 is empty") {
		t.Errorf("overlay is\n%s", view)
	}
	m = devKey(m, "enter")
	if m.buf.lines[2] != "Binding Vow: Holds" || !strings.Contains(m.buf.lines[3], `ikke contains(xs, "")`) {
		t.Errorf("buffer is\n%s", m.buf.text())
	}

	// Once the program is edited the failure is about a program that is gone.
	if _, still := m.failedLine(); still {
		t.Error("a stale failure is still being offered against")
	}
}

// A perf hint about a written global has no repair of its own; alt+a goes to
// where the repair is.
func TestDevFixFollowsAHintToTheWrites(t *testing.T) {
	const src = `Cursed Energy: input.txt
Shikigami: Ints
Cursed Object: seen As 0
Cursed Technique: Map Each
    Using: (x) -> x also seen := seen + 1
Cursed Technique: Map Each
    Using: (x) -> x + seen
Cursed Tool: seen As 0
Maximum Technique: Sum
Reveal: stdout`
	m, _ := newClockedModel(src)
	m = settle(t, m)
	hinted := 0
	for line := range m.intel.diags {
		if len(m.perfGlobals(line)) > 0 && (hinted == 0 || line > hinted) {
			hinted = line
		}
	}
	if hinted == 0 {
		t.Fatalf("no perf hint to follow; diags: %v", m.intel.diags)
	}
	m.buf.gotoLine(hinted)
	m = devKey(m, "alt+a")
	if m.buf.row+1 != 5 || !strings.Contains(m.status, "seen is written here (1 of 2") {
		t.Fatalf("row %d status %q", m.buf.row+1, m.status)
	}
	m = devKey(m, "alt+a")
	if m.buf.row+1 != 8 {
		t.Errorf("second stop is line %d", m.buf.row+1)
	}
	m = devKey(m, "alt+a")
	if m.buf.row+1 != 5 {
		t.Errorf("the walk should wrap; on line %d", m.buf.row+1)
	}
	if m.buf.text() != src || m.dirty {
		t.Error("following a hint changed the program")
	}
}

// A run of the example says what the puzzle says the answer is.
func TestDevExampleVerdict(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: day.example\nReveal: stdout")
	m.aoc = &devAoC{puzzle: &aoc.Puzzle{ExampleAnswers: []string{"24000", "45000"}}, bound: "example"}
	got := strings.Join(m.exampleVerdict("Part 1: 24000\nPart 2: 41000\n"), "\n")
	if !strings.Contains(got, "part 1: 24000 — as the puzzle says") || !strings.Contains(got, "part 2: 41000 — the puzzle says 45000") {
		t.Errorf("verdict:\n%s", got)
	}
	m.aoc.bound = "input"
	if v := m.exampleVerdict("Part 1: 24000\n"); v != nil {
		t.Errorf("a run of the real input was compared with the example's answer: %v", v)
	}
}

// One unlabelled line is one answer; it is not reported as wrong for the
// other part.
func TestDevExampleVerdictForOneUnlabelledLine(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: day.example\nReveal: stdout")
	m.aoc = &devAoC{puzzle: &aoc.Puzzle{ExampleAnswers: []string{"142", "281"}}, bound: "example"}
	got := strings.Join(m.exampleVerdict("281\n"), "\n")
	if !strings.Contains(got, "281 — as the puzzle says for part 2") || strings.Contains(got, "142") {
		t.Errorf("verdict:\n%s", got)
	}
	got = strings.Join(m.exampleVerdict("7\n"), "\n")
	if strings.Count(got, "the puzzle says") != 1 {
		t.Errorf("verdict:\n%s", got)
	}
}

// The toggle goes both ways for every spelling it recognises, and reads past
// either kind of comment.
func TestDevToggleInputReturns(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("2\n"), 0o644)
	m, _ := newClockedModel("Cursed Energy: sample.txt technically the small one\nReveal: stdout")
	m.path = filepath.Join(dir, "p.domain")
	m = devKey(m, "alt+t")
	if m.buf.lines[0] != "Cursed Energy: input.txt" {
		t.Fatalf("line 1 is %q (status %q)", m.buf.lines[0], m.status)
	}
	m = devKey(m, "alt+t")
	if m.buf.lines[0] != "Cursed Energy: sample.txt" {
		t.Errorf("did not switch back: %q (status %q)", m.buf.lines[0], m.status)
	}
}

// While the vow run holds the front end, a key is answered rather than left
// to block on the lock.
func TestDevKeysDoNotBlockOnAVowRun(t *testing.T) {
	m, _ := newClockedModel(refactorable)
	m.vowsBusy = true
	frontEndMu.Lock()
	defer frontEndMu.Unlock()
	done := make(chan devModel, 1)
	go func() { done <- devKey(m, "f2") }()
	select {
	case got := <-done:
		if got.ask != nil || !strings.Contains(got.status, "looking at the parse") {
			t.Errorf("status %q", got.status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the key blocked on the front-end lock")
	}
}

// A rewrite never leaves the cursor inside a character.
func TestDevRewriteKeepsTheCursorOnACharacter(t *testing.T) {
	m, _ := newClockedModel("# an em dash — here\nCursed Energy: input.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal:   stdout")
	m.path = filepath.Join(t.TempDir(), "p.domain")
	m.buf.row, m.buf.col = 0, 15 // the middle byte of the dash
	m.buf.row, m.buf.col = 1, 15
	m.buf.row = 0
	m = devKey(m, "ctrl+s")
	if line := m.buf.line(); m.buf.col < len(line) && !utf8.RuneStart(line[m.buf.col]) {
		t.Errorf("cursor at byte %d of %q is inside a character", m.buf.col, line)
	}
}
