// The interactive line editor for `domain repl` on a real terminal.
//
// Everything here exists because a terminal can do things a pipe cannot: edit
// a line, complete a token, show the type of a statement before it is
// submitted, draw a spinner while a program runs, and — the reason the
// evaluation moved off this file's Update path — interrupt a run that is
// never going to finish. `While` loops are unbounded by design, and a REPL
// that evaluates inside its own event loop cannot hear Ctrl+C, because raw
// mode has already turned it from a signal into a keystroke nobody is reading.
// So a submitted line is handed to a tea.Cmd, the model keeps painting, and
// Ctrl+C stops the run through an ir.Interrupter.
//
// Piped input and tests use the plain reader in repl.go instead — see Repl.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/ir"
)

// replMu serializes everything that resolves or runs a program. Resolution
// and evaluation both read and write package-level state in prims (the
// ambient-binding stacks a For loop pushes), so the live type preview running
// in the background must never overlap the evaluation of a submitted line.
var replMu sync.Mutex

// previewDelay is how long typing has to pause before the session resolves
// what has been typed so far to show its type. Long enough that a fast typist
// never triggers it mid-word, short enough to feel immediate on a pause. It is
// a variable so tests can drive the editor without waiting for it.
var previewDelay = 150 * time.Millisecond

// replTTY runs the interactive loop over a real terminal.
func replTTY(stdin *os.File, stdout io.Writer) int {
	p := tea.NewProgram(newReplModel(),
		tea.WithInput(stdin), tea.WithOutput(stdout), tea.WithFilter(guardUnsavedQuit))
	final, err := p.Run()
	if err != nil {
		fmt.Fprintf(stdout, "error: %v\n", err)
		return 1
	}
	if m, ok := final.(replModel); ok {
		m.hist.save()
	}
	return 0
}

// guardUnsavedQuit turns the quit of a session with unsaved statements into a
// confirmation instead. It is a message filter rather than a check inside
// Update because a quit can arrive from several places (Ctrl+C, Ctrl+D,
// :quit), and the guard should hold for all of them.
func guardUnsavedQuit(model tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.QuitMsg); !ok {
		return msg
	}
	m, ok := model.(replModel)
	if !ok || m.confirmingQuit || !m.core.dirty || len(m.core.stmts) == 0 {
		return msg
	}
	return confirmQuitMsg{}
}

// The messages the model sends itself.
type (
	// evalDoneMsg reports that the submitted line has been fully handled.
	evalDoneMsg struct{ quit bool }
	// previewTickMsg fires when typing has paused long enough to resolve.
	previewTickMsg struct{ gen int }
	// previewMsg carries the type the typed line would produce.
	previewMsg struct {
		gen  int
		typ  string
		okay bool
	}
	// editDoneMsg reports that $EDITOR exited.
	editDoneMsg struct {
		path string
		err  error
	}
	// confirmQuitMsg is a quit held back because the session is unsaved.
	confirmQuitMsg struct{}
)

// replModel is the bubbletea Model driving one interactive session. It wraps
// the same repl core repl.go's plain reader uses; core.out is a buffer this
// model reads from after every line so the REPL's own writes (results,
// errors, :command output) can be re-emitted through tea.Println instead of
// being written straight to the terminal, which raw mode does not allow.
type replModel struct {
	ti   textinput.Model
	core *repl
	buf  *strings.Builder
	seen int // bytes of buf already echoed via tea.Println
	hist *history
	spin spinner.Model
	help help.Model
	keys replKeyMap

	completing bool
	candidates []string
	candIdx    int
	tokenStart int // byte offset into the input line

	// One evaluation runs at a time; queued lines are the rest of a pasted
	// block waiting their turn.
	evaluating   bool
	interrupt    *ir.Interrupter
	progress     *progressCounter
	queue        []string
	echoPrompt   string // the prompt the line in flight was typed at
	echoLine     string
	interrupting bool
	evalStarted  time.Time

	// A pasted program is one unit of work: its lines are collected while the
	// queue drains and reported once, instead of a screenful of intermediate
	// values. See summarizePaste.
	pasting     bool
	pasteEchoes []string
	pasteOut    []string

	// awaitingClipboard is set between asking the terminal for its clipboard
	// (:paste) and the reply, so an unrelated OSC 52 report is not mistaken
	// for a program.
	awaitingClipboard bool

	preview    string // the type the line being typed would produce
	previewGen int
	focused    bool // the terminal window has focus (previews pause without it)

	// pendingTheme is a palette swap waiting for a running program to finish;
	// see retheme.
	pendingTheme *bool

	// pager, when set, is a full-screen reader over output too tall to print;
	// while it is open it takes every key (repl_pager.go).
	pager *pager
	// search, when set, is the Ctrl+R history search (repl_search.go).
	search *historySearch
	// picker, when set, is the :load/:save file browser (repl_picker.go).
	picker *picker
	// browser, when set, is the :doc primitive catalog (repl_doc.go).
	browser *docBrowser
	// stepper, when set, is the run visualizer over the session's own program
	// (repl_visualize.go).
	stepper *visualModel

	// block, when set, is the whole-body editor for a pending statement
	// (repl_block.go).
	block *blockEditor

	// watch, when set, is a file whose changes replay the program
	// (repl_watch.go).
	watch    *watch
	watchGen int

	width, height  int
	showHelp       bool
	status         string // one-shot line under the prompt; cleared on the next key
	confirmingQuit bool
	// stmtsAtQuitPrompt is how long the program was when the quit guard last
	// asked; growing past it means there is new work to guard again.
	stmtsAtQuitPrompt int
}

func newReplModel() replModel {
	ti := textinput.New()
	ti.Prompt = promptTop
	ti.ShowSuggestions = true
	// A real terminal cursor rather than a drawn one: it blinks the way the
	// user's terminal blinks, and its *shape* is then free to carry state
	// (see cursor).
	ti.SetVirtualCursor(false)
	ti.Focus()
	buf := &strings.Builder{}
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(styFrame))
	h := help.New()
	progress := &progressCounter{}
	return replModel{
		ti:  ti,
		buf: buf,
		core: &repl{out: buf, baseDir: ".", color: true, width: 100,
			progress: progress, interactive: true},
		hist:     newHistory(),
		spin:     sp,
		help:     h,
		keys:     defaultReplKeys(),
		progress: progress,
		width:    100,
		focused:  true,
	}
}

func (m replModel) Init() tea.Cmd {
	// Ask the terminal what it is painted on. Nothing waits for the answer:
	// the dark palette stands until (and unless) a report says otherwise.
	return tea.Batch(tea.Println(replBanner), tea.RequestBackgroundColor)
}

func (m replModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The block editor is a textarea: like the stepper, it needs more than
	// keystrokes (its cursor blinks on commands of its own).
	if m.block != nil {
		open, body, cmd := m.block.update(msg)
		if open {
			return m, cmd
		}
		head := m.block.head
		m.block = nil
		if body == nil {
			m.status = "(block unchanged)"
			return m, nil
		}
		// The head and its body go back as one statement, submitted the way a
		// finished block always is: an empty line closes it.
		m.core.pending = append([]string{head}, body...)
		model, cmd := m.startEval("", false)
		next := model.(replModel)
		// The head was echoed when it was first typed; the body was written
		// inside the overlay, so the transcript needs it now.
		next.echoPrompt, next.echoLine = promptContinue, strings.Join(body, "\n")
		return next, cmd
	}

	// The stepper is a whole model rather than a widget, so it sees every
	// message — including the window size and background reports it uses.
	if m.stepper != nil {
		next, cmd := m.stepper.Update(msg)
		m.stepper, _ = next.(*visualModel)
		quit, pass := stepperQuit(cmd)
		if quit {
			m.stepper = nil
			return m, nil
		}
		return m, pass
	}

	// An open overlay owns the keyboard: it is a mode, not a decoration.
	if key, isKey := msg.(tea.KeyPressMsg); isKey {
		switch {
		case m.picker != nil:
			open, path := m.picker.update(key)
			if open {
				return m, nil
			}
			command := m.picker.command
			m.picker = nil
			if path == "" {
				m.status = "(cancelled)"
				return m, nil
			}
			return m.startEval(pickedCommand(command, path), false)
		case m.browser != nil:
			open, statement := m.browser.update(key)
			if open {
				return m, nil
			}
			m.browser = nil
			if statement != "" {
				m.setLine(statement + " ")
			}
			return m, m.schedulePreview()
		case m.pager != nil:
			open, cmd := m.pager.update(msg)
			if !open {
				m.pager = nil
			}
			return m, cmd
		case m.search != nil:
			open, accepted := m.search.update(key, m.hist)
			if open {
				return m, nil
			}
			m.search = nil
			if accepted != "" {
				m.setLine(accepted)
			}
			return m, m.schedulePreview()
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil

	case spinner.TickMsg:
		if !m.evaluating {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case evalDoneMsg:
		return m.finishEval(msg)

	case previewTickMsg:
		if msg.gen != m.previewGen || m.evaluating {
			return m, nil
		}
		return m, m.previewCmd(m.previewGen, m.ti.Value())

	case previewMsg:
		if msg.gen == m.previewGen {
			m.preview = ""
			if msg.okay {
				m.preview = msg.typ
			}
		}
		return m, nil

	case editDoneMsg:
		return m.finishEdit(msg)

	case confirmQuitMsg:
		m.confirmingQuit = true
		m.stmtsAtQuitPrompt = len(m.core.stmts)
		m.status = "unsaved statements — `:save <file>` to keep them, `:quit!` or ctrl+c to discard"
		return m, nil

	case tea.BackgroundColorMsg:
		return m.retheme(isLightColor(msg.Color)), nil

	case tea.FocusMsg:
		m.focused = true
		return m, m.schedulePreview()

	case tea.BlurMsg:
		// Nobody is reading a preview of a window they are not looking at,
		// and resolving a large program is not free.
		m.focused = false
		return m, nil

	case tea.KeyboardEnhancementsMsg:
		// Ctrl+Enter only reaches a program on terminals that disambiguate
		// keys; where it does not, the help should not promise it.
		m.keys.ForceBlock.SetHelp("ctrl+enter", "force a block")
		return m, nil

	case tea.PasteMsg:
		return m.paste(string(msg.Content))

	case watchTickMsg:
		if m.watch == nil || msg.gen != m.watch.gen {
			return m, nil // a watch that has since been stopped or replaced
		}
		if m.evaluating || !m.watch.changed() {
			return m, m.watchTick()
		}
		// The file moved: replay what the session already has. `:list` is a
		// no-op statement-wise, so the replay goes through the same path a
		// submitted line does — spinner, interrupt, and all.
		model, cmd := m.startEval(":replay", false)
		return model, tea.Batch(cmd, m.watchTick())

	case tea.PasteStartMsg, tea.PasteEndMsg:
		// The content arrives as a single PasteMsg; these only bracket it.
		return m, nil

	case tea.ClipboardMsg:
		if !m.awaitingClipboard {
			return m, nil // an OSC 52 report this session did not ask for
		}
		m.awaitingClipboard = false
		if strings.TrimSpace(msg.Content) == "" {
			m.status = "(the clipboard is empty)"
			return m, nil
		}
		return m.paste(msg.Content)

	case tea.KeyPressMsg:
		return m.key(msg)
	}

	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

// retheme installs the palette the terminal's background calls for. It waits
// for a running program: the evaluation goroutine paints its own results into
// the session buffer, so swapping the styles under it would be a data race.
// The pending swap is applied in finishEval.
func (m replModel) retheme(light bool) replModel {
	if light == lightTheme {
		return m
	}
	if m.evaluating {
		m.pendingTheme = &light
		return m
	}
	useTheme(light)
	return m
}

// resize adapts the editor to the terminal: the input gets the width the
// prompt leaves it, and the profile charts get the whole line.
func (m replModel) resize(width, height int) replModel {
	m.width, m.height = width, height
	m.help.SetWidth(width)
	m.core.width = width
	if w := width - len(m.ti.Prompt) - 1; w > 0 {
		m.ti.SetWidth(w)
	}
	if m.pager != nil {
		m.pager.resize(width, height)
	}
	if m.block != nil {
		m.block.resize(width, height)
	}
	return m
}

// key handles one keystroke.
func (m replModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// While a program runs the editor is not accepting input: the only
	// question that can be answered is whether to stop it.
	if m.evaluating {
		if key.Matches(msg, m.keys.Cancel) && m.interrupt != nil {
			m.interrupt.Stop()
			m.interrupting = true
			m.status = "interrupting…"
			// Whatever a paste queued behind this line goes with it: the
			// interrupt was for the program, not for one statement of it.
			m.queue = nil
		}
		return m, nil
	}

	if msg.String() != "tab" {
		m.completing = false
		m.candidates = nil
	}
	// A keystroke answers whatever the last one said. The quit confirmation
	// is deliberately *not* cleared here: it is cleared when new unsaved work
	// appears (finishEval), because a confirmation that typing could revoke
	// would make `:quit` unable to ever leave — each retyped command would
	// re-arm the guard it was answering.
	m.status = ""

	switch {
	case key.Matches(msg, m.keys.Complete):
		return m.completeTab()

	case key.Matches(msg, m.keys.Cancel):
		return m.cancel()

	case key.Matches(msg, m.keys.Quit):
		if m.ti.Value() != "" {
			break // ctrl+d mid-line is delete-forward; let the editor have it
		}
		if len(m.core.pending) > 0 {
			m.status = "finish the block with an empty line, or ctrl+c to discard it"
			return m, nil
		}
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		return m, nil

	case key.Matches(msg, m.keys.Suspend):
		return m, tea.Suspend

	case key.Matches(msg, m.keys.Block):
		if len(m.core.pending) == 0 {
			m.status = "ctrl+o edits a block; there is no block open yet"
			return m, nil
		}
		head, body := m.core.pending[0], m.core.pending[1:]
		m.block = newBlockEditor(head, body, m.ti.Value(), m.width, m.height)
		return m, nil

	case key.Matches(msg, m.keys.Search):
		m.search = newHistorySearch(m.hist, m.ti.Value())
		return m, nil

	case key.Matches(msg, m.keys.Clear):
		// The scrollback is the session's record, so this clears the screen
		// rather than the history: what scrolled off is still up there.
		return m, tea.ClearScreen

	case key.Matches(msg, m.keys.HistoryPrev):
		if line, ok := m.hist.prev(m.ti.Value()); ok {
			m.setLine(line)
		}
		return m, m.schedulePreview()

	case key.Matches(msg, m.keys.HistoryNext):
		if line, ok := m.hist.next(); ok {
			m.setLine(line)
		}
		return m, m.schedulePreview()

	case key.Matches(msg, m.keys.Submit), key.Matches(msg, m.keys.ForceBlock):
		return m.submit(msg)
	}

	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	m.refreshSuggestions()
	return m, tea.Batch(cmd, m.schedulePreview())
}

// submit hands the current line to the session. Enter submits it as typed;
// Ctrl+Enter / Alt+Enter force continuation mode on a statement the parser
// would not otherwise ask for a block on — except on a :command, which has no
// block to open and would only become a parse error.
func (m replModel) submit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	line := strings.TrimRight(m.ti.Value(), " \t\r")
	isCommand := strings.HasPrefix(strings.TrimSpace(line), ":")
	if isCommand && isForceQuit(strings.TrimSpace(line)) {
		// An explicit `:quit!` is the answer to the unsaved-work guard, not
		// something for it to hold back. The statement count goes with it, so
		// finishing this very line does not look like new work and re-arm the
		// guard before the quit reaches it.
		m.confirmingQuit = true
		m.stmtsAtQuitPrompt = len(m.core.stmts)
	}
	force := key.Matches(msg, m.keys.ForceBlock) && len(m.core.pending) == 0 && !isCommand

	if force && line == "" {
		return m, nil
	}
	// A blank line with nothing to close does nothing at all — least of all
	// leave an empty prompt behind in the scrollback.
	if line == "" && len(m.core.pending) == 0 {
		return m, nil
	}
	if isCommand {
		if handled, model, cmd := m.watchCommand(strings.TrimSpace(line)); handled {
			m.hist.add(line)
			return model, cmd
		}
		if command, ok := wantsPicker(line); ok {
			m.hist.add(line)
			m.setLine("")
			m.picker = newPicker(command, m.core.baseDir)
			return m, nil
		}
		if handled, model, cmd := m.terminalCommand(strings.TrimSpace(line)); handled {
			return model, cmd
		}
	}
	m.hist.add(line)
	return m.startEval(line, force)
}

// startEval runs one line on a background command, leaving the event loop free
// to spin the spinner and listen for an interrupt.
func (m replModel) startEval(line string, force bool) (tea.Model, tea.Cmd) {
	m.evaluating, m.interrupting = true, false
	m.evalStarted = time.Now()
	m.echoPrompt, m.echoLine = m.ti.Prompt, line
	m.preview = ""
	m.progress.Reset()
	m.interrupt = ir.NewInterrupter(m.progress)
	m.core.trace = m.interrupt
	m.ti.SetValue("")

	core, interrupt := m.core, m.interrupt
	run := func() tea.Msg {
		replMu.Lock()
		defer replMu.Unlock()
		if force {
			// Forced continuation: the statement is held for its block
			// without asking the front end whether it wants one.
			core.pending = []string{line}
			return evalDoneMsg{}
		}
		quit := core.handleLine(line)
		_ = interrupt // kept alive for the interrupting goroutine
		return evalDoneMsg{quit: quit}
	}
	return m, tea.Batch(run, m.spin.Tick)
}

// finishEval moves what the session printed into the scrollback, resets the
// editor, and starts the next queued line if a paste left one.
func (m replModel) finishEval(msg evalDoneMsg) (tea.Model, tea.Cmd) {
	m.evaluating = false
	m.core.trace = nil
	m.interrupt = nil
	if m.pendingTheme != nil {
		useTheme(*m.pendingTheme)
		m.pendingTheme = nil
	}
	if m.confirmingQuit && len(m.core.stmts) != m.stmtsAtQuitPrompt {
		m.confirmingQuit = false // new unsaved work: the guard applies again
	}

	echoed := m.echoPrompt + highlightSource(m.echoLine, true)
	out := strings.TrimSuffix(m.buf.String()[m.seen:], "\n")
	m.seen = m.buf.Len()
	if out != "" && !m.pasting {
		echoed += "\n" + out
	}

	// textinput sanitizes literal tabs down to a single space (it's a
	// single-line widget), so seed with repl.go's own 4-space indent
	// convention instead of a tab — it survives SetValue untouched.
	seed := ""
	if len(m.core.pending) > 0 {
		seed = "    "
	}
	m.setLine(seed)
	m.ti.Prompt = m.core.prompt()
	m = m.resize(m.width, m.height)
	if m.interrupting {
		m.status = ""
	}

	if m.pasting {
		// Hold the line and its output: a pasted program is reported once,
		// below, rather than one intermediate value at a time.
		m.pasteEchoes = append(m.pasteEchoes, echoed)
		m.pasteOut = append(m.pasteOut, out)
		echoed = ""
		if len(m.queue) == 0 && !msg.quit {
			echoed = summarizePaste(m.pasteEchoes, m.pasteOut)
			m.pasting, m.pasteEchoes, m.pasteOut = false, nil, nil
		}
	}

	if tv := m.core.takeTrace(); tv != nil {
		m.stepper = newVisualModel(tv)
		m.stepper.width, m.stepper.height = m.width, m.height
		out = ""
	}

	var cmds []tea.Cmd
	if tooTallToPrint(out, m.height) {
		// The transcript keeps the line that asked; the answer opens a reader
		// rather than scrolling the session away.
		m.pager = newPager(pagerTitle(m.echoLine, out), out, m.width, m.height)
		m.pager.sortable = m.core.sortableStats()
		echoed = m.echoPrompt + highlightSource(m.echoLine, true)
	}
	if echoed != "" {
		cmds = append(cmds, tea.Println(echoed))
	}
	switch {
	case msg.quit:
		return m, tea.Sequence(append(cmds, tea.Quit)...)
	case len(m.queue) > 0:
		next := m.queue[0]
		m.queue = m.queue[1:]
		model, cmd := m.startEval(next, false)
		return model, tea.Batch(append(cmds, cmd)...)
	}
	cmds = append(cmds, m.schedulePreview())
	return m, tea.Batch(cmds...)
}

// cancel is Ctrl+C outside a running program: it abandons whatever is in
// progress — the line, then the block — and only quits when there is nothing
// left to abandon.
func (m replModel) cancel() (tea.Model, tea.Cmd) {
	switch {
	case m.ti.Value() != "":
		m.setLine("")
		m.status = "(cancelled)"
		return m, nil
	case len(m.core.pending) > 0:
		m.core.pending = nil
		m.ti.Prompt = m.core.prompt()
		m.setLine("")
		m.status = "(block discarded)"
		return m, nil
	}
	return m, tea.Quit
}

// paste handles bracketed paste. A single-line paste is ordinary typing; a
// multi-line one is a program, and is submitted line by line exactly as a
// piped script would be — the alternative, which is what a bare textinput
// does, is to collapse the newlines to spaces and silently mangle it.
func (m replModel) paste(content string) (tea.Model, tea.Cmd) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if !strings.Contains(content, "\n") {
		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(tea.PasteMsg{Content: content})
		m.refreshSuggestions()
		return m, tea.Batch(cmd, m.schedulePreview())
	}

	lines := strings.Split(content, "\n")
	// Whatever was already typed heads the first pasted line.
	lines[0] = m.ti.Value() + lines[0]
	if last := len(lines) - 1; strings.TrimSpace(lines[last]) == "" {
		lines = lines[:last] // a trailing newline is not a blank line to submit
	}
	for _, line := range lines {
		m.hist.add(strings.TrimRight(line, " \t"))
	}
	if m.evaluating {
		m.queue = append(m.queue, lines...)
		return m, nil
	}
	// The paste is reported as one thing when it finishes draining.
	m.pasting, m.pasteEchoes, m.pasteOut = true, nil, nil
	m.queue = append(m.queue, lines[1:]...)
	return m.startEval(strings.TrimRight(lines[0], " \t"), false)
}

// setLine replaces the input and puts the cursor at its end.
//
// It deliberately does not arm ghost text: a line the session placed there —
// recalled from history, accepted from a search, taken from the catalog — is
// the line that was asked for, and dimming a continuation onto the end of it
// only suggests it was not.
func (m *replModel) setLine(s string) {
	m.ti.SetValue(s)
	m.ti.CursorEnd()
	m.completing, m.candidates = false, nil
	m.ti.SetSuggestions(nil)
}

// View draws the prompt line, and whatever the session has to say about it.
func (m replModel) View() tea.View {
	if m.pager != nil {
		v := tea.NewView(m.pager.view())
		v.AltScreen = true
		v.WindowTitle = m.windowTitle()
		return v
	}

	var b strings.Builder
	if m.block != nil {
		v := tea.NewView(m.block.view(m.width))
		v.AltScreen = true
		v.WindowTitle = m.windowTitle()
		v.Cursor = m.block.cursor()
		return v
	}
	if m.stepper != nil {
		v := tea.NewView(m.stepper.View().Content)
		v.AltScreen = true
		v.WindowTitle = "domain visualize"
		return v
	}
	if m.browser != nil {
		v := tea.NewView(m.browser.view(m.width, m.height))
		v.AltScreen = true
		v.WindowTitle = m.windowTitle()
		return v
	}
	if m.picker != nil {
		v := tea.NewView(m.picker.view(m.width, m.height))
		v.AltScreen = true
		v.WindowTitle = m.windowTitle()
		return v
	}
	if m.search != nil {
		v := tea.NewView(m.search.view(m.width))
		v.WindowTitle = m.windowTitle()
		v.ReportFocus = true
		return v
	}
	if m.evaluating {
		b.WriteString(m.echoPrompt + highlightSource(m.echoLine, true) + "\n")
		b.WriteString(m.spin.View() + m.progressBar() + " " + styDim.Render(m.evalHint()))
	} else {
		b.WriteString(m.ti.View())
		if m.preview != "" {
			b.WriteString(styDim.Render("  : ") + styType.Render(m.preview))
		}
	}
	if m.completing && len(m.candidates) > 1 {
		b.WriteString("\n" + m.candidateBar())
	}
	if m.status != "" {
		b.WriteString("\n" + styDim.Render(m.status))
	}
	if m.showHelp {
		b.WriteString("\n" + m.help.View(m.keys))
	}

	v := tea.NewView(b.String())
	v.WindowTitle = m.windowTitle()
	v.Cursor = m.cursor()
	v.ReportFocus = true
	v.ProgressBar = m.terminalProgress()
	return v
}

// evalHint is the line under the spinner. It starts as a plain "this is
// running", grows an elapsed time once there is one worth reading, and after
// ten seconds says the thing the user is starting to wonder: a loop that never
// ends looks exactly like a loop that has not finished yet.
func (m replModel) evalHint() string {
	if m.interrupting {
		return "interrupting…"
	}
	elapsed := time.Since(m.evalStarted)
	switch {
	case elapsed < time.Second:
		return "evaluating… ctrl+c interrupts"
	case elapsed < 10*time.Second:
		return fmt.Sprintf("evaluating %.1fs… ctrl+c interrupts", elapsed.Seconds())
	default:
		return fmt.Sprintf("evaluating %.0fs… ctrl+c interrupts (an unbounded loop looks like this)",
			elapsed.Seconds())
	}
}

// progressBar draws how far the replay has got, once the program has resolved
// and the number of stages is known. A run that is still resolving, or one
// stuck inside a single long stage, simply has nothing to draw.
func (m replModel) progressBar() string {
	pct, known := m.progress.Percent()
	if !known {
		return ""
	}
	done, total := m.progress.Counts()
	return " " + heat(float64(pct), true).Render(bars(float64(pct), 10)) +
		styDim.Render(fmt.Sprintf(" %d/%d", min(done, total), total))
}

// terminalProgress drives the terminal's own progress indicator — the one a
// terminal shows on its tab or in the taskbar — so a long replay is visible
// from a window that is not on top. It is indeterminate until the stage count
// is known, and absent entirely when nothing is running.
func (m replModel) terminalProgress() *tea.ProgressBar {
	if !m.evaluating {
		return nil
	}
	if pct, known := m.progress.Percent(); known {
		return tea.NewProgressBar(tea.ProgressBarDefault, pct)
	}
	return tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
}

// cursor places the terminal's own cursor, and shapes it to say where the
// session is: a bar at the top level, a block inside an unfinished indented
// body, and no cursor at all while a program is running, since nothing typed
// then would be accepted.
func (m replModel) cursor() *tea.Cursor {
	if m.evaluating {
		return nil
	}
	c := m.ti.Cursor()
	if c == nil {
		return nil
	}
	c.Shape = tea.CursorBar
	if len(m.core.pending) > 0 {
		c.Shape = tea.CursorBlock
	}
	return c
}

// windowTitle keeps the current value's type in the terminal's title bar, so a
// session identifies itself in a tab strip full of shells.
func (m replModel) windowTitle() string {
	if m.core.lastType == "" {
		return "domain repl"
	}
	return "domain repl — " + m.core.lastType
}

// candidateBar shows the completion cycle in place: the candidate currently in
// the line, and what the next Tab will replace it with.
func (m replModel) candidateBar() string {
	const window = 8
	start := max(min(m.candIdx-window/2, len(m.candidates)-window), 0)
	end := min(start+window, len(m.candidates))

	cells := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		if i == m.candIdx {
			cells = append(cells, styCursor.Render(" "+m.candidates[i]+" "))
			continue
		}
		cells = append(cells, styDim.Render(" "+m.candidates[i]+" "))
	}
	line := strings.Join(cells, "")
	if len(m.candidates) > end-start {
		line += styDim.Render(fmt.Sprintf(" +%d", len(m.candidates)-(end-start)))
	}
	return truncateVis(line, m.width)
}

// --- completion -------------------------------------------------------------

// completeTab starts or advances a Tab-completion cycle: the first Tab on a
// token computes candidates via completeToken and shows the first one; each
// subsequent Tab (while still completing) advances to the next, wrapping
// around. Any other key (handled in key, above) resets the cycle before its
// own handling runs.
func (m replModel) completeTab() (tea.Model, tea.Cmd) {
	value := m.ti.Value()
	// The editor counts the cursor in runes and completeToken slices bytes,
	// so every crossing between them is converted: a single accented
	// character earlier in the line is enough to make the two disagree.
	cursor := byteOffset(value, m.ti.Position())

	if m.completing {
		m.candIdx = (m.candIdx + 1) % len(m.candidates)
	} else {
		candidates, tokenStart := completeToken(value, cursor, m.core.baseDir)
		if len(candidates) == 0 {
			return m, nil
		}
		m.candidates = candidates
		m.tokenStart = tokenStart
		m.candIdx = 0
		m.completing = true
	}

	candidate := m.candidates[m.candIdx]
	newValue := value[:m.tokenStart] + candidate + value[cursor:]
	m.ti.SetValue(newValue)
	m.ti.SetCursor(runeIndex(newValue, m.tokenStart+len(candidate)))
	m.refreshSuggestions()
	return m, m.schedulePreview()
}

// refreshSuggestions feeds the editor its ghost text: the first completion
// candidate, shown dimmed ahead of the cursor as you type. Suggestions are
// whole lines because that is what the widget matches against, and only when
// the cursor is at the end of one — a suggestion that completes the middle of
// a line would be showing text that is already there.
func (m *replModel) refreshSuggestions() {
	value := m.ti.Value()
	cursor := byteOffset(value, m.ti.Position())
	if cursor != len(value) || strings.TrimSpace(value) == "" {
		m.ti.SetSuggestions(nil)
		return
	}
	candidates, start := completeToken(value, cursor, m.core.baseDir)
	lines := make([]string, 0, len(candidates))
	for _, c := range candidates {
		full := value[:start] + c
		// Only candidates that continue what was typed *exactly*. The widget
		// matches case-insensitively, which would render "simple Domain: "
		// under a typed "s" — a spelling the completion would never produce
		// and the parser would not accept.
		if strings.HasPrefix(full, value) {
			lines = append(lines, full)
		}
	}
	m.ti.SetSuggestions(lines)
}

// byteOffset converts a rune index (what the editor counts in) to a byte
// offset (what the completion and the line itself are indexed by).
func byteOffset(s string, runeIdx int) int {
	if runeIdx <= 0 {
		return 0
	}
	for i := range s {
		if runeIdx == 0 {
			return i
		}
		runeIdx--
	}
	return len(s)
}

// runeIndex is byteOffset's inverse.
func runeIndex(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	if byteOff > len(s) {
		byteOff = len(s)
	}
	return utf8.RuneCountInString(s[:byteOff])
}

// --- live type preview ------------------------------------------------------

// schedulePreview arms the resolve that shows what the typed line would
// produce. Each keystroke supersedes the last, so a burst of typing resolves
// once, after it stops.
func (m *replModel) schedulePreview() tea.Cmd {
	m.previewGen++
	if !m.focused {
		return nil
	}
	gen := m.previewGen
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return previewTickMsg{gen: gen} })
}

// previewCmd resolves the session plus the line being typed, on copies of
// everything it needs, and reports the type the statement would produce. A
// line that does not resolve simply has no preview: an error message that
// appears while a statement is still half-typed is noise, not help.
func (m replModel) previewCmd(gen int, line string) tea.Cmd {
	stmts, ok := trialStatements(m.core, line)
	if !ok {
		return func() tea.Msg { return previewMsg{gen: gen} }
	}
	baseDir := m.core.baseDir
	return func() tea.Msg {
		replMu.Lock()
		defer replMu.Unlock()
		pipe, _, err := resolveStatements(stmts, baseDir)
		if err != nil || len(pipe.Nodes) == 0 {
			return previewMsg{gen: gen}
		}
		return previewMsg{gen: gen, typ: fmt.Sprint(pipe.Nodes[len(pipe.Nodes)-1].Out), okay: true}
	}
}

// trialStatements builds the program the line being typed would make, as a
// copy the background resolve can hold without racing the session. It reports
// false when there is nothing worth resolving.
func trialStatements(core *repl, line string) ([]string, bool) {
	if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), ":") {
		return nil, false
	}
	stmts := slices.Clone(core.stmts)
	if len(core.pending) > 0 {
		block := append(slices.Clone(core.pending), line)
		return append(stmts, strings.Join(block, "\n")), true
	}
	if line[0] == ' ' || line[0] == '\t' {
		return nil, false // an indented line with no block open resolves to nothing
	}
	return append(stmts, line), true
}

// --- terminal-only commands -------------------------------------------------

// terminalCommand handles the :commands that need the terminal itself. It
// reports whether it took the line; anything else falls through to the session
// core, which knows the rest.
func (m replModel) terminalCommand(line string) (bool, tea.Model, tea.Cmd) {
	name, _ := splitCommand(line)
	switch name {
	case ":edit":
		model, cmd := m.edit()
		return true, model, cmd
	case ":copy":
		program := strings.Join(m.core.stmts, "\n")
		if program == "" {
			m.status = "(empty domain)"
			return true, m, nil
		}
		m.setLine("")
		m.status = fmt.Sprintf("copied %d statement(s) to the clipboard", statementCount(m.core.stmts))
		return true, m, tea.SetClipboard(program + "\n")
	case ":doc":
		if _, arg := splitCommand(line); arg == "" {
			m.setLine("")
			m.browser = newDocBrowser()
			return true, m, nil
		}
		return false, m, nil

	case ":paste":
		m.setLine("")
		m.awaitingClipboard = true
		m.status = "reading the clipboard…"
		return true, m, tea.ReadClipboard

	case ":keys":
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		m.setLine("")
		return true, m, nil
	}
	return false, m, nil
}

// edit writes the session out, opens it in $EDITOR, and reloads whatever comes
// back — the REPL's answer to a statement six lines up being the wrong one.
func (m replModel) edit() (tea.Model, tea.Cmd) {
	editor := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	path := filepath.Join(os.TempDir(), fmt.Sprintf("domain-repl-%d.domain", os.Getpid()))
	program := strings.Join(m.core.stmts, "\n")
	if program != "" {
		program += "\n"
	}
	if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
		m.status = fmt.Sprintf("cannot write %s: %v", path, err)
		return m, nil
	}
	m.setLine("")

	// A shell-less exec keeps a path with spaces working; the editor
	// command itself may still be `code -w`-style, so it is split on spaces.
	fields := strings.Fields(editor)
	cmd := exec.Command(fields[0], append(fields[1:], path)...) //nolint:gosec // the user's own $EDITOR
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editDoneMsg{path: path, err: err}
	})
}

// finishEdit reloads the edited program, keeping the session untouched if it
// does not resolve.
func (m replModel) finishEdit(msg editDoneMsg) (tea.Model, tea.Cmd) {
	defer func() { _ = os.Remove(msg.path) }()
	if msg.err != nil {
		m.status = fmt.Sprintf("editor exited: %v", msg.err)
		return m, nil
	}
	src, err := os.ReadFile(msg.path)
	if err != nil {
		m.status = fmt.Sprintf("cannot read back %s: %v", msg.path, err)
		return m, nil
	}
	replMu.Lock()
	m.core.adopt(string(src))
	replMu.Unlock()

	out := strings.TrimSuffix(m.buf.String()[m.seen:], "\n")
	m.seen = m.buf.Len()
	m.ti.Prompt = m.core.prompt()
	m = m.resize(m.width, m.height)
	if out == "" {
		return m, nil
	}
	return m, tea.Println(out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// --- key bindings -----------------------------------------------------------

// replKeyMap is the editor's own bindings — the ones textinput does not
// already provide. Having them in one place is what lets `:keys` and Ctrl+G
// print an accurate list rather than a hand-maintained one.
type replKeyMap struct {
	Submit      key.Binding
	ForceBlock  key.Binding
	Complete    key.Binding
	HistoryPrev key.Binding
	HistoryNext key.Binding
	Cancel      key.Binding
	Quit        key.Binding
	Help        key.Binding
	Suspend     key.Binding
	Clear       key.Binding
	Search      key.Binding
	Block       key.Binding
}

func defaultReplKeys() replKeyMap {
	return replKeyMap{
		Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
		ForceBlock: key.NewBinding(key.WithKeys("ctrl+enter", "alt+enter", "shift+enter"),
			key.WithHelp("alt+enter", "force a block")),
		Complete:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete / cycle")),
		HistoryPrev: key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "previous line")),
		HistoryNext: key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next line")),
		Cancel:      key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "interrupt / clear")),
		Quit:        key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "quit on an empty line")),
		Help:        key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "keys")),
		Suspend:     key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("ctrl+z", "suspend")),
		Clear:       key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear the screen")),
		Search:      key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "search history")),
		Block:       key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "edit the whole block")),
	}
}

// ShortHelp and FullHelp make the key map printable by bubbles/help.
func (k replKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Complete, k.HistoryPrev, k.Cancel, k.Help}
}

func (k replKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.ForceBlock, k.Complete},
		{k.HistoryPrev, k.HistoryNext, k.Suspend},
		{k.Cancel, k.Quit, k.Help},
		{k.Clear, k.Search, k.Block},
	}
}

// isForceQuit reports the spellings that mean "leave without saving".
func isForceQuit(line string) bool {
	name, _ := splitCommand(line)
	return name == ":quit!" || name == ":q!"
}

// summarizePaste renders a drained paste as one block: the statements as
// pasted, whatever they had to say that was not just a value (errors, command
// output), and the value the program ended on.
//
// The intermediate `=> value : Type` lines are dropped on purpose. They are
// the point when a statement is typed and the noise when forty arrive at once,
// where the questions are "did it take" and "what did it end up with".
func summarizePaste(echoes, outs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", promptTop, styDim.Render(fmt.Sprintf("(pasted %d line(s))", len(echoes))))
	for _, echo := range echoes {
		b.WriteString(strings.TrimPrefix(echo, promptTop) + "\n")
	}

	var last string
	for _, out := range outs {
		switch {
		case strings.TrimSpace(out) == "":
		case isResultLine(out):
			last = out // superseded by any later one
		default:
			b.WriteString(out + "\n") // an error, or a :command's own output
		}
	}
	if last != "" {
		b.WriteString(last + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// isResultLine reports whether output is just a `=> value : Type` line — the
// kind summarizePaste keeps only the last of.
func isResultLine(out string) bool {
	plain := ansi.Strip(out)
	return !strings.Contains(strings.TrimSuffix(plain, "\n"), "\n") && strings.HasPrefix(plain, "=> ")
}
