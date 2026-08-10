// Applying a diagnostic's own fix.
//
// The diagnostics engine already computes the repair: a `Fix` carries the byte
// range to replace and what to put there, and `Confident` says whether it is
// sure enough to apply without being asked. `domain expansion: fix` uses
// exactly that from the command line. Until now the editor showed you the
// message — "did you mean \"Cursed Technique\"?" — and then made you type the
// answer it already had.
//
// Only confident fixes are offered. An ambiguous one is a suggestion, and a
// key that sometimes guesses is worse than a key that sometimes declines: the
// message is still on screen, and typing it is still available.
//
// Fix offsets are into the program text, and the buffer is lines, so the one
// piece of real work here is converting between them. It goes through the same
// text the analysis ran on rather than the buffer's current state, and refuses
// when those have diverged — applying a fix computed against text that has
// since been edited would corrupt a line rather than repair it.
package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"domain/diag"
)

// fixableAt returns the confident fix for a 1-based line, if there is one.
func (m devModel) fixableAt(line int) (diag.Diagnostic, bool) {
	for _, d := range m.intel.diags[line] {
		if d.HasConfidentFix() {
			return d, true
		}
	}
	return diag.Diagnostic{}, false
}

// applyFix repairs the cursor's line from the diagnostic on it.
func (m devModel) applyFix() (tea.Model, tea.Cmd) {
	// Staleness is checked before anything is read from the analysis. A fix is
	// a byte range, and the *line* it was reported on is just as stale as the
	// range itself — so consulting the diagnostics first would answer "nothing
	// to fix here" about a line that has since moved.
	if m.intel.text != m.buf.text() {
		m.status = "the program changed since it was checked — pause a moment and try again"
		return m, nil
	}

	line := m.buf.row + 1
	d, ok := m.fixableAt(line)
	if !ok {
		if _, any := m.worstOn(line); any {
			m.status = "no automatic fix for this one"
		} else {
			m.status = "nothing to fix on this line"
		}
		return m, nil
	}

	src := m.buf.text()
	if d.Fix.Start < 0 || d.Fix.End > len(src) || d.Fix.Start > d.Fix.End {
		m.status = "that fix does not fit this program"
		return m, nil
	}

	m.undo.record(m.buf, false, m.now())
	fixed := src[:d.Fix.Start] + d.Fix.Replacement + src[d.Fix.End:]
	m.buf.lines = strings.Split(fixed, "\n")

	// The cursor lands where the repair did, which is where you were looking.
	m.buf.row, m.buf.col = offsetToPos(fixed, d.Fix.Start+len(d.Fix.Replacement))
	m.buf.goalCol = m.buf.col
	m.buf.anchor = nil
	m.dirty = true
	m.status = "fixed: " + firstLineOf(d.Msg)
	m.scrollToCursor()
	return m, m.touched()
}

// fixAllConfident applies every confident fix in the program at once, which is
// what `domain expansion: fix` does to a file.
//
// The engine has already done the work: Analyze repairs as it goes so it can
// see past each error to the next one, and hands back the repaired text and a
// count. Reimplementing that loop here would be a second implementation of the
// same fix-and-re-analyze discipline, free to disagree with the first.
func (m devModel) fixAllConfident() (tea.Model, tea.Cmd) {
	src := m.buf.text()
	frontEndMu.Lock()
	rep := diag.Analyze(m.path, src)
	frontEndMu.Unlock()
	if rep.Applied == 0 || rep.FixedSrc == src {
		m.status = "nothing to fix automatically"
		return m, nil
	}

	row := m.buf.row
	m.undo.record(m.buf, false, m.now())
	m.buf.lines = strings.Split(strings.TrimSuffix(rep.FixedSrc, "\n"), "\n")
	m.buf.row = min(row, len(m.buf.lines)-1)
	m.buf.col, m.buf.goalCol = 0, 0
	m.buf.anchor = nil
	m.dirty = true
	m.status = fmt.Sprintf("applied %d fix(es)", rep.Applied)
	m.scrollToCursor()
	return m, m.touched()
}

// offsetToPos converts a byte offset in the program to a buffer position.
func offsetToPos(text string, offset int) (row, col int) {
	offset = max(0, min(offset, len(text)))
	row = strings.Count(text[:offset], "\n")
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	return row, offset - start
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
