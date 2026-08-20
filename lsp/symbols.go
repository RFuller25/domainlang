// Where a name comes from.
//
// Everything else the server knows is answered per *statement*: hover, inlay
// hints and go-to-definition all take a line and describe what is on it. That
// is the right unit for a pipeline, where a line is a stage — and the wrong
// one for the question a reader actually asks most often, which is about one
// word rather than one line: *what is `total`, and where did it come from?*
//
// So this is the other index: every name the program itself introduces, with
// the place it was introduced, the type it was given, and — for a global — the
// lines that write to it afterwards. It is built from the AST rather than from
// the IR, so it still answers while the program does not yet type-check; the
// types come from the resolved pipeline when there is one, and are simply
// absent when there is not.
package lsp

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/lexer"
	"domain/prims"
	"domain/token"
	"domain/typecheck"
)

// Symbol kinds, as they are shown to a reader.
const (
	KindGlobal  = "global"
	KindBinding = "binding"
	KindParam   = "Shikigami parameter"
	KindLambda  = "lambda parameter"
	KindFunc    = "function binding"
)

// Symbol is one name the program introduces, and everything known about it.
type Symbol struct {
	Name string
	Kind string
	// Type is the resolved type, rendered. Empty when the program did not
	// resolve far enough to give the name one — a lambda parameter never has
	// one, since its type is decided by the primitive it is handed to.
	Type string
	// Pos is where the name is declared, and Decl is that source line as
	// written, trimmed — which says more than any summary could.
	Pos  token.Position
	Decl string
	// Writes are the `Cursed Tool` lines that change a global after it is
	// declared. A global that is never written is a constant in all but name,
	// and knowing which one you are looking at is most of the value of asking.
	Writes []int
	// From and To bound the lines this declaration governs, so a name declared
	// twice resolves to the one actually in scope rather than to whichever was
	// written last.
	From, To int
}

// scope reports whether the declaration governs the given line.
func (s Symbol) scope(line int) bool { return line >= s.From && line <= s.To }

// Symbols indexes every name the program declares, in declaration order.
//
// Names are not unique: a binding may shadow a global, and two stages may bind
// the same name independently. Each declaration is its own entry, and
// SymbolAt picks between them by scope.
func (a *Analysis) Symbols() []Symbol {
	if a == nil || a.Prog == nil {
		return nil
	}
	_, bindByLine := a.typesOrEmpty()
	ends := newLineIndex(a.Prog)

	var out []Symbol
	typeOf := func(line int) string {
		if t := bindByLine[line]; t != nil {
			return t.String()
		}
		return ""
	}
	// A binding's type comes from the resolved pipeline where there is one.
	// Where there is not — an `As` binding of a constant is folded into the
	// lambda that reads it and never becomes a node of its own — the expression
	// is typed directly. It is asked in an empty environment, so a value that
	// depends on anything else simply has no answer, which is the honest one.
	bindingType := func(b *ast.Binding) string {
		if t := typeOf(b.Pos.Line); t != "" {
			return t
		}
		if b.Value == nil {
			return ""
		}
		if t, err := typecheck.ExprType(b.Value, typecheck.Env{}); err == nil && t != nil {
			return t.String()
		}
		return ""
	}
	// The kind a binding is shown as. `Consider f As (x) -> …` binds a
	// *function*, called by name from the stage's expressions; `Consider m Of
	// (xs) -> …` binds the value that lambda computes from the pipeline. Same
	// lambda, and the preposition is the whole difference — which is exactly
	// why it is worth saying which one a reader is looking at.
	bindKind := func(b *ast.Binding) string {
		if b.Lambda != nil && !b.Of {
			return KindFunc
		}
		return KindBinding
	}
	add := func(s Symbol) {
		s.Decl = strings.TrimSpace(lineText(a.Text, s.Pos.Line))
		out = append(out, s)
	}

	// The globals of a `Cursed Object`, and the `Cursed Tool` lines that write
	// them. A write is recorded against the declaration it changes rather than
	// as a declaration of its own: `Cursed Tool: n As n + 1` introduces
	// nothing, and reporting it as the place `n` comes from would send a reader
	// past the line that actually says what `n` is.
	var writes []Symbol
	var walkGlobals func(stmts []*ast.Statement)
	walkGlobals = func(stmts []*ast.Statement) {
		for _, st := range stmts {
			for _, d := range st.Decls {
				if st.Keyword == "Cursed Tool" {
					writes = append(writes, Symbol{Name: d.Name, Pos: d.Pos})
					continue
				}
				add(Symbol{
					Name: d.Name, Kind: KindGlobal, Type: typeOf(d.Pos.Line),
					Pos: d.Pos, From: d.Pos.Line, To: math.MaxInt,
				})
			}
			walkGlobals(st.Block)
			for _, b := range st.Binds {
				walkGlobals(b.Body)
			}
		}
	}
	walkGlobals(a.Prog.Statements)
	for _, def := range a.Prog.Shikigamis {
		walkGlobals(def.Body)
	}

	// Stage bindings and lambda parameters, scoped to the statement that owns
	// them: both are visible to the expressions of that statement and of the
	// statements nested beneath it, and nowhere else.
	var walkLocals func(stmts []*ast.Statement)
	walkLocals = func(stmts []*ast.Statement) {
		for _, st := range stmts {
			end := ends.endOf(st)
			for _, b := range st.Binds {
				add(Symbol{
					Name: b.Name, Kind: bindKind(b), Type: bindingType(b),
					Pos: b.Pos, From: b.Pos.Line, To: end,
				})
				for _, p := range lambdaParams(b.Lambda) {
					add(Symbol{Name: p, Kind: KindLambda, Pos: b.Lambda.Pos,
						From: b.Lambda.Pos.Line, To: end})
				}
				walkLocals(b.Body)
			}
			for _, arg := range st.Args {
				la, ok := arg.Value.(ast.LambdaArg)
				if !ok {
					continue
				}
				for _, p := range lambdaParams(la.Lambda) {
					add(Symbol{Name: p, Kind: KindLambda, Pos: la.Lambda.Pos,
						From: la.Lambda.Pos.Line, To: max(end, la.Lambda.Pos.Line)})
				}
			}
			walkLocals(st.Block)
		}
	}
	walkLocals(a.Prog.Statements)

	// A Shikigami's parameters, and the bindings written at the top of its
	// body: both are scoped to the definition and are invisible outside it.
	for _, def := range a.Prog.Shikigamis {
		from, to := def.Pos.Line, ends.endOfBody(def)
		for _, p := range def.Params {
			add(Symbol{
				Name: p.Name, Kind: KindParam, Type: prims.TypeString(p.Type),
				Pos: def.Pos, From: from, To: to,
			})
		}
		for _, b := range def.Binds {
			add(Symbol{
				Name: b.Name, Kind: bindKind(b), Type: bindingType(b),
				Pos: b.Pos, From: b.Pos.Line, To: to,
			})
		}
		walkLocals(def.Body)
	}

	// Attach each write to the declaration in scope where it was written.
	for _, w := range writes {
		if i := pick(out, w.Name, w.Pos.Line); i >= 0 {
			out[i].Writes = append(out[i].Writes, w.Pos.Line)
		}
	}
	return out
}

// typesOrEmpty is typesByLine with a nil pipeline tolerated: a program that
// does not resolve still has names, and their declarations are still worth
// pointing at even when no type can be put on them.
func (a *Analysis) typesOrEmpty() (outByLine, bindByLine map[int]*ir.Type) {
	if a.Pipe == nil {
		return map[int]*ir.Type{}, map[int]*ir.Type{}
	}
	return a.typesByLine()
}

func lambdaParams(l *ast.Lambda) []string {
	if l == nil {
		return nil
	}
	return l.Params
}

// pick chooses the declaration of name that governs line: the innermost one,
// which is the last to have been declared among those still in scope.
//
// A name read *above* everything that declares it falls back to the first
// declaration written below, because that is the state a program is in while
// it is being written — the read is typed first and the declaration follows —
// and answering there is the point. A declaration whose scope has already
// closed is not a fallback: it is a different name that happens to be spelled
// the same, and pointing at it would be worse than saying nothing.
func pick(syms []Symbol, name string, line int) int {
	best, forward := -1, -1
	for i, s := range syms {
		if s.Name != name {
			continue
		}
		if s.scope(line) && (best < 0 || s.Pos.Line >= syms[best].Pos.Line) {
			best = i
		}
		if forward < 0 && s.From > line {
			forward = i
		}
	}
	if best >= 0 {
		return best
	}
	return forward
}

// SymbolAt is the whole "what is this word" question: the name under a
// position, resolved to the declaration in scope there.
//
// line is 1-based and col is a 0-based *byte* offset into that line, which is
// what both callers hold — the editor works in bytes, and the protocol handler
// converts its UTF-16 offset before asking.
func (a *Analysis) SymbolAt(line, col int) (Symbol, bool) {
	name, ok := a.WordAt(line, col)
	if !ok {
		return Symbol{}, false
	}
	syms := a.Symbols()
	i := pick(syms, name, line)
	if i < 0 {
		return Symbol{}, false
	}
	return syms[i], true
}

// WordAt returns the identifier at a position, if the position is on one.
//
// The lexer decides, so a word inside a string literal or a comment is not an
// identifier and nothing is reported for it — which is the whole reason this
// does not simply scan for word characters. Source that does not lex falls
// back to exactly that scan, because the moment a reader most wants to know
// what a name is tends to be while the line they are looking at is broken.
func (a *Analysis) WordAt(line, col int) (string, bool) {
	if a == nil {
		return "", false
	}
	src := lineText(a.Text, line)
	if col < 0 || col > len(src) {
		return "", false
	}
	// A cursor sitting just past a word — where it lands after typing one — is
	// on that word.
	if (col == len(src) || !isNameByte(src[col])) && col > 0 && isNameByte(src[col-1]) {
		col--
	}
	if col >= len(src) || !isNameByte(src[col]) {
		return "", false
	}
	start, end := col, col
	for start > 0 && isNameByte(src[start-1]) {
		start--
	}
	for end < len(src) && isNameByte(src[end]) {
		end++
	}
	word := src[start:end]

	if toks, err := lexer.Lex(a.Text); err == nil {
		off := lineStartOffset(a.Text, line) + start
		for _, t := range toks {
			if t.Kind == token.IDENT && t.Pos.Offset == off && t.Literal == word {
				return word, true
			}
			if t.Pos.Offset > off {
				break
			}
		}
		return "", false // inside a string, a comment, or a foreign block
	}
	return word, true
}

func isNameByte(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// lineStartOffset is the byte offset of the start of a 1-based line.
func lineStartOffset(text string, line int) int {
	if line <= 1 {
		return 0
	}
	cur, start := 1, 0
	for i := range len(text) {
		if text[i] != '\n' {
			continue
		}
		cur++
		start = i + 1
		if cur == line {
			return start
		}
	}
	return start
}

// Describe renders a symbol as the markdown an editor shows on hover.
func (s Symbol) Describe() string {
	var b strings.Builder
	if s.Type != "" {
		fmt.Fprintf(&b, "**%s** — `%s`\n\n", s.Name, s.Type)
	} else {
		fmt.Fprintf(&b, "**%s**\n\n", s.Name)
	}
	fmt.Fprintf(&b, "_%s, declared on line %d_", s.Kind, s.Pos.Line)
	if s.Decl != "" {
		fmt.Fprintf(&b, "\n\n```domain\n%s\n```", s.Decl)
	}
	switch len(s.Writes) {
	case 0:
		if s.Kind == KindGlobal {
			b.WriteString("\n\n_never written after it is declared_")
		}
	case 1:
		fmt.Fprintf(&b, "\n\n_written on line %d_", s.Writes[0])
	default:
		fmt.Fprintf(&b, "\n\n_written on lines %s_", joinInts(s.Writes))
	}
	return b.String()
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// scope ends
// ---------------------------------------------------------------------------

// lineIndex answers "where does this statement's block stop?".
//
// A binding is visible to the statement that owns it and to everything nested
// beneath it, so its scope ends where that nesting ends — one line before the
// next statement written outside it. The heads of every statement in the file
// are enough to find that: the subtree's last head line is known, and the next
// head after it belongs to something else.
//
// It is an approximation in one direction only. A statement whose last child
// carries several lines of argument value ends a little early, which can cost
// a hover on those lines its answer; nothing here ever reports a name as in
// scope where it is not.
type lineIndex struct{ heads []int }

func newLineIndex(prog *ast.Program) *lineIndex {
	li := &lineIndex{}
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, st := range stmts {
			li.heads = append(li.heads, st.Pos.Line)
			for _, b := range st.Binds {
				li.heads = append(li.heads, b.Pos.Line)
				walk(b.Body)
			}
			for _, d := range st.Decls {
				li.heads = append(li.heads, d.Pos.Line)
			}
			walk(st.Block)
		}
	}
	walk(prog.Statements)
	for _, def := range prog.Shikigamis {
		li.heads = append(li.heads, def.Pos.Line)
		for _, b := range def.Binds {
			li.heads = append(li.heads, b.Pos.Line)
		}
		walk(def.Body)
	}
	slices.Sort(li.heads)
	return li
}

// endOf is the last line governed by a statement.
func (li *lineIndex) endOf(st *ast.Statement) int {
	return li.after(stmtMaxLine(st))
}

// endOfBody is the last line governed by a Shikigami definition.
func (li *lineIndex) endOfBody(def *ast.ShikigamiDef) int {
	last := def.Pos.Line
	for _, b := range def.Binds {
		last = max(last, b.Pos.Line)
	}
	for _, st := range def.Body {
		last = max(last, stmtMaxLine(st))
	}
	return li.after(last)
}

// after is the line before the next statement head written past line.
func (li *lineIndex) after(line int) int {
	if i, _ := slices.BinarySearch(li.heads, line+1); i < len(li.heads) {
		return li.heads[i] - 1
	}
	return math.MaxInt
}

// stmtMaxLine is the last statement head line inside a statement's subtree.
func stmtMaxLine(st *ast.Statement) int {
	last := st.Pos.Line
	for _, b := range st.Binds {
		last = max(last, b.Pos.Line)
		for _, sub := range b.Body {
			last = max(last, stmtMaxLine(sub))
		}
	}
	for _, d := range st.Decls {
		last = max(last, d.Pos.Line)
	}
	for _, a := range st.Args {
		last = max(last, a.Pos.Line)
	}
	for _, sub := range st.Block {
		last = max(last, stmtMaxLine(sub))
	}
	return last
}
