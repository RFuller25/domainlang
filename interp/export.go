package interp

import "domain/ir"

// A serializable form of a recording, for `domain expansion: visualize --json`.
//
// The stepper and the plain printer both render the same tree; this is that
// tree as data, so a CI job can assert that a stage stayed under its share of
// the run, or a tool that is not a terminal can read what happened. It is a
// deliberate schema rather than struct tags scattered over the recorder's
// internals: Step and TraceNode are shaped for the UI's convenience and would
// make a poor wire format, and pinning them as one would freeze both.
//
// Everything here is derived from a finished recording, so building it costs
// nothing during the run.

// Recording is a whole captured run.
type Recording struct {
	Steps    int           `json:"steps"`
	Capped   bool          `json:"capped"`
	Total    string        `json:"total"`
	TotalNs  int64         `json:"total_ns"`
	Rows     []Row         `json:"rows"`
	Hotspots []HotspotJSON `json:"hotspots"`
}

// Row is one node of the recorded tree: a step, or a frame holding steps.
type Row struct {
	Kind  string `json:"kind"` // "step" or "frame"
	Label string `json:"label"`

	// Step-only fields.
	Prim string `json:"prim,omitempty"`
	Type string `json:"type,omitempty"`
	Line int    `json:"line,omitempty"` // in the program file; 0 when unknown
	From string `json:"from,omitempty"` // the other source Line belongs to
	Size *int   `json:"size,omitempty"` // absent where a size is meaningless
	In   string `json:"in,omitempty"`   // short rendering of the input
	Out  string `json:"out,omitempty"`  // short rendering of the output
	Err  string `json:"error,omitempty"`

	// What the row's own body produced, on a row that has one: a Channel or
	// Part's block value, or one lap of a loop. Those stages hand their input
	// back to the pipeline, so Out and Result are different answers and a
	// reader asking what the block computed wants this one.
	Result     string `json:"result,omitempty"`
	ResultType string `json:"result_type,omitempty"`
	ResultSize *int   `json:"result_size,omitempty"`

	// Folded marks the synthetic row standing in for a run of loop laps. Its
	// children are those laps, unabridged.
	Folded bool `json:"folded,omitempty"`

	TimeNs  int64   `json:"time_ns"`
	Time    string  `json:"time"`
	Pct     float64 `json:"pct"`
	SelfNs  int64   `json:"self_ns"`
	SelfPct float64 `json:"self_pct"`

	Children []Row `json:"children,omitempty"`
}

// HotspotJSON is one ranked call site.
type HotspotJSON struct {
	Name    string  `json:"name"`
	Prim    string  `json:"prim"`
	Line    int     `json:"line,omitempty"`
	From    string  `json:"from,omitempty"`
	Calls   int     `json:"calls"`
	Self    string  `json:"self"`
	SelfNs  int64   `json:"self_ns"`
	SelfPct float64 `json:"self_pct"`
	Failed  bool    `json:"failed,omitempty"`
}

// Export renders the recording as data. Percentages are rounded to a tenth, the
// same figure the UI shows, so a report and a test of that report cannot
// disagree about what a step cost.
func (r *Recorder) Export() Recording {
	t := r.Timing()
	rec := Recording{
		Steps:   r.steps,
		Capped:  r.truncated,
		Total:   FormatDuration(t.Overall()),
		TotalNs: t.Overall().Nanoseconds(),
		Rows:    exportRows(r.Roots(), t),
	}
	for _, h := range t.Hotspots(0) {
		line, from := sourceOf(h.Node)
		rec.Hotspots = append(rec.Hotspots, HotspotJSON{
			Name: h.Name, Prim: h.Node.Prim, Line: line, From: from,
			Calls: h.Calls, Self: FormatDuration(h.Self),
			SelfNs: h.Self.Nanoseconds(), SelfPct: round1(h.SelfPct),
			Failed: h.Failed,
		})
	}
	return rec
}

func exportRows(nodes []*TraceNode, t *Timing) []Row {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]Row, 0, len(nodes))
	for _, n := range nodes {
		nt := t.Of(n)
		row := Row{
			Kind: "frame", Label: n.Label(), Folded: n.Folded,
			TimeNs: nt.Total.Nanoseconds(), Time: FormatDuration(nt.Total),
			Pct: round1(nt.TotalPct), SelfNs: nt.Self.Nanoseconds(), SelfPct: round1(nt.SelfPct),
			Children: exportRows(n.Children, t),
		}
		if b := n.Block; b != nil {
			row.Result, row.ResultType = b.Short, b.Type
			if b.SizeOK {
				size := b.Size
				row.ResultSize = &size
			}
		}
		if !n.IsFrame() {
			s := n.Step
			row.Kind, row.Prim, row.In, row.Out = "step", s.Node.Prim, s.InShort, s.Short
			row.Line, row.From = sourceOf(s.Node)
			if s.Node.Out != nil {
				row.Type = s.Node.Out.String()
			}
			if s.SizeOK {
				size := s.Size
				row.Size = &size
			}
			if s.Err != nil {
				row.Err = s.Err.Error()
			}
		}
		out = append(out, row)
	}
	return out
}

// sourceOf reports the line a node came from, and the source that line belongs
// to when it is not the program file (see ir.MetaForeign).
func sourceOf(n *ir.Node) (line int, from string) {
	if n == nil {
		return 0, ""
	}
	where, foreign := n.Foreign()
	if foreign {
		return n.Pos.Line, where
	}
	return n.Pos.Line, ""
}

// round1 matches the displayed precision, so a percentage read out of the JSON
// is the one the UI showed.
func round1(pct float64) float64 {
	return float64(int(pct*10+0.5)) / 10
}
