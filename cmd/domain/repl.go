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
// A failing statement is reported — through the same diagnostics engine
// `domain check` uses — and dropped; the session survives. Pipeline Reveal
// output is suppressed during replays (the REPL prints the current value after
// every statement anyway).
//
// This file is the session core: it is what a piped script drives, and what
// the interactive editor in repl_tty.go drives on a real terminal.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/term"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// The session's fixed strings. The interactive editor prints its own prompts,
// so they live here once rather than once per reader.
const (
	replBanner     = "Domain REPL — an interactive domain expansion. :help lists commands, :quit leaves."
	promptTop      = "domain> "
	promptContinue = "   ...> "
)

const replHelp = `Every line is a Domain pipeline statement; the value threads top to bottom.
Indented argument/body lines continue the statement above; finish a block
with a blank line. Commands:
  :help           this text
  :list           show the program built so far
  :type           show the current value's type
  :stats          replay under the profiler and chart where the time goes
  :visualize      step through a recorded run (a text trace when piped)
  :replay         run the program again as it stands
  :watch <file>   replay whenever that file changes (terminal only)
  :doc [name]     a primitive's signature and summary (bare :doc browses)
  :docs [port]    serve the documentation and link error codes into it
  :undo           drop the last statement
  :reset          drop everything
  :load <file>    replace the program with a file's statements
  :save <file>    write the program to a file (:save! overwrites)
  :edit           open the program in $EDITOR and reload it (terminal only)
  :copy           copy the program to the system clipboard (terminal only)
  :paste          load a program from the system clipboard (terminal only)
  :keys           show the editor's key bindings (terminal only)
  :quit           leave the domain (:quit! discards unsaved statements)
`

// ttyOnlyCommands are handled by the interactive editor (repl_tty.go); the
// core knows their names so a piped session explains itself instead of
// reporting an unknown command.
var ttyOnlyCommands = map[string]string{
	":edit": "opens $EDITOR", ":copy": "uses the system clipboard",
	":paste": "reads the system clipboard", ":keys": "lists key bindings",
	":watch": "polls a file in the background",
}

// repl holds one interactive session.
type repl struct {
	in       *bufio.Scanner
	out      io.Writer
	stmts    []string // accepted statements, each possibly multi-line
	pending  []string // statement under construction (awaiting its block)
	lead     []string // comment/blank lines waiting to attach to the next statement
	baseDir  string   // resolution root for Cursed Energy file targets
	color    bool     // colorize values, diagnostics and charts
	width    int      // terminal width, for charts (0 = a sensible default)
	dirty    bool     // statements accepted since the last :save
	lastType string   // the current value's type, for the terminal's title bar
	// trace is installed on every run. The interactive editor sets an
	// interrupter here so a runaway loop can be stopped (ir/interrupt.go);
	// a piped session leaves it nil, since Ctrl+C still reaches the process.
	trace ir.Tracer
	// progress, when set, is told how many stages a resolved run has, so the
	// editor can show how far along it is (repl_progress.go).
	progress *progressCounter
	// lastProfile is the profile `:stats` last took, kept so a reader over it
	// can re-order it without running the program again.
	lastProfile *interp.Stats
	// lastTrace is the recording `:visualize` last made, waiting for the
	// editor to open a stepper over it (repl_visualize.go).
	lastTrace *traceView
	// interactive marks the session driven by the editor rather than by a
	// pipe: what it can hand to an overlay, a pipe has to be told in text.
	interactive bool
}

// Repl runs the interactive loop until :quit or EOF. It returns the process
// exit code. A real terminal gets the full editor — history, completion, live
// types, interruptible evaluation (repl_tty.go); anything else (piped input,
// and every test that feeds a strings.Reader) gets the plain line-at-a-time
// reader.
func Repl(stdin io.Reader, stdout io.Writer) int {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(f.Fd()) {
		return replTTY(f, stdout)
	}
	return replPlain(stdin, stdout)
}

// replPlain is the line-at-a-time reader: no editing, no color, no escapes.
func replPlain(stdin io.Reader, stdout io.Writer) int {
	r := &repl{in: bufio.NewScanner(stdin), out: stdout, baseDir: "."}
	fmt.Fprintln(r.out, replBanner)
	for {
		fmt.Fprint(r.out, r.prompt())
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

// prompt is the prompt for the session's current state.
func (r *repl) prompt() string {
	if len(r.pending) > 0 {
		return promptContinue
	}
	return promptTop
}

// handleLine routes one already-read, right-trimmed input line: a :command,
// a comment, a blank line (completes a pending block), an indented
// continuation line, or a fresh top-level statement. It reports whether the
// session should end.
func (r *repl) handleLine(line string) (quit bool) {
	switch {
	case strings.HasPrefix(strings.TrimSpace(line), ":"):
		r.flushPending()
		return r.command(strings.TrimSpace(line))
	case line == "":
		if len(r.pending) > 0 {
			r.flushPending()
		}
	case line[0] == ' ' || line[0] == '\t':
		if len(r.pending) == 0 {
			fmt.Fprintln(r.out, "error: this indented line continues nothing — start with a top-level `Keyword: operation`")
			return false
		}
		r.pending = append(r.pending, strings.ReplaceAll(line, "\t", "    "))
	case strings.HasPrefix(line, "#"):
		// A comment at the top level is not a statement: hold it and let it
		// travel with the statement it introduces, the way :load keeps a
		// file's comments attached to what they describe.
		r.flushPending()
		r.lead = append(r.lead, line)
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
	stmt := r.withLead(line)
	trial := append(slices.Clone(r.stmts), stmt)
	pipe, src, err := r.frontEnd(trial)
	if err != nil && needsBlock(err) {
		r.pending = []string{stmt}
		return
	}
	if err != nil {
		r.reportError(src, err)
		return
	}
	r.commit(trial)
	r.evalAndShow(pipe, true)
}

// withLead attaches any held comment lines to the statement they introduce.
func (r *repl) withLead(line string) string {
	if len(r.lead) == 0 {
		return line
	}
	stmt := strings.Join(append(r.lead, line), "\n")
	r.lead = nil
	return stmt
}

// flushPending completes the statement under construction: evaluate it with
// its block, reporting (and dropping) a statement that still fails.
func (r *repl) flushPending() {
	if len(r.pending) == 0 {
		return
	}
	stmt := strings.Join(r.pending, "\n")
	r.pending = nil
	trial := append(slices.Clone(r.stmts), stmt)
	pipe, src, err := r.frontEnd(trial)
	if err != nil {
		r.reportError(src, err)
		return
	}
	r.commit(trial)
	r.evalAndShow(pipe, true)
}

// commit accepts a trial program as the session's new state.
func (r *repl) commit(stmts []string) {
	r.stmts = stmts
	r.dirty = true
}

// needsBlock reports whether an error means "the statement is fine so far, it
// just wants indented lines" — the continuation-mode trigger. The front end
// says so structurally (lexer.Error.Incomplete, parser.Error.NeedsBlock,
// prims.ResolveError.NeedsBlock) rather than in prose, so rewording an error
// cannot change what the REPL does.
func needsBlock(err error) bool {
	// An open parenthesis is the lexer's version of the same situation: an
	// expression broken across lines has not finished arriving.
	var le *lexer.Error
	if errors.As(err, &le) {
		return le.Incomplete
	}
	var pe *parser.Error
	if errors.As(err, &pe) {
		return pe.NeedsBlock
	}
	var re *prims.ResolveError
	if errors.As(err, &re) {
		return re.NeedsBlock
	}
	// Statement-boundary recovery reports a list. A single entry is the same
	// situation as a lone error; more than one means something else is wrong
	// too, and waiting for a block would only hide it.
	var list parser.ErrorList
	if errors.As(err, &list) && len(list) == 1 {
		return list[0].NeedsBlock
	}
	return false
}

// frontEnd resolves the joined statements into a pipeline.
func (r *repl) frontEnd(stmts []string) (*ir.Pipeline, string, error) {
	return resolveStatements(stmts, r.baseDir)
}

// resolveStatements is the front end as a free function: it takes everything
// it needs by value, so the interactive editor can resolve a copy of the
// session (for the live type preview) without reaching into a live one.
func resolveStatements(stmts []string, baseDir string) (*ir.Pipeline, string, error) {
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
	opts := prims.ResolveOptions{BaseDir: baseDir, Search: prims.SearchPath()}
	pipe, err := prims.ResolveWith(prog, opts)
	if err != nil {
		return nil, src, err
	}
	return pipe, src, nil
}

// context is the execution context every replay runs in. There is no program
// stdin — the terminal cannot double as one — so a `Cursed Energy:` target
// that does not exist is an error here rather than silently reading nothing.
func (r *repl) context() *ir.Context {
	return &ir.Context{
		Stdin:   nil,
		Stdout:  io.Discard, // the REPL prints the value itself; Reveal replays stay quiet
		BaseDir: r.baseDir,
		Trace:   r.trace,
	}
}

// evalAndShow replays the whole pipeline and prints the current value and its
// type. The caller has already resolved pipe via frontEnd (no need to parse
// again here). With rollback set, a runtime failure drops the statement that
// caused it so the session keeps its last-good program; :undo and :load pass
// false, since their program is not a fresh trial to reject.
func (r *repl) evalAndShow(pipe *ir.Pipeline, rollback bool) {
	if len(pipe.Nodes) == 0 {
		return // a lone Shikigami definition produces no value
	}
	if r.progress != nil {
		r.progress.SetTotal(len(pipe.Nodes))
	}
	v, err := interp.Run(pipe, r.context())
	if err != nil {
		if errors.Is(err, ir.ErrInterrupted) {
			fmt.Fprintln(r.out, "interrupted"+droppedSuffix(rollback))
		} else {
			// A foreign block's failure carries its runtime's whole report,
			// so the note about the dropped statement goes on the first line
			// rather than after somebody else's traceback.
			head, rest := err.Error(), ""
			var rte *ir.RuntimeError
			if errors.As(err, &rte) {
				head, rest, _ = strings.Cut(rte.Error(), "\n")
			}
			fmt.Fprintf(r.out, "runtime error: %s%s\n", head, droppedSuffix(rollback))
			if rest != "" {
				fmt.Fprintln(r.out, rest)
			}
		}
		if rollback && len(r.stmts) > 0 {
			r.stmts = r.stmts[:len(r.stmts)-1]
		}
		return
	}
	out := pipe.Nodes[len(pipe.Nodes)-1].Out
	r.lastType = fmt.Sprint(out)
	fmt.Fprintln(r.out, r.formatResult(v, out))
}

// adopt replaces the program with src — an edited copy of itself, from
// :edit — keeping the base directory the session already resolves against. A
// program that does not resolve is reported and the session left as it was.
func (r *repl) adopt(src string) {
	old := r.stmts
	r.stmts = splitStatements(src)
	if _, trial, err := r.frontEnd(r.stmts); err != nil {
		r.reportError(trial, err)
		r.stmts = old
		return
	}
	r.pending, r.lead, r.dirty = nil, nil, true
	if len(r.stmts) == 0 {
		fmt.Fprintln(r.out, "(empty domain)")
		return
	}
	r.replay()
}

func droppedSuffix(rollback bool) string {
	if rollback {
		return " (statement dropped)"
	}
	return ""
}

// formatResult renders the `=> value : Type` line.
func (r *repl) formatResult(v ir.Value, typ *ir.Type) string {
	value, typeName := ir.FormatShort(v), fmt.Sprint(typ)
	if r.color {
		return styDim.Render("=> ") + styValue.Render(value) + styDim.Render(" : ") + styType.Render(typeName)
	}
	return fmt.Sprintf("=> %s : %s", value, typeName)
}

// reportError prints a failed statement through the diagnostics engine — the
// same carets, "did you mean" suggestions and repairs `domain check` prints —
// falling back to the raw error when the analyzer has nothing to add.
func (r *repl) reportError(src string, err error) {
	if out := r.renderDiagnostics(src); out != "" {
		fmt.Fprint(r.out, out)
		return
	}
	fmt.Fprintf(r.out, "error: %v\n", err)
}

// command handles one :directive; it reports whether the session should end.
func (r *repl) command(line string) bool {
	name, arg := splitCommand(line)
	switch name {
	case ":quit", ":q", ":exit", ":quit!", ":q!":
		return true
	case ":help", ":h":
		fmt.Fprint(r.out, replHelp)
	case ":list", ":l":
		if len(r.stmts) == 0 {
			fmt.Fprintln(r.out, "(empty domain)")
			break
		}
		fmt.Fprintln(r.out, highlightSource(strings.Join(r.stmts, "\n"), r.color))
	case ":type", ":t":
		pipe, _, err := r.frontEnd(r.stmts)
		if err != nil || len(pipe.Nodes) == 0 {
			fmt.Fprintln(r.out, "(no value yet)")
			break
		}
		fmt.Fprintln(r.out, pipe.Nodes[len(pipe.Nodes)-1].Out)
	case ":replay":
		if len(r.stmts) == 0 {
			fmt.Fprintln(r.out, "(empty domain)")
			break
		}
		r.replay()
	case ":stats":
		r.stats()
	case ":docs":
		r.docs(arg)
	case ":doc":
		r.doc(arg)
	case ":visualize", ":vis":
		r.visualize()
	case ":undo", ":u":
		if len(r.stmts) == 0 {
			fmt.Fprintln(r.out, "(nothing to undo)")
			break
		}
		r.stmts = r.stmts[:len(r.stmts)-1]
		r.dirty = true
		if len(r.stmts) > 0 {
			r.replay()
		} else {
			r.lastType = ""
			fmt.Fprintln(r.out, "(empty domain)")
		}
	case ":reset":
		r.stmts, r.lead, r.dirty, r.lastType = nil, nil, true, ""
		fmt.Fprintln(r.out, "(empty domain)")
	case ":load":
		r.load(arg)
	case ":save", ":save!":
		r.save(arg, name == ":save!")
	default:
		if why, ok := ttyOnlyCommands[name]; ok {
			fmt.Fprintf(r.out, "%s is only available on an interactive terminal (it %s)\n", name, why)
			break
		}
		fmt.Fprintf(r.out, "unknown command %s (:help lists commands)\n", name)
	}
	return false
}

// splitCommand splits a :directive into its name and its argument — the rest
// of the line, verbatim. The argument is not split on spaces: a file path may
// contain them, and `:load my program.domain` should open that file rather
// than one called "my".
func splitCommand(line string) (name, arg string) {
	name = line
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		name, arg = line[:i], strings.TrimSpace(line[i+1:])
	}
	return name, unquotePath(arg)
}

// unquotePath accepts the shapes a path arrives in: bare, quoted (because it
// has spaces), or starting with ~ for the home directory.
func unquotePath(arg string) string {
	if len(arg) >= 2 && (arg[0] == '"' && arg[len(arg)-1] == '"' || arg[0] == '\'' && arg[len(arg)-1] == '\'') {
		arg = arg[1 : len(arg)-1]
	}
	if arg == "~" || strings.HasPrefix(arg, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			arg = filepath.Join(home, strings.TrimPrefix(arg[1:], "/"))
		}
	}
	return arg
}

// load replaces the session with a file's statements, validated first: a
// broken file leaves the session untouched.
func (r *repl) load(path string) {
	if path == "" {
		fmt.Fprintln(r.out, "usage: :load <file.domain>")
		return
	}
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(r.out, "error: %v\n", err)
		return
	}
	oldStmts, oldBase := r.stmts, r.baseDir
	r.stmts = splitStatements(string(src))
	r.baseDir = filepath.Dir(path)
	if _, trial, err := r.frontEnd(r.stmts); err != nil {
		r.reportError(trial, err)
		r.stmts, r.baseDir = oldStmts, oldBase
		return
	}
	r.lead, r.dirty = nil, false
	r.replay()
}

// save writes the session's program to a file. An existing file is not
// overwritten unless the command was spelled `:save!` — a REPL is exactly
// where a mistyped path is likely, and a program is exactly what one should
// not silently destroy.
func (r *repl) save(path string, force bool) {
	if path == "" {
		fmt.Fprintln(r.out, "usage: :save <file.domain>")
		return
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(r.out, "%s already exists — use `:save! %s` to overwrite it\n", path, path)
			return
		}
	}
	src := strings.Join(r.stmts, "\n") + "\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		fmt.Fprintf(r.out, "error: %v\n", err)
		return
	}
	r.dirty = false
	fmt.Fprintf(r.out, "saved %d statement(s) to %s\n", statementCount(r.stmts), path)
}

// replay re-runs the current program and reports its value, without the
// rollback a fresh statement gets — after :undo or :load the program is not a
// trial to reject.
func (r *repl) replay() {
	pipe, src, err := r.frontEnd(r.stmts)
	if err != nil {
		r.reportError(src, err)
		return
	}
	r.evalAndShow(pipe, false)
}

// splitStatements cuts a program source into top-level statements: a new
// statement starts at every non-blank, non-comment column-0 line; indented
// lines belong to the statement above. Comments and the blank lines around
// them are *trivia* — they travel with the statement they introduce rather
// than counting as statements of their own, so `:undo` drops a statement with
// its comment, and `:save` writes the file back the way it was read.
func splitStatements(src string) []string {
	var stmts []string
	var cur, lead []string
	flush := func() {
		if len(cur) > 0 {
			stmts = append(stmts, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		indented := trimmed != "" && (trimmed[0] == ' ' || trimmed[0] == '\t')
		switch {
		case strings.TrimSpace(trimmed) == "":
			// A blank line before the first statement is leading padding, not
			// something to preserve; anywhere else it is part of the layout.
			if len(stmts) > 0 || len(cur) > 0 || len(lead) > 0 {
				lead = append(lead, "")
			}
		case !indented && strings.HasPrefix(strings.TrimSpace(trimmed), "#"):
			lead = append(lead, trimmed)
		case indented:
			cur = append(append(cur, lead...), trimmed)
			lead = nil
		default:
			flush()
			cur = append(lead, trimmed)
			lead = nil
		}
	}
	// Trailing trivia belongs to the last statement — it was written after it.
	if len(lead) > 0 && strings.TrimSpace(strings.Join(lead, "")) != "" {
		if len(cur) == 0 && len(stmts) > 0 {
			stmts[len(stmts)-1] += "\n" + strings.Join(lead, "\n")
		} else {
			cur = append(cur, lead...)
		}
	}
	flush()
	return stmts
}

// statementCount counts the chunks that carry an actual statement, so a
// program's comments are not reported as program.
func statementCount(stmts []string) int {
	n := 0
	for _, chunk := range stmts {
		for _, line := range strings.Split(chunk, "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if line[0] != ' ' && line[0] != '\t' {
				n++
				break
			}
		}
	}
	return n
}
