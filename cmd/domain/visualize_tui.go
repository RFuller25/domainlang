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
	"io"
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"domain/codegen"
	"domain/interp"
)

// runVisualizeTUI drives the stepper on a real terminal.
func runVisualizeTUI(view *traceView, stdin io.Reader, stdout, stderr io.Writer) int {
	m := newVisualModel(view)
	opts := []tea.ProgramOption{tea.WithOutput(stdout)}
	if f, ok := stdin.(*os.File); ok {
		opts = append(opts, tea.WithInput(f))
	}
	prog := tea.NewProgram(m, opts...)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	return 0
}

// detailPane is what the right-hand pane is currently showing.
type detailPane int

const (
	paneValue   detailPane = iota // the selected step's input and output
	paneExplain                   // the optimizer's rewrites
	paneHot                       // the timing profile, worst first
	paneSource                    // the program, with each line's share of the run
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
	flat     []*interp.TraceNode // every node in tree order, for the jump keys
	cursor   int
	width    int
	height   int
	pane     detailPane
	screen   screen

	// goTop is the first line of the emitted program on screen. The Go view
	// scrolls freely — it opens at the selected row's code and goes wherever
	// the reader takes it from there.
	goTop int

	// helpFrom is the screen `?` was pressed on, so the key list returns the
	// reader to what they were reading rather than to the tree.
	helpFrom screen

	// Search narrows the tree to matching rows and the paths that reach them.
	filter    string
	searching bool

	// status is a one-shot message in the footer — what a jump key did, or why
	// it did nothing. It clears on the next keystroke, so it never goes stale.
	status string
}

func newVisualModel(view *traceView) *visualModel {
	m := &visualModel{
		view:     view,
		expanded: map[*interp.TraceNode]bool{},
		parents:  map[*interp.TraceNode]*interp.TraceNode{},
		width:    100,
		height:   30,
	}
	// The parent links and tree order never change — the recording is finished —
	// so they are built once here rather than on every rebuild.
	var index func(nodes []*interp.TraceNode, parent *interp.TraceNode)
	index = func(nodes []*interp.TraceNode, parent *interp.TraceNode) {
		for _, n := range nodes {
			m.parents[n] = parent
			m.flat = append(m.flat, n)
			index(n.Children, n)
		}
	}
	index(view.rec.Roots(), nil)

	// Frames start collapsed: a loop with 400 iterations should not bury the
	// stages around it. Everything else has no children to hide.
	m.rebuild()
	return m
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

// matching is the set of rows the filter admits: every row whose label matches,
// every ancestor that leads to one (so the path stays readable), and everything
// underneath a match (so a matched frame can still be stepped into). It returns
// nil when no filter is active, which means "keep everything".
func (m *visualModel) matching() map[*interp.TraceNode]bool {
	if m.filter == "" {
		return nil
	}
	needle := strings.ToLower(m.filter)
	keep := map[*interp.TraceNode]bool{}

	var walk func(n *interp.TraceNode, underMatch bool) bool
	walk = func(n *interp.TraceNode, underMatch bool) bool {
		hit := strings.Contains(strings.ToLower(n.Label()), needle)
		found := hit || underMatch
		for _, c := range n.Children {
			if walk(c, found) {
				found = true
			}
		}
		if found {
			keep[n] = true
		}
		return found
	}
	for _, n := range m.view.rec.Roots() {
		walk(n, false)
	}
	return keep
}

func (m *visualModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		useTheme(isLightColor(msg.Color))
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		m.status = ""
		if m.screen != screenTree {
			return m.updateScreen(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			// Escape backs out of whatever is narrowing the view before it
			// leaves: a filtered tree, then a pane, then the program.
			switch {
			case m.filter != "":
				m.filter = ""
				m.rebuild()
			case m.pane != paneValue:
				m.pane = paneValue
			default:
				return m, tea.Quit
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "right", "l", "enter":
			m.expand()
		case "left", "h":
			m.collapse()
		case "g":
			m.cursor = 0
		case "G":
			m.cursor = max(0, len(m.rows)-1)
		case "e":
			m.togglePane(paneExplain)
		case "t":
			m.togglePane(paneHot)
		case "s":
			m.togglePane(paneSource)
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
		}
	}
	return m, nil
}

// updateScreen handles keys while a full-screen view is up. Both close the same
// way — the key that opened them, esc, or q — because a view you are *in* is
// somewhere to come back from, not a program to quit. ctrl+c still quits.
func (m *visualModel) updateScreen(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.screen == screenHelp {
		// Any key leaves the key list: it is a reference, not a mode, and a
		// reader who presses something is done reading.
		m.screen = m.helpFrom
		return m, nil
	}
	page := max(1, (m.height-4)/2)
	last := max(0, len(m.goSrc())-1)
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
}

// updateSearch handles keys while the filter is being typed. The tree updates
// on every keystroke, so the search is the result rather than a prelude to it.
func (m *visualModel) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.searching, m.filter = false, ""
		m.rebuild()
	case "enter":
		// Accept: the filter stays, the typing stops, the tree is navigable.
		m.searching = false
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.rebuild()
		}
	default:
		if t := msg.Text; t != "" && !strings.ContainsFunc(t, unicode.IsControl) {
			m.filter += t
			m.rebuild()
		}
	}
	return m, nil
}

// togglePane opens a pane, or closes it back to the value view when it is
// already open — one key in, the same key out.
func (m *visualModel) togglePane(p detailPane) {
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
	treeW := max(m.width*55/100, 30)
	detailW := max(m.width-treeW-3, 20)
	bodyH := max(m.height-3, 3)

	left := m.treeLines(treeW, bodyH)
	right := m.detailLines(detailW, bodyH)

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
		fmt.Fprintf(&b, "%s\n", pad(" "+styDim.Render(fmt.Sprintf("%4d ", line))+styLabel.Render(text), m.width))
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
	pos := fmt.Sprintf("  %d–%d/%d · ", m.goTop+1, min(m.goTop+max(m.height-4, 1), lines), lines)
	keys := []string{"j/k scroll", "ctrl+d/u page", "g/G ends", "z back to the step", "esc back"}
	return styDim.Render(pos + strings.Join(keys, " · "))
}

// helpView is the key list, opened with ? — where the keys live now that the
// footer does not carry them. A stepper has more keys than a footer can hold
// without becoming the loudest thing on screen.
func (m *visualModel) helpView() string {
	sections := []struct {
		name  string
		pairs [][2]string
	}{
		{"moving", [][2]string{
			{"j / k", "down and up (arrows too)"},
			{"l / h", "open and close a row; enter steps into an open one"},
			{"g / G", "first and last row"},
			{"H", "jump to the hottest row — the most self time"},
			{"!", "jump to the next failing step, wrapping"},
			{"/", "search: narrows the tree as you type, enter accepts, esc clears"},
		}},
		{"panes", [][2]string{
			{"t", "the timing profile — call sites ranked by self time"},
			{"s", "the program source, with each line's share of the run"},
			{"e", "the optimizer's rewrites"},
			{"esc", "back to the value pane, then out of a filter, then quit"},
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
		}},
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", pad(styTitle.Render("  keys ")+styDim.Render("any key returns"), m.width))
	lines := 1
	for _, sec := range sections {
		if lines >= m.height-1 {
			break
		}
		fmt.Fprintf(&b, "%s\n", pad("  "+styHeading.Render(sec.name), m.width))
		lines++
		for _, p := range sec.pairs {
			if lines >= m.height-1 {
				break
			}
			fmt.Fprintf(&b, "%s\n", pad("    "+styKey.Render(pad(p[0], 14))+
				styDim.Render(truncateVis(p[1], max(10, m.width-20))), m.width))
			lines++
		}
	}
	for ; lines < m.height-1; lines++ {
		b.WriteString(pad("", m.width) + "\n")
	}
	b.WriteString(pad(styDim.Render("  the same keys are in ")+
		styKey.Render("docs/cli.md")+styDim.Render(" · any key returns"), m.width))
	return b.String()
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

	nt := t.Of(r.node)
	pct := fmt.Sprintf("%6s", pctText(nt.TotalPct, nt.Known))
	bar := ""
	if barW > 0 {
		bar = " " + shareBar(nt, barW)
	}
	rightW := 6 + 1 + 6 + len([]rune(bar))
	labelW := max(8, w-rightW-5)
	label = pad(truncateVis(label, labelW), labelW)

	// Selecting a row highlights the whole line; otherwise each cell carries its
	// own meaning in color — frames are structure, failures are red, and the
	// share is colored by how big it is.
	if selected {
		return styCursor.Render(pad(fmt.Sprintf("%s %s %s %6s %s%s",
			cursor, marker, label, size, pct, bar), w))
	}
	labelStyle := styLabel
	switch {
	case failed:
		labelStyle = styErr
	case r.node.IsFrame():
		labelStyle = styFrame
	}
	if m.filter != "" {
		labelStyle = highlightStyle(r.node, m.filter, labelStyle)
	}
	hot := heat(nt.TotalPct, nt.Known)
	line := fmt.Sprintf("%s %s %s %s %s%s",
		cursor, styMarker.Render(marker), labelStyle.Render(label),
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
func highlightStyle(n *interp.TraceNode, filter string, fallback lipgloss.Style) lipgloss.Style {
	if strings.Contains(strings.ToLower(n.Label()), strings.ToLower(filter)) {
		return styMatch
	}
	return fallback
}

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

// detailLines renders the right-hand pane, whichever one is open.
func (m *visualModel) detailLines(w, h int) []string {
	switch m.pane {
	case paneExplain:
		return m.explainLines(w, h)
	case paneHot:
		return m.hotLines(w, h)
	case paneSource:
		return m.sourceLines(w, h)
	}
	return m.valueLines(w, h)
}

// valueLines describes the selected row: what it produced, and what it cost.
func (m *visualModel) valueLines(w, h int) []string {
	node := m.selectedNode()
	if node == nil {
		if m.filter != "" {
			return []string{styDim.Render("(no row matches — esc clears the filter)")}
		}
		return []string{styDim.Render("(nothing recorded)")}
	}
	nt := m.view.times().Of(node)
	if node.IsFrame() {
		return m.frameLines(node, nt, w, h)
	}
	s := node.Step
	// A block — a Channel, a Part — hands its input back to the pipeline, so
	// the type and size that describe it are its *body's*, not its own. The
	// passthrough is still shown below, as the value the next stage receives.
	block := node.Block
	out := []string{styHeading.Render(s.Node.Prim)}
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
		out = append(out, styErr.Render("error: "+s.Err.Error()), "")
	}
	if block != nil {
		out = append(out, styHeading.Render("result"),
			styDim.Render("  what the body produced, after every step in it"))
		out = append(out, valueBody(block.Short, block.Full, block.FullOK, w, h-len(out))...)
		out = append(out, "", styHeading.Render("passes on"),
			styDim.Render("  the value the next stage receives, unchanged"),
			"  "+styValue.Render(truncateVis(s.Short, w-2)))
		return out
	}
	out = append(out, styHeading.Render("out"))
	return append(out, valueBody(s.Short, s.Full, s.FullOK, w, h-len(out))...)
}

// valueBody renders a captured value: the full rendering where one was kept,
// the short one otherwise, clipped to the space left in the pane.
func valueBody(short, full string, fullOK bool, w, h int) []string {
	body := full
	if body == "" {
		body = short
	}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if len(out) >= h {
			break
		}
		out = append(out, "  "+styValue.Render(truncateVis(line, w-2)))
	}
	if !fullOK {
		out = append(out, styDim.Render("  … (value truncated for display)"))
	}
	return out
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
func (m *visualModel) frameLines(node *interp.TraceNode, nt interp.NodeTiming, w, h int) []string {
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
		out = append(out, valueBody(b.Short, b.Full, b.FullOK, w, h-len(out))...)
	}
	return out
}

// hotLines renders the timing profile: every call site ranked by self time.
//
// The tree answers "what happened"; this answers "what should I fix". They are
// different questions, and on a recording with 400 loop iterations the tree
// cannot answer the second one — 400 rows of 2µs each are individually
// invisible and collectively the whole run.
func (m *visualModel) hotLines(w, h int) []string {
	out := []string{
		styHeading.Render("where the time went"),
		styDim.Render("call sites by self time, worst first"),
		"",
	}
	hot := m.view.times().Hotspots(max(1, h-4))
	if len(hot) == 0 {
		return append(out, styDim.Render("  nothing took measurable time"))
	}
	for _, s := range hot {
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
		line := fmt.Sprintf("%s %8s %s",
			style.Render(pad(truncateVis(name, nameW), nameW)),
			interp.FormatDuration(s.Self),
			heat(s.SelfPct, true).Render(fmt.Sprintf("%6s", interp.FormatPercent(s.SelfPct))))
		out = append(out, line)
	}
	return out
}

// sourceLines renders the program with each line's share of the run in the
// gutter — the timing profile projected back onto the text the user wrote,
// which is where a fix has to happen.
func (m *visualModel) sourceLines(w, h int) []string {
	out := []string{
		styHeading.Render("source"),
		styDim.Render("self time by line"),
		"",
	}
	src := m.view.source()
	if len(src) == 0 {
		return append(out, styDim.Render("  (the program file could not be read)"))
	}
	byLine := m.view.lineShares()

	// Center the window on the selected step's line, so the pane tracks the
	// tree rather than sitting at the top of the file.
	focus := 0
	if s := m.selected(); s != nil {
		if _, foreign := s.Node.Foreign(); !foreign {
			focus = s.Node.Pos.Line
		}
	}
	body := max(1, h-len(out))
	start := 0
	if focus > 0 && focus > body/2 {
		start = min(focus-body/2-1, max(0, len(src)-body))
	}

	for i := start; i < len(src) && len(out) < h; i++ {
		line := i + 1
		// A line nothing ran on gets blank space rather than `0%`: the gutter is
		// for finding the hot lines, and a column of zeroes hides them.
		gutter := strings.Repeat(" ", 6)
		if share, ok := byLine[line]; ok {
			gutter = heat(share, true).Render(fmt.Sprintf("%6s", interp.FormatPercent(share)))
		}
		text := truncateVis(src[i], max(4, w-12))
		num := fmt.Sprintf("%4d", line)
		if line == focus {
			out = append(out, gutter+" "+styCursor.Render(pad(num+" "+text, max(4, w-7))))
			continue
		}
		out = append(out, gutter+" "+styDim.Render(num)+" "+styLabel.Render(text))
	}
	return out
}

// explainLines renders the optimizer's rewrites, toggled with `e`.
func (m *visualModel) explainLines(w, h int) []string {
	out := []string{styHeading.Render("optimizer rewrites"), ""}
	if len(m.view.rewrites) == 0 {
		return append(out, styDim.Render("  no optimizations applied"))
	}
	for _, r := range m.view.rewrites {
		if len(out) >= h {
			break
		}
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
			styDim.Render(fmt.Sprintf("  %d rows · enter accepts · esc clears", len(m.rows)))
	}
	pos := fmt.Sprintf("  %d/%d", m.cursor+1, len(m.rows))
	if m.filter != "" {
		pos += fmt.Sprintf(" · /%s", m.filter)
	}
	keys := styKey.Render("?") + styDim.Render(" keys · ") + styKey.Render("q") + styDim.Render(" quit")
	switch {
	case m.status != "":
		// A status message is what the reader just asked for, so it gets the
		// width; the keys are one keystroke away regardless.
		return styDim.Render(pos+" · ") + styKey.Render(truncateVis(m.status, m.width-len([]rune(pos))-6))
	case m.view.runErr != nil:
		return styDim.Render(pos+" · ") + styErr.Render("run failed") + styDim.Render(" · ") + keys
	}
	return styDim.Render(pos+" · ") + keys
}
