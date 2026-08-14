// `domain expansion: coverage <folder>` — which of the primitive catalog and
// the expression-layer builtins has a folder of programs never exercised.
//
// The "what have you not tried yet" nudge. A year of Advent of Code solutions
// reaches for the same dozen primitives; the catalog has 85, and a reader who
// has never met `Sliding Reduce` has no way to discover that from their own
// code. This command is the diff between what a folder uses and what the
// language offers, pointed at the reference page for each thing it finds.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"domain/ir"
	"domain/prims"
	"domain/runner"
)

type coverageOptions struct {
	Dynamic bool
	Used    bool // invert the report: what the folder *does* use
	Only    string
	Exclude string
	Min     float64
	JSON    bool
	Plain   bool
}

func parseCoverageArgs(args []string) (string, coverageOptions, error) {
	var opts coverageOptions
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
		case a == "--dynamic":
			opts.Dynamic = true
		case a == "--used":
			opts.Used = true
		case a == "--json":
			opts.JSON = true
		case a == "--plain":
			opts.Plain = true
		case a == "--only":
			opts.Only, err = next(a)
		case strings.HasPrefix(a, "--only="):
			opts.Only = strings.TrimPrefix(a, "--only=")
		case a == "--exclude":
			opts.Exclude, err = next(a)
		case strings.HasPrefix(a, "--exclude="):
			opts.Exclude = strings.TrimPrefix(a, "--exclude=")
		case a == "--min":
			var s string
			if s, err = next(a); err == nil {
				_, err = fmt.Sscanf(s, "%f", &opts.Min)
			}
		case strings.HasPrefix(a, "--min="):
			_, err = fmt.Sscanf(strings.TrimPrefix(a, "--min="), "%f", &opts.Min)
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
		return "", opts, fmt.Errorf("coverage needs a folder")
	}
	switch opts.Only {
	case "", "prims", "builtins", "keywords":
	default:
		return "", opts, fmt.Errorf("--only takes prims, builtins or keywords (got %q)", opts.Only)
	}
	return path, opts, nil
}

// programFile is one program found in the scanned folder.
type programFile struct {
	Path  string
	Input string // sibling input, or "" when there is none
}

// scanFolder finds every .domain program under root, pairing each with its
// input. The layout is the one challenges/, examples/ and testdata/ use.
func scanFolder(root, exclude string) ([]programFile, error) {
	var out []programFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".domain") {
			return nil
		}
		if exclude != "" {
			if ok, _ := filepath.Match(exclude, filepath.Base(path)); ok {
				return nil
			}
		}
		out = append(out, programFile{Path: path, Input: siblingInput(path)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Coverage reports what a folder exercises against the catalog.
func Coverage(root string, opts coverageOptions, stdout, stderr io.Writer) int {
	fi, err := os.Stat(root)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	if !fi.IsDir() {
		fmt.Fprintf(stderr, "domain: %s is not a folder\n", root)
		return 2
	}
	progs, err := scanFolder(root, opts.Exclude)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	if len(progs) == 0 {
		fmt.Fprintf(stderr, "domain: no .domain programs under %s\n", root)
		return 1
	}

	rep := &coverageReport{Root: root, Dynamic: opts.Dynamic, Total: len(progs)}
	rep.collect(progs)
	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep.jsonShape(opts)); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
	} else {
		rep.write(stdout, opts)
	}
	if opts.Min > 0 && rep.primPct() < opts.Min {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Collecting
// ---------------------------------------------------------------------------

type skipped struct {
	Path   string
	Reason string
}

type coverageReport struct {
	Root    string
	Dynamic bool
	Total   int

	// Static is what the programs are written in terms of; Ran is what
	// actually evaluated. The two are kept apart because they answer
	// different questions and only one of them is available for builtins.
	Static  *prims.Usage
	Ran     map[string]int
	RanFrom int // programs that contributed to Ran

	Skipped []skipped
}

func (r *coverageReport) collect(progs []programFile) {
	r.Static = &prims.Usage{
		Prims:    map[string]int{},
		Builtins: map[string]int{},
		Keywords: map[string]int{},
	}
	r.Ran = map[string]int{}

	for _, p := range progs {
		// Coverage is always measured on the *unoptimized* pipeline. fuseMapMap
		// turns two Map Each nodes into one and elideRedundantSort deletes a
		// Sort outright, so measuring the optimized IR would report that a
		// program which visibly uses Sort does not — exactly backwards for a
		// command whose job is telling you what you have not written yet.
		pipe, err := runner.LoadPipeline(p.Path, false)
		if err != nil {
			r.Skipped = append(r.Skipped, skipped{p.Path, firstLine(err.Error())})
			continue
		}
		r.Static.Merge(prims.Used(pipe))

		if r.Dynamic {
			r.runOne(p)
		}
	}
}

// runOne interprets a program with a counting tracer installed and records
// which primitives actually evaluated.
//
// It is in-process, because a subprocess cannot carry a trace hook — and so it
// is strictly one at a time: the interpreter keeps process-global state and two
// concurrent runs corrupt each other (see runner.Interpret).
func (r *coverageReport) runOne(p programFile) {
	if p.Input == "" {
		r.Skipped = append(r.Skipped, skipped{p.Path, "no input file to run it against"})
		return
	}
	in, err := os.Open(p.Input)
	if err != nil {
		r.Skipped = append(r.Skipped, skipped{p.Path, err.Error()})
		return
	}
	defer func() { _ = in.Close() }()

	counter := &primCounter{seen: map[string]int{}}
	ctx := &ir.Context{Stdin: in, Stdout: io.Discard, Trace: counter}
	if _, _, err := runner.Interpret(p.Path, false, ctx); err != nil {
		// A program that failed still exercised everything it reached on the
		// way, so its counts are kept; the failure is reported beside them.
		r.Skipped = append(r.Skipped, skipped{p.Path, "ran with an error: " + firstLine(err.Error())})
	}
	for k, v := range counter.seen {
		r.Ran[k] += v
	}
	r.RanFrom++
}

// primCounter is the value-free trace consumer: it keeps names and counts and
// nothing else, so it can run over any input without bounding what it holds.
type primCounter struct{ seen map[string]int }

func (c *primCounter) Step(ev ir.StepEvent) {
	if ev.Node != nil {
		c.seen[ev.Node.Prim]++
	}
}

func (c *primCounter) PushFrame(string, *ir.Type) {}
func (c *primCounter) PopFrame(ir.Value)          {}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

func (r *coverageReport) primPct() float64 {
	all := prims.AllPrims()
	if len(all) == 0 {
		return 100
	}
	return 100 * float64(len(r.Static.Prims)) / float64(len(all))
}

// missing returns the catalog entries with no use, and the count of used ones.
func missing(all []string, used map[string]int) (out []string, usedCount int) {
	for _, name := range all {
		if used[name] > 0 {
			usedCount++
		} else {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, usedCount
}

func (r *coverageReport) write(w io.Writer, opts coverageOptions) {
	fmt.Fprintf(w, "%s — %d program(s)\n\n", r.Root, r.Total)

	allPrims, allBuiltins, allKeywords := prims.AllPrims(), prims.AllBuiltins(), prims.AllKeywords()
	missPrims, usedPrims := missing(allPrims, r.Static.Prims)
	missBuiltins, usedBuiltins := missing(allBuiltins, r.Static.Builtins)
	missKeywords, usedKeywords := missing(allKeywords, r.Static.Keywords)

	show := func(kind string) bool { return opts.Only == "" || opts.Only == kind }

	if show("prims") {
		fmt.Fprintf(w, "  primitives  %3d / %3d  (%.0f%%)\n", usedPrims, len(allPrims), pct(usedPrims, len(allPrims)))
	}
	if show("builtins") {
		fmt.Fprintf(w, "  builtins    %3d / %3d  (%.0f%%)\n", usedBuiltins, len(allBuiltins), pct(usedBuiltins, len(allBuiltins)))
	}
	if show("keywords") {
		fmt.Fprintf(w, "  keywords    %3d / %3d  (%.0f%%)\n", usedKeywords, len(allKeywords), pct(usedKeywords, len(allKeywords)))
	}

	// The header states what the numbers mean, because "written down" and
	// "actually ran" are different claims and only one of them is available
	// for builtins.
	fmt.Fprintln(w)
	if r.Dynamic {
		fmt.Fprintf(w, "  counted from the source; %d program(s) were also run, and the\n", r.RanFrom)
		fmt.Fprintf(w, "  primitives that never evaluated are marked ○ below.\n")
	} else {
		fmt.Fprintf(w, "  counted from the source: a primitive inside a branch that never runs\n")
		fmt.Fprintf(w, "  still counts. Add --dynamic to also run each program against its input.\n")
	}
	fmt.Fprintln(w)

	if opts.Used {
		r.writeUsed(w, opts, show)
	} else {
		r.writeMissing(w, opts, show, missPrims, missBuiltins, missKeywords)
	}

	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "\n  skipped (%d):\n", len(r.Skipped))
		for _, s := range r.Skipped {
			fmt.Fprintf(w, "    %s — %s\n", s.Path, s.Reason)
		}
	}
}

func (r *coverageReport) writeMissing(w io.Writer, opts coverageOptions, show func(string) bool, missPrims, missBuiltins, missKeywords []string) {
	if show("prims") && len(missPrims) > 0 {
		// Grouped by the keyword class each primitive lives under, which is
		// how the reference is organized and so how a reader will go looking.
		byKeyword := map[string][]string{}
		for _, id := range missPrims {
			byKeyword[prims.Catalog[id].Keyword] = append(byKeyword[prims.Catalog[id].Keyword], id)
		}
		for _, kw := range sortedKeys(byKeyword) {
			fmt.Fprintf(w, "  %s — %d not exercised\n", kw, len(byKeyword[kw]))
			for _, id := range byKeyword[kw] {
				doc := prims.Catalog[id]
				fmt.Fprintf(w, "    %-22s %-34s %s#%s\n", id, doc.Signature, doc.DocPage(), doc.DocAnchor)
			}
			fmt.Fprintln(w)
		}
	}
	if show("prims") && r.Dynamic {
		// Written but never reached: a strictly stronger finding than "never
		// written", and the one --dynamic exists for.
		var never []string
		for id := range r.Static.Prims {
			if r.Ran[id] == 0 {
				never = append(never, id)
			}
		}
		sort.Strings(never)
		if len(never) > 0 {
			fmt.Fprintf(w, "  ○ written but never evaluated — %d\n", len(never))
			for _, id := range never {
				fmt.Fprintf(w, "    %s\n", id)
			}
			fmt.Fprintln(w)
		}
	}
	if show("builtins") && len(missBuiltins) > 0 {
		fmt.Fprintf(w, "  builtins not exercised — %d\n", len(missBuiltins))
		writeWrapped(w, "    ", missBuiltins)
		fmt.Fprintln(w)
	}
	if show("keywords") && len(missKeywords) > 0 {
		fmt.Fprintf(w, "  keywords not exercised — %d\n", len(missKeywords))
		writeWrapped(w, "    ", missKeywords)
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "  `domain expansion: documentation` and search a name to see what it does\n")
}

func (r *coverageReport) writeUsed(w io.Writer, opts coverageOptions, show func(string) bool) {
	if show("prims") {
		fmt.Fprintf(w, "  primitives used (%d), most first:\n", len(r.Static.Prims))
		for _, e := range byCount(r.Static.Prims) {
			ran := ""
			if r.Dynamic && r.Ran[e.name] == 0 {
				ran = "  ○ never evaluated"
			}
			fmt.Fprintf(w, "    %-24s %6d%s\n", e.name, e.n, ran)
		}
		fmt.Fprintln(w)
	}
	if show("builtins") {
		fmt.Fprintf(w, "  builtins used (%d), most first:\n", len(r.Static.Builtins))
		for _, e := range byCount(r.Static.Builtins) {
			fmt.Fprintf(w, "    %-24s %6d\n", e.name, e.n)
		}
		fmt.Fprintln(w)
	}
	if show("keywords") {
		fmt.Fprintf(w, "  keywords used (%d):\n", len(r.Static.Keywords))
		for _, e := range byCount(r.Static.Keywords) {
			fmt.Fprintf(w, "    %-28s %6d\n", e.name, e.n)
		}
	}
}

type nameCount struct {
	name string
	n    int
}

func byCount(m map[string]int) []nameCount {
	out := make([]nameCount, 0, len(m))
	for k, v := range m {
		out = append(out, nameCount{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].name < out[j].name
	})
	return out
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func pct(n, total int) float64 {
	if total == 0 {
		return 100
	}
	return 100 * float64(n) / float64(total)
}

// writeWrapped prints names in columns rather than one per line: sixty-seven
// unexercised builtins as a single column is a wall, not a nudge.
func writeWrapped(w io.Writer, indent string, names []string) {
	const width = 76
	line := indent
	for _, n := range names {
		if len(line)+len(n)+2 > width && line != indent {
			fmt.Fprintln(w, line)
			line = indent
		}
		line += n + "  "
	}
	if strings.TrimSpace(line) != "" {
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
}

type coverageJSON struct {
	Root     string         `json:"root"`
	Programs int            `json:"programs"`
	Dynamic  bool           `json:"dynamic"`
	Prims    coverageKind   `json:"primitives"`
	Builtins coverageKind   `json:"builtins"`
	Keywords coverageKind   `json:"keywords"`
	NeverRan []string       `json:"written_but_never_evaluated,omitempty"`
	Skipped  []skippedJSON  `json:"skipped,omitempty"`
	Counts   map[string]int `json:"primitive_counts,omitempty"`
}

type coverageKind struct {
	Used    int      `json:"used"`
	Total   int      `json:"total"`
	Percent float64  `json:"percent"`
	Missing []string `json:"missing"`
}

type skippedJSON struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func (r *coverageReport) jsonShape(opts coverageOptions) coverageJSON {
	mk := func(all []string, used map[string]int) coverageKind {
		miss, n := missing(all, used)
		if miss == nil {
			miss = []string{}
		}
		return coverageKind{Used: n, Total: len(all), Percent: pct(n, len(all)), Missing: miss}
	}
	out := coverageJSON{
		Root: r.Root, Programs: r.Total, Dynamic: r.Dynamic,
		Prims:    mk(prims.AllPrims(), r.Static.Prims),
		Builtins: mk(prims.AllBuiltins(), r.Static.Builtins),
		Keywords: mk(prims.AllKeywords(), r.Static.Keywords),
		Counts:   r.Static.Prims,
	}
	if r.Dynamic {
		for id := range r.Static.Prims {
			if r.Ran[id] == 0 {
				out.NeverRan = append(out.NeverRan, id)
			}
		}
		sort.Strings(out.NeverRan)
	}
	for _, s := range r.Skipped {
		out.Skipped = append(out.Skipped, skippedJSON{s.Path, s.Reason})
	}
	return out
}
