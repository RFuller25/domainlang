// Drawing the wheel.
//
// The geometry is fixed and the light moves. Eight arms reach from a hub to
// eight tips at the compass points, and everything that animates — the sweep
// rotating through the arms, the pulse travelling out along the turn in flight,
// the hub's heartbeat, the flash when an adaptation lands — is a change of
// colour on cells that never move. Rotating the characters themselves was the
// first idea and it looks like static: at 39 columns a diagonal has four
// positions, and cycling between them reads as flicker rather than spin.
//
// Everything is composed as plain runes on a grid first and coloured on the way
// out, which is this package's rule (visualize_style.go: **style last**). A
// wheel built out of pre-styled strings would have escape sequences occupying
// columns, and the arms would not meet the hub.
package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/codegen"
	"domain/mahoraga"
	"domain/optimizer"
)

// The canvas. Two columns to a row keeps the aspect roughly square in a
// terminal cell, which is why the horizontal arms are fourteen cells and the
// vertical ones six: on screen they come out the same length.
const (
	wheelW  = 39
	wheelH  = 15
	wheelCX = 19
	wheelCY = 7
)

// wheelPt is one cell of an arm.
type wheelPt struct {
	x, y int
	r    rune
}

// wheelArm is the cells of one handle's arm, hub-end first. The last cell is
// the tip, which carries the handle's mark rather than the arm's glyph.
func wheelArm(k int) []wheelPt {
	var out []wheelPt
	// diag lays a diagonal as two cells per row, which is what makes a 2:1
	// terminal cell read as a 45° line rather than a dotted one.
	diag := func(sx, sy int, r rune) {
		for i := 1; i <= 5; i++ {
			out = append(out,
				wheelPt{wheelCX + sx*(2*i-1), wheelCY + sy*i, r},
				wheelPt{wheelCX + sx*2*i, wheelCY + sy*i, r})
		}
	}
	switch k {
	case 0: // north
		for i := 1; i <= 6; i++ {
			out = append(out, wheelPt{wheelCX, wheelCY - i, '│'})
		}
	case 1: // north-east
		diag(1, -1, '╱')
	case 2: // east
		for i := 1; i <= 14; i++ {
			out = append(out, wheelPt{wheelCX + i, wheelCY, '─'})
		}
	case 3: // south-east
		diag(1, 1, '╲')
	case 4: // south
		for i := 1; i <= 6; i++ {
			out = append(out, wheelPt{wheelCX, wheelCY + i, '│'})
		}
	case 5: // south-west
		diag(-1, 1, '╱')
	case 6: // west
		for i := 1; i <= 14; i++ {
			out = append(out, wheelPt{wheelCX - i, wheelCY, '─'})
		}
	case 7: // north-west
		diag(-1, -1, '╲')
	}
	return out
}

// wheelDigitOffset is where a handle's number sits relative to its tip: outside
// the wheel, on the side the arm points.
var wheelDigitOffset = [8][2]int{
	{0, -1}, {2, 0}, {2, 0}, {2, 0}, {0, 1}, {-2, 0}, {-2, 0}, {-2, 0},
}

// ---------------------------------------------------------------------------
// Ink
// ---------------------------------------------------------------------------

// wheelInk names a colour role. The canvas stores roles rather than styles so
// that a whole frame can be recoloured — the adaptation flash does exactly
// that — without rebuilding the geometry.
type wheelInk int

const (
	inkNone wheelInk = iota
	inkBase
	inkMid
	inkBright
	inkPeak
	inkHub
	inkPend
	inkSpent
	inkAbsent
	inkFlash
	// inkEffect0 begins the crimson-to-gold ramp a lit handle is coloured with.
	// It is last so that inkEffect0+level stays a valid ink.
	inkEffect0
)

func wheelStyle(k wheelInk) lipgloss.Style {
	switch k {
	case inkBase:
		return styWheelBase
	case inkMid:
		return styWheelMid
	case inkBright:
		return styWheelBright
	case inkPeak:
		return styWheelPeak
	case inkHub:
		return styWheelHub
	case inkPend:
		return styWheelPend
	case inkSpent:
		return styWheelSpent
	case inkAbsent:
		return styWheelAbsent
	case inkFlash:
		return styWheelFlash
	}
	if k >= inkEffect0 {
		return wheelEffectRamp[min(int(k-inkEffect0), len(wheelEffectRamp)-1)]
	}
	return styWheelBase
}

// effectInk grades a win. The bands are wide at the bottom because that is
// where the interesting distinction is: 2% and 4% are different findings, 30%
// and 40% are both "large".
func effectInk(effect float64) wheelInk {
	switch {
	case effect < 0.02:
		return inkEffect0
	case effect < 0.05:
		return inkEffect0 + 1
	case effect < 0.10:
		return inkEffect0 + 2
	case effect < 0.20:
		return inkEffect0 + 3
	}
	return inkEffect0 + 4
}

// ---------------------------------------------------------------------------
// The canvas
// ---------------------------------------------------------------------------

type wheelCanvas struct {
	runes [wheelH][wheelW]rune
	ink   [wheelH][wheelW]wheelInk
}

func newWheelCanvas() *wheelCanvas {
	c := &wheelCanvas{}
	for y := range c.runes {
		for x := range c.runes[y] {
			c.runes[y][x] = ' '
		}
	}
	return c
}

func (c *wheelCanvas) set(x, y int, r rune, ink wheelInk) {
	if x < 0 || y < 0 || x >= wheelW || y >= wheelH {
		return
	}
	c.runes[y][x], c.ink[y][x] = r, ink
}

// lines renders the canvas, grouping runs of one ink into a single styled
// string so a frame costs a handful of escape sequences rather than 585.
func (c *wheelCanvas) lines() []string {
	out := make([]string, wheelH)
	for y := range wheelH {
		var b strings.Builder
		x := 0
		for x < wheelW {
			ink := c.ink[y][x]
			start := x
			for x < wheelW && c.ink[y][x] == ink {
				x++
			}
			text := string(c.runes[y][start:x])
			if ink == inkNone {
				b.WriteString(text)
				continue
			}
			b.WriteString(wheelStyle(ink).Render(text))
		}
		out[y] = b.String()
	}
	return out
}

// ---------------------------------------------------------------------------
// Painting one frame
// ---------------------------------------------------------------------------

// hubGlyphs is the heartbeat, slow enough to read as breathing rather than
// blinking.
var hubGlyphs = []rune{'◉', '◎', '◉', '●'}

// turningGlyphs is what a handle does while its turn is running.
var turningGlyphs = []rune{'◇', '◈', '◆', '◈'}

// wheelLines paints the wheel for the current frame.
func (m *wheelModel) wheelLines() []string {
	c := newWheelCanvas()

	for k := range 8 {
		h := &m.handles[k]
		arm := wheelArm(k)
		armInk := m.spokeInk(k)
		if h.state == handleAbsent {
			armInk = inkAbsent
		}
		// The turn in flight sends a pulse out along its arm — the one piece of
		// motion on the wheel that is about *this* turn rather than the search as
		// a whole, so a reader can see which handle is being worked without
		// reading a word.
		pulse := -1
		if h.state == handleTurning && !m.paused {
			pulse = (m.frame / 2) % len(arm)
		}
		for i, p := range arm[:len(arm)-1] {
			// An arm for a turn that does not exist is drawn dashed as well as
			// dim, so "there is no such turn yet" survives a screenshot, a
			// colourless terminal and NO_COLOR.
			if h.state == handleAbsent && i%2 == 1 {
				continue
			}
			ink := armInk
			switch {
			case pulse < 0:
			case i == pulse:
				ink = inkPeak
			case i == pulse-1 || i == pulse+1:
				ink = inkBright
			}
			c.set(p.x, p.y, p.r, ink)
		}

		tip := arm[len(arm)-1]
		mark, tipInk := m.handleMark(k)
		c.set(tip.x, tip.y, mark, tipInk)

		off := wheelDigitOffset[k]
		digitInk := inkPend
		switch h.state {
		case handleAbsent:
			digitInk = inkAbsent
		case handleTurning:
			digitInk = inkPeak
		case handleSpent, handleSkipped:
			digitInk = inkSpent
		case handleLit:
			digitInk = effectInk(h.best)
		}
		c.set(tip.x+off[0], tip.y+off[1], rune('1'+k), digitInk)
	}

	hub := hubGlyphs[(m.frame/6)%len(hubGlyphs)]
	if m.done {
		hub = '◉'
	}
	c.set(wheelCX, wheelCY, hub, inkHub)

	// The flash is the whole wheel, not the handle: an adaptation is the search
	// as a whole getting faster, and lighting one tip for it would bury the only
	// moment worth interrupting a reader for.
	if m.flash > 0 && !m.paused {
		over := inkFlash
		if m.flash <= 5 {
			over = inkPeak
		}
		for y := range wheelH {
			for x := range wheelW {
				if c.ink[y][x] != inkNone {
					c.ink[y][x] = over
				}
			}
		}
	}
	return c.lines()
}

// spokeInk is one arm's colour from the rotating sweep: bright just behind the
// sweep and fading ahead of it, which is what gives the rotation a direction.
func (m *wheelModel) spokeInk(k int) wheelInk {
	lag := ((m.sweep-k*wheelSubsteps)%wheelPositions + wheelPositions) % wheelPositions
	switch {
	case lag < 2:
		return inkPeak
	case lag < 5:
		return inkBright
	case lag < 9:
		return inkMid
	}
	return inkBase
}

// handleMark is the glyph at one arm's tip and the ink for it.
func (m *wheelModel) handleMark(k int) (rune, wheelInk) {
	h := &m.handles[k]
	switch h.state {
	case handleAbsent:
		return '◌', inkAbsent
	case handleTurning:
		if h.kept > 0 {
			return '◆', effectInk(h.best)
		}
		return turningGlyphs[(m.frame/3)%len(turningGlyphs)], inkPeak
	case handleLit:
		if h.glow > 0 {
			return '✦', inkFlash
		}
		return '◆', effectInk(h.best)
	case handleSpent:
		return '○', inkSpent
	case handleSkipped:
		return '⊘', inkSpent
	}
	return '·', inkPend
}

// compactWheel is the wheel for a terminal too short to hold it: the same eight
// handles in one row. A wheel clipped in half would be worse than no wheel, and
// the handles are the information — the arms are how it is read, not what it
// says.
func (m *wheelModel) compactWheel() string {
	var b strings.Builder
	for k := range 8 {
		mark, ink := m.handleMark(k)
		b.WriteString(wheelStyle(ink).Render(string(mark)))
		digit := wheelStyle(inkPend).Render(string(rune('1' + k)))
		if m.handles[k].state == handleLit {
			digit = wheelStyle(effectInk(m.handles[k].best)).Render(string(rune('1' + k)))
		}
		b.WriteString(digit)
		if k < 7 {
			b.WriteString(wheelStyle(m.spokeInk(k)).Render("──"))
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// The view
// ---------------------------------------------------------------------------

func (m *wheelModel) View() tea.View {
	if m.screen != screenWheel {
		return fullScreen(m.screenView())
	}
	lines := m.body()
	body := max(1, m.height-1)
	var b strings.Builder
	for i := range body {
		if i < len(lines) {
			b.WriteString(pad(lines[i], m.width))
		} else {
			b.WriteString(pad("", m.width))
		}
		b.WriteString("\n")
	}
	b.WriteString(pad(m.footer(), m.width))
	return fullScreen(b.String())
}

// body is everything above the footer, in priority order: a short terminal
// loses the log before it loses the wheel, and the wheel before the numbers.
func (m *wheelModel) body() []string {
	out := []string{m.header(), ""}

	// The wheel wants fifteen rows and the roster ten; below that the compact
	// row says the same thing in one.
	if m.height >= wheelH+11 {
		out = append(out, m.wheelAndRoster()...)
	} else {
		out = append(out, "   "+m.compactWheel(), "")
		for _, l := range m.rosterLines(max(20, m.width-6)) {
			out = append(out, "  "+l)
		}
	}
	out = append(out, "")
	out = append(out, m.progressLines()...)
	out = append(out, "")

	// Whatever is left goes to the ledger tail, which is the part that grows.
	spent := len(out) + 1
	if room := m.height - spent - 1; room >= 2 {
		out = append(out, m.tailLines(room)...)
	}
	return out
}

// wheelAndRoster puts the wheel beside the eight turns it stands for.
func (m *wheelModel) wheelAndRoster() []string {
	art := m.wheelLines()
	rosterW := m.width - wheelW - 5
	if rosterW < 24 {
		// Too narrow to sit beside it; the roster goes underneath.
		out := make([]string, 0, wheelH+10)
		for _, l := range art {
			out = append(out, "  "+l)
		}
		return append(out, "")
	}
	roster := m.rosterLines(rosterW)
	// The roster is ten lines against the wheel's fifteen, so it is centred
	// against the hub rather than the top: the eye reads the two as one object.
	offset := max(0, (wheelH-len(roster))/2)

	out := make([]string, 0, wheelH)
	for i := range wheelH {
		right := ""
		if j := i - offset; j >= 0 && j < len(roster) {
			right = roster[j]
		}
		out = append(out, "  "+art[i]+"   "+right)
	}
	return out
}

// rosterLines names the eight turns and what each one found.
func (m *wheelModel) rosterLines(w int) []string {
	out := []string{styHeading.Render("the eight turns")}
	rightW := 22
	nameW := max(10, w-rightW-5)
	for k := range 8 {
		h := &m.handles[k]
		mark, ink := m.handleMark(k)
		name := h.name
		if name == "" {
			name = "—"
		}
		right, rightStyle := m.rosterRight(k)
		line := wheelStyle(ink).Render(string(mark)) + " " +
			styDim.Render(fmt.Sprintf("%d", k+1)) + " " +
			m.rosterName(k).Render(pad(truncateVis(name, nameW), nameW)) + " " +
			rightStyle.Render(pad(truncateVis(right, rightW), rightW))
		out = append(out, line)
	}
	return append(out, "")
}

// rosterName styles a turn's name by what became of it, so the roster is
// readable without reading the marks.
func (m *wheelModel) rosterName(k int) lipgloss.Style {
	switch m.handles[k].state {
	case handleAbsent:
		return styWheelAbsent
	case handleTurning:
		return styWheelPeak
	case handleLit:
		return effectStyle(m.handles[k].best)
	case handleSkipped:
		return styWheelSpent
	}
	return styLabel
}

func effectStyle(effect float64) lipgloss.Style { return wheelStyle(effectInk(effect)) }

// rosterRight is the right-hand column of one roster row: what the turn did.
func (m *wheelModel) rosterRight(k int) (string, lipgloss.Style) {
	h := &m.handles[k]
	switch h.state {
	case handleAbsent:
		if k+1 > 8 || !mahoraga.TurnBuilt(k+1) {
			return "not built yet", styWheelAbsent
		}
		return "beyond --turns", styWheelAbsent
	case handleSkipped:
		return fmt.Sprintf("abandoned at %d", h.tried), styWheelSpent
	case handleTurning:
		if m.candTotal > 1 {
			return fmt.Sprintf("trying %d of %d", m.candIndex, m.candTotal), styWheelPeak
		}
		return "turning…", styWheelPeak
	case handleLit:
		return fmt.Sprintf("%d kept · +%.1f%%", h.kept, h.best*100), effectStyle(h.best)
	case handleSpent:
		if k == 0 && m.baseline > 0 {
			return shortDuration(m.baseline), styLabel
		}
		if h.tried == 0 {
			return "nothing to try", styDim
		}
		return fmt.Sprintf("%d tried, none kept", h.tried), styDim
	}
	// A turn still pending when the search is over was never reached — the
	// reader stopped it, or a turn before it did. "waiting" would be waiting for
	// something that is not coming.
	if m.done {
		return "never reached", styWheelPend
	}
	return "waiting", styWheelPend
}

// header is the one line that says what is being adapted and how far in.
func (m *wheelModel) header() string {
	left := styTitle.Render(" MAHORAGA ") + " " +
		styLabel.Render(shortPath(m.program)) + styDim.Render(" → ") +
		styLabel.Render(shortPath(m.input))

	var state string
	switch {
	case m.err != nil:
		state = styErr.Render("failed")
	case m.done:
		state = styWheelPeak.Render("the wheel has stopped")
	case m.finishing:
		state = styKey.Render("finishing…")
	case m.paused:
		state = styKey.Render("held")
	case m.active > 0:
		state = styDim.Render(fmt.Sprintf("turn %d of 8", m.active))
	default:
		state = styDim.Render("starting")
	}
	// The elapsed clock is the last thing to go: on a search with no time limit
	// it is the number a reader checks most, and it costs five columns.
	for _, right := range []string{
		state + styDim.Render(fmt.Sprintf(" · %s · %d tried · %d kept",
			clock(m.elapsed()), m.tried, m.kept)),
		state + styDim.Render(fmt.Sprintf(" · %s · %d tried", clock(m.elapsed()), m.tried)),
		state + styDim.Render(" · "+clock(m.elapsed())),
		styDim.Render(clock(m.elapsed())),
	} {
		gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right) - 2
		if gap >= 1 {
			return " " + left + strings.Repeat(" ", gap) + right
		}
	}
	return " " + truncateVis(left, max(1, m.width-2))
}

// progressLines are the numbers: what is in flight, what it is being compared
// against, and the shape of the search so far.
func (m *wheelModel) progressLines() []string {
	var out []string

	label := m.candidate
	switch {
	case m.err != nil:
		out = append(out, "  "+styErr.Render(truncateVis(m.err.Error(), m.width-4)))
	case m.done:
		out = append(out, "  "+m.verdictLine())
		// A candidate that measured faster and still could not be told from the
		// champion is a question the run's measurement budget could not answer,
		// which is a different report from "nothing worked".
		if m.recipe != nil && m.recipe.Inconclusive > 0 {
			out = append(out, "  "+styKey.Render(fmt.Sprintf(
				"%d looked faster and could not be distinguished — a quieter machine or more --runs",
				m.recipe.Inconclusive)))
		}
	case label != "":
		head := "  " + styKey.Render("▸ ") + styValue.Render(truncateVis(label, max(10, m.width-40)))
		if m.candTotal > 1 {
			head += styDim.Render(fmt.Sprintf("  [%d/%d]", m.candIndex, m.candTotal))
		}
		out = append(out, head)
		out = append(out, "    "+scannerBar(24, m.frame, m.paused)+" "+
			styDim.Render("building and measuring"))
	default:
		out = append(out, "  "+styDim.Render("…"))
		out = append(out, "")
	}

	out = append(out, "  "+m.numbersLine())
	if s := m.sparkLine(max(10, m.width-30)); s != "" {
		out = append(out, "  "+s)
	}
	return out
}

// numbersLine is the comparison every decision on the wheel turns on.
func (m *wheelModel) numbersLine() string {
	if m.baseline <= 0 {
		return styDim.Render("measuring the baseline — nothing is comparable until it is done")
	}
	speed := 1.0
	if m.bestRatio > 0 {
		speed = 1 / m.bestRatio
	}
	style := styDim
	if m.kept > 0 {
		style = styValue
	}
	return styDim.Render("baseline ") + styLabel.Render(shortDuration(m.baseline)) +
		styDim.Render("   best ") + style.Render(shortDuration(m.champion())) +
		styDim.Render("   ") + style.Render(fmt.Sprintf("%.3f×", speed)) +
		styDim.Render(fmt.Sprintf("   noise ±%.1f%%", m.noiseFloor*100))
}

// verdictLine is the headline once the search is over. It has to be able to say
// "nothing" without dressing it up — a tuner that always reports a win is not
// measuring.
func (m *wheelModel) verdictLine() string {
	r := m.recipe
	switch {
	case r == nil:
		return styErr.Render("the search did not finish")
	case r.Overturned():
		return styErr.Render("BASELINE UNBEATEN") + styDim.Render(
			" — the champion did not survive its re-measurement; the baseline was written")
	case !r.Improved():
		return styWheelSpent.Render("BASELINE UNBEATEN") + styDim.Render(
			" — everything findable on this program and this input was already found")
	}
	return effectStyle(1-1/r.Speedup).Render(fmt.Sprintf("ADAPTED — %.2f× faster", r.Speedup)) +
		styDim.Render(fmt.Sprintf(" over %d adaptation(s)", len(r.Kept())))
}

// sparkLine draws every candidate's speed against the baseline. The roster says
// which turns paid; this says whether the search is finding anything at all,
// which on a long turn is the difference between working and stuck.
func (m *wheelModel) sparkLine(w int) string {
	if len(m.spark) < 2 {
		return ""
	}
	data := m.spark
	if len(data) > w {
		data = data[len(data)-w:]
	}
	const ramp = " ▁▂▃▄▅▆▇█"
	var b strings.Builder
	b.WriteString(styDim.Render("candidates "))
	for _, v := range data {
		// Centred on 1.0 — the baseline — because the question a reader asks of
		// this row is "is anything above the line", not "how big are these".
		level := int((v-0.9)/0.4*8) + 1
		level = min(max(level, 1), 8)
		cell := string([]rune(ramp)[level])
		style := styWheelSpent
		if v > 1+m.noiseFloor {
			style = effectStyle(1 - 1/v)
		}
		b.WriteString(style.Render(cell))
	}
	b.WriteString(styDim.Render(" faster ↑"))
	return b.String()
}

// tailLines are the last few things the search decided, newest last — the
// running commentary the plain reporter prints as it goes.
func (m *wheelModel) tailLines(room int) []string {
	out := []string{styHeading.Render("  the last few candidates")}
	entries := m.entries
	if n := room - 1; len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	if len(entries) == 0 {
		return append(out, "  "+styDim.Render("nothing decided yet"))
	}
	for _, e := range entries {
		out = append(out, "  "+m.entryLine(e, m.width-4))
	}
	return out
}

// entryLine renders one finished candidate.
func (m *wheelModel) entryLine(e wheelEntry, w int) string {
	mark, style := "·", styDim
	switch {
	case e.kept:
		mark, style = "✓", effectStyle(e.effect)
	case e.failed:
		mark, style = "✗", styErr
	}
	right := e.reason
	if e.kept {
		right = fmt.Sprintf("%+.1f%% · %s · %s", e.effect*100, shortDuration(e.mean), e.tier)
	}
	labelW := max(12, w-len(right)-20)
	return styDim.Render(clock(e.timestamp)) + " " +
		styDim.Render(fmt.Sprintf("t%d", e.turn)) + " " +
		style.Render(mark) + " " +
		styLabel.Render(pad(truncateVis(e.label, labelW), labelW)) + " " +
		style.Render(truncateVis(right, max(6, w-labelW-12)))
}

// footer carries the keys, which is short enough here to hold all of them.
func (m *wheelModel) footer() string {
	if m.status != "" {
		return " " + styKey.Render(truncateVis(m.status, m.width-2))
	}
	if m.done {
		return " " + styDim.Render("the wheel has stopped · ") +
			styKey.Render("a") + styDim.Render(" ledger · ") +
			styKey.Render("r") + styDim.Render(" recipe · ") +
			styKey.Render("p") + styDim.Render(" passes · any other key closes")
	}
	keys := [][2]string{
		{"space", "hold"}, {"s", "skip turn"}, {"a", "ledger"},
		{"r", "recipe"}, {"p", "passes"}, {"?", "keys"}, {"q", "finish and keep"},
	}
	var b strings.Builder
	b.WriteString(" ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(styDim.Render(" · "))
		}
		b.WriteString(styKey.Render(k[0]) + styDim.Render(" "+k[1]))
	}
	return truncateVis(b.String(), m.width)
}

// scannerBar is the indeterminate progress of a build. There is nothing to
// measure — a `go build` reports no fraction — so the bar says "still working"
// and nothing it cannot back up.
func scannerBar(w, frame int, paused bool) string {
	if w < 4 {
		return ""
	}
	head := 0
	if !paused {
		// A triangle wave: the head runs to the end and back, so the bar is
		// always moving without ever implying it is about to finish.
		period := 2 * (w - 1)
		p := (frame / 2) % period
		head = p
		if p >= w {
			head = period - p
		}
	}
	cells := make([]string, w)
	for i := range cells {
		switch d := i - head; {
		case d == 0:
			cells[i] = styWheelPeak.Render("█")
		case d == -1 || d == 1:
			cells[i] = styWheelBright.Render("▓")
		case d == -2 || d == 2:
			cells[i] = styWheelMid.Render("▒")
		default:
			cells[i] = styWheelBase.Render("─")
		}
	}
	return styDim.Render("▐") + strings.Join(cells, "") + styDim.Render("▌")
}

// ---------------------------------------------------------------------------
// The full-screen readers
// ---------------------------------------------------------------------------

func (m *wheelModel) screenView() string {
	body := m.screenBody()
	rows := max(1, m.height-2)
	top := min(m.scroll, max(0, len(body)-1))

	var b strings.Builder
	head := styTitle.Render("  " + m.screenTitle() + " ")
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
	b.WriteString(pad(styDim.Render("  any other key returns to the wheel"), m.width))
	return b.String()
}

func (m *wheelModel) screenTitle() string {
	switch m.screen {
	case screenLedger:
		return "every candidate"
	case screenRecipe:
		return "the recipe as it stands"
	case screenPasses:
		return "optimizer passes"
	case screenWheelHelp:
		return "keys"
	}
	return ""
}

func (m *wheelModel) screenBody() []string {
	switch m.screen {
	case screenLedger:
		return m.ledgerBody()
	case screenRecipe:
		return m.recipeBody()
	case screenPasses:
		return m.passesBody()
	case screenWheelHelp:
		return m.wheelHelpBody()
	}
	return nil
}

// ledgerBody is everything tried, kept or not.
//
// The rejections are the point. A search that showed only its wins would hide
// how much was tried and found to be inside the noise, which is the single most
// useful thing to know when deciding whether to believe the rest.
func (m *wheelModel) ledgerBody() []string {
	if len(m.entries) == 0 {
		return []string{"", "  " + styDim.Render("nothing has been decided yet")}
	}
	out := []string{"", "  " + styDim.Render(fmt.Sprintf(
		"%d tried · %d kept · everything below was built and run", m.tried, m.kept))}
	turn := 0
	for _, e := range m.entries {
		if e.turn != turn {
			turn = e.turn
			name := ""
			if h := m.handle(turn); h != nil {
				name = h.name
			}
			out = append(out, "", "  "+styHeading.Render(fmt.Sprintf("turn %d — %s", turn, name)))
		}
		out = append(out, "  "+m.entryLine(e, m.width-4))
	}
	return out
}

// recipeBody is what a recipe written at this moment would say. It is live
// rather than final because the interesting question during a long turn is
// "what have I got so far", and the file does not exist until the end.
func (m *wheelModel) recipeBody() []string {
	row := func(k, v string) string {
		return "  " + styDim.Render(pad(k, 12)) + " " + styLabel.Render(v)
	}
	out := []string{"",
		row("program", m.program),
		row("input", m.input),
		row("tier", m.tier.String()),
		row("binary", m.outPath),
		row("recipe", m.recipePath),
		"",
		row("baseline", shortDuration(m.baseline)),
		row("best", shortDuration(m.champion())+
			"  (the baseline scaled by every accepted race's ratio)"),
		row("noise", fmt.Sprintf("±%.2f%% (standard error of the baseline mean)", m.noiseFloor*100)),
		"",
	}

	kept := 0
	for _, e := range m.entries {
		if e.kept {
			kept++
		}
	}
	if kept == 0 {
		out = append(out, "  "+styDim.Render(
			"no adaptation has been kept, so this recipe would rebuild exactly what `domain build` does"))
	} else {
		out = append(out, "  "+styHeading.Render("kept"))
		for _, e := range m.entries {
			if !e.kept {
				continue
			}
			out = append(out, fmt.Sprintf("    %s %s %s",
				styDim.Render(fmt.Sprintf("turn %d", e.turn)),
				styLabel.Render(pad(truncateVis(e.label, 40), 40)),
				effectStyle(e.effect).Render(fmt.Sprintf("%+.1f%%", e.effect*100))))
		}
	}

	out = append(out, "", "  "+styHeading.Render("build"))
	if len(m.flags) == 0 && m.rounds == 0 && m.tuning.Empty() {
		out = append(out, "    "+styDim.Render("the defaults — this would rebuild what `domain build` builds"))
	}
	for _, f := range m.flags {
		out = append(out, "    "+styValue.Render(truncateVis(f, m.width-6)))
	}
	if m.rounds > 0 {
		out = append(out, "    "+styValue.Render(fmt.Sprintf("optimizer rounds capped at %d", m.rounds)))
	}
	for _, line := range tuningLines(m.tuning) {
		out = append(out, "    "+styValue.Render(truncateVis(line, m.width-6)))
	}

	if m.recipe != nil && !m.recipe.Contract.Empty() {
		out = append(out, "", "  "+styHeading.Render("contract"))
		for _, line := range contractLines(m.recipe.Contract) {
			out = append(out, "    "+line)
		}
	}
	out = append(out, "", "  "+styDim.Render(
		"the expected output is recorded by path, never by content — see mahoraga.Oracle"))
	if !m.done {
		out = append(out, "  "+styDim.Render(
			"nothing is written until the champion is re-measured against the baseline"))
	}
	return out
}

// passesBody shows which optimizer passes the champion is built with. Turn 4
// switches them off one at a time, so "which ones are off" is the whole content
// of what that turn found.
func (m *wheelModel) passesBody() []string {
	all := optimizer.PassNames()
	on := map[string]bool{}
	if m.schedule == nil {
		for _, n := range all {
			on[n] = true
		}
	} else {
		for _, n := range m.schedule {
			on[n] = true
		}
	}
	off := 0
	for _, n := range all {
		if !on[n] {
			off++
		}
	}
	out := []string{"", "  " + styDim.Render(fmt.Sprintf(
		"%d passes · %d switched off for this program and this input", len(all), off))}
	if m.rounds > 0 {
		out = append(out, "  "+styDim.Render(fmt.Sprintf("cascade capped at %d rounds", m.rounds)))
	}
	out = append(out, "")
	for _, n := range all {
		if on[n] {
			out = append(out, "    "+styValue.Render("on  ")+styLabel.Render(n))
			continue
		}
		out = append(out, "    "+styErr.Render("off ")+styWheelSpent.Render(n))
	}
	if off > 0 {
		out = append(out, "", "  "+styDim.Render(
			"a pass switched off here is one that made *this* program slower — "+
				"the general optimizer cannot know that, because it is not allowed to see the input"))
	}
	return out
}

func (m *wheelModel) wheelHelpBody() []string {
	sections := []struct {
		name  string
		pairs [][2]string
	}{
		{"while the wheel turns", [][2]string{
			{"space", "hold the animation — the search keeps running"},
			{"s", "abandon the turn in flight and move to the next one"},
			{"q", "stop looking, but still re-measure and write both artifacts"},
			{"ctrl+c", "abort — nothing is written"},
		}},
		{"reading it", [][2]string{
			{"a", "every candidate tried, kept or not, with why"},
			{"r", "what a recipe written right now would say"},
			{"p", "which optimizer passes the champion is built with"},
			{"j / k, g / G", "scroll a screen; ctrl+d and ctrl+u page"},
		}},
		{"the wheel", [][2]string{
			{"◌", "a turn the catalogue has not reached"},
			{"·", "a turn that has not been reached yet"},
			{"◈", "the turn in flight"},
			{"○", "ran, and kept nothing — a real finding, not a failure"},
			{"◆", "adapted; crimson through gold by how much it won"},
			{"⊘", "abandoned with s"},
			{"the sweep", "turns faster when candidates are finishing faster"},
		}},
		{"what it is doing", [][2]string{
			{"screen", "a cheap measurement; most candidates lose and stop here"},
			{"confirm", "a full-length one, for a candidate that looks like winning"},
			{"noise", "the standard error of the baseline mean, not its deviation"},
			{"the end", "the champion is re-measured against the baseline, interleaved"},
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

// tuningLines renders what the code generator was told about the input, in the
// language of what it does rather than of the struct field that carries it.
// A reader of a recipe has to be able to see what was assumed on their behalf.
func tuningLines(t codegen.Tuning) []string {
	var out []string
	if t.ListCapacity > 0 {
		out = append(out, fmt.Sprintf("list capacity %d, measured — not the len/2+1 guess", t.ListCapacity))
	}
	if t.ASCIIText {
		out = append(out, "no UTF-8 decoding — every byte of the input was verified to be one rune")
	}
	if t.GCPercent != 0 {
		out = append(out, fmt.Sprintf("collector set to %d for the whole run", t.GCPercent))
	}
	if t.MemoryLimitBytes > 0 {
		out = append(out, fmt.Sprintf("memory limit %s — the backstop under a disabled collector",
			formatBytes(uint64(t.MemoryLimitBytes))))
	}
	return out
}

// contractLines renders what a pinned recipe requires of an input. The clauses
// nobody can re-check are marked, because they are the ones that bind the
// binary to one file rather than to a shape.
func contractLines(c mahoraga.Contract) []string {
	var out []string
	if c.ASCII {
		out = append(out, styValue.Render("every byte of the input decodes as one rune"))
	}
	if c.MinSegments > 0 {
		out = append(out, styValue.Render(fmt.Sprintf(
			"the input has at least %d segments", c.MinSegments)))
	}
	for _, u := range c.Unverifiable {
		out = append(out, styErr.Render("✗ ")+styLabel.Render(u)+
			styDim.Render(" — not re-checkable without running the program"))
	}
	return out
}
