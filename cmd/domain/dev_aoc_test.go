package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"domain/aoc"
)

// A day in the middle of an event, so a fetch is never refused for asking
// before the puzzle exists.
var aocNow = time.Date(2023, time.December, 26, 12, 0, 0, 0, time.UTC)

const aocSumProgram = "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n"

// aocTestClient is a client pointed at a fake site and a temporary cache.
// Nothing in this file touches the network or the real cache directory.
func aocTestClient(t *testing.T, handler http.HandlerFunc) *aoc.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &aoc.Client{
		Session:  "cookie",
		BaseURL:  srv.URL,
		CacheDir: t.TempDir(),
		HTTP:     srv.Client(),
		Now:      func() time.Time { return aocNow },
	}
}

// aocTestPuzzle is a day as the screen sees one, without a page to parse.
func aocTestPuzzle(answers ...string) *aoc.Puzzle {
	return &aoc.Puzzle{
		Year: 2023, Day: 7, Title: "Camel Cards",
		Parts: [][]aoc.Block{
			{{Kind: aoc.Heading, Text: "Day 7: Camel Cards"}, {Kind: aoc.Para, Text: "Rank every hand."}},
			{{Kind: aoc.Para, Text: "Now J cards are jokers."}},
		},
		Example: "1\n2\n3\n",
		Input:   "10\n20\n30\n",
		Answers: answers,
	}
}

// aocOpen puts a program on disk and delivers a fetched puzzle to it, which is
// the state every test below starts from.
func aocOpen(t *testing.T, p *aoc.Puzzle, client *aoc.Client) devModel {
	t.Helper()
	m := devWriteProgram(t, aocSumProgram, "1\n2\n3\n")
	m.width, m.height = 80, 30
	return devSend(m, aocFetchedMsg{client: client, year: p.Year, day: p.Day, puzzle: p})
}

// aocRun runs the program and comes back to the editor with nothing over it —
// the monitor dismissed and the output pane with it, which is where somebody
// who ran a program and went back to work is standing.
func aocRun(t *testing.T, m devModel) devModel {
	t.Helper()
	m = runToCompletion(t, m)
	if m.output != nil {
		m = devKey(m, "esc")
	}
	return m
}

// ---------------------------------------------------------------------------
// asking for a day
// ---------------------------------------------------------------------------

func TestParseYearDayTakesEverySeparator(t *testing.T) {
	cases := []struct {
		in         string
		year, day  int
		shouldFail bool
	}{
		{in: "2023 7", year: 2023, day: 7},
		{in: "2023/7", year: 2023, day: 7},
		{in: "2023-07", year: 2023, day: 7},
		{in: "2023.7", year: 2023, day: 7},
		{in: " 2023  day 7 ", year: 2023, day: 7},
		{in: "7", year: 2023, day: 7}, // the year comes from the calendar
		{in: "", shouldFail: true},
		{in: "december 7", shouldFail: true},
		{in: "2023 7 1", shouldFail: true},
	}
	for _, c := range cases {
		year, day, err := parseYearDay(c.in, aocNow)
		if c.shouldFail {
			if err == nil {
				t.Errorf("parseYearDay(%q) = %d %d, want a refusal", c.in, year, day)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseYearDay(%q): %v", c.in, err)
			continue
		}
		if year != c.year || day != c.day {
			t.Errorf("parseYearDay(%q) = %d %d, want %d %d", c.in, year, day, c.year, c.day)
		}
	}
}

// alt+c opens the question, typing edits it, and a day that cannot be a puzzle
// is refused at the prompt rather than at the server.
func TestAoCPromptRefusesADayThatIsNotOne(t *testing.T) {
	m := newTestDevModel("")
	m = devKey(m, "alt+c")
	if m.aocAsk == nil {
		t.Fatal("alt+c did not open the question")
	}
	m.aocAsk.text = "2023 40"
	m = devKey(m, "enter")
	if m.aocAsk == nil {
		t.Fatal("the prompt closed on a day that does not exist")
	}
	if !strings.Contains(m.aocAsk.err, "day 25") {
		t.Errorf("prompt error = %q, want it to say what the days are", m.aocAsk.err)
	}
	if !strings.Contains(ansi.Strip(m.view()), "day 25") {
		t.Error("the refusal is not on screen")
	}
}

func TestAoCPromptStartsAFetch(t *testing.T) {
	m := newTestDevModel("")
	m = devKey(m, "alt+c")
	m.aocAsk.text = "2023 7"
	next, cmd := m.Update(devKeyMsg("enter"))
	m = next.(devModel)
	if m.aocAsk != nil {
		t.Error("the prompt stayed open after a day it accepted")
	}
	if cmd == nil {
		t.Fatal("no fetch was started")
	}
	if !strings.Contains(m.aocBusy, "2023") || !strings.Contains(m.aocBusy, "7") {
		t.Errorf("busy = %q, want it to name the day being fetched", m.aocBusy)
	}
}

func TestAoCPromptCancels(t *testing.T) {
	m := devKey(newTestDevModel(""), "alt+c")
	m = devKey(m, "esc")
	if m.aocAsk != nil {
		t.Error("esc did not close the question")
	}
}

// A day named on the command line is checked there, so a typo is a message on
// the terminal rather than a screen that opens and complains.
func TestTheAoCFlagIsReadAndChecked(t *testing.T) {
	_, opts, err := parseDevelopmentArgs([]string{"day7.domain", "--aoc", "2023/7"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.AoCYear != 2023 || opts.AoCDay != 7 {
		t.Errorf("--aoc 2023/7 parsed as %d %d", opts.AoCYear, opts.AoCDay)
	}
	if _, _, err := parseDevelopmentArgs([]string{"--aoc=2023-40"}); err == nil {
		t.Error("--aoc 2023-40 was accepted, and there is no day 40")
	}
	if _, _, err := parseDevelopmentArgs([]string{"--aoc"}); err == nil {
		t.Error("--aoc with nothing after it was accepted")
	}
}

// And it is fetched as the editor opens, rather than opening on a prompt that
// asks for the day that was just given.
//
// With no cookie anywhere the fetch fails without a request, which is what
// makes this safe to run: the message that comes back is the one the editor
// would have shown, and no network was involved in producing it.
func TestTheAoCFlagFetchesOnOpen(t *testing.T) {
	t.Setenv("AOC_SESSION", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	m := newTestDevModel("")
	m.aocWanted = &[2]int{2023, 7}
	var fetched *aocFetchedMsg
	for _, msg := range collectMsgs(m.Init()) {
		if f, ok := msg.(aocFetchedMsg); ok {
			fetched = &f
		}
	}
	if fetched == nil {
		t.Fatal("opening with --aoc did not fetch the day")
	}
	if fetched.year != 2023 || fetched.day != 7 {
		t.Errorf("fetched %d day %d, want 2023 day 7", fetched.year, fetched.day)
	}
}

// ---------------------------------------------------------------------------
// what a fetch leaves behind
// ---------------------------------------------------------------------------

func TestFetchWritesBothInputsAndBindsTheExample(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle(), nil)
	if m.aoc == nil {
		t.Fatal("the puzzle screen did not open")
	}
	dir := filepath.Dir(m.path)
	for _, name := range []string{"aoc-2023-07.input", "aoc-2023-07.example"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not written beside the program: %v", name, err)
		}
	}
	// The example is what a program is written against, so it is the one bound.
	if m.input != "aoc-2023-07.example" {
		t.Errorf("input = %q, want the example bound", m.input)
	}
	if !strings.Contains(m.buf.text(), "Cursed Energy: aoc-2023-07.example") {
		t.Errorf("the source stage was not repointed:\n%s", m.buf.text())
	}
	if m.aoc.bound != "example" {
		t.Errorf("bound = %q, want %q", m.aoc.bound, "example")
	}
}

// The binding goes into the program, so the program still runs the same way
// under `domain run` — and it runs here, which is the proof.
func TestARunReadsTheFetchedExample(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle(), nil)
	m = devKey(m, "esc") // off the puzzle screen, back to the program
	m = runToCompletion(t, m)
	if m.trace == nil {
		t.Fatal("the program did not run")
	}
	if got := strings.TrimSpace(m.trace.revealed); got != "6" {
		t.Errorf("the run printed %q, want 6 — the sum of the example", got)
	}
}

// `i` and `e` move the program between the two files, and both are one
// keystroke because that is the difference between "does it work" and "is it
// right".
func TestTheScreenSwitchesBetweenExampleAndInput(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle(), nil)
	m = devKey(m, "i")
	if m.input != "aoc-2023-07.input" || m.aoc.bound != "input" {
		t.Fatalf("i bound %q (%q), want the real input", m.input, m.aoc.bound)
	}
	m = runToCompletion(t, m)
	if got := strings.TrimSpace(m.trace.revealed); got != "60" {
		t.Errorf("the run printed %q, want 60 — the sum of the puzzle input", got)
	}
	m = devKey(m, "e")
	if m.input != "aoc-2023-07.example" || m.aoc.bound != "example" {
		t.Errorf("e bound %q (%q), want the example back", m.input, m.aoc.bound)
	}
}

// A hand-corrected example is not overwritten by opening the day again: the
// choice of listing is a guess, and correcting it is a reasonable thing to
// have done.
func TestAFetchDoesNotOverwriteAnEditedExample(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle(), nil)
	path := filepath.Join(filepath.Dir(m.path), "aoc-2023-07.example")
	if err := os.WriteFile(path, []byte("9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m = devSend(m, aocFetchedMsg{year: 2023, day: 7, puzzle: aocTestPuzzle()})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "9\n" {
		t.Errorf("the example was overwritten: %q", b)
	}
}

// A failed fetch has a page of setup instructions on it, so it goes where the
// editor puts things worth reading rather than onto the status line.
func TestAFailedFetchExplainsItself(t *testing.T) {
	m := devSend(newTestDevModel(""), aocFetchedMsg{year: 2023, day: 7, err: aoc.ErrNoSession})
	if m.aocShowing {
		t.Error("a failed fetch opened the puzzle screen")
	}
	if m.output == nil {
		t.Fatal("nothing was reported")
	}
	shown := strings.Join(m.output.lines, "\n")
	if !strings.Contains(shown, "AOC_SESSION") {
		t.Errorf("the failure does not say how to set a cookie:\n%s", shown)
	}
	if m.aocBusy != "" {
		t.Errorf("busy = %q after a failure, want it cleared", m.aocBusy)
	}
}

// ---------------------------------------------------------------------------
// the answer
// ---------------------------------------------------------------------------

func TestAoCAnswerFor(t *testing.T) {
	cases := []struct {
		name          string
		output        string
		part          int
		answer, where string
		ok            bool
	}{
		{
			name:   "a Part block labels its own answer",
			output: "Part 1: 6440\nPart 2: 5905", part: 2,
			answer: "5905", where: `the ` + "`" + `Part "2":` + "`" + ` line`, ok: true,
		},
		{
			name:   "one line is the answer whichever part is asked for",
			output: "6440", part: 2,
			answer: "6440", where: "the only line of output", ok: true,
		},
		{
			name:   "unlabelled lines go in order",
			output: "6440\n5905", part: 2,
			answer: "5905", where: "line 2 of 2", ok: true,
		},
		{
			name:   "a part past the end takes the last line",
			output: "6440\n5905\n99", part: 4,
			answer: "99", where: "the last of 3 lines", ok: true,
		},
		{
			name:   "blank lines are not answers",
			output: "\n  \n6440\n", part: 1,
			answer: "6440", where: "the only line of output", ok: true,
		},
		{name: "nothing printed", output: "", part: 1},
		{
			name:   "a picture is not an answer",
			output: "Part 1:\n#..#\n.##.", part: 1,
			where: "the Part 1: output is more than one line",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			answer, where, ok := aocAnswerFor(c.output, c.part)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (answer %q, where %q)", ok, c.ok, answer, where)
			}
			if answer != c.answer {
				t.Errorf("answer = %q, want %q", answer, c.answer)
			}
			if c.where != "" && where != c.where {
				t.Errorf("where = %q, want %q", where, c.where)
			}
		})
	}
}

// The site's own answer, from the page, settles the question without a
// request — which is the check worth having while refactoring a day that is
// already solved.
func TestCheckAgainstTheAnswerTheSiteAlreadyTook(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle("6"), nil)
	m = devKey(m, "1") // the part the answer belongs to
	m = devKey(m, "esc")
	m = aocRun(t, m)

	m = devKey(m, "alt+x")
	if !m.aocShowing {
		t.Fatal("alt+x did not open the puzzle screen")
	}
	report := strings.Join(m.aoc.report, " | ")
	if m.aoc.reportErr {
		t.Errorf("a right answer was reported as wrong: %s", report)
	}
	if !strings.Contains(report, "right") {
		t.Errorf("report = %q, want it to say the answer is right", report)
	}
	if m.aoc.pending != "" {
		t.Errorf("pending = %q — an answer the site already has must not be offered for sending", m.aoc.pending)
	}
}

func TestCheckSaysWhenTheAnswerIsWrong(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle("99"), nil)
	m = devKey(m, "1")
	m = devKey(m, "esc")
	m = aocRun(t, m)
	m = devKey(m, "alt+x")

	report := strings.Join(m.aoc.report, " | ")
	if !m.aoc.reportErr {
		t.Errorf("a wrong answer was not marked wrong: %s", report)
	}
	if !strings.Contains(report, "99") {
		t.Errorf("report = %q, want it to say what the site took", report)
	}
}

func TestCheckNeedsARunFirst(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle(), nil)
	m = devKey(m, "c")
	if !m.aoc.reportErr || !strings.Contains(strings.Join(m.aoc.report, " "), "ctrl+r") {
		t.Errorf("report = %v, want it to say what to press", m.aoc.report)
	}
}

func TestCheckWithoutAPuzzleSaysSo(t *testing.T) {
	m := newTestDevModel(aocSumProgram)
	m = devKey(m, "alt+x")
	if m.aocShowing {
		t.Error("alt+x opened a screen with no puzzle on it")
	}
	if !strings.Contains(m.status, "alt+c") {
		t.Errorf("status = %q, want it to point at alt+c", m.status)
	}
}

// ---------------------------------------------------------------------------
// submitting
// ---------------------------------------------------------------------------

// answering is a fake site that replies to a submission with the given prose.
func answering(t *testing.T, said string) (*aoc.Client, *int) {
	t.Helper()
	var posts int
	c := aocTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		_, _ = w.Write([]byte("<main><article><p>" + said + "</p></article></main>"))
	})
	return c, &posts
}

// An unsolved part is not judged here — the site is the only thing that knows
// — so the answer is proposed, and nothing is sent until it is confirmed.
func TestSubmittingAsksFirst(t *testing.T) {
	client, posts := answering(t, "That's the right answer! You are one gold star closer.")
	m := aocOpen(t, aocTestPuzzle(), client)
	m = devKey(m, "esc")
	m = aocRun(t, m)
	m = devKey(m, "alt+x")

	if m.aoc.pending != "6" {
		t.Fatalf("pending = %q, want the run's answer offered", m.aoc.pending)
	}
	if !strings.Contains(strings.Join(m.aoc.report, " "), "the only line of output") {
		t.Errorf("report = %v, want it to say where the answer came from", m.aoc.report)
	}
	if *posts != 0 {
		t.Fatalf("%d answers were sent before anyone confirmed one", *posts)
	}
	if !strings.Contains(ansi.Strip(m.view()), "y sends 6") {
		t.Error("the footer does not say what y does")
	}

	next, cmd := m.Update(devKeyMsg("y"))
	m = next.(devModel)
	if cmd == nil {
		t.Fatal("y did not send the answer")
	}
	for _, msg := range collectMsgs(cmd) {
		if done, ok := msg.(aocSubmittedMsg); ok {
			m = devSend(m, done)
		}
	}
	if *posts != 1 {
		t.Fatalf("%d answers were sent, want 1", *posts)
	}
	if !m.aoc.reportErr && !strings.Contains(strings.Join(m.aoc.report, " "), "star") {
		t.Errorf("report = %v, want the star", m.aoc.report)
	}
	if !strings.Contains(m.status, "★") {
		t.Errorf("status = %q, want the star on it", m.status)
	}
}

// Any key that is not y declines the proposal, and nothing is sent.
func TestAnswerNotConfirmedIsNotSent(t *testing.T) {
	client, posts := answering(t, "That's the right answer!")
	m := aocOpen(t, aocTestPuzzle(), client)
	m = devKey(m, "esc")
	m = aocRun(t, m)
	m = devKey(m, "alt+x")
	if m.aoc.pending == "" {
		t.Fatal("nothing was proposed to decline")
	}
	m = devKey(m, "n")
	if m.aoc.pending != "" {
		t.Errorf("pending = %q after declining", m.aoc.pending)
	}
	if *posts != 0 {
		t.Errorf("%d answers were sent without a y", *posts)
	}
	if !strings.Contains(strings.Join(m.aoc.report, " "), "not sent") {
		t.Errorf("report = %v, want it to say nothing was sent", m.aoc.report)
	}
}

// A wrong answer comes back with the direction to move in, which is half of
// how a day gets solved.
func TestAWrongAnswerKeepsTheHint(t *testing.T) {
	client, _ := answering(t, "That's not the right answer; your answer is too high. Please wait one minute before trying again.")
	m := aocOpen(t, aocTestPuzzle(), client)
	m = devKey(m, "esc")
	m = aocRun(t, m)
	m = devKey(m, "alt+x")

	next, cmd := m.Update(devKeyMsg("y"))
	m = next.(devModel)
	for _, msg := range collectMsgs(cmd) {
		if done, ok := msg.(aocSubmittedMsg); ok {
			m = devSend(m, done)
		}
	}
	report := strings.Join(m.aoc.report, " ")
	if !m.aoc.reportErr {
		t.Errorf("a refused answer was not marked wrong: %s", report)
	}
	if !strings.Contains(report, "too high") {
		t.Errorf("report = %q, want the direction", report)
	}
}

// ---------------------------------------------------------------------------
// the screen
// ---------------------------------------------------------------------------

func TestThePuzzleScreenShowsTheDayAndTheKeys(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle("6"), nil)
	view := ansi.Strip(m.view())
	for _, want := range []string{
		"advent of code", "2023 day 7", "Camel Cards",
		"part 2 of 2", "jokers", "example: aoc-2023-07.example",
		"c check", "esc back",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen does not show %q:\n%s", want, view)
		}
	}
	// One star: part one is solved and part two is not.
	if !strings.Contains(view, "★☆") {
		t.Errorf("the screen does not show the day's stars:\n%s", view)
	}
}

func TestThePuzzleScreenSwitchesParts(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle("6"), nil)
	if m.aoc.part != 2 {
		t.Errorf("part = %d, want the first unsolved one", m.aoc.part)
	}
	m = devKey(m, "1")
	if m.aoc.part != 1 {
		t.Fatalf("part = %d after pressing 1", m.aoc.part)
	}
	if !strings.Contains(ansi.Strip(m.view()), "Rank every hand.") {
		t.Error("part one's text is not on screen")
	}
	m = devKey(m, "2")
	if !strings.Contains(ansi.Strip(m.view()), "jokers") {
		t.Error("part two's text is not on screen")
	}
}

// Part two does not exist until part one is solved, and asking for it says so
// rather than showing an empty screen.
func TestALockedPartTwoSaysWhy(t *testing.T) {
	p := aocTestPuzzle()
	p.Parts = p.Parts[:1]
	m := aocOpen(t, p, nil)
	m = devKey(m, "2")
	if m.aoc.part != 1 {
		t.Errorf("part = %d, want to have stayed on one", m.aoc.part)
	}
	if !strings.Contains(strings.Join(m.aoc.report, " "), "part one is solved") {
		t.Errorf("report = %v, want it to say why", m.aoc.report)
	}
}

func TestThePuzzleScreenScrollsAndCloses(t *testing.T) {
	p := aocTestPuzzle()
	for i := range 60 {
		p.Parts[0] = append(p.Parts[0], aoc.Block{Kind: aoc.Para, Text: "paragraph " + strings.Repeat("x", i%7+3)})
	}
	m := aocOpen(t, p, nil)
	m = devKey(m, "1")
	m = devKey(m, "down")
	m = devKey(m, "down")
	if m.aoc.top != 2 {
		t.Errorf("top = %d after two downs, want 2", m.aoc.top)
	}
	m = devKey(m, "end")
	if m.aoc.top == 2 {
		t.Error("end did not move to the bottom")
	}
	m = devKey(m, "esc")
	if m.aocShowing {
		t.Error("esc did not close the screen")
	}
	// And it comes back, with the day still loaded — nothing about a fetched
	// puzzle stops being true because the screen was dismissed.
	m = devKey(m, "alt+c")
	if !m.aocShowing || m.aocAsk != nil {
		t.Error("alt+c asked for a day again instead of reopening the one it had")
	}
}

// Every key the screen answers to is on the screen. The footer is the only
// documentation someone reads while using it.
func TestThePuzzleScreenDocumentsItsKeys(t *testing.T) {
	m := aocOpen(t, aocTestPuzzle(), nil)
	footer := ansi.Strip(m.aocFooter(120))
	for _, k := range []string{"c", "s", "e", "i", "1", "2", "r", "d", "esc"} {
		if !strings.Contains(footer, k) {
			t.Errorf("the footer does not mention %q: %s", k, footer)
		}
	}
}
