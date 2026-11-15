// Package tidy rewrites a Domain program into the shape the language's own
// advice describes, without changing what it computes.
//
// It is the automatic half of a list of best practices: destructure tuple
// parameters instead of indexing them, share work above the Parts, fold
// `Case:` lines that differ in one word into one template, stop writing a
// global that only needed to be set once, and pick one spelling per file. The
// editor runs it on save.
//
// Three rules shape every pass, and they are why this can run uninvited.
//
// **A pass edits text, not a tree.** The lexer discards comments, so printing
// a rewritten AST would delete every one of them — the same constraint package
// format works under. Each pass finds what it wants in the AST, locates it in
// the token stream, and replaces byte ranges of the source.
//
// **A pass that is not certain does nothing.** Every condition here is
// conservative: a name that might be shadowed, a statement that might print, a
// global that might be read in between — each is a reason to leave the program
// exactly as written. What could not be done is reported as a note, which is
// advice, rather than done badly.
//
// **Nothing is kept unless the result still resolves.** After each pass the
// program is lexed, parsed, resolved and type-checked again, and the types
// every `Reveal` prints are compared with what they were. A pass whose output
// fails any of that is discarded whole and the others carry on. A program that
// does not resolve to begin with is returned untouched: tidying is for working
// programs, and `domain expansion: fix` is what repairs broken ones.
package tidy

import (
	"fmt"
	"sort"
	"strings"

	"domain/ast"
	"domain/format"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
	"domain/token"
	"domain/typecheck"
)

// Result is what tidying a program came to.
type Result struct {
	// Src is the tidied program — the input itself when nothing applied.
	Src string
	// Applied describes each rewrite that was kept, in the order they ran.
	Applied []string
	// Notes is advice for what was noticed but could not be done safely.
	Notes []string
}

// Changed reports whether anything was rewritten.
func (r Result) Changed() bool { return len(r.Applied) > 0 }

// Summary is one line describing what was applied, for a status bar.
func (r Result) Summary() string { return strings.Join(r.Applied, ", ") }

// Options selects what Apply does beyond the rewrites.
type Options struct {
	// Format runs the canonical formatter last. The editor's save wants it;
	// a test of one pass does not.
	Format bool
}

// Apply tidies src. path is the program's file, used to find the libraries it
// imports; it may be empty.
//
// It touches the front end's package-level state (the type checker's binding
// stack and lambda observer), so a caller that runs analyses concurrently must
// hold whatever lock serialises them.
func Apply(path, src string, opts Options) Result {
	res := Result{Src: src}
	hadNL := strings.HasSuffix(src, "\n")

	cur, err := load(path, src)
	if err != nil {
		return res
	}

	for _, p := range passes {
		edits, summary, notes := p.run(cur)
		res.Notes = append(res.Notes, notes...)
		if len(edits) == 0 {
			continue
		}
		next, ok := applyEdits(cur.src, edits)
		if !ok || next == cur.src {
			continue
		}
		nu, err := load(path, next)
		if err != nil {
			continue
		}
		if p.exact {
			if fingerprint(cur.pipe) != fingerprint(nu.pipe) {
				continue
			}
		} else if revealTypes(cur.pipe) != revealTypes(nu.pipe) {
			continue
		}
		cur = nu
		res.Applied = append(res.Applied, summary)
	}

	out := cur.src
	if opts.Format {
		if f, err := format.Format(out); err == nil {
			if strings.TrimSuffix(f, "\n") != strings.TrimSuffix(out, "\n") {
				res.Applied = append(res.Applied, "formatted")
			}
			out = f
		}
	}
	out = strings.TrimSuffix(out, "\n")
	if hadNL {
		out += "\n"
	}
	if len(res.Applied) == 0 {
		return res
	}
	res.Src = out
	return res
}

// applyOne runs a single rewrite that was asked for by name, under the same
// rule as the ones that run on save: nothing is kept unless the program still
// resolves and prints values of the types it printed before.
func applyOne(path, src string, run func(u *unit) ([]edit, string, error)) (Result, error) {
	res := Result{Src: src}
	hadNL := strings.HasSuffix(src, "\n")
	u, err := load(path, src)
	if err != nil {
		return res, fmt.Errorf("the program does not check yet: %s", firstLine(err.Error()))
	}
	edits, summary, err := run(u)
	if err != nil {
		return res, err
	}
	next, ok := applyEdits(u.src, edits)
	if !ok || next == u.src {
		return res, fmt.Errorf("nothing to change")
	}
	nu, err := load(path, next)
	if err != nil {
		return res, fmt.Errorf("that would not check: %s", firstLine(err.Error()))
	}
	if revealTypes(u.pipe) != revealTypes(nu.pipe) {
		return res, fmt.Errorf("that would change what the program prints")
	}
	next = strings.TrimSuffix(next, "\n")
	if hadNL {
		next += "\n"
	}
	res.Src, res.Applied = next, []string{summary}
	return res, nil
}

// IsName reports whether s can be written as an identifier.
func IsName(s string) bool {
	if s == "" || s == "_" || s[0] >= '0' && s[0] <= '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !token.IsIdentByte(s[i]) {
			return false
		}
	}
	return !strings.EqualFold(s, token.CommentWord)
}

// pass is one rewrite. exact demands that the resolved pipeline be identical
// afterwards, which is the right bar for a pass that only changes spelling;
// the others change the pipeline's shape on purpose and are held to what the
// program prints instead.
type pass struct {
	name  string
	run   func(u *unit) (edits []edit, summary string, notes []string)
	exact bool
}

// The order matters in two places. Destructuring runs first because it is the
// most local. Hoisting runs after the passes that rewrite statements, so two
// Parts that were about to become identical are compared after they have.
var passes = []pass{
	{name: "destructure", run: passDestructure},
	{name: "cases", run: passCases},
	{name: "globals", run: passLateGlobals},
	{name: "parts", run: passHoist},
	{name: "keywords", run: passKeywordSpelling, exact: true},
	{name: "case", run: passPhraseCase, exact: true},
}

// ---------------------------------------------------------------------------
// one program, read every way a pass needs it
// ---------------------------------------------------------------------------

// unit is a program with everything derived from it.
type unit struct {
	path string
	src  string
	toks []token.Token
	prog *ast.Program
	pipe *ir.Pipeline
	// tokAt maps a byte offset to the index of the token starting there.
	// Layout tokens (INDENT, DEDENT) share an offset with the token after
	// them; the later index wins, which is the real token.
	tokAt map[int]int
	// lamTypes is the parameter types each lambda was checked against, keyed
	// by the lambda's offset. A lambda checked more than once — a Shikigami
	// called twice — has one entry per check.
	lamTypes map[int][][]*ir.Type
	// lamResult is the type each of those checks gave the lambda's body.
	lamResult map[int][]*ir.Type
}

func load(path, src string) (u *unit, err error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return nil, err
	}
	u = &unit{path: path, src: src, toks: toks, prog: prog,
		tokAt: make(map[int]int, len(toks)), lamTypes: map[int][][]*ir.Type{},
		lamResult: map[int][]*ir.Type{}}
	for i, t := range toks {
		u.tokAt[t.Pos.Offset] = i
	}

	prev := typecheck.LambdaObserver
	typecheck.LambdaObserver = func(l *ast.Lambda, types []*ir.Type, result *ir.Type) {
		u.lamTypes[l.Pos.Offset] = append(u.lamTypes[l.Pos.Offset], append([]*ir.Type(nil), types...))
		u.lamResult[l.Pos.Offset] = append(u.lamResult[l.Pos.Offset], result)
	}
	defer func() { typecheck.LambdaObserver = prev }()

	u.pipe, err = prims.ResolveWith(prog, prims.FileOptions(path))
	if err != nil {
		return nil, err
	}
	return u, nil
}

// nested returns the nodes a node runs as its body, if it has one.
func nested(n *ir.Node) []*ir.Node {
	if n == nil || n.Meta == nil {
		return nil
	}
	ns, _ := n.Meta["nodes"].([]*ir.Node)
	return ns
}

// fingerprint renders a pipeline closely enough that two programs with the
// same one are the same program.
func fingerprint(p *ir.Pipeline) string {
	var b strings.Builder
	var walk func(ns []*ir.Node, depth int)
	walk = func(ns []*ir.Node, depth int) {
		for _, n := range ns {
			fmt.Fprintf(&b, "%d|%s|%s|%s|%s\n", depth, n.Prim, n.Display, n.In, n.Out)
			walk(nested(n), depth+1)
		}
	}
	walk(p.Nodes, 0)
	return b.String()
}

// revealTypes is the type of every value the program prints, in order, with
// the label it prints under. It is what must not move when a pass reshapes
// the pipeline: a record whose fields changed order still type-checks
// everywhere it is read, and prints differently.
func revealTypes(p *ir.Pipeline) string {
	var b strings.Builder
	var walk func(ns []*ir.Node, frame string)
	walk = func(ns []*ir.Node, frame string) {
		for _, n := range ns {
			if strings.HasPrefix(n.Display, "Reveal -> ") {
				fmt.Fprintf(&b, "%s|%s|%s\n", frame, n.Display, n.In)
			}
			f := frame
			if n.Prim == "Part" {
				f = n.Display
			}
			walk(nested(n), f)
		}
	}
	walk(p.Nodes, "")
	return b.String()
}

// ---------------------------------------------------------------------------
// edits
// ---------------------------------------------------------------------------

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
}

// applyEdits applies every edit at once. Overlapping edits mean two rewrites
// disagreed about the same text, and the answer to that is to apply neither.
func applyEdits(src string, es []edit) (string, bool) {
	sort.SliceStable(es, func(i, j int) bool { return es[i].start < es[j].start })
	var b strings.Builder
	at := 0
	for _, e := range es {
		if e.start < at || e.end < e.start || e.end > len(src) {
			return src, false
		}
		b.WriteString(src[at:e.start])
		b.WriteString(e.text)
		at = e.end
	}
	b.WriteString(src[at:])
	return b.String(), true
}

// lineStart is the offset of the first byte of the line holding off.
func lineStart(src string, off int) int {
	return strings.LastIndexByte(src[:off], '\n') + 1
}

// lineEnd is the offset of the newline ending the line holding off, or the
// end of the source.
func lineEnd(src string, off int) int {
	if i := strings.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i
	}
	return len(src)
}

// lineOffset is the offset at which a 1-based line starts; one past the last
// line is the end of the source.
func lineOffset(src string, line int) int {
	off := 0
	for i := 1; i < line; i++ {
		j := strings.IndexByte(src[off:], '\n')
		if j < 0 {
			return len(src)
		}
		off += j + 1
	}
	return off
}

// deleteLines removes whole lines first..last (1-based, inclusive), newline
// and all.
func deleteLines(src string, first, last int) edit {
	return edit{start: lineOffset(src, first), end: lineOffset(src, last+1)}
}

// restIsBlank reports whether nothing but whitespace follows off on its line —
// in particular, no comment a deletion would take with it.
func restIsBlank(src string, off int) bool {
	return strings.TrimSpace(src[off:lineEnd(src, off)]) == ""
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func isLayout(k token.Kind) bool {
	return k == token.NEWLINE || k == token.INDENT || k == token.DEDENT
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// ---------------------------------------------------------------------------
// walking the tree
// ---------------------------------------------------------------------------

// eachStatement visits every statement in the program that stands on a line
// of its own — the pipeline, every block under it, and every Shikigami body —
// with the Shikigami it is inside, if any. The statements behind an `Of`
// binding are not visited: they sit mid-line, and nothing here rewrites them.
func (u *unit) eachStatement(f func(s *ast.Statement, def *ast.ShikigamiDef)) {
	var walk func(ss []*ast.Statement, def *ast.ShikigamiDef)
	walk = func(ss []*ast.Statement, def *ast.ShikigamiDef) {
		for _, s := range ss {
			if s == nil {
				continue
			}
			f(s, def)
			walk(s.Block, def)
		}
	}
	for _, d := range u.prog.Shikigamis {
		walk(d.Body, d)
	}
	walk(u.prog.Statements, nil)
}

// lambdaSite is a lambda and the top-level construct it was written in.
type lambdaSite struct {
	lam *ast.Lambda
	top int // offset of the enclosing top-level statement or definition
}

// lambdas returns every lambda in the program, in source order.
func (u *unit) lambdas() []lambdaSite {
	var out []lambdaSite
	var stmts func(ss []*ast.Statement, top int)
	binds := func(bs []*ast.Binding, top int) {
		for _, b := range bs {
			if b == nil {
				continue
			}
			if b.Lambda != nil {
				out = append(out, lambdaSite{b.Lambda, top})
			}
			stmts(b.Body, top)
		}
	}
	stmts = func(ss []*ast.Statement, top int) {
		for _, s := range ss {
			if s == nil {
				continue
			}
			t := top
			if t < 0 {
				t = s.Pos.Offset
			}
			for _, a := range s.Args {
				if la, ok := a.Value.(ast.LambdaArg); ok && la.Lambda != nil {
					out = append(out, lambdaSite{la.Lambda, t})
				}
			}
			binds(s.Binds, t)
			binds(s.Decls, t)
			stmts(s.Block, t)
		}
	}
	for _, d := range u.prog.Shikigamis {
		binds(d.Binds, d.Pos.Offset)
		stmts(d.Body, d.Pos.Offset)
	}
	stmts(u.prog.Statements, -1)
	sort.SliceStable(out, func(i, j int) bool { return out[i].lam.Pos.Offset < out[j].lam.Pos.Offset })
	return out
}

// walkExpr visits e and everything under it, parents first.
func walkExpr(e ast.Expr, f func(ast.Expr)) {
	if e == nil {
		return
	}
	f(e)
	switch x := e.(type) {
	case *ast.BinaryExpr:
		walkExpr(x.Left, f)
		walkExpr(x.Right, f)
	case *ast.UnaryExpr:
		walkExpr(x.X, f)
	case *ast.FieldAccess:
		walkExpr(x.Target, f)
	case *ast.CallExpr:
		walkExpr(x.Fn, f)
		for _, a := range x.Args {
			walkExpr(a, f)
		}
	case *ast.CondExpr:
		walkExpr(x.Cond, f)
		walkExpr(x.Then, f)
		walkExpr(x.Else, f)
	case *ast.LetExpr:
		walkExpr(x.Value, f)
		walkExpr(x.Body, f)
	case *ast.AssignExpr:
		walkExpr(x.Value, f)
	case *ast.AlsoExpr:
		walkExpr(x.Body, f)
		for _, c := range x.Clauses {
			walkExpr(c, f)
		}
	}
}

// countIdent is how many times name is read under e.
func countIdent(e ast.Expr, name string) int {
	n := 0
	walkExpr(e, func(x ast.Expr) {
		if id, ok := x.(*ast.Ident); ok && id.Name == name {
			n++
		}
	})
	return n
}

// topLevelStarts is the offset of everything that begins at the top level, in
// order, with the end of the source last — so the extent of any one of them is
// up to the next.
func (u *unit) topLevelStarts() []int {
	var out []int
	for _, s := range u.prog.Statements {
		out = append(out, s.Pos.Offset)
	}
	for _, d := range u.prog.Shikigamis {
		out = append(out, d.Pos.Offset)
	}
	for _, im := range u.prog.Imports {
		out = append(out, im.Pos.Offset)
	}
	if u.prog.Scope != nil {
		out = append(out, u.prog.Scope.Pos.Offset)
	}
	sort.Ints(out)
	return append(out, len(u.src))
}

// nextStart is the first top-level start after off.
func nextStart(starts []int, off int) int {
	for _, s := range starts {
		if s > off {
			return s
		}
	}
	return starts[len(starts)-1]
}

// tokRange is the tokens of a construct starting at start and ending before
// limit, with trailing layout trimmed: [first, last).
func (u *unit) tokRange(start, limit int) (first, last int) {
	first, ok := u.tokAt[start]
	if !ok {
		return 0, 0
	}
	last = first
	for last < len(u.toks) && u.toks[last].Kind != token.EOF && u.toks[last].Pos.Offset < limit {
		last++
	}
	for last > first && isLayout(u.toks[last-1].Kind) {
		last--
	}
	return first, last
}

// lastLine is the line the last token of [first, last) ends on.
func (u *unit) lastLine(first, last int) int {
	if last <= first {
		return 0
	}
	t := u.toks[last-1]
	return t.Pos.Line + strings.Count(u.src[t.Pos.Offset:min(t.End, len(u.src))], "\n")
}

// Check reports why a program cannot be tidied: it must lex, parse, resolve
// and type-check, since every rewrite is verified by doing all four again.
func Check(path, src string) error {
	_, err := load(path, src)
	return err
}
