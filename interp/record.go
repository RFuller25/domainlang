package interp

import (
	"fmt"
	"slices"
	"time"

	"domain/ast"
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
//
// The step cap has one exception, and it is the whole reason a debugger exists:
// a *failing* step is recorded however far past the cap it happens. A run that
// dies at step 400,000 would otherwise record its first stretch, say "capped",
// and have nothing to jump to — the one row the reader opened the tool for.

// DefaultMaxSteps is how many steps a recording keeps before it stops.
//
// It is a memory bound and nothing more, so it is set where a recording of a
// real program fits comfortably rather than where a cautious guess would put
// it: the trace of an Advent of Code solution over its real input runs to
// hundreds of thousands of steps, and a debugger that shows the first 10,000 of
// them is answering a question nobody asked. Unlimited is one flag away
// (`--max-steps 0`), and the header says so whenever the cap was reached.
const DefaultMaxSteps = 250000

// Unlimited, as a max-steps argument, records the whole run however long it is.
const Unlimited = -1

// defaultForeignBudget caps the total bytes spent on captured foreign streams.
// A block inside a `Map Each` body runs once per element and each run may be
// handed the whole input, so without a ceiling a recording of one could be
// larger than the input several thousand times over. Past it the runs are still
// recorded — what ran, how long it took — and only the streams are dropped,
// which is the part a reader has already seen a representative sample of.
const defaultForeignBudget = 1 << 20 // 1 MiB

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

	// Spent marks a value whose full rendering was never built because the
	// recording's byte budget was gone — a different answer from a value too
	// large to keep whole (FullOK), and one a reader has to be able to tell
	// apart: the first says "ask for less", the second says "there is more".
	Spent bool

	// Apply is the `Using:` application this step is represented by, when it
	// made one. See Application.
	Apply *Application

	// Foreign is the foreign block execution this step is represented by, when
	// it made one. See ForeignExec.
	Foreign *ForeignExec
}

// ForeignExec is one recorded execution of a foreign-language block, which one
// of the step's executions it is, and how many the step made in all.
//
// Only one is kept, for the reason Application keeps only one: a block inside a
// `Map Each` body runs once per element, and a recording holding every one of
// them would be the input again, several times over. Unlike an application it
// cannot be replayed to recover the rest — a subprocess is not a pure
// expression — so what is here is what was captured while it ran.
//
// Which one is kept is the interesting part. The first, normally: it is
// representative and it is the one a reader can be shown without being asked
// which. But a run that *failed* displaces it, because a block that works for
// the first forty elements and dies on the forty-first is exactly the shape of
// the bug someone opens this pane to find, and showing them the healthy first
// run and its tidy stdout is showing them the one thing that is not the answer.
type ForeignExec struct {
	Run   ir.ForeignRun
	Index int // which execution this was, 1-based
	Count int
}

// Application is one application of a `Using:` lambda: the expression, and what
// it was applied to.
//
// It is the seed of the expression breakdown, not the breakdown itself. The
// expression layer is pure, so (lambda, arguments) is enough to reproduce every
// intermediate value on demand (eval.TraceLambda) — which means a recording can
// stay this small and still answer, for any step, what its expression actually
// computed.
//
// Only one of a step's applications is kept, because that is the one a reader
// can be shown: a `Map Each` over ten thousand elements applies its lambda ten
// thousand times, and a recording holding all of them would be the data, not a
// trace of it. Index says which one it is and Count how many there were.
//
// Normally it is the first. On a step that *failed* it is the last one to
// start, which is the one that failed: applications are reported on the way in
// (see eval.EvalLambdaTyped), the failing one is what stopped the step, and so
// nothing was applied after it. Without that swap the expression pane answers a
// question about element 1 while the row beside it is marked failed at element
// 900 — the pane showing its most confident wrong answer at the exact moment it
// is most likely to be read.
type Application struct {
	Lambda *ast.Lambda
	Types  []*ir.Type
	Args   []ir.Value
	Index  int // which application this was, 1-based
	Count  int
}

// Recorded is one captured value: the renderings a reader shows, and the size
// that answers "how much data is this". Step keeps the same fields inline for
// its own output; this is the form the *body* of a block reports.
type Recorded struct {
	Short  string // ir.FormatShort
	Full   string // ir.FormatValue of the value, truncated; "" when not kept
	FullOK bool   // whether Full holds the whole rendering
	Spent  bool   // whether Full is empty because the byte budget was gone
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

	maxSteps  int // 0 or less: no bound
	budget    int
	fgnBudget int
	steps     int
	extra     int // failing steps recorded past the cap; see Step
	truncated bool

	// progress, when set, is called every progressEvery steps. It is how a
	// command can say a long program is still running rather than leaving a
	// blank terminal: the recorder is the only thing that knows how far the run
	// has got, and calling out from here keeps it a synchronous integer
	// compare instead of a second goroutine reading the count from under it.
	progress func(Progress)
	started  time.Time

	stack    []*TraceNode // open frames, innermost last
	pending  []*TraceNode // top-level frames awaiting their enclosing step
	orphans  *TraceNode   // the synthetic row for pending frames, built once
	orphaned int          // how many frames the orphan row was last built for

	// apply is the first lambda application seen since the last step reported
	// and applyLast the most recent one, with applies counting them. A lambda
	// runs inside a node's Eval, so the next step to report is the node that
	// ran it — and which of the two it keeps depends on whether that step
	// failed (see Application).
	apply     *Application
	applyLast *Application
	applies   int
	// foreign is the foreign block execution the step in progress is
	// represented by, and how many it made in total. Same shape as apply and
	// for the same reason: a subprocess is run under a node's Eval, not beside
	// it, so it is caught by a watcher and attached to whichever step reports
	// next.
	foreign  *ir.ForeignRun
	fgnIndex int
	fgnCost  int // bytes the retained run was charged, refunded if it is replaced
	foreigns int
}

// Progress is how far a recording has got, for a caller showing a long program
// is still running.
type Progress struct {
	Steps   int
	Elapsed time.Duration
	Capped  bool
}

// progressEvery is how many steps pass between progress reports. A caller that
// wants a slower display throttles on its own clock; this only has to be often
// enough that the first report comes quickly and rare enough to cost nothing.
const progressEvery = 2000

// NewRecorder returns a recorder keeping at most maxSteps steps. Zero means
// DefaultMaxSteps; Unlimited (or any negative) records the whole run.
func NewRecorder(maxSteps int) *Recorder {
	if maxSteps == 0 {
		maxSteps = DefaultMaxSteps
	}
	if maxSteps < 0 {
		maxSteps = 0 // no bound
	}
	return &Recorder{maxSteps: maxSteps, budget: defaultValueBudget, fgnBudget: defaultForeignBudget}
}

// OnProgress installs a callback reporting how far the run has got, called
// every progressEvery steps while recording. Pass nil to turn it off.
func (r *Recorder) OnProgress(f func(Progress)) { r.progress = f }

// capped reports whether the step cap has been reached.
func (r *Recorder) capped() bool { return r.maxSteps > 0 && r.steps >= r.maxSteps }

// Step records one node evaluation.
func (r *Recorder) Step(e ir.StepEvent) {
	pastCap := false
	if r.capped() {
		pastCap = true
		r.truncated = true
		// A failing step is recorded anyway, however far past the cap it
		// happened: it is the row the whole tool exists to get someone to, and
		// dropping it leaves a recording that says "run failed" in the footer
		// and has no failure in it. The frames it hangs under are kept for the
		// same reason — PopFrame drops an empty frame past the cap, and this
		// one is no longer empty.
		if e.Err == nil {
			r.pulse()
			return
		}
		r.extra++
	} else {
		r.steps++
	}
	r.pulse()

	out := r.capture(e.Out)
	st := &Step{
		Index:   r.steps + r.extra - 1,
		Node:    e.Node,
		Depth:   e.Depth,
		Frame:   e.Frame,
		InShort: ir.FormatShort(e.In),
		Short:   out.Short,
		Full:    out.Full,
		FullOK:  out.FullOK,
		Spent:   out.Spent,
		Size:    out.Size,
		SizeOK:  out.SizeOK,
		Err:     e.Err,
		Dur:     e.Dur,
	}
	// Whatever lambda applications have happened since the last step were run
	// by this node's Eval, so they belong to this step — and are cleared either
	// way, so a step that ran no lambda never inherits an earlier one's. On a
	// failed step the *last* one is kept: it is the application that failed,
	// for the reason Application documents.
	if a := r.apply; a != nil {
		if e.Err != nil && r.applyLast != nil {
			a = r.applyLast
		}
		a.Count = r.applies
		st.Apply = a
	}
	r.apply, r.applyLast, r.applies = nil, nil, 0
	if r.foreign != nil {
		st.Foreign = &ForeignExec{Run: *r.foreign, Index: r.fgnIndex, Count: r.foreigns}
	}
	r.foreign, r.foreigns, r.fgnIndex, r.fgnCost = nil, 0, 0, 0

	// A step owns whatever frames were opened during its Eval, at whatever
	// level it reported on: a loop nested inside another loop's body opened its
	// laps from inside that body, and they belong under the nested loop's row
	// rather than beside it.
	//
	// That reasoning holds only while every step is recorded. Past the cap it
	// does not: the steps that would have claimed the waiting frames were
	// dropped, so the frames pending at this level were *not* all opened by this
	// step's Eval. A failure recorded past the cap therefore adopts nothing —
	// otherwise a dropped `Repeat 300`'s laps would hang under whichever later
	// step happened to fail, and be reported as that step's own cost. They stay
	// pending and surface under the "(incomplete)" row, which is what they are.
	node := &TraceNode{Step: st}
	if len(r.stack) > 0 {
		parent := r.stack[len(r.stack)-1]
		if !pastCap {
			node.Children = fold(adopt(node, parent.pend))
			parent.pend = nil
		}
		parent.Children = append(parent.Children, node)
		return
	}
	if !pastCap {
		node.Children = fold(adopt(node, r.pending))
		r.pending = nil
	}
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

// pulse reports progress every progressEvery steps, for a caller showing that a
// long program is still running.
func (r *Recorder) pulse() {
	if r.progress == nil {
		return
	}
	if r.started.IsZero() {
		r.started = time.Now()
	}
	if (r.steps+r.extra)%progressEvery != 0 {
		return
	}
	r.progress(Progress{Steps: r.steps + r.extra, Elapsed: time.Since(r.started), Capped: r.truncated})
}

// capture renders a value for display, spending the byte budget on the full
// form. FormatShort is always kept, so a value is never invisible — only its
// detail is.
//
// The rendering is built *to* the limit rather than built and then cut
// (ir.FormatValueLimit). The difference is not the 64 KiB that gets kept, it is
// the rest: a step whose output is a list the program is midway through
// building would otherwise render every element of it into a string — tens of
// megabytes, once per step, thrown away except for the head.
func (r *Recorder) capture(v ir.Value) Recorded {
	size, sizeOK := ir.SizeOf(v)
	rec := Recorded{Short: ir.FormatShort(v), Size: size, SizeOK: sizeOK}
	if r.budget <= 0 {
		rec.Spent = true
		return rec
	}
	// Never build more than can be kept: whichever of the per-value cap and
	// what is left of the budget is smaller.
	full, complete := ir.FormatValueLimit(v, min(maxValueBytes, r.budget))
	rec.Full, rec.FullOK = full, complete
	r.budget -= len(full)
	return rec
}

// Applied records one `Using:` application. It has the signature of
// eval.Applied, so a caller that wants expression detail in its recording wires
// the two together (eval.WatchApplications) and needs to know nothing else.
//
// The lambda layer sits below the trace hook — a `Using:` is applied inside a
// primitive, where no node evaluation reports — so this is the only way the
// recorder can learn an expression ran at all.
func (r *Recorder) Applied(l *ast.Lambda, types []*ir.Type, args []ir.Value) {
	r.applies++
	// Past the step cap the recording has stopped storing values, but it has
	// not stopped recording *failures* (see Step) — and a failing step's
	// expression is the one thing that makes such a row worth reaching. So the
	// latest application is still tracked; only the growth that would not be
	// shown is skipped.
	//
	// Kept as given: eval hands over copies (see eval.Applied).
	a := &Application{Lambda: l, Types: types, Args: args, Index: r.applies}
	r.applyLast = a
	if r.apply == nil {
		r.apply = a
	}
}

// ForeignRan records a foreign block execution. It is prims.ForeignWatcher,
// structurally — interp does not import prims, exactly as it does not import
// eval for Applied; the caller wires the two together.
func (r *Recorder) ForeignRan(run ir.ForeignRun) {
	r.foreigns++
	// One run is kept per step: the first, unless a later one failed, which
	// displaces it (see ForeignExec). Unlike an application this is knowable
	// here — a foreign run is reported once it is over, with its error.
	if r.foreign != nil && (r.foreign.Err != nil || run.Err == nil) {
		return
	}
	// A displaced run gives its bytes back, so a block that fails on its
	// thousandth element is not refused capture by the budget its own healthy
	// runs spent.
	if r.foreign != nil {
		r.fgnBudget += r.fgnCost
	}
	// The streams are the expensive part and the first to go; the run itself
	// still gets recorded, so a reader past the budget is told a block ran and
	// what it cost rather than not told at all.
	cost := len(run.Stdin.Text) + len(run.Stdout.Text) + len(run.Stderr.Text)
	if cost > r.fgnBudget {
		run.Stdin.Text, run.Stdout.Text, run.Stderr.Text = "", "", ""
		cost = 0
	}
	r.fgnBudget -= cost
	r.foreign, r.fgnIndex, r.fgnCost = &run, r.foreigns, cost
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

// Steps reports how many steps were recorded, including any failing ones kept
// past the cap.
func (r *Recorder) Steps() int { return r.steps + r.extra }

// Truncated reports whether the step cap was reached, so a UI can say so.
func (r *Recorder) Truncated() bool { return r.truncated }

// Summary is a one-line description of the recording, for a status bar. A
// capped recording names the flag that lifts the cap: "capped" on its own tells
// a reader their trace is incomplete and leaves them nowhere to go.
func (r *Recorder) Summary() string {
	if !r.truncated {
		return fmt.Sprintf("%d steps", r.steps)
	}
	if r.extra > 0 {
		return fmt.Sprintf("%d steps (capped at %d — %s past the cap kept; --max-steps 0 records it all)",
			r.steps+r.extra, r.maxSteps, plural(r.extra, "failing step"))
	}
	return fmt.Sprintf("%d steps (capped at %d — the run continued past this point; "+
		"--max-steps 0 records it all)", r.steps, r.maxSteps)
}

// plural renders a count with its noun, so a summary does not say "1 steps".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
