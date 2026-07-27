package interp

import (
	"fmt"
	"time"

	"domain/ir"
)

// The value-capturing consumer of the trace hook, built for the step-by-step
// visualizer (`domain expansion: visualize`).
//
// It records a *tree*, not a list: a loop's iterations and a Channel or Part
// body are frames you can step into. Building that tree needs the same ordering
// fact --stats relies on — a node's own Step is reported after its Eval returns,
// so its frames have already come and gone by then. Completed frames are held
// as pending children and attached to the next top-level step, which is the one
// whose Eval produced them.
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

// TraceNode is one row of the recorded tree: either a step or a frame that
// holds steps.
type TraceNode struct {
	Frame    string // set when this node is a frame ("Repeat 4 iter 2/4")
	Step     *Step  // set when this node is a step
	Children []*TraceNode
}

// IsFrame reports whether this row is a frame rather than a step.
func (n *TraceNode) IsFrame() bool { return n.Step == nil }

// Counts reports how many steps and frames are recorded underneath this row, at
// any depth. A collapsed `Repeat 500` row says what it is hiding with these.
func (n *TraceNode) Counts() (steps, frames int) {
	for _, c := range n.Children {
		if c.IsFrame() {
			frames++
		} else {
			steps++
		}
		s, f := c.Counts()
		steps, frames = steps+s, frames+f
	}
	return steps, frames
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

	stack   []*TraceNode // open frames, innermost last
	pending []*TraceNode // completed frames awaiting their enclosing step
	orphans *TraceNode   // the synthetic row for pending frames, built once
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

	size, sizeOK := ir.SizeOf(e.Out)
	st := &Step{
		Index:   r.steps - 1,
		Node:    e.Node,
		Depth:   e.Depth,
		Frame:   e.Frame,
		InShort: ir.FormatShort(e.In),
		Short:   ir.FormatShort(e.Out),
		Size:    size,
		SizeOK:  sizeOK,
		Err:     e.Err,
		Dur:     e.Dur,
	}
	r.captureFull(st, e.Out)

	node := &TraceNode{Step: st}
	if len(r.stack) > 0 {
		// Inside a frame: this step belongs to it directly.
		parent := r.stack[len(r.stack)-1]
		parent.Children = append(parent.Children, node)
		return
	}
	// A top-level step owns whatever frames were opened during its Eval.
	node.Children = append(node.Children, adopt(node, r.pending)...)
	r.pending = nil
	r.roots = append(r.roots, node)
}

// adopt attaches completed frames to the step that produced them, collapsing a
// frame that merely restates its owner. A Channel or Part opens exactly one
// frame labelled the same as its own row, so keeping it would put a redundant
// level between the row and its body; a loop's iterations are genuinely
// distinct rows and are kept.
func adopt(owner *TraceNode, frames []*TraceNode) []*TraceNode {
	out := make([]*TraceNode, 0, len(frames))
	for _, f := range frames {
		if f.IsFrame() && f.Frame == owner.Label() {
			out = append(out, f.Children...)
			continue
		}
		out = append(out, f)
	}
	return out
}

// captureFull stores the value's full rendering while the byte budget lasts.
// FormatShort is always kept, so a step is never invisible — only its detail is.
func (r *Recorder) captureFull(st *Step, v ir.Value) {
	if r.budget <= 0 {
		return
	}
	full := ir.FormatValue(v)
	if len(full) > maxValueBytes {
		st.Full = full[:maxValueBytes]
		st.FullOK = false
		r.budget -= maxValueBytes
		return
	}
	st.Full = full
	st.FullOK = true
	r.budget -= len(full)
}

// PushFrame opens a frame.
func (r *Recorder) PushFrame(label string) {
	f := &TraceNode{Frame: label}
	if len(r.stack) > 0 {
		parent := r.stack[len(r.stack)-1]
		parent.Children = append(parent.Children, f)
	}
	r.stack = append(r.stack, f)
}

// PopFrame closes the innermost frame. A frame closed at the top level is held
// until the step that produced it reports.
func (r *Recorder) PopFrame() {
	if len(r.stack) == 0 {
		return
	}
	f := r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	if len(r.stack) == 0 {
		r.pending = append(r.pending, f)
	}
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
	r.orphans.Children = r.pending
	return append(append([]*TraceNode(nil), r.roots...), r.orphans)
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
