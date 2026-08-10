// Command domain is the CLI for the Domain language.
//
//	domain <file.domain>                 # interpret (bare file, no other args)
//	domain <file.domain> <args...>       # compile (any extra args select the compiler)
//	domain run <file.domain> [flags]     # interpret, explicitly (flags allowed)
//	domain build <file.domain> [flags]   # compile, explicitly
//	domain check <file.domain>           # typecheck only, run nothing
//
// A bare program file interprets it: lex -> parse -> lower/typecheck ->
// optimize -> interpret, printing the program's Reveal output to stdout.
// Any additional argument switches to the compiler backend, which hands the
// optimized IR to codegen and produces a standalone optimized binary. The
// explicit `run` and `build` subcommands remain for flagged interpretation
// (e.g. `domain run prog.domain --explain`) and scripts.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
	"domain/prims"
)

// Options controls a single run.
type Options struct {
	Explain  bool
	Optimize bool
	Release  bool // skip Binding Vows (debug-on / release-off)
	Stats    bool // report per-stage counts and timings on stderr
	Verbose  bool // with Stats: list nested (loop/channel/part) steps too
}

// BuildOptions controls a single build.
type BuildOptions struct {
	Explain  bool
	Optimize bool
	Release  bool   // compile Binding Vows out of the binary
	Out      string // binary output path; "" derives it from the source name
	EmitGo   string // also write the generated Go source here ("-" for stdout)
	Run      bool   // build, run with the current stdin, then clean up
}

// exitError carries a child process's exit code out of Build --run. The child
// already wrote its own error to stderr, so main exits with the code without
// printing a second message.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		path, opts, err := parseRunArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "domain: %v\n", err)
			os.Exit(2)
		}
		if err := Execute(path, opts, os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "domain: %v\n", err)
			os.Exit(1)
		}
	case "build":
		path, opts, err := parseBuildArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "domain: %v\n", err)
			os.Exit(2)
		}
		if err := Build(path, opts, os.Stdin, os.Stdout, os.Stderr); err != nil {
			exitBuildErr(err)
		}
	case "check":
		path, err := parseCheckArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "domain: %v\n", err)
			os.Exit(2)
		}
		if err := Check(path, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "domain: %v\n", err)
			os.Exit(1)
		}
	case "fmt":
		paths, opts, err := parseFmtArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "domain: %v\n", err)
			os.Exit(2)
		}
		os.Exit(Fmt(paths, opts, os.Stdin, os.Stdout, os.Stderr))
	case "repl":
		os.Exit(Repl(os.Stdin, os.Stdout))
	case "lsp":
		os.Exit(Lsp(os.Stdin, os.Stdout, os.Stderr))
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		// `domain expansion: <command>` — the diagnostics/lint/fix/optimize
		// family — is detected before the run/build fallback.
		if cmd, rest, ok := expansionInvocation(os.Args[1:]); ok {
			os.Exit(Expansion(cmd, rest, os.Stdin, os.Stdout, os.Stderr))
		}
		// No subcommand: a bare program file interprets it; any extra
		// argument means the compiler is wanted.
		args := os.Args[1:]
		if isImplicitRun(args) {
			if err := Execute(args[0], Options{Optimize: true}, os.Stdin, os.Stdout, os.Stderr); err != nil {
				fmt.Fprintf(os.Stderr, "domain: %v\n", err)
				os.Exit(1)
			}
			return
		}
		path, opts, err := parseBuildArgs(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "domain: %v\n", err)
			usage(os.Stderr)
			os.Exit(2)
		}
		if err := Build(path, opts, os.Stdin, os.Stdout, os.Stderr); err != nil {
			exitBuildErr(err)
		}
	}
}

// exitBuildErr maps a Build failure to a process exit. A child exit from
// --run propagates its code silently (the program already reported itself on
// stderr); everything else gets the usual "domain: ..." line and code 1.
func exitBuildErr(err error) {
	var xe *exitError
	if errors.As(err, &xe) {
		os.Exit(xe.code)
	}
	fmt.Fprintf(os.Stderr, "domain: %v\n", err)
	os.Exit(1)
}

// isImplicitRun reports whether a subcommand-less invocation selects the
// interpreter: exactly one argument, and it is a program path rather than a
// flag. Anything more (an output path, --emit-go, any flag) selects the
// compiler.
func isImplicitRun(args []string) bool {
	return len(args) == 1 && !strings.HasPrefix(args[0], "-")
}

func usage(w io.Writer) {
	fmt.Fprint(w, domainBanner(isColorTerminal(w)))
	fmt.Fprint(w, `Domain — describe what, let the compiler choose how.

Usage:
  domain <file.domain>                interpret the program (bare file, no other args)
  domain <file.domain> <args...>      compile it (any extra argument selects the compiler)
  domain run <file.domain> [flags]    interpret, explicitly (accepts the shared flags)
  domain build <file.domain> [flags]  compile, explicitly
  domain check <file.domain>          typecheck only: report the first error, run nothing
  domain fmt <file.domain>... [-w]    canonical whitespace (-w in place, --check for CI)
  domain repl                         interactive pipeline builder (replay-on-each-line)
  domain lsp                          language server over stdio (diagnostics, hover, defs, fixes)
  domain help | -h | --help           show this help

Expansion commands (the diagnostics engine):
  domain expansion: diagnosis <file>        full error list with fix suggestions (read-only)
  domain expansion: lint <file>             errors + style warnings + performance hints (read-only)
  domain expansion: fix <file>              apply unambiguous fixes in place (original kept as .bak)
  domain expansion: optimize <file>         optimization report; rewrites the source where possible (.bak)
  domain expansion: maximum compile <file>  fix, lint, optimize, then compile and run with stdin
  domain expansion: visualize <file>        step through a run and watch the data change shape
  domain expansion: development [file]      write a program in a terminal editor that knows the language
  domain expansion: documentation [-p PORT] serve the browsable docs website locally (default port 4444)
  domain expansion: vscode [--dir PATH]     install the VS Code extension carried inside this binary

Shared flags (run and build):
  --explain      print the algorithm substitutions the optimizer made
  --no-optimize  use the naive pipeline (skip the optimizer)
  --release      shed Binding Vows: run skips them, build compiles them out

Run flags:
  --stats        per-stage counts and timings on stderr (interpreter only)
  --verbose      with --stats, also list nested loop/channel/part steps

Build flags:
  -o <binary>    where to write the compiled binary (default: source name without .domain)
  --emit-go <f>  also write the generated Go source ("-" for stdout)
  --run          run the binary immediately with the current stdin; without -o
                 it is built to a temp path and cleaned up afterwards

Examples:
  domain day1.domain < input.txt      interpret day1.domain
  domain day1.domain -o day1          compile to ./day1
  domain run day1.domain --explain    interpret, showing optimizer rewrites
`)
}

func parseRunArgs(args []string) (string, Options, error) {
	opts := Options{Optimize: true}
	var path string
	for _, a := range args {
		switch a {
		case "--explain":
			opts.Explain = true
		case "--no-optimize":
			opts.Optimize = false
		case "--release":
			opts.Release = true
		case "--stats":
			opts.Stats = true
		case "--verbose":
			opts.Verbose = true
		default:
			if strings.HasPrefix(a, "-") {
				return "", opts, fmt.Errorf("unknown flag %q", a)
			}
			if path != "" {
				return "", opts, fmt.Errorf("unexpected extra argument %q", a)
			}
			path = a
		}
	}
	if path == "" {
		return "", opts, fmt.Errorf("missing <file.domain>")
	}
	return path, opts, nil
}

// parseCheckArgs accepts exactly one program path; check has no flags — it
// always runs the full static front end and nothing else.
func parseCheckArgs(args []string) (string, error) {
	var path string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return "", fmt.Errorf("check takes no flags (got %q)", a)
		}
		if path != "" {
			return "", fmt.Errorf("unexpected extra argument %q", a)
		}
		path = a
	}
	if path == "" {
		return "", fmt.Errorf("missing <file.domain>")
	}
	return path, nil
}

// Check runs the static front end only — read, lex, parse, resolve (which is
// where Domain typechecks) — and reports the first positioned error without
// executing anything. Success prints "<path>: ok".
func Check(path string, stdout, stderr io.Writer) error {
	if _, err := loadPipeline(path, false, false, stderr); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: ok\n", path)
	return nil
}

func parseBuildArgs(args []string) (string, BuildOptions, error) {
	opts := BuildOptions{Optimize: true}
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--explain":
			opts.Explain = true
		case "--no-optimize":
			opts.Optimize = false
		case "--release":
			opts.Release = true
		case "--run":
			opts.Run = true
		case "-o", "--output":
			i++
			if i >= len(args) {
				return "", opts, fmt.Errorf("%s requires a path", a)
			}
			opts.Out = args[i]
		case "--emit-go":
			i++
			if i >= len(args) {
				return "", opts, fmt.Errorf("--emit-go requires a path")
			}
			opts.EmitGo = args[i]
		default:
			if strings.HasPrefix(a, "-") {
				return "", opts, fmt.Errorf("unknown flag %q", a)
			}
			if path != "" {
				return "", opts, fmt.Errorf("unexpected extra argument %q", a)
			}
			path = a
		}
	}
	if path == "" {
		return "", opts, fmt.Errorf("missing <file.domain>")
	}
	return path, opts, nil
}

// loadPipeline is the shared front end: read, lex, parse, resolve, optimize.
// Rewrite explanations go to stderr when explain is set.
func loadPipeline(path string, optimize, explain bool, stderr io.Writer) (*ir.Pipeline, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	toks, err := lexer.Lex(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}

	prog, err := parser.Parse(string(src), toks)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}

	pipe, err := prims.ResolveWith(prog, prims.FileOptions(path))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}

	rewrites := optimizer.Optimize(pipe, optimize)
	if explain {
		if len(rewrites) == 0 {
			fmt.Fprintln(stderr, "[explain] no optimizations applied.")
		}
		for _, r := range rewrites {
			fmt.Fprintf(stderr, "[explain] %s\n", r.Message)
		}
	}
	return pipe, nil
}

// Execute runs a Domain program end to end. It is the single code path shared
// by the CLI and the end-to-end tests.
func Execute(path string, opts Options, stdin io.Reader, stdout, stderr io.Writer) error {
	pipe, err := loadPipeline(path, opts.Optimize, opts.Explain, stderr)
	if err != nil {
		return err
	}

	ctx := &ir.Context{
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  os.Stderr,
		BaseDir: filepath.Dir(path),
		Release: opts.Release,
	}
	// --stats installs the aggregating tracer. Without it ctx.Trace stays nil
	// and every evaluation site is one nil check away from what it always was.
	var stats *interp.Stats
	if opts.Stats {
		stats = interp.NewStats()
		ctx.Trace = stats
	}
	_, runErr := interp.Run(pipe, ctx)
	// Report even on failure: the stage that failed is usually the interesting
	// one, and the table shows how far the program got.
	if stats != nil {
		stats.Report(stderr, opts.Verbose)
	}
	return runErr
}

// Build compiles a Domain program to a standalone optimized Go binary. The
// generated source can additionally be written out with --emit-go (to stdout
// when the path is "-"). With Run set the binary is executed immediately with
// the given stdin/stdout/stderr — and when no -o was given it is built to a
// temp path and cleaned up afterwards, so `domain build --run prog.domain <
// input` is a one-shot compile-and-run. A nonzero child exit comes back as an
// *exitError carrying the child's code.
func Build(path string, opts BuildOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	pipe, err := loadPipeline(path, opts.Optimize, opts.Explain, stderr)
	if err != nil {
		return err
	}

	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{Release: opts.Release})
	if err != nil {
		return err
	}

	if opts.EmitGo == "-" {
		fmt.Fprint(stdout, goSrc)
	} else if opts.EmitGo != "" {
		if err := os.WriteFile(opts.EmitGo, []byte(goSrc), 0o644); err != nil {
			return err
		}
	}

	out := opts.Out
	if out == "" {
		if opts.Run {
			dir, err := os.MkdirTemp("", "domain-run-")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(dir) }()
			out = filepath.Join(dir, defaultBinaryName(path))
		} else {
			out = defaultBinaryName(path)
		}
	}
	if err := codegen.BuildBinary(goSrc, out); err != nil {
		return err
	}
	if !opts.Run {
		return nil
	}

	cmd := exec.Command(out)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &exitError{code: ee.ExitCode()}
		}
		return err
	}
	return nil
}

// defaultBinaryName derives the output binary path from the program path:
// testdata/day1.domain -> day1 (in the current directory). A source without
// the .domain extension gets a .bin suffix so the build can never overwrite
// the program itself.
func defaultBinaryName(path string) string {
	base := filepath.Base(path)
	if name := strings.TrimSuffix(base, ".domain"); name != base && name != "" {
		return name
	}
	return base + ".bin"
}
