// `:stats` — where the session's time goes.
//
// `domain run --stats` prints the same numbers as a table on stderr. In the
// REPL the question is usually "which stage did I just make expensive", asked
// repeatedly while building a pipeline, and a column of digits answers that
// slowly: you have to read every row before you know which one matters. So the
// same rows are drawn as bars, colored by the visualizer's heat ramp — the
// hot stage is the one you see first, before reading anything.
//
// The profile is a real replay under interp's aggregating tracer (interp's
// Stats), so it costs one extra run of the program and measures the
// tree-walking interpreter — not the compiled binary, which the header says
// out loud for the same reason `--stats` does.
package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"domain/interp"
	"domain/ir"
)

// stats replays the current program under the profiler and charts the result.
func (r *repl) stats() {
	if len(r.stmts) == 0 {
		fmt.Fprintln(r.out, "(empty domain)")
		return
	}
	pipe, src, err := r.frontEnd(r.stmts)
	if err != nil {
		r.reportError(src, err)
		return
	}
	if len(pipe.Nodes) == 0 {
		fmt.Fprintln(r.out, "(no value yet)")
		return
	}

	prof := interp.NewStats()
	ctx := r.context()
	// Keep the session's own tracers in the chain, so a profiling run of a
	// runaway loop can still be interrupted and still reports its progress.
	if it, ok := r.trace.(*ir.Interrupter); ok && it != nil {
		if counter, ok := it.Inner.(*progressCounter); ok {
			counter.Inner = prof
			defer func() { counter.Inner = nil }()
		} else {
			it.Inner = prof
			defer func() { it.Inner = nil }()
		}
	} else {
		ctx.Trace = prof
	}

	if _, err := interp.Run(pipe, ctx); err != nil {
		// A failed run still profiled everything up to the failure, which is
		// often exactly the run worth looking at.
		fmt.Fprintf(r.out, "runtime error: %v (profile covers the run up to it)\n", err)
	}
	// Kept so a pager over this profile can re-order it without re-running the
	// program: the same measurements, read worst-first instead of in program
	// order (repl_pager.go).
	r.lastProfile = prof
	fmt.Fprint(r.out, renderStats(prof, r.width, r.color, false))
}

// maxChildRows bounds the per-stage detail: a loop can touch dozens of nodes,
// and the profile is read for the ones that cost something.
const maxChildRows = 5

// sortableStats returns a renderer for the profile this session last took, or
// nil when it has not taken one. The pager uses it to re-order in place.
func (r *repl) sortableStats() func(bySelf bool) string {
	prof, width, color := r.lastProfile, r.width, r.color
	if prof == nil {
		return nil
	}
	return func(bySelf bool) string { return renderStats(prof, width, color, bySelf) }
}

// renderStats draws the profile: one bar per stage, hottest color for the
// hottest stage, with the nested steps of any stage that opened frames listed
// underneath it. With bySelf set the stages are ordered by cost rather than by
// their place in the program — the question a profile is read for second.
func renderStats(prof *interp.Stats, width int, color bool, bySelf bool) string {
	rows := prof.Rows()
	if bySelf {
		rows = slices.Clone(rows)
		slices.SortStableFunc(rows, func(a, b interp.StageRow) int { return cmp.Compare(b.Dur, a.Dur) })
	}
	total := prof.Total()
	head := fmt.Sprintf("[stats] %d stage(s) · %s total · tree-walking interpreter, not the compiled binary",
		len(rows), interp.FormatDuration(total))

	var b strings.Builder
	b.WriteString(paintIf(color, styDim, head) + "\n")
	if len(rows) == 0 {
		return b.String()
	}

	if width <= 0 {
		width = 100
	}
	bar := 24
	if width < 90 {
		bar = 12
	}
	// The name column takes what the fixed columns leave: index, timing,
	// percentage, bar, and the spaces between them.
	name := width - (bar + 26)
	name = max(min(name, 46), 14)

	for i, row := range rows {
		label := row.Name
		if row.Frames > 0 {
			label = fmt.Sprintf("%s (%d frames, %d steps)", label, row.Frames, row.Nested)
		}
		style := heat(row.Pct, true)
		if row.Failed {
			style = styErr
		}
		fmt.Fprintf(&b, "  %s %s %s %s %s\n",
			paintIf(color, styDim, fmt.Sprintf("%3d", i+1)),
			pad(truncateVis(label, name), name),
			paintIf(color, style, fmt.Sprintf("%9s", interp.FormatDuration(row.Dur))),
			paintIf(color, style, fmt.Sprintf("%5.1f%%", row.Pct)),
			paintIf(color, style, bars(row.Pct, bar)))

		for _, child := range hottest(row.Children) {
			fmt.Fprintf(&b, "      %s %s %s\n",
				pad(truncateVis("↳ "+child.Name, name), name),
				paintIf(color, styDim, fmt.Sprintf("%9s", interp.FormatDuration(child.Dur))),
				paintIf(color, styDim, fmt.Sprintf("×%d", child.Calls)))
		}
	}
	return b.String()
}

// hottest orders a stage's nested steps by cost and keeps the top few.
func hottest(children []interp.StageRow) []interp.StageRow {
	if len(children) == 0 {
		return nil
	}
	sorted := slices.Clone(children)
	slices.SortStableFunc(sorted, func(a, b interp.StageRow) int { return cmp.Compare(b.Dur, a.Dur) })
	if len(sorted) > maxChildRows {
		sorted = sorted[:maxChildRows]
	}
	return sorted
}

// bars draws a share of the run as a bar of w cells. A stage that took a
// non-zero but tiny share still gets one cell (see barCells), so "ran" and
// "did not run" never look alike.
func bars(pct float64, w int) string {
	filled := barCells(pct, w)
	return strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
}
