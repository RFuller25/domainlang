// The Bubble Tea host for the editor: keystrokes in, painted program out.
//
// The shape follows repl_tty.go, because the two answer the same questions the
// same way. An open overlay owns the keyboard — it is a mode, not a decoration
// — so overlays are checked before the editor's own keys. The unsaved-work
// guard is a message filter rather than a check in Update, because a quit can
// arrive from several places and the guard should hold for all of them.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"

	"domain/ir"
)

// devModel is the editor's state.
type devModel struct {
	buf   *devBuffer
	path  string // the file being edited; empty until it is named
	input string // the program's input file, for running

	dirty  bool
	status string

	width, height int
	// top is the first buffer line on screen. Painting costs far more than
	// lexing, and it is paid per visible line, so what is off screen is never
	// painted at all.
	top int
	// leftCol is the first *display column* of the program shown, for lines
	// wider than the window. There is no soft wrap here by design, so without
	// this a cursor past the right edge would simply be invisible — the line is
	// clipped and the cursor goes with it.
	leftCol int

	undo devUndoStack
	// now is the clock the undo coalescing measures pauses against. A field so
	// tests can drive a run of typing without sleeping through it.
	now func() time.Time

	// gen counts buffer changes, so an analysis that arrives after another edit
	// can be dropped rather than shown against text it was not computed from.
	gen   int
	intel devIntel

	// running is true between starting a run and its result arriving. The run
	// is on a command rather than in Update precisely so that this can be true
	// while the editor keeps painting and listening for the interrupt.
	running   bool
	interrupt *ir.Interrupter
	spin      spinner.Model
	output    *devOutput
	stepper   *visualModel
	// trace is the last run's recording. It is what the value bar, the timing
	// gutter and the stage walk read, and what the stepper opens over.
	trace *traceView
	// stages is what each line produced on that run, derived from the trace
	// once rather than walked on every frame.
	stages map[int]devStage

	// blocks is every foldable region, from the parse rather than from the
	// indentation; folded is which of them are closed, keyed by header line.
	blocks map[int]devBlock
	folded map[int]bool

	// origin is the file a cross-file definition jump came from, so it can be
	// undone. Nil when this buffer was opened directly.
	origin *devOrigin

	picker   *picker
	search   *devSearch
	complete *devComplete
	inspect  *devInspect
	browser  *docBrowser
	suggest  *devSuggest
	// gotoLine is the line-number prompt, nil when it is closed.
	gotoLine *string
	// showHelp puts the key list on the whole screen, the way the stepper
	// does: a reference is easier to read at full width than beside the thing
	// it describes.
	showHelp bool
	helpTop  int

	// pickingInput distinguishes the browser opened to choose the program's
	// input from the ones that load and save the program itself.
	pickingInput bool

	confirmingQuit bool
	keys           devKeyMap
}

func newDevModel(text string) devModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return devModel{
		buf:    newDevBuffer(text),
		width:  80,
		height: 24,
		keys:   defaultDevKeys(),
		now:    time.Now,
		spin:   sp,
	}
}

// devQuitMsg is the editor asking to leave; the filter below may turn it into
// a confirmation instead.
type devConfirmQuitMsg struct{}

// guardUnsavedDevQuit turns the quit of an editor with unsaved work into a
// confirmation. Like the REPL's guard it is a message filter, so it catches
// every route out rather than the one that happened to be remembered.
func guardUnsavedDevQuit(model tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.QuitMsg); !ok {
		return msg
	}
	m, ok := model.(devModel)
	if !ok || m.confirmingQuit || !m.dirty {
		return msg
	}
	return devConfirmQuitMsg{}
}

func (m devModel) Init() tea.Cmd {
	// Ask the terminal what it is painted on. Nothing waits for the answer:
	// the dark palette stands until (and unless) a report says otherwise.
	// The program is analyzed straight away so an opened file shows its types
	// before it is touched, rather than only after the first keystroke.
	return tea.Batch(tea.RequestBackgroundColor, analyzeCmd(m.gen, m.path, m.buf.text()))
}

func (m devModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The stepper is a whole model rather than a widget, so it sees every
	// message — including the window size and background reports it uses.
	if m.stepper != nil {
		next, cmd := m.stepper.Update(msg)
		m.stepper, _ = next.(*visualModel)
		if quit, pass := stepperQuit(m.stepper, cmd); quit {
			m.stepper = nil
			return m, nil
		} else {
			return m, pass
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
		return m, nil

	case tea.BackgroundColorMsg:
		useTheme(isLightColor(msg.Color))
		return m, nil

	case devConfirmQuitMsg:
		m.confirmingQuit = true
		m.status = "unsaved changes — ctrl+s to save, ctrl+q again to discard"
		return m, nil

	case devRunDoneMsg:
		return m.finishRun(msg.result)

	case spinner.TickMsg:
		if !m.running {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case devIdleMsg:
		if msg.gen != m.gen {
			return m, nil // superseded by a later edit
		}
		return m, analyzeCmd(m.gen, m.path, m.buf.text())

	case devIntelMsg:
		if msg.intel.gen != m.gen {
			return m, nil // computed from text that has since changed
		}
		m.intel = msg.intel
		// The parse the analysis produced is also what knows where the blocks
		// are, so folding follows the program without a second parse.
		if msg.intel.analysis != nil {
			m.blocks = devBlocks(msg.intel.analysis.Prog)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// key routes one keystroke: overlays first, then the editor's own bindings,
// then text.
func (m devModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// A run in progress owns Ctrl+C: that is the whole reason the run is on a
	// command rather than inside Update, and it has to be checked before the
	// quit binding that shares the key.
	if m.running {
		// ctrl+c interrupts rather than copying or leaving. This is checked
		// before every other binding, which is the whole reason the run is on a
		// command and not inside Update.
		if msg.String() == "ctrl+c" {
			m.interrupt.Stop()
			m.status = "stopping…"
		}
		return m, nil
	}
	if m.output != nil {
		return m.outputKey(msg)
	}
	if m.picker != nil {
		return m.pickerKey(msg)
	}
	if m.showHelp {
		return m.helpKey(msg)
	}
	if m.complete != nil {
		open, accepted := m.completeKey(msg)
		switch {
		case accepted:
			m = m.acceptComplete()
			return m, m.touched()
		case !open:
			m.complete = nil
			// The keystroke that closed the popup is still a keystroke: it goes
			// on to do whatever it would have done, so dismissing by typing does
			// not swallow the character.
			if msg.String() == "esc" || msg.String() == "ctrl+c" {
				return m, nil
			}
		default:
			return m, nil
		}
	}
	if m.suggest != nil {
		open, accepted := m.suggest.key(msg)
		if open {
			return m, nil
		}
		pick := m.suggest.candidates[m.suggest.cursor]
		m.suggest = nil
		if !accepted {
			return m, nil
		}
		m = m.insertSuggestion(pick)
		m.scrollToCursor()
		return m, m.touched()
	}
	if m.browser != nil {
		open, pick := m.browser.update(msg)
		if open {
			return m, nil
		}
		m.browser = nil
		if pick == "" {
			m.status = "(cancelled)"
			return m, nil
		}
		// The catalog hands back a statement ready to be finished, which goes
		// in on a line of its own rather than into the middle of one.
		m.undo.record(m.buf, false, m.now())
		if m.buf.line() != "" {
			m.buf.end()
			m.buf.newline()
		}
		m.buf.insert(pick)
		m.dirty = true
		m.scrollToCursor()
		return m, m.touched()
	}
	if m.inspect != nil {
		// The panel is a reference, not a mode: any key closes it.
		m.inspect = nil
		return m, nil
	}
	if m.search != nil {
		if !m.devSearchKey(msg) {
			m.search = nil
		}
		m.scrollToCursor()
		return m, nil
	}
	if m.gotoLine != nil {
		return m.gotoLineKey(msg)
	}

	// Any key that is not the quit itself withdraws the confirmation: the
	// question was "leave without saving", and carrying on editing answers it.
	if m.confirmingQuit && !key.Matches(msg, m.keys.Quit) {
		m.confirmingQuit = false
		m.status = ""
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Save):
		return m.save(m.path)
	case key.Matches(msg, m.keys.SaveAs):
		m.picker = newPicker(":save", m.dir())
		return m, nil
	case key.Matches(msg, m.keys.Open):
		m.picker = newPicker(":load", m.dir())
		return m, nil
	case key.Matches(msg, m.keys.Help):
		m.showHelp, m.helpTop = true, 0
		return m, nil

	case key.Matches(msg, m.keys.Undo):
		if !m.undo.undo(m.buf) {
			m.status = "nothing to undo"
			return m, nil
		}
		m.dirty, m.status = true, ""
		m.scrollToCursor()
		return m, m.touched()

	case key.Matches(msg, m.keys.Redo):
		if !m.undo.redo(m.buf) {
			m.status = "nothing to redo"
			return m, nil
		}
		m.dirty, m.status = true, ""
		m.scrollToCursor()
		return m, m.touched()

	case key.Matches(msg, m.keys.Find):
		s := &devSearch{origin: m.buf.cursor()}
		m.search = s
		return m, nil

	case key.Matches(msg, m.keys.Goto):
		empty := ""
		m.gotoLine = &empty
		return m, nil

	case key.Matches(msg, m.keys.SelectAll):
		m.buf.selectAll()
		m.scrollToCursor()
		return m, nil

	case key.Matches(msg, m.keys.Copy):
		return m.copy(false)

	case key.Matches(msg, m.keys.Cut):
		return m.copy(true)

	case key.Matches(msg, m.keys.Paste):
		return m.paste()

	case key.Matches(msg, m.keys.Complete):
		if c, ok := m.openComplete(); ok {
			m.complete = c
			return m, nil
		}
		// Nothing to offer, so the key falls through to indenting — a
		// completion that silently does nothing is worse than none.

	case key.Matches(msg, m.keys.Inspect):
		if ins, ok := m.inspectAtCursor(); ok {
			m.inspect = &ins
			return m, nil
		}
		m.status = "nothing to inspect on this line"
		return m, nil

	case key.Matches(msg, m.keys.Definition):
		m, _ = m.jumpToDefinition()
		m.scrollToCursor()
		return m, nil

	case key.Matches(msg, m.keys.Run):
		return m.runProgram()

	case key.Matches(msg, m.keys.Visualize):
		return m.openStepper()

	case key.Matches(msg, m.keys.StageNext), key.Matches(msg, m.keys.StagePrev):
		delta := 1
		if key.Matches(msg, m.keys.StagePrev) {
			delta = -1
		}
		if !m.stageStep(delta) {
			m.status = "nothing recorded yet — ctrl+r runs and records"
			return m, nil
		}
		m.scrollToCursor()
		return m, nil

	case key.Matches(msg, m.keys.Input):
		p := newPicker(":load", m.inputPickerDir())
		p.anyFile = true
		m.picker = p
		m.pickingInput = true
		return m, nil

	case key.Matches(msg, m.keys.Suggest):
		if m.input == "" {
			m.status = "choose an input first (ctrl+e)"
			return m, nil
		}
		b, err := os.ReadFile(filepath.Join(m.baseDir(), m.input))
		if err != nil {
			m.status = "cannot read " + m.input
			return m, nil
		}
		sg, ok := suggestFor(m.input, string(b))
		if !ok {
			m.status = "nothing to suggest for " + m.input
			return m, nil
		}
		m.suggest = sg
		return m, nil

	case key.Matches(msg, m.keys.Docs):
		m.browser = newDocBrowser()
		return m, nil

	case key.Matches(msg, m.keys.JumpBack):
		m, _ = m.jumpBack()
		m.scrollToCursor()
		return m, nil

	case key.Matches(msg, m.keys.Fold):
		var ok bool
		if m, ok = m.toggleFold(); ok {
			m.scrollToCursor()
		}
		return m, nil

	case key.Matches(msg, m.keys.UnfoldAll):
		m = m.unfoldAll()
		return m, nil

	case key.Matches(msg, m.keys.Explain):
		return m.explainPane()

	case key.Matches(msg, m.keys.Fix):
		return m.applyFix()

	case key.Matches(msg, m.keys.FixAll):
		return m.fixAllConfident()

	case key.Matches(msg, m.keys.Format):
		m, changed := m.formatBuffer(m.buf.text())
		if !changed {
			return m, nil
		}
		m.scrollToCursor()
		return m, m.touched()
	}

	return m.editKey(msg)
}

// editKey is everything that moves the cursor or changes the text. It is split
// out because undo has to see the buffer *before* the change, and because
// whether a keystroke extends the selection is a property of the key rather
// than of what it does.
func (m devModel) editKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name := msg.String()

	// A shifted motion extends the selection; an unshifted one drops it. That
	// is the whole rule, and it is decided here rather than inside each motion.
	if shifted, ok := devUnshift(name); ok {
		m.buf.startSelecting()
		name = shifted
	} else if devIsMotion(name) {
		m.buf.clearSelection()
	}

	if devChangesText(name, msg) {
		m.undo.record(m.buf, devIsTyping(name, msg), m.now())
	}

	before := m.buf.text()
	m.apply(name, msg)
	if m.buf.text() != before {
		m.dirty = true
		m.status = ""
		m.scrollToCursor()
		return m, m.touched()
	}
	m.scrollToCursor()
	return m, nil
}

// skipFolded moves the cursor out of a folded block in the direction it was
// travelling, so vertical motion steps over a fold rather than into it — a
// cursor on a line that is not drawn is a cursor nobody can see.
func (m *devModel) skipFolded(dir int) {
	for m.hidden(m.buf.row+1) && m.buf.row > 0 && m.buf.row < len(m.buf.lines)-1 {
		if dir > 0 {
			m.buf.down()
		} else {
			m.buf.up()
		}
	}
	// A fold at the very end of the buffer has nowhere further to go; come back
	// to its header rather than sitting inside it.
	for m.hidden(m.buf.row+1) && m.buf.row > 0 {
		m.buf.up()
	}
}

// touched records that the buffer changed and restarts the idle timer. The
// generation it bumps is what lets a stale analysis be recognized and dropped.
func (m *devModel) touched() tea.Cmd {
	m.gen++
	return m.scheduleIntel()
}

// apply carries out one motion or edit. The dirty flag above is decided by
// whether the text changed rather than by which key was pressed, so moving the
// cursor through a file can never mark it unsaved.
func (m devModel) apply(name string, msg tea.KeyPressMsg) {
	switch name {
	case "left":
		m.buf.left()
	case "right":
		m.buf.right()
	case "up":
		m.buf.up()
		m.skipFolded(-1)
	case "down":
		m.buf.down()
		m.skipFolded(1)
	case "home":
		m.buf.home()
	case "end":
		m.buf.end()
	case "pgup":
		for range m.page() {
			m.buf.up()
		}
	case "pgdown":
		for range m.page() {
			m.buf.down()
		}
	case "ctrl+left", "alt+left":
		m.buf.wordLeft()
	case "ctrl+right", "alt+right":
		m.buf.wordRight()
	case "ctrl+home":
		m.buf.gotoLine(1)
	case "ctrl+end":
		m.buf.row = len(m.buf.lines) - 1
		m.buf.end()
	case "enter":
		m.buf.deleteSelection()
		m.buf.newline()
	case "backspace":
		if !m.buf.deleteSelection() {
			m.buf.backspace()
		}
	case "delete":
		m.buf.deleteForward()
	case "tab":
		// Tab indents a selection and inserts inside a line, which is what makes
		// it usable for both jobs without a second key.
		if _, _, ok := m.buf.selection(); ok {
			first, last := m.buf.indentTarget()
			m.buf.indentRows(first, last, false)
		} else {
			m.buf.insert(devIndent)
		}
	case "shift+tab":
		first, last := m.buf.indentTarget()
		m.buf.indentRows(first, last, true)
	default:
		// Key.Text is non-empty exactly when the key produced printable
		// characters, which keeps every function and modifier key from
		// inserting its own name into the program.
		if msg.Text != "" {
			m.buf.deleteSelection()
			m.buf.insert(msg.Text)
		}
	}
}

// ---------------------------------------------------------------------------
// overlays
// ---------------------------------------------------------------------------

func (m devModel) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	open, path := m.picker.update(msg)
	if open {
		return m, nil
	}
	saving, forInput := m.picker.saving(), m.pickingInput
	m.picker, m.pickingInput = nil, false
	if path == "" {
		// Cancelling the picker that opened the editor leaves nothing to edit.
		if m.path == "" && !m.dirty && len(m.buf.lines) == 1 && m.buf.lines[0] == "" {
			return m, tea.Quit
		}
		m.status = "(cancelled)"
		return m, nil
	}
	switch {
	case forInput:
		m, _ = m.bindInput(path)
		m.scrollToCursor()
		// Choosing an input is when its shape is worth reading, so the offer
		// comes now rather than waiting to be asked. Esc declines it.
		if b, err := os.ReadFile(path); err == nil {
			if sg, ok := suggestFor(filepath.Base(path), string(b)); ok {
				m.suggest = sg
			}
		}
		return m, m.touched()
	case saving:
		return m.save(path)
	}
	return m.open(path)
}

func (m devModel) helpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	last := max(0, len(devHelpBody())-max(1, m.height-2))
	switch msg.String() {
	case "down", "j":
		m.helpTop = min(m.helpTop+1, last)
	case "up", "k":
		m.helpTop = max(m.helpTop-1, 0)
	case "pgdown", " ":
		m.helpTop = min(m.helpTop+m.page(), last)
	case "pgup":
		m.helpTop = max(m.helpTop-m.page(), 0)
	default:
		// The key list is a reference, not a mode: anything that is not a
		// scroll closes it.
		m.showHelp = false
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// files
// ---------------------------------------------------------------------------

// dir is where a file browser should open: beside the file being edited, or
// the working directory when there is not one yet.
func (m devModel) dir() string {
	if m.path == "" {
		return "."
	}
	return filepath.Dir(m.path)
}

func (m devModel) open(path string) (tea.Model, tea.Cmd) {
	b, err := os.ReadFile(path)
	if err != nil {
		m.status = fmt.Sprintf("could not open %s: %v", filepath.Base(path), err)
		return m, nil
	}
	m.buf = newDevBuffer(string(b))
	m.path, m.dirty, m.top, m.leftCol = path, false, 0, 0
	m.status = ""
	// The analysis, the folds and the recording all describe the program that
	// was here a moment ago. Carrying any of them into a different file would
	// show one program's answers against another's text.
	m.intel = devIntel{}
	m.blocks, m.folded = nil, nil
	m.trace, m.stages = nil, nil
	m.gen++
	return m, analyzeCmd(m.gen, m.path, m.buf.text())
}

func (m devModel) save(path string) (tea.Model, tea.Cmd) {
	if path == "" {
		m.picker = newPicker(":save", m.dir())
		return m, nil
	}
	// A trailing newline, because a Domain program is a text file and every
	// tool that reads one — including this editor, which trims it back off —
	// expects the last line to be terminated.
	if err := os.WriteFile(path, []byte(m.buf.text()+"\n"), 0o644); err != nil {
		m.status = fmt.Sprintf("could not save %s: %v", filepath.Base(path), err)
		return m, nil
	}
	m.path, m.dirty, m.confirmingQuit = path, false, false
	m.status = "saved " + filepath.Base(path)
	return m, nil
}

// ---------------------------------------------------------------------------
// scrolling
// ---------------------------------------------------------------------------

// textHeight is how many buffer lines fit, once the status line has its own.
func (m devModel) textHeight() int { return max(1, m.height-1) }

func (m devModel) page() int { return max(1, m.textHeight()-1) }

// scrollToCursor keeps the cursor line on screen, moving the window by the
// least that achieves it so that reading does not jump.
func (m *devModel) scrollToCursor() {
	h := m.textHeight()
	m.top = min(m.top, m.buf.row)
	m.top = max(m.top, m.buf.row-h+1)
	m.top = max(0, min(m.top, max(0, len(m.buf.lines)-h)))
	// A buffer shorter than the window always starts at the top; without this
	// a deletion could leave the view scrolled past the end.
	if len(m.buf.lines) <= h {
		m.top = 0
	}

	// And the same horizontally, in display columns rather than bytes — a line
	// of accented characters is narrower on screen than it is in memory, and
	// the window is measured in cells.
	w := m.textWidth()
	cur := ansi.StringWidth(m.buf.line()[:m.buf.col])
	m.leftCol = min(m.leftCol, cur)
	m.leftCol = max(m.leftCol, cur-w+1)
	m.leftCol = max(0, m.leftCol)
}

// textWidth is how many columns the program itself gets, once the gutter has
// taken its own.
func (m devModel) textWidth() int {
	return max(1, m.width-m.gutterWidth())
}

// gutterWidth is the width of the line-number column and its separator.
func (m devModel) gutterWidth() int {
	return len(strconv.Itoa(max(len(m.buf.lines), 1))) + 3
}

// View declares the alternate screen: an editor takes the terminal for as long
// as it runs and gives the scrollback back untouched when it leaves, which is
// the one thing that distinguishes it from the REPL's inline transcript.
func (m devModel) View() tea.View {
	v := tea.NewView(m.view())
	v.AltScreen = true
	return v
}

// ---------------------------------------------------------------------------
// key classification
// ---------------------------------------------------------------------------

// devUnshift maps a shifted motion to the motion it extends the selection
// with. Shift is what turns moving into selecting, and keeping the mapping in
// one table means a motion added later cannot silently lose that.
func devUnshift(name string) (string, bool) {
	shifted := map[string]string{
		"shift+left": "left", "shift+right": "right",
		"shift+up": "up", "shift+down": "down",
		"shift+home": "home", "shift+end": "end",
		"shift+pgup": "pgup", "shift+pgdown": "pgdown",
		"ctrl+shift+left": "ctrl+left", "ctrl+shift+right": "ctrl+right",
	}
	s, ok := shifted[name]
	return s, ok
}

// devIsMotion reports whether a key only moves the cursor. An unshifted motion
// drops the selection, which is the other half of the shift rule.
func devIsMotion(name string) bool {
	switch name {
	case "left", "right", "up", "down", "home", "end", "pgup", "pgdown",
		"ctrl+left", "ctrl+right", "alt+left", "alt+right", "ctrl+home", "ctrl+end":
		return true
	}
	return false
}

// devChangesText reports whether a key can alter the buffer, and so needs an
// undo step recorded before it runs.
func devChangesText(name string, msg tea.KeyPressMsg) bool {
	switch name {
	case "enter", "backspace", "delete", "tab", "shift+tab":
		return true
	}
	return msg.Text != ""
}

// devIsTyping reports whether a change may join the run of typing in progress.
// Enter and the indent keys are decisions rather than characters, so each is
// its own undo step however fast it follows the last one.
func devIsTyping(name string, msg tea.KeyPressMsg) bool {
	switch name {
	case "enter", "tab", "shift+tab":
		return false
	case "backspace", "delete":
		return true // deleting back through a word is one thought, like typing it
	}
	return msg.Text != ""
}

// ---------------------------------------------------------------------------
// the clipboard
// ---------------------------------------------------------------------------

// copy puts the selection on the system clipboard, cutting it when asked. With
// nothing selected it takes the current line, which is what every editor does
// and what makes the key useful without a selection gesture first.
func (m devModel) copy(cut bool) (tea.Model, tea.Cmd) {
	text, whole := m.buf.selectedText(), false
	if text == "" {
		text, whole = m.buf.line(), true
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.status = "no system clipboard here"
		return m, nil
	}
	if !cut {
		m.status = fmt.Sprintf("copied %d line(s)", strings.Count(text, "\n")+1)
		return m, nil
	}

	m.undo.record(m.buf, false, m.now())
	if whole {
		// Cutting a whole line takes the line, not its contents: leaving an
		// empty line behind is the one thing nobody means by "cut this line".
		if len(m.buf.lines) == 1 {
			m.buf.lines[0] = ""
		} else {
			m.buf.lines = append(m.buf.lines[:m.buf.row], m.buf.lines[m.buf.row+1:]...)
			m.buf.row = min(m.buf.row, len(m.buf.lines)-1)
		}
		m.buf.col, m.buf.goalCol = 0, 0
	} else {
		m.buf.deleteSelection()
	}
	m.dirty = true
	m.status = fmt.Sprintf("cut %d line(s)", strings.Count(text, "\n")+1)
	m.scrollToCursor()
	return m, m.touched()
}

func (m devModel) paste() (tea.Model, tea.Cmd) {
	text, err := clipboard.ReadAll()
	if err != nil {
		m.status = "no system clipboard here"
		return m, nil
	}
	if text == "" {
		m.status = "(the clipboard is empty)"
		return m, nil
	}
	m.undo.record(m.buf, false, m.now())
	m.buf.insertText(text)
	m.dirty, m.status = true, ""
	m.scrollToCursor()
	return m, m.touched()
}

// ---------------------------------------------------------------------------
// go to line
// ---------------------------------------------------------------------------

func (m devModel) gotoLineKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.gotoLine = nil
	case "enter":
		if n, err := strconv.Atoi(*m.gotoLine); err == nil {
			m.buf.gotoLine(n)
			m.scrollToCursor()
		}
		m.gotoLine = nil
	case "backspace":
		if s := *m.gotoLine; s != "" {
			trimmed := s[:len(s)-1]
			m.gotoLine = &trimmed
		}
	default:
		// Digits only: a prompt that accepts letters and then refuses them at
		// Enter has wasted the typing.
		if len(msg.Text) == 1 && msg.Text[0] >= '0' && msg.Text[0] <= '9' {
			s := *m.gotoLine + msg.Text
			m.gotoLine = &s
		}
	}
	return m, nil
}
