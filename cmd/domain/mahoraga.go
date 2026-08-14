// `domain expansion: mahoraga <file.domain> <input> <expected>` — adapting one
// program to one input.
//
// The search engine is package mahoraga; this file is argument parsing, the
// plain-text reporter and the final verdict. The wheel — eight handles that
// light as turns adapt — is mahoraga_wheel.go, and it consumes the same event
// stream, so the search never knows which of the two is watching.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"domain/interp"
	"domain/mahoraga"
)

type mahoragaOptions struct {
	Out          string
	Recipe       string
	Turns        int
	BaselineRuns int
	ScreenRuns   int
	MinEffect    float64
	Tier         mahoraga.Tier
	Timeout      time.Duration
	Seed         int64
	JSON         bool
	Plain        bool

	// Quiet drops the running commentary and prints only the verdict.
	//
	// It is a third point on the same axis rather than a variant of --plain.
	// The wheel is for watching a search; --plain is for reading one line per
	// candidate as it happens; --quiet is for a script that wants the result
	// and would otherwise scroll fifty rejections past a log. The verdict is
	// never suppressed — it is the thing the command is for, and a flag that
	// silenced it would leave only an exit code to interpret.
	Quiet bool

	// Replay rebuilds from a recorded recipe instead of searching; Verify
	// only checks a recipe's contract against an input and reports.
	Replay string
	Verify string
}

func parseMahoragaArgs(args []string) (prog, input, expected string, opts mahoragaOptions, err error) {
	opts.Tier = mahoraga.Pinned
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
		var e error
		switch {
		case a == "-o" || a == "--out":
			opts.Out, e = next(a)
		case strings.HasPrefix(a, "--out="):
			opts.Out = strings.TrimPrefix(a, "--out=")
		case a == "--recipe":
			opts.Recipe, e = next(a)
		case strings.HasPrefix(a, "--recipe="):
			opts.Recipe = strings.TrimPrefix(a, "--recipe=")
		case a == "--turns":
			var s string
			if s, e = next(a); e == nil {
				opts.Turns, e = strconv.Atoi(s)
			}
		case strings.HasPrefix(a, "--turns="):
			opts.Turns, e = strconv.Atoi(strings.TrimPrefix(a, "--turns="))
		case a == "--runs":
			var s string
			if s, e = next(a); e == nil {
				opts.BaselineRuns, e = strconv.Atoi(s)
			}
		case strings.HasPrefix(a, "--runs="):
			opts.BaselineRuns, e = strconv.Atoi(strings.TrimPrefix(a, "--runs="))
		case a == "--screen-runs":
			var s string
			if s, e = next(a); e == nil {
				opts.ScreenRuns, e = strconv.Atoi(s)
			}
		case strings.HasPrefix(a, "--screen-runs="):
			opts.ScreenRuns, e = strconv.Atoi(strings.TrimPrefix(a, "--screen-runs="))
		case a == "--min-effect":
			var s string
			if s, e = next(a); e == nil {
				opts.MinEffect, e = strconv.ParseFloat(s, 64)
			}
		case strings.HasPrefix(a, "--min-effect="):
			opts.MinEffect, e = strconv.ParseFloat(strings.TrimPrefix(a, "--min-effect="), 64)
		case a == "--tier":
			var s string
			if s, e = next(a); e == nil {
				var ok bool
				if opts.Tier, ok = mahoraga.ParseTier(s); !ok {
					e = fmt.Errorf("--tier takes general, guarded or pinned (got %q)", s)
				}
			}
		case strings.HasPrefix(a, "--tier="):
			var ok bool
			if opts.Tier, ok = mahoraga.ParseTier(strings.TrimPrefix(a, "--tier=")); !ok {
				e = fmt.Errorf("--tier takes general, guarded or pinned")
			}
		case a == "--timeout":
			var s string
			if s, e = next(a); e == nil {
				opts.Timeout, e = time.ParseDuration(s)
			}
		case strings.HasPrefix(a, "--timeout="):
			opts.Timeout, e = time.ParseDuration(strings.TrimPrefix(a, "--timeout="))
		case a == "--seed":
			var s string
			if s, e = next(a); e == nil {
				opts.Seed, e = strconv.ParseInt(s, 10, 64)
			}
		case strings.HasPrefix(a, "--seed="):
			opts.Seed, e = strconv.ParseInt(strings.TrimPrefix(a, "--seed="), 10, 64)
		case a == "--replay":
			opts.Replay, e = next(a)
		case strings.HasPrefix(a, "--replay="):
			opts.Replay = strings.TrimPrefix(a, "--replay=")
		case a == "--verify":
			opts.Verify, e = next(a)
		case strings.HasPrefix(a, "--verify="):
			opts.Verify = strings.TrimPrefix(a, "--verify=")
		case a == "--json":
			opts.JSON = true
		case a == "--plain":
			opts.Plain = true
		case a == "--quiet" || a == "-q":
			opts.Quiet = true
		default:
			if strings.HasPrefix(a, "-") {
				return "", "", "", opts, fmt.Errorf("unknown flag %q", a)
			}
			files = append(files, a)
		}
		if e != nil {
			return "", "", "", opts, e
		}
	}
	// --replay and --verify work from the recipe, which already records the
	// program, the input and the expected output, so the three files become
	// optional overrides rather than requirements.
	if opts.Replay != "" || opts.Verify != "" {
		switch len(files) {
		case 0:
		case 1:
			input = files[0]
		case 2:
			input, expected = files[0], files[1]
		default:
			return "", "", "", opts, fmt.Errorf(
				"with --replay or --verify, give at most <input> and <expected>")
		}
		return "", input, expected, opts, nil
	}
	if len(files) != 3 {
		return "", "", "", opts, fmt.Errorf(
			"mahoraga needs three files: <file.domain> <input> <expected> (got %d)", len(files))
	}
	if opts.BaselineRuns < 0 || opts.ScreenRuns < 0 {
		return "", "", "", opts, fmt.Errorf("run counts cannot be negative")
	}
	return files[0], files[1], files[2], opts, nil
}

// Mahoraga runs the search — or replays a recipe, or verifies one — and
// reports.
//
// There are two faces on the same search. On a terminal it is the wheel
// (mahoraga_wheel.go): eight handles, one per turn, lighting as they adapt.
// Everywhere else — `--plain`, `--json`, a pipe, CI — it is a line per event.
// The search is identical either way; it emits mahoraga.Event and never learns
// which of the two is watching.
func Mahoraga(prog, input, expected string, opts mahoragaOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	if opts.Verify != "" {
		return mahoragaVerify(opts, input, stdout, stderr)
	}
	if opts.Replay != "" {
		return mahoragaReplay(opts, input, expected, stdout, stderr)
	}
	sopts := mahoraga.Options{
		Program: prog, Input: input, Expected: expected,
		BaselineRuns: opts.BaselineRuns, ScreenRuns: opts.ScreenRuns,
		MinEffect: opts.MinEffect, Turns: opts.Turns, Tier: opts.Tier,
		Timeout: opts.Timeout, Seed: opts.Seed,
		Out:    orDefault(opts.Out, mahoraga.DefaultOut(prog)),
		Recipe: orDefault(opts.Recipe, mahoraga.DefaultRecipe(prog)),
	}

	wheel := wantsWheel(opts, stdin, stdout)
	var rep mahoraga.Reporter
	var bridge *wheelBridge
	switch {
	case wheel:
		bridge = newWheelBridge()
		rep = bridge
	case opts.JSON || opts.Quiet:
		// No reporter at all. The search runs identically — it only ever calls
		// Report through a nil check — so quiet is genuinely the same search
		// with nothing watching, not a mode it has to know about.
	default:
		rep = newMahoragaPlainReporter(stdout)
	}

	search, err := mahoraga.NewSearch(sopts, rep)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	defer search.Close()

	var recipe *mahoraga.Recipe
	if wheel {
		var code int
		if recipe, code = runMahoragaWheel(search, bridge, prog, input, opts, stdin, stdout, stderr); code != 0 {
			return code
		}
	} else if recipe, err = search.Run(); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	if err := recipe.Write(sopts.Recipe); err != nil {
		fmt.Fprintf(stderr, "domain: writing the recipe: %v\n", err)
		return 1
	}

	if opts.JSON {
		if err := recipe.WriteJSON(stdout); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		return 0
	}
	// The verdict is printed after the wheel has given the terminal back. The
	// wheel draws on the alternate screen, so everything it showed is gone the
	// moment the program exits; what a reader keeps is what lands here.
	writeMahoragaVerdict(stdout, recipe, sopts)
	return 0
}

// wantsWheel decides whether this run gets the animation.
//
// Both ends have to be a terminal: the wheel reads keys from one and paints the
// other, and a search whose output is being captured must not emit a quarter of
// a million escape sequences into the capture.
func wantsWheel(opts mahoragaOptions, stdin io.Reader, stdout io.Writer) bool {
	if opts.Plain || opts.JSON || opts.Quiet {
		return false
	}
	in, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(in.Fd()) {
		return false
	}
	out, ok := stdout.(*os.File)
	return ok && term.IsTerminal(out.Fd())
}

// mahoragaVerify answers "can I still use this binary?" without building.
func mahoragaVerify(opts mahoragaOptions, input string, stdout, stderr io.Writer) int {
	r, err := mahoraga.ReadRecipe(opts.Verify)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	if input == "" {
		input = r.Input
	}
	v := mahoraga.Verify(r, input)
	switch {
	case v.Matches:
		fmt.Fprintf(stdout, "  ✓ %s is the input this recipe was adapted to\n", input)
		return 0
	case v.Safe:
		// Safe means nothing here is *pinned*. Guarded adaptations may well be
		// in the recipe and they keep a fallback, so the binary is still
		// correct — it may simply no longer be faster. Claiming everything is
		// general-tier, as this used to, would be saying more than Verify checked.
		fmt.Fprintf(stdout, "  · %s is a different input. Nothing in this recipe is pinned\n", input)
		fmt.Fprintf(stdout, "    to the original, so the binary is still correct here:\n")
	default:
		fmt.Fprintf(stdout, "  ✗ %s is outside this recipe's contract:\n", input)
	}
	for _, why := range v.Reasons {
		fmt.Fprintf(stdout, "      %s\n", why)
	}
	if !v.Safe {
		return 1
	}
	return 0
}

// mahoragaReplay rebuilds the adapted binary from a recipe.
func mahoragaReplay(opts mahoragaOptions, input, expected string, stdout, stderr io.Writer) int {
	r, err := mahoraga.ReadRecipe(opts.Replay)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	out := orDefault(opts.Out, orDefault(r.Artifact, mahoraga.DefaultOut(r.Program)))
	if err := mahoraga.Replay(r, input, expected, out); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "  rebuilt from %s\n", opts.Replay)
	fmt.Fprintf(stdout, "  %d adaptation(s) reapplied, output re-checked\n", len(r.Kept()))
	fmt.Fprintf(stdout, "  binary  %s\n", out)
	return 0
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// ---------------------------------------------------------------------------
// The plain reporter
// ---------------------------------------------------------------------------

// mahoragaPlainReporter prints one line per event. It is what CI and the tests
// consume, and what runs when there is no terminal to animate.
type mahoragaPlainReporter struct{ w io.Writer }

func newMahoragaPlainReporter(w io.Writer) *mahoragaPlainReporter {
	return &mahoragaPlainReporter{w: w}
}

func (r *mahoragaPlainReporter) Report(e mahoraga.Event) {
	switch e.Kind {
	case mahoraga.TurnStart:
		fmt.Fprintf(r.w, "\nturn %d — %s\n", e.Turn, e.TurnName)
	case mahoraga.CandidateStart:
		if e.Total > 1 {
			fmt.Fprintf(r.w, "  [%d/%d] %s\n", e.Index, e.Total, e.Candidate)
		} else if e.Turn != 1 {
			fmt.Fprintf(r.w, "  %s\n", e.Candidate)
		}
	case mahoraga.CandidateMeasured:
		if e.Turn == 1 {
			m := e.Measurement
			fmt.Fprintf(r.w, "  baseline  %s mean · %s min · ±%s over %d runs\n",
				interp.FormatDuration(m.Mean), interp.FormatDuration(m.Min),
				interp.FormatDuration(m.StdDev), m.Runs)
		}
	case mahoraga.Adapted:
		// Both sides of the race, not the candidate's figure alone. A single
		// absolute number invites comparison with a baseline measured minutes
		// earlier, which is how a 842ms candidate came to be reported as a win
		// beside a 713ms baseline; the pair makes the drift visible instead.
		fmt.Fprintf(r.w, "    ✓ adapted — %.1f%% faster (%s → %s, raced alternately), tier %s\n",
			e.Effect*100, interp.FormatDuration(e.Champion.Mean),
			interp.FormatDuration(e.Measurement.Mean), e.Tier)
	case mahoraga.Rejected:
		fmt.Fprintf(r.w, "    · %s\n", e.Reason)
	}
}

// writeMahoragaVerdict is the final report.
//
// It has to be able to say "baseline unbeaten" without embarrassment: a tuner
// that always reports a win is not measuring, and the search having found
// nothing is a real and informative outcome.
func writeMahoragaVerdict(w io.Writer, r *mahoraga.Recipe, opts mahoraga.Options) {
	fmt.Fprintf(w, "\n%s\n", strings.Repeat("─", 66))
	base := time.Duration(r.FinalBaseline.MeanNanos)
	champ := time.Duration(r.Champion.MeanNanos)
	kept := r.Kept()

	switch {
	case r.Overturned():
		fmt.Fprintf(w, "  BASELINE UNBEATEN — and the search thought otherwise.\n\n")
		fmt.Fprintf(w, "  Candidates looked like wins during the search and did not survive\n")
		fmt.Fprintf(w, "  the final re-measurement. A champion picked across dozens of\n")
		fmt.Fprintf(w, "  measurements is partly picked for favourable noise, which is what\n")
		fmt.Fprintf(w, "  that measurement exists to catch. The baseline binary was written.\n\n")
		fmt.Fprintf(w, "  baseline   %s  ← this is what was written\n", interp.FormatDuration(base))
		fmt.Fprintf(w, "  overturned %s  when re-measured against it\n",
			interp.FormatDuration(time.Duration(r.OverturnedChampion.MeanNanos)))
	case len(kept) == 0:
		// Nothing was kept, so the champion *is* the baseline binary and the two
		// final figures are one program measured twice. Printing them as
		// "baseline" against "best" with a ratio would be inventing a comparison
		// — it read "best 1.95ms (0.95×)" for a binary identical to the one on
		// the line above. What the pair actually shows is how repeatable the
		// measurement was, so that is what it is labelled as.
		fmt.Fprintf(w, "  BASELINE UNBEATEN — the compiler had already found everything\n")
		fmt.Fprintf(w, "  I can find on this program and this input.\n\n")
		fmt.Fprintf(w, "  baseline  %s   ← nothing was adapted, so this is the binary\n",
			interp.FormatDuration(base))
		fmt.Fprintf(w, "  the same build measured twice at the end came to %s and %s\n",
			interp.FormatDuration(base), interp.FormatDuration(champ))
	case !r.Improved():
		fmt.Fprintf(w, "  BASELINE UNBEATEN — what was adapted did not clear the noise.\n\n")
		fmt.Fprintf(w, "  baseline  %s\n", interp.FormatDuration(base))
		fmt.Fprintf(w, "  best      %s  (%.2f×, inside the %.1f%% noise floor)\n",
			interp.FormatDuration(champ), r.Speedup, r.NoiseFloorPct)
	default:
		fmt.Fprintf(w, "  ADAPTED — %.2f× faster\n\n", r.Speedup)
		fmt.Fprintf(w, "  baseline  %s\n", interp.FormatDuration(base))
		fmt.Fprintf(w, "  adapted   %s\n", interp.FormatDuration(champ))
	}
	fmt.Fprintf(w, "  noise floor %.1f%% · re-measured against the baseline after the search\n", r.NoiseFloorPct)

	if len(kept) > 0 {
		fmt.Fprintf(w, "\n  adaptations kept (%d):\n", len(kept))
		for _, a := range kept {
			fmt.Fprintf(w, "    turn %d  %-34s %-8s %.1f%% faster\n", a.Turn, a.ID, a.Tier, a.EffectPct)
		}
	}
	tried := len(r.Adaptations) - len(kept)
	if tried > 0 {
		fmt.Fprintf(w, "\n  %d candidate(s) tried and rejected — see the recipe for each and why\n", tried)
	}
	// How quiet the machine was. It rejects nothing — the baseline runs in
	// every race, so each comparison is internal to the race making it — but a
	// reader deciding how much to trust a small effect should know that the box
	// was moving underneath it.
	if r.DriftedRaces > 0 {
		fmt.Fprintf(w, "  %d of %d races ran while the machine was more than %.0f%% away from\n",
			r.DriftedRaces, len(r.Adaptations), mahoraga.DriftNotable*100)
		fmt.Fprintf(w, "  where it started — the comparisons still hold, the box was just busy\n")
	}
	// A candidate that measured faster and still could not be told from the
	// champion is not a failed candidate; it is a question the measurement
	// budget could not answer. Saying so is the difference between "this does
	// not work here" and "ask again with more runs", and only one of those is
	// something the user can act on.
	if r.Inconclusive > 0 {
		fmt.Fprintf(w, "  %d looked faster and could not be distinguished from the champion —\n", r.Inconclusive)
		fmt.Fprintf(w, "  a quieter machine or `--runs %d` would settle them\n", 3*max(opts.BaselineRuns, mahoraga.DefaultBaselineRuns))
	}
	if r.TurnsSkipped > 0 {
		fmt.Fprintf(w, "  %d turn(s) of the wheel are not built yet\n", r.TurnsSkipped)
	}

	fmt.Fprintf(w, "\n  binary  %s\n", opts.Out)
	fmt.Fprintf(w, "  recipe  %s\n", opts.Recipe)

	// What this binary costs in generality is a question about the adaptations
	// that were *kept*, not about how far the search was allowed to go. A search
	// run at the default pinned tier that kept only general and guarded
	// adaptations produced a program that is correct on any input, and warning
	// about a pin nobody made would be the report inventing a cost.
	if !r.Improved() {
		return
	}
	switch r.HighestTier() {
	case mahoraga.Pinned:
		fmt.Fprintf(w, "\n  This binary is adapted to %s and is not a general program.\n", opts.Input)
		fmt.Fprintf(w, "  It carries no input check — that would cost time on every run —\n")
		fmt.Fprintf(w, "  so running it on different input may answer wrongly without saying so.\n")
		writeContract(w, r.Contract)
	case mahoraga.Guarded:
		fmt.Fprintf(w, "\n  Tuned for %s, and still correct on any input: every adaptation\n", opts.Input)
		fmt.Fprintf(w, "  kept here has a fallback. On different input it may simply be no\n")
		fmt.Fprintf(w, "  faster than the baseline.\n")
	}
}

// writeContract prints what a pinned binary requires of an input.
//
// The point of printing it is that a contract is a *narrower* claim than "only
// this file": an assumption like "every byte is one rune" is satisfied by any
// number of inputs, and a reader who can see the assumption can decide for
// themselves. The clauses nobody can re-check are listed separately, because
// they are the ones that really do bind the binary to one input.
func writeContract(w io.Writer, c mahoraga.Contract) {
	if c.Empty() {
		return
	}
	fmt.Fprintf(w, "\n  the contract, which `--verify <recipe> <input>` re-checks:\n")
	if c.ASCII {
		fmt.Fprintf(w, "    · every byte of the input decodes as one rune\n")
	}
	if c.MinSegments > 0 {
		fmt.Fprintf(w, "    · the input has at least %d segments\n", c.MinSegments)
	}
	for _, u := range c.Unverifiable {
		fmt.Fprintf(w, "    ✗ %s\n", u)
	}
	if len(c.Unverifiable) > 0 {
		fmt.Fprintf(w, "    (the ✗ clauses cannot be re-established without running the\n")
		fmt.Fprintf(w, "     program, so this recipe is bound to the input it was adapted to)\n")
	}
}

// mahoragaOut and mahoragaRecipe expose the resolved paths for tests.
func mahoragaOut(prog string, opts mahoragaOptions) string {
	return orDefault(opts.Out, mahoraga.DefaultOut(prog))
}

func mahoragaRecipePath(prog string, opts mahoragaOptions) string {
	return orDefault(opts.Recipe, mahoraga.DefaultRecipe(prog))
}
