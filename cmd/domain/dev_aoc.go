// Advent of Code, inside the editor.
//
// A December day has a shape: read the puzzle, get the example working, run it
// on your own input, find out whether the number is right. Three of those four
// steps used to happen in a browser, and the editor's part of it was the one
// in the middle — so the loop was two windows wide and the answer travelled by
// clipboard.
//
// `alt+c` asks for a year and a day and closes that loop. The description is
// on screen next to the program, the two inputs are on disk beside it, and the
// answer the last run produced is checked against the site without leaving the
// terminal. Package domain/aoc is everything with a URL in it — the caching,
// the session cookie, the rules about not asking twice — and this is the part
// with a keyboard in it.
//
// Three decisions worth stating, because each could reasonably have gone the
// other way:
//
// The example is bound as the program's input, not the real one. It is the
// input a program is *written* against: it is small enough to read, the prose
// walks through it, and the expected answer is in the text. `i` switches to the
// real input for the run that counts, and that is one keystroke rather than the
// order of the workflow.
//
// The answer is read from the run's output rather than typed. `Part "1":` and
// `Part "2":` label their output, so a two-part program says which number
// belongs to which part and there is nothing to guess. Where a program does
// not use Parts the rule is stated on screen ("the only line of output") rather
// than applied silently, because a wrong answer submitted on the editor's
// behalf is worse than no help at all.
//
// Submitting always asks. Everything else here can be undone or repeated;
// sending an answer cannot, it is rate-limited by the site, and it is the one
// action where a stray keystroke costs a minute of waiting. So `s` proposes and
// `y` sends.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/aoc"
)

// devAoC is the puzzle screen: what was fetched, which part is being read, and
// what the last check made of the program's answer.
type devAoC struct {
	client *aoc.Client
	puzzle *aoc.Puzzle
	// part is the one being read, counted from one. It follows the puzzle's own
	// idea of what is being worked on when the screen opens, and `1`/`2` move it
	// after that.
	part int
	top  int

	// inputPath and examplePath are the two files written beside the program;
	// bound says which of them the program currently reads.
	inputPath, examplePath string
	bound                  string

	// report is what the last check or submission said, kept until the next
	// one — a verdict that vanished when the screen was scrolled would be a
	// verdict nobody read.
	report    []string
	reportErr bool
	// pending is an answer proposed for submission, waiting for the `y` that
	// sends it. Empty the rest of the time, which is most of the time.
	pending string
}

// aocPrompt is the year-and-day question, on the bottom line of the editor.
type aocPrompt struct {
	text string
	err  string
}

// ---------------------------------------------------------------------------
// messages and commands
// ---------------------------------------------------------------------------

// aocFetchedMsg is a puzzle arriving from the site or the cache.
type aocFetchedMsg struct {
	client    *aoc.Client
	year, day int
	puzzle    *aoc.Puzzle
	err       error
	// refresh distinguishes re-reading a page (`r`, or the reread that follows
	// a correct answer) from fetching one for the first time: the second binds
	// an input and the first must not move it.
	refresh bool
}

// aocSubmittedMsg is the site's verdict on an answer.
type aocSubmittedMsg struct {
	part    int
	verdict aoc.Verdict
	err     error
}

// aocFetchCmd fetches a day off the event loop, building the client if there
// is not one yet.
//
// The client is built here rather than when the key is pressed so that a
// missing session cookie arrives as an ordinary failed fetch — one path for
// "this did not work", with the whole of the setup instructions on it.
func aocFetchCmd(c *aoc.Client, year, day int, refresh bool) tea.Cmd {
	return func() tea.Msg {
		msg := aocFetchedMsg{client: c, year: year, day: day, refresh: refresh}
		if msg.client == nil {
			client, err := aoc.NewClient()
			if err != nil {
				msg.err = err
				return msg
			}
			msg.client = client
		}
		if refresh {
			msg.puzzle, msg.err = msg.client.Refresh(year, day)
		} else {
			msg.puzzle, msg.err = msg.client.Fetch(year, day)
		}
		return msg
	}
}

// aocSubmitCmd sends one answer.
func aocSubmitCmd(c *aoc.Client, year, day, part int, answer string) tea.Cmd {
	return func() tea.Msg {
		v, err := c.Submit(year, day, part, answer)
		return aocSubmittedMsg{part: part, verdict: v, err: err}
	}
}

// ---------------------------------------------------------------------------
// opening it
// ---------------------------------------------------------------------------

// openAoC is alt+c: the puzzle if one has been fetched, the question if not.
//
// A day outlives its screen, the way a run outlives its monitor. Closing the
// description is dismissing a screen, not putting the puzzle down — so alt+c
// after an esc shows the same day again rather than fetching it a second time.
func (m devModel) openAoC() (tea.Model, tea.Cmd) {
	if m.aocBusy != "" {
		return m, nil
	}
	if m.aoc != nil && m.aoc.puzzle != nil {
		m.aocShowing = true
		return m, nil
	}
	m.aocAsk = &aocPrompt{text: m.aocGuess()}
	return m, nil
}

// aocGuess is what the prompt opens with.
//
// During the event, today: that is what somebody opening this in December
// wants nine times in ten. Otherwise the last day fetched in this session, and
// otherwise the most recent event's first day — never an empty prompt, because
// the format is easier to correct than to remember.
func (m devModel) aocGuess() string {
	now := time.Now()
	if m.aoc != nil && m.aoc.puzzle != nil {
		return fmt.Sprintf("%d %d", m.aoc.puzzle.Year, m.aoc.puzzle.Day)
	}
	if now.Month() == time.December && now.Day() <= 25 {
		return fmt.Sprintf("%d %d", now.Year(), now.Day())
	}
	return fmt.Sprintf("%d 1", aoc.LatestYear(now))
}

// parseYearDay reads what was typed at the prompt.
//
// Every separator someone might reach for is one separator: "2023 7", "2023/7",
// "2023-07" and "2023.7" are the same request. A bare number is a day in the
// most recent event, which is what "12" means in December.
func parseYearDay(s string, now time.Time) (year, day int, err error) {
	fields := strings.FieldsFunc(strings.TrimSpace(strings.ToLower(s)), func(r rune) bool {
		return r == ' ' || r == '/' || r == '-' || r == '.' || r == ':' || r == '\t'
	})
	// "day" is how the site writes it and how people say it; it carries no
	// information here, so it is dropped rather than refused.
	var nums []int
	for _, f := range fields {
		if f == "day" {
			continue
		}
		n, convErr := strconv.Atoi(f)
		if convErr != nil {
			return 0, 0, fmt.Errorf("%q is not a year or a day", f)
		}
		nums = append(nums, n)
	}
	switch len(nums) {
	case 1:
		return aoc.LatestYear(now), nums[0], nil
	case 2:
		return nums[0], nums[1], nil
	default:
		return 0, 0, fmt.Errorf("write a year and a day, like %d 7", aoc.LatestYear(now))
	}
}

// aocPromptKey handles the year-and-day question.
func (m devModel) aocPromptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.aocAsk = nil
		m.status = "(cancelled)"
	case "enter":
		year, day, err := parseYearDay(m.aocAsk.text, time.Now())
		if err == nil {
			err = aoc.Validate(year, day, time.Now())
		}
		if err != nil {
			m.aocAsk.err = err.Error()
			return m, nil
		}
		m.aocAsk = nil
		var client *aoc.Client
		if m.aoc != nil {
			client = m.aoc.client
		}
		m.aocBusy = fmt.Sprintf("advent of code %d day %d…", year, day)
		m.status = m.aocBusy
		return m, aocFetchCmd(client, year, day, false)
	case "backspace":
		if s := m.aocAsk.text; s != "" {
			m.aocAsk.text = s[:len(s)-1]
			m.aocAsk.err = ""
		}
	default:
		if msg.Text != "" {
			m.aocAsk.text += msg.Text
			m.aocAsk.err = ""
		}
	}
	return m, nil
}

// aocPromptLine draws the question on the bottom line, beside whatever it has
// to say about the last thing typed into it.
func (m devModel) aocPromptLine() string {
	line := styHeading.Render(" advent of code ") + " " + m.aocAsk.text + styCursor.Render(" ")
	hint := "  year and day, like " + aoc.DayName(aoc.LatestYear(time.Now()), 7)
	if m.aocAsk.err != "" {
		hint = "  " + m.aocAsk.err
		return truncateVis(line+styErr.Render(hint), m.width)
	}
	return truncateVis(line+styDim.Render(hint), m.width)
}

// ---------------------------------------------------------------------------
// what a fetch leaves behind
// ---------------------------------------------------------------------------

// finishAoCFetch takes delivery of a puzzle: the files go beside the program,
// the example becomes its input, and the description takes the screen.
func (m devModel) finishAoCFetch(msg aocFetchedMsg) (tea.Model, tea.Cmd) {
	m.aocBusy, m.status = "", ""
	if msg.err != nil {
		// The setup instructions for a missing cookie are a page of prose, and
		// the status line is one line — so a failure goes to the output pane,
		// which is the editor's place for something worth reading.
		m.output = &devOutput{
			title: "advent of code",
			lines: wrapLines(msg.err.Error(), max(20, m.width-2)),
			err:   true,
		}
		return m, nil
	}

	screen := m.aoc
	if screen == nil || !msg.refresh {
		screen = &devAoC{}
	}
	screen.client, screen.puzzle = msg.client, msg.puzzle
	screen.report, screen.pending = nil, ""
	if !msg.refresh {
		screen.top = 0
	}
	screen.part = min(max(screen.part, msg.puzzle.Working()), max(msg.puzzle.Unlocked(), 1))

	input, example, err := writeAoCInputs(m.baseDir(), msg.puzzle, msg.refresh)
	if err != nil {
		m.output = &devOutput{title: "advent of code", lines: wrapLines(err.Error(), max(20, m.width-2)), err: true}
		return m, nil
	}
	screen.inputPath, screen.examplePath = input, example
	m.aoc, m.aocShowing = screen, true

	// A refresh is a re-read of the page, not a new day: moving the program's
	// input under it would be answering a question nobody asked.
	if msg.refresh {
		m.status = "re-read " + msg.puzzle.Name()
		return m, nil
	}
	return m.bindAoCInput(example, "example")
}

// bindAoCInput points the program at one of the two files.
func (m devModel) bindAoCInput(path, which string) (tea.Model, tea.Cmd) {
	if path == "" {
		m.aoc.report, m.aoc.reportErr = []string{"there is no " + which + " for this day"}, true
		return m, nil
	}
	next, _ := m.bindInput(path)
	m = next
	m.aoc.bound = which
	m.scrollToCursor()
	return m, m.touched()
}

// writeAoCInputs puts the day's two inputs beside the program.
//
// Beside the program rather than only in the cache, because `Cursed Energy:`
// names a file the program reads and a program that read a path under
// ~/.cache would be a program that only runs on this machine. The cache is
// this tool's; these two files are the project's.
//
// An existing file is left alone: the example is a guess at which listing in
// the description was the worked one, and correcting that guess by hand is a
// reasonable thing to have done. A refresh rewrites both, which is the way to
// ask for the guess again.
func writeAoCInputs(dir string, p *aoc.Puzzle, overwrite bool) (input, example string, err error) {
	write := func(name, body string) (string, error) {
		if body == "" {
			return "", nil
		}
		path := filepath.Join(dir, name)
		if !overwrite {
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return "", fmt.Errorf("writing %s: %w", name, err)
		}
		return path, nil
	}
	base := "aoc-" + p.Name()
	if input, err = write(base+".input", p.Input); err != nil {
		return "", "", err
	}
	if example, err = write(base+".example", p.Example); err != nil {
		return "", "", err
	}
	return input, example, nil
}

// ---------------------------------------------------------------------------
// the answer
// ---------------------------------------------------------------------------

// aocAnswerFor picks the answer for a part out of what a run printed, and says
// where it got it.
//
// A `Part "1":` block labels its own output, which makes a two-part program
// unambiguous — and that is the shape an AoC program in this language wants to
// have. Everything else is a rule that could be wrong, so each one names
// itself: the screen says "the only line of output" or "line 2 of 3", and a
// reader who disagrees can see that they do before anything is sent anywhere.
func aocAnswerFor(output string, part int) (answer, where string, ok bool) {
	var lines []string
	for line := range strings.SplitSeq(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		return "", "", false
	}

	label := fmt.Sprintf("Part %d:", part)
	for _, line := range lines {
		if !strings.HasPrefix(line, label) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, label))
		if rest == "" {
			// `Part "1":` with its value on the lines below is a picture rather
			// than a number, and a picture is not an answer.
			return "", fmt.Sprintf("the %s output is more than one line", label), false
		}
		return rest, fmt.Sprintf("the `Part \"%d\":` line", part), true
	}

	if len(lines) == 1 {
		return lines[0], "the only line of output", true
	}
	if part <= len(lines) {
		return lines[part-1], fmt.Sprintf("line %d of %d", part, len(lines)), true
	}
	return lines[len(lines)-1], fmt.Sprintf("the last of %d lines", len(lines)), true
}

// aocCheck compares the last run's answer with what is known about the puzzle.
//
// Known means one of two things, and both are worth having: the site itself
// once the part is solved (the page carries "your puzzle answer was"), and this
// tool's own record of an answer it saw accepted. Either way the check costs no
// request. When neither knows, the only thing that does is the site, so the
// answer is proposed for submission rather than judged here.
func (m devModel) aocCheck() (devModel, tea.Cmd) {
	a := m.aoc
	a.report, a.reportErr, a.pending = nil, false, ""

	if m.trace == nil {
		a.report, a.reportErr = []string{"nothing has been run yet — ctrl+r runs the program"}, true
		return m, nil
	}
	if m.trace.runErr != nil {
		a.report, a.reportErr = []string{
			"the last run did not finish, so it has no answer to check",
			m.trace.runErr.Error(),
		}, true
		return m, nil
	}
	answer, where, ok := aocAnswerFor(m.trace.revealed, a.part)
	if !ok {
		what := "the run printed nothing to check"
		if where != "" {
			what = where
		}
		a.report, a.reportErr = []string{what}, true
		return m, nil
	}

	known, isKnown := a.puzzle.Answer(a.part)
	if !isKnown && a.client != nil {
		known, isKnown = a.client.Accepted(a.puzzle.Year, a.puzzle.Day, a.part)
	}
	head := fmt.Sprintf("part %d: %s", a.part, answer)
	switch {
	case isKnown && answer == known:
		a.report = []string{head + " — right, and the site already has it", "from " + where}
	case isKnown:
		a.report, a.reportErr = []string{
			head + " — wrong; the site took " + known,
			"from " + where + ", against " + m.aocWhichInput(),
		}, true
	default:
		a.pending = answer
		// What sends it is on the footer, which is pinned and says so in the
		// place the eye goes for a key — so this says what the answer is and
		// leaves the offer to the row that is about keys.
		a.report = []string{
			head + " — the site has not seen this one",
			"from " + where + ", against " + m.aocWhichInput(),
		}
		if tried := a.tried(); len(tried) > 0 {
			a.report = append(a.report, "already refused: "+strings.Join(tried, ", "))
		}
		if a.bound == "example" {
			a.report = append(a.report, "note: the program is reading the example, not your input (i switches)")
		}
	}
	return m, nil
}

// checkAnswer is alt+x from the editor: check what the last run produced,
// against the day that is loaded.
//
// It opens the screen rather than answering on the status line, because a
// check has more to say than one line: which number it took, where it took it
// from, which input the program was reading, and — when the site has not seen
// it yet — the offer to send it.
func (m devModel) checkAnswer() (tea.Model, tea.Cmd) {
	if m.aoc == nil || m.aoc.puzzle == nil {
		m.status = "no puzzle loaded — alt+c fetches a day"
		return m, nil
	}
	m.aocShowing = true
	next, cmd := m.aocCheck()
	return next, cmd
}

// tried is every answer the site has already refused for this part.
func (a *devAoC) tried() []string {
	if a.client == nil {
		return nil
	}
	return a.client.Tried(a.puzzle.Year, a.puzzle.Day, a.part)
}

// aocWhichInput names the file the program is reading, since an answer checked
// against the example is a different claim from one checked against the input.
func (m devModel) aocWhichInput() string {
	switch m.aoc.bound {
	case "example":
		return "the example"
	case "input":
		return "your puzzle input"
	}
	if m.input != "" {
		return m.input
	}
	return "no input"
}

// aocSubmit sends the proposed answer.
func (m devModel) aocSubmit() (tea.Model, tea.Cmd) {
	a := m.aoc
	if m.aocBusy != "" {
		return m, nil // one answer is already on its way
	}
	if a.pending == "" {
		// `s` with nothing proposed checks first: that is what turns an answer
		// into a proposal, and if the check settles the question on its own
		// there is nothing left to send.
		next, cmd := m.aocCheck()
		return next, cmd
	}
	if a.client == nil {
		a.report, a.reportErr = []string{"no session — fetch the day again to set one up"}, true
		return m, nil
	}
	answer := a.pending
	a.pending = ""
	a.report, a.reportErr = []string{"sending " + answer + "…"}, false
	m.aocBusy = "sending " + answer
	return m, aocSubmitCmd(a.client, a.puzzle.Year, a.puzzle.Day, a.part, answer)
}

// finishAoCSubmit takes delivery of a verdict.
//
// A correct answer is followed by a re-read of the page, because the page has
// changed: part two is on it now. That is the one place this fetches without
// being asked to, and it is the one place where not fetching would leave the
// screen showing something that is no longer true.
func (m devModel) finishAoCSubmit(msg aocSubmittedMsg) (tea.Model, tea.Cmd) {
	m.aocBusy = ""
	if m.aoc == nil {
		return m, nil
	}
	a := m.aoc
	if msg.err != nil {
		a.report, a.reportErr = wrapVis(msg.err.Error(), max(20, m.width-4)), true
		return m, nil
	}

	v := msg.verdict
	a.reportErr = !v.Star()
	a.report = append([]string{v.Summary()}, wrapVis(v.Message, max(20, m.width-4))...)
	if !v.Sent {
		a.report = append(a.report, "(decided here — nothing was sent)")
	}
	if v.Star() {
		m.status = "★ " + v.Summary()
		return m, aocFetchCmd(a.client, a.puzzle.Year, a.puzzle.Day, true)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// keys
// ---------------------------------------------------------------------------

// aocKey handles the puzzle screen, which owns the keyboard while it is up.
func (m devModel) aocKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := m.aoc
	body := m.aocBodyHeight()
	last := max(0, len(m.aocBody())-body)

	// The confirmation is checked first and consumes only `y`: any other key
	// declines it, and then goes on to do whatever it would have done.
	if a.pending != "" {
		if msg.String() == "y" {
			return m.aocSubmit()
		}
		a.pending = ""
		a.report = append(a.report, "(not sent)")
	}

	switch msg.String() {
	case "esc", "q", "alt+c":
		m.aocShowing = false
	case "down", "j":
		a.top = min(a.top+1, last)
	case "up", "k":
		a.top = max(a.top-1, 0)
	case "pgdown", " ":
		a.top = min(a.top+body, last)
	case "pgup":
		a.top = max(a.top-body, 0)
	case "home":
		a.top = 0
	case "end":
		a.top = last
	case "1", "2":
		part, _ := strconv.Atoi(msg.String())
		if part > a.puzzle.Unlocked() {
			a.report, a.reportErr = []string{"part two appears once part one is solved"}, true
			break
		}
		a.part, a.top = part, 0
	case "tab":
		if a.puzzle.Unlocked() > 1 {
			a.part, a.top = a.part%a.puzzle.Unlocked()+1, 0
		}
	case "e":
		return m.bindAoCInput(a.examplePath, "example")
	case "i":
		return m.bindAoCInput(a.inputPath, "input")
	case "ctrl+r":
		// Running from here keeps the loop on one screen: read the puzzle,
		// run, check. The monitor draws over this while it goes and hands the
		// screen back when it is dismissed, so nothing is lost by running from
		// the description.
		return m.runProgram()
	case "c":
		next, cmd := m.aocCheck()
		return next, cmd
	case "s":
		return m.aocSubmit()
	case "r":
		if m.aocBusy != "" {
			break
		}
		m.aocBusy = "re-reading " + a.puzzle.Name()
		return m, aocFetchCmd(a.client, a.puzzle.Year, a.puzzle.Day, true)
	case "d":
		if m.aocBusy != "" {
			break
		}
		m.aocShowing, m.aocAsk = false, &aocPrompt{text: m.aocGuess()}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// the screen
// ---------------------------------------------------------------------------

// aocBodyHeight is how many rows the description gets, once the header, the
// report and the footer have taken theirs.
func (m devModel) aocBodyHeight() int {
	used := 4 // title, rule, the part line, a blank
	if len(m.aoc.report) > 0 {
		used += len(m.aoc.report) + 1
	}
	return max(1, m.height-used-1) // and the footer
}

// aocBody is the description, wrapped, as lines.
//
// It is rebuilt per frame rather than cached because the thing it depends on
// is the window width, and a cache keyed on width that nobody invalidates is
// how a resized terminal ends up showing yesterday's layout.
func (m devModel) aocBody() []string {
	w := max(20, m.width-4)
	var out []string
	prev := aoc.Heading
	for _, b := range m.aoc.puzzle.Part(m.aoc.part) {
		kind := b.Kind
		switch b.Kind {
		case aoc.Heading:
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, styHeading.Render(" "+b.Text+" "))
		case aoc.Code:
			out = append(out, "")
			for line := range strings.SplitSeq(b.Text, "\n") {
				out = append(out, styValue.Render("    "+line))
			}
		case aoc.Item:
			if prev != aoc.Item && len(out) > 0 {
				out = append(out, "")
			}
			for i, line := range wrapVis(b.Text, w-4) {
				marker := "  • "
				if i > 0 {
					marker = "    "
				}
				out = append(out, marker+line)
			}
		default:
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, wrapVis(b.Text, w)...)
		}
		prev = kind
	}
	if len(out) == 0 {
		out = []string{styDim.Render("(the description came back empty — r re-reads the page)")}
	}
	return out
}

// aocView draws the whole screen: the day, the description, what the last
// check said, and the keys.
func (m devModel) aocView() string {
	a := m.aoc
	w := max(20, m.width)

	left := styTitle.Render("domain expansion: development") + styDim.Render("  advent of code")
	right := styDim.Render(fmt.Sprintf("%d day %d ", a.puzzle.Year, a.puzzle.Day))
	rows := []string{spread(left, right, w), styRule.Render(strings.Repeat("─", w))}
	rows = append(rows, m.aocHeadline(w), "")

	body := m.aocBody()
	h := m.aocBodyHeight()
	a.top = max(0, min(a.top, max(0, len(body)-h)))
	for i := a.top; i < min(a.top+h, len(body)); i++ {
		rows = append(rows, truncateVis("  "+body[i], w))
	}
	for len(rows) < h+4 {
		rows = append(rows, "")
	}

	if len(a.report) > 0 {
		mark := styHeading.Render(" check ")
		if a.reportErr {
			mark = styErr.Render(" check ")
		}
		rows = append(rows, mark+" "+truncateVis(a.report[0], max(1, w-9)))
		for _, line := range a.report[1:] {
			rows = append(rows, truncateVis("         "+styDim.Render(line), w))
		}
	}

	// The footer is pinned, so the key that leaves is in the same place on a
	// long description and a short one.
	pinned := max(1, m.height-1)
	if len(rows) > pinned {
		rows = rows[:pinned]
	}
	for len(rows) < pinned {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n") + "\n" + m.aocFooter(w)
}

// aocHeadline says which part is being read, how the day is going, and which
// of the two inputs the program is pointed at — the three facts that decide
// what the keys below will do.
func (m devModel) aocHeadline(w int) string {
	a := m.aoc
	title := a.puzzle.Title
	if title == "" {
		title = a.puzzle.Name()
	}
	stars := strings.Repeat("★", a.puzzle.Solved()) + strings.Repeat("☆", 2-a.puzzle.Solved())

	left := styKeyword.Render(title) + styDim.Render(fmt.Sprintf("  part %d of %d  ", a.part, max(a.puzzle.Unlocked(), 1))) + styKey.Render(stars)
	reading := "no input bound"
	if a.bound != "" {
		reading = a.bound + ": " + filepath.Base(m.input)
	}
	if m.aocBusy != "" {
		reading = m.aocBusy
	}
	return spread("  "+left, styDim.Render(reading+" "), w)
}

// aocFooter is the key line.
//
// It sheds keys rather than being cut off. A footer truncated mid-word tells
// you there was something else and not what — so the least important keys go
// first, and the one that leaves is pinned, because a screen whose exit is off
// the edge is a screen someone is stuck on.
func (m devModel) aocFooter(w int) string {
	a := m.aoc
	if a.pending != "" {
		return truncateVis(styKey.Render("  y")+styDim.Render(" sends "+a.pending+" to adventofcode.com")+
			styDim.Render("  ·  any other key leaves it unsent"), w)
	}
	key := func(k, what string) string { return styKey.Render(k) + styDim.Render(" "+what) }
	pair := func(a, b, what string) string {
		return styKey.Render(a) + styDim.Render("/") + styKey.Render(b) + styDim.Render(" "+what)
	}
	parts := []string{
		"  " + key("c", "check"),
		key("s", "submit"),
		pair("e", "i", "example/input"),
		pair("1", "2", "part"),
		key("r", "re-read"),
		key("d", "day"),
	}
	leave := key("esc", "back")
	sep := styDim.Render(" · ")
	for {
		line := strings.Join(append(append([]string{}, parts...), leave), sep)
		if ansi.StringWidth(line) <= w || len(parts) == 0 {
			return truncateVis(line, w)
		}
		parts = parts[:len(parts)-1]
	}
}

// spread puts one thing on the left and one on the right of a row, or drops
// the right one when there is no room for both.
func spread(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return truncateVis(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}
