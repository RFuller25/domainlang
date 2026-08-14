// `domain expansion: stats <folder>` — a whole year of solutions at a glance:
// per-program runtime, lines, stages, which optimizer passes fired, and
// whether the answer still matches what was expected.
//
// This is the survey command. `bench` is for one program studied properly —
// four cells, allocation, best of five; `stats` runs one configuration over a
// folder and lays the results out as a leaderboard, which is a different
// question and deserves a much cheaper answer per program.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"domain/interp"
	"domain/prims"
	"domain/runner"
)

type statsOptions struct {
	Sort      string
	Top       int
	Runs      int
	Timeout   time.Duration
	Interpret bool
	Markdown  bool
	JSON      bool
	Plain     bool
	Exclude   string
}

func parseStatsArgs(args []string) (string, statsOptions, error) {
	opts := statsOptions{Sort: "name", Runs: 3}
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
		case a == "--sort":
			opts.Sort, err = next(a)
		case strings.HasPrefix(a, "--sort="):
			opts.Sort = strings.TrimPrefix(a, "--sort=")
		case a == "--top":
			var s string
			if s, err = next(a); err == nil {
				opts.Top, err = strconv.Atoi(s)
			}
		case strings.HasPrefix(a, "--top="):
			opts.Top, err = strconv.Atoi(strings.TrimPrefix(a, "--top="))
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
		case a == "--exclude":
			opts.Exclude, err = next(a)
		case strings.HasPrefix(a, "--exclude="):
			opts.Exclude = strings.TrimPrefix(a, "--exclude=")
		case a == "--interpret":
			opts.Interpret = true
		case a == "--markdown":
			opts.Markdown = true
		case a == "--json":
			opts.JSON = true
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
		return "", opts, fmt.Errorf("stats needs a folder")
	}
	switch opts.Sort {
	case "name", "time", "loc", "passes":
	default:
		return "", opts, fmt.Errorf("--sort takes name, time, loc or passes (got %q)", opts.Sort)
	}
	if opts.Runs < 1 {
		return "", opts, fmt.Errorf("--runs must be at least 1")
	}
	return path, opts, nil
}

// statRow is one program's line in the leaderboard.
type statRow struct {
	Path   string
	Name   string
	LOC    int // non-blank, non-comment source lines
	Lines  int // every line, for --json
	Stages int // top-level nodes in the unoptimized pipeline
	Passes []string
	Wall   time.Duration
	Build  time.Duration

	// Expected is whether the output matched the .expected sibling. It is a
	// pointer so "no .expected file" and "did not match" stay distinct: a
	// folder without them gets the column dropped rather than a column of
	// dashes implying failure.
	Expected *bool

	// Status is empty on success, else why this row has no timing. A program
	// that failed keeps its row — dropping it would quietly shrink the folder
	// and make the totals wrong.
	Status string
}

// Stats surveys a folder and prints the leaderboard.
func Stats(root string, opts statsOptions, stdout, stderr io.Writer) int {
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
	defer runner.Cleanup()

	cfg := runner.Config{Compiled: !opts.Interpret, Optimize: true}
	rows := make([]statRow, 0, len(progs))
	vocab := prims.Used(nil)
	for _, p := range progs {
		rows = append(rows, measureOne(p, cfg, opts, vocab))
	}
	sortRows(rows, opts.Sort)
	if opts.Top > 0 && len(rows) > opts.Top {
		rows = rows[:opts.Top]
	}

	rep := &statsReport{Root: root, Config: cfg, Runs: opts.Runs, Rows: rows, Vocab: vocab}
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
	if rep.failures() > 0 {
		return 1
	}
	return 0
}

func measureOne(p programFile, cfg runner.Config, opts statsOptions, vocab *prims.Usage) statRow {
	row := statRow{Path: p.Path, Name: strings.TrimSuffix(filepath.Base(p.Path), ".domain")}

	src, err := os.ReadFile(p.Path)
	if err != nil {
		row.Status = err.Error()
		return row
	}
	row.LOC, row.Lines = countLOC(string(src))

	// Stage count and vocabulary come from the unoptimized pipeline, so the
	// column measures what was written rather than what survived; the pass
	// list necessarily comes from the optimized run.
	if pipe, err := runner.LoadPipeline(p.Path, false); err == nil {
		row.Stages = len(pipe.Nodes)
		vocab.Merge(prims.Used(pipe))
	}
	if _, rewrites, err := runner.LoadRewrites(p.Path); err == nil {
		seen := map[string]bool{}
		for _, rw := range rewrites {
			if rw.Pass != "" && !seen[rw.Pass] {
				seen[rw.Pass] = true
				row.Passes = append(row.Passes, rw.Pass)
			}
		}
		sort.Strings(row.Passes)
	} else {
		row.Status = firstLine(err.Error())
		return row
	}

	in := runner.Input{}
	if p.Input != "" {
		in.Path = p.Input
	}
	res, err := runner.Run(p.Path, cfg, in, runner.Options{
		Runs:      opts.Runs,
		Timeout:   opts.Timeout,
		DomainBin: measureDomainBin,
	})
	if err != nil {
		row.Status = firstLine(err.Error())
		return row
	}
	if res.Err != nil {
		row.Status = firstLine(res.Err.Error())
		return row
	}
	row.Build = res.Build
	switch {
	case res.Timeout:
		row.Status = "did not finish"
		return row
	case res.ExitCode != 0:
		row.Status = fmt.Sprintf("exit %d", res.ExitCode)
		return row
	}
	row.Wall = res.Wall

	if exp := expectedFile(p.Path); exp != "" {
		want, err := os.ReadFile(exp)
		if err == nil {
			ok := bytes.Equal(bytes.TrimRight(want, "\n"), bytes.TrimRight(res.Stdout, "\n"))
			row.Expected = &ok
		}
	}
	return row
}

func expectedFile(program string) string {
	p := strings.TrimSuffix(program, ".domain") + ".expected"
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// countLOC returns non-blank non-comment lines and the raw line count. The
// headline number is the one a reader would arrive at by counting.
func countLOC(src string) (loc, total int) {
	for _, line := range strings.Split(src, "\n") {
		total++
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		loc++
	}
	if strings.HasSuffix(src, "\n") {
		total-- // the split's trailing empty element
	}
	return loc, total
}

func sortRows(rows []statRow, by string) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch by {
		case "time":
			// Rows without a time sort last whichever way the list runs:
			// "did not finish" is not "instantaneous".
			if (a.Wall == 0) != (b.Wall == 0) {
				return b.Wall == 0
			}
			return a.Wall > b.Wall
		case "loc":
			return a.LOC > b.LOC
		case "passes":
			return len(a.Passes) > len(b.Passes)
		default:
			return a.Name < b.Name
		}
	})
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

type statsReport struct {
	Root   string
	Config runner.Config
	Runs   int
	Rows   []statRow
	Vocab  *prims.Usage
}

func (r *statsReport) failures() int {
	n := 0
	for _, row := range r.Rows {
		if row.Status != "" || (row.Expected != nil && !*row.Expected) {
			n++
		}
	}
	return n
}

// anyExpected reports whether any row had a .expected sibling; without one the
// ✓ column is dropped rather than filled with dashes.
func (r *statsReport) anyExpected() bool {
	for _, row := range r.Rows {
		if row.Expected != nil {
			return true
		}
	}
	return false
}

func (r *statsReport) totals() (loc, stages int, wall time.Duration, passes int, ok, of int) {
	seen := map[string]bool{}
	for _, row := range r.Rows {
		loc += row.LOC
		stages += row.Stages
		wall += row.Wall
		for _, p := range row.Passes {
			passes++
			seen[p] = true
		}
		if row.Expected != nil {
			of++
			if *row.Expected {
				ok++
			}
		}
	}
	return loc, stages, wall, passes, ok, of
}

func (r *statsReport) writeTable(w io.Writer) {
	fmt.Fprintf(w, "%s — %d program(s), %s, best of %d\n\n", r.Root, len(r.Rows), r.Config.Label(), r.Runs)

	showExp := r.anyExpected()
	head := fmt.Sprintf("  %-22s %5s %7s %10s  %-28s", "program", "LOC", "stages", "runtime", "passes fired")
	if showExp {
		head += " ✓"
	}
	fmt.Fprintln(w, head)
	fmt.Fprintf(w, "  %s\n", strings.Repeat("─", len(head)))

	for _, row := range r.Rows {
		runtime := interp.FormatDuration(row.Wall)
		if row.Status != "" {
			runtime = row.Status
		}
		passes := "—"
		if n := len(row.Passes); n > 0 {
			passes = fmt.Sprintf("×%d %s", n, truncateList(row.Passes, 22))
		}
		line := fmt.Sprintf("  %-22s %5d %7d %10s  %-28s",
			truncateText(row.Name, 22), row.LOC, row.Stages, runtime, passes)
		if showExp {
			line += " " + expectedMark(row.Expected)
		}
		fmt.Fprintln(w, line)
	}

	loc, stages, wall, passes, ok, of := r.totals()
	fmt.Fprintf(w, "  %s\n", strings.Repeat("─", len(head)))
	total := fmt.Sprintf("  %-22s %5d %7d %10s  %-28s",
		fmt.Sprintf("%d programs", len(r.Rows)), loc, stages,
		interp.FormatDuration(wall), fmt.Sprintf("%d rewrites", passes))
	if showExp {
		total += fmt.Sprintf(" %d/%d", ok, of)
	}
	fmt.Fprintln(w, total)
	fmt.Fprintln(w)

	r.writeHighlights(w)
}

func (r *statsReport) writeHighlights(w io.Writer) {
	slowest := topBy(r.Rows, func(a, b statRow) bool { return a.Wall > b.Wall }, 3)
	if len(slowest) > 0 && slowest[0].Wall > 0 {
		var parts []string
		for _, row := range slowest {
			if row.Wall > 0 {
				parts = append(parts, fmt.Sprintf("%s (%s)", row.Name, interp.FormatDuration(row.Wall)))
			}
		}
		fmt.Fprintf(w, "  slowest         %s\n", strings.Join(parts, " · "))
	}
	longest := topBy(r.Rows, func(a, b statRow) bool { return a.LOC > b.LOC }, 3)
	if len(longest) > 0 {
		var parts []string
		for _, row := range longest {
			parts = append(parts, fmt.Sprintf("%s (%d)", row.Name, row.LOC))
		}
		fmt.Fprintf(w, "  longest         %s\n", strings.Join(parts, " · "))
	}
	most := topBy(r.Rows, func(a, b statRow) bool { return len(a.Passes) > len(b.Passes) }, 3)
	if len(most) > 0 && len(most[0].Passes) > 0 {
		var parts []string
		for _, row := range most {
			if len(row.Passes) > 0 {
				parts = append(parts, fmt.Sprintf("%s (%d)", row.Name, len(row.Passes)))
			}
		}
		fmt.Fprintf(w, "  most rewritten  %s\n", strings.Join(parts, " · "))
	}

	// The pass histogram: which of the optimizer's 29 passes this folder
	// actually triggers, which is a fact about the programs as much as the
	// compiler.
	hist := map[string]int{}
	for _, row := range r.Rows {
		for _, p := range row.Passes {
			hist[p]++
		}
	}
	if len(hist) > 0 {
		fmt.Fprintf(w, "\n  passes fired across the folder:\n")
		for _, e := range byCount(hist) {
			fmt.Fprintf(w, "    %-28s %3d\n", e.name, e.n)
		}
	}

	usedPrims := len(r.Vocab.Prims)
	fmt.Fprintf(w, "\n  vocabulary      %d / %d primitives · %d / %d builtins\n",
		countInCatalog(r.Vocab.Prims), len(prims.AllPrims()),
		len(r.Vocab.Builtins), len(prims.AllBuiltins()))
	if usedPrims > 0 {
		fmt.Fprintf(w, "                  `domain expansion: coverage %s` for what is missing\n", r.Root)
	}
	if n := r.failures(); n > 0 {
		fmt.Fprintf(w, "\n  %d program(s) failed or did not match their .expected output\n", n)
	}
}

// countInCatalog counts only entries the catalog knows, so the numerator
// cannot exceed the denominator: the structural statements (loops, Channel,
// Part) are counted in Usage but are not registry primitives.
func countInCatalog(used map[string]int) int {
	n := 0
	for id := range used {
		if _, ok := prims.Catalog[id]; ok {
			n++
		}
	}
	return n
}

func topBy(rows []statRow, less func(a, b statRow) bool, n int) []statRow {
	out := append([]statRow(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func expectedMark(b *bool) string {
	switch {
	case b == nil:
		return " "
	case *b:
		return "✓"
	default:
		return "✗"
	}
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func truncateList(names []string, width int) string {
	if len(names) == 0 {
		return ""
	}
	s := names[0]
	if len(s) > width {
		return truncateText(s, width)
	}
	if len(names) > 1 {
		s += "…"
	}
	return s
}

func (r *statsReport) writeMarkdown(w io.Writer) {
	fmt.Fprintf(w, "### `%s`\n\n", r.Root)
	fmt.Fprintf(w, "%d programs · %s · best of %d\n\n", len(r.Rows), r.Config.Label(), r.Runs)
	showExp := r.anyExpected()
	if showExp {
		fmt.Fprintln(w, "| Program | LOC | Stages | Runtime | Passes | ✓ |")
		fmt.Fprintln(w, "|---|---:|---:|---:|---:|:-:|")
	} else {
		fmt.Fprintln(w, "| Program | LOC | Stages | Runtime | Passes |")
		fmt.Fprintln(w, "|---|---:|---:|---:|---:|")
	}
	for _, row := range r.Rows {
		runtime := interp.FormatDuration(row.Wall)
		if row.Status != "" {
			runtime = row.Status
		}
		line := fmt.Sprintf("| `%s` | %d | %d | %s | %d |",
			row.Name, row.LOC, row.Stages, runtime, len(row.Passes))
		if showExp {
			line = strings.TrimSuffix(line, "|") + fmt.Sprintf("| %s |", expectedMark(row.Expected))
		}
		fmt.Fprintln(w, line)
	}
	loc, stages, wall, passes, ok, of := r.totals()
	line := fmt.Sprintf("| **%d programs** | **%d** | **%d** | **%s** | **%d** |",
		len(r.Rows), loc, stages, interp.FormatDuration(wall), passes)
	if showExp {
		line = strings.TrimSuffix(line, "|") + fmt.Sprintf("| **%d/%d** |", ok, of)
	}
	fmt.Fprintln(w, line)
}

type statRowJSON struct {
	Program    string   `json:"program"`
	Path       string   `json:"path"`
	LOC        int      `json:"loc"`
	Lines      int      `json:"lines"`
	Stages     int      `json:"stages"`
	Passes     []string `json:"passes"`
	WallNanos  int64    `json:"wall_nanos,omitempty"`
	BuildNanos int64    `json:"build_nanos,omitempty"`
	Expected   *bool    `json:"expected_match,omitempty"`
	Status     string   `json:"status,omitempty"`
}

type statsJSON struct {
	Root     string        `json:"root"`
	Config   string        `json:"config"`
	Runs     int           `json:"runs"`
	Programs []statRowJSON `json:"programs"`
	Totals   struct {
		LOC       int   `json:"loc"`
		Stages    int   `json:"stages"`
		WallNanos int64 `json:"wall_nanos"`
		Rewrites  int   `json:"rewrites"`
		Matched   int   `json:"expected_matched"`
		OfN       int   `json:"expected_total"`
	} `json:"totals"`
	Vocabulary struct {
		Primitives int `json:"primitives_used"`
		OfPrims    int `json:"primitives_total"`
		Builtins   int `json:"builtins_used"`
		OfBuiltins int `json:"builtins_total"`
	} `json:"vocabulary"`
}

func (r *statsReport) jsonShape() statsJSON {
	out := statsJSON{Root: r.Root, Config: r.Config.Label(), Runs: r.Runs}
	for _, row := range r.Rows {
		j := statRowJSON{
			Program: row.Name, Path: row.Path, LOC: row.LOC, Lines: row.Lines,
			Stages: row.Stages, Passes: row.Passes, Expected: row.Expected, Status: row.Status,
		}
		if j.Passes == nil {
			j.Passes = []string{}
		}
		if row.Wall > 0 {
			j.WallNanos = row.Wall.Nanoseconds()
		}
		if row.Build > 0 {
			j.BuildNanos = row.Build.Nanoseconds()
		}
		out.Programs = append(out.Programs, j)
	}
	loc, stages, wall, passes, ok, of := r.totals()
	out.Totals.LOC, out.Totals.Stages = loc, stages
	out.Totals.WallNanos = wall.Nanoseconds()
	out.Totals.Rewrites, out.Totals.Matched, out.Totals.OfN = passes, ok, of
	out.Vocabulary.Primitives = countInCatalog(r.Vocab.Prims)
	out.Vocabulary.OfPrims = len(prims.AllPrims())
	out.Vocabulary.Builtins = len(r.Vocab.Builtins)
	out.Vocabulary.OfBuiltins = len(prims.AllBuiltins())
	return out
}
