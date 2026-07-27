// The Domain REPL (`domain repl`): an interactive pipeline builder. Each
// accepted statement is appended to the program and the WHOLE pipeline is
// replayed from the top (the replay model — always correct with Channels,
// Shikigami, loops, and vows; AoC-scale programs replay in microseconds), then
// the new current value and its type are printed:
//
//	domain> Cursed Energy: input.txt
//	=> "1000\n2000\n\n3000" : Text
//	domain> Cursed Technique: Split Text by "\n\n"
//	=> ["1000\n2000", "3000"] : List<Text>
//
// Statements with indented blocks (Using:, Channel bodies, Shikigami
// definitions) enter continuation mode: keep typing indented lines, and
// finish with a blank line or the next top-level statement.
//
// A failing statement is reported and dropped — the session survives. Pipeline
// Reveal output is suppressed during replays (the REPL prints the current
// value after every statement anyway).
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

const replHelp = `Every line is a Domain pipeline statement; the value threads top to bottom.
Indented argument/body lines continue the statement above; finish a block
with a blank line. Commands:
  :help           this text
  :list           show the program built so far
  :type           show the current value's type
  :undo           drop the last statement
  :reset          drop everything
  :load <file>    replace the program with a file's statements
  :save <file>    write the program to a file
  :quit           leave the domain
`

// repl holds one interactive session.
type repl struct {
	in      *bufio.Scanner
	out     io.Writer
	stmts   []string // accepted statements, each possibly multi-line
	pending []string // statement under construction (awaiting its block)
	baseDir string   // resolution root for Cursed Energy file targets
}

// Repl runs the interactive loop until :quit or EOF. It returns the process
// exit code. A real terminal gets arrow-key editing, session history, and
// auto-indented continuation lines (repl_tty.go); anything else (piped
// input, and every test below, which feeds a strings.Reader) gets the plain
// line-at-a-time reader.
func Repl(stdin io.Reader, stdout io.Writer) int {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(f.Fd()) {
		return replTTY(f, stdout)
	}
	return replPlain(stdin, stdout)
}

// replPlain is today's REPL loop, unchanged in behavior.
func replPlain(stdin io.Reader, stdout io.Writer) int {
	r := &repl{in: bufio.NewScanner(stdin), out: stdout, baseDir: "."}
	fmt.Fprintln(r.out, "Domain REPL — an interactive domain expansion. :help lists commands, :quit leaves.")
	for {
		if len(r.pending) > 0 {
			fmt.Fprint(r.out, "   ...> ")
		} else {
			fmt.Fprint(r.out, "domain> ")
		}
		if !r.in.Scan() {
			fmt.Fprintln(r.out)
			if err := r.in.Err(); err != nil {
				fmt.Fprintf(r.out, "error: %v\n", err)
				return 1
			}
			return 0
		}
		line := strings.TrimRight(r.in.Text(), " \t\r")
		if quit := r.handleLine(line); quit {
			return 0
		}
	}
}

// handleLine routes one already-read, right-trimmed input line: a :command,
// a blank line (completes a pending block), an indented continuation line,
// or a fresh top-level statement. It reports whether the session should end.
func (r *repl) handleLine(line string) (quit bool) {
	switch {
	case strings.HasPrefix(strings.TrimSpace(line), ":"):
		r.flushPending()
		return r.command(strings.TrimSpace(line))
	case line == "":
		r.flushPending()
	case line[0] == ' ' || line[0] == '\t':
		if len(r.pending) == 0 {
			fmt.Fprintln(r.out, "error: this indented line continues nothing — start with a top-level `Keyword: operation`")
			return false
		}
		r.pending = append(r.pending, strings.ReplaceAll(line, "\t", "    "))
	default:
		r.flushPending()
		r.acceptTopLevel(line)
	}
	return false
}

// acceptTopLevel starts a new statement. If it already forms a complete,
// resolvable program it is evaluated immediately; if the front end says an
// indented block is still missing, the statement waits in continuation mode;
// any other error is reported and the line dropped.
func (r *repl) acceptTopLevel(line string) {
	trial := append(append([]string{}, r.stmts...), line)
	pipe, _, err := r.frontEnd(trial)
	if err != nil && needsBlock(err) {
		r.pending = []string{line}
		return
	}
	if err != nil {
		fmt.Fprintf(r.out, "error: %v\n", err)
		return
	}
	r.stmts = trial
	r.evalAndShow(pipe)
}

// flushPending completes the statement under construction: evaluate it with
// its block, reporting (and dropping) a statement that still fails.
func (r *repl) flushPending() {
	if len(r.pending) == 0 {
		return
	}
	stmt := strings.Join(r.pending, "\n")
	r.pending = nil
	trial := append(append([]string{}, r.stmts...), stmt)
	pipe, _, err := r.frontEnd(trial)
	if err != nil {
		fmt.Fprintf(r.out, "error: %v\n", err)
		return
	}
	r.stmts = trial
	r.evalAndShow(pipe)
}

// needsBlock recognizes front-end errors that mean "the statement is fine so
// far, it just wants indented lines" — the continuation-mode triggers.
func needsBlock(err error) bool {
	msg := err.Error()
	for _, frag := range []string{
		"must be followed by an indented body",
		"must be followed by an indented sub-pipeline",
		"requires a Using: lambda",
		"has an empty body",
		"requires a seed",
		"requires a Seed",
		"From: consumers",
	} {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	return false
}

// frontEnd resolves the joined statements into a pipeline.
func (r *repl) frontEnd(stmts []string) (*ir.Pipeline, string, error) {
	src := strings.Join(stmts, "\n") + "\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, src, err
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return nil, src, err
	}
	// Imports resolve against the REPL's base directory, the same root
	// Cursed Energy file targets use.
	opts := prims.ResolveOptions{BaseDir: r.baseDir, Search: prims.SearchPath()}
	pipe, err := prims.ResolveWith(prog, opts)
	if err != nil {
		return nil, src, err
	}
	return pipe, src, nil
}

// evalAndShow replays the whole pipeline and prints the current value and its
// type. The caller has already resolved pipe via frontEnd (no need to parse
// again here). A runtime failure rolls the last statement back so the
// session keeps the last-good program.
func (r *repl) evalAndShow(pipe *ir.Pipeline) {
	if len(pipe.Nodes) == 0 {
		return // a lone Shikigami definition produces no value
	}
	ctx := &ir.Context{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard, // the REPL prints the value itself; Reveal replays stay quiet
		BaseDir: r.baseDir,
	}
	v, err := interp.Run(pipe, ctx)
	if err != nil {
		fmt.Fprintf(r.out, "runtime error: %v (statement dropped)\n", err)
		r.stmts = r.stmts[:len(r.stmts)-1]
		return
	}
	fmt.Fprintf(r.out, "=> %s : %s\n", ir.FormatShort(v), pipe.Nodes[len(pipe.Nodes)-1].Out)
}

// command handles one :directive; it reports whether the session should end.
func (r *repl) command(line string) bool {
	fields := strings.Fields(line)
	switch fields[0] {
	case ":quit", ":q", ":exit":
		return true
	case ":help", ":h":
		fmt.Fprint(r.out, replHelp)
	case ":list", ":l":
		if len(r.stmts) == 0 {
			fmt.Fprintln(r.out, "(empty domain)")
			break
		}
		fmt.Fprintln(r.out, strings.Join(r.stmts, "\n"))
	case ":type", ":t":
		pipe, _, err := r.frontEnd(r.stmts)
		if err != nil || len(pipe.Nodes) == 0 {
			fmt.Fprintln(r.out, "(no value yet)")
			break
		}
		fmt.Fprintln(r.out, pipe.Nodes[len(pipe.Nodes)-1].Out)
	case ":undo", ":u":
		if len(r.stmts) == 0 {
			fmt.Fprintln(r.out, "(nothing to undo)")
			break
		}
		r.stmts = r.stmts[:len(r.stmts)-1]
		if len(r.stmts) > 0 {
			r.evalAndShowKeep()
		} else {
			fmt.Fprintln(r.out, "(empty domain)")
		}
	case ":reset":
		r.stmts = nil
		fmt.Fprintln(r.out, "(empty domain)")
	case ":load":
		if len(fields) < 2 {
			fmt.Fprintln(r.out, "usage: :load <file.domain>")
			break
		}
		src, err := os.ReadFile(fields[1])
		if err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			break
		}
		old := r.stmts
		oldBaseDir := r.baseDir
		r.stmts = splitStatements(string(src))
		r.baseDir = filepath.Dir(fields[1])
		if _, _, err := r.frontEnd(r.stmts); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			r.stmts = old
			r.baseDir = oldBaseDir
			break
		}
		r.evalAndShowKeep()
	case ":save":
		if len(fields) < 2 {
			fmt.Fprintln(r.out, "usage: :save <file.domain>")
			break
		}
		src := strings.Join(r.stmts, "\n") + "\n"
		if err := os.WriteFile(fields[1], []byte(src), 0o644); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			break
		}
		fmt.Fprintf(r.out, "saved %d statement(s) to %s\n", len(r.stmts), fields[1])
	default:
		fmt.Fprintf(r.out, "unknown command %s (:help lists commands)\n", fields[0])
	}
	return false
}

// evalAndShowKeep replays and reports like evalAndShow but never rolls back —
// used after :undo/:load where the program is not a fresh trial.
func (r *repl) evalAndShowKeep() {
	pipe, _, err := r.frontEnd(r.stmts)
	if err != nil {
		fmt.Fprintf(r.out, "error: %v\n", err)
		return
	}
	if len(pipe.Nodes) == 0 {
		return
	}
	ctx := &ir.Context{Stdin: strings.NewReader(""), Stdout: io.Discard, BaseDir: r.baseDir}
	v, err := interp.Run(pipe, ctx)
	if err != nil {
		fmt.Fprintf(r.out, "runtime error: %v\n", err)
		return
	}
	fmt.Fprintf(r.out, "=> %s : %s\n", ir.FormatShort(v), pipe.Nodes[len(pipe.Nodes)-1].Out)
}

// splitStatements cuts a program source into top-level statements: a new
// statement starts at every non-blank column-0 line; indented lines belong to
// the statement above.
func splitStatements(src string) []string {
	var stmts []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			stmts = append(stmts, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		switch {
		case strings.TrimSpace(trimmed) == "":
			continue
		case trimmed[0] == ' ' || trimmed[0] == '\t':
			cur = append(cur, trimmed)
		default:
			flush()
			cur = []string{trimmed}
		}
	}
	flush()
	return stmts
}
