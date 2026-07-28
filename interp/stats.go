package interp

import (
	"fmt"
	"io"
	"sort"
	"time"

	"domain/ir"
)

// `--stats`: per-stage counts and timings.
//
// This is the value-free consumer of the trace hook (ir/trace.go): it keeps
// nothing but numbers, so it can run over any input without bounding what it
// captures. Nested work — loop iterations, Channel and Part bodies — is
// attributed to the stage that encloses it, with per-node detail under
// --verbose.
//
// Attribution rests on one ordering fact: a node's own Step is reported *after*
// its Eval returns, so everything that happened inside a loop, Channel or Part
// body is reported before the enclosing stage is. Nested steps are therefore
// buffered and flushed onto the next top-level stage to report.
//
// These numbers measure the tree-walking interpreter, not a compiled binary.
// The header says so, because they are not the language's performance.
type Stats struct {
	stages []*stageStat
	index  map[*ir.Node]*stageStat
	total  time.Duration

	depth  int          // open trace frames
	frames int          // frames opened since the last flush
	buf    []nestedStep // nested steps awaiting their enclosing stage
}

// stageStat is one top-level stage's accumulated cost.
type stageStat struct {
	node     *ir.Node
	order    int
	calls    int
	dur      time.Duration
	frames   int // sub-pipeline frames entered (loop iterations, bodies)
	nested   int // node evaluations inside those frames
	outSize  int
	sizeKnwn bool
	failed   bool
	children []*nestedStat
}

// nestedStep is one buffered evaluation from inside a sub-pipeline. It keeps no
// values, only what the report needs.
type nestedStep struct {
	node   *ir.Node
	dur    time.Duration
	size   int
	sizeOK bool
	failed bool
}

// nestedStat aggregates one node's cost across every iteration it ran in.
type nestedStat struct {
	node     *ir.Node
	order    int
	calls    int
	dur      time.Duration
	outSize  int
	sizeKnwn bool
	failed   bool
}

// NewStats returns a tracer ready to install on a Context.
func NewStats() *Stats {
	return &Stats{index: map[*ir.Node]*stageStat{}}
}

// Step records one node evaluation.
func (s *Stats) Step(e ir.StepEvent) {
	size, sizeOK := ir.SizeOf(e.Out)

	if s.depth > 0 {
		s.buf = append(s.buf, nestedStep{
			node: e.Node, dur: e.Dur, size: size, sizeOK: sizeOK, failed: e.Err != nil,
		})
		return
	}

	st, ok := s.index[e.Node]
	if !ok {
		st = &stageStat{node: e.Node, order: len(s.stages)}
		s.index[e.Node] = st
		s.stages = append(s.stages, st)
	}
	st.calls++
	st.dur += e.Dur
	s.total += e.Dur
	if sizeOK {
		st.outSize, st.sizeKnwn = size, true
	}
	if e.Err != nil {
		st.failed = true
	}

	// Everything buffered happened inside this stage's own Eval.
	s.flushInto(st)
}

// flushInto attributes the buffered nested steps to their enclosing stage.
func (s *Stats) flushInto(st *stageStat) {
	if len(s.buf) == 0 && s.frames == 0 {
		return
	}
	st.frames += s.frames
	st.nested += len(s.buf)

	byNode := make(map[*ir.Node]*nestedStat, len(st.children))
	for _, c := range st.children {
		byNode[c.node] = c
	}
	for _, ns := range s.buf {
		c, ok := byNode[ns.node]
		if !ok {
			c = &nestedStat{node: ns.node, order: len(st.children)}
			byNode[ns.node] = c
			st.children = append(st.children, c)
		}
		c.calls++
		c.dur += ns.dur
		if ns.sizeOK {
			c.outSize, c.sizeKnwn = ns.size, true
		}
		if ns.failed {
			c.failed = true
		}
	}
	s.buf = s.buf[:0]
	s.frames = 0
}

// PushFrame opens a sub-pipeline.
func (s *Stats) PushFrame(string) {
	s.depth++
	s.frames++
}

// PopFrame closes the innermost sub-pipeline.
func (s *Stats) PopFrame() {
	if s.depth > 0 {
		s.depth--
	}
}

// Stages reports how many top-level stages were recorded (for tests).
func (s *Stats) Stages() int { return len(s.stages) }

// Total reports the summed duration of every top-level stage (for tests).
func (s *Stats) Total() time.Duration { return s.total }

// StageRow is one stage's accumulated cost, in the form a renderer needs.
// Report writes its own table below; Rows exists for the renderers that do not
// want a table — the REPL's `:stats` draws bars from these numbers.
type StageRow struct {
	Name      string        // the stage's Display, or its primitive id
	Type      string        // the stage's output type, "" when it has none
	Calls     int           // how many times the stage itself ran
	Frames    int           // sub-pipeline frames it opened (iterations, bodies)
	Nested    int           // node evaluations inside those frames
	Size      int           // the size of the last output value
	SizeKnown bool          // whether Size means anything (scalars have no size)
	Dur       time.Duration // inclusive: everything nested inside the stage
	Pct       float64       // Dur as a percentage of the whole run
	Failed    bool
	Children  []StageRow // per-node detail from inside this stage's frames
}

// Rows returns the recorded stages in program order, with the percentages
// Report prints already worked out.
func (s *Stats) Rows() []StageRow {
	sorted := append([]*stageStat(nil), s.stages...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].order < sorted[j].order })

	rows := make([]StageRow, 0, len(sorted))
	for _, st := range sorted {
		row := StageRow{
			Name:      stageName(st.node),
			Type:      typeName(st.node),
			Calls:     st.calls,
			Frames:    st.frames,
			Nested:    st.nested,
			Size:      st.outSize,
			SizeKnown: st.sizeKnwn,
			Dur:       st.dur,
			Pct:       s.share(st.dur),
			Failed:    st.failed,
		}
		for _, c := range st.children {
			row.Children = append(row.Children, StageRow{
				Name:      stageName(c.node),
				Type:      typeName(c.node),
				Calls:     c.calls,
				Size:      c.outSize,
				SizeKnown: c.sizeKnwn,
				Dur:       c.dur,
				Pct:       s.share(c.dur),
				Failed:    c.failed,
			})
		}
		rows = append(rows, row)
	}
	return rows
}

// share expresses a duration as a percentage of the whole recorded run.
func (s *Stats) share(d time.Duration) float64 {
	if s.total == 0 {
		return 0
	}
	return 100 * float64(d) / float64(s.total)
}

// Report writes the stats table.
func (s *Stats) Report(w io.Writer, verbose bool) {
	fmt.Fprintf(w, "[stats] interpreter, %d stages, %s total (tree-walking evaluator, not the compiled binary)\n",
		len(s.stages), FormatDuration(s.total))
	if len(s.stages) == 0 {
		return
	}
	fmt.Fprintf(w, "  %3s  %-38s %-18s %9s %9s %6s\n", "#", "stage", "out type", "size", "time", "%")

	sorted := append([]*stageStat(nil), s.stages...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].order < sorted[j].order })

	for i, st := range sorted {
		name := stageName(st.node)
		if st.frames > 0 {
			name = fmt.Sprintf("%s (%d frames, %d steps)", name, st.frames, st.nested)
		}
		pct := s.share(st.dur)
		fmt.Fprintf(w, "  %3d  %-38s %-18s %9s %9s %5.1f\n",
			i+1, truncate(name, 38), truncate(typeName(st.node), 18),
			sizeText(st.sizeKnwn, st.outSize), FormatDuration(st.dur), pct)

		if verbose {
			for _, c := range st.children {
				fmt.Fprintf(w, "       ↳ %-36s %-18s %9s %9s  ×%d\n",
					truncate(stageName(c.node), 36), truncate(typeName(c.node), 18),
					sizeText(c.sizeKnwn, c.outSize), FormatDuration(c.dur), c.calls)
			}
		}
	}
}

func stageName(n *ir.Node) string {
	if n.Display != "" {
		return n.Display
	}
	return n.Prim
}

func typeName(n *ir.Node) string {
	if n.Out == nil {
		return ""
	}
	return n.Out.String()
}

// sizeText renders a value size, or an em dash for a scalar, where "1" would be
// misleading rather than informative.
func sizeText(known bool, n int) string {
	if !known {
		return "—"
	}
	return fmt.Sprintf("%d", n)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
