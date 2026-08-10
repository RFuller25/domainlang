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
	"time"

	"domain/codegen"
	"domain/eval"
	"domain/interp"
	"domain/ir"
	"domain/optimizer"
	"domain/prims"
)

// visualizeOptions are the parsed `visualize` arguments.
type visualizeOptions struct {
	Input     string // --input FILE: program input, since the terminal cannot be stdin
	InputText string // --input-text S: the same, given inline
	MaxSteps  int    // --max-steps N: capture bound (0 = default, interp.Unlimited = all)
	Depth     int    // --depth N: how deep the text trace nests (0 = all of it)
	Plain     bool   // --plain: print the trace as text instead of opening the UI
	JSON      bool   // --json: print the recording as data instead
	Expand    bool   // --expand-loops: show every lap, rather than folding them
	Go        bool   // --go: the Go the compiler backend would emit
	Exprs     bool   // --expressions: break every Using: expression down
	Watch     bool   // --watch: re-record whenever the program or its input changes
	Optimize  bool
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
		case a == "--expand-loops":
			opts.Expand = true
		case a == "--go":
			opts.Go = true
		case a == "--expressions" || a == "--exprs":
			opts.Exprs = true
		case a == "--watch" || a == "-w":
			opts.Watch = true
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
		case a == "--input-text":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--input-text needs some text")
			}
			i++
			opts.InputText = args[i]
		case strings.HasPrefix(a, "--input-text="):
			opts.InputText = strings.TrimPrefix(a, "--input-text=")
		case a == "--max-steps":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--max-steps needs a number")
			}
			i++
			n, err := parseMaxSteps(args[i])
			if err != nil {
				return "", opts, err
			}
			opts.MaxSteps = n
		case strings.HasPrefix(a, "--max-steps="):
			n, err := parseMaxSteps(strings.TrimPrefix(a, "--max-steps="))
			if err != nil {
				return "", opts, err
			}
			opts.MaxSteps = n
		case a == "--depth":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--depth needs a number")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return "", opts, fmt.Errorf("--depth needs a positive number, got %q", args[i])
			}
			opts.Depth = n
		case strings.HasPrefix(a, "--depth="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--depth="))
			if err != nil || n <= 0 {
				return "", opts, fmt.Errorf("--depth needs a positive number")
			}
			opts.Depth = n
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
	if opts.Input != "" && opts.InputText != "" {
		return "", opts, fmt.Errorf("--input and --input-text both say what the program reads; pick one")
	}
	return path, opts, nil
}

// parseMaxSteps reads a capture bound. Zero is not a mistake to reject but the
// way to ask for the whole run: the cap exists to bound memory, and a reader
// who knows their program is long should be able to say so.
func parseMaxSteps(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("--max-steps needs a number of steps, or 0 for the whole run, got %q", s)
	}
	if n == 0 {
		return interp.Unlimited, nil
	}
	return n, nil
}

// recordSpec is everything needed to record a run: the program, what it reads,
// and the bounds. It exists as a value rather than as arguments strewn through
// Visualize because a recording is no longer made once — `r` in the stepper and
// `--watch` both make it again, and "again" has to mean exactly the same run
// (see visualize_record.go).
type recordSpec struct {
	path     string
	input    string // the program's stdin, already read
	maxSteps int
	optimize bool

	// progress, when set, is called while the run records. See
	// visualize_progress.go.
	progress func(interp.Progress)
}

// record runs the program once under the recording tracer.
//
// The program is run to completion before anything is displayed — that is the
// whole model, and it is what makes a failing run explorable — so on a long
// program this is where the time goes, and why the caller is given a progress
// hook to prove the tool has not hung.
func (spec recordSpec) record() (*traceView, error) {
	pipe, rewrites, err := loadForVisualize(spec.path, spec.optimize)
	if err != nil {
		return nil, err
	}

	rec := interp.NewRecorder(spec.maxSteps)
	rec.OnProgress(spec.progress)
	// The recorder also wants the `Using:` applications, and those happen below
	// the trace hook rather than beside it — inside a primitive, once per
	// element, where no node evaluation reports. eval is where they all pass
	// through, so that is where the recorder listens (see interp.Application).
	defer eval.WatchApplications(rec.Applied)()
	// And the foreign blocks, for the same reason and through the same kind of
	// seam: a subprocess runs under a node's Eval, so nothing at the pipeline
	// layer sees what crossed the wire (see prims.WatchForeignRuns).
	defer prims.WatchForeignRuns(rec.ForeignRan)()

	// The program's own Reveal output is captured rather than printed: the point
	// of the visualizer is the trace, and a raw-mode terminal cannot take
	// interleaved writes anyway.
	var revealed strings.Builder
	ctx := &ir.Context{
		Stdin:   strings.NewReader(spec.input),
		Stdout:  &revealed,
		BaseDir: filepath.Dir(spec.path),
		Trace:   rec,
	}
	_, runErr := interp.Run(pipe, ctx)

	return &traceView{
		path:     spec.path,
		pipe:     pipe,
		rec:      rec,
		rewrites: rewrites,
		revealed: strings.TrimRight(revealed.String(), "\n"),
		runErr:   runErr,
		recorded: time.Now(),
	}, nil
}

// Visualize records a run of the program and hands it to the stepper.
func Visualize(path string, opts visualizeOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	// Resolved once up front only to answer "where does this program's input
	// come from"; record() front-ends it again for the run itself, so a
	// re-record picks up an edited program rather than the one loaded here.
	pipe, _, err := loadForVisualize(path, opts.Optimize)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	input, err := programInput(path, opts, stdin, pipe)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 2
	}

	spec := recordSpec{path: path, input: input, maxSteps: opts.MaxSteps, optimize: opts.Optimize}
	// A long program is recorded to the end before anything is shown, so
	// without this a terminal sits blank for however long the run takes. It
	// goes to stderr, which leaves --json and --plain pipeable.
	prog := newProgressPrinter(stderr)
	spec.progress = prog.report

	view, err := spec.record()
	prog.done()
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	view.expand, view.depth = opts.Expand, opts.Depth
	// The progress line belongs to *this* recording only. A re-record inside
	// the stepper (`r`, `--watch`) happens with the alternate screen up, where
	// writing to stderr would paint over the UI; the footer says "recording…"
	// there instead.
	spec.progress = nil

	// --json is asked for explicitly, so it wins over both other forms.
	if opts.JSON {
		if err := view.writeJSON(stdout, opts.Go, opts.Exprs); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		return 0
	}
	// Without a terminal there is nothing to drive, so print the trace instead.
	// That is also what makes the command testable. --go and --expressions do
	// *not* land here any more: both have a better form inside the UI (the code
	// screen, the expression pane), and answering an interactive request for
	// them with a wall of text was the tool declining to do the thing it is
	// for. --plain is how you ask for text.
	if opts.Plain || !isColorTerminal(stdout) {
		view.writePlain(stdout)
		if opts.Exprs {
			view.writeExprs(stdout)
			// The inside of a foreign stage is its program and its wire
			// traffic, which is the same question --expressions asks of every
			// other stage (see visualize_foreign.go).
			view.writeForeign(stdout)
		}
		if opts.Go {
			view.writeGo(stdout)
		}
		return 0
	}
	return runVisualizeTUI(view, spec, opts, stdin, stdout, stderr)
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
// the REPL documents — so the input comes from --input-text, or --input, or
// from a non-terminal stdin read in full before the UI starts, or from the
// program's own `Cursed Energy:` file target (which the interpreter resolves
// itself, so an empty string is the right answer).
func programInput(path string, opts visualizeOptions, stdin io.Reader, pipe *ir.Pipeline) (string, error) {
	// Given inline, a trailing newline is almost always meant: a program that
	// splits its input on lines would otherwise see one fewer than was typed.
	if opts.InputText != "" {
		if strings.HasSuffix(opts.InputText, "\n") {
			return opts.InputText, nil
		}
		return opts.InputText + "\n", nil
	}
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
	pipe     *ir.Pipeline // the program that was run, for the emitted-Go pane
	rec      *interp.Recorder
	rewrites []optimizer.Rewrite
	revealed string
	runErr   error
	expand   bool      // print every lap of a folded loop rather than folding it
	depth    int       // how deep the text trace nests; 0 is all of it
	recorded time.Time // when this run was recorded, for the re-record status line

	// srcLines is the program's text when it did not come from a file on disk.
	// The editor records a buffer that may never have been saved, and the
	// source pane has to show the program you are looking at rather than the
	// one that happens to be on disk under the same name.
	srcLines []string

	// Derived on first use, so that every way of building a traceView — the
	// command, and the tests that build one directly — gets the same answers
	// without having to remember to ask for them.
	timing  *interp.Timing
	srcOnce bool
	src     []string
	shares  map[int]float64
	goOnce  bool
	goSrc   []string
	goSpans map[*ir.Node]codegen.Span
	goErr   error
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
		switch {
		case v.srcLines != nil:
			v.src = v.srcLines
		default:
			if b, err := os.ReadFile(v.path); err == nil {
				v.src = strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
			}
		}
	}
	return v.src
}

// emitted returns the Go the compiler backend would produce for this program,
// as lines, plus the lines each node became. It is compiled once, on demand:
// the visualizer's job is the run, and a reader who never opens the code pane
// should not pay for it.
//
// A program the backend cannot compile yet is not a failure of the visualizer —
// the interpreter ran it perfectly well — so the error is returned to be shown
// in place of the code.
func (v *traceView) emitted() ([]string, map[*ir.Node]codegen.Span, error) {
	if v.goOnce {
		return v.goSrc, v.goSpans, v.goErr
	}
	v.goOnce = true
	if v.pipe == nil {
		v.goErr = fmt.Errorf("no program to compile")
		return nil, nil, v.goErr
	}
	src, spans, err := codegen.EmitAnnotated(v.pipe, codegen.Options{})
	if err != nil {
		v.goErr = err
		return nil, nil, err
	}
	v.goSrc, v.goSpans = strings.Split(strings.TrimRight(src, "\n"), "\n"), spans
	return v.goSrc, v.goSpans, nil
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
	Go        string   `json:"go,omitempty"` // the emitted Go, with --go

	// Expressions is one entry per step that ran a `Using:` expression, with
	// --expressions. See traceView.expressions.
	Expressions []exprJSON `json:"expressions,omitempty"`

	// Foreign is the same for the stages whose inside is another language's
	// program rather than an expression: its source, and the bytes that
	// crossed to and from it. Always included when there are any — unlike an
	// expression breakdown it cannot be recovered later by replaying, so a
	// recording that left it out could not be asked again.
	Foreign []foreignJSON `json:"foreign,omitempty"`

	// Embedded, so the recording's own fields sit at the top level of the
	// document rather than under a wrapper nobody would want to type.
	interp.Recording
}

// writeJSON prints the recording as JSON, for a reader that is not a terminal:
// a CI job asserting a stage stayed under its share of the run, or a tool that
// wants the trace without parsing a table.
func (v *traceView) writeJSON(w io.Writer, withGo, withExprs bool) error {
	doc := visualizeJSON{
		Program:   v.path,
		Revealed:  v.revealed,
		Recording: v.rec.Export(),
	}
	if withExprs {
		doc.Expressions = v.expressions()
	}
	doc.Foreign = v.foreignDocs()
	if v.runErr != nil {
		doc.Failed = v.runErr.Error()
	}
	// A program the backend cannot compile reports why, in the field the
	// caller asked for: an empty "go" would look like a program that compiled
	// to nothing.
	if withGo {
		src, _, err := v.emitted()
		if err != nil {
			doc.Go = err.Error()
		} else {
			doc.Go = strings.Join(src, "\n")
		}
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
// indentedError renders an error inside the step table, where every line has
// to stay within the table's left margin.
//
// Most runtime errors are one line and this is just a prefix. A foreign block's
// is not: it carries the traceback or compile error its runtime produced, and
// those lines would otherwise break back to column zero through the middle of
// an aligned table. They are indented under the first line instead, so the
// table still reads as a table and the runtime's output still reads as its own.
func indentedError(indent, label string, err error) string {
	lines := strings.Split(strings.TrimRight(err.Error(), "\n"), "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s%s\n", indent, label, lines[0])
	cont := indent + strings.Repeat(" ", len([]rune(label)))
	for _, l := range lines[1:] {
		fmt.Fprintf(&b, "%s%s\n", cont, l)
	}
	return b.String()
}

func (v *traceView) writePlain(w io.Writer) {
	t := v.times()
	fmt.Fprintln(w, v.header())
	if v.runErr != nil {
		fmt.Fprint(w, indentedError("", "run failed: ", v.runErr))
	}
	fmt.Fprintln(w, "% is the step's share of the run; self% excludes the work of its nested frames.")
	// Said only where it applies: on a program with no blocks it would be two
	// lines of explanation for something the trace never does.
	if hasBlocks(v.rec.Roots()) {
		fmt.Fprintln(w, "A block row (Channel, Part, one loop lap) reports what its body produced, which is")
		fmt.Fprintln(w, "not what it passes on: those stages hand on the value that entered them.")
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s %6s %9s %7s %7s\n",
		col("step", 40), col("out type", 22), "size", "time", "%", "self%")

	var walk func(nodes []*interp.TraceNode, depth int)
	walk = func(nodes []*interp.TraceNode, depth int) {
		// --depth stops the walk short, for a reader whose trace is going into
		// a CI log: the top two levels of a recording are the program, and the
		// four hundred below them are one loop.
		if v.depth > 0 && depth >= v.depth {
			if steps, frames := hiddenBy(nodes); steps+frames > 0 || len(nodes) > 0 {
				fmt.Fprintf(w, "%s… %s below this depth (--depth raises the limit)\n",
					strings.Repeat("  ", depth), plural(len(nodes)+steps+frames, "row"))
			}
			return
		}
		for _, n := range nodes {
			indent := strings.Repeat("  ", depth)
			nt := t.Of(n)
			// Pad by runes and against fixed columns, so nesting and multi-byte
			// type names keep the columns straight. A frame is not a step: it
			// has no value or type of its own, only what its body came to.
			label, outType, size := indent+n.Frame, "", ""
			if !n.IsFrame() {
				label, outType, size = indent+n.Label(), typeOf(n.Step), sizeOf(n.Step)
			}
			if b := n.Block; b != nil {
				outType, size = b.Type, recSize(b)
			}
			fmt.Fprintf(w, "%s %s %6s %9s %7s %7s\n",
				col(label, 40), col(outType, 22), size,
				interp.FormatDuration(nt.Total), pctText(nt.TotalPct, nt.Known),
				selfPctText(nt))
			if !n.IsFrame() && n.Step.Err != nil {
				fmt.Fprint(w, indentedError(indent+"  ", "error: ", n.Step.Err))
			}
			// A folded run of laps prints its first lap and then says what it
			// is standing in for: five hundred laps of the same three steps
			// would otherwise be the whole output, and none of it the program.
			if laps, folded := n.Iterations(); folded && !v.expand && laps > 1 {
				walk(n.Children[:1], depth+1)
				hidden, _ := hiddenBy(n.Children[1:])
				fmt.Fprintf(w, "%s… %d more iterations, %s (--expand-loops shows them)\n",
					strings.Repeat("  ", depth+1), laps-1, plural(hidden, "step"))
				continue
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

// writeGo prints the Go the compiler backend would emit for this program — the
// text form of the stepper's code pane, for a reader who is not a terminal.
func (v *traceView) writeGo(w io.Writer) {
	src, _, err := v.emitted()
	if err != nil {
		fmt.Fprintf(w, "\ngenerated go:\n  %v\n", err)
		return
	}
	fmt.Fprintf(w, "\ngenerated go (%d lines):\n", len(src))
	for _, line := range src {
		fmt.Fprintln(w, line)
	}
}

// hasBlocks reports whether anything in the recording has a body of its own,
// which is what the plain output's note about block rows is worth printing for.
func hasBlocks(nodes []*interp.TraceNode) bool {
	for _, n := range nodes {
		if n.Block != nil || hasBlocks(n.Children) {
			return true
		}
	}
	return false
}

// hiddenBy reports what a set of rows holds: how many steps, and how many
// frames. It is what a fold says about the laps it is standing in for.
func hiddenBy(nodes []*interp.TraceNode) (steps, frames int) {
	for _, n := range nodes {
		if n.IsFrame() {
			frames++
		} else {
			steps++
		}
		s, f := n.Counts()
		steps, frames = steps+s, frames+f
	}
	return steps, frames
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

// recSize renders a captured value's size, the same way sizeOf renders a step's.
func recSize(r *interp.Recorded) string {
	if r == nil || !r.SizeOK {
		return "—"
	}
	return strconv.Itoa(r.Size)
}
