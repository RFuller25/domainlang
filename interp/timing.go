package interp

import (
	"fmt"
	"sort"
	"time"

	"domain/ir"
)

// Turning a recording's raw durations into shares of the whole run, for
// `domain expansion: visualize`.
//
// Two facts about the recorded durations shape this.
//
// First, they *nest*. ir.EvalNode times a node's entire Eval, so a loop's
// duration already contains every iteration of its body, and a Channel's
// contains its whole sub-pipeline. Summing every recorded step would count the
// same nanoseconds once per level of nesting and produce a "total" several
// times the length of the run. The denominator is therefore the sum of the
// *top-level* rows only — the same total `--stats` reports — which is exactly
// what makes the top-level percentages add up to 100.
//
// Second, once work nests, "how long did this step take" has two answers, and
// showing only one of them misleads. A `Repeat 500` row at 98% is not a slow
// loop primitive; it is 500 iterations of whatever is inside it. So every row
// carries both: Total, the inclusive figure, and Self, what remains after its
// own frames are subtracted. Self is where the time actually went.

// NodeTiming is one row's cost, both ways of counting it.
type NodeTiming struct {
	Total    time.Duration // including everything nested inside this row
	Self     time.Duration // Total minus the work of this row's own frames
	TotalPct float64       // Total as a percentage of the whole recorded run
	SelfPct  float64       // Self as a percentage of the whole recorded run
	Known    bool          // whether the percentages mean anything (see Timing)

	// Nested reports whether Self is a distinct, meaningful number for this
	// row, so a renderer knows when showing it earns its column. On a leaf step
	// Self equals Total; on a frame Self is zero by construction, since a frame
	// is a label around a sub-pipeline and never does work of its own.
	Nested bool
}

// Hotspot is one call site's cost, added up over every time it ran. It is
// ranked by self time, because that is the question a profile answers: not
// "which row contains the most work" — the first stage of a pipeline always
// does — but "which one *is* the work".
type Hotspot struct {
	Node     *ir.Node
	Name     string
	Calls    int
	Self     time.Duration
	SelfPct  float64
	Total    time.Duration
	TotalPct float64
	Failed   bool
}

// Timing is the timing profile of a recording, keyed by trace node.
type Timing struct {
	overall time.Duration
	nodes   map[*TraceNode]NodeTiming

	hot     map[*ir.Node]*Hotspot
	hotSeen []*ir.Node // first-seen order, so equal costs rank stably
	hottest *TraceNode // the single row with the most self time
}

// Timing computes the recording's timing profile.
//
// It is derived rather than accumulated during the run: the recorder is on the
// hot path of an instrumented run and the tree already holds every duration, so
// there is nothing to gain by counting twice.
func (r *Recorder) Timing() *Timing {
	t := &Timing{
		nodes: make(map[*TraceNode]NodeTiming, r.steps),
		hot:   map[*ir.Node]*Hotspot{},
	}
	for _, n := range r.Roots() {
		t.overall += t.measure(n)
	}
	// A second pass, because the percentages need the denominator that the
	// first pass is still computing.
	for n, nt := range t.nodes {
		if t.overall > 0 {
			nt.TotalPct = 100 * float64(nt.Total) / float64(t.overall)
			nt.SelfPct = 100 * float64(nt.Self) / float64(t.overall)
			nt.Known = true
		}
		t.nodes[n] = nt
	}
	for _, h := range t.hot {
		if t.overall > 0 {
			h.SelfPct = 100 * float64(h.Self) / float64(t.overall)
			h.TotalPct = 100 * float64(h.Total) / float64(t.overall)
		}
	}
	return t
}

// measure records one row's cost and returns its inclusive duration.
func (t *Timing) measure(n *TraceNode) time.Duration {
	var nested time.Duration
	for _, c := range n.Children {
		nested += t.measure(c)
	}
	// A step's own duration already includes its frames. A frame is not timed
	// itself — it is a label around a sub-pipeline — so it costs what happened
	// inside it, and has no self time of its own.
	total := nested
	if !n.IsFrame() {
		total = n.Step.Dur
	}
	self := total - nested
	// Clock granularity, and a recording that hit --max-steps partway through a
	// body, can both leave a row's children summing past the row itself. Zero is
	// the honest floor; a negative self time would only ever be noise.
	if self < 0 {
		self = 0
	}
	t.nodes[n] = NodeTiming{
		Total:  total,
		Self:   self,
		Nested: !n.IsFrame() && len(n.Children) > 0,
	}
	if !n.IsFrame() {
		t.addHot(n, total, self)
		if t.hottest == nil || self > t.nodes[t.hottest].Self {
			t.hottest = n
		}
	}
	return total
}

// addHot folds one recorded evaluation into its call site's running total. The
// key is the ir.Node, not the primitive name: a `Map Each` inside a loop is one
// call site that ran many times, and rolling its iterations together is the
// whole point — 400 rows of 2µs are invisible, one row of 800µs is not.
func (t *Timing) addHot(n *TraceNode, total, self time.Duration) {
	h, ok := t.hot[n.Step.Node]
	if !ok {
		h = &Hotspot{Node: n.Step.Node, Name: n.Label()}
		t.hot[n.Step.Node] = h
		t.hotSeen = append(t.hotSeen, n.Step.Node)
	}
	h.Calls++
	h.Self += self
	h.Total += total
	if n.Step.Err != nil {
		h.Failed = true
	}
}

// Hotspots ranks the recording's call sites by self time, worst first, keeping
// at most limit of them (0 keeps all). Call sites that cost nothing measurable
// are left out: a list of zeroes is not a profile.
func (t *Timing) Hotspots(limit int) []Hotspot {
	out := make([]Hotspot, 0, len(t.hotSeen))
	for _, node := range t.hotSeen {
		if h := t.hot[node]; h.Self > 0 {
			out = append(out, *h)
		}
	}
	// Stable, over a slice already in first-seen order, so equal costs stay in
	// pipeline order rather than shuffling between runs.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Self > out[j].Self })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Hottest is the single row with the most self time — where a reader should
// look first, and what the stepper's jump key goes to. It is a row rather than
// a call site because the answer has to be somewhere the cursor can land.
func (t *Timing) Hottest() *TraceNode { return t.hottest }

// Overall is the denominator: the summed cost of the recording's top-level
// rows. It is the run's own work, not the command's wall clock — front-ending
// the program and building the UI are not the program's time.
func (t *Timing) Overall() time.Duration { return t.overall }

// Of reports a row's cost. An unrecorded node reports a zero profile rather
// than panicking, so a renderer never has to ask twice.
func (t *Timing) Of(n *TraceNode) NodeTiming { return t.nodes[n] }

// FormatDuration renders a duration at a readable precision: three significant
// figures is as much as a tree-walking interpreter's timings deserve.
func FormatDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0"
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1e3)
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
}

// FormatPercent renders a share in at most six columns. A step that ran but
// rounds to nothing reads `<0.1%` rather than `0.0%`, because "too fast to
// measure" and "did not run" are different answers.
func FormatPercent(pct float64) string {
	switch {
	case pct <= 0:
		return "0%"
	case pct < 0.1:
		return "<0.1%"
	default:
		return fmt.Sprintf("%.1f%%", pct)
	}
}
