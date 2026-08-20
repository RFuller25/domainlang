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
	binds := bindLines(prog)
	byLine := tokensByLine(toks)
	openedBy := parenContinuations(toks)
	blocks, rawOf := rawBlocks(src, toks)

	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	pendingBlank := false
	// How far each line that opens a multi-line parenthesis moved, so the lines
	// continuing it can move with it. A line always precedes its continuations,
	// so its shift is known by the time they are reached.
	shifts := map[int]int{}

	for i, raw := range lines {
		lineNo := i + 1

		// A foreign block is another language's source: it is re-indented with
		// its opener and otherwise reproduced byte for byte (see foreign.go).
		// The whole block is emitted at once, on reaching its first line, since
		// a negative shift is decided by the block rather than line by line.
		if bi, ok := rawOf[lineNo]; ok {
			b := blocks[bi]
			if lineNo != b.first {
				continue
			}
			for _, line := range shiftRaw(lines[b.first-1:b.last], shifts[b.opener]) {
				out = appendWithBlank(out, &pendingBlank, line)
			}
			continue
		}

		// A line inside an open parenthesis is not a line the layout knows
		// about: the lexer joined it to the line that opened the parenthesis,
		// so it has no depth of its own and is not a statement to re-render.
		// What its indentation *is* is the author's alignment of a call broken
		// across lines — the one thing a formatter must not flatten — so it is
		// kept as written, shifted by however far its opening line moved.
		if openLine, ok := openedBy[lineNo]; ok {
			out = appendWithBlank(out, &pendingBlank, shiftIndent(raw, shifts[openLine]))
			continue
		}

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
		shifts[lineNo] = d*indentWidth - (len(raw) - len(strings.TrimLeft(raw, " \t")))
		var body string
		switch {
		case binds[lineNo] != nil:
			body = renderBinding(src, byLine[lineNo], code, binds[lineNo])
		case args[lineNo]:
			body = renderTokens(src, byLine[lineNo])
		default:
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

// parenContinuations maps each source line that sits inside an open
// parenthesis to the line that opened it. Those are the lines the lexer joined
// to their opener rather than treating as lines of their own (see package
// lexer), so they carry no INDENT/DEDENT and lineDepths cannot speak for them.
//
// The whole span is mapped, not only the lines holding tokens, so a comment
// written between two arguments of a broken-up call travels with them instead
// of being re-indented to the enclosing block.
func parenContinuations(toks []token.Token) map[int]int {
	cont := map[int]int{}
	depth, openLine := 0, 0
	for _, t := range toks {
		switch t.Kind {
		case token.LPAREN:
			if depth == 0 {
				openLine = t.Pos.Line
			}
			depth++
		case token.RPAREN:
			if depth > 0 {
				depth--
			}
			// Only the outermost parenthesis delimits a span; a nested one is
			// inside a span already marked.
			if depth == 0 {
				for l := openLine + 1; l <= t.Pos.Line; l++ {
					cont[l] = openLine
				}
			}
		}
	}
	return cont
}

// shiftIndent re-indents a line by delta columns, keeping its interior exactly
// as written. A tab in the leading whitespace counts as one column, which is
// also how it is rewritten — Domain indents with spaces, and a line the
// formatter touches should not be the one place a tab survives.
func shiftIndent(line string, delta int) string {
	body := strings.TrimLeft(line, " \t")
	if body == "" {
		return ""
	}
	width := max(len(line)-len(body)+delta, 0)
	return strings.Repeat(" ", width) + strings.TrimRight(body, " \t")
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
		case token.RAW:
			// A foreign block has no Domain layout: its lines are reproduced
			// from source, and letting them claim a depth would also let a
			// comment above the block borrow one (see commentDepth).
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

// bindLines collects the lines holding a `Consider x As/Of …` binding, walking
// the program the way argLines does. The binding itself comes along because
// how the line is rendered depends on which form it takes.
func bindLines(prog *ast.Program) map[int]*ast.Binding {
	lines := make(map[int]*ast.Binding)
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			for _, b := range s.Binds {
				lines[b.Pos.Line] = b
				walk(b.Body)
			}
			// A `Cursed Object` / `Cursed Tool` declaration line is *not*
			// registered: renderBinding normalizes a `Consider NAME As|Of`
			// head, and a declaration has no `Consider` to normalize —
			// feeding one through would write the word in. Its `Of` body is
			// still walked, so a `Consider` written inside a declaration's
			// sub-pipeline is normalized like any other.
			for _, d := range s.Decls {
				walk(d.Body)
			}
			walk(s.Block)
		}
	}
	walk(prog.Statements)
	for _, d := range prog.Shikigamis {
		for _, b := range d.Binds {
			lines[b.Pos.Line] = b
			walk(b.Body)
		}
		walk(d.Body)
	}
	return lines
}

// renderBinding normalizes a binding line. `Consider NAME As|Of` is the head
// either way; what follows decides how much of the rest may be touched.
//
// An expression or a lambda is expression-layer text and is re-rendered like
// any argument's value. An operation phrase is not: it is read as a Shikigami
// name and as a file path, exactly as a statement's phrase is, so it is copied
// verbatim for the same reason renderStatement copies one.
func renderBinding(src string, toks []token.Token, code string, b *ast.Binding) string {
	const head = 3 // Consider, NAME, As|Of
	if len(toks) < head {
		return renderTokens(src, toks)
	}
	// Canonical capitalization: the keywords are matched case-insensitively,
	// and every other themed word in a formatted program is spelled this way.
	prep := "As"
	if b.Of {
		prep = "Of"
	}
	canonicalHead := "Consider " + text(src, toks[1]) + " " + prep
	if len(toks) == head {
		return canonicalHead
	}
	if !b.Of || b.Lambda != nil {
		return canonicalHead + " " + renderTokens(src, toks[head:])
	}
	return canonicalHead + " " + verbatimTail(src, toks, code, head)
}

// verbatimTail is the rest of a line from toks[from] onward, copied from
// source. Shared by renderStatement and renderBinding, which both have a
// canonical head and a phrase that must not be rewritten.
func verbatimTail(src string, toks []token.Token, code string, from int) string {
	if from >= len(toks) {
		return ""
	}
	// The line's absolute start is the first token's offset minus the leading
	// whitespace that token sits behind.
	lead := len(code) - len(strings.TrimLeft(code, " \t"))
	lineStart := toks[0].Pos.Offset - lead
	start, end := toks[from].Pos.Offset, lineStart+len(code)
	if start < 0 || start > end || end > len(src) {
		return renderTokens(src, toks[from:]) // defensive
	}
	return strings.TrimRight(src[start:end], " \t")
}

// tokensByLine groups the real (non-layout) tokens by source line.
func tokensByLine(toks []token.Token) map[int][]token.Token {
	byLine := make(map[int][]token.Token)
	for _, t := range toks {
		switch t.Kind {
		case token.INDENT, token.DEDENT, token.NEWLINE, token.EOF, token.RAW:
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
	phrase := verbatimTail(src, toks, code, k+1)
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
		// beforePrev stays the zero token at the start of the run, which is
		// what tells needsSpace a leading `-` is unary.
		var beforePrev token.Token
		if i > 1 {
			beforePrev = toks[i-2]
		}
		if i > 0 && needsSpace(toks[i-1], t, beforePrev) {
			b.WriteByte(' ')
		}
		b.WriteString(text(src, t))
	}
	return b.String()
}

// needsSpace decides whether a space separates prev from cur. beforePrev is
// the token before prev, needed to tell a unary minus from a binary one.
func needsSpace(prev, cur, beforePrev token.Token) bool {
	// Never a space before these.
	switch cur.Kind {
	case token.COLON, token.COMMA, token.RPAREN, token.DOT, token.RBRACE:
		return false
	}
	// Never a space after these. A record literal's braces hug their fields —
	// `{a: 1}`, not `{ a: 1 }` — which is what makes the written form match how
	// ir.FormatValue prints the value back.
	switch prev.Kind {
	case token.LPAREN, token.DOT, token.LBRACE:
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
		"ikke", "consider", "as", "in",
		// `e also c1, c2`. Without this the call-hugging rule below would read
		// `also (x := 1)` as a call and emit `also(x := 1)`.
		"also":
		return true
	}
	return false
}

// splitComment splits a line into its code text and its trailing comment
// (including the whitespace run before the marker). Finding the marker is
// token.CommentStart's job — it knows both spellings and knows that neither
// counts inside a string literal, which is what keeps `Using: (c) -> c = "#"`
// intact.
func splitComment(line string) (code, comment string) {
	i := token.CommentStart(line)
	if i < 0 {
		return line, ""
	}
	j := i
	for j > 0 && (line[j-1] == ' ' || line[j-1] == '\t') {
		j--
	}
	return line[:j], line[j:]
}
