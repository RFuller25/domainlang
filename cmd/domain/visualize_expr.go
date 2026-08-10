// The expression breakdown: what a step's `Using:` expression computed, one
// subexpression at a time.
//
// The trace answers what each *stage* produced, and stops at the stage
// boundary: `Map Each` took a list of 200 and gave back a list of 200, and the
// arithmetic that turned each element into the next is invisible. That
// arithmetic is where the bugs are — an off-by-one inside a `min(list(...))`,
// an `if` taking the arm you did not expect — and it is exactly what a reader
// staring at a wrong number needs to see.
//
// So a step that ran a lambda opens up: every parenthesis is its own row, with
// the value it came to, nested the way the source nests. The rows are what
// *ran*, not what was written, so an `if` shows only the arm it took and a
// short-circuited `or` shows only the operand it needed — which is itself often
// the answer.
//
// The values are recomputed rather than recorded: interp keeps only the
// arguments of one application per step, and eval replays it on demand
// (eval.TraceLambda). That is what makes the detail free until it is asked for.
//
// *Which* application is kept is the recorder's decision and it is not always
// the first — on a step that failed it is the one that failed (see
// interp.Application). This file's job is to say so, because a breakdown of
// element 1 shown beside a row marked failed at element 900 is worse than no
// breakdown: it is a confident answer to a question nobody asked.
package main

import (
	"fmt"
	"io"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/format"
	"domain/interp"
	"domain/ir"
)

// plainExprWidth is the expression column of the text form. Wide enough for
// most of a hand-written subexpression, narrow enough that the value column
// stays in the same place down the section.
const plainExprWidth = 58

// exprRow is one subexpression of a breakdown.
type exprRow struct {
	depth int
	text  string // the subexpression, rendered as Domain source
	value string // what it came to; "" when it failed
	err   string
}

// exprBreakdown is a replayed application, ready to display.
type exprBreakdown struct {
	Header string // the lambda, and what it was applied to
	Note   string // why there is more to this than the rows show; "" when there is not
	Failed bool   // whether the application shown is the one that failed
	Rows   []exprRow
}

// breakdownOf replays a step's recorded application and renders it. The error
// is the reason there is nothing to show — a step that ran no expression, a
// `Using:` that is a pipeline rather than an expression — and is meant to be
// displayed in place of the rows.
func breakdownOf(s *interp.Step) (*exprBreakdown, error) {
	if s == nil {
		return nil, fmt.Errorf("a frame is a label around a sub-pipeline; it runs no expression of its own")
	}
	a := s.Apply
	if a == nil {
		return nil, fmt.Errorf("%s has no Using: expression", s.Node.Prim)
	}

	// A failing replay still has a tree, and the failure is usually the reason
	// to be looking: the error is carried on the row that raised it, and shown
	// here as well so it is not missed when the tree is long.
	root, err := eval.TraceLambda(a.Lambda, a.Types, a.Args...)
	if root == nil {
		return nil, err
	}

	b := &exprBreakdown{Header: applicationHeader(a), Failed: s.Err != nil}
	switch {
	case root.Capped:
		b.Note = "the expression was too large to replay in full — the rows below stop part way"
	case b.Failed && a.Count > 1:
		// Which application this is matters most on a failing stage, and it is
		// not the first: the recorder keeps the one that failed (see
		// interp.Application), and a reader who assumed otherwise would be
		// reading element 1's arithmetic to explain element 900's error.
		b.Note = fmt.Sprintf("application %d of %d — the one that failed", a.Index, a.Count)
	case b.Failed:
		b.Note = "the application that failed"
	case a.Count > 1:
		b.Note = fmt.Sprintf("the first of %s", plural(a.Count, "application"))
	}
	b.Rows = exprRows(root, 0, nil)
	return b, nil
}

// applicationHeader names the expression and the values it ran on, so the rows
// below have something to be values *of*.
func applicationHeader(a *interp.Application) string {
	binds := make([]string, 0, len(a.Lambda.Params))
	for i, p := range a.Lambda.Params {
		if i < len(a.Args) {
			binds = append(binds, fmt.Sprintf("%s = %s", p, ir.FormatShort(a.Args[i])))
		}
	}
	if len(binds) == 0 {
		return "(no arguments)"
	}
	return strings.Join(binds, ", ")
}

// exprRows flattens a replayed tree in source order, nested by depth.
//
// Literals and names are left out: a row saying `4` came to `4` is noise, and
// what a parameter is bound to is already in the header. What remains is every
// place the expression actually *did* something — which is one row per pair of
// parentheses, plus the operators between them.
func exprRows(n *eval.ExprNode, depth int, out []exprRow) []exprRow {
	if n == nil {
		return out
	}
	if !trivialExpr(n.Expr) {
		r := exprRow{depth: depth, text: format.Expr(n.Expr), value: ir.FormatShort(n.Value)}
		if n.Err != nil {
			r.value, r.err = "", n.Err.Error()
		}
		out = append(out, r)
		depth++
	}
	for _, c := range n.Children {
		out = exprRows(c, depth, out)
	}
	return out
}

// trivialExpr reports whether a subexpression evaluates to itself, or to
// something the header already gave — the rows that would say nothing.
func trivialExpr(e ast.Expr) bool {
	switch e.(type) {
	case *ast.IntLit, *ast.FloatLit, *ast.BoolLit, *ast.StringLit, *ast.Ident:
		return true
	}
	return false
}

// steppedExpr pairs a step with the breakdown of the expression it ran.
type steppedExpr struct {
	step *interp.Step
	b    *exprBreakdown
}

// breakdowns walks the recording and returns every step that ran a `Using:`
// expression, in trace order, with its breakdown.
//
// A step whose expression cannot be replayed — a `Using:` written as a pipeline
// — is left out rather than listed as an error: this is the whole-program view,
// and the reason belongs to whoever asked about that one step.
func (v *traceView) breakdowns() []steppedExpr {
	var out []steppedExpr
	var walk func(nodes []*interp.TraceNode)
	walk = func(nodes []*interp.TraceNode) {
		for _, n := range nodes {
			if !n.IsFrame() && n.Step.Apply != nil {
				if b, err := breakdownOf(n.Step); err == nil {
					out = append(out, steppedExpr{step: n.Step, b: b})
				}
			}
			walk(n.Children)
		}
	}
	walk(v.rec.Roots())
	return out
}

// writeExprs prints every step's expression breakdown — the text form of the
// stepper's expression pane, for a reader who is not a terminal. It is opt-in
// (`--expressions`) because it is a second trace of a different shape, and
// stapling it under a table of stages would bury the table.
func (v *traceView) writeExprs(w io.Writer) {
	entries := v.breakdowns()
	if len(entries) == 0 {
		fmt.Fprintf(w, "\nexpressions:\n  no stage in this program ran a Using: expression\n")
		return
	}
	fmt.Fprintf(w, "\nexpressions:\n")
	fmt.Fprintf(w, "  every parenthesis, and what it came to; an if shows only the arm it took\n")
	for _, e := range entries {
		where := v.where(e.step.Node)
		if where != "" {
			where = " · " + where
		}
		fmt.Fprintf(w, "\n  %s%s\n", e.step.Node.Prim, where)
		fmt.Fprintf(w, "    %s\n", e.b.Header)
		if e.b.Note != "" {
			fmt.Fprintf(w, "    %s\n", e.b.Note)
		}
		for _, r := range e.b.Rows {
			// Clipped, unlike every other column of the plain output: the
			// outermost row of a nested expression is the whole expression, and
			// letting it run would push the value — the column this section
			// exists for — off the end of the line. The full text is in the
			// program.
			label := truncateVis(strings.Repeat("  ", r.depth)+r.text, plainExprWidth)
			if r.err != "" {
				fmt.Fprintf(w, "    %s error: %s\n", col(label, plainExprWidth), r.err)
				continue
			}
			fmt.Fprintf(w, "    %s %s\n", col(label, plainExprWidth), r.value)
		}
	}
}

// exprJSON is one step's breakdown as data, for `--json --expressions`: a CI
// job asserting an expression still computes what it used to, or a tool that
// wants the arithmetic without driving a terminal.
type exprJSON struct {
	Step int    `json:"step"`
	Prim string `json:"prim"`
	Line int    `json:"line,omitempty"`
	// Which of the step's applications this is, and how many it made. On a
	// failing step it is the one that failed rather than the first, so a tool
	// reading this document is told which element the arithmetic belongs to.
	Application  int           `json:"application,omitempty"`
	Applications int           `json:"applications,omitempty"`
	Failed       bool          `json:"failed,omitempty"`
	Bound        string        `json:"bound"`
	Note         string        `json:"note,omitempty"`
	Parts        []exprRowJSON `json:"parts"`
}

type exprRowJSON struct {
	Depth int    `json:"depth"`
	Expr  string `json:"expr"`
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

// expressions is the --expressions document.
func (v *traceView) expressions() []exprJSON {
	var out []exprJSON
	for _, e := range v.breakdowns() {
		doc := exprJSON{
			Step:         e.step.Index,
			Prim:         e.step.Node.Prim,
			Application:  e.step.Apply.Index,
			Applications: e.step.Apply.Count,
			Failed:       e.b.Failed,
			Bound:        e.b.Header,
			Note:         e.b.Note,
			Parts:        make([]exprRowJSON, 0, len(e.b.Rows)),
		}
		// Left out for a node inlined from another file, for the reason
		// traceView.where explains: a confident wrong line number is worse
		// than none.
		if _, foreign := e.step.Node.Foreign(); !foreign {
			doc.Line = e.step.Node.Pos.Line
		}
		for _, r := range e.b.Rows {
			doc.Parts = append(doc.Parts, exprRowJSON{
				Depth: r.depth, Expr: r.text, Value: r.value, Error: r.err,
			})
		}
		out = append(out, doc)
	}
	return out
}
