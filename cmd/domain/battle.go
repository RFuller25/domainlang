// `domain expansion: battle <a.domain> [--lang L] <b>` — two programs, one
// input, one required answer, raced against each other.
//
// The command publishes a head-to-head number, which is the most contestable
// thing this repo produces. Two things follow from that, and they are the
// whole design:
//
// **Correctness gates the race.** Both programs run once and their output is
// compared before any timing is reported. If they disagree, no winner is
// declared at all — a faster program that prints the wrong answer has not come
// out on top, and reporting a time for it would be the single most misleading
// thing a head-to-head could do.
//
// **Every rule the numbers depend on is stated on the verdict screen**, so the
// result can be argued with rather than merely believed: how many runs, which
// side compiled, whether build time is counted, and what the input was.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"domain/interp"
	"domain/langs"
	"domain/runner"
)

type battleOptions struct {
	Lang           string
	Input          string
	InputText      string
	Runs           int
	Timeout        time.Duration
	Interpret      bool // race the interpreter rather than a compiled binary
	ChallengerArgs []string
	JSON           bool
	Plain          bool
}

func parseBattleArgs(args []string) (string, string, battleOptions, error) {
	opts := battleOptions{}
	var files []string
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
		case a == "--lang" || a == "-l":
			opts.Lang, err = next(a)
		case strings.HasPrefix(a, "--lang="):
			opts.Lang = strings.TrimPrefix(a, "--lang=")
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
		case a == "--interpret":
			opts.Interpret = true
		case a == "--challenger-args":
			var s string
			if s, err = next(a); err == nil {
				opts.ChallengerArgs = strings.Fields(s)
			}
		case strings.HasPrefix(a, "--challenger-args="):
			opts.ChallengerArgs = strings.Fields(strings.TrimPrefix(a, "--challenger-args="))
		case a == "--json":
			opts.JSON = true
		case a == "--plain":
			opts.Plain = true
		default:
			if strings.HasPrefix(a, "-") {
				return "", "", opts, fmt.Errorf("unknown flag %q", a)
			}
			files = append(files, a)
		}
		if err != nil {
			return "", "", opts, err
		}
	}
	if len(files) < 2 {
		return "", "", opts, fmt.Errorf("battle needs two programs: <a.domain> [--lang L] <b>")
	}
	if len(files) > 2 {
		return "", "", opts, fmt.Errorf("battle takes two programs, got %d", len(files))
	}
	if opts.Input != "" && opts.InputText != "" {
		return "", "", opts, fmt.Errorf("--input and --input-text both say what the programs read; pick one")
	}
	if opts.Runs < 0 {
		return "", "", opts, fmt.Errorf("--runs cannot be negative")
	}
	return files[0], files[1], opts, nil
}

// challengerSpec resolves the challenger's language: --lang when given, else
// inferred from the filename. Being told and guessing are kept apart in the
// error, because "I could not tell what language this is" and "I do not know
// that language" are different problems with different fixes.
func challengerSpec(path string, opts battleOptions) (langs.Spec, error) {
	if opts.Lang != "" {
		spec, ok := langs.Lookup(opts.Lang)
		if !ok {
			return langs.Spec{}, fmt.Errorf("unknown language %q; known: %s",
				opts.Lang, strings.Join(langs.Names(), ", "))
		}
		return spec, nil
	}
	if spec, ok := langs.ByExt(path); ok {
		return spec, nil
	}
	return langs.Spec{}, fmt.Errorf(
		"cannot tell what language %s is written in; name it with --lang (one of: %s)",
		filepath.Base(path), strings.Join(langs.Names(), ", "))
}

// Battle races two programs and reports.
func Battle(domainPath, challengerPath string, opts battleOptions, stdout, stderr io.Writer) int {
	for _, p := range []string{domainPath, challengerPath} {
		if _, err := os.Stat(p); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
	}
	spec, err := challengerSpec(challengerPath, opts)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 2
	}
	argv, err := spec.CommandFor(challengerPath)
	if err != nil {
		// A missing runtime is not a failure of anyone's program, so it is
		// reported as the setup problem it is and nothing is raced.
		var missing *langs.NotInstalledError
		if errors.As(err, &missing) {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	argv = append(argv, opts.ChallengerArgs...)
	defer runner.Cleanup()

	cfg := runner.Config{Compiled: !opts.Interpret, Optimize: true}
	side := "compiled"
	if opts.Interpret {
		side = "interpreted"
	}
	contestants := []runner.Contestant{
		{Label: filepath.Base(domainPath) + " (Domain, " + side + ")", Program: domainPath, Config: cfg},
		{Label: filepath.Base(challengerPath) + " (" + spec.Name + ")", Argv: argv, Dir: filepath.Dir(challengerPath)},
	}

	in, inputDesc := battleInput(domainPath, challengerPath, opts)
	ropts := runner.Options{Runs: opts.Runs, Timeout: opts.Timeout, DomainBin: measureDomainBin}

	// Stage 1: do they agree? Nothing is timed until they do.
	checks, err := runner.CheckAgreement(contestants, in, ropts)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	rep := &battleReport{
		Contestants: contestants,
		Input:       inputDesc,
		Runs:        runsOfBattle(opts),
		Lang:        spec.Name,
		Interpreted: opts.Interpret,
		Answer:      battleAnswer(checks[0].Stdout),
	}
	if d := runner.FindDisagreement(checks); d != nil {
		rep.Disagreement = d
		rep.Results = checks
		rep.write(stdout, opts)
		return 1
	}

	// Stage 2: the race.
	results, err := runner.RaceContestants(contestants, in, ropts)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	rep.Results = results
	// A side that agreed on the check but broke during the race is still a
	// disagreement — reported rather than raced past.
	if d := runner.FindDisagreement(results); d != nil {
		rep.Disagreement = d
		rep.write(stdout, opts)
		return 1
	}
	rep.write(stdout, opts)
	return 0
}

func runsOfBattle(opts battleOptions) int {
	if opts.Runs <= 0 {
		return runner.DefaultRuns
	}
	return opts.Runs
}

// battleInput decides what both programs read. Both sides get the same bytes;
// that is the one thing a race cannot compromise on.
func battleInput(domainPath, challengerPath string, opts battleOptions) (runner.Input, string) {
	if opts.InputText != "" {
		return runner.Input{Bytes: []byte(opts.InputText)}, "--input-text"
	}
	if opts.Input != "" {
		return runner.Input{Path: opts.Input}, opts.Input
	}
	if sib := siblingInput(domainPath); sib != "" {
		return runner.Input{Path: sib}, sib + " (found beside the Domain program)"
	}
	if sib := siblingInput(challengerPath); sib != "" {
		return runner.Input{Path: sib}, sib + " (found beside the challenger)"
	}
	return runner.Input{}, "none"
}

// battleAnswer is the program's answer as one line, for the verdict header —
// a multi-line answer is elided rather than pasted into the banner.
func battleAnswer(b []byte) string {
	return firstLine(strings.TrimRight(string(b), "\n"))
}

// ---------------------------------------------------------------------------
// The verdict
// ---------------------------------------------------------------------------

type battleReport struct {
	Contestants  []runner.Contestant
	Results      []runner.Result
	Input        string
	Runs         int
	Lang         string
	Interpreted  bool
	Answer       string
	Disagreement *runner.Disagreement
}

func (r *battleReport) write(w io.Writer, opts battleOptions) {
	if opts.JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r.jsonShape())
		return
	}
	r.writePlain(w)
}

func (r *battleReport) writePlain(w io.Writer) {
	// A no-contest is reported without any timings at all. The disagreement is
	// usually found by the single agreement check, before the race has run —
	// so there is no best-of-N to report, and printing the check's one run
	// under a "best of 5" header would be inventing a measurement that was
	// never taken. There is also nothing to compare: the two programs did not
	// answer the same question.
	if r.Disagreement != nil {
		fmt.Fprintf(w, "input: %s\n\n", r.Input)
		r.writeDisagreement(w)
		return
	}

	fmt.Fprintf(w, "input: %s · best of %d\n\n", r.Input, r.Runs)

	for i := range r.Results {
		res := &r.Results[i]
		fmt.Fprintf(w, "  %s\n", r.Contestants[i].Label)
		fmt.Fprintf(w, "    run     %s\n", battleTime(res))
		if res.Build > 0 {
			fmt.Fprintf(w, "    build   %s\n", interp.FormatDuration(res.Build))
			fmt.Fprintf(w, "    first   %s  (build + run — what you wait for the first time)\n",
				interp.FormatDuration(res.Build+res.Wall))
		}
		if res.Alloc.PeakRSS > 0 {
			fmt.Fprintf(w, "    peak    %s\n", formatBytes(uint64(res.Alloc.PeakRSS)))
		}
		if res.Err != nil {
			fmt.Fprintf(w, "    error   %v\n", res.Err)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "  output ✓ identical (%s)\n\n", r.Answer)
	r.writeVerdict(w)
	r.writeRules(w)
}

func (r *battleReport) writeDisagreement(w io.Writer) {
	d := r.Disagreement
	fmt.Fprintln(w, "  NO CONTEST — the two programs did not compute the same thing.")
	fmt.Fprintln(w)
	switch {
	case d.BFailed && d.AFailed:
		fmt.Fprintf(w, "    %s failed: %s\n", r.label(d.A), d.FailReason)
	case d.BFailed:
		fmt.Fprintf(w, "    %s failed: %s\n", r.label(d.B), d.FailReason)
		if s := strings.TrimSpace(string(r.Results[d.B].Stderr)); s != "" {
			fmt.Fprintf(w, "      %s\n", firstLine(s))
		}
	default:
		fmt.Fprintf(w, "    first difference at line %d:\n", d.Line)
		width := max(len(r.label(d.A)), len(r.label(d.B))) + 1
		fmt.Fprintf(w, "      %-*s %q\n", width, r.label(d.A)+":", d.AText)
		fmt.Fprintf(w, "      %-*s %q\n", width, r.label(d.B)+":", d.BText)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  No winner is declared. A faster program that prints a different")
	fmt.Fprintln(w, "  answer has not come out on top.")
}

func (r *battleReport) label(i int) string {
	if i < 0 || i >= len(r.Contestants) {
		return "?"
	}
	return r.Contestants[i].Label
}

// writeVerdict names the winner on both clocks, because they can disagree and
// showing only one would be taking a position. `run` is the compute; `first
// answer` includes the build, which the compiled side pays and an interpreted
// challenger does not.
func (r *battleReport) writeVerdict(w io.Writer) {
	if len(r.Results) != 2 {
		return
	}
	a, b := &r.Results[0], &r.Results[1]
	if a.Wall <= 0 || b.Wall <= 0 {
		fmt.Fprintln(w, "  no verdict: one side produced no time")
		return
	}
	winner, loser := 0, 1
	if b.Wall < a.Wall {
		winner, loser = 1, 0
	}
	ratio := runner.Speedup(r.Results[loser].Wall, r.Results[winner].Wall)
	fmt.Fprintf(w, "  %s WINS — %.1f× faster on the run\n", strings.ToUpper(r.label(winner)), ratio)

	firstA, firstB := a.Build+a.Wall, b.Build+b.Wall
	fw, fl := 0, 1
	if firstB < firstA {
		fw, fl = 1, 0
	}
	firstRatio := runner.Speedup(
		r.Results[fl].Build+r.Results[fl].Wall,
		r.Results[fw].Build+r.Results[fw].Wall)
	if fw != winner {
		fmt.Fprintf(w, "  %s wins to first answer — %.1f× (the build is the difference)\n",
			r.label(fw), firstRatio)
	} else if a.Build > 0 || b.Build > 0 {
		fmt.Fprintf(w, "  and %.1f× to first answer, build included\n", firstRatio)
	}
	fmt.Fprintln(w)
}

// writeRules states what the numbers rest on, so the result can be argued
// with rather than merely believed.
func (r *battleReport) writeRules(w io.Writer) {
	side := "compiled and optimized (`domain build`), which is what the language ships"
	if r.Interpreted {
		side = "interpreted (`domain run`) — --interpret was given"
	}
	fmt.Fprintln(w, "  how this was measured:")
	fmt.Fprintf(w, "    · both sides are subprocesses, both read the input as a redirected file\n")
	fmt.Fprintf(w, "    · best of %d, alternating, so drift lands on both sides\n", r.Runs)
	fmt.Fprintf(w, "    · the Domain side is %s\n", side)
	fmt.Fprintf(w, "    · the %s side runs as its own runtime runs it, with no added flags\n", r.Lang)
	fmt.Fprintf(w, "    · build time is reported separately and is not counted in the run\n")
}

func battleTime(res *runner.Result) string {
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

type battleSideJSON struct {
	Label      string `json:"label"`
	WallNanos  int64  `json:"wall_nanos,omitempty"`
	BuildNanos int64  `json:"build_nanos,omitempty"`
	PeakRSS    int64  `json:"peak_rss_bytes,omitempty"`
	ExitCode   int    `json:"exit_code"`
	Timeout    bool   `json:"timeout,omitempty"`
	Error      string `json:"error,omitempty"`
}

type battleJSON struct {
	Input        string           `json:"input"`
	Runs         int              `json:"runs"`
	Language     string           `json:"challenger_language"`
	Agreed       bool             `json:"agreed"`
	Answer       string           `json:"answer,omitempty"`
	Disagreement string           `json:"disagreement,omitempty"`
	Winner       string           `json:"winner,omitempty"`
	Speedup      float64          `json:"speedup,omitempty"`
	Sides        []battleSideJSON `json:"sides"`
}

func (r *battleReport) jsonShape() battleJSON {
	out := battleJSON{
		Input: r.Input, Runs: r.Runs, Language: r.Lang,
		Agreed: r.Disagreement == nil, Answer: r.Answer,
	}
	if r.Disagreement != nil {
		out.Answer = ""
		d := r.Disagreement
		if d.BFailed {
			out.Disagreement = fmt.Sprintf("%s failed: %s", r.label(d.B), d.FailReason)
		} else {
			out.Disagreement = fmt.Sprintf("line %d differs: %q vs %q", d.Line, d.AText, d.BText)
		}
	}
	for i := range r.Results {
		res := &r.Results[i]
		s := battleSideJSON{
			Label: r.label(i), ExitCode: res.ExitCode, Timeout: res.Timeout,
			PeakRSS: res.Alloc.PeakRSS,
		}
		if res.Wall > 0 {
			s.WallNanos = res.Wall.Nanoseconds()
		}
		if res.Build > 0 {
			s.BuildNanos = res.Build.Nanoseconds()
		}
		if res.Err != nil {
			s.Error = res.Err.Error()
		}
		out.Sides = append(out.Sides, s)
	}
	if out.Agreed && len(r.Results) == 2 && r.Results[0].Wall > 0 && r.Results[1].Wall > 0 {
		w, l := 0, 1
		if r.Results[1].Wall < r.Results[0].Wall {
			w, l = 1, 0
		}
		out.Winner = r.label(w)
		out.Speedup = runner.Speedup(r.Results[l].Wall, r.Results[w].Wall)
	}
	return out
}
