package prims

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Domain Expansion: <language> — a stage written in another language.
//
//	Domain Expansion: Python
//	    import sys
//	    print(sum(int(x) for x in sys.stdin))
//
// The current pipeline value is encoded onto the child process's stdin, the
// program runs, and its stdout is decoded as the next stage's value. That is
// the whole semantic: it is a shell pipe with a Domain value on each end.
//
// The keyword is `Domain Expansion` because the statement names an
// implementation rather than describing a result — which is also why it is the
// one Domain Expansion the optimizer will never touch. Every other one is a
// request the compiler may honor a faster way; this one is a literal
// instruction, opaque by construction. Nothing can be fused through it,
// reordered around it, or substituted for it, and the node is not Swappable.

// ---------------------------------------------------------------------------
// The wire format
// ---------------------------------------------------------------------------
//
// Both backends have to agree on this byte for byte — the interpreter encodes
// with the Go below, the compiler emits Go that does the same thing — so it is
// written down here once, as rules rather than as code:
//
//	Text          the text itself
//	Int/Float/Bool  its ordinary rendering (the same one Reveal prints)
//	List<scalar>  one element per line
//	Grid<scalar>  one row per line, cells rendered as Reveal renders a grid
//
// with a single closing newline on a non-empty value. Decoding reverses it,
// stripping one trailing newline first. The empty output decodes to the empty
// list, and to an error for anything else — a program that printed nothing
// where a number was expected made a mistake worth naming.
//
// The set is deliberately narrow. Everything in it has one obvious spelling as
// lines of text that every language in the list can read with its own standard
// library and write with a print statement; Maps, Sets, Records, Tuples and
// Sparse planes do not, and inventing an encoding for them here would be
// inventing a serialization format for four languages at once. They are
// refused at resolve time, by name, so the refusal arrives while the program is
// being written rather than as a decode failure at run time.

// foreignScalar reports whether t is a scalar the wire format can carry.
func foreignScalar(t *ir.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case ir.KInt, ir.KFloat, ir.KText, ir.KBool:
		return true
	}
	return false
}

// foreignEncodable reports whether a value of type t can be written to a
// foreign program's stdin.
func foreignEncodable(t *ir.Type) bool {
	if t == nil {
		return false
	}
	switch {
	case foreignScalar(t):
		return true
	case t.Kind == ir.KList && foreignScalar(t.Elem):
		return true
	case t.Kind == ir.KGrid && foreignScalar(t.Elem):
		return true
	}
	return false
}

// foreignDecodable reports whether a foreign program's stdout can be read back
// as a value of type t. It is narrower than foreignEncodable: a grid's rows
// have no separator to split cells on, so a grid can be shown to a foreign
// program but not received from one — `Convert To Grid` is one stage away.
func foreignDecodable(t *ir.Type) bool {
	if t == nil {
		return false
	}
	return foreignScalar(t) || (t.Kind == ir.KList && foreignScalar(t.Elem))
}

// foreignEncode renders a value for a foreign program's stdin.
func foreignEncode(v ir.Value, t *ir.Type) (string, error) {
	switch {
	case t.Kind == ir.KList:
		l, err := ir.AsList(v)
		if err != nil {
			return "", err
		}
		if len(l) == 0 {
			return "", nil
		}
		parts := make([]string, len(l))
		for i, e := range l {
			parts[i] = ir.FormatValue(e)
		}
		return strings.Join(parts, "\n") + "\n", nil
	default:
		// Scalars and grids both render as themselves; a grid's rendering is
		// already one row per line.
		s := ir.FormatValue(v)
		if s == "" {
			return "", nil
		}
		return s + "\n", nil
	}
}

// foreignDecode reads a foreign program's stdout back as a value of type t.
func foreignDecode(out string, t *ir.Type) (ir.Value, error) {
	body := strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r")
	if t.Kind == ir.KList {
		if body == "" {
			return []ir.Value{}, nil
		}
		lines := strings.Split(body, "\n")
		vals := make([]ir.Value, len(lines))
		for i, line := range lines {
			v, err := foreignScalarOf(strings.TrimSuffix(line, "\r"), t.Elem)
			if err != nil {
				return nil, fmt.Errorf("line %d of the output: %w", i+1, err)
			}
			vals[i] = v
		}
		return vals, nil
	}
	return foreignScalarOf(strings.TrimSpace(body), t)
}

// foreignScalarOf parses one field of foreign output as a scalar.
func foreignScalarOf(s string, t *ir.Type) (ir.Value, error) {
	switch t.Kind {
	case ir.KText:
		return s, nil
	case ir.KInt:
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected an Int, got %q", s)
		}
		return n, nil
	case ir.KFloat:
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("expected a Float, got %q", s)
		}
		return f, nil
	case ir.KBool:
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "1":
			return true, nil
		case "false", "0":
			return false, nil
		}
		return nil, fmt.Errorf("expected a Bool (true/false), got %q", s)
	}
	return nil, fmt.Errorf("cannot read %s from foreign output", t)
}

// ---------------------------------------------------------------------------
// The primitive
// ---------------------------------------------------------------------------

// foreignPrim is the one entry in the registry that matches a language name.
// Its ID is a single primitive rather than one per language because the four
// differ only in how their runtime is started: the type rules, the wire format
// and the failure modes are identical, and four registry entries would be four
// copies of the same documentation.
var foreignPrim = &Primitive{
	ID:      "Foreign Block",
	Keyword: "Domain Expansion",
	Phrases: ast.ForeignLanguages,
	Match: func(op *ast.Operation) bool {
		if op == nil || len(op.Words) != 1 || len(op.Strings) > 0 ||
			len(op.Ints) > 0 || len(op.OpSyms) > 0 || len(op.Modifiers) > 0 {
			return false
		}
		_, ok := ast.ForeignLanguage(op.Words[0])
		return ok
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		fb, ok := args.ForeignBlock()
		if !ok {
			// The lexer only captures a block for a line of exactly this shape,
			// so reaching here means the block was never written.
			lang, _ := ast.ForeignLanguage(op.Words[0])
			return nil, &ResolveError{Pos: pos, NeedsBlock: true, Msg: fmt.Sprintf(
				"Domain Expansion: %s needs an indented block of %s code beneath it", lang, lang)}
		}
		inType, outType, err := foreignTypes(fb, in, pos)
		if err != nil {
			return nil, err
		}
		lang, source := fb.Language, fb.Source
		display := fmt.Sprintf("%s block (%s -> %s)", lang, inType, outType)
		return &ir.Node{
			Prim:    "Foreign Block",
			In:      inType,
			Out:     outType,
			Display: display,
			Meta: map[string]any{
				"lang":   lang,
				"source": source,
			},
			Pos: pos,
			Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
				stdin, err := foreignEncode(v, inType)
				if err != nil {
					return nil, runtimeErr("Foreign Block", pos, "encoding the input for the %s block: %v", lang, err)
				}
				stdout, err := runForeign(ctx, lang, source, stdin)
				if err != nil {
					return nil, runtimeErr("Foreign Block", pos, "%v", err)
				}
				out, err := foreignDecode(stdout, outType)
				if err != nil {
					return nil, runtimeErr("Foreign Block", pos,
						"the %s block was declared to produce %s, but %v", lang, outType, err)
				}
				return out, nil
			},
		}, nil
	},
}

// foreignTypes settles a foreign block's input and output types.
//
// Without a declared signature a block is "whatever is flowing, out as Text":
// the value on its stdin is encoded from the type the pipeline already has, and
// what it prints is text until something downstream says otherwise. That is the
// shell-pipe reading of the statement, and it keeps the common case — a block
// that prints an answer, or one whose output the existing vocabulary reshapes —
// free of type annotations.
//
// A declared `: In -> Out` replaces both halves and is checked against the
// pipeline, exactly as a Shikigami's declared signature is. It is the only way
// to get a non-Text value back out, because nothing else can know what the
// foreign program meant by its output.
func foreignTypes(fb *ast.ForeignBlock, in *ir.Type, pos token.Position) (*ir.Type, *ir.Type, error) {
	if in == nil {
		return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"a %s block transforms the current value, so it cannot be the first stage; read the input with Cursed Energy first",
			fb.Language)}
	}
	inType, outType := in, ir.Text()
	if fb.Sig != nil {
		declared, err := lowerTypeExpr(fb.Sig.In, fb.Sig.Pos)
		if err != nil {
			return nil, nil, err
		}
		if !declared.Equal(in) {
			return nil, nil, &ResolveError{Pos: fb.Sig.Pos, Msg: fmt.Sprintf(
				"the %s block declares it takes %s, but the pipeline produced %s",
				fb.Language, declared, in)}
		}
		inType = declared
		if outType, err = lowerTypeExpr(fb.Sig.Out, fb.Sig.Pos); err != nil {
			return nil, nil, err
		}
	}
	if !foreignEncodable(inType) {
		return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"a %s block cannot be given %s: a foreign stage exchanges lines of text, so its input must be Int, Float, Text, Bool, a List of those, or a Grid of those",
			fb.Language, inType)}
	}
	if !foreignDecodable(outType) {
		return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"a %s block cannot produce %s: a foreign stage exchanges lines of text, so its output must be Int, Float, Text, Bool, or a List of those",
			fb.Language, outType)}
	}
	return inType, outType, nil
}

// ---------------------------------------------------------------------------
// Watching a run
// ---------------------------------------------------------------------------

// A foreign block is the one stage whose behaviour a reader cannot see from the
// pipeline trace. Every other primitive is Domain the whole way down: the value
// that went in, the value that came out, and a `Using:` expression in between
// that eval can replay on demand. Here the middle is another language's program
// and the bytes it exchanged, and neither is visible from either side.
//
// So it is watched, the way lambda applications are (eval.WatchApplications):
// one package-level hook, installed by whoever wants to record, seen by every
// run because runForeign is the single door. An ordinary run pays one nil
// check per subprocess — against the cost of starting a process, nothing.
//
// Unlike an application, a run cannot be replayed to recover its detail later:
// it is a subprocess with whatever consequences it had, and re-running one
// behind the reader's back is exactly what eval.TraceLambda refuses to do for
// a pipeline body. What is wanted has to be captured while it happens.

// ForeignWatcher is called with each foreign block execution while one is
// installed.
type ForeignWatcher func(ir.ForeignRun)

// watchingForeign is the installed watcher, or nil in an ordinary run. Package
// level under the same standing assumption as eval's and ir's: interp.Run is
// not called concurrently within one process.
var watchingForeign ForeignWatcher

// WatchForeignRuns installs f as the watcher for foreign block executions and
// returns a function restoring whatever was installed before. A nil f turns
// watching off.
func WatchForeignRuns(f ForeignWatcher) (restore func()) {
	prev := watchingForeign
	watchingForeign = f
	return func() { watchingForeign = prev }
}

// maxForeignCapture bounds each captured stream. A foreign stage is often
// handed the whole puzzle input, and a recording that kept every byte of it
// would be larger than the program it describes; a reader only ever looks at
// the head anyway, which is where a wire-format mistake shows.
const maxForeignCapture = 4096

// ---------------------------------------------------------------------------
// Running the thing
// ---------------------------------------------------------------------------

// runForeign writes source into a throwaway directory, runs it with stdin on
// its standard input, and returns its standard output.
//
// The child's stderr goes to the program's own stderr, so a print-debugging
// line in a foreign block reaches the terminal without disturbing the value
// flowing through the pipeline — the same bargain `Reveal: stderr` makes. A nil
// sink discards it, as it does there.
func runForeign(ctx *ir.Context, lang, source, stdin string) (string, error) {
	// A platform with no processes to start says so plainly, rather than
	// reporting the runtime as missing from a PATH that does not exist. This is
	// the documentation site's playground, where the whole front end is
	// compiled to WebAssembly — the same platform `Cursed Energy` special-cases
	// for its absent filesystem.
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		return "", fmt.Errorf(
			"a %s block cannot run here: this build has no subprocesses (the playground is compiled to WebAssembly)",
			lang)
	}
	dir, err := os.MkdirTemp("", "domain-foreign-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	argv, files, err := foreignCommand(lang, dir)
	if err != nil {
		return "", err
	}
	files[foreignFile(lang)] = source
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return "", err
		}
	}

	var out, errBuf bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	started := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(started)

	if runErr != nil {
		runErr = foreignFailure(lang, runErr, errBuf.String())
	}
	if watchingForeign != nil {
		// Reported whether it succeeded or failed: a failing block is the one a
		// reader most wants to see the input to.
		watchingForeign(ir.ForeignRun{
			Lang:    lang,
			Command: strings.Join(argv, " "),
			Stdin:   ir.CaptureText(stdin, maxForeignCapture),
			Stdout:  ir.CaptureText(out.String(), maxForeignCapture),
			Stderr:  ir.CaptureText(errBuf.String(), maxForeignCapture),
			Err:     runErr,
			Dur:     elapsed,
		})
	}
	if runErr != nil {
		// The runtime's own report is the useful part of a failure and goes
		// into the error, which is the one place it is read from. Forwarding it
		// here as well would print a Python traceback twice.
		return "", runErr
	}
	if ctx != nil && ctx.Stderr != nil && errBuf.Len() > 0 {
		_, _ = ctx.Stderr.Write(errBuf.Bytes())
	}
	return out.String(), nil
}

// foreignFailure turns a child's failure into an error that says what the
// foreign runtime said. Its own message is the useful part — a Python traceback
// or a Go compile error names the line in the block — so it is reproduced
// rather than summarized.
func foreignFailure(lang string, runErr error, stderr string) error {
	stderr = strings.TrimRight(stderr, "\n")
	var exit *exec.ExitError
	if !errors.As(runErr, &exit) {
		return fmt.Errorf("could not run the %s block: %w", lang, runErr)
	}
	if stderr == "" {
		return fmt.Errorf("the %s block exited with status %d and said nothing",
			lang, exit.ExitCode())
	}
	// The first line stands alone, because that is where every renderer puts
	// the stage tag; the runtime's own report follows it.
	return fmt.Errorf("the %s block failed with status %d\n%s", lang, exit.ExitCode(), stderr)
}

// foreignFile is the name the block is written under. Extensions matter: a Go
// file must end in .go to compile, and the others are named for the benefit of
// whoever is reading a stack trace.
func foreignFile(lang string) string {
	switch lang {
	case "Python":
		return "program.py"
	case "Go":
		return "main.go"
	case "rask":
		return "program.rask"
	case "cRust":
		return "program.crust"
	}
	return "program.txt"
}

// foreignCommand resolves the runtime for a language and returns the command to
// run, plus any extra files the runtime needs beside the program.
//
// Every language's runtime can be overridden with an environment variable —
// DOMAIN_PYTHON, DOMAIN_GO, DOMAIN_RASK, DOMAIN_CRUST — which may name a
// command with arguments ("uv run python"). That is what makes the feature
// usable on a machine where the binary is not on PATH under its usual name, and
// what lets the tests run against whatever is actually installed.
func foreignCommand(lang, dir string) ([]string, map[string]string, error) {
	prog := filepath.Join(dir, foreignFile(lang))
	switch lang {
	case "Python":
		bin, err := foreignBinary(lang, "DOMAIN_PYTHON", "python3", "python")
		if err != nil {
			return nil, nil, err
		}
		return append(bin, prog), map[string]string{}, nil
	case "Go":
		bin, err := foreignBinary(lang, "DOMAIN_GO", "go")
		if err != nil {
			return nil, nil, err
		}
		// `go run .` inside the throwaway module: the block is a whole
		// `package main`, exactly as a hand-written Go program would be, and
		// the build cache keeps the repeat cost down.
		return append(bin, "run", "."),
			map[string]string{"go.mod": "module domainforeign\n\ngo 1.22\n"}, nil
	case "rask":
		bin, err := foreignBinary(lang, "DOMAIN_RASK", "rask")
		if err != nil {
			return nil, nil, err
		}
		return append(bin, prog), map[string]string{}, nil
	case "cRust":
		bin, err := foreignBinary(lang, "DOMAIN_CRUST", "crust")
		if err != nil {
			return nil, nil, err
		}
		return append(bin, prog), map[string]string{}, nil
	}
	return nil, nil, fmt.Errorf("no runner for %q", lang)
}

// foreignBinary finds a language's runtime: the environment override if it is
// set, else the first candidate on PATH.
func foreignBinary(lang, env string, candidates ...string) ([]string, error) {
	if override := strings.TrimSpace(os.Getenv(env)); override != "" {
		return strings.Fields(override), nil
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return []string{path}, nil
		}
	}
	return nil, fmt.Errorf(
		"a %s block needs %s on PATH to run (set %s to name it differently)",
		lang, strings.Join(candidates, " or "), env)
}
