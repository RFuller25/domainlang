// The rewrites that need an answer first.
//
// Saving tidies what can be tidied without asking (dev_tidy.go). What is left
// over is left over for one reason each time: the program does not contain a
// name the rewrite needs. `item(st, 9)` does not say what field nine is; two
// Parts that differ by one lambda do not say what their shared computation is
// called. These are the same rewrites with the question attached.
//
// alt+r lists what is available — for the name under the cursor first, then
// for the program — and each entry asks for what it needs on the bottom line
// before changing anything. Every one is a single undo step and goes through
// the same check the save-time rewrites do: the result must resolve, and must
// print values of the types it printed before.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"domain/tidy"
	"domain/token"
)

// ---------------------------------------------------------------------------
// asking
// ---------------------------------------------------------------------------

// devQuestion is one thing to ask on the bottom line.
type devQuestion struct {
	label string // what is being asked for
	hint  string // what it is about, dimmed after the answer
	def   string // the answer an empty Enter gives; "" means an answer is optional
}

// devAsk is a run of questions and what to do with the answers.
type devAsk struct {
	questions []devQuestion
	answers   []string
	text      string
	done      func(m devModel, answers []string) (devModel, tea.Cmd)
}

func (m devModel) askKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := m.ask
	switch msg.String() {
	case "esc", "ctrl+c":
		m.ask = nil
		m.status = "(cancelled)"
		return m, nil
	case "backspace":
		if a.text != "" {
			a.text = a.text[:len(a.text)-1]
		}
		return m, nil
	case "enter":
		answer := strings.TrimSpace(a.text)
		if answer == "" {
			answer = a.questions[len(a.answers)].def
		}
		a.answers = append(a.answers, answer)
		a.text = ""
		if len(a.answers) < len(a.questions) {
			return m, nil
		}
		m.ask = nil
		return a.done(m, a.answers)
	}
	if msg.Text != "" {
		a.text += msg.Text
	}
	return m, nil
}

func (m devModel) askLine() string {
	a := m.ask
	q := a.questions[len(a.answers)]
	hint := q.hint
	if q.def != "" {
		hint += "  (enter for " + q.def + ")"
	}
	if n := len(a.questions); n > 1 {
		hint += fmt.Sprintf("  %d of %d", len(a.answers)+1, n)
	}
	return truncateVis(styHeading.Render(" "+q.label+" ")+" "+a.text+styCursor.Render(" ")+
		styDim.Render("  "+hint), m.width)
}

// applyRewrite puts a rewrite's result in the buffer as one undo step.
func (m devModel) applyRewrite(res tidy.Result, err error) (devModel, tea.Cmd) {
	if err != nil {
		m.status = "not changed: " + err.Error()
		return m, nil
	}
	row, col := m.buf.row, m.buf.col
	m.undo.record(m.buf, false, m.now())
	m.buf.lines = strings.Split(strings.TrimSuffix(res.Src, "\n"), "\n")
	m.buf.row, m.buf.col = min(row, len(m.buf.lines)-1), col
	m.buf.clampCol()
	m.buf.goalCol = m.buf.col
	m.buf.anchor = nil
	m.dirty = true
	m.status = res.Summary()
	m.scrollToCursor()
	return m, m.touched()
}

// ---------------------------------------------------------------------------
// the menu
// ---------------------------------------------------------------------------

// refactorItem is one rewrite on offer.
type refactorItem struct {
	title string
	why   string
	run   func(m devModel) (devModel, tea.Cmd)
}

// devRefactor is the alt+r overlay.
type devRefactor struct {
	items  []refactorItem
	cursor int
}

// openRefactor gathers what can be done to this program, here.
func (m devModel) openRefactor() (tea.Model, tea.Cmd) {
	if m.running {
		m.status = "a run is in progress — try again when it ends"
		return m, nil
	}
	path, src, line, col := m.path, m.buf.text(), m.buf.row+1, m.buf.col
	var items []refactorItem

	frontEndMu.Lock()
	if err := tidy.Check(path, src); err != nil {
		frontEndMu.Unlock()
		m.status = "the program has to check before it can be rewritten: " + firstLineOf(err.Error())
		return m, nil
	}
	if t, ok := tidy.FindRename(path, src, line, col); ok {
		items = append(items, m.renameItem(t))
	}
	for _, ask := range tidy.UnnamedFields(path, src, line) {
		items = append(items, m.nameFieldsItem(ask))
	}
	if sp, ok := tidy.FindSharedParts(path, src); ok {
		items = append(items, m.extractItem(sp))
	}
	for _, c := range tidy.FindLoopCounters(path, src) {
		items = append(items, m.carryItem(c))
	}
	frontEndMu.Unlock()

	if len(items) == 0 {
		m.status = "nothing to refactor here — put the cursor on a name to rename it"
		return m, nil
	}
	m.refactor = &devRefactor{items: items}
	return m, nil
}

func (m devModel) refactorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.refactor
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		m.refactor = nil
	case "down", "ctrl+n", "j":
		r.cursor = (r.cursor + 1) % len(r.items)
	case "up", "ctrl+p", "k":
		r.cursor = (r.cursor + len(r.items) - 1) % len(r.items)
	case "enter":
		m.refactor = nil
		return r.items[r.cursor].run(m)
	}
	return m, nil
}

func (m devModel) refactorView() string {
	r := m.refactor
	var b strings.Builder
	b.WriteString(styTitle.Render("refactor") + "\n")
	b.WriteString(styDim.Render("each is one undo step, and is kept only if the program still checks") + "\n\n")
	for i, it := range r.items {
		marker, head := "  ", it.title
		if i == r.cursor {
			marker, head = styCursor.Render("› "), styKeyword.Render(it.title)
		}
		b.WriteString(truncateVis(marker+head, m.width) + "\n")
		b.WriteString(truncateVis("    "+styDim.Render(it.why), m.width) + "\n\n")
	}
	b.WriteString(styDim.Render("↑/↓ choose · enter go on · esc leave the program alone"))
	return b.String()
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// rewriteLocked runs one rewrite while holding the front end.
func rewriteLocked(f func() (tidy.Result, error)) (tidy.Result, error) {
	frontEndMu.Lock()
	defer frontEndMu.Unlock()
	return f()
}

// ---------------------------------------------------------------------------
// rename
// ---------------------------------------------------------------------------

func (m devModel) renameItem(t tidy.RenameTarget) refactorItem {
	return refactorItem{
		title: fmt.Sprintf("rename %s", t.Name),
		why:   fmt.Sprintf("the %s under the cursor — %s", t.Kind, countOf(t.Uses, "place", "places")),
		run:   func(m devModel) (devModel, tea.Cmd) { return m.askRename(t), nil },
	}
}

func (m devModel) askRename(t tidy.RenameTarget) devModel {
	path, src, line, col := m.path, m.buf.text(), m.buf.row+1, m.buf.col
	m.ask = &devAsk{
		questions: []devQuestion{{
			label: "rename " + t.Name + " to",
			hint:  fmt.Sprintf("%s, %s", t.Kind, countOf(t.Uses, "place", "places")),
		}},
		done: func(m devModel, answers []string) (devModel, tea.Cmd) {
			if answers[0] == "" {
				m.status = "(cancelled)"
				return m, nil
			}
			if m.buf.text() != src {
				m.status = "the program changed — try the rename again"
				return m, nil
			}
			return m.applyRewrite(rewriteLocked(func() (tidy.Result, error) {
				return tidy.Rename(path, src, line, col, answers[0])
			}))
		},
	}
	return m
}

// renameHere is f2: the rename, without the menu.
func (m devModel) renameHere() (tea.Model, tea.Cmd) {
	if m.running {
		m.status = "a run is in progress — try again when it ends"
		return m, nil
	}
	frontEndMu.Lock()
	t, ok := tidy.FindRename(m.path, m.buf.text(), m.buf.row+1, m.buf.col)
	frontEndMu.Unlock()
	if !ok {
		m.status = "no name here that can be renamed — a global, a binding, a parameter, a local or a Shikigami"
		return m, nil
	}
	return m.askRename(t), nil
}

// ---------------------------------------------------------------------------
// naming a tuple's fields
// ---------------------------------------------------------------------------

func (m devModel) nameFieldsItem(ask tidy.FieldAsk) refactorItem {
	return refactorItem{
		title: fmt.Sprintf("name the fields of %s (line %d)", ask.Param, ask.Line),
		why: fmt.Sprintf("%s — %s read through item() with no name; naming them destructures every lambda over this tuple",
			ask.Type, countOf(len(ask.Fields), "field is", "fields are")),
		run: func(m devModel) (devModel, tea.Cmd) {
			path, src := m.path, m.buf.text()
			var qs []devQuestion
			for _, f := range ask.Fields {
				qs = append(qs, devQuestion{
					label: fmt.Sprintf("field %d", f.Index),
					hint:  fmt.Sprintf("item(%s, %d) : %s — empty leaves it unnamed", ask.Param, f.Index, f.Type),
				})
			}
			m.ask = &devAsk{questions: qs, done: func(m devModel, answers []string) (devModel, tea.Cmd) {
				names := map[int]string{}
				for i, a := range answers {
					if a != "" {
						names[ask.Fields[i].Index] = a
					}
				}
				if len(names) == 0 {
					m.status = "(no names given)"
					return m, nil
				}
				if m.buf.text() != src {
					m.status = "the program changed — try again"
					return m, nil
				}
				return m.applyRewrite(rewriteLocked(func() (tidy.Result, error) {
					return tidy.NameFields(path, src, ask.Key, names)
				}))
			}}
			return m, nil
		},
	}
}

// ---------------------------------------------------------------------------
// a Shikigami from what the Parts share
// ---------------------------------------------------------------------------

func (m devModel) extractItem(sp tidy.SharedParts) refactorItem {
	return refactorItem{
		title: "extract what the Parts share into a Shikigami",
		why: fmt.Sprintf("%s run the same %d statements from line %d, except for one %s",
			strings.Join(sp.Labels, " and "), sp.Statements, sp.Line, sp.ParamType),
		run: func(m devModel) (devModel, tea.Cmd) {
			path, src := m.path, m.buf.text()
			m.ask = &devAsk{
				questions: []devQuestion{
					{label: "Shikigami name", hint: "what the shared computation is called"},
					{label: "parameter name", hint: "the one thing that differs : " + sp.ParamType, def: sp.DefaultParam},
				},
				done: func(m devModel, answers []string) (devModel, tea.Cmd) {
					if answers[0] == "" {
						m.status = "(cancelled)"
						return m, nil
					}
					if m.buf.text() != src {
						m.status = "the program changed — try again"
						return m, nil
					}
					return m.applyRewrite(rewriteLocked(func() (tidy.Result, error) {
						return tidy.ExtractShikigami(path, src, answers[0], answers[1])
					}))
				},
			}
			return m, nil
		},
	}
}

// ---------------------------------------------------------------------------
// a counter in its loop
// ---------------------------------------------------------------------------

func (m devModel) carryItem(c tidy.LoopCounter) refactorItem {
	return refactorItem{
		title: fmt.Sprintf("carry %s in the loop on line %d", c.Name, c.Line),
		why: fmt.Sprintf("%s writes it every lap; as part of the loop's own value it becomes a constant afterwards, "+
			"and the stages reading it get their optimizer rewrites back", c.Loop),
		run: func(m devModel) (devModel, tea.Cmd) {
			path, src := m.path, m.buf.text()
			return m.applyRewrite(rewriteLocked(func() (tidy.Result, error) {
				return tidy.CarryInLoop(path, src, c.Name)
			}))
		},
	}
}

// ---------------------------------------------------------------------------
// following a hint to the writes behind it
// ---------------------------------------------------------------------------

// devWriteTour is a walk through the lines that write a global: where the
// performance hint on a stage sends you, since that is where its fix is.
type devWriteTour struct {
	name  string
	lines []int // 1-based
	at    int
	text  string
}

var perfNameRe = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*)`")

// perfGlobals is the globals a line's performance hint names.
func (m devModel) perfGlobals(line int) []string {
	var out []string
	for _, d := range m.intel.diags[line] {
		if d.Code != "perf" || !strings.Contains(d.Msg, "writes after it is declared") {
			continue
		}
		head, _, _ := strings.Cut(d.Msg, ", which")
		for _, mm := range perfNameRe.FindAllStringSubmatch(head, -1) {
			out = append(out, mm[1])
		}
	}
	return out
}

// followWrites is alt+a where the line's advice is "stop writing this
// global": it goes to the writes, one per press. It reports whether it had
// anywhere to go.
func (m devModel) followWrites() (devModel, bool) {
	text := m.buf.text()
	line := m.buf.row + 1

	// Already on a tour of this text: the next stop.
	if t := m.writeTour; t != nil && t.text == text && t.lines[t.at] == line {
		t.at = (t.at + 1) % len(t.lines)
		return m.arriveAtWrite(), true
	}
	names := m.perfGlobals(line)
	if len(names) == 0 || m.intel.text != text {
		return m, false
	}
	for _, name := range names {
		if lines := tidy.GlobalWrites(text, name); len(lines) > 0 {
			m.writeTour = &devWriteTour{name: name, lines: lines, text: text}
			m.tourFrom = line
			return m.arriveAtWrite(), true
		}
	}
	return m, false
}

func (m devModel) arriveAtWrite() devModel {
	t := m.writeTour
	m.buf.gotoLine(t.lines[t.at])
	m.buf.clearSelection()
	more := "alt+a again for the next"
	if len(t.lines) == 1 {
		more = "the only one"
	}
	m.status = fmt.Sprintf("%s is written here (%d of %d, %s) — alt+r can carry it in its loop; line %d reads it",
		t.name, t.at+1, len(t.lines), more, m.tourFrom)
	m.scrollToCursor()
	return m
}

// ---------------------------------------------------------------------------
// the other input
// ---------------------------------------------------------------------------

// sourceTarget is what the program's `Cursed Energy:` line names.
func (m devModel) sourceTarget() string {
	for _, line := range m.buf.lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) >= 14 && strings.EqualFold(trimmed[:14], "cursed energy:") {
			rest := trimmed[14:]
			// Either comment marker ends the target: `#`, or `technically`.
			if i := token.CommentStart(rest); i >= 0 {
				rest = rest[:i]
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// otherInput is the file that pairs with the one the program reads: the
// example for an input, the input for an example. "" when there is none.
//
// A loaded puzzle knows its two files outright. Without one the pair is found
// by name, under the conventions people actually use: `day5.example` beside
// `day5.input`, `example.txt` beside `input.txt`.
func (m devModel) otherInput() string {
	cur := m.sourceTarget()
	if cur == "" || strings.EqualFold(cur, "stdin") {
		return ""
	}
	rel := func(p string) string {
		if r, err := filepath.Rel(m.baseDir(), p); err == nil {
			return r
		}
		return p
	}
	if a := m.aoc; a != nil && a.inputPath != "" && a.examplePath != "" {
		switch cur {
		case rel(a.inputPath):
			return rel(a.examplePath)
		case rel(a.examplePath):
			return rel(a.inputPath)
		}
	}
	ext := filepath.Ext(cur)
	stem := strings.TrimSuffix(cur, ext)
	var cands []string
	swap := func(from, to string) {
		if strings.Contains(cur, from) {
			cands = append(cands, strings.Replace(cur, from, to, 1))
		}
	}
	// Every spelling goes both ways, or the key that left could not return.
	swap("example", "input")
	swap("sample", "input")
	swap("test", "input")
	swap("input", "example")
	swap("input", "sample")
	swap("input", "test")
	cands = append(cands, stem+".example", stem+"_example"+ext, stem+".example"+ext, stem+".input", stem+".txt")
	for _, c := range cands {
		if c == cur {
			continue
		}
		if st, err := os.Stat(filepath.Join(m.baseDir(), c)); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// toggleInput is alt+t: point the program at the other of its two inputs.
func (m devModel) toggleInput() (tea.Model, tea.Cmd) {
	other := m.otherInput()
	if other == "" {
		cur := m.sourceTarget()
		switch {
		case cur == "":
			m.status = "no input yet — ctrl+e chooses one, alt+c fetches a day's"
		default:
			stem := strings.TrimSuffix(cur, filepath.Ext(cur))
			m.status = fmt.Sprintf("nothing pairs with %s — an example would be %s.example beside it", cur, stem)
		}
		return m, nil
	}
	next, _ := m.bindInput(filepath.Join(m.baseDir(), other))
	m = next
	if a := m.aoc; a != nil {
		switch filepath.Join(m.baseDir(), other) {
		case a.examplePath:
			a.bound = "example"
		case a.inputPath:
			a.bound = "input"
		default:
			a.bound = ""
		}
	}
	m.status = "reading " + other + " — alt+t switches back"
	m.scrollToCursor()
	return m, m.touched()
}

// exampleVerdict is what to say beside a run of the example: what the puzzle
// text says the answer is, and whether this run agreed. Nil when there is
// nothing to compare — no puzzle, not the example, or no answer in the prose.
func (m devModel) exampleVerdict(output string) []string {
	a := m.aoc
	if a == nil || a.puzzle == nil || a.bound != "example" {
		return nil
	}
	var out []string
	wants := a.puzzle.ExampleAnswers

	// One unlabelled line is one answer, and nothing says which part it is
	// for. Asking about each part in turn would report the same number
	// against both and call one of them wrong on every run.
	var lines []string
	for _, l := range strings.Split(output, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	if len(lines) == 1 && !strings.HasPrefix(lines[0], "Part ") {
		got, last := lines[0], 0
		for i, want := range wants {
			if want == "" {
				continue
			}
			last = i + 1
			if got == want {
				return []string{
					fmt.Sprintf("example: %s — as the puzzle says for part %d", got, i+1),
					"alt+t switches to your input",
				}
			}
		}
		if last == 0 {
			return nil
		}
		return []string{
			fmt.Sprintf("example: %s — the puzzle says %s for part %d", got, wants[last-1], last),
			"alt+t switches to your input",
		}
	}

	for i, want := range wants {
		if want == "" {
			continue
		}
		got, _, ok := aocAnswerFor(output, i+1)
		switch {
		case !ok:
			continue
		case got == want:
			out = append(out, fmt.Sprintf("example, part %d: %s — as the puzzle says", i+1, got))
		default:
			out = append(out, fmt.Sprintf("example, part %d: %s — the puzzle says %s", i+1, got, want))
		}
	}
	if len(out) > 0 {
		out = append(out, "alt+t switches to your input")
	}
	return out
}

// failedLine is the line the last run died on, if it died and the program has
// not changed since.
func (m devModel) failedLine() (int, bool) {
	if m.trace == nil || m.trace.runErr == nil || strings.Join(m.trace.srcLines, "\n") != m.buf.text() {
		return 0, false
	}
	line := 0
	for l, st := range m.stages {
		if st.Failed && (line == 0 || l < line) {
			line = l
		}
	}
	return line, line > 0
}
