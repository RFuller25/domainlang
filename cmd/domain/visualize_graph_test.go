package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// plain strips styling from one line, so a test asserts on what a reader sees.
func plain(s string) string { return ansi.Strip(s) }

// The drawing is parsed back out of the rendering, so the parser is the part
// that can be wrong quietly: everything downstream trusts it.
func TestParseGraphRendering(t *testing.T) {
	g, ok := parseGraphRendering("{a: [(b, 1), (c, 7)], b: [(d, 2)], c: [], d: []}")
	if !ok {
		t.Fatal("a well-formed graph rendering did not parse")
	}
	if len(g.nodes) != 4 || g.nodes[0] != "a" || g.nodes[3] != "d" {
		t.Errorf("nodes = %v, want [a b c d] in insertion order", g.nodes)
	}
	if g.arcCount() != 3 {
		t.Errorf("arcCount = %d, want 3", g.arcCount())
	}
	if got := g.arcs[0]; len(got) != 2 || got[1].to != "c" || got[1].w != "7" {
		t.Errorf("a's arcs = %v, want b(1) and c(7)", got)
	}
	if !g.weighted() {
		t.Error("a graph with a weight of 7 is weighted")
	}

	// A tuple node carries commas and parens of its own, which is exactly what
	// a naive split on ", " would get wrong.
	pts, ok := parseGraphRendering("{(0, 0): [((1, 1), 4)], (1, 1): []}")
	if !ok {
		t.Fatal("a tuple-node graph did not parse")
	}
	if len(pts.nodes) != 2 || pts.nodes[0] != "(0, 0)" {
		t.Errorf("tuple nodes = %v, want [(0, 0) (1, 1)]", pts.nodes)
	}
	if a := pts.arcs[0]; len(a) != 1 || a[0].to != "(1, 1)" || a[0].w != "4" {
		t.Errorf("tuple arc = %v, want (1, 1) weighing 4", a)
	}

	// A Text node's own punctuation must not be read as structure.
	quoted, ok := parseGraphRendering(`{"a, b": [("c: d", 1)], "c: d": []}`)
	if !ok {
		t.Fatal("a graph over quoted text nodes did not parse")
	}
	if len(quoted.nodes) != 2 || quoted.nodes[0] != `"a, b"` {
		t.Errorf("quoted nodes = %v", quoted.nodes)
	}

	if g, ok := parseGraphRendering("{}"); !ok || len(g.nodes) != 0 {
		t.Error("the empty graph should parse as a graph with no nodes")
	}
}

// Anything that is not the shape we know is left alone. A capture cut in half
// by the recorder's budget is the common case, and a display that guesses
// wrong should look ordinary rather than broken.
func TestParseGraphRenderingRefusesWhatItCannotRead(t *testing.T) {
	for _, body := range []string{
		"{a: [(b, 1), (c, 7)], b: [(d, 2), (e", // truncated mid-value
		"[1, 2, 3]",                            // a list
		"{a: 1, b: 2}",                         // a map, not an adjacency
		"{a: [(b, 1)]",                         // no closing brace
		"{a: [(b, 1)], b: [(zzz, 1)], c: []}",  // an arc to a node that is not a key
		"not a value at all",
	} {
		if _, ok := parseGraphRendering(body); ok {
			t.Errorf("%q parsed as a graph, and should not have", body)
		}
	}
}

// The drawing's contract: every node is drawn exactly once as itself, and every
// arc that could not be a tree branch is marked rather than dropped.
func TestGraphDrawingShowsEveryNodeOnce(t *testing.T) {
	body := "{a: [(b, 1), (c, 1)], b: [(d, 1)], c: [(d, 1)], d: [], q: []}"
	lines := graphBody(body, 70)
	text := plainLines(lines)

	for _, node := range []string{"a", "b", "c", "d", "q"} {
		if !strings.Contains(text, node) {
			t.Errorf("node %q is missing from the drawing:\n%s", node, text)
		}
	}
	// d is reached twice: drawn under b, and marked under c.
	if n := markedArcs(text); n != 1 {
		t.Errorf("%d marked arcs, want exactly 1 (d is reached twice):\n%s", n, text)
	}
	if !strings.Contains(text, "5 nodes · 4 arcs") {
		t.Errorf("the summary should count nodes and arcs:\n%s", text)
	}
	// The isolated node is a piece of its own rather than being lost.
	if !strings.Contains(text, "\n  q") {
		t.Errorf("the isolated node q should be drawn as its own piece:\n%s", text)
	}
}

// A cycle terminates. This is the property that makes the drawing safe at all:
// a walk that followed a back edge would not stop.
func TestGraphDrawingTerminatesOnACycle(t *testing.T) {
	text := plainLines(graphBody("{x: [(y, 1)], y: [(z, 1)], z: [(x, 1)]}", 70))
	if !strings.Contains(text, "has a cycle") {
		t.Errorf("the summary should say the graph has a cycle:\n%s", text)
	}
	// Three nodes drawn, and the arc that closes the loop marked.
	for _, n := range []string{"x", "y", "z"} {
		if !strings.Contains(text, n) {
			t.Errorf("node %q missing from a cyclic graph's drawing:\n%s", n, text)
		}
	}
	if n := markedArcs(text); n != 1 {
		t.Errorf("%d marked arcs, want 1 — the arc that closes the loop:\n%s", n, text)
	}
	// A graph with no root at all still draws: the walk falls back to
	// insertion order once the roots are exhausted.
	if !strings.Contains(text, "0 roots") {
		t.Errorf("a pure cycle has no roots, and the summary should say so:\n%s", text)
	}
}

// Weights ride on the arc where there are any, and are left off entirely where
// every arc weighs 1 — a column of [1] is noise, not information.
func TestGraphDrawingShowsWeightsOnlyWhenThereAreAny(t *testing.T) {
	weighted := plainLines(graphBody("{a: [(b, 3), (c, 12)], b: [], c: []}", 70))
	if !strings.Contains(weighted, "[ 3]") || !strings.Contains(weighted, "[12]") {
		t.Errorf("weights should ride on the arcs, right-aligned:\n%s", weighted)
	}
	if !strings.Contains(weighted, "weighted") {
		t.Errorf("the summary should say the graph is weighted:\n%s", weighted)
	}

	unweighted := plainLines(graphBody("{a: [(b, 1), (c, 1)], b: [], c: []}", 70))
	if strings.Contains(unweighted, "[1]") {
		t.Errorf("an unweighted graph should draw without weights:\n%s", unweighted)
	}
	if strings.Contains(unweighted, "weighted") {
		t.Errorf("an unweighted graph should not be called weighted:\n%s", unweighted)
	}
}

// Past a size where a drawing is mostly back-edge markers, the listing is both
// shorter and easier to read, and the pane says which one it chose.
func TestGraphFallsBackToAListing(t *testing.T) {
	// A dense graph: 40 nodes, each pointing at every other.
	var entries []string
	for i := range 40 {
		var arcs []string
		for j := range 40 {
			if i != j {
				arcs = append(arcs, "(n"+itoa(j)+", 1)")
			}
		}
		entries = append(entries, "n"+itoa(i)+": ["+strings.Join(arcs, ", ")+"]")
	}
	text := plainLines(graphBody("{"+strings.Join(entries, ", ")+"}", 70))
	if !strings.Contains(text, "listed by node instead") {
		t.Errorf("a dense graph should fall back to the listing:\n%s", firstLines(text, 4))
	}
	if strings.Contains(text, "↩") {
		t.Error("the listing should not carry the drawing's markers")
	}
	// And it is still a per-node view rather than one unreadable line.
	if !strings.Contains(text, "n0  →") {
		t.Errorf("the listing should show a node and its arcs:\n%s", firstLines(text, 6))
	}
}

// The value pane routes a Graph to the drawing. Before this it fell to the
// default branch and arrived as one line, cut at the pane's width.
func TestValuePaneDrawsAGraph(t *testing.T) {
	v := recordedValue{
		full:   "{a: [(b, 1), (c, 7)], b: [(d, 2), (e, 3)], c: [(e, 1)], d: [], e: []}",
		fullOK: true,
		typ:    "Graph<Text>",
	}
	lines := valueBody(v, 60)
	if len(lines) < 5 {
		t.Fatalf("a five-node graph rendered in %d lines:\n%s", len(lines), plainLines(lines))
	}
	text := plainLines(lines)
	if strings.Contains(text, "…") {
		t.Errorf("nothing should be cut at 60 columns:\n%s", text)
	}
	if !strings.Contains(text, "├") && !strings.Contains(text, "└") {
		t.Errorf("the graph should be drawn with branches:\n%s", text)
	}
}

// ---------------------------------------------------------------------------
// The full-screen value
// ---------------------------------------------------------------------------

// `f` opens the selected value over the whole terminal, and esc comes back.
func TestVisualValueScreenOpensAndCloses(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 20}, pressKey("f"))
	if m.screen != screenValue {
		t.Fatalf("f should open the value screen, got screen %d", m.screen)
	}
	view := plain(m.View().Content)
	if !strings.Contains(view, "value") {
		t.Errorf("the value screen should name itself:\n%s", firstLines(view, 3))
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.screen != screenTree {
		t.Error("esc should return to the tree")
	}
	// f closes it too, the way c closes the Go screen: a view you are in is
	// somewhere to come back from.
	m = send(m, pressKey("f"), pressKey("f"))
	if m.screen != screenTree {
		t.Error("f should close the screen it opened")
	}
}

// The screen scrolls, and stops where the last line comes into view.
func TestVisualValueScreenScrolls(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 8}, pressKey("f"))
	body := m.valueScreenBody()
	if len(body) <= m.height-3 {
		t.Skip("this value fits on screen; nothing to scroll")
	}
	m = send(m, pressKey("j"), pressKey("j"))
	if m.valueTop != 2 {
		t.Errorf("valueTop = %d after two j, want 2", m.valueTop)
	}
	m = send(m, pressKey("g"))
	if m.valueTop != 0 {
		t.Errorf("g should go to the top, got %d", m.valueTop)
	}
	m = send(m, pressKey("G"))
	last := max(0, len(body)-max(1, m.height-3))
	if m.valueTop != last {
		t.Errorf("G should stop at %d, got %d", last, m.valueTop)
	}
	// And no further: the screen never scrolls off into blank.
	m = send(m, pressKey("j"), pressKey("j"), pressKey("j"))
	if m.valueTop != last {
		t.Errorf("the scroll ran past the end: %d, want %d", m.valueTop, last)
	}
}

// The whole point: the screen shows a value at a width the half-pane cannot.
func TestVisualValueScreenIsWiderThanThePane(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 20})
	paneW := m.detailWidth()
	m = send(m, pressKey("f"))
	if got := max(m.width-2, 20); got <= paneW {
		t.Errorf("the value screen is %d columns and the pane %d — it should be wider", got, paneW)
	}
}

// `z` folds long lines instead of cutting them, which is the other half of
// "see all of it": a line wider than the terminal has no width left to give.
func TestVisualValueScreenWraps(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 40, Height: 20}, pressKey("f"))
	cut := m.valueScreenBody()
	m = send(m, pressKey("z"))
	if !m.valueWrap {
		t.Fatal("z should turn wrapping on")
	}
	wrapped := m.valueScreenBody()
	if len(wrapped) < len(cut) {
		t.Errorf("wrapping produced fewer lines (%d) than cutting (%d)", len(wrapped), len(cut))
	}
	// Toggling back is the same view again.
	m = send(m, pressKey("z"))
	if m.valueWrap {
		t.Error("z should turn wrapping off again")
	}
}

// A row that holds no value says so rather than showing an empty screen.
func TestVisualValueScreenOnARowWithoutAValue(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 20})
	m.cursor = 0
	m.rows = nil // no rows at all: the emptiest case there is
	m = send(m, pressKey("f"))
	view := plain(m.View().Content)
	if !strings.Contains(view, "no value") {
		t.Errorf("an empty selection should say so:\n%s", firstLines(view, 4))
	}
}

// markedArcs counts the arcs the drawing marked, which is lines *ending* in the
// marker — the legend explaining it carries one too.
func markedArcs(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), "↩") {
			n++
		}
	}
	return n
}

// firstLines is the first n lines of a rendering, for a readable failure.
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	return strings.Join(lines[:min(n, len(lines))], "\n")
}

// itoa keeps the dense-graph builder readable.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
