// What the language knows about the program on screen.
//
// This is the reason the editor exists. Everything here was already in the
// binary — the resolver's per-statement types, the diagnostics engine, the
// primitive catalog, the language server's completion — and was reachable only
// by saving a file and asking another process about it. Here it is a function
// call against the buffer, which means it can be true of text that has not
// been saved and does not yet parse.
//
// The work is debounced rather than done per keystroke. Resolving is
// microseconds on an AoC-scale program, so this is not about cost: it is that
// a type which flickers on every character is noise, and one that appears when
// you pause is an answer. The same reason the REPL delays its preview.
//
// Analysis is failure-tolerant throughout. `Analyze` hands back the nodes it
// managed before giving up, so the prefix that resolved still shows its types
// while the line you are typing does not — which is the state a program spends
// almost all of its life in.
package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"path/filepath"

	"domain/diag"
	"domain/format"
	"domain/lsp"
)

// devIdleDelay is how long editing has to pause before the program is
// re-analyzed. A variable so tests can drive the editor without waiting.
var devIdleDelay = 200 * time.Millisecond

// devIntel is everything known about the current buffer text.
type devIntel struct {
	// gen is the buffer generation this was computed from. A result that
	// arrives after another edit is dropped rather than shown against text it
	// was not computed from.
	gen int
	// text is the program the analysis ran on. A diagnostic's fix is a byte
	// range, and a byte range against different text edits the wrong
	// characters, so the two are kept together.
	text     string
	analysis *lsp.Analysis
	// hints is the type flowing out of each statement, by 1-based line.
	hints map[int]string
	// diags is every finding, by 1-based line, worst first within a line.
	diags map[int][]diag.Diagnostic
	errs  int
	warns int
	// hints_ counts the advisory findings — the linter's territory — which are
	// counted apart from errors so the status line can say which is which.
	hints_ int
}

// devIntelMsg carries a finished analysis back to the model.
type devIntelMsg struct{ intel devIntel }

// devIdleMsg fires when editing has paused long enough to be worth analyzing.
type devIdleMsg struct{ gen int }

// scheduleIntel restarts the idle timer. Every edit calls it, so the analysis
// happens once after a burst of typing rather than once per keystroke.
func (m devModel) scheduleIntel() tea.Cmd {
	gen := m.gen
	return tea.Tick(devIdleDelay, func(time.Time) tea.Msg { return devIdleMsg{gen: gen} })
}

// analyzeCmd runs the front end and the diagnostics engine off the event loop.
// Both are pure functions of the text, so nothing here touches the model.
func analyzeCmd(gen int, path, text string) tea.Cmd {
	return func() tea.Msg {
		// Resolution writes package-level state, and this runs on a command
		// while another may still be running. Without this the two corrupt each
		// other's binding tables, and the damage surfaces as nonsense: a linter
		// reporting that `Combine` ignores its `From:` argument.
		frontEndMu.Lock()
		defer frontEndMu.Unlock()

		intel := devIntel{gen: gen, text: text, hints: map[int]string{}, diags: map[int][]diag.Diagnostic{}}

		intel.analysis = lsp.Analyze(path, text)
		for _, h := range intel.analysis.TypeHints() {
			intel.hints[h.Line] = h.Label
		}

		// The diagnostics engine repairs as it goes so it can see past the
		// first error; only the findings are wanted here, never its FixedSrc —
		// the editor must not rewrite what someone is typing.
		// Analyze is the whole of `domain expansion: lint`: it runs the checker
		// and the linter, in that order, and — importantly — runs the
		// resolve-time half of the linter only when the program actually
		// resolved. A statement the resolver never reached never had the chance
		// to read its arguments and would report every one of them as ignored,
		// which is exactly what a second, unguarded Lint pass here produced.
		rep := diag.Analyze(path, text)
		for _, d := range rep.Diags {
			intel.diags[d.Pos.Line] = append(intel.diags[d.Pos.Line], d)
		}
		intel.errs, intel.warns, intel.hints_ = rep.Counts()
		return devIntelMsg{intel: intel}
	}
}

// hintFor is the type shown at the end of a line, if there is one.
func (m devModel) hintFor(row int) string {
	if m.intel.hints == nil {
		return ""
	}
	return m.intel.hints[row+1]
}

// worstOn returns the most severe diagnostic on a 1-based line.
func (m devModel) worstOn(line int) (diag.Diagnostic, bool) {
	found := false
	var worst diag.Diagnostic
	for _, d := range m.intel.diags[line] {
		if !found || d.Severity < worst.Severity {
			worst, found = d, true
		}
	}
	return worst, found
}

// gutterMark is the character that stands beside a line with something wrong
// with it. One column, because the gutter is beside every line and a wider
// marker would move the program's text for the sake of the rare line that has
// a finding.
func (m devModel) gutterMark(row int) (string, bool) {
	d, ok := m.worstOn(row + 1)
	if !ok {
		return "", false
	}
	switch d.Severity {
	case diag.Error:
		return styErr.Render("✗"), true
	case diag.Warning:
		return styKey.Render("!"), true
	}
	return styDim.Render("·"), true
}

// diagnosticLine is what the status bar says about the cursor's line. The
// message for the line you are on beats a count of the whole file: the count
// is in the status line anyway, and the message is the one that tells you what
// to do next.
func (m devModel) diagnosticLine() string {
	d, ok := m.worstOn(m.buf.row + 1)
	if !ok {
		return ""
	}
	style := styErr
	if d.Severity != diag.Error {
		style = styKey
	}
	msg := style.Render(d.Msg)
	if d.Help != "" {
		msg += styDim.Render("  " + d.Help)
	}
	return msg
}

// ---------------------------------------------------------------------------
// inspect
// ---------------------------------------------------------------------------

// devInspect is the panel describing whatever the cursor is on.
type devInspect struct{ lines []string }

// inspectAtCursor builds the panel, or reports that there is nothing to say.
func (m devModel) inspectAtCursor() (devInspect, bool) {
	if m.intel.analysis == nil {
		return devInspect{}, false
	}
	ins, ok := m.intel.analysis.InspectLine(m.buf.row + 1)
	if !ok {
		return devInspect{}, false
	}

	out := []string{styTitle.Render(ins.Title)}
	if ins.Signature != "" {
		out = append(out, styType.Render(ins.Signature))
	}
	if ins.TypeStep != "" && ins.TypeStep != ins.Signature {
		// The concrete step this call makes, which the declared signature's
		// type variables do not show.
		out = append(out, styDim.Render("here: ")+styType.Render(ins.TypeStep))
	}
	if ins.Summary != "" {
		out = append(out, "", ins.Summary)
	}
	if ins.DocAnchor != "" {
		out = append(out, "", styDim.Render("primitives.md#"+ins.DocAnchor))
	}
	return devInspect{lines: out}, true
}

// ---------------------------------------------------------------------------
// go to definition
// ---------------------------------------------------------------------------

// jumpToDefinition moves the cursor to the Shikigami defined on the cursor's
// line, when it is a call to one defined in this file.
func (m devModel) jumpToDefinition() (devModel, bool) {
	if m.intel.analysis == nil {
		return m, false
	}
	from, fromLine := m.path, m.buf.row+1
	loc, ok := m.intel.analysis.DefinitionAt(m.buf.row + 1)
	if !ok {
		m.status = "no Shikigami call on this line"
		return m, false
	}
	switch {
	case loc.Origin == "prelude":
		// A prelude name is real and has nowhere to jump to. Saying which it is
		// beats saying nothing, which reads as "no such definition".
		m.status = fmt.Sprintf("%s is a prelude Shikigami — see language.md", loc.Name)
		return m, false

	case loc.Path != "" && loc.Path != m.path:
		// A definition in a library file. Following it means leaving this
		// buffer, so unsaved work is asked about rather than discarded — an
		// editor may not lose your program to a navigation key.
		if m.dirty {
			m.status = fmt.Sprintf("%s is in %s — save first (ctrl+s) to follow it", loc.Name, filepath.Base(loc.Path))
			return m, false
		}
		next, _ := m.open(loc.Path)
		m = next.(devModel)
		if m.path != loc.Path {
			return m, false // open() put the reason in the status line
		}
		// Where we came from, so ctrl+[ comes back. One step is enough: this
		// follows imports, and an import chain deep enough to need a stack is
		// not a program anyone is editing by hand.
		m.origin = &devOrigin{path: from, line: fromLine}
		m.buf.gotoLine(loc.Pos.Line)
		m.status = "→ " + loc.Name + " in " + filepath.Base(loc.Path)
		return m, true
	}
	m.buf.gotoLine(loc.Pos.Line)
	m.status = "→ " + loc.Name
	return m, true
}

// devOrigin is where a cross-file jump came from, so it can be undone.
type devOrigin struct {
	path string
	line int
}

// jumpBack returns to the file a definition was followed from.
func (m devModel) jumpBack() (devModel, bool) {
	if m.origin == nil {
		m.status = "nowhere to go back to"
		return m, false
	}
	if m.dirty {
		m.status = "save first (ctrl+s) to go back"
		return m, false
	}
	o := m.origin
	next, _ := m.open(o.path)
	m = next.(devModel)
	if m.path != o.path {
		return m, false
	}
	m.origin = nil
	m.buf.gotoLine(o.line)
	m.status = "← " + filepath.Base(o.path)
	return m, true
}

// ---------------------------------------------------------------------------
// format
// ---------------------------------------------------------------------------

// formatBuffer runs the canonical formatter over the buffer, keeping the
// cursor on the line it was on. The column is not preserved: formatting moves
// text within a line, and a column that no longer means what it did is worse
// than the start of the right line.
func (m devModel) formatBuffer(src string) (devModel, bool) {
	out, err := format.Format(src)
	if err != nil {
		m.status = "cannot format: " + firstLine(err.Error())
		return m, false
	}
	// The formatter terminates its output; the buffer does not hold a trailing
	// newline. Comparing without trimming would report every program as changed.
	out = strings.TrimSuffix(out, "\n")
	if out == src {
		m.status = "already formatted"
		return m, false
	}
	row := m.buf.row
	m.undo.record(m.buf, false, m.now())
	m.buf.lines = strings.Split(out, "\n")
	m.buf.row = min(row, len(m.buf.lines)-1)
	m.buf.col, m.buf.goalCol = 0, 0
	m.buf.anchor = nil
	m.dirty, m.status = true, "formatted"
	return m, true
}
