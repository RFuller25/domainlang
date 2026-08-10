// What the stage changed.
//
// The command's own description is "step through a run and watch the data
// change shape", and until now the value pane showed the shape and left the
// *change* to the reader's memory: the output got a whole pane, and the input
// got one truncated line above it. For a stage that maps two hundred elements
// to two hundred elements that is the wrong way round — the answer is not what
// came out, it is which of the two hundred moved.
//
// `d` answers that directly. Both sides are lined up as items — lines of text,
// elements of a list, rows of a grid — and the difference between them is shown
// as a diff: what went, what arrived, and how much stayed put.
//
// One thing had to be worked around. The recorder keeps only a *short*
// rendering of each step's input (interp.Step.InShort), because in a pipeline
// the input is the previous step's output and keeping both would double every
// recording. So that is where the full input comes from: the step before this
// one in trace order, checked against InShort to be sure it really is the value
// that flowed in. When the check fails — the first stage of the program, a
// branch, a body boundary — the pane says so and diffs what it has, rather than
// diffing two things that were never adjacent.
package main

import (
	"fmt"
	"strings"

	"domain/interp"
)

// diffLines renders the `d` pane: what this stage did to the value.
func (m *visualModel) diffLines(w int) []string {
	node := m.selectedNode()
	if node == nil {
		return []string{styDim.Render("(nothing recorded)")}
	}
	out := []string{
		styHeading.Render("what changed"),
		styDim.Render("the value in, against the value out"),
		"",
	}
	if node.IsFrame() {
		return append(out, styDim.Render(
			"  a frame is a label around a sub-pipeline — it changes nothing itself;"),
			styDim.Render("  its steps are the rows underneath"))
	}
	s := node.Step
	if s.Node.In == nil {
		return append(out, styDim.Render("  a source stage consumes nothing — it has no input to differ from"))
	}

	in, exact := m.inputValue(node)
	outVal := stepValue(s)
	// A block hands its input back to the pipeline, so the interesting
	// comparison is the input against what the *body* produced.
	if node.Block != nil {
		outVal = recordedOf(node.Block)
		out = append(out, styDim.Render("  (a block passes its input on; this is what its body produced)"), "")
	}

	if !exact {
		out = append(out, styDim.Render(
			"  the recording keeps the short form of a step's input, and the previous"),
			styDim.Render("  step is not this one's producer — comparing what there is"), "")
	}

	before, after := diffItems(in), diffItems(outVal)
	if before.kind != after.kind || before.kind == diffScalar {
		return append(out, scalarChange(in, outVal, w)...)
	}

	out = append(out, styDim.Render(fmt.Sprintf("  %s in · %s out · comparing %s",
		sizeLabel(in), sizeLabel(outVal), before.noun)), "")
	return append(out, renderDiff(before.items, after.items, w)...)
}

// inputValue is the full rendering of what flowed into a step, and whether it
// is certainly that value. See the file comment for why it comes from the
// step before.
func (m *visualModel) inputValue(node *interp.TraceNode) (recordedValue, bool) {
	s := node.Step
	fallback := recordedValue{short: s.InShort, typ: inTypeOf(s)}

	// The step before this one in trace order, skipping frames.
	at, ok := m.order[node]
	if !ok {
		return fallback, false
	}
	var prev *interp.TraceNode
	for j := at - 1; j >= 0; j-- {
		if !m.flat[j].IsFrame() {
			prev = m.flat[j]
			break
		}
	}
	if prev == nil {
		return fallback, false
	}
	// The check that makes this honest: the previous step's output has to be
	// the value this step says it received.
	if prev.Step.Short != s.InShort {
		return fallback, false
	}
	v := stepValue(prev.Step)
	v.typ = inTypeOf(s) // described by what this step expects, not what produced it
	return v, true
}

func inTypeOf(s *interp.Step) string {
	if s == nil || s.Node == nil || s.Node.In == nil {
		return ""
	}
	return s.Node.In.String()
}

// sizeLabel describes how much data a value holds, for the line above a diff.
func sizeLabel(v recordedValue) string {
	if !v.sizeOK {
		if v.typ != "" {
			return v.typ
		}
		return "a value"
	}
	return fmt.Sprintf("%d", v.size)
}

// diffKind is how a value was broken into comparable items.
type diffKind int

const (
	diffScalar diffKind = iota
	diffLines
	diffElems
)

type diffItemSet struct {
	kind  diffKind
	noun  string
	items []string
}

// diffItems breaks a value into the items a diff compares. The type decides:
// a grid and a Text both compare by line, a collection by element, and anything
// else is a single value that either changed or did not.
func diffItems(v recordedValue) diffItemSet {
	body := v.text()
	switch {
	case body == "":
		return diffItemSet{kind: diffScalar}
	case strings.HasPrefix(v.typ, "Grid"), strings.HasPrefix(v.typ, "Sparse"):
		return diffItemSet{kind: diffLines, noun: "rows", items: strings.Split(body, "\n")}
	case v.typ == "Text":
		if !strings.Contains(body, "\n") {
			return diffItemSet{kind: diffScalar}
		}
		return diffItemSet{kind: diffLines, noun: "lines", items: strings.Split(body, "\n")}
	case isCollectionType(v.typ):
		elems, ok := splitRendered(body)
		if !ok {
			return diffItemSet{kind: diffScalar}
		}
		return diffItemSet{kind: diffElems, noun: "elements", items: elems}
	}
	return diffItemSet{kind: diffScalar}
}

// scalarChange is the diff of two things that are not sequences: it says what
// they were and what they became, which for a number is the whole story.
func scalarChange(in, out recordedValue, w int) []string {
	before, after := in.text(), out.text()
	if before == after {
		return []string{styDim.Render("  unchanged"), "  " + styValue.Render(truncateVis(before, w-2))}
	}
	lines := []string{styDim.Render("  was")}
	for _, l := range strings.Split(before, "\n") {
		lines = append(lines, "  "+styErr.Render(truncateVis(showEnds(l), w-2)))
	}
	lines = append(lines, "", styDim.Render("  now"))
	for _, l := range strings.Split(after, "\n") {
		lines = append(lines, "  "+styMatch.Render(truncateVis(showEnds(l), w-2)))
	}
	return lines
}

// maxDiffItems bounds what is compared. The diff is quadratic in the worst
// case, and two hundred thousand elements is not a thing anyone reads anyway.
const maxDiffItems = 2000

// renderDiff shows how one sequence became another: removals, additions, and
// runs of unchanged items collapsed to a count, since a diff that prints the
// hundred and eighty items that stayed the same has buried its own answer.
func renderDiff(before, after []string, w int) []string {
	if len(before) > maxDiffItems || len(after) > maxDiffItems {
		return []string{styDim.Render(fmt.Sprintf(
			"  too much to compare (%d against %d items) — the value pane shows both",
			len(before), len(after)))}
	}
	ops := diffOps(before, after)

	var out []string
	same := 0
	flushSame := func() {
		if same == 0 {
			return
		}
		out = append(out, styDim.Render(fmt.Sprintf("    %s unchanged", plural(same, "item"))))
		same = 0
	}
	for _, op := range ops {
		switch op.kind {
		case opSame:
			same++
		case opDel:
			flushSame()
			out = append(out, styErr.Render(pad(truncateVis("  - "+showEnds(op.text), w-2), w-2)))
		case opAdd:
			flushSame()
			out = append(out, styMatch.Render(pad(truncateVis("  + "+showEnds(op.text), w-2), w-2)))
		}
	}
	flushSame()
	if len(out) == 1 && strings.Contains(out[0], "unchanged") {
		return []string{styDim.Render("  nothing changed — every item came through as it was")}
	}
	return out
}

type diffOpKind int

const (
	opSame diffOpKind = iota
	opDel
	opAdd
)

type diffOp struct {
	kind diffOpKind
	text string
}

// diffOps is a longest-common-subsequence diff. It is the textbook table, which
// is more than enough here: the inputs are bounded by maxDiffItems, and the
// alternative — comparing by index — reports an inserted element as every
// element after it having changed, which is the one answer a reader must not be
// given.
func diffOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// lcs[i][j] is the length of the longest common subsequence of a[i:] and
	// b[j:], filled backwards so the walk forwards below reads naturally.
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{opSame, a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{opDel, a[i]})
			i++
		default:
			ops = append(ops, diffOp{opAdd, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{opDel, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{opAdd, b[j]})
	}
	return ops
}
