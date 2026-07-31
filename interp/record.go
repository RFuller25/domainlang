package interp

import (
	"fmt"
	"slices"
	"time"

	"domain/ir"
)

// The value-capturing consumer of the trace hook, built for the step-by-step
// visualizer (`domain expansion: visualize`).
//
// It records a *tree*, not a list: a loop's iterations and a Channel or Part
// body are frames you can step into. Building that tree needs the same ordering
// fact --stats relies on — a node's own Step is reported after its Eval returns,
// so its frames have already come and gone by then. A closed frame is therefore
// held at the level that opened it until a step reports there, and that step is
// the one whose Eval produced it. Holding them per level, rather than only at
// the top, is what puts a loop nested inside another loop's body in charge of
// its own laps instead of leaving them beside it.
//
// Two things the tree records that the pipeline cannot say. A block's *result*:
// a Channel and a Part hand their input back to the pipeline, so what the code
// inside them computed appears in no step's output, and the frame reports it as
// it closes. And a *fold*: past a couple of laps, a loop's iterations are
// gathered under one row that opens onto all of them, so five hundred laps of
// the same three steps do not bury the program around them.
//
// Capture is bounded twice over, because a Domain program can have a million
// loop iterations over a million-element list: a cap on how many steps are kept
// at all, and a byte budget for full value renderings. Past either limit the
// recorder keeps going but stops storing, and says so — a visualizer that
// silently showed a truncated run would be worse than one that admits it.

// DefaultMaxSteps is how many steps a recording keeps before it stops.
const DefaultMaxSteps = 10000

// defaultValueBudget caps the total bytes spent on full value renderings.
const defaultValueBudget = 8 << 20 // 8 MiB

// maxValueBytes caps one value's full rendering.
const maxValueBytes = 64 << 10 // 64 KiB

// Step is one recorded node evaluation.
type Step struct {
	Index   int
	Node    *ir.Node
	Depth   int
	Frame   string
	InShort string // ir.FormatShort of the input
	Short   string // ir.FormatShort of the output
	Full    string // ir.FormatValue of the output, truncated; "" when not kept
	FullOK  bool   // whether Full holds the whole rendering
	Size    int
	SizeOK  bool
	Err     error
	Dur     time.Duration
}

// Recorded is one captured value: the renderings a reader shows, and the size
// that answers "how much data is this". Step keeps the same fields inline for
// its own output; this is the form the *body* of a block reports.
type Recorded struct {
	Short  string // ir.FormatShort
	Full   string // ir.FormatValue of the value, truncated; "" when not kept
	FullOK bool   // whether Full holds the whole rendering
	Size   int
	SizeOK bool

	// Type names what the value is. It comes from whoever opened the frame
	// (Recorder.PushFrame): a frame carries no node of its own to ask, and a
	// value cannot answer either — an empty list is a list of what?
	Type string
}

// TraceNode is one row of the recorded tree: either a step or a frame that
// holds steps.
type TraceNode struct {
	Frame    string // set when this node is a frame ("Repeat 4 iter 2/4")
	Step     *Step  // set when this node is a step
	Children []*TraceNode

	// Block is what the row's own body produced, when the row has one: the
	// value a Channel, a Part or one loop iteration computed. It is a separate
	// answer from a step's Out because those stages are passthroughs — the
	// pipeline carries on with the value that went *in*, so the step's own
	// output describes what flowed past the block, not what the block did.
	// nil on a row with no body, and on a body that did not finish.
	Block *Recorded

	// Folded marks the synthetic row that stands in for a run of sibling
	// frames — the laps of one loop, gathered so they can be opened as a group
	// rather than burying everything after them. See fold.
	Folded bool

	// pend holds frames closed inside this one whose step has not reported
	// yet; they are attached to that step when it does. See Recorder.Step.
	pend []*TraceNode

	// blockType is the type this frame's body produces, named by whoever
	// opened it (Recorder.PushFrame), and copied onto Block when it closes.
	blockType string
}

// IsFrame reports whether this row is a frame rather than a step.
func (n *TraceNode) IsFrame() bool { return n.Step == nil }

// Counts reports how many steps and frames are recorded underneath this row, at
// any depth. A collapsed `Repeat 500` row says what it is hiding with these.
//
// A fold is not counted as a frame of its own: it is one row standing in for
// the laps beneath it, and counting both would report a loop of four laps as
// five frames.
func (n *TraceNode) Counts() (steps, frames int) {
	for _, c := range n.Children {
		switch {
		case !c.IsFrame():
			steps++
		case !c.Folded:
			frames++
		}
		s, f := c.Counts()
		steps, frames = steps+s, frames+f
	}
	return steps, frames
}

// Iterations reports how many frames a folded row stands for, and whether it is
// one at all — what a reader needs to describe the fold without unpacking it.
func (n *TraceNode) Iterations() (int, bool) {
	if !n.Folded {
		return 0, false
	}
	return len(n.Children), true
}

// Label renders the row's headline.
func (n *TraceNode) Label() string {
	if n.IsFrame() {
		return n.Frame
	}
	if n.Step.Node.Display != "" {
		return n.Step.Node.Display
	}
	return n.Step.Node.Prim
}

// Recorder is a Tracer that keeps a bounded tree of the run.
type Recorder struct {
	roots []*TraceNode

	maxSteps  int
	budget    int
	steps     int
	truncated bool

	stack    []*TraceNode // open frames, innermost last
	pending  []*TraceNode // top-level frames awaiting their enclosing step
	orphans  *TraceNode   // the synthetic row for pending frames, built once
	orphaned int          // how many frames the orphan row was last built for
}

// NewRecorder returns a recorder keeping at most maxSteps steps (0 means
// DefaultMaxSteps).
func NewRecorder(maxSteps int) *Recorder {
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}
	return &Recorder{maxSteps: maxSteps, budget: defaultValueBudget}
}

// Step records one node evaluation.
func (r *Recorder) Step(e ir.StepEvent) {
	if r.steps >= r.maxSteps {
		r.truncated = true
		return
	}
	r.steps++

	out := r.capture(e.Out)
	st := &Step{
		Index:   r.steps - 1,
		Node:    e.Node,
		Depth:   e.Depth,
		Frame:   e.Frame,
		InShort: ir.FormatShort(e.In),
		Short:   out.Short,
		Full:    out.Full,
		FullOK:  out.FullOK,
		Size:    out.Size,
		SizeOK:  out.SizeOK,
		Err:     e.Err,
		Dur:     e.Dur,
	}

	// A step owns whatever frames were opened during its Eval, at whatever
	// level it reported on: a loop nested inside another loop's body opened its
	// laps from inside that body, and they belong under the nested loop's row
	// rather than beside it.
	node := &TraceNode{Step: st}
	if len(r.stack) > 0 {
		parent := r.stack[len(r.stack)-1]
		node.Children = fold(adopt(node, parent.pend))
		parent.pend = nil
		parent.Children = append(parent.Children, node)
		return
	}
	node.Children = fold(adopt(node, r.pending))
	r.pending = nil
	r.roots = append(r.roots, node)
}

// adopt attaches completed frames to the step that produced them, collapsing a
// frame that merely restates its owner. A Channel or Part opens exactly one
// frame labelled the same as its own row, so keeping it would put a redundant
// level between the row and its body; a loop's iterations are genuinely
// distinct rows and are kept.
//
// The collapsed frame's result moves onto the owner, because it is the one
// thing the owner's own Out cannot say: a Channel and a Part hand their input
// back to the pipeline, so without this the row for a block would report the
// value that went into it as the value it produced.
func adopt(owner *TraceNode, frames []*TraceNode) []*TraceNode {
	out := make([]*TraceNode, 0, len(frames))
	for _, f := range frames {
		if f.IsFrame() && f.Frame == owner.Label() {
			owner.Block = f.Block
			out = append(out, f.Children...)
			continue
		}
		out = append(out, f)
	}
	return number(out)
}

// number distinguishes a run of frames that all came out with the same label.
//
// A loop names its own laps, because it knows how many there are. A `Using:`
// body does not: the primitive runs it once per element through the lambda
// layer, which never says which element. Here the count is known, so the rows
// can say `Map Each body 3/8` rather than eight rows of the same three words.
func number(rows []*TraceNode) []*TraceNode {
	if len(rows) < 2 {
		return rows
	}
	base := rows[0].Frame
	for _, r := range rows {
		if !r.IsFrame() || r.Frame != base {
			return rows
		}
	}
	for i, r := range rows {
		r.Frame = fmt.Sprintf("%s %d/%d", base, i+1, len(rows))
	}
	return rows
}

// foldFrom is how many sibling frames a row needs before they are folded into
// one. Two laps of a loop read better in place; twenty bury the stage after
// them, and five hundred bury the program.
const foldFrom = 3

// fold gathers a row's children into a single collapsed frame when they are
// nothing but frames — the laps of one loop, which are the same few steps over
// and over. The laps are kept, not summarized away: the fold is a row that
// opens onto all of them, so the shape of the run is readable at a glance and
// still explorable in full.
//
// Anything else is left alone. A block body's rows are its distinct stages, and
// there is nothing repetitive to gather.
func fold(children []*TraceNode) []*TraceNode {
	if len(children) < foldFrom {
		return children
	}
	for _, c := range children {
		if !c.IsFrame() {
			return children
		}
	}
	last := children[len(children)-1]
	return []*TraceNode{{
		Frame:    fmt.Sprintf("%d iterations", len(children)),
		Folded:   true,
		Children: children,
		// The last lap's result is the loop's result, so the folded row can
		// answer what the laps came to without being opened.
		Block: last.Block,
	}}
}

// capture renders a value for display, spending the byte budget on the full
// form. FormatShort is always kept, so a value is never invisible — only its
// detail is.
func (r *Recorder) capture(v ir.Value) Recorded {
	size, sizeOK := ir.SizeOf(v)
	rec := Recorded{Short: ir.FormatShort(v), Size: size, SizeOK: sizeOK}
	if r.budget <= 0 {
		return rec
	}
	full := ir.FormatValue(v)
	if len(full) > maxValueBytes {
		rec.Full = full[:maxValueBytes]
		r.budget -= maxValueBytes
		return rec
	}
	rec.Full, rec.FullOK = full, true
	r.budget -= len(full)
	return rec
}

// PushFrame opens a frame. It is not attached anywhere yet: where it belongs
// depends on which step turns out to have opened it, and that step reports
// only once its Eval — and so this frame — is finished.
//
// The type is kept for the frame's result, since a frame has no node of its own
// to ask and a value cannot be asked either — an empty list is a List of what?
func (r *Recorder) PushFrame(label string, out *ir.Type) {
	f := &TraceNode{Frame: label}
	if out != nil {
		f.blockType = out.String()
	}
	r.stack = append(r.stack, f)
}

// PopFrame closes the innermost frame, recording what its body produced. The
// frame is held at the level that opened it until the step that produced it
// reports (Step adopts it), since that is the row it belongs under.
func (r *Recorder) PopFrame(out ir.Value) {
	if len(r.stack) == 0 {
		return
	}
	f := r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	// Past the step cap the recording has stopped storing, and a loop with a
	// million laps still closes a million frames: rendering their results would
	// be work spent on a recording that already says it is incomplete.
	if out != nil && !r.truncated {
		block := r.capture(out)
		block.Type = f.blockType
		f.Block = &block
	}
	// Frames this one opened whose step never reported — a body that failed
	// part way through — would otherwise be lost with the level they waited on.
	f.Children = append(f.Children, f.pend...)
	f.pend = nil

	// Past the cap a frame that recorded nothing is dropped rather than kept:
	// steps are bounded, but a `Map Each` over a million elements opens a
	// million frames, and holding empty rows for all of them is the unbounded
	// growth the cap exists to prevent.
	if r.truncated && len(f.Children) == 0 && f.Block == nil {
		return
	}
	if len(r.stack) == 0 {
		r.pending = append(r.pending, f)
		return
	}
	parent := r.stack[len(r.stack)-1]
	parent.pend = append(parent.pend, f)
}

// Roots returns the recorded top-level rows. Any frames still pending (a run
// that failed inside a loop, so the enclosing step never reported) are attached
// to a synthetic row rather than dropped, since that is exactly the run a user
// most wants to look at.
//
// The synthetic row is built once and reused, because callers key maps by node
// pointer — the timing profile and the UI's collapse state both do — and a row
// that was a different object on every call would silently miss every lookup.
func (r *Recorder) Roots() []*TraceNode {
	if len(r.pending) == 0 {
		return r.roots
	}
	if r.orphans == nil {
		r.orphans = &TraceNode{Frame: "(incomplete — the enclosing stage did not finish)"}
	}
	// Rebuilt only when there is something new to hold, for the same reason the
	// row itself is reused: folding builds rows, and rows callers have keyed a
	// map by must not be replaced underneath them.
	if r.orphaned != len(r.pending) {
		r.orphaned = len(r.pending)
		r.orphans.Children = fold(r.pending)
	}
	return append(slices.Clone(r.roots), r.orphans)
}

// Steps reports how many steps were recorded.
func (r *Recorder) Steps() int { return r.steps }

// Truncated reports whether the step cap was reached, so a UI can say so.
func (r *Recorder) Truncated() bool { return r.truncated }

// Summary is a one-line description of the recording, for a status bar.
func (r *Recorder) Summary() string {
	if r.truncated {
		return fmt.Sprintf("%d steps (capped — the run continued past this point)", r.steps)
	}
	return fmt.Sprintf("%d steps", r.steps)
}
