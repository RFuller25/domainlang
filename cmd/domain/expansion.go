// The `domain expansion:` command family — the CLI face of the diagnostics
// engine (package diag):
//
//	domain expansion: diagnosis <file>        error list + fix suggestions, read-only
//	domain expansion: lint <file>             errors + style warnings + perf hints, read-only
//	domain expansion: fix <file>              apply confident fixes in place (.bak backup)
//	domain expansion: optimize <file>         optimization report + source rewrites (.bak backup)
//	domain expansion: bench <file>            four-cell timing and allocation report
//	domain expansion: coverage <folder>       what of the catalog a folder never exercises
//	domain expansion: stats <folder>          per-program runtime, LOC and passes, as a leaderboard
//	domain expansion: battle <a> <b>          race a Domain program against one in another language
//	domain expansion: mahoraga <f> <in> <out> adapt one program to one input, and record how
//	domain expansion: maximum compile <file>  fix → lint → optimize → compile → run
//	domain expansion: documentation [-p PORT] serve the docs website (default port 4444)
//	domain expansion: development [file]      edit a program in a terminal editor
//	domain expansion: vscode [--dir PATH]     install the VS Code extension this binary carries
//
// Both the shell-split form (`domain expansion: lint prog.domain`) and the
// quoted form (`domain "expansion: lint" prog.domain`) are accepted.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"domain/diag"
	"domain/optimizer"
)

// expansionCommands are the recognized command phrases, longest first so
// "maximum compile" wins over any future one-word prefix of it.
var expansionCommands = [][]string{
	{"maximum", "compile"},
	{"documentation"},
	{"development"},
	{"vscode"},
	{"visualize"},
	{"bench"},
	{"coverage"},
	{"stats"},
	{"battle"},
	{"mahoraga"},
	{"diagnosis"},
	{"lint"},
	{"optimize"},
	{"fix"},
}

// expansionInvocation detects a `domain expansion: ...` invocation. It
// returns the command words and the remaining arguments (file and flags).
func expansionInvocation(args []string) (cmd []string, rest []string, ok bool) {
	if len(args) == 0 {
		return nil, nil, false
	}
	head := strings.ToLower(args[0])
	var words []string
	switch {
	case head == "expansion:" || head == "expansion":
		words = args[1:]
	case strings.HasPrefix(head, "expansion:"): // quoted: "expansion: maximum compile"
		words = append(strings.Fields(args[0][len("expansion:"):]), args[1:]...)
	default:
		return nil, nil, false
	}
	for _, c := range expansionCommands {
		if matchWords(words, c) {
			return c, words[len(c):], true
		}
	}
	return nil, words, true // it was an expansion invocation, but the command is unknown
}

func matchWords(words, cmd []string) bool {
	if len(words) < len(cmd) {
		return false
	}
	for i, c := range cmd {
		if !strings.EqualFold(words[i], c) {
			return false
		}
	}
	return true
}

// Expansion runs one expansion command and returns the process exit code
// (0 success, 1 program error, 2 usage error).
func Expansion(cmd, rest []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if cmd == nil {
		if len(rest) == 0 {
			fmt.Fprintln(stderr, "domain: missing expansion command")
		} else {
			fmt.Fprintf(stderr, "domain: unknown expansion command %q\n", strings.Join(rest, " "))
		}
		fmt.Fprintln(stderr, "known: maximum compile, documentation, development, vscode, visualize, bench, coverage, stats, battle, mahoraga, lint, optimize, fix, diagnosis")
		return 2
	}

	name := strings.Join(cmd, " ")
	switch name {
	// `documentation` is the odd one out: it serves the docs website rather
	// than analyzing a program, so it takes an optional -p/--port flag and no
	// file. Handle it before the shared "one file, no flags" parsing below.
	case "documentation":
		fmt.Fprint(stdout, expansionBanner("documentation", isColorTerminal(stdout)))
		return cmdDocumentation(rest, stdout, stderr)

	// `vscode` installs the editor extension this binary carries. Like
	// `documentation` it takes flags and no file.
	case "vscode":
		fmt.Fprint(stdout, expansionBanner("vscode", isColorTerminal(stdout)))
		return cmdVSCode(rest, stdout, stderr)

	// `visualize` also takes flags of its own (--input, --max-steps, --plain),
	// so it is parsed before the shared "one file, no flags" rule below. It
	// prints no banner: the stepper owns the screen.
	// `development` is the editor. Like `visualize` it takes flags of its own
	// and prints no banner, and unlike every other command here its file
	// argument is optional — with none, it asks which program to open.
	case "development":
		path, opts, err := parseDevelopmentArgs(rest)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 2
		}
		return Development(path, opts, stdin, stdout, stderr)

	case "visualize":
		path, opts, err := parseVisualizeArgs(rest)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 2
		}
		return Visualize(path, opts, stdin, stdout, stderr)

	// The measurement commands take flags of their own too. Each prints its
	// banner only in the human-facing modes: --json and --markdown produce
	// output something else consumes, and a banner in front of it would make
	// the document malformed.
	case "bench":
		path, opts, err := parseBenchArgs(rest)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 2
		}
		if !opts.JSON && !opts.Markdown {
			fmt.Fprint(stdout, expansionBanner("bench", isColorTerminal(stdout) && !opts.Plain))
		}
		return Bench(path, opts, stdout, stderr)

	case "coverage":
		path, opts, err := parseCoverageArgs(rest)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 2
		}
		if !opts.JSON {
			fmt.Fprint(stdout, expansionBanner("coverage", isColorTerminal(stdout) && !opts.Plain))
		}
		return Coverage(path, opts, stdout, stderr)

	case "stats":
		path, opts, err := parseStatsArgs(rest)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 2
		}
		if !opts.JSON && !opts.Markdown {
			fmt.Fprint(stdout, expansionBanner("stats", isColorTerminal(stdout) && !opts.Plain))
		}
		return Stats(path, opts, stdout, stderr)

	case "battle":
		a, b, opts, err := parseBattleArgs(rest)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 2
		}
		if !opts.JSON {
			fmt.Fprint(stdout, expansionBanner("battle", isColorTerminal(stdout) && !opts.Plain))
		}
		return Battle(a, b, opts, stdout, stderr)

	case "mahoraga":
		prog, input, expected, opts, err := parseMahoragaArgs(rest)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 2
		}
		if !opts.JSON && !opts.Quiet {
			fmt.Fprint(stdout, expansionBanner("mahoraga", isColorTerminal(stdout) && !opts.Plain))
		}
		return Mahoraga(prog, input, expected, opts, stdin, stdout, stderr)
	}

	var path string
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(stderr, "domain: expansion commands take no flags (got %q)\n", a)
			return 2
		}
		if path != "" {
			fmt.Fprintf(stderr, "domain: unexpected extra argument %q\n", a)
			return 2
		}
		path = a
	}
	if path == "" {
		fmt.Fprintln(stderr, "domain: missing <file.domain>")
		return 2
	}
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "domain: reading %s: %v\n", path, err)
		return 1
	}
	color := isColorTerminal(stdout)
	fmt.Fprint(stdout, expansionBanner(name, color))

	switch name {
	case "diagnosis":
		return cmdDiagnosis(path, string(src), stdout, color)
	case "lint":
		return cmdLint(path, string(src), stdout, color)
	case "fix":
		return cmdFix(path, string(src), stdout, stderr, color)
	case "optimize":
		return cmdOptimize(path, string(src), stdout, stderr, color)
	case "maximum compile":
		return cmdMaximumCompile(path, string(src), stdin, stdout, stderr, color)
	}
	return 2
}

// renderDiag writes one rendered diagnostic and the blank line that separates
// it from the next — the shape every expansion command prints a diagnostic in.
func renderDiag(w io.Writer, d *diag.Diagnostic, path string, color bool) {
	fmt.Fprint(w, diag.Render(d, path, color))
	fmt.Fprintln(w)
}

// cmdDiagnosis reads the program and reports every error with suggestions —
// the read-only deep dive.
func cmdDiagnosis(path, src string, w io.Writer, color bool) int {
	rep := diag.Analyze(path, src)
	errs, warns, hints := rep.Counts()

	fmt.Fprintf(w, "Domain Expansion: Diagnosis — %s\n\n", path)
	if errs == 0 {
		fmt.Fprintln(w, "no errors found. The domain is stable.")
	}
	fixable := 0
	for i := range rep.Diags {
		d := &rep.Diags[i]
		renderDiag(w, d, path, color)
		if d.Severity == diag.Error && d.HasConfidentFix() {
			fixable++
		}
	}
	fmt.Fprintf(w, "%d error(s), %d warning(s), %d hint(s)\n", errs, warns, hints)
	if fixable > 0 {
		fmt.Fprintf(w, "%d error(s) are auto-fixable — run `domain expansion: fix %s`\n", fixable, path)
	}
	if errs > 0 {
		return 1
	}
	return 0
}

// cmdLint reports everything the analyzer and linter found, compactly.
func cmdLint(path, src string, w io.Writer, color bool) int {
	rep := diag.Analyze(path, src)
	for i := range rep.Diags {
		renderDiag(w, &rep.Diags[i], path, color)
	}
	errs, warns, hints := rep.Counts()
	if errs+warns+hints == 0 {
		fmt.Fprintf(w, "%s: clean — no errors, warnings, or hints\n", path)
	} else {
		fmt.Fprintf(w, "%d error(s), %d warning(s), %d hint(s)\n", errs, warns, hints)
	}
	if errs > 0 {
		return 1
	}
	return 0
}

// cmdFix applies every confident repair in place, backing the original up as
// <path>.bak first.
func cmdFix(path, src string, w, stderr io.Writer, color bool) int {
	res := diag.FixSrc(path, src)
	if len(res.Applied) == 0 {
		if len(res.Remaining) == 0 {
			fmt.Fprintf(w, "%s: nothing to fix\n", path)
			return 0
		}
		fmt.Fprintf(w, "%s: no automatic fixes available; %d error(s) need a human:\n\n", path, len(res.Remaining))
		for i := range res.Remaining {
			renderDiag(w, &res.Remaining[i], path, color)
		}
		return 1
	}
	if err := backupAndWrite(path, src, res.Fixed); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	fmt.Fprintf(w, "%s: applied %d fix(es) (original saved as %s.bak)\n", path, len(res.Applied), path)
	for i := range res.Applied {
		d := &res.Applied[i]
		fmt.Fprintf(w, "  line %d: %s\n", d.Pos.Line, d.Msg)
		if d.Help != "" {
			fmt.Fprintf(w, "          → %s\n", d.Help)
		}
	}
	if len(res.Remaining) > 0 {
		fmt.Fprintf(w, "\n%d error(s) could not be fixed automatically:\n\n", len(res.Remaining))
		for i := range res.Remaining {
			renderDiag(w, &res.Remaining[i], path, color)
		}
		return 1
	}
	return 0
}

// cmdOptimize requires a clean program, applies the source-level rewrites in
// place (.bak backup), and prints the full IR optimization report.
func cmdOptimize(path, src string, w, stderr io.Writer, color bool) int {
	rep := diag.Analyze(path, src)
	if errs, _, _ := rep.Counts(); errs > 0 {
		fmt.Fprintf(w, "%s: cannot optimize a broken program — %d error(s):\n\n", path, errs)
		for i := range rep.Diags {
			if rep.Diags[i].Severity == diag.Error {
				renderDiag(w, &rep.Diags[i], path, color)
			}
		}
		fmt.Fprintf(w, "run `domain expansion: fix %s` first\n", path)
		return 1
	}

	out, rewrites := diag.OptimizeSource(path, src)
	if len(rewrites) > 0 {
		if err := backupAndWrite(path, src, out); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		fmt.Fprintf(w, "%s: rewrote the source (%d change(s), original saved as %s.bak)\n", path, len(rewrites), path)
		for _, r := range rewrites {
			fmt.Fprintf(w, "  line %d: %s\n", r.Line, r.Desc)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintf(w, "%s: no source-level rewrites apply\n", path)
	}

	// The IR-level report: what the optimizer will do on every run/build.
	pipe, err := loadPipeline(path, false, false, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	irRewrites := optimizer.Optimize(pipe, true)
	if len(irRewrites) == 0 {
		fmt.Fprintln(w, "no further IR optimizations apply")
	} else {
		fmt.Fprintf(w, "IR optimizations applied on every run/build (%d):\n", len(irRewrites))
		for _, r := range irRewrites {
			fmt.Fprintf(w, "  %s\n", r.Message)
		}
	}
	return 0
}

// cmdMaximumCompile is the whole ritual: fix what can be fixed, report what
// the linter sees, then compile with full optimization and run immediately.
func cmdMaximumCompile(path, src string, stdin io.Reader, w, stderr io.Writer, color bool) int {
	fmt.Fprintf(w, "Domain Expansion: Maximum Compile — %s\n", path)

	// Stage 1: fix.
	res := diag.FixSrc(path, src)
	working := src
	if len(res.Applied) > 0 {
		if err := backupAndWrite(path, src, res.Fixed); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		working = res.Fixed
		fmt.Fprintf(w, "[fix] applied %d fix(es) (original saved as %s.bak)\n", len(res.Applied), path)
		for i := range res.Applied {
			fmt.Fprintf(w, "  line %d: %s\n", res.Applied[i].Pos.Line, res.Applied[i].Msg)
		}
	}
	if len(res.Remaining) > 0 {
		fmt.Fprintf(w, "[fix] %d error(s) could not be fixed automatically:\n\n", len(res.Remaining))
		for i := range res.Remaining {
			renderDiag(w, &res.Remaining[i], path, color)
		}
		return 1
	}

	// Stage 2: lint the (possibly fixed) source.
	rep := diag.Analyze(path, working)
	warned := false
	for i := range rep.Diags {
		if rep.Diags[i].Severity == diag.Error {
			continue // cannot happen when fix left nothing remaining, but stay safe
		}
		if !warned {
			fmt.Fprintln(w, "[lint]")
			warned = true
		}
		renderDiag(w, &rep.Diags[i], path, color)
	}

	// Stage 3+4: compile with the optimizer narrating, then run.
	fmt.Fprintln(w, "[compile]")
	err := Build(path, BuildOptions{Optimize: true, Explain: true, Run: true}, stdin, w, stderr)
	if err != nil {
		var xe *exitError
		if errors.As(err, &xe) {
			return xe.code
		}
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	return 0
}

// backupAndWrite saves the original source as <path>.bak, then writes the new
// content over path, preserving the file's permission bits.
func backupAndWrite(path, original, updated string) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path+".bak", []byte(original), mode); err != nil {
		return fmt.Errorf("writing backup %s.bak: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// isColorTerminal reports whether w is an interactive terminal that wants
// ANSI color (NO_COLOR and TERM=dumb are honored).
func isColorTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
