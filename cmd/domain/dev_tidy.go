// Tidying on save, and offering Binding Vows.
//
// Both are package tidy; this is their presentation.
//
// ctrl+s tidies and then saves. The rewrites are the ones that are safe to
// make uninvited — each is checked against the type checker before it is kept,
// and a program that does not check is saved exactly as typed — but "safe" is
// not the same as "wanted", so the save is one undo step like any other edit.
// ctrl+z puts the program back as it was written, and saving *that* text again
// saves it as written: undoing the tidy is how you decline it, and the editor
// does not ask twice about a program it has already been told to leave alone.
//
// Vows are a different kind of thing and get a different key. A rewrite keeps
// the program's meaning; a vow adds a claim about the input, and only the
// author knows whether "this list has 1000 lines" is a fact about the puzzle
// or about today's file. So alt+v runs the program as far as the parse, shows
// what is true of the value there, and inserts only what is chosen.
package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"domain/tidy"
)

// devVowTimeout is how long the run behind alt+v may take. The parse of a
// puzzle input is milliseconds; a program that has not finished it in this
// long is doing its real work above the cursor, and waiting longer would only
// hold the front end's lock against the editor's own analysis.
var devVowTimeout = 5 * time.Second

// tidyBuffer applies the rewrites to the buffer, reporting what they were.
// It leaves the buffer alone — and says nothing — when there is nothing to do,
// when the program was already declined, or when a run holds the front end.
func (m devModel) tidyBuffer(format bool) (devModel, tidy.Result, bool) {
	src := m.buf.text()
	if m.running {
		// A run holds the front-end lock until it ends. Tidying is not worth
		// freezing the editor for; the next save will do it.
		return m, tidy.Result{}, false
	}
	if src == m.tidyDeclined {
		return m, tidy.Result{}, false
	}
	frontEndMu.Lock()
	res := tidy.Apply(m.path, src, tidy.Options{Format: format})
	frontEndMu.Unlock()
	if !res.Changed() || res.Src == src {
		return m, res, false
	}

	row, col := m.buf.row, m.buf.col
	m.undo.record(m.buf, false, m.now())
	m.buf.lines = strings.Split(res.Src, "\n")
	m.buf.row, m.buf.col = min(row, len(m.buf.lines)-1), col
	// The same byte column on a different line may be the middle of a
	// character; clampCol is the buffer's own rule for where a cursor can be.
	m.buf.clampCol()
	m.buf.goalCol = m.buf.col
	m.buf.anchor = nil
	// What the program was before, so that undoing this and saving again is
	// read as the answer it is.
	m.tidyDeclined = src
	return m, res, true
}

// tidyNow is alt+o: the save's rewrites without the save, for looking at what
// they would do.
func (m devModel) tidyNow() (tea.Model, tea.Cmd) {
	if m.running {
		m.status = "a run is in progress — try again when it ends"
		return m, nil
	}
	// Asking by name overrides an earlier refusal.
	m.tidyDeclined = ""
	m, res, changed := m.tidyBuffer(false)
	if !changed {
		m.status = "nothing to tidy"
		if len(res.Notes) > 0 {
			m.status = "nothing to tidy automatically — " + res.Notes[0]
		}
		return m, nil
	}
	m.dirty = true
	m.status = res.Summary() + " (ctrl+z takes it back)"
	m.scrollToCursor()
	return m, m.touched()
}

// tidyStatus is the save's report: what was rewritten, or failing that the
// first thing that could not be.
func tidyStatus(saved string, res tidy.Result, changed bool) string {
	switch {
	case changed:
		return saved + " — " + res.Summary() + " (ctrl+z takes it back)"
	case len(res.Notes) > 0:
		return saved + " — " + res.Notes[0]
	}
	return saved
}

// ---------------------------------------------------------------------------
// vows
// ---------------------------------------------------------------------------

// devVows is the overlay listing vows that hold of the parse.
type devVows struct {
	offer  tidy.VowOffer
	cursor int
	text   string // the program the offer was computed for
}

type devVowsMsg struct {
	gen   int
	text  string
	offer tidy.VowOffer
	err   error
}

// suggestVows starts the run behind alt+v.
func (m devModel) suggestVows() (tea.Model, tea.Cmd) {
	if m.running {
		m.status = "a run is in progress — try again when it ends"
		return m, nil
	}
	m.vowsBusy = true
	m.status = "running the parse to see what holds of it…"
	gen, path, text, line, dir := m.gen, m.path, m.buf.text(), m.buf.row+1, m.baseDir()
	// After a run that died, the question changes from "what is true of the
	// parse" to "what was wrong with the value that broke it".
	failed, afterFailure := m.failedLine()
	if afterFailure {
		m.status = fmt.Sprintf("looking at the value that entered line %d…", failed)
	}
	other := m.otherInput()
	return m, func() tea.Msg {
		frontEndMu.Lock()
		defer frontEndMu.Unlock()
		var timer *time.Timer
		deadline := func(abort func()) { timer = time.AfterFunc(devVowTimeout, abort) }
		var offer tidy.VowOffer
		var err error
		if afterFailure {
			offer, err = tidy.SuggestVowsBefore(path, text, failed, dir, deadline)
		} else {
			offer, err = tidy.SuggestVowsFor(path, text, line, dir, other, deadline)
		}
		if timer != nil {
			timer.Stop()
		}
		return devVowsMsg{gen: gen, text: text, offer: offer, err: err}
	}
}

func (m devModel) finishVows(msg devVowsMsg) (tea.Model, tea.Cmd) {
	m.vowsBusy = false
	if msg.gen != m.gen || msg.text != m.buf.text() {
		m.status = "the program changed while it was running — alt+v again"
		return m, nil
	}
	if msg.err != nil {
		m.status = "no vows: " + msg.err.Error()
		return m, nil
	}
	m.status = ""
	m.vows = &devVows{offer: msg.offer, text: msg.text}
	return m, nil
}

// vowsKey handles one keystroke on the overlay.
func (m devModel) vowsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.vows
	n := len(v.offer.Vows)
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		m.vows = nil
		return m, nil
	case "down", "ctrl+n", "j":
		v.cursor = (v.cursor + 1) % n
		return m, nil
	case "up", "ctrl+p", "k":
		v.cursor = (v.cursor + n - 1) % n
		return m, nil
	case "enter":
		m.vows = nil
		m = m.insertVows(v, []tidy.Vow{v.offer.Vows[v.cursor]})
	case "a":
		// "All" means all that are worth having: once the vows have been tried
		// on a second input, the ones that failed there are known to be wrong
		// about the puzzle and are not swept in with the rest.
		all := v.offer.Vows
		if v.offer.Other != "" {
			all = nil
			for _, c := range v.offer.Vows {
				if c.Portable {
					all = append(all, c)
				}
			}
			if len(all) == 0 {
				m.status = "none of these hold on " + v.offer.Other + " — enter inserts the highlighted one anyway"
				return m, nil
			}
		}
		m.vows = nil
		m = m.insertVows(v, all)
	default:
		return m, nil
	}
	m.scrollToCursor()
	return m, m.touched()
}

// insertVows writes the chosen vows after the statement they describe.
func (m devModel) insertVows(v *devVows, chosen []tidy.Vow) devModel {
	at := min(v.offer.AfterLine, len(m.buf.lines))
	var lines []string
	for _, c := range chosen {
		lines = append(lines, c.Lines...)
	}
	m.undo.record(m.buf, false, m.now())
	rest := append([]string{}, m.buf.lines[at:]...)
	m.buf.lines = append(append(m.buf.lines[:at:at], lines...), rest...)
	m.buf.row, m.buf.col, m.buf.goalCol = at, 0, 0
	m.buf.anchor = nil
	m.dirty = true
	m.status = fmt.Sprintf("vowed: %s", strings.TrimPrefix(chosen[0].Lines[0], "Binding Vow: "))
	if len(chosen) > 1 {
		m.status = fmt.Sprintf("inserted %d vows — `--release` sheds them", len(chosen))
	}
	return m
}

// vowsView draws the overlay: each vow, with the evidence for it.
func (m devModel) vowsView() string {
	v := m.vows
	var b strings.Builder
	b.WriteString(styTitle.Render("vows after "+v.offer.Stage) + "\n")
	switch {
	case v.offer.Broken:
		b.WriteString(styDim.Render("what was wrong with the value the failed stage was given — each of these is false on this run, and would have stopped it here") + "\n\n")
	case v.offer.Other != "":
		b.WriteString(styDim.Render("tried on this input and on "+v.offer.Other+" — the ones that hold on both are facts about the puzzle") + "\n\n")
	default:
		b.WriteString(styDim.Render("what held on this run — keep the ones that are true of the puzzle, not just of this input") + "\n\n")
	}

	for i, c := range v.offer.Vows {
		marker := "  "
		head := c.Lines[0]
		if i == v.cursor {
			marker = styCursor.Render("› ")
			head = styKeyword.Render(head)
		}
		b.WriteString(truncateVis(marker+head, m.width) + "\n")
		for _, l := range c.Lines[1:] {
			b.WriteString(truncateVis("  "+styFrame.Render(l), m.width) + "\n")
		}
		why := c.Why
		if c.Elsewhere != "" {
			why += " · " + c.Elsewhere
		}
		b.WriteString(truncateVis("    "+styDim.Render(why), m.width) + "\n\n")
	}
	b.WriteString(styDim.Render("↑/↓ choose · enter insert · a insert all · esc leave the program alone"))
	return b.String()
}
