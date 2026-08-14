package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"domain/interp"
	"domain/ir"
)

// The inside of a foreign stage.
//
// Every other stage in a Domain program is Domain the whole way down, and the
// `x` pane answers "what did this stage actually compute" by breaking its
// `Using:` expression into subexpressions and their values. A foreign stage has
// no expression: its inside is another language's program and the bytes that
// crossed to and from it. That is still the question `x` asks, so it is still
// what `x` answers — with the block's source and its wire traffic, rather than
// with a dead end saying the stage has no expression.
//
// The wire traffic is the part worth the plumbing. A foreign stage is the one
// place a Domain value stops being a value and becomes bytes, and that is where
// its mistakes live: the trailing newline that was or was not there, the grid
// whose rows were not what the block expected, the empty list that arrived as
// no input at all. A reader shown only the Domain value on each side can see
// that the answer is wrong and never see why.

// foreignOf returns the recorded execution for the selected step, when it is a
// foreign stage that ran.
func foreignOf(s *interp.Step) (*interp.ForeignExec, bool) {
	if s == nil || s.Node == nil || s.Node.Prim != "Foreign Block" {
		return nil, false
	}
	return s.Foreign, s.Foreign != nil
}

// foreignSource is the block's source text, which is on the node itself and so
// is available whether or not the stage ever ran.
func foreignSource(s *interp.Step) (lang, src string) {
	if s == nil || s.Node == nil || s.Node.Meta == nil {
		return "", ""
	}
	lang, _ = s.Node.Meta["lang"].(string)
	src, _ = s.Node.Meta["source"].(string)
	return lang, src
}

// foreignLines renders the inside of a foreign stage: what ran, what it was
// given, and what it said.
func (m *visualModel) foreignLines(w int) []string {
	s := m.selected()
	lang, src := foreignSource(s)
	out := []string{
		styHeading.Render(strings.ToLower(lang) + " block"),
		styDim.Render("the program, and the bytes that crossed to and from it"),
		"",
	}

	exec, ran := foreignOf(s)
	if !ran {
		// The stage is in the program but this recording never reached it —
		// the run failed earlier, or the capture bound was hit.
		out = append(out, styDim.Render("  this stage did not run in the recording"), "")
	} else {
		out = append(out, field("ran", styValue.Render(truncateVisLeft(shortCommand(exec.Run.Command), w-8))))
		took := interp.FormatDuration(exec.Run.Dur)
		if exec.Count > 1 {
			// A block inside a Map Each body runs once per element, so the
			// reader has to be told which one they are looking at — and on a
			// stage that failed it is not the first: the failing run displaces
			// it, because that is the run they came here for.
			if exec.Run.Err != nil {
				took += fmt.Sprintf("  · run %d of %d — the one that failed", exec.Index, exec.Count)
			} else {
				took += fmt.Sprintf("  · the first of %d runs", exec.Count)
			}
		}
		out = append(out, field("took", styValue.Render(took)))
		out = append(out, "")
	}

	out = append(out, styHeading.Render("source"))
	out = append(out, codeBlock(src, w)...)

	if !ran {
		return out
	}
	out = append(out, "")
	out = append(out, streamLines("stdin", exec.Run.Stdin, w)...)
	out = append(out, streamLines("stdout", exec.Run.Stdout, w)...)
	if exec.Run.Stderr.Bytes > 0 {
		out = append(out, streamLines("stderr", exec.Run.Stderr, w)...)
	}
	return out
}

// shortCommand drops the throwaway directory from the command line. The
// directory is deleted the moment the block finishes, so its name identifies
// nothing a reader can go and look at, while the binary that ran — which
// python3, from PATH or from DOMAIN_PYTHON — is the whole reason this line is
// here, and is what the full path pushes off the edge.
//
// It says nothing about the interpreter's own path, which can be long enough
// to fill the line by itself (`/run/current-system/sw/bin/python3` on NixOS).
// Keeping the name visible there is truncateVisLeft's job, not this one's: the
// path is real and worth showing, it just cannot all fit.
func shortCommand(cmd string) string {
	parts := strings.Fields(cmd)
	for i, part := range parts {
		if strings.Contains(part, "domain-foreign-") {
			parts[i] = filepath.Base(part)
		}
	}
	return strings.Join(parts, " ")
}

// streamLines renders one captured stream under a heading that says how much of
// it there was — the byte count being half the answer to a wire-format
// question, and the only way to tell an empty stream from a missing one.
func streamLines(name string, c ir.Capture, w int) []string {
	head := fmt.Sprintf("%s · %s", name, plural(c.Bytes, "byte"))
	if c.Bytes == 0 {
		return []string{styHeading.Render(head), styDim.Render("  (nothing)"), ""}
	}
	// A stream the recording had no budget left for is not an empty stream, and
	// saying "(empty)" for it would be the one wrong answer.
	if c.Text == "" {
		return []string{styHeading.Render(head),
			styDim.Render("  (not captured — the recording's budget was spent)"), ""}
	}
	out := []string{styHeading.Render(head)}
	out = append(out, codeBlock(c.Text, w)...)
	if c.Truncated() {
		out = append(out, styDim.Render(fmt.Sprintf("  … (%s captured)", plural(len(c.Text), "byte"))))
	}
	return append(out, "")
}

// codeBlock renders text that is not Domain — foreign source, or bytes off a
// pipe — with its own line structure kept and its whitespace made visible at
// the ends, where a wire-format mistake hides.
func codeBlock(text string, w int) []string {
	if text == "" {
		return []string{styDim.Render("  (empty)")}
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		out = append(out, "  "+styLabel.Render(truncateVis(showEnds(line), w-2)))
	}
	// Say so when the last line had no newline: for a stream that is the
	// difference between a value the next stage can read and one it cannot.
	if !strings.HasSuffix(text, "\n") {
		out = append(out, styDim.Render("  (no trailing newline)"))
	}
	return out
}

// showEnds makes trailing whitespace visible, since that is exactly the kind of
// difference a reader is here to find and the kind a terminal hides.
func showEnds(line string) string {
	trimmed := strings.TrimRight(line, " \t")
	if trimmed == line {
		return line
	}
	return trimmed + strings.Repeat("·", len(line)-len(trimmed))
}

// ---------------------------------------------------------------------------
// The text and data forms
// ---------------------------------------------------------------------------

// maxForeignReported bounds the text report. --json is not bounded: a tool
// asking for the data wants all of it, and the recording's own budgets already
// bound how much there can be.
const maxForeignReported = 20

// foreignJSON is one foreign stage as --json reports it.
type foreignJSON struct {
	Step    int     `json:"step"`
	Lang    string  `json:"lang"`
	Line    int     `json:"line,omitempty"`
	Source  string  `json:"source"`
	Command string  `json:"command,omitempty"`
	Millis  float64 `json:"millis,omitempty"`
	// Run is which of the step's executions is reported here — the first,
	// unless a later one failed and displaced it — and Runs how many there
	// were. See interp.ForeignExec.
	Run    int    `json:"run,omitempty"`
	Runs   int    `json:"runs,omitempty"`
	Stdin  string `json:"stdin,omitempty"`
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	Error  string `json:"error,omitempty"`
}

// foreignSteps is every foreign stage in the recording, in order.
func (v *traceView) foreignSteps() []*interp.Step {
	var out []*interp.Step
	var walk func(nodes []*interp.TraceNode)
	walk = func(nodes []*interp.TraceNode) {
		for _, n := range nodes {
			if !n.IsFrame() && n.Step.Node != nil && n.Step.Node.Prim == "Foreign Block" {
				out = append(out, n.Step)
			}
			walk(n.Children)
		}
	}
	walk(v.rec.Roots())
	return out
}

// foreignDocs is the foreign half of the --expressions document: the same
// question ("what did this stage do inside?") for the stages that answer it
// with a program rather than with an expression.
func (v *traceView) foreignDocs() []foreignJSON {
	var out []foreignJSON
	for _, s := range v.foreignSteps() {
		lang, src := foreignSource(s)
		doc := foreignJSON{Step: s.Index, Lang: lang, Source: src}
		// Left out for a node inlined from another file, for the reason
		// traceView.where explains.
		if _, elsewhere := s.Node.Foreign(); !elsewhere {
			doc.Line = s.Node.Pos.Line
		}
		if exec, ran := foreignOf(s); ran {
			doc.Command = exec.Run.Command
			doc.Millis = float64(exec.Run.Dur.Microseconds()) / 1000
			doc.Run, doc.Runs = exec.Index, exec.Count
			doc.Stdin, doc.Stdout, doc.Stderr =
				exec.Run.Stdin.Text, exec.Run.Stdout.Text, exec.Run.Stderr.Text
			if exec.Run.Err != nil {
				doc.Error = exec.Run.Err.Error()
			}
		}
		out = append(out, doc)
	}
	return out
}

// writeForeign prints the foreign stages under --expressions, in the same shape
// as the expression breakdowns beside them.
func (v *traceView) writeForeign(w io.Writer) {
	docs := v.foreignDocs()
	if len(docs) == 0 {
		return
	}
	fmt.Fprintf(w, "\nforeign blocks:\n")
	fmt.Fprintf(w, "  the program each one ran, and the bytes that crossed to and from it\n")
	for i, d := range docs {
		// A block inside a `Map Each` body runs once per element, and each run
		// is its own step. The first few are representative; the rest would be
		// the whole input again, transposed.
		if i >= maxForeignReported {
			fmt.Fprintf(w, "\n  … %d more runs (--json has them all)\n", len(docs)-i)
			break
		}
		where := ""
		if d.Line > 0 {
			where = fmt.Sprintf(" · line %d", d.Line)
		}
		fmt.Fprintf(w, "\n  %s block%s\n", d.Lang, where)
		if d.Command != "" {
			fmt.Fprintf(w, "    ran %s\n", shortCommand(d.Command))
			if d.Runs > 1 {
				which := "the first is shown"
				if d.Error != "" {
					// Not the first: a failing run displaces it, because that
					// is the one the reader is here for.
					which = fmt.Sprintf("run %d is shown — the one that failed", d.Run)
				}
				fmt.Fprintf(w, "    %d runs — %s\n", d.Runs, which)
			}
		} else {
			fmt.Fprintf(w, "    this stage did not run in the recording\n")
		}
		writeIndented(w, "source", d.Source)
		if d.Command == "" {
			continue
		}
		writeIndented(w, "stdin", d.Stdin)
		writeIndented(w, "stdout", d.Stdout)
		if d.Stderr != "" {
			writeIndented(w, "stderr", d.Stderr)
		}
	}
}

// writeIndented prints a labelled block of foreign text, keeping its own line
// structure and its trailing whitespace visible.
func writeIndented(w io.Writer, label, text string) {
	if text == "" {
		fmt.Fprintf(w, "    %s: (empty)\n", label)
		return
	}
	fmt.Fprintf(w, "    %s:\n", label)
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		fmt.Fprintf(w, "      %s\n", showEnds(line))
	}
	if !strings.HasSuffix(text, "\n") {
		fmt.Fprintf(w, "      (no trailing newline)\n")
	}
}
