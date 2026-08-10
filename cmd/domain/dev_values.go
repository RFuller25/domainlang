// What each stage actually produced.
//
// The type at the end of a line says the shape; the value says whether the
// shape is the one you meant. That is the difference between an editor that
// checks a program and the REPL, whose whole appeal is `=> [1000, 2000] :
// List<Int>` — the value first, the type after it.
//
// Nothing here runs anything. Every run records (dev_run.go), and a recording
// already holds each step's rendered output keyed by the node that produced it,
// which carries the source line it came from. So this is a walk over a tree
// that exists, and the answers are as fresh as the last run rather than as
// fresh as the last keystroke — which is the honest thing for a value to be.
package main

import (
	"fmt"
	"slices"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"domain/interp"
	"domain/ir"
	"domain/token"
)

// devStage is what one line of the program did on the last run.
type devStage struct {
	Line int // 1-based
	// Short is the value's compact rendering — `[1000, 2000, …]` — which is
	// what fits on a bar above the status line.
	Short string
	// Type is the value's type, so the bar reads like the REPL's own echo.
	Type string
	Size int
	// SizeOK distinguishes "no elements" from "size is not a meaningful
	// question here", which is what a scalar's size is.
	SizeOK bool
	// SharePct is this line's share of the run, for the timing gutter.
	SharePct float64
	Failed   bool
}

// devStages walks a recording and collects what each source line produced.
//
// Two traps, both about which line a node belongs to, and both the ones the
// language server's inlay hints already document.
//
// A Shikigami call **inlines its whole body**, and those nodes carry positions
// from the definition — which for `Shikigami: Ints` is the embedded prelude, so
// they are not coordinates in this file at all. The resolver tags the last node
// of an inlined group with the call site (`callPos`), and that is the line the
// call reports. Every other node of such a group is marked foreign and is
// skipped: without that, a four-line program shows values against lines 10 and
// 11, which is what this walk did before the test caught it.
//
// The last step at a line otherwise wins, which is the same rule the type hints
// use: a line that ran many times inside a loop reports its final value.
func devStages(view *traceView) map[int]devStage {
	if view == nil || view.rec == nil {
		return nil
	}
	out := map[int]devStage{}

	var walk func(nodes []*interp.TraceNode)
	walk = func(nodes []*interp.TraceNode) {
		for _, n := range nodes {
			if s := n.Step; s != nil && s.Node != nil {
				line, ok := devLineOf(s.Node)
				if !ok {
					walk(n.Children)
					continue
				}
				st := devStage{
					Line:   line,
					Short:  s.Short,
					Size:   s.Size,
					SizeOK: s.SizeOK,
					Failed: s.Err != nil,
				}
				if s.Node.Out != nil {
					st.Type = s.Node.Out.String()
				}
				// A failed step is the interesting one and must not be
				// displaced by a later success on the same line.
				if prev, seen := out[line]; !seen || !prev.Failed {
					out[line] = st
				}
			}
			walk(n.Children)
		}
	}
	walk(view.rec.Roots())

	// The timing profile is computed by the same code the stepper's heat pane
	// uses, so a line's share means the same thing in both.
	for line, pct := range view.lineShares() {
		st := out[line]
		st.Line, st.SharePct = line, pct
		out[line] = st
	}
	return out
}

// devLineOf is the line in *this* program a node belongs to.
//
// A node inlined from a Shikigami reports its call site; one whose position
// belongs to another source entirely — the prelude, an imported library —
// reports nothing, because pointing at that line number here would point at
// somebody else's program.
func devLineOf(n *ir.Node) (int, bool) {
	if pos, ok := n.Meta["callPos"].(token.Position); ok && pos.Line > 0 {
		return pos.Line, true
	}
	if _, foreign := n.Foreign(); foreign {
		return 0, false
	}
	if n.Pos.Line <= 0 {
		return 0, false
	}
	return n.Pos.Line, true
}

// stageFor is what the line under the cursor produced, if anything did.
func (m devModel) stageFor(row int) (devStage, bool) {
	st, ok := m.stages[row+1]
	if !ok || st.Short == "" {
		return devStage{}, false
	}
	return st, true
}

// valueBar is the row above the status line showing what the cursor's line
// produced. It is only drawn when there is something to say, so a program that
// has not been run costs no space at all.
func (m devModel) valueBar() string {
	st, ok := m.stageFor(m.buf.row)
	if !ok {
		return ""
	}
	label := styHeading.Render(" => ")
	body := st.Short
	if st.Failed {
		label = styErr.Render(" !! ")
	}

	tail := ""
	if st.Type != "" {
		tail += styType.Render(" : " + st.Type)
	}
	if st.SizeOK {
		tail += styDim.Render(fmt.Sprintf("  %d", st.Size))
	}
	if st.SharePct >= 1 {
		tail += styDim.Render(fmt.Sprintf("  %.0f%% of the run", st.SharePct))
	}

	// The value is what gets cut when the terminal is narrow: the type and the
	// share are short and fixed, and losing them to a long list would be the
	// wrong trade.
	room := m.width - ansi.StringWidth(label) - ansi.StringWidth(tail)
	if room < 8 {
		return truncateVis(label+body, m.width)
	}
	return label + truncateVis(body, room) + tail
}

// heatFor tints a line number by its share of the last run, so the expensive
// stage is the one that catches the eye. It reuses the stepper's own ramp, so a
// hot line looks the same in both.
func (m devModel) heatFor(row int) (lipgloss.Style, bool) {
	st, ok := m.stages[row+1]
	if !ok || st.SharePct < devHeatFloor {
		return lipgloss.Style{}, false
	}
	return heat(st.SharePct, true), true
}

// devHeatFloor is the share below which a line is not worth tinting. Under a
// few percent the colour is noise, and a program where every line is warm has
// told you nothing.
const devHeatFloor = 5

// ---------------------------------------------------------------------------
// walking the stages
// ---------------------------------------------------------------------------

// stageStep moves the cursor to the next or previous line that produced a
// value, so a recording can be walked from the buffer without opening the
// stepper. It reports whether it moved.
//
// This is the stepper's core gesture — see what each stage did, in order —
// against the program you are editing rather than against a tree beside it.
func (m *devModel) stageStep(delta int) bool {
	lines := m.stageLines()
	if len(lines) == 0 {
		return false
	}
	cur := m.buf.row + 1
	switch {
	case delta > 0:
		for _, l := range lines {
			if l > cur {
				m.buf.gotoLine(l)
				return true
			}
		}
		m.buf.gotoLine(lines[0]) // wrap: a pipeline is a loop to read round
	default:
		for i := len(lines) - 1; i >= 0; i-- {
			if lines[i] < cur {
				m.buf.gotoLine(lines[i])
				return true
			}
		}
		m.buf.gotoLine(lines[len(lines)-1])
	}
	return true
}

// stageLines is every line that produced a value, in program order.
func (m devModel) stageLines() []int {
	var out []int
	for line, st := range m.stages {
		if st.Short != "" {
			out = append(out, line)
		}
	}
	slices.Sort(out)
	return out
}

// stageSummary is what the status line says about a recording as a whole.
func (m devModel) stageSummary() string {
	if m.trace == nil {
		return ""
	}
	n := len(m.stageLines())
	return styDim.Render(fmt.Sprintf("%d stage(s) recorded", n))
}
