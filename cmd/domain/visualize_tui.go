// The bubbletea stepper for `domain expansion: visualize`. The recording it
// walks is built in interp (record.go, timing.go) and is pure Go — that is
// where the trace coverage lives; this file is layout and key handling, tested
// the way repl_tty.go is, by driving the model with injected messages.
//
// The stepper is two panes: the left is always the recorded tree, and the right
// switches between the selected row's value, the optimizer's rewrites, the
// timing profile, and the program source with a per-line share of the run. Each
// answers a different question about the same recording, and only one of them
// fits beside the tree at a time.
//
// Two views take the whole terminal instead, because half of one would not do:
// the Go the compiler backend emits (a program in its own right, read by
// scrolling) and the key list, which is why the footer does not carry one.
package main

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"domain/codegen"
	"domain/interp"
)

// detailPane is what the right-hand pane is currently showing.
type detailPane int

const (
	paneValue   detailPane = iota // the selected step's input and output
	paneExplain                   // the optimizer's rewrites
	paneHot                       // the timing profile, worst first
	paneSource                    // the program, with each line's share of the run
	paneExpr                      // the selected step's Using: expression, piece by piece
	paneDiff                      // what the step changed, in against out
)

// paneFocus is which half of the screen the movement keys drive.
//
// The tree used to own them outright, which made every other pane a poster: the
// value pane could hold 64 KiB of captured list and show the eleven lines that
// fit, with no way to see the twelfth. Focus is the smallest thing that fixes
// that — tab moves it, and j/k mean "move the cursor" or "scroll what I am
// reading" depending on where it is.
type paneFocus int

const (
	focusTree paneFocus = iota
	focusDetail
)

// screen is what fills the terminal: the two-pane stepper, or one of the views
// that earn the whole width — the emitted Go, which is a program in its own
// right and unreadable in half a pane, and the key list.
type screen int

const (
	screenTree screen = iota
	screenGo
	screenHelp
)

// visRow is one visible line of the tree: a node plus its indentation depth.
type visRow struct {
	node  *interp.TraceNode
	depth int
}

// visualModel is the stepper's state.
type visualModel struct {
	view     *traceView
	rows     []visRow
	expanded map[*interp.TraceNode]bool
	parents  map[*interp.TraceNode]*interp.TraceNode
	flat     []*interp.TraceNode       // every node in tree order, for the jump keys
	order    map[*interp.TraceNode]int // a node's position in flat
	keys     map[*interp.TraceNode]string
	cursor   int
	width    int
	height   int
	pane     detailPane
	screen   screen
	focus    paneFocus

	// treeShare is the tree pane's width as a percentage of the terminal. It is
	// adjustable because the right answer depends on the program: a pipeline of
	// short stage names wants a narrow tree, and one full of `Channel "…"`
	// labels wants most of the screen.
	treeShare int

	// detailTop is the first line of the right-hand pane on screen, and
	// detailLen how many lines it last rendered — the pair a scroll needs to
	// know where its bottom is.
	detailTop, detailLen int

	// goTop is the first line of the emitted program on screen. The Go view
	// scrolls freely — it opens at the selected row's code and goes wherever
	// the reader takes it from there.
	goTop int

	// helpTop scrolls the key list, which outgrew a short terminal.
	helpTop int

	// goFind is the code screen's own search. It is separate from the tree's
	// filter because the two search different things and share no rows.
	goFind      string
	searchingGo bool

	// helpFrom is the screen `?` was pressed on, so the key list returns the
	// reader to what they were reading rather than to the tree.
	helpFrom screen

	// Search narrows the tree to matching rows and the paths that reach them.
	// parsed is filter as terms, and parsedFrom the text it was parsed from.
	filter     string
	searching  bool
	parsed     []searchTerm
	parsedFrom string
	// jump collects a step number while `:` is being typed.
	jumping bool
	jumpBuf string

	// spec is how to record this program again; watch re-records on its own
	// when a file changes; recording guards against two runs at once. See
	// visualize_record.go.
	spec      recordSpec
	watch     *visWatch
	recording bool

	// changed marks rows whose output differs from the recording this one
	// replaced, so a re-record shows its own consequences.
	changed map[*interp.TraceNode]bool

	// quitting records that the stepper is leaving, alongside the tea.Quit it
	// returns. The REPL embeds this model as an overlay and has to tell "the
	// reader pressed q" from "the reader started something"; reading a flag lets
	// it do that without running the command to look at it. See stepperQuit.
	quitting bool

	// status is a one-shot message in the footer — what a jump key did, or why
	// it did nothing. It clears on the next keystroke, so it never goes stale.
	status string
}

// quit ends the stepper, recording that it was a quit.
func (m *visualModel) quit() tea.Cmd {
	m.quitting = true
	return tea.Quit
}

func newVisualModel(view *traceView) *visualModel {
	m := &visualModel{
		width:     100,
		height:    30,
		treeShare: defaultTreeShare,
		changed:   map[*interp.TraceNode]bool{},
	}
	m.adopt(view)
	return m
}

// defaultTreeShare is how much of the width the tree takes before anyone
// adjusts it.
const defaultTreeShare = 55

// stepNumberWidth is how wide the tree pane has to be before it carries step
// numbers as well as labels. At the default share that is a terminal of about
// 116 columns — wide enough that six columns of number cost the label nothing
// it needed, and `>` reaches it on a narrower one.
const stepNumberWidth = 64

// adopt installs a recording, building the indexes that describe it. It is
// separate from newVisualModel because a re-record installs a *new* recording
// into a model a reader has already arranged (see visualize_record.go), and
// everything derived from the tree has to be rebuilt against the new one while
// everything the reader chose stays where it is.
func (m *visualModel) adopt(view *traceView) {
	m.view = view
	m.expanded = map[*interp.TraceNode]bool{}
	m.parents = map[*interp.TraceNode]*interp.TraceNode{}
	m.order = map[*interp.TraceNode]int{}
	m.flat = nil
	m.detailTop = 0

	// The parent links and tree order never change within a recording — it is
	// finished — so they are built once here rather than on every rebuild. The
	// position index goes with them: the diff pane asks "what ran before this
	// row" on every frame it draws, and scanning a quarter of a million rows to
	// answer it would be a search per keystroke.
	var index func(nodes []*interp.TraceNode, parent *interp.TraceNode)
	index = func(nodes []*interp.TraceNode, parent *interp.TraceNode) {
		for _, n := range nodes {
			m.parents[n] = parent
			m.order[n] = len(m.flat)
			m.flat = append(m.flat, n)
			index(n.Children, n)
		}
	}
	index(view.rec.Roots(), nil)
	m.keys = nodeKeys(view.rec.Roots())

	// Frames start collapsed: a loop with 400 iterations should not bury the
	// stages around it. Everything else has no children to hide.
	m.rebuild()
}

// expandAll opens every frame, which is what --expand-loops asks for. The flag
// used to be read only by the text printer, so on a terminal it was accepted
// and quietly did nothing.
func (m *visualModel) expandAll() {
	for _, n := range m.flat {
		if len(n.Children) > 0 {
			m.expanded[n] = true
		}
	}
	m.rebuild()
}

// Init asks the terminal for its background color so the palette can match it
// (visualize_style.go); the dark default stands until an answer arrives.
func (m *visualModel) Init() tea.Cmd { return tea.RequestBackgroundColor }

// rebuild flattens the tree into the visible rows, honoring collapse state and
// the active filter.
func (m *visualModel) rebuild() {
	// Keep the cursor on the row it was on: after a filter changes, the row at
	// index 7 is a different row, and a cursor that jumps is disorienting.
	var was *interp.TraceNode
	if m.cursor < len(m.rows) {
		was = m.rows[m.cursor].node
	}

	keep := m.matching()
	m.rows = m.rows[:0]
	var walk func(nodes []*interp.TraceNode, depth int)
	walk = func(nodes []*interp.TraceNode, depth int) {
		for _, n := range nodes {
			if keep != nil && !keep[n] {
				continue
			}
			m.rows = append(m.rows, visRow{node: n, depth: depth})
			if len(n.Children) > 0 && m.isOpen(n) {
				walk(n.Children, depth+1)
			}
		}
	}
	walk(m.view.rec.Roots(), 0)

	if was != nil {
		for i, r := range m.rows {
			if r.node == was {
				m.cursor = i
				return
			}
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
}

// isOpen reports whether a row's children are on screen. A filtered tree opens
// itself — hiding a match inside a collapsed frame would make the search look
// like it found nothing — so this is the one answer the collapse markers, the
// hidden-work counts and the rebuild all have to agree on.
func (m *visualModel) isOpen(n *interp.TraceNode) bool {
	return m.expanded[n] || m.filter != ""
}

func (m *visualModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		useTheme(isLightColor(msg.Color))
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case recordedMsg:
		m.finishRecording(msg)
		return m, m.watch.tick()

	case visWatchTickMsg:
		if m.watch == nil || msg.gen != m.watch.gen {
			return m, nil
		}
		if path, ok := m.watch.changed(); ok {
			return m, m.startRecording(shortPath(path))
		}
		return m, m.watch.tick()

	case editorDoneMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("editor exited: %v", msg.err)
			return m, nil
		}
		// The recording is of the program as it was, so it is not silently
		// stale — but the reader has almost certainly just changed the thing it
		// describes, and `r` is the key that catches it up.
		m.status = "back from the editor — r records the program again"
		return m, nil

	case tea.KeyPressMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		if m.jumping {
			return m.updateJump(msg)
		}
		m.status = ""
		if m.screen != screenTree {
			return m.updateScreen(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, m.quit()
		case "esc":
			// Escape backs out of whatever is narrowing the view before it
			// leaves: the detail pane's focus, then a filtered tree, then a
			// pane, then the program.
			switch {
			case m.focus != focusTree:
				m.focus = focusTree
			case m.filter != "":
				m.filter = ""
				m.rebuild()
			case m.pane != paneValue:
				m.pane = paneValue
			default:
				return m, m.quit()
			}
		case "tab":
			m.toggleFocus()
		case "down", "j":
			m.moveDown(1)
		case "up", "k":
			m.moveUp(1)
		case "ctrl+d", "pgdown":
			m.moveDown(m.pageSize())
		case "ctrl+u", "pgup":
			m.moveUp(m.pageSize())
		case "right", "l", "enter":
			if m.focus == focusDetail {
				m.enterDetail()
				break
			}
			m.expand()
		case "left", "h":
			if m.focus == focusDetail {
				m.focus = focusTree
				break
			}
			m.collapse()
		case "g":
			m.toTop()
		case "G":
			m.toBottom()
		case "e":
			m.togglePane(paneExplain)
		case "t":
			m.togglePane(paneHot)
		case "s":
			m.togglePane(paneSource)
		case "x":
			m.togglePane(paneExpr)
		case "d":
			m.togglePane(paneDiff)
		case "c":
			m.openGo()
		case "?":
			m.openHelp()
		case "H":
			m.jumpToHottest()
		case "!":
			m.jumpToFailure()
		case "/":
			m.searching = true
		case "n":
			m.nextMatch(1)
		case "N":
			m.nextMatch(-1)
		case ":", "#":
			m.jumping, m.jumpBuf = true, ""
		case "<":
			m.resizeTree(-5)
		case ">":
			m.resizeTree(5)
		case "r":
			return m, m.startRecording("r")
		case "w":
			m.writeRecording()
		case "y":
			return m, m.yankValue()
		case "o":
			return m, m.openEditor()
		}
	}
	return m, nil
}

// pageSize is how far ctrl+d and ctrl+u move, in either half of the screen.
func (m *visualModel) pageSize() int { return max(1, (m.height-4)/2) }

// toggleFocus moves between driving the tree and scrolling what is being read.
// A pane with nothing off-screen refuses focus rather than swallowing the keys
// silently.
func (m *visualModel) toggleFocus() {
	if m.focus == focusDetail {
		m.focus = focusTree
		return
	}
	m.focus = focusDetail
	m.status = "scrolling the pane — tab or esc returns to the tree"
}

// moveDown moves the cursor or scrolls the pane, depending on the focus.
func (m *visualModel) moveDown(n int) {
	if m.focus == focusDetail {
		m.detailTop = min(m.detailTop+n, m.detailBottom())
		return
	}
	m.cursor = min(m.cursor+n, max(0, len(m.rows)-1))
	m.detailTop = 0 // a new row is a new thing to read, from its top
}

func (m *visualModel) moveUp(n int) {
	if m.focus == focusDetail {
		m.detailTop = max(m.detailTop-n, 0)
		return
	}
	m.cursor = max(m.cursor-n, 0)
	m.detailTop = 0
}

func (m *visualModel) toTop() {
	if m.focus == focusDetail {
		m.detailTop = 0
		return
	}
	m.cursor, m.detailTop = 0, 0
}

func (m *visualModel) toBottom() {
	if m.focus == focusDetail {
		m.detailTop = m.detailBottom()
		return
	}
	m.cursor, m.detailTop = max(0, len(m.rows)-1), 0
}

// detailWidth is how wide the right-hand pane renders, which both the view and
// a scroll that has to measure the pane need to agree on.
func (m *visualModel) detailWidth() int {
	treeW := max(m.width*m.treeShare/100, 30)
	return max(m.width-treeW-3, 20)
}

// detailBottom is the furthest the detail pane can scroll: far enough to bring
// the last line into view, and no further, so the pane never scrolls off into
// blank space.
//
// It measures the pane itself when nothing has rendered yet, rather than
// trusting the length the last frame left behind: the first keystroke after a
// pane opens arrives before that frame exists, and a bottom of zero would pin
// the scroll at the top for exactly one press.
func (m *visualModel) detailBottom() int {
	if m.detailLen == 0 {
		m.detailLen = len(m.detailLines(m.detailWidth()))
	}
	body := max(1, m.height-3)
	return max(0, m.detailLen-body)
}

// enterDetail is what `enter` does with the focus in the right-hand pane: the
// navigable panes jump the tree to whatever the pane's own cursor is on, which
// is what makes the profile a way to get somewhere rather than a poster.
func (m *visualModel) enterDetail() {
	switch m.pane {
	case paneHot:
		m.jumpToHotspot()
	case paneSource:
		m.jumpToSourceLine()
	default:
		m.focus = focusTree
	}
}

// resizeTree widens or narrows the tree pane, within bounds that keep both
// halves usable.
func (m *visualModel) resizeTree(delta int) {
	m.treeShare = min(max(m.treeShare+delta, 25), 80)
	m.status = fmt.Sprintf("tree %d%% of the width", m.treeShare)
}

// updateScreen handles keys while a full-screen view is up. Both close the same
// way — the key that opened them, esc, or q — because a view you are *in* is
// somewhere to come back from, not a program to quit. ctrl+c still quits.
func (m *visualModel) updateScreen(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, m.quit()
	}
	if m.searchingGo {
		return m.updateGoSearch(msg)
	}
	page := max(1, (m.height-4)/2)
	if m.screen == screenHelp {
		// The key list scrolls now: it outgrew a short terminal, and silently
		// dropping the last two sections meant the keys a reader had not
		// learned yet were the ones they could not see. Anything that is not a
		// scroll still leaves, since it is a reference and not a mode.
		// Far enough to bring the last line into view, and no further: a G that
		// scrolled to the final *line* would leave a screen of blank above it.
		last := max(0, len(m.helpBody())-max(1, m.height-2))
		switch key {
		case "down", "j":
			m.helpTop = min(m.helpTop+1, last)
		case "up", "k":
			m.helpTop = max(m.helpTop-1, 0)
		case "ctrl+d", "pgdown", " ":
			m.helpTop = min(m.helpTop+page, last)
		case "ctrl+u", "pgup":
			m.helpTop = max(m.helpTop-page, 0)
		case "g":
			m.helpTop = 0
		case "G":
			m.helpTop = last
		default:
			m.screen, m.helpTop = m.helpFrom, 0
		}
		return m, nil
	}
	last := max(0, len(m.goSrc())-max(1, m.height-3))
	switch key {
	case "esc", "q", "c":
		m.screen = screenTree
	case "?":
		m.openHelp()
	case "down", "j":
		m.goTop = min(m.goTop+1, last)
	case "up", "k":
		m.goTop = max(m.goTop-1, 0)
	case "ctrl+d", "pgdown", " ":
		m.goTop = min(m.goTop+page, last)
	case "ctrl+u", "pgup":
		m.goTop = max(m.goTop-page, 0)
	case "g":
		m.goTop = 0
	case "G":
		m.goTop = last
	case "/":
		// The code screen searches its own text rather than the tree: the two
		// have nothing to do with each other, and a `/` here that quietly
		// filtered the rows behind this screen would be the wrong answer given
		// confidently.
		m.searchingGo, m.goFind = true, ""
	case "n":
		m.findInGo(1)
	case "N":
		m.findInGo(-1)
	case "z":
		// Back to the selected row's code, for a reader who scrolled away and
		// wants the stage they came in on.
		m.goTop = m.goStart()
	}
	return m, nil
}

// goSrc is the emitted program's lines, or nothing when it could not be
// compiled — the view says why in that case.
func (m *visualModel) goSrc() []string {
	src, _, _ := m.view.emitted()
	return src
}

// goSpan is the emitted lines the selected row became, if it became any.
func (m *visualModel) goSpan() (codegen.Span, bool) {
	_, spans, err := m.view.emitted()
	if err != nil {
		return codegen.Span{}, false
	}
	s := m.selected()
	if s == nil {
		return codegen.Span{}, false
	}
	span, ok := spans[s.Node]
	return span, ok
}

// goStart is where the Go view opens: at the selected row's code, roughly
// centered, so the answer to "what did this stage become" is on screen without
// hunting for it. A row with no code of its own starts at the top.
func (m *visualModel) goStart() int {
	span, ok := m.goSpan()
	if !ok {
		return 0
	}
	body := max(1, m.height-4)
	return min(max(span.Start-body/3-1, 0), max(0, len(m.goSrc())-body))
}

// openGo switches to the emitted program, at the selected row's code.
func (m *visualModel) openGo() {
	m.screen = screenGo
	m.goTop = m.goStart()
}

// openHelp shows the key list over whatever is on screen.
func (m *visualModel) openHelp() {
	m.helpFrom = m.screen
	m.screen = screenHelp
	m.helpTop = 0
}

// updateGoSearch handles keys while a search is being typed on the code screen.
// It scrolls to a match as it is typed, so the search is the result rather than
// a prelude to it — the same shape as the tree's.
func (m *visualModel) updateGoSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, m.quit()
	case "esc":
		m.searchingGo, m.goFind = false, ""
	case "enter":
		// Accept, and re-report where the search landed: every keystroke clears
		// the status line, so without this the enter that accepts a search wipes
		// the line telling you what it found.
		m.searchingGo = false
		m.findInGo(0)
	case "backspace":
		if r := []rune(m.goFind); len(r) > 0 {
			m.goFind = string(r[:len(r)-1])
			m.findInGo(0)
		}
	default:
		if t := msg.Text; t != "" && !strings.ContainsFunc(t, unicodeIsControl) {
			m.goFind += t
			m.findInGo(0)
		}
	}
	return m, nil
}

// findInGo scrolls the code screen to a line containing the search text: the
// next one with dir 1, the previous with -1, and the nearest at or after the
// top of the window with 0, which is what typing does.
func (m *visualModel) findInGo(dir int) {
	if m.goFind == "" {
		m.status = "no search on this screen — / starts one"
		return
	}
	src := m.goSrc()
	if len(src) == 0 {
		return
	}
	needle := strings.ToLower(m.goFind)
	start := m.goTop
	if dir != 0 {
		start = m.goTop + dir
	}
	for i := range src {
		idx := ((start+dir*i)%len(src) + len(src)) % len(src)
		if dir == 0 {
			idx = (start + i) % len(src)
		}
		if strings.Contains(strings.ToLower(src[idx]), needle) {
			m.goTop = min(idx, max(0, len(src)-max(1, m.height-3)))
			m.status = fmt.Sprintf("line %d matches %q", idx+1, m.goFind)
			return
		}
	}
	m.status = fmt.Sprintf("no line matches %q", m.goFind)
}

// unicodeIsControl is unicode.IsControl, named locally so the search code can
// use it without this file's import list growing a package for one predicate.
func unicodeIsControl(r rune) bool { return unicode.IsControl(r) }

// togglePane opens a pane, or closes it back to the value view when it is
// already open — one key in, the same key out. Opening a pane scrolls it to the
// top: it is a new thing to read.
func (m *visualModel) togglePane(p detailPane) {
	m.detailTop = 0
	if m.pane == p {
		m.pane = paneValue
		return
	}
	m.pane = p
}

// expand opens the row under the cursor, or steps into it when already open.
func (m *visualModel) expand() {
	if len(m.rows) == 0 {
		return
	}
	cur := m.rows[m.cursor]
	if len(cur.node.Children) == 0 {
		return
	}
	if m.expanded[cur.node] {
		m.cursor++ // already open: step into the first child
		return
	}
	m.expanded[cur.node] = true
	m.rebuild()
}

// collapse closes the row under the cursor, or moves to its parent when the row
// has nothing to close.
func (m *visualModel) collapse() {
	if len(m.rows) == 0 {
		return
	}
	cur := m.rows[m.cursor]
	if m.expanded[cur.node] {
		delete(m.expanded, cur.node)
		m.rebuild()
		return
	}
	// Walk back to the nearest shallower row: the parent.
	for i := m.cursor - 1; i >= 0; i-- {
		if m.rows[i].depth < cur.depth {
			m.cursor = i
			return
		}
	}
}

// jumpToHottest moves to the row with the most self time. On a recording deep
// enough to need a profile, that row is usually inside a collapsed frame, which
// is exactly why hunting for it by hand does not work.
func (m *visualModel) jumpToHottest() {
	hot := m.view.times().Hottest()
	if hot == nil {
		m.status = "nothing recorded"
		return
	}
	nt := m.view.times().Of(hot)
	m.reveal(hot)
	m.status = fmt.Sprintf("hottest: %s, %s self (%s)",
		hot.Label(), interp.FormatDuration(nt.Self), pctText(nt.SelfPct, nt.Known))
}

// jumpToFailure moves to the next failing step, wrapping. A failed run is the
// common reason to open a debugger, and the failing step can be thousands of
// rows into a recording.
func (m *visualModel) jumpToFailure() {
	var cur *interp.TraceNode
	if m.cursor < len(m.rows) {
		cur = m.rows[m.cursor].node
	}
	start := 0
	for i, n := range m.flat {
		if n == cur {
			start = i + 1
			break
		}
	}
	for i := range m.flat {
		n := m.flat[(start+i)%len(m.flat)]
		if n.IsFrame() || n.Step.Err == nil {
			continue
		}
		m.reveal(n)
		m.status = "failure: " + n.Step.Err.Error()
		return
	}
	m.status = "no failing step in this recording"
}

// reveal opens every frame between a row and the top and puts the cursor on it.
// A jump that landed on a row nobody could see would not be a jump.
func (m *visualModel) reveal(target *interp.TraceNode) {
	for p := m.parents[target]; p != nil; p = m.parents[p] {
		m.expanded[p] = true
	}
	// A filter that hides the target would silently swallow the jump, so the
	// jump wins and the filter is dropped.
	if m.filter != "" && !m.matching()[target] {
		m.filter = ""
	}
	m.rebuild()
	for i, r := range m.rows {
		if r.node == target {
			m.cursor = i
			return
		}
	}
}

// selectedNode returns the row under the cursor, step or frame.
func (m *visualModel) selectedNode() *interp.TraceNode {
	if len(m.rows) == 0 {
		return nil
	}
	return m.rows[m.cursor].node
}

// selected returns the step under the cursor, or nil on a frame row.
func (m *visualModel) selected() *interp.Step {
	if n := m.selectedNode(); n != nil {
		return n.Step
	}
	return nil
}

func (m *visualModel) View() tea.View {
	switch m.screen {
	case screenGo:
		return fullScreen(m.goView())
	case screenHelp:
		return fullScreen(m.helpView())
	}
	treeW := max(m.width*m.treeShare/100, 30)
	detailW := m.detailWidth()
	bodyH := max(m.height-3, 3)

	left := m.treeLines(treeW, bodyH)

	// The pane renders all of itself and the view takes the window it can show.
	// That is what lets it scroll at all: the recorder keeps up to 64 KiB of a
	// value, and a pane that only ever built the eleven lines that fit had no
	// way to reach the twelfth.
	full := m.detailLines(detailW)
	m.detailLen = len(full)
	if m.detailTop > m.detailBottom() {
		m.detailTop = m.detailBottom()
	}
	right := full
	if m.detailTop < len(full) {
		right = full[m.detailTop:]
	} else {
		right = nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", pad(m.title(), m.width))
	divider := styRule.Render("│")
	for i := range bodyH {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		fmt.Fprintf(&b, "%s %s %s\n", pad(l, treeW), divider, pad(r, detailW))
	}
	b.WriteString(pad(m.footer(), m.width))
	return fullScreen(b.String())
}

// maxDetailLines bounds what a pane builds. A recording can hold a 64 KiB value
// and the source pane a whole file; neither reaches this, and a pathological
// one stops here rather than restyling a hundred thousand lines per keystroke.
const maxDetailLines = 4000

// fullScreen wraps rendered content as the view. In bubbletea v2 the alternate
// screen is a property of the view, not a program option: the stepper takes the
// whole terminal and restores it on exit.
func fullScreen(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// goView renders the emitted program across the whole terminal: a title, the
// code, and a footer of its own keys.
//
// It answers the question the source pane cannot — a Domain stage is one line,
// and what it *is*, the loop and the allocation and the scan, is twenty lines
// of Go — and it answers it about the *program*, not only the selected row: the
// view opens at that row's code and scrolls anywhere from there, because the
// code around a stage is most of what makes it legible.
func (m *visualModel) goView() string {
	src, _, err := m.view.emitted()
	body := max(m.height-3, 1)

	var b strings.Builder
	if err != nil {
		fmt.Fprintf(&b, "%s\n", pad(styTitle.Render("  emitted go "), m.width))
		for _, line := range wrapVis(err.Error(), m.width-4) {
			fmt.Fprintf(&b, "%s\n", pad("  "+styErr.Render(line), m.width))
		}
		fmt.Fprintf(&b, "%s\n", pad(styDim.Render(
			"  the interpreter ran this program; the compiler backend cannot lower it yet"), m.width))
		b.WriteString(pad(styDim.Render("  esc back · ")+styKey.Render("?")+styDim.Render(" keys"), m.width))
		return b.String()
	}

	head := styTitle.Render("  emitted go ") + styDim.Render(fmt.Sprintf("%s · %s",
		m.view.path, plural(len(src), "line")))
	fmt.Fprintf(&b, "%s\n", pad(head, m.width))
	fmt.Fprintf(&b, "%s\n", pad("  "+styDim.Render(m.goWhere()), m.width))

	span, marked := m.goSpan()
	for i := m.goTop; i < m.goTop+body-1; i++ {
		if i >= len(src) {
			b.WriteString(pad("", m.width) + "\n")
			continue
		}
		line := i + 1
		text := truncateVis(src[i], max(4, m.width-7))
		// The selected row's own lines are lit and marked in the gutter, so the
		// stage stays findable however far the reader has scrolled from it.
		if marked && line >= span.Start && line < span.End {
			fmt.Fprintf(&b, "%s\n", pad(styKey.Render("▌")+styDim.Render(fmt.Sprintf("%4d ", line))+
				styValue.Render(text), m.width))
			continue
		}
		style := styLabel
		if m.goFind != "" && strings.Contains(strings.ToLower(src[i]), strings.ToLower(m.goFind)) {
			style = styMatch
		}
		fmt.Fprintf(&b, "%s\n", pad(" "+styDim.Render(fmt.Sprintf("%4d ", line))+style.Render(text), m.width))
	}
	b.WriteString(pad(m.goFooter(len(src)), m.width))
	return b.String()
}

// goWhere is the line under the title: which lines belong to the row the view
// was opened on, or why none do.
func (m *visualModel) goWhere() string {
	if span, ok := m.goSpan(); ok {
		return fmt.Sprintf("%s → lines %d–%d", m.rowLabel(), span.Start, span.End-1)
	}
	if m.selected() == nil {
		return "a frame is a label around a sub-pipeline, and compiles to nothing of its own"
	}
	return m.rowLabel() + " left no code of its own — the backend fused it into its neighbour"
}

// rowLabel names the row the cursor is on, for a view that is not showing it.
func (m *visualModel) rowLabel() string {
	if n := m.selectedNode(); n != nil {
		return n.Label()
	}
	return "(no row)"
}

func (m *visualModel) goFooter(lines int) string {
	if m.searchingGo {
		return "  " + styKey.Render("/") + styMatch.Render(m.goFind+" ") +
			styDim.Render("  enter accepts · esc clears")
	}
	pos := fmt.Sprintf("  %d–%d/%d · ", m.goTop+1, min(m.goTop+max(m.height-4, 1), lines), lines)
	if m.status != "" {
		return styDim.Render(pos) + styKey.Render(truncateVis(m.status, max(10, m.width-len([]rune(pos))-2)))
	}
	keys := []string{"j/k scroll", "ctrl+d/u page", "/ find", "g/G ends", "z back to the step", "esc back"}
	return styDim.Render(pos + strings.Join(keys, " · "))
}

// helpView is the key list, opened with ? — where the keys live now that the
// footer does not carry them. A stepper has more keys than a footer can hold
// without becoming the loudest thing on screen.
func (m *visualModel) helpView() string {
	body := m.helpBody()
	rows := max(1, m.height-2)
	top := min(m.helpTop, max(0, len(body)-1))

	var b strings.Builder
	head := styTitle.Render("  keys ")
	if len(body) > rows {
		head += styDim.Render(fmt.Sprintf("%d–%d of %d · j/k scrolls",
			top+1, min(top+rows, len(body)), len(body)))
	}
	fmt.Fprintf(&b, "%s\n", pad(head, m.width))
	for i := range rows {
		if top+i < len(body) {
			fmt.Fprintf(&b, "%s\n", pad(body[top+i], m.width))
			continue
		}
		b.WriteString(pad("", m.width) + "\n")
	}
	b.WriteString(pad(styDim.Render("  the same keys are in ")+
		styKey.Render("docs/cli.md")+styDim.Render(" · any other key returns"), m.width))
	return b.String()
}

// helpBody is the key list as lines, built whole so it can be scrolled. It used
// to be written straight to the screen and stopped at the terminal's height,
// which quietly hid whole sections on a short window.
func (m *visualModel) helpBody() []string {
	sections := []struct {
		name  string
		pairs [][2]string
	}{
		{"moving", [][2]string{
			{"j / k", "down and up (arrows too)"},
			{"ctrl+d / u", "half a page"},
			{"l / h", "open and close a row; enter steps into an open one"},
			{"g / G", "first and last row"},
			{"H", "jump to the hottest row — the most self time"},
			{"!", "jump to the next failing step, wrapping"},
			{": or #", "go to a step by its number, the one --json prints"},
		}},
		{"reading the pane", [][2]string{
			{"tab", "move the keys to the right-hand pane, to scroll it"},
			{"j / k, g / G", "scroll it, once tab is there"},
			{"enter", "in the profile or source pane, go to that row"},
			{"esc", "back to the tree, then out of a filter, then a pane, then quit"},
			{"< / >", "give the tree less or more of the width"},
		}},
		{"searching", [][2]string{
			{"/", "search: narrows the tree as you type, enter accepts, esc clears"},
			{"n / N", "next and previous match, with the tree left whole"},
			{"type:", "match the out type — /type:List<Int>"},
			{"prim: line:", "match the primitive, or the program line"},
			{"out: in:", "match a captured value — /out:47"},
			{"err:", "only rows that failed; err:parse to match the message"},
			{"#42", "the step with that number"},
			{">5ms  <1%", "rows above or below a self-time bound"},
		}},
		{"panes", [][2]string{
			{"x", "the row's Using: expression, one parenthesis at a time"},
			{"d", "what the stage changed — the value in against the value out"},
			{"t", "the timing profile — call sites ranked by self time"},
			{"s", "the program source, with each line's share of the run"},
			{"e", "the optimizer's rewrites"},
		}},
		{"doing something with it", [][2]string{
			{"r", "record the program again, keeping this view"},
			{"w", "write the recording beside the program as JSON"},
			{"y", "copy the selected value to the clipboard"},
			{"o", "open the program at this stage's line in $EDITOR"},
		}},
		{"screens", [][2]string{
			{"c", "the emitted Go, opened at the selected row's code"},
			{"?", "this list"},
			{"q", "quit"},
		}},
		{"reading a row", [][2]string{
			{"%", "the row's share of the run; self% excludes its frames"},
			{"result", "what a block's body produced — a Channel, Part or lap"},
			{"N iterations", "a folded run of laps; open it for all of them"},
			{"●", "changed since the last recording (after r or --watch)"},
		}},
	}

	var out []string
	for _, sec := range sections {
		out = append(out, "  "+styHeading.Render(sec.name))
		for _, p := range sec.pairs {
			out = append(out, "    "+styKey.Render(pad(p[0], 14))+
				styDim.Render(truncateVis(p[1], max(10, m.width-20))))
		}
		out = append(out, "")
	}
	return out
}

// title is the header line: the program, how much of it was recorded, and the
// total every percentage on screen is a share of.
func (m *visualModel) title() string {
	t := m.view.times()
	head := fmt.Sprintf("  %s ", m.view.path)
	tail := fmt.Sprintf("%s · %s total", m.view.rec.Summary(), interp.FormatDuration(t.Overall()))
	return styTitle.Render(head) + styDim.Render(tail)
}

// treeLines renders the pipeline tree pane, scrolled to keep the cursor visible.
func (m *visualModel) treeLines(w, h int) []string {
	start := 0
	if m.cursor >= h {
		start = m.cursor - h + 1
	}
	// The share bar is the first thing to go on a narrow pane: the number it
	// draws is already in the column beside it.
	barW := 0
	if w >= 60 {
		barW = 6
	}
	t := m.view.times()

	if len(m.rows) == 0 {
		if m.filter != "" {
			return []string{styDim.Render(pad("  no row matches "+m.filter, w))}
		}
		return []string{styDim.Render(pad("  nothing was recorded", w))}
	}

	out := make([]string, 0, h)
	for i := start; i < len(m.rows) && len(out) < h; i++ {
		out = append(out, m.treeLine(m.rows[i], i == m.cursor, w, barW, t))
	}
	return out
}

// treeLine renders one row. Every cell is padded and truncated as plain text
// and only then colored, so an escape sequence never counts as a column.
func (m *visualModel) treeLine(r visRow, selected bool, w, barW int, t *interp.Timing) string {
	open := m.isOpen(r.node)
	marker := " "
	if len(r.node.Children) > 0 {
		marker = "▸"
		if open {
			marker = "▾"
		}
	}
	cursor := " "
	if selected {
		cursor = "▶"
	}
	indent := strings.Repeat("  ", r.depth)

	// A frame has no value of its own, so its size column stays empty; what it
	// does have is the cost of everything underneath it, which is the whole
	// reason to show timings on frame rows too.
	label, size := indent+r.node.Frame, ""
	failed := false
	if !r.node.IsFrame() {
		label, size = indent+r.node.Label(), sizeOf(r.node.Step)
		failed = r.node.Step.Err != nil
		if failed {
			label += " ✗"
		}
	}
	// A block reports what its body produced, for the reason valueLines gives:
	// its own output is the value it passes through, not the one it computed.
	if b := r.node.Block; b != nil {
		size = recSize(b)
	}
	// A closed row says what it is hiding, so a `Repeat 500` is not a mystery
	// until it is opened — and so the row that is 98% of the run explains, on
	// its face, that the 98% is 500 iterations of something.
	if steps, frames := r.node.Counts(); !open && steps+frames > 0 {
		switch laps, folded := foldedChild(r.node); {
		case r.node.Folded:
			// The label already counts the laps. What it does not say is how
			// much work they came to.
			label += fmt.Sprintf(" (%s)", plural(steps, "step"))
		case folded:
			// The fold is an implementation detail of the display, so a loop
			// counts its laps rather than the one row standing in for them.
			label += fmt.Sprintf(" (%s, %s)", plural(laps, "iteration"), plural(steps, "step"))
		case frames > 0:
			label += fmt.Sprintf(" (%s, %s)", plural(frames, "frame"), plural(steps, "step"))
		default:
			label += fmt.Sprintf(" (%s)", plural(steps, "step"))
		}
	}

	// A row whose output differs from the recording this one replaced is marked,
	// so a re-record shows its own consequences instead of leaving the reader to
	// diff two screens from memory. See visualize_record.go.
	change := " "
	if m.changed[r.node] {
		change = "●"
	}
	// The step number is what --json prints and what `:` jumps to, so a reader
	// holding one from a pipeline can find the row it names. It goes in only
	// where there is room for it — on a narrow pane the label is worth more,
	// and the value pane carries the number regardless.
	// A frame has no number, but it keeps the *column*: without it a frame's
	// label starts six columns left of its own children's, and the indentation
	// that says what is inside what stops reading as indentation.
	num := ""
	if w >= stepNumberWidth {
		num = strings.Repeat(" ", 6)
		if !r.node.IsFrame() {
			num = fmt.Sprintf("%5d ", r.node.Step.Index)
		}
	}

	nt := t.Of(r.node)
	pct := fmt.Sprintf("%6s", pctText(nt.TotalPct, nt.Known))
	bar := ""
	if barW > 0 {
		bar = " " + shareBar(nt, barW)
	}
	rightW := 6 + 1 + 6 + len([]rune(bar))
	labelW := max(8, w-rightW-7-len([]rune(num)))
	label = pad(truncateVis(label, labelW), labelW)

	// Selecting a row highlights the whole line; otherwise each cell carries its
	// own meaning in color — frames are structure, failures are red, and the
	// share is colored by how big it is.
	if selected {
		return styCursor.Render(pad(fmt.Sprintf("%s%s %s %s%s %6s %s%s",
			change, cursor, marker, num, label, size, pct, bar), w))
	}
	labelStyle := styLabel
	switch {
	case failed:
		labelStyle = styErr
	case r.node.IsFrame():
		labelStyle = styFrame
	}
	if m.filter != "" && m.isMatch(r.node) {
		labelStyle = styMatch
	}
	hot := heat(nt.TotalPct, nt.Known)
	line := fmt.Sprintf("%s%s %s %s%s %s %s%s",
		styKey.Render(change), cursor, styMarker.Render(marker), styDim.Render(num),
		labelStyle.Render(label),
		styDim.Render(fmt.Sprintf("%6s", size)), hot.Render(pct), hot.Render(bar))
	return pad(line, w)
}

// foldedChild reports the laps of the fold a row holds, for a row whose whole
// content is one — a loop, whose children the recorder gathered.
func foldedChild(n *interp.TraceNode) (laps int, ok bool) {
	if len(n.Children) != 1 {
		return 0, false
	}
	return n.Children[0].Iterations()
}

// plural renders a count with its noun, so a row does not say "1 steps".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// highlightStyle picks out rows the filter actually matched, as opposed to the
// ancestors kept only to show the path to them.
// shareBar draws a row's share of the run: the light run is its total share,
// the solid head the part that is the row's own work rather than its frames'.
// That is what tells a `Repeat 500` row at 98% apart from a genuinely slow
// primitive at 98% — the loop's bar is almost all light.
func shareBar(nt interp.NodeTiming, w int) string {
	if w <= 0 {
		return ""
	}
	if !nt.Known {
		return strings.Repeat(" ", w)
	}
	total := barCells(nt.TotalPct, w)
	self := min(barCells(nt.SelfPct, w), total)
	return strings.Repeat("█", self) + strings.Repeat("░", total-self) + strings.Repeat(" ", w-total)
}

// barCells converts a percentage to a cell count, giving anything that ran at
// all one cell — a step at 0.4% should be visible, not blank.
func barCells(pct float64, w int) int {
	n := int(pct/100*float64(w) + 0.5)
	if n == 0 && pct > 0 {
		n = 1
	}
	return min(max(n, 0), w)
}

// detailLines renders the right-hand pane in full, whichever one is open. The
// caller scrolls it; a pane that clipped itself to the visible height could not
// be scrolled at all.
func (m *visualModel) detailLines(w int) []string {
	var out []string
	switch m.pane {
	case paneExplain:
		out = m.explainLines(w)
	case paneHot:
		out = m.hotLines(w)
	case paneSource:
		out = m.sourceLines(w)
	case paneExpr:
		out = m.exprLines(w)
	case paneDiff:
		out = m.diffLines(w)
	default:
		out = m.valueLines(w)
	}
	if len(out) > maxDetailLines {
		out = append(out[:maxDetailLines],
			styDim.Render(fmt.Sprintf("  … %d more lines than this pane will build",
				len(out)-maxDetailLines)))
	}
	return out
}

// exprLines renders the selected step's `Using:` expression piece by piece —
// every parenthesis its own row, with the value it came to.
//
// It is the one pane that answers a question about the *inside* of a stage. The
// value pane says a `Map Each` turned 200 numbers into 200 numbers; this says
// what the expression did to the first of them, which is where a wrong answer
// is actually made.
func (m *visualModel) exprLines(w int) []string {
	// A foreign stage has no expression, but it does have an inside, and this
	// is the pane that shows one. See visualize_foreign.go.
	if lang, _ := foreignSource(m.selected()); lang != "" {
		return m.foreignLines(w)
	}
	out := []string{
		styHeading.Render("expression"),
		styDim.Render("every parenthesis, and what it came to"),
		"",
	}
	b, err := breakdownOf(m.selected())
	if err != nil {
		for _, line := range wrapVis(err.Error(), w-4) {
			out = append(out, "  "+styDim.Render(line))
		}
		return out
	}
	out = append(out, "  "+styValue.Render(truncateVis(b.Header, w-2)))
	if b.Note != "" {
		// The note says which application this is, and on a failing step that
		// is load-bearing: the pane is showing the one that failed, not the
		// first, and a reader comparing it against element 1 would be lost.
		style := styDim
		if b.Failed {
			style = styErr
		}
		for _, line := range wrapVis(b.Note, w-4) {
			out = append(out, "  "+style.Render(line))
		}
	}
	out = append(out, "")

	// The value column is fixed and the text takes what is left: an expression
	// nests, so the text is what needs the room, and a value too big to sit in
	// a column is a list the value pane shows properly anyway.
	valW := min(max(w/3, 8), 24)
	for _, r := range b.Rows {
		textW := max(8, w-valW-3-2*r.depth)
		text := pad(truncateVis(strings.Repeat("  ", r.depth)+r.text, textW+2*r.depth), textW+2*r.depth)
		if r.err != "" {
			out = append(out, "  "+styErr.Render(text)+" "+styErr.Render(truncateVis(r.err, valW)))
			continue
		}
		out = append(out, "  "+styLabel.Render(text)+" "+styValue.Render(truncateVis(r.value, valW)))
	}
	return out
}

// valueLines describes the selected row: what it produced, and what it cost.
func (m *visualModel) valueLines(w int) []string {
	node := m.selectedNode()
	if node == nil {
		if m.filter != "" {
			return []string{styDim.Render("(no row matches — esc clears the filter)")}
		}
		return []string{styDim.Render("(nothing recorded)")}
	}
	nt := m.view.times().Of(node)
	if node.IsFrame() {
		return m.frameLines(node, nt, w)
	}
	s := node.Step
	// A block — a Channel, a Part — hands its input back to the pipeline, so
	// the type and size that describe it are its *body's*, not its own. The
	// passthrough is still shown below, as the value the next stage receives.
	block := node.Block
	out := []string{styHeading.Render(s.Node.Prim)}
	out = append(out, field("step", styDim.Render(fmt.Sprintf("#%d", s.Index))))
	if block != nil {
		out = append(out, field("type", styType.Render(block.Type)), field("size", recSize(block)))
	} else {
		out = append(out, field("type", styType.Render(typeOf(s))), field("size", sizeOf(s)))
	}
	if where := m.view.where(s.Node); where != "" {
		out = append(out, field("source", styDim.Render(where)))
	}
	out = append(out, timeLines(nt)...)
	out = append(out, "")
	// A source node consumes nothing, and has no input to show — its declared
	// input type is what says so.
	if s.Node.In != nil {
		out = append(out, field("in", truncateVis(s.InShort, w-7)), "")
	}
	if s.Err != nil {
		// A foreign block's failure carries its runtime's whole report — a
		// traceback, a compile error — and that report's own line structure is
		// what makes it readable, so it is kept rather than reflowed. This is
		// the pane with room for it; the row in the tree carries only a ✗.
		for i, line := range strings.Split(s.Err.Error(), "\n") {
			if i == 0 {
				line = "error: " + line
			}
			out = append(out, styErr.Render(truncateVis(line, w-2)))
		}
		out = append(out, "")
	}
	if block != nil {
		out = append(out, styHeading.Render("result"),
			styDim.Render("  what the body produced, after every step in it"))
		out = append(out, valueBody(recordedOf(block), w)...)
		out = append(out, "", styHeading.Render("passes on"),
			styDim.Render("  the value the next stage receives, unchanged"),
			"  "+styValue.Render(truncateVis(s.Short, w-2)))
		return out
	}
	out = append(out, styHeading.Render("out"))
	return append(out, valueBody(stepValue(s), w)...)
}

// field renders one `name  value` line of the detail pane.
func field(name, value string) string {
	return styDim.Render(pad(name, 6)) + " " + value
}

// timeLines renders a row's cost. The `self` line appears only where it says
// something the `time` line does not: on a row with nested frames, where the
// difference between the two is the whole answer to "where did the time go".
func timeLines(nt interp.NodeTiming) []string {
	hot := heat(nt.TotalPct, nt.Known)
	out := []string{field("time", fmt.Sprintf("%-9s %s %s",
		interp.FormatDuration(nt.Total), hot.Render(pctText(nt.TotalPct, nt.Known)),
		styDim.Render("of the run")))}
	if nt.Nested {
		out = append(out, field("self", fmt.Sprintf("%-9s %s %s",
			interp.FormatDuration(nt.Self), heat(nt.SelfPct, nt.Known).Render(pctText(nt.SelfPct, nt.Known)),
			styDim.Render("excluding frames"))))
	}
	return out
}

// frameLines describes a frame row: what it cost — one iteration of a loop is
// exactly the thing you want to compare against its siblings — and what its
// body came to, which is the only place that value appears.
func (m *visualModel) frameLines(node *interp.TraceNode, nt interp.NodeTiming, w int) []string {
	what := "(a frame — the rows underneath are its steps)"
	if laps, folded := node.Iterations(); folded {
		what = fmt.Sprintf("(%d laps of one loop, folded — l opens them)", laps)
	}
	out := []string{styFrame.Render(node.Frame), styDim.Render(what), ""}
	out = append(out, timeLines(nt)...)
	steps, frames := node.Counts()
	out = append(out, field("steps", fmt.Sprintf("%d", steps)))
	if frames > 0 {
		out = append(out, field("frames", fmt.Sprintf("%d", frames)))
	}
	// A frame does hold one value: what its body came to. On a fold that is the
	// last lap's value, which is what the loop as a whole produced.
	if b := node.Block; b != nil {
		out = append(out, "", styHeading.Render("result"))
		if b.Type != "" {
			out = append(out, field("type", styType.Render(b.Type)), field("size", recSize(b)))
		}
		out = append(out, valueBody(recordedOf(b), w)...)
	}
	return out
}

// hotLines renders the timing profile: every call site ranked by self time.
//
// The tree answers "what happened"; this answers "what should I fix". They are
// different questions, and on a recording with 400 loop iterations the tree
// cannot answer the second one — 400 rows of 2µs each are individually
// invisible and collectively the whole run.
// The pane is navigable: with the focus on it (tab), the cursor moves through
// the ranking and `enter` takes the tree to that call site. A ranked list you
// cannot act on makes the reader do the search by hand, which on a recording
// deep enough to need a profile is the search that does not work.
func (m *visualModel) hotLines(w int) []string {
	out := []string{
		styHeading.Render("where the time went"),
		styDim.Render("call sites by self time, worst first"),
		"",
	}
	hot := m.view.times().Hotspots(0)
	if len(hot) == 0 {
		return append(out, styDim.Render("  nothing took measurable time"))
	}
	if m.focus == focusDetail {
		out[1] = styDim.Render("call sites by self time · enter goes to one")
	}
	for i, s := range hot {
		name := s.Name
		if s.Calls > 1 {
			name = fmt.Sprintf("%s ×%d", name, s.Calls)
		}
		if s.Failed {
			name += " ✗"
		}
		style := styLabel
		if s.Failed {
			style = styErr
		}
		nameW := max(8, w-18)
		plain := fmt.Sprintf("%s %8s %6s", pad(truncateVis(name, nameW), nameW),
			interp.FormatDuration(s.Self), interp.FormatPercent(s.SelfPct))
		// The selected entry is lit only while the pane has the focus: a
		// highlight in a pane the keys do not drive is an invitation to press
		// something that will not work.
		if m.focus == focusDetail && i == m.paneEntry(hotHeaderLines) {
			out = append(out, styCursor.Render(pad(plain, w)))
			continue
		}
		out = append(out, fmt.Sprintf("%s %8s %s",
			style.Render(pad(truncateVis(name, nameW), nameW)),
			interp.FormatDuration(s.Self),
			heat(s.SelfPct, true).Render(fmt.Sprintf("%6s", interp.FormatPercent(s.SelfPct)))))
	}
	return out
}

// hotHeaderLines is how many lines of heading sit above the first ranked entry.
const hotHeaderLines = 3

// paneEntry is which entry of a navigable pane is selected, given how many
// heading lines sit above the first one.
//
// The scroll offset doubles as the cursor — the topmost entry in the visible
// window is the one `enter` acts on — which needs no second piece of state and
// cannot drift out of sync with what is on screen. Subtracting the heading is
// what makes "topmost entry" mean the first *entry* rather than the first line,
// which at the top of an unscrolled pane is a title.
func (m *visualModel) paneEntry(header int) int { return max(0, m.detailTop-header) }

// jumpToHotspot takes the tree to the call site the profile pane is on.
func (m *visualModel) jumpToHotspot() {
	hot := m.view.times().Hotspots(0)
	i := m.paneEntry(hotHeaderLines)
	if i >= len(hot) {
		m.status = "no call site there"
		return
	}
	target := hot[i]
	// A call site is an ir.Node and can have run many times; the row to land on
	// is the recorded step of it that cost the most, which is the one the
	// ranking is about.
	var best *interp.TraceNode
	var bestSelf time.Duration
	for _, n := range m.flat {
		if n.IsFrame() || n.Step.Node != target.Node {
			continue
		}
		if self := m.view.times().Of(n).Self; best == nil || self > bestSelf {
			best, bestSelf = n, self
		}
	}
	if best == nil {
		m.status = "that call site is not in the recorded tree"
		return
	}
	m.focus = focusTree
	m.reveal(best)
	m.status = fmt.Sprintf("%s · %s self over %s",
		target.Name, interp.FormatDuration(target.Self), plural(target.Calls, "call"))
}

// sourceLines renders the program with each line's share of the run in the
// gutter — the timing profile projected back onto the text the user wrote,
// which is where a fix has to happen.
// It is navigable too: with the focus on it, `enter` takes the tree to the
// first step recorded on the line at the top of the window — the profile read
// in the other direction, from the text a fix has to happen in back to the row
// that proves it.
func (m *visualModel) sourceLines(w int) []string {
	out := []string{
		styHeading.Render("source"),
		styDim.Render("self time by line"),
		"",
	}
	src := m.view.source()
	if len(src) == 0 {
		return append(out, styDim.Render("  (the program file could not be read)"))
	}
	if m.focus == focusDetail {
		out[1] = styDim.Render("self time by line · enter goes to a line's step")
	}
	byLine := m.view.lineShares()

	// The whole file is rendered and the caller scrolls it. While the tree has
	// the focus the pane follows the cursor's line, which is what makes it a
	// companion to the tree rather than a second thing to drive.
	focus := 0
	if s := m.selected(); s != nil {
		if _, foreign := s.Node.Foreign(); !foreign {
			focus = s.Node.Pos.Line
		}
	}
	if m.focus == focusTree && focus > 0 {
		body := max(1, m.height-3-len(out))
		m.detailTop = max(0, min(focus-body/2-1, len(src)-body))
	}

	for i := range src {
		line := i + 1
		// A line nothing ran on gets blank space rather than `0%`: the gutter is
		// for finding the hot lines, and a column of zeroes hides them.
		gutter := strings.Repeat(" ", 6)
		if share, ok := byLine[line]; ok {
			gutter = heat(share, true).Render(fmt.Sprintf("%6s", interp.FormatPercent(share)))
		}
		text := truncateVis(src[i], max(4, w-12))
		num := fmt.Sprintf("%4d", line)
		if line == focus || (m.focus == focusDetail && i == m.paneEntry(sourceHeaderLines)) {
			out = append(out, gutter+" "+styCursor.Render(pad(num+" "+text, max(4, w-7))))
			continue
		}
		out = append(out, gutter+" "+styDim.Render(num)+" "+styLabel.Render(text))
	}
	return out
}

// sourceHeaderLines is how many lines of heading sit above line 1 of the file.
const sourceHeaderLines = 3

// jumpToSourceLine takes the tree to a step recorded on the line the source
// pane is showing at the top of its window.
func (m *visualModel) jumpToSourceLine() {
	line := m.paneEntry(sourceHeaderLines) + 1 // the topmost file line in the window
	var target *interp.TraceNode
	for _, n := range m.flat {
		if n.IsFrame() {
			continue
		}
		if _, foreign := n.Step.Node.Foreign(); foreign {
			continue
		}
		if n.Step.Node.Pos.Line == line {
			target = n
			break
		}
	}
	if target == nil {
		m.status = fmt.Sprintf("nothing ran on line %d", line)
		return
	}
	m.focus = focusTree
	m.reveal(target)
	m.status = fmt.Sprintf("line %d · %s", line, target.Label())
}

// explainLines renders the optimizer's rewrites, toggled with `e`.
func (m *visualModel) explainLines(w int) []string {
	out := []string{styHeading.Render("optimizer rewrites"), ""}
	if len(m.view.rewrites) == 0 {
		return append(out, styDim.Render("  no optimizations applied"))
	}
	for _, r := range m.view.rewrites {
		for _, line := range wrapVis(r.Message, w-4) {
			out = append(out, "  "+styValue.Render(line))
		}
		out = append(out, "")
	}
	return out
}

// footer is one quiet line: where the cursor is, whatever the last key did, and
// the way to the keys. The legend it used to carry was the loudest thing on
// screen and still could not hold every key — `?` holds all of them.
func (m *visualModel) footer() string {
	if m.searching {
		return "  " + styKey.Render("/") + styMatch.Render(m.filter+" ") +
			styDim.Render(fmt.Sprintf("  %d rows · ", len(m.rows))) +
			styDim.Render(truncateVis(searchHint, max(10, m.width-len([]rune(m.filter))-24)))
	}
	if m.jumping {
		return "  " + styKey.Render("step #") + styMatch.Render(m.jumpBuf+" ") +
			styDim.Render("  enter goes there · esc cancels")
	}
	pos := fmt.Sprintf("  %d/%d", m.cursor+1, len(m.rows))
	if m.focus == focusDetail {
		pos += fmt.Sprintf(" · pane %d/%d", min(m.detailTop+1, m.detailLen), m.detailLen)
	}
	if m.filter != "" {
		pos += fmt.Sprintf(" · /%s", m.filter)
	}
	keys := styKey.Render("?") + styDim.Render(" keys · ") + styKey.Render("q") + styDim.Render(" quit")
	switch {
	case m.status != "":
		// A status message is what the reader just asked for, so it gets the
		// width; the keys are one keystroke away regardless.
		return styDim.Render(pos+" · ") + styKey.Render(truncateVis(m.status, m.width-len([]rune(pos))-6))
	case m.recording:
		return styDim.Render(pos+" · ") + styKey.Render("recording…")
	case m.view.runErr != nil:
		return styDim.Render(pos+" · ") + styErr.Render("run failed") + styDim.Render(" · ") + keys
	case m.watch != nil:
		return styDim.Render(pos+" · ") + styDim.Render(truncateVis(watchStatus(m.watch),
			max(10, m.width-len([]rune(pos))-16))) + styDim.Render(" · ") + keys
	}
	return styDim.Render(pos+" · ") + keys
}
