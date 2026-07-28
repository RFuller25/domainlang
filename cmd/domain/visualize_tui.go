// The bubbletea stepper for `domain expansion: visualize`. The recording it
// walks is built in interp (record.go, timing.go) and is pure Go — that is
// where the trace coverage lives; this file is layout and key handling, tested
// the way repl_tty.go is, by driving the model with injected messages.
//
// The screen is two panes. The left is always the recorded tree; the right
// switches between the selected row's value, the optimizer's rewrites, the
// timing profile, and the program source with a per-line share of the run. All
// four answer a different question about the same recording, and only one of
// them fits on screen at a time.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	for i := 0; i < len(m.flat); i++ {
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
	treeW := m.width * 55 / 100
	if treeW < 30 {
		treeW = 30
	}
	detailW := m.width - treeW - 3
	if detailW < 20 {
		detailW = 20
	}
	bodyH := m.height - 3
	if bodyH < 3 {
		bodyH = 3
	}

	left := m.treeLines(treeW, bodyH)
	right := m.detailLines(detailW, bodyH)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", pad(m.title(), m.width))
	divider := styRule.Render("│")
	for i := 0; i < bodyH; i++ {
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
	// In bubbletea v2 the alternate screen is a property of the view, not a
	// program option: the stepper takes the whole screen and restores it on exit.
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
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
	// A closed row says what it is hiding, so a `Repeat 500` is not a mystery
	// until it is opened — and so the row that is 98% of the run explains, on
	// its face, that the 98% is 500 iterations of something.
	if steps, frames := r.node.Counts(); !open && steps+frames > 0 {
		if frames > 0 {
			label += fmt.Sprintf(" (%d frames, %d steps)", frames, steps)
		} else {
			label += fmt.Sprintf(" (%d steps)", steps)
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
		return m.frameLines(node, nt)
	}
	s := node.Step
	out := []string{
		styHeading.Render(s.Node.Prim),
		field("type", styType.Render(typeOf(s))),
		field("size", sizeOf(s)),
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
	out = append(out, styHeading.Render("out"))
	if s.Err != nil {
		out = append(out, "", styErr.Render("error: "+s.Err.Error()))
	}
	body := s.Full
	if body == "" {
		body = s.Short
	}
	for _, line := range strings.Split(body, "\n") {
		if len(out) >= h {
			break
		}
		out = append(out, "  "+styValue.Render(truncateVis(line, w-2)))
	}
	if !s.FullOK {
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

// frameLines describes a frame row. A frame holds no value, but it does hold a
// cost — one iteration of a loop is exactly the thing you want to compare
// against its siblings.
func (m *visualModel) frameLines(node *interp.TraceNode, nt interp.NodeTiming) []string {
	out := []string{
		styFrame.Render(node.Frame),
		styDim.Render("(a frame — the rows underneath are its steps)"),
		"",
	}
	out = append(out, timeLines(nt)...)
	steps, frames := node.Counts()
	out = append(out, field("steps", fmt.Sprintf("%d", steps)))
	if frames > 0 {
		out = append(out, field("frames", fmt.Sprintf("%d", frames)))
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

func (m *visualModel) footer() string {
	if m.searching {
		return "  " + styKey.Render("/") + styMatch.Render(m.filter+" ") +
			styDim.Render(fmt.Sprintf("  %d rows · enter accepts · esc clears", len(m.rows)))
	}
	pos := fmt.Sprintf("  %d/%d", m.cursor+1, len(m.rows))
	if m.filter != "" {
		pos += fmt.Sprintf(" · /%s", m.filter)
	}
	switch {
	case m.status != "":
		return styDim.Render(pos+" · ") + styKey.Render(truncateVis(m.status, m.width-len([]rune(pos))-6))
	case m.view.runErr != nil:
		return styDim.Render(pos+" · ") + styErr.Render("run failed") +
			styDim.Render(" · ") + m.keyHelp()
	}
	return styDim.Render(pos+" · ") + m.keyHelp()
}

// keyHelp is the footer's key legend, with the keys themselves picked out.
func (m *visualModel) keyHelp() string {
	pairs := [][2]string{
		{"j/k", "move"}, {"l/h", "open/close"}, {"/", "search"},
		{"H", "hottest"}, {"!", "failure"},
		{"t", "profile"}, {"s", "source"}, {"e", "explain"}, {"q", "quit"},
	}
	var parts []string
	for _, p := range pairs {
		parts = append(parts, styKey.Render(p[0])+" "+styDim.Render(p[1]))
	}
	return strings.Join(parts, styDim.Render(" · "))
}
