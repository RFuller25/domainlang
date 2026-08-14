// `domain expansion: bench <file>` — the four-cell measurement: both
// backends, each with the optimizer on and off, timed and with their
// allocation reported.
//
// The command exists because timing a Domain program by hand gets the
// methodology wrong in ways that flatter or slander the language: piping the
// input instead of redirecting it, taking a mean instead of a minimum, timing
// one configuration's five runs before the other's so a thermal step lands on
// one side. Package runner settles all of that once; this file is the table.
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

	"domain/interp"
	"domain/runner"
)

// measureDomainBin is the binary the runner re-executes for interpreted
// cells. Empty means the currently running executable, which is exactly right
// for the CLI — that executable is `domain`. The tests set it, because under
// `go test` the running executable is the test binary, and an interpreted cell
// would otherwise re-run the test suite instead of the program.
var measureDomainBin string

type benchOptions struct {
	Input     string
	InputText string
	Runs      int
	Timeout   time.Duration
	Release   bool
	Cells     []runner.Config
	JSON      bool
	Markdown  bool
	Plain     bool
}

func parseBenchArgs(args []string) (string, benchOptions, error) {
	// A copy, never runner.Four itself: --release marks each cell in place,
	// and mutating the package-level grid would leak that setting into every
	// later measurement in the process.
	opts := benchOptions{Cells: append([]runner.Config(nil), runner.Four...)}
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func(flag string) (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", flag)
			}
			i++
			return args[i], nil
		}
		var err error
		switch {
		case a == "--input" || a == "-i":
			opts.Input, err = next(a)
		case strings.HasPrefix(a, "--input="):
			opts.Input = strings.TrimPrefix(a, "--input=")
		case a == "--input-text":
			opts.InputText, err = next(a)
		case strings.HasPrefix(a, "--input-text="):
			opts.InputText = strings.TrimPrefix(a, "--input-text=")
		case a == "--runs":
			var s string
			if s, err = next(a); err == nil {
				opts.Runs, err = strconv.Atoi(s)
			}
		case strings.HasPrefix(a, "--runs="):
			opts.Runs, err = strconv.Atoi(strings.TrimPrefix(a, "--runs="))
		case a == "--timeout":
			var s string
			if s, err = next(a); err == nil {
				opts.Timeout, err = time.ParseDuration(s)
			}
		case strings.HasPrefix(a, "--timeout="):
			opts.Timeout, err = time.ParseDuration(strings.TrimPrefix(a, "--timeout="))
		case a == "--cells":
			var s string
			if s, err = next(a); err == nil {
				opts.Cells, err = parseCells(s)
			}
		case strings.HasPrefix(a, "--cells="):
			opts.Cells, err = parseCells(strings.TrimPrefix(a, "--cells="))
		case a == "--release":
			opts.Release = true
		case a == "--json":
			opts.JSON = true
		case a == "--markdown":
			opts.Markdown = true
		case a == "--plain":
			opts.Plain = true
		default:
			if strings.HasPrefix(a, "-") {
				return "", opts, fmt.Errorf("unknown flag %q", a)
			}
			if path != "" {
				return "", opts, fmt.Errorf("unexpected extra argument %q", a)
			}
			path = a
		}
		if err != nil {
			return "", opts, err
		}
	}
	if path == "" {
		return "", opts, fmt.Errorf("bench needs a program file")
	}
	if opts.Input != "" && opts.InputText != "" {
		return "", opts, fmt.Errorf("--input and --input-text both say what the program reads; pick one")
	}
	if opts.Runs < 0 {
		return "", opts, fmt.Errorf("--runs cannot be negative")
	}
	if opts.Release {
		for i := range opts.Cells {
			opts.Cells[i].Release = true
		}
	}
	return path, opts, nil
}

// parseCells reads a subset of the grid: "interpret/optimized,compile/naive".
// Interpreting the naive pipeline is often the one cell nobody wants to wait
// for, which is the whole reason this flag exists.
func parseCells(s string) ([]runner.Config, error) {
	var out []runner.Config
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		half := strings.SplitN(part, "/", 2)
		if len(half) != 2 {
			return nil, fmt.Errorf("cell %q is not <backend>/<mode>", part)
		}
		var c runner.Config
		switch strings.TrimSpace(half[0]) {
		case "interpret":
		case "compile":
			c.Compiled = true
		default:
			return nil, fmt.Errorf("cell %q: backend must be interpret or compile", part)
		}
		switch strings.TrimSpace(half[1]) {
		case "naive":
		case "optimized":
			c.Optimize = true
		default:
			return nil, fmt.Errorf("cell %q: mode must be naive or optimized", part)
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--cells named no cells")
	}
	return out, nil
}

// benchInput decides what the program reads: an explicit flag, else a sibling
// input file, else nothing. The report always says which, because a timing
// against an input the reader did not expect is worse than no timing.
func benchInput(path string, opts benchOptions) (runner.Input, string) {
	if opts.InputText != "" {
		return runner.Input{Bytes: []byte(opts.InputText)}, "--input-text"
	}
	if opts.Input != "" {
		return runner.Input{Path: opts.Input}, opts.Input
	}
	if sib := siblingInput(path); sib != "" {
		return runner.Input{Path: sib}, sib + " (found beside the program)"
	}
	return runner.Input{}, "none"
}

// siblingInput finds the conventional input beside a program: <stem>.input,
// then <stem>_input.txt, then input.txt — the layouts challenges/, examples/
// and testdata/ already use.
func siblingInput(path string) string {
	dir := filepath.Dir(path)
	stem := strings.TrimSuffix(filepath.Base(path), ".domain")
	for _, name := range []string{stem + ".input", stem + "_input.txt", "input.txt"} {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// Bench measures one program across the requested cells and reports.
func Bench(path string, opts benchOptions, stdout, stderr io.Writer) int {
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	defer runner.Cleanup()

	in, inputDesc := benchInput(path, opts)
	results, err := runner.Race(path, opts.Cells, in, runner.Options{
		Runs:      opts.Runs,
		Timeout:   opts.Timeout,
		Alloc:     true,
		DomainBin: measureDomainBin,
	})
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}

	// Which optimizer passes fired, for the footer. A program that fails to
	// resolve would already have failed every cell above.
	var passes []string
	if _, rewrites, err := runner.LoadRewrites(path); err == nil {
		seen := map[string]bool{}
		for _, r := range rewrites {
			if r.Pass != "" && !seen[r.Pass] {
				seen[r.Pass] = true
				passes = append(passes, r.Pass)
			}
		}
	}

	rep := &benchReport{
		Program: path,
		Input:   inputDesc,
		Runs:    runsOf(opts),
		Passes:  passes,
		Results: results,
	}
	rep.check()

	switch {
	case opts.JSON:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep.jsonShape()); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
	case opts.Markdown:
		rep.writeMarkdown(stdout)
	default:
		rep.writeTable(stdout)
	}
	if rep.Disagreement != "" || rep.harnessFailed() {
		return 1
	}
	return 0
}

func runsOf(opts benchOptions) int {
	if opts.Runs <= 0 {
		return runner.DefaultRuns
	}
	return opts.Runs
}

// ---------------------------------------------------------------------------
// The report
// ---------------------------------------------------------------------------

type benchReport struct {
	Program string
	Input   string
	Runs    int
	Passes  []string
	Results []runner.Result

	// Disagreement is set when two cells produced different output. That is a
	// compiler bug — the naive/optimized pair is the optimizer's own oracle
	// and the interpret/compile pair is the backends' — so it is reported in
	// those words and nothing else in the report is worth reading until it is
	// fixed.
	Disagreement string
}

func (r *benchReport) harnessFailed() bool {
	for i := range r.Results {
		if r.Results[i].Err != nil {
			return true
		}
	}
	return false
}

// check compares the output of every cell that produced one.
func (r *benchReport) check() {
	var ref *runner.Result
	for i := range r.Results {
		res := &r.Results[i]
		if res.Err != nil || res.Failed() {
			continue
		}
		if ref == nil {
			ref = res
			continue
		}
		if string(res.Stdout) != string(ref.Stdout) {
			r.Disagreement = fmt.Sprintf(
				"%s and %s produced different output",
				ref.Config.Label(), res.Config.Label())
			return
		}
	}
}

func (r *benchReport) writeTable(w io.Writer) {
	fmt.Fprintf(w, "%s · input: %s · best of %d\n\n", r.Program, r.Input, r.Runs)

	fmt.Fprintf(w, "  %-22s %10s %12s %12s %10s\n", "cell", "time", "peak RSS", "allocated", "build")
	fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 70))
	for i := range r.Results {
		res := &r.Results[i]
		fmt.Fprintf(w, "  %-22s %10s %12s %12s %10s\n",
			res.Config.Label(), benchTime(res), benchRSS(res), benchAlloc(res), benchBuild(res))
	}
	fmt.Fprintln(w)

	for _, line := range r.ratios() {
		fmt.Fprintf(w, "  %s\n", line)
	}

	if r.Disagreement != "" {
		fmt.Fprintf(w, "\n  ✗ COMPILER BUG: %s\n", r.Disagreement)
		fmt.Fprintf(w, "    the four cells must agree; this is the optimizer's and the\n")
		fmt.Fprintf(w, "    backends' own correctness oracle. The timings above are not\n")
		fmt.Fprintf(w, "    worth reading until it is fixed.\n")
		r.writeDiff(w)
		return
	}
	// The agreement check is the most valuable thing in the report, so the
	// report says when it could not be made. One surviving cell is a common
	// outcome on a heavy program — the naive pipeline is often the one that
	// cannot finish — and silently omitting the ✓ would read as a pass.
	switch n := r.agreeing(); {
	case n > 1:
		fmt.Fprintf(w, "  ✓ all %d cells that ran agreed on the output\n", n)
	case n == 1:
		fmt.Fprintf(w, "  · only one cell produced output, so nothing was cross-checked\n")
	default:
		fmt.Fprintf(w, "  · no cell produced output\n")
	}
	if len(r.Passes) > 0 {
		fmt.Fprintf(w, "  %d optimizer pass(es) fired: %s\n", len(r.Passes), strings.Join(r.Passes, ", "))
	} else {
		fmt.Fprintf(w, "  no optimizer passes fired\n")
	}
	for i := range r.Results {
		if err := r.Results[i].Err; err != nil {
			fmt.Fprintf(w, "  ! %s could not run: %v\n", r.Results[i].Config.Label(), err)
		}
	}
}

// writeDiff shows the first differing line between the reference cell and the
// one that disagreed, which is nearly always enough to see what happened.
func (r *benchReport) writeDiff(w io.Writer) {
	var ref *runner.Result
	for i := range r.Results {
		res := &r.Results[i]
		if res.Err != nil || res.Failed() {
			continue
		}
		if ref == nil {
			ref = res
			continue
		}
		if string(res.Stdout) == string(ref.Stdout) {
			continue
		}
		a := strings.Split(strings.TrimRight(string(ref.Stdout), "\n"), "\n")
		b := strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n")
		for j := 0; j < max(len(a), len(b)); j++ {
			var x, y string
			if j < len(a) {
				x = a[j]
			}
			if j < len(b) {
				y = b[j]
			}
			if x != y {
				fmt.Fprintf(w, "\n    line %d\n      %s: %q\n      %s: %q\n",
					j+1, ref.Config.Label(), x, res.Config.Label(), y)
				return
			}
		}
		return
	}
}

func (r *benchReport) agreeing() int {
	n := 0
	for i := range r.Results {
		if r.Results[i].Err == nil && !r.Results[i].Failed() {
			n++
		}
	}
	return n
}

// ratios reports what the optimizer and the compiler each bought, but only
// between two cells that both produced a time. A cell that timed out has no
// number, and extrapolating one would be inventing the result.
func (r *benchReport) ratios() []string {
	find := func(compiled, optimize bool) *runner.Result {
		for i := range r.Results {
			c := r.Results[i].Config
			if c.Compiled == compiled && c.Optimize == optimize {
				return &r.Results[i]
			}
		}
		return nil
	}
	timed := func(res *runner.Result) bool {
		return res != nil && res.Err == nil && !res.Failed() && res.Wall > 0
	}
	var out []string
	for _, compiled := range []bool{false, true} {
		naive, opt := find(compiled, false), find(compiled, true)
		if timed(naive) && timed(opt) {
			label := "interpreting"
			if compiled {
				label = "compiling"
			}
			out = append(out, fmt.Sprintf("the optimizer buys %.1f× when %s",
				float64(naive.Wall)/float64(opt.Wall), label))
		}
	}
	for _, optimize := range []bool{false, true} {
		interp, comp := find(false, optimize), find(true, optimize)
		if timed(interp) && timed(comp) {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			out = append(out, fmt.Sprintf("compiling buys %.1f× over interpreting (%s)",
				float64(interp.Wall)/float64(comp.Wall), mode))
		}
	}
	return out
}

func (r *benchReport) writeMarkdown(w io.Writer) {
	fmt.Fprintf(w, "### `%s`\n\n", r.Program)
	fmt.Fprintf(w, "Input: `%s` · best of %d\n\n", r.Input, r.Runs)
	fmt.Fprintln(w, "| Cell | Time | Peak RSS | Allocated | Build |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---:|")
	for i := range r.Results {
		res := &r.Results[i]
		fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s |\n",
			res.Config.Label(), benchTime(res), benchRSS(res), benchAlloc(res), benchBuild(res))
	}
	fmt.Fprintln(w)
	for _, line := range r.ratios() {
		fmt.Fprintf(w, "- %s\n", line)
	}
	if r.Disagreement != "" {
		fmt.Fprintf(w, "\n**COMPILER BUG: %s**\n", r.Disagreement)
	}
}

type benchCellJSON struct {
	Cell       string `json:"cell"`
	Compiled   bool   `json:"compiled"`
	Optimized  bool   `json:"optimized"`
	WallNanos  int64  `json:"wall_nanos,omitempty"`
	BuildNanos int64  `json:"build_nanos,omitempty"`
	PeakRSS    int64  `json:"peak_rss_bytes,omitempty"`
	TotalAlloc uint64 `json:"total_alloc_bytes,omitempty"`
	Mallocs    uint64 `json:"mallocs,omitempty"`
	NumGC      uint32 `json:"num_gc,omitempty"`
	Timeout    bool   `json:"timeout,omitempty"`
	ExitCode   int    `json:"exit_code"`
	Error      string `json:"error,omitempty"`
}

type benchJSON struct {
	Program      string          `json:"program"`
	Input        string          `json:"input"`
	Runs         int             `json:"runs"`
	Passes       []string        `json:"passes_fired"`
	Disagreement string          `json:"disagreement,omitempty"`
	Cells        []benchCellJSON `json:"cells"`
}

func (r *benchReport) jsonShape() benchJSON {
	out := benchJSON{
		Program: r.Program, Input: r.Input, Runs: r.Runs,
		Passes: r.Passes, Disagreement: r.Disagreement,
	}
	if out.Passes == nil {
		out.Passes = []string{}
	}
	for i := range r.Results {
		res := &r.Results[i]
		c := benchCellJSON{
			Cell: res.Config.Label(), Compiled: res.Config.Compiled,
			Optimized: res.Config.Optimize, ExitCode: res.ExitCode, Timeout: res.Timeout,
			PeakRSS: res.Alloc.PeakRSS,
		}
		if res.Err != nil {
			c.Error = res.Err.Error()
		}
		if res.Wall > 0 {
			c.WallNanos = res.Wall.Nanoseconds()
		}
		if res.Build > 0 {
			c.BuildNanos = res.Build.Nanoseconds()
		}
		if res.Alloc.Reported {
			c.TotalAlloc, c.Mallocs, c.NumGC = res.Alloc.TotalAlloc, res.Alloc.Mallocs, res.Alloc.NumGC
		}
		out.Cells = append(out.Cells, c)
	}
	return out
}

// ---------------------------------------------------------------------------
// Cell rendering
// ---------------------------------------------------------------------------

// benchTime is the cell's headline. A run that did not finish says so rather
// than showing a number: "did not finish" and "took 60s" are different facts,
// and this is a common outcome on a naive pipeline over a real input.
func benchTime(res *runner.Result) string {
	switch {
	case res.Err != nil:
		return "—"
	case res.Timeout:
		return "did not finish"
	case res.ExitCode != 0:
		return fmt.Sprintf("exit %d", res.ExitCode)
	default:
		return interp.FormatDuration(res.Wall)
	}
}

func benchBuild(res *runner.Result) string {
	if res.Build == 0 {
		return "—"
	}
	return interp.FormatDuration(res.Build)
}

func benchRSS(res *runner.Result) string {
	if res.Alloc.PeakRSS == 0 {
		return "—"
	}
	return formatBytes(uint64(res.Alloc.PeakRSS))
}

func benchAlloc(res *runner.Result) string {
	if !res.Alloc.Reported {
		return "—"
	}
	return formatBytes(res.Alloc.TotalAlloc)
}

// Byte counts render through formatBytes, shared with the development
// monitor (dev_monitor.go).
