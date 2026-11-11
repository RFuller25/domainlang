// `domain expansion: oracle <file>` — the differential check: run a program
// through both backends, each with the optimizer on and off, and say whether
// the four agree.
//
// `bench` makes the same comparison as a side effect of timing, and that is the
// wrong place to look for it. A correctness question should not cost five timed
// runs and an allocation pass per cell, should accept more than one input, and
// — the part bench cannot do in a footer — should say *which half of the
// compiler* is wrong. The grid is two independent axes, so a disagreement has a
// direction: naive against optimized is the optimizer's oracle, interpret
// against compile is the backends'. Reading the direction off the grid is the
// difference between "the cells disagree" and "an optimizer pass is unsound on
// this program".
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"domain/runner"
)

type oracleOptions struct {
	Inputs    []string
	InputText string
	Timeout   time.Duration
	Release   bool
	Cells     []runner.Config
	JSON      bool
	Plain     bool
}

func parseOracleArgs(args []string) (string, oracleOptions, error) {
	opts := oracleOptions{Cells: append([]runner.Config(nil), runner.Four...)}
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
			var s string
			if s, err = next(a); err == nil {
				opts.Inputs = append(opts.Inputs, s)
			}
		case strings.HasPrefix(a, "--input="):
			opts.Inputs = append(opts.Inputs, strings.TrimPrefix(a, "--input="))
		case a == "--input-text":
			opts.InputText, err = next(a)
		case strings.HasPrefix(a, "--input-text="):
			opts.InputText = strings.TrimPrefix(a, "--input-text=")
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
		return "", opts, fmt.Errorf("oracle needs a program file")
	}
	if len(opts.Inputs) > 0 && opts.InputText != "" {
		return "", opts, fmt.Errorf("--input and --input-text both say what the program reads; pick one")
	}
	if len(opts.Cells) < 2 {
		return "", opts, fmt.Errorf("--cells named one cell, and one cell has nothing to agree with")
	}
	if opts.Release {
		for i := range opts.Cells {
			opts.Cells[i].Release = true
		}
	}
	return path, opts, nil
}

// oracleCase is one input, and what the grid made of it.
type oracleCase struct {
	Input   string
	Results []runner.Result
	// Groups partitions the cells that ran by what they did: group[i] is the
	// index of the first cell whose outcome cell i shares, so cells that agree
	// carry the same number. -1 marks a cell the harness could not run.
	Groups  []int
	Verdict string // "agree", "disagree", or "unchecked"
	Blame   string // which half of the compiler a disagreement points at
}

// outcome is what a cell did, reduced to what two cells must share to agree.
//
// A cell that succeeded is its output, byte for byte — the same standard
// `bench` holds. A cell that failed is how it failed and nothing else: the two
// backends word a runtime error differently and flush what they had printed at
// different moments, and neither is a disagreement about the program.
func outcome(r *runner.Result) string {
	switch {
	case r.Timeout:
		return "timeout"
	case r.ExitCode != 0:
		return fmt.Sprintf("exit %d", r.ExitCode)
	}
	return "ok\x00" + string(r.Stdout)
}

func (c *oracleCase) judge() {
	c.Groups = make([]int, len(c.Results))
	ran, distinct := 0, 0
	for i := range c.Results {
		if c.Results[i].Err != nil {
			c.Groups[i] = -1
			continue
		}
		ran++
		c.Groups[i] = i
		for j := 0; j < i; j++ {
			if c.Groups[j] == j && outcome(&c.Results[j]) == outcome(&c.Results[i]) {
				c.Groups[i] = j
				break
			}
		}
		if c.Groups[i] == i {
			distinct++
		}
	}
	switch {
	case distinct > 1:
		c.Verdict = "disagree"
		c.Blame = c.blame()
	case ran < 2:
		c.Verdict = "unchecked"
	default:
		c.Verdict = "agree"
	}
}

// blame reads the direction of a disagreement off the grid.
//
// Two cells that differ only in the optimizer are compared for each backend,
// and two that differ only in the backend for each optimizer setting. Whichever
// axis every difference lies along names the component; when both carry one,
// no single component explains it and the report says so rather than guessing.
func (c *oracleCase) blame() string {
	optimizer, backend := false, false
	for i := range c.Results {
		for j := i + 1; j < len(c.Results); j++ {
			if c.Groups[i] < 0 || c.Groups[j] < 0 || c.Groups[i] == c.Groups[j] {
				continue
			}
			a, b := c.Results[i].Config, c.Results[j].Config
			switch {
			case a.Compiled == b.Compiled && a.Optimize != b.Optimize:
				optimizer = true
			case a.Compiled != b.Compiled && a.Optimize == b.Optimize:
				backend = true
			}
		}
	}
	switch {
	case optimizer && !backend:
		return "optimizer"
	case backend && !optimizer:
		return "backend"
	}
	return "both"
}

func blameText(blame string) []string {
	switch blame {
	case "optimizer":
		return []string{
			"the optimizer changed this program's answer: the naive and optimized",
			"pipelines differ on the same backend, so a pass is unsound here.",
			"`domain run <file> --explain` lists the rewrites that applied.",
		}
	case "backend":
		return []string{
			"the backends disagree: the compiled binary and the interpreter differ",
			"on the same pipeline, so a codegen case does not match its primitive.",
			"`domain build <file> --emit-go -` prints the Go that was compiled.",
		}
	}
	return []string{
		"the cells differ along both axes, so no single component explains it:",
		"the optimizer and a backend are each involved.",
	}
}

// Oracle runs the grid once per input and reports whether it agreed.
func Oracle(path string, opts oracleOptions, stdout, stderr io.Writer) int {
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	defer runner.Cleanup()

	type namedInput struct {
		in   runner.Input
		desc string
	}
	var inputs []namedInput
	for _, p := range opts.Inputs {
		if _, err := os.Stat(p); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		inputs = append(inputs, namedInput{runner.Input{Path: p}, p})
	}
	if len(inputs) == 0 {
		in, desc := benchInput(path, benchOptions{InputText: opts.InputText})
		inputs = append(inputs, namedInput{in, desc})
	}

	rep := &oracleReport{Program: path}
	for _, ni := range inputs {
		results, err := runner.Race(path, opts.Cells, ni.in, runner.Options{
			Runs:       1,
			Timeout:    opts.Timeout,
			KeepStdout: true,
			DomainBin:  measureDomainBin,
		})
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		c := oracleCase{Input: ni.desc, Results: results}
		c.judge()
		rep.Cases = append(rep.Cases, c)
	}

	if opts.JSON {
		if err := writeJSON(stdout, rep.jsonShape()); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
	} else {
		rep.writeTable(stdout)
	}
	if !rep.agreed() {
		return 1
	}
	return 0
}

type oracleReport struct {
	Program string
	Cases   []oracleCase
}

// agreed is the exit status: every input checked, and every check passed. An
// input nothing could be cross-checked on is a failure of the tool to answer,
// and exiting 0 on it would read as a pass in CI.
func (r *oracleReport) agreed() bool {
	for i := range r.Cases {
		if r.Cases[i].Verdict != "agree" {
			return false
		}
	}
	return true
}

func (r *oracleReport) writeTable(w io.Writer) {
	for i := range r.Cases {
		c := &r.Cases[i]
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s · input: %s\n\n", r.Program, c.Input)
		fmt.Fprintf(w, "  %-22s %-10s %s\n", "cell", "outcome", "output")
		fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 58))
		names := groupNames(c.Groups)
		for j := range c.Results {
			res := &c.Results[j]
			status, output := "ok", ""
			switch {
			case res.Err != nil:
				status, output = "no run", headLine(res.Err.Error())
			case res.Timeout:
				status = "timeout"
			case res.ExitCode != 0:
				status, output = fmt.Sprintf("exit %d", res.ExitCode), headLine(string(res.Stderr))
			default:
				output = describeOutput(res.Stdout)
			}
			group := " "
			if c.Verdict == "disagree" && c.Groups[j] >= 0 {
				group = names[c.Groups[j]]
			}
			fmt.Fprintf(w, "  %-22s %-10s %s %s\n", res.Config.Label(), status, group, truncateVis(output, 60))
		}
		fmt.Fprintln(w)

		switch c.Verdict {
		case "agree":
			if c.Results[firstRan(c)].Failed() {
				fmt.Fprintf(w, "  ✓ all %d cells agree — the program fails the same way in each\n", countRan(c))
			} else {
				fmt.Fprintf(w, "  ✓ all %d cells agree on the output\n", countRan(c))
			}
		case "unchecked":
			fmt.Fprintf(w, "  ✗ fewer than two cells ran, so nothing was cross-checked\n")
		case "disagree":
			fmt.Fprintf(w, "  ✗ COMPILER BUG: the cells do not agree\n")
			for _, line := range blameText(c.Blame) {
				fmt.Fprintf(w, "    %s\n", line)
			}
			c.writeDiffs(w, names)
		}
	}
	if len(r.Cases) > 1 {
		bad := 0
		for i := range r.Cases {
			if r.Cases[i].Verdict != "agree" {
				bad++
			}
		}
		fmt.Fprintf(w, "\n%d input(s) checked, %d failed\n", len(r.Cases), bad)
	}
}

// writeDiffs shows where each dissenting group first parts from the reference
// — the first cell that ran, which on the full grid is the naive interpreter,
// the configuration everything else is defined against.
func (c *oracleCase) writeDiffs(w io.Writer, names map[int]string) {
	ref := firstRan(c)
	seen := map[int]bool{ref: true}
	for j := range c.Results {
		g := c.Groups[j]
		if g < 0 || seen[g] {
			continue
		}
		seen[g] = true
		a, b := &c.Results[ref], &c.Results[g]
		fmt.Fprintf(w, "\n    %s (%s) against %s (%s)\n", names[ref], a.Config.Label(), names[g], b.Config.Label())
		if a.Failed() || b.Failed() {
			fmt.Fprintf(w, "      %s: %s\n      %s: %s\n",
				a.Config.Label(), outcomeWords(a), b.Config.Label(), outcomeWords(b))
			continue
		}
		d := runner.FindDisagreement([]runner.Result{*a, *b})
		if d == nil || d.Line == 0 {
			// Byte-different but line-identical: trailing whitespace.
			fmt.Fprintf(w, "      the outputs differ only in their trailing newlines (%d against %d bytes)\n",
				len(a.Stdout), len(b.Stdout))
			continue
		}
		fmt.Fprintf(w, "      line %d\n        %s: %q\n        %s: %q\n",
			d.Line, a.Config.Label(), d.AText, b.Config.Label(), d.BText)
	}
}

func outcomeWords(r *runner.Result) string {
	switch {
	case r.Timeout:
		return "did not finish"
	case r.ExitCode != 0:
		if msg := headLine(string(r.Stderr)); msg != "" {
			return fmt.Sprintf("exit %d — %s", r.ExitCode, msg)
		}
		return fmt.Sprintf("exit %d", r.ExitCode)
	}
	return "succeeded, " + describeOutput(r.Stdout)
}

func firstRan(c *oracleCase) int {
	for j := range c.Results {
		if c.Groups[j] >= 0 {
			return j
		}
	}
	return 0
}

func countRan(c *oracleCase) int {
	n := 0
	for _, g := range c.Groups {
		if g >= 0 {
			n++
		}
	}
	return n
}

// groupNames letters the groups in the order they first appear: A is always
// the reference cell's.
func groupNames(groups []int) map[int]string {
	names := map[int]string{}
	for _, g := range groups {
		if g < 0 {
			continue
		}
		if _, ok := names[g]; !ok {
			names[g] = string(rune('A' + len(names)))
		}
	}
	return names
}

func describeOutput(out []byte) string {
	if len(out) == 0 {
		return "(nothing printed)"
	}
	lines := strings.Count(strings.TrimRight(string(out), "\n"), "\n") + 1
	head := headLine(string(out))
	if len(head) > 28 {
		head = head[:27] + "…"
	}
	if lines == 1 {
		return head
	}
	return fmt.Sprintf("%s … (%d lines)", head, lines)
}

func headLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func (r *oracleReport) jsonShape() any {
	type cell struct {
		Cell     string `json:"cell"`
		Outcome  string `json:"outcome"`
		Group    string `json:"group,omitempty"`
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
		Error    string `json:"error,omitempty"`
	}
	type oneCase struct {
		Input   string `json:"input"`
		Verdict string `json:"verdict"`
		Blame   string `json:"blame,omitempty"`
		Cells   []cell `json:"cells"`
	}
	out := struct {
		Program string    `json:"program"`
		Agreed  bool      `json:"agreed"`
		Cases   []oneCase `json:"cases"`
	}{Program: r.Program, Agreed: r.agreed()}
	for i := range r.Cases {
		c := &r.Cases[i]
		names := groupNames(c.Groups)
		oc := oneCase{Input: c.Input, Verdict: c.Verdict, Blame: c.Blame}
		for j := range c.Results {
			res := &c.Results[j]
			cl := cell{Cell: res.Config.Label(), ExitCode: res.ExitCode, Stdout: string(res.Stdout)}
			switch {
			case res.Err != nil:
				cl.Outcome, cl.Error = "no run", res.Err.Error()
			case res.Timeout:
				cl.Outcome = "timeout"
			case res.ExitCode != 0:
				cl.Outcome, cl.Error = "failed", headLine(string(res.Stderr))
			default:
				cl.Outcome = "ok"
			}
			if c.Groups[j] >= 0 {
				cl.Group = names[c.Groups[j]]
			}
			oc.Cells = append(oc.Cells, cl)
		}
		out.Cases = append(out.Cases, oc)
	}
	return out
}
