// Package format implements `domain fmt`: canonical whitespace for Domain
// source.
//
// The formatter is deliberately **line-oriented** rather than a printer over
// the AST. The lexer discards comments — there is no COMMENT token — so
// re-printing a parsed program would delete every comment in the file, which
// is disqualifying for a language whose programs are heavily commented. So
// each physical line is rewritten in place: its indentation is recomputed from
// the lexer's own INDENT/DEDENT layout, and its interior is normalized only as
// far as is provably safe.
//
// "Provably safe" is the whole design constraint, and it splits lines in two:
//
//   - **Statement lines** carry an operation phrase whose *raw source text* is
//     load-bearing: `prims` reads `Operation.Raw` verbatim as a Shikigami call
//     name (shikigami.go) and as a `Read Source` file target (builtins.go). A
//     phrase's interior is therefore copied byte for byte, so `data/day1.txt`
//     can never become `data / day1.txt` and a Shikigami name can never lose a
//     space. Only the keyword segment ahead of the colon is normalized.
//   - **Argument lines** (`Using:`, `Seed:`, `Mode:`, `From:`) are parsed
//     structurally and their source text is never used as a key, so they are
//     re-rendered canonically from their tokens.
//
// Keywords are preserved exactly as written. Every keyword in Domain is
// optional and mixing the two spellings line by line is an intentional
// language feature, so the formatter never adds or removes one.
package format

import (
	"strings"

	"domain/ast"
	"domain/lexer"
	"domain/parser"
	"domain/token"
)

// indentWidth is one level of indentation. Domain's indentation is spaces
// only (a tab in indentation is a lex error), so this is the sole unit.
const indentWidth = 4

// Format returns src with canonical whitespace. It is idempotent.
//
// A source that does not lex or parse is returned **unchanged** along with the
// error: the formatter never rewrites a file it cannot fully understand, so a
// broken program can never be made worse by running fmt over it. (Tab
// indentation is a lex error, which is why `domain expansion: fix` — not fmt —
// is what repairs it.)
func Format(src string) (string, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return src, err
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return src, err
	}

	depths := lineDepths(toks)
	args := argLines(prog)
	byLine := tokensByLine(toks)

	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	pendingBlank := false

	for i, raw := range lines {
		lineNo := i + 1
		code, comment := splitComment(raw)
		codeTrimmed := strings.TrimSpace(code)

		// Blank and comment-only lines carry no layout tokens of their own.
		if codeTrimmed == "" {
			if strings.TrimSpace(comment) == "" {
				// A blank line: remember it, emit at most one, and never at the
				// very start or end of the file.
				if len(out) > 0 {
					pendingBlank = true
				}
				continue
			}
			// A comment on its own line documents what follows, so it takes the
			// depth of the next line that has content; at the end of a block or
			// file it keeps the previous content line's depth instead.
			d := commentDepth(depths, lineNo, len(lines))
			out = appendWithBlank(out, &pendingBlank, indent(d)+strings.TrimSpace(comment))
			continue
		}

		d := depths[lineNo]
		var body string
		if args[lineNo] {
			body = renderTokens(src, byLine[lineNo])
		} else {
			body = renderStatement(src, byLine[lineNo], code)
		}
		out = appendWithBlank(out, &pendingBlank, indent(d)+body+joinComment(comment))
	}

	// Exactly one trailing newline, and nothing for an empty file.
	if len(out) == 0 {
		return "", nil
	}
	return strings.Join(out, "\n") + "\n", nil
}

// appendWithBlank appends line, first emitting the one pending blank line if a
// run of them was seen (collapsing runs of 2+ to a single separator).
func appendWithBlank(out []string, pending *bool, line string) []string {
	if *pending {
		out = append(out, "")
		*pending = false
	}
	return append(out, line)
}

func indent(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat(" ", depth*indentWidth)
}

// joinComment renders a trailing comment. The original whitespace run before
// the `#` is preserved (so hand-aligned trailing comments stay aligned),
// with at least one space.
func joinComment(comment string) string {
	if comment == "" {
		return ""
	}
	gap := comment[:len(comment)-len(strings.TrimLeft(comment, " \t"))]
	if gap == "" {
		gap = " "
	}
	return gap + strings.TrimRight(strings.TrimLeft(comment, " \t"), " \t")
}

// lineDepths maps each source line that has content to its block depth, taken
// from the lexer's own INDENT/DEDENT stream — the same layout the parser sees,
// so the formatter's indentation cannot disagree with the program's structure.
// Layout tokens are emitted before their line's content, so the counter is
// already correct when the first content token of a line is reached.
func lineDepths(toks []token.Token) map[int]int {
	depths := make(map[int]int)
	depth := 0
	for _, t := range toks {
		switch t.Kind {
		case token.INDENT:
			depth++
			continue
		case token.DEDENT:
			if depth > 0 {
				depth--
			}
			continue
		case token.NEWLINE, token.EOF:
			continue
		}
		if _, seen := depths[t.Pos.Line]; !seen {
			depths[t.Pos.Line] = depth
		}
	}
	return depths
}

// commentDepth picks the indentation for a comment-only line: the next content
// line's depth, else the previous content line's, else the top level.
func commentDepth(depths map[int]int, lineNo, total int) int {
	for l := lineNo + 1; l <= total; l++ {
		if d, ok := depths[l]; ok {
			return d
		}
	}
	for l := lineNo - 1; l >= 1; l-- {
		if d, ok := depths[l]; ok {
			return d
		}
	}
	return 0
}

// argLines collects the lines holding a named argument (`Using:`, `Mode:`,
// `Seed:`, `From:`), walking the whole program — top level, nested blocks, and
// Shikigami bodies. An argument is always exactly one line (the parser expects
// a NEWLINE after its value), so a line number identifies it exactly.
func argLines(prog *ast.Program) map[int]bool {
	lines := make(map[int]bool)
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			for _, a := range s.Args {
				lines[a.Pos.Line] = true
			}
			walk(s.Block)
		}
	}
	walk(prog.Statements)
	for _, d := range prog.Shikigamis {
		walk(d.Body)
	}
	return lines
}

// tokensByLine groups the real (non-layout) tokens by source line.
func tokensByLine(toks []token.Token) map[int][]token.Token {
	byLine := make(map[int][]token.Token)
	for _, t := range toks {
		switch t.Kind {
		case token.INDENT, token.DEDENT, token.NEWLINE, token.EOF:
			continue
		}
		byLine[t.Pos.Line] = append(byLine[t.Pos.Line], t)
	}
	return byLine
}

// text returns a token's exact source text. Using the source slice rather than
// the token's decoded Literal keeps string escapes exactly as the author wrote
// them — the formatter has no business rewriting `\t` into a tab.
func text(src string, t token.Token) string {
	if t.Pos.Offset < 0 || t.End > len(src) || t.Pos.Offset >= t.End {
		return t.Literal
	}
	return src[t.Pos.Offset:t.End]
}

// renderStatement normalizes a statement line conservatively: the keyword
// segment ahead of the colon is re-rendered from its tokens (collapsing
// whitespace runs), and the operation phrase after it is copied verbatim from
// source, because `Operation.Raw` is read as a Shikigami name and as a file
// path. A line with no keyword colon (a bare operation phrase) is copied whole.
func renderStatement(src string, toks []token.Token, code string) string {
	trimmed := strings.TrimRight(code, " \t")
	k := keywordColon(toks)
	if k < 0 {
		return strings.TrimLeft(trimmed, " \t")
	}

	head := renderTokens(src, toks[:k+1]) // includes the colon
	phrase := ""
	if k+1 < len(toks) {
		// Verbatim from the first phrase token to the end of this line's code.
		// The line's absolute start is the first token's offset minus the
		// leading whitespace that token sits behind.
		lead := len(code) - len(strings.TrimLeft(code, " \t"))
		lineStart := toks[0].Pos.Offset - lead
		start, end := toks[k+1].Pos.Offset, lineStart+len(code)
		if start >= 0 && start <= end && end <= len(src) {
			phrase = strings.TrimRight(src[start:end], " \t")
		} else { // defensive: fall back to canonical rendering
			phrase = renderTokens(src, toks[k+1:])
		}
	}
	if phrase == "" {
		return head
	}
	return head + " " + phrase
}

// keywordColon returns the index of the colon that ends a statement's keyword
// segment, or -1 when the line has none. The segment must be identifier words
// (a themed keyword) optionally followed by a string (`Channel "moves":`), so
// a colon inside a phrase — or one in an expression — is never mistaken for it.
func keywordColon(toks []token.Token) int {
	for i, t := range toks {
		switch t.Kind {
		case token.IDENT:
			continue
		case token.STRING:
			// Only legal immediately before the colon (`Channel "name":`).
			if i+1 < len(toks) && toks[i+1].Kind == token.COLON {
				return i + 1
			}
			return -1
		case token.COLON:
			if i == 0 {
				return -1
			}
			return i
		default:
			return -1
		}
	}
	return -1
}

// renderTokens re-renders a token run with canonical spacing. Used for
// argument lines in full, and for statement keyword segments.
func renderTokens(src string, toks []token.Token) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 && needsSpace(toks[i-1], t, prevSignificant(toks, i-1)) {
			b.WriteByte(' ')
		}
		b.WriteString(text(src, t))
	}
	return b.String()
}

// prevSignificant returns the token before index i, or a zero token when i is
// the first — used to decide whether a `-` is unary.
func prevSignificant(toks []token.Token, i int) token.Token {
	if i <= 0 {
		return token.Token{}
	}
	return toks[i-1]
}

// needsSpace decides whether a space separates prev from cur. beforePrev is
// the token before prev, needed to tell a unary minus from a binary one.
func needsSpace(prev, cur, beforePrev token.Token) bool {
	// Never a space before these.
	switch cur.Kind {
	case token.COLON, token.COMMA, token.RPAREN, token.DOT:
		return false
	}
	// Never a space after these.
	switch prev.Kind {
	case token.LPAREN, token.DOT:
		return false
	}
	// A call's parenthesis hugs its function name; a grouping paren after an
	// operator or a colon does not. `and`/`or`/`if`/`then`/`else` lex as IDENT
	// but are operators, so `else (x)` must keep its space.
	if cur.Kind == token.LPAREN && prev.Kind == token.IDENT && !isWordOperator(prev) {
		return false
	}
	// A unary minus hugs its operand: `-x`, `(-1)`, `f(a, -1)`.
	if isUnaryMinus(prev, beforePrev) {
		return false
	}
	return true
}

// isUnaryMinus reports whether tok is a minus in prefix position, i.e. one
// whose left neighbour cannot end an operand.
func isUnaryMinus(tok, before token.Token) bool {
	if tok.Kind != token.MINUS {
		return false
	}
	switch before.Kind {
	case token.INT, token.FLOAT, token.STRING, token.RPAREN:
		return false // binary: the left side is an operand
	case token.IDENT:
		// An identifier ends an operand, unless it is a word operator —
		// `then -1` is a negative literal, `x - 1` is a subtraction.
		return isWordOperator(before)
	}
	return true
}

// isWordOperator reports whether t is one of the expression layer's
// keyword-like operators. The lexer emits them as plain identifiers and
// parser/expr.go gives them meaning by literal, so the formatter has to
// recognize them the same way.
func isWordOperator(t token.Token) bool {
	if t.Kind != token.IDENT {
		return false
	}
	switch t.Literal {
	case "and", "or", "if", "then", "else",
		// v0.5: `ikke x`, and `consider n as v in body`. Without these the
		// call-hugging rule would read `ikke (x > 5)` as a function call and
		// emit `ikke(x > 5)`.
		"ikke", "consider", "as", "in":
		return true
	}
	return false
}

// splitComment splits a line into its code text and its trailing comment
// (including the whitespace run before the `#`). A `#` inside a string literal
// is not a comment, so the scan tracks string state and its escapes — that is
// what keeps `Using: (c) -> c = "#"` intact.
func splitComment(line string) (code, comment string) {
	inString := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case inString && c == '\\':
			i++ // skip the escaped byte
		case inString && c == '"':
			inString = false
		case inString:
		case c == '"':
			inString = true
		case c == '#':
			j := i
			for j > 0 && (line[j-1] == ' ' || line[j-1] == '\t') {
				j--
			}
			return line[:j], line[j:]
		}
	}
	return line, ""
}
