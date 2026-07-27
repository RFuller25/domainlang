// `domain expansion: visualize <file>` — step through a run and watch the data
// change shape.
//
// The program is resolved, optimized and run **once** under interp's recording
// tracer; the UI then navigates the recorded tree. Running to completion first
// keeps the model pure and the UI responsive, and it means a program that fails
// mid-run is still explorable up to the failure — with the failing step and its
// error in place, which is what makes this a debugger rather than a demo.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"domain/interp"
	"domain/ir"
	"domain/optimizer"
)

// visualizeOptions are the parsed `visualize` arguments.
type visualizeOptions struct {
	Input    string // --input FILE: program input, since the terminal cannot be stdin
	MaxSteps int    // --max-steps N: capture bound (0 = the recorder's default)
	Plain    bool   // --plain: print the trace as text instead of opening the UI
	JSON     bool   // --json: print the recording as data instead
	Optimize bool
}

// parseVisualizeArgs parses `domain expansion: visualize` arguments.
func parseVisualizeArgs(args []string) (string, visualizeOptions, error) {
	opts := visualizeOptions{Optimize: true}
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--plain":
			opts.Plain = true
		case a == "--json":
			opts.JSON = true
		case a == "--no-optimize":
			opts.Optimize = false
		case a == "--input" || a == "-i":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("%s needs a file", a)
			}
			i++
			opts.Input = args[i]
		case strings.HasPrefix(a, "--input="):
			opts.Input = strings.TrimPrefix(a, "--input=")
		case a == "--max-steps":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--max-steps needs a number")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return "", opts, fmt.Errorf("--max-steps needs a positive number, got %q", args[i])
			}
			opts.MaxSteps = n
		case strings.HasPrefix(a, "--max-steps="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--max-steps="))
			if err != nil || n <= 0 {
				return "", opts, fmt.Errorf("--max-steps needs a positive number")
			}
			opts.MaxSteps = n
		case strings.HasPrefix(a, "-"):
			return "", opts, fmt.Errorf("unknown flag %q for visualize", a)
		default:
			if path != "" {
				return "", opts, fmt.Errorf("visualize takes one program file")
			}
			path = a
		}
	}
	if path == "" {
		return "", opts, fmt.Errorf("visualize needs a program file")
	}
	return path, opts, nil
}

// Visualize records a run of the program and hands it to the stepper.
func Visualize(path string, opts visualizeOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	pipe, rewrites, err := loadForVisualize(path, opts.Optimize)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}

	input, err := programInput(path, opts, stdin, pipe)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 2
	}

	rec := interp.NewRecorder(opts.MaxSteps)
	// The program's own Reveal output is captured rather than printed: the point
	// of the visualizer is the trace, and a raw-mode terminal cannot take
	// interleaved writes anyway.
	var revealed strings.Builder
	ctx := &ir.Context{
		Stdin:   strings.NewReader(input),
		Stdout:  &revealed,
		BaseDir: filepath.Dir(path),
		Trace:   rec,
	}
	_, runErr := interp.Run(pipe, ctx)

	view := &traceView{
		path:     path,
		rec:      rec,
		rewrites: rewrites,
		revealed: strings.TrimRight(revealed.String(), "\n"),
		runErr:   runErr,
	}

	// --json is asked for explicitly, so it wins over both other forms.
	if opts.JSON {
		if err := view.writeJSON(stdout); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		return 0
	}
	// Without a terminal there is nothing to drive, so print the trace instead.
	// That is also what makes the command testable.
	if opts.Plain || !isColorTerminal(stdout) {
		view.writePlain(stdout)
		return 0
	}
	return runVisualizeTUI(view, stdin, stdout, stderr)
}

// loadForVisualize front-ends the program and returns the optimized pipeline
// plus the rewrites the optimizer applied, for the explain pane.
func loadForVisualize(path string, optimize bool) (*ir.Pipeline, []optimizer.Rewrite, error) {
	// loadPipeline reports rewrites to a writer; capture them by re-running
	// Optimize here instead, so the messages can be shown in the UI.
	pipe, err := loadPipeline(path, false, false, io.Discard)
	if err != nil {
		return nil, nil, err
	}
	rewrites := optimizer.Optimize(pipe, optimize)
	return pipe, rewrites, nil
}

// programInput decides what the program reads.
//
// An interactive terminal cannot double as program stdin — the same constraint
// the REPL documents — so the input comes from --input, or from a non-terminal
// stdin read in full before the UI starts, or from the program's own
// `Cursed Energy:` file target (which the interpreter resolves itself, so an
// empty string is the right answer).
func programInput(path string, opts visualizeOptions, stdin io.Reader, pipe *ir.Pipeline) (string, error) {
	if opts.Input != "" {
		b, err := os.ReadFile(opts.Input)
		if err != nil {
			return "", fmt.Errorf("reading --input: %w", err)
		}
		return string(b), nil
	}
	// A non-terminal stdin is real piped input: read it before the UI starts.
	// An interactive terminal is not readable as program input at all, which is
	// the constraint this whole function exists to work around.
	if !isTerminalReader(stdin) {
		if b, err := io.ReadAll(stdin); err == nil && len(b) > 0 {
			return string(b), nil
		}
	}
	// Otherwise the program must name its own input file, which the interpreter
	// will resolve on its own — so an empty string is the right answer.
	if namesReadableSource(path, pipe) {
		return "", nil
	}
	return "", fmt.Errorf("visualize needs the program's input: pass --input <file>, " +
		"pipe it in, or give the program a `Cursed Energy:` file target " +
		"(an interactive terminal cannot also be the program's stdin)")
}

// isTerminalReader reports whether r is an interactive terminal, which cannot
// double as the program's stdin.
func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// namesReadableSource reports whether the program's source stage names a file
// that exists, in which case the interpreter will find it on its own.
//
// The question is asked of the *resolved pipeline* rather than the source text,
// because the source line is not reliably findable by reading: the keyword is
// optional (`input.txt` on its own is a source), declarations like
// `Innate Domain:` and `Shikigami` sit above it, and comments and blank lines
// intervene. The Read Source node carries the target it will actually open.
func namesReadableSource(path string, pipe *ir.Pipeline) bool {
	if pipe == nil {
		return false
	}
	for _, n := range pipe.Nodes {
		if n.Prim != "Read Source" {
			continue
		}
		target, _ := n.Meta["target"].(string)
		if target == "" || target == "stdin" {
			return false
		}
		if filepath.IsAbs(target) {
			_, err := os.Stat(target)
			return err == nil
		}
		_, err := os.Stat(filepath.Join(filepath.Dir(path), target))
		return err == nil
	}
	return false
}

// traceView is everything the UI (or the plain printer) needs.
type traceView struct {
	path     string
	rec      *interp.Recorder
	rewrites []optimizer.Rewrite
	revealed string
	runErr   error

	// Derived on first use, so that every way of building a traceView — the
	// command, and the tests that build one directly — gets the same answers
	// without having to remember to ask for them.
	timing  *interp.Timing
	srcOnce bool
	src     []string
	shares  map[int]float64
}

// times returns the recording's timing profile, computing it once.
func (v *traceView) times() *interp.Timing {
	if v.timing == nil {
		v.timing = v.rec.Timing()
	}
	return v.timing
}

// source returns the program's lines, read once. An unreadable file is not an
// error worth failing the command over — the source pane says so instead, and
// everything else in the UI still works.
func (v *traceView) source() []string {
	if !v.srcOnce {
		v.srcOnce = true
		if b, err := os.ReadFile(v.path); err == nil {
			v.src = strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		}
	}
	return v.src
}

// where names the source a step came from: a line of the program, or the other
// file its position belongs to.
//
// Inlining is why this is not simply `line N`. A Shikigami call is replaced by
// its body, and the body's nodes keep the *definition's* positions — which, for
// a prelude or imported definition, are coordinates in a file the user is not
// looking at. Pointing confidently at that line number in their program would
// be worse than saying nothing, so ir.MetaForeign marks those nodes and they
// report where they actually came from.
func (v *traceView) where(n *ir.Node) string {
	if n == nil {
		return ""
	}
	if from, foreign := n.Foreign(); foreign {
		return fmt.Sprintf("inlined from %s", from)
	}
	if n.Pos.Line <= 0 {
		return ""
	}
	return fmt.Sprintf("line %d", n.Pos.Line)
}

// lineShares maps each line of the program to the share of the run spent in the
// steps on it — the profile projected back onto the text that produced it.
// Inlined foreign nodes are left out for the reason where() explains.
func (v *traceView) lineShares() map[int]float64 {
	if v.shares != nil {
		return v.shares
	}
	v.shares = map[int]float64{}
	for _, h := range v.times().Hotspots(0) {
		if _, foreign := h.Node.Foreign(); foreign || h.Node.Pos.Line <= 0 {
			continue
		}
		v.shares[h.Node.Pos.Line] += h.SelfPct
	}
	return v.shares
}

// visualizeJSON is the `--json` document: the recording as data, plus what the
// command knows that the recorder does not.
type visualizeJSON struct {
	Program   string   `json:"program"`
	Failed    string   `json:"failed,omitempty"`
	Optimizer []string `json:"optimizer,omitempty"`
	Revealed  string   `json:"revealed,omitempty"`

	// Embedded, so the recording's own fields sit at the top level of the
	// document rather than under a wrapper nobody would want to type.
	interp.Recording
}

// writeJSON prints the recording as JSON, for a reader that is not a terminal:
// a CI job asserting a stage stayed under its share of the run, or a tool that
// wants the trace without parsing a table.
func (v *traceView) writeJSON(w io.Writer) error {
	doc := visualizeJSON{
		Program:   v.path,
		Revealed:  v.revealed,
		Recording: v.rec.Export(),
	}
	if v.runErr != nil {
		doc.Failed = v.runErr.Error()
	}
	for _, r := range v.rewrites {
		doc.Optimizer = append(doc.Optimizer, r.Message)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// header is the one-line description of the recording: what was run, how much
// of it was kept, and the denominator every percentage below is a share of.
func (v *traceView) header() string {
	return fmt.Sprintf("%s — %s · %s total", v.path, v.rec.Summary(),
		interp.FormatDuration(v.times().Overall()))
}

// writePlain prints the recorded trace as indented text. This is the no-terminal
// form of the same information the stepper shows.
func (v *traceView) writePlain(w io.Writer) {
	t := v.times()
	fmt.Fprintln(w, v.header())
	if v.runErr != nil {
		fmt.Fprintf(w, "run failed: %v\n", v.runErr)
	}
	fmt.Fprintln(w, "% is the step's share of the run; self% excludes the work of its nested frames.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s %6s %9s %7s %7s\n",
		col("step", 40), col("out type", 22), "size", "time", "%", "self%")

	var walk func(nodes []*interp.TraceNode, depth int)
	walk = func(nodes []*interp.TraceNode, depth int) {
		for _, n := range nodes {
			indent := strings.Repeat("  ", depth)
			nt := t.Of(n)
			// Pad by runes and against fixed columns, so nesting and multi-byte
			// type names keep the columns straight. A frame is not a step: it
			// has no value or type of its own, only the cost of its contents.
			label, outType, size := indent+n.Frame, "", ""
			if !n.IsFrame() {
				label, outType, size = indent+n.Label(), typeOf(n.Step), sizeOf(n.Step)
			}
			fmt.Fprintf(w, "%s %s %6s %9s %7s %7s\n",
				col(label, 40), col(outType, 22), size,
				interp.FormatDuration(nt.Total), pctText(nt.TotalPct, nt.Known),
				selfPctText(nt))
			if !n.IsFrame() && n.Step.Err != nil {
				fmt.Fprintf(w, "%serror: %v\n", indent+"  ", n.Step.Err)
			}
			walk(n.Children, depth+1)
		}
	}
	walk(v.rec.Roots(), 0)

	if len(v.rewrites) > 0 {
		fmt.Fprintln(w, "\noptimizer:")
		for _, r := range v.rewrites {
			fmt.Fprintf(w, "  %s\n", r.Message)
		}
	}
	if v.revealed != "" {
		fmt.Fprintf(w, "\nrevealed:\n%s\n", v.revealed)
	}
}

// col renders a table cell w columns wide, counting runes so a multi-byte type
// name does not shift the columns. Unlike pad it never clips: a label longer
// than its column pushes the rest of *its own* row right rather than losing
// characters. The plain output is meant to be read and grepped, where a clipped
// `Channel "…"` label costs more than a ragged row does.
func col(s string, w int) string {
	if n := len([]rune(s)); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// pctText renders a share, or an em dash when there is no denominator to be a
// share of — a run too fast for the clock to resolve at all.
func pctText(pct float64, known bool) string {
	if !known {
		return "—"
	}
	return interp.FormatPercent(pct)
}

// selfPctText renders the self share only for a row that has nested work. On a
// leaf step self and total are the same number, and printing it twice would be
// noise in the column that matters most.
func selfPctText(nt interp.NodeTiming) string {
	if !nt.Nested {
		return ""
	}
	return pctText(nt.SelfPct, nt.Known)
}

func typeOf(s *interp.Step) string {
	if s.Node.Out == nil {
		return ""
	}
	return s.Node.Out.String()
}

func sizeOf(s *interp.Step) string {
	if !s.SizeOK {
		return "—"
	}
	return strconv.Itoa(s.Size)
}
