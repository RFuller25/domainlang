package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Splitting a rendered collection
// ---------------------------------------------------------------------------

func TestSplitRendered(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
		ok   bool
	}{
		{"flat list", "[1, 2, 3]", []string{"1", "2", "3"}, true},
		{"one element", "[7]", []string{"7"}, true},
		{"nested lists", "[[1, 2], [3, 4]]", []string{"[1, 2]", "[3, 4]"}, true},
		{"records", "[{a: 1, b: 2}, {a: 3, b: 4}]", []string{"{a: 1, b: 2}", "{a: 3, b: 4}"}, true},
		{"map", `{"a": 1, "b": 2}`, []string{`"a": 1`, `"b": 2`}, true},
		// A rendered string can hold the delimiters, which is the case a naive
		// split on ", " gets wrong.
		{"commas in strings", `["a, b", "c"]`, []string{`"a, b"`, `"c"`}, true},
		{"brackets in strings", `["[1", "2]"]`, []string{`"[1"`, `"2]"`}, true},
		{"escaped quote", `["say \"hi\", ok", "x"]`, []string{`"say \"hi\", ok"`, `"x"`}, true},
		{"not a collection", "42", nil, false},
		{"empty", "[]", nil, false},
		{"unbalanced", "[1, 2", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := splitRendered(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, c.ok, got)
			}
			if !ok {
				return
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("element %d = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rendering a value by its shape
// ---------------------------------------------------------------------------

// A list long enough to be worth it gets one element per line with its index,
// because the two hundredth element used to be off the right edge and therefore
// unreachable.
func TestValueBodyIndexesALongList(t *testing.T) {
	v := recordedValue{
		typ: "List<Int>", full: "[10, 20, 30, 40, 50]", fullOK: true, size: 5, sizeOK: true,
	}
	got := plainLines(valueBody(v, 40))
	for i, want := range []string{"0 10", "1 20", "4 50"} {
		if !strings.Contains(got, want) {
			t.Errorf("element %d: want a line %q in:\n%s", i, want, got)
		}
	}
}

// A short list is left as it was: one line is more readable than four, and the
// index adds nothing when you can see the whole thing.
func TestValueBodyLeavesAShortListAlone(t *testing.T) {
	v := recordedValue{typ: "List<Int>", full: "[1, 2]", fullOK: true}
	got := plainLines(valueBody(v, 40))
	if strings.Count(strings.TrimSpace(got), "\n") != 0 {
		t.Errorf("a two-element list should stay on one line:\n%s", got)
	}
}

// A grid gets row numbers, and a picture-shaped one gets a column ruler —
// a coordinate being the whole reason to open a grid in a debugger.
func TestValueBodyRendersAGrid(t *testing.T) {
	v := recordedValue{typ: "Grid<Text>", full: "..#\n.#.\n#..", fullOK: true}
	got := plainLines(valueBody(v, 40))
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want a ruler and three rows, got:\n%s", got)
	}
	if !strings.Contains(lines[0], "012") {
		t.Errorf("first line should be a column ruler, got %q", lines[0])
	}
	for i, want := range []string{"0 ..#", "1 .#.", "2 #.."} {
		if !strings.Contains(lines[i+1], want) {
			t.Errorf("row %d = %q, want %q", i, lines[i+1], want)
		}
	}
}

// A grid of spaced-out numbers is a table, not a picture, and a column ruler
// would line up with nothing.
func TestValueBodyGridOfNumbersHasNoRuler(t *testing.T) {
	v := recordedValue{typ: "Grid<Int>", full: "1 22 3\n4 5 6", fullOK: true}
	got := plainLines(valueBody(v, 40))
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want two rows and no ruler, got:\n%s", got)
	}
}

// Text keeps its lines and shows its trailing whitespace, which is the
// difference that most often explains why the next stage could not parse it.
func TestValueBodyShowsTextWhitespace(t *testing.T) {
	v := recordedValue{typ: "Text", full: "one  \ntwo", fullOK: true}
	got := plainLines(valueBody(v, 40))
	if !strings.Contains(got, "one··") {
		t.Errorf("trailing spaces should be visible:\n%s", got)
	}
	if !strings.Contains(got, "no trailing newline") {
		t.Errorf("a missing final newline should be reported:\n%s", got)
	}
}

// The two reasons a rendering is partial are different answers, and the pane
// says which. Reporting a budget that was spent as "truncated" sent readers
// looking for a longer value that was never built.
func TestValueBodyDistinguishesSpentFromTruncated(t *testing.T) {
	spent := plainLines(valueBody(recordedValue{typ: "List<Int>", short: "[1, …]", spent: true}, 60))
	if !strings.Contains(spent, "budget was spent") {
		t.Errorf("a value the budget could not pay for should say so:\n%s", spent)
	}
	cut := plainLines(valueBody(recordedValue{typ: "Text", full: "abc", fullOK: false}, 60))
	if !strings.Contains(cut, "only the first part") {
		t.Errorf("a value cut at the cap should say so:\n%s", cut)
	}
	if strings.Contains(cut, "budget") {
		t.Errorf("a cut value should not blame the budget:\n%s", cut)
	}
}

// ---------------------------------------------------------------------------
// The diff pane
// ---------------------------------------------------------------------------

func TestDiffOpsFindsAnInsertion(t *testing.T) {
	// Compared by index, an insertion at the front reads as every element
	// having changed. That is the one answer a reader must not be given.
	ops := diffOps([]string{"a", "b", "c"}, []string{"x", "a", "b", "c"})
	var adds, dels, sames int
	for _, op := range ops {
		switch op.kind {
		case opAdd:
			adds++
		case opDel:
			dels++
		default:
			sames++
		}
	}
	if adds != 1 || dels != 0 || sames != 3 {
		t.Errorf("got %d added, %d removed, %d unchanged; want 1/0/3", adds, dels, sames)
	}
}

func TestDiffOpsIdentical(t *testing.T) {
	ops := diffOps([]string{"a", "b"}, []string{"a", "b"})
	for _, op := range ops {
		if op.kind != opSame {
			t.Errorf("an identical pair produced a %v", op.kind)
		}
	}
}

// The pane collapses runs of unchanged items: a diff that prints the hundred
// and eighty that stayed the same has buried its own answer.
func TestRenderDiffCollapsesUnchangedRuns(t *testing.T) {
	before := []string{"1", "2", "3", "4", "5"}
	after := []string{"1", "2", "9", "4", "5"}
	got := plainLines(renderDiff(before, after, 60))
	if !strings.Contains(got, "- 3") || !strings.Contains(got, "+ 9") {
		t.Errorf("the changed element should be shown both ways:\n%s", got)
	}
	if !strings.Contains(got, "2 items unchanged") {
		t.Errorf("runs of unchanged items should collapse:\n%s", got)
	}
	if strings.Contains(got, "  1\n") {
		t.Errorf("unchanged items should not be listed:\n%s", got)
	}
}

func TestRenderDiffOnNoChange(t *testing.T) {
	got := plainLines(renderDiff([]string{"a", "b"}, []string{"a", "b"}, 60))
	if !strings.Contains(got, "nothing changed") {
		t.Errorf("an unchanged pair should say so:\n%s", got)
	}
}

// The pane is reachable, describes the right stage, and does not claim a source
// stage changed something.
func TestDiffPane(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30}, pressKey("d"))
	if m.pane != paneDiff {
		t.Fatal("d should open the diff pane")
	}
	// Row 0 is the source stage, which consumes nothing.
	got := plainLines(m.detailLines(50))
	if !strings.Contains(got, "no input") {
		t.Errorf("a source stage has nothing to differ from:\n%s", got)
	}
	// The next stage does have an input, and the pane compares it.
	m = send(m, pressKey("j"))
	got = plainLines(m.detailLines(50))
	if !strings.Contains(got, "what changed") {
		t.Errorf("the diff pane should describe the stage:\n%s", got)
	}
	// d again closes it, the way every other pane key does.
	m = send(m, pressKey("d"))
	if m.pane != paneValue {
		t.Error("d should toggle back to the value pane")
	}
}

// A frame changes nothing itself, and the pane says so rather than comparing
// two things that were never adjacent.
func TestDiffPaneOnAFrame(t *testing.T) {
	m := visModel(t)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	for i := range m.rows {
		if m.rows[i].node.IsFrame() {
			m.cursor = i
			break
		}
	}
	if !m.selectedNode().IsFrame() {
		// The tree opens collapsed; open the loop to reach a frame.
		m = send(m, pressKey("j"), pressKey("j"), pressKey("j"), pressKey("l"), pressKey("j"))
	}
	if !m.selectedNode().IsFrame() {
		t.Skip("no frame row reachable in this recording")
	}
	m = send(m, pressKey("d"))
	got := plainLines(m.detailLines(50))
	if !strings.Contains(got, "changes nothing itself") {
		t.Errorf("a frame should say it changes nothing:\n%s", got)
	}
}
