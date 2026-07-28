// Package parser turns a token stream into an AST. It implements two cooperating
// parsers: a line/block parser for the themed pipeline layer, and a small Pratt
// parser for the plain expression layer used inside lambdas and vow predicates.
//
// Errors carry source positions, and the parser recovers at top-level
// statement boundaries: a broken statement is reported and skipped, and
// parsing continues on the next top-level line, so one pass surfaces every
// independent mistake (capped, and a single error keeps its plain *Error
// type). A program that produced any error is rejected — recovery improves
// reporting, never acceptance.
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"domain/ast"
	"domain/token"
)

// Error is a parse error with a source position.
type Error struct {
	Pos token.Position
	Msg string
	// NeedsBlock marks the errors that mean "this statement is fine so far,
	// it is just missing its indented block". A file has nowhere to put the
	// missing lines and reports the error as it stands; the REPL reads the
	// flag and waits for them instead (continuation mode). It is a property
	// of the error, not of its wording, so rephrasing a message can never
	// silently change what the REPL does.
	NeedsBlock bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

// ErrorList is two or more parse errors in source order, produced by
// statement-boundary recovery. Parse returns a plain *Error when only one
// error was found, so single-error behavior (and type assertions on it) is
// unchanged.
type ErrorList []*Error

func (l ErrorList) Error() string {
	msgs := make([]string, len(l))
	for i, e := range l {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// maxParseErrors caps recovery so one structural mistake early in a badly
// broken file cannot produce an avalanche of follow-on reports.
const maxParseErrors = 10

// singleWordThemed are the themed keywords that are a single identifier. An
// indented `IDENT:` line is a named argument unless that identifier is one of
// these (multi-word themed keywords like "Cursed Technique" are two idents, so
// they never look like a single `IDENT:` arg). This lets arbitrary argument and
// Shikigami parameter names (Mode, Using, From, k, n, ...) be recognized as
// arguments without a fixed whitelist.
var singleWordThemed = map[string]bool{
	"Reveal":    true,
	"Channel":   true,
	"Shikigami": true,
}

type parser struct {
	src  string
	toks []token.Token
	pos  int
}

// Parse parses a whole program. src is the original source text, used to
// recover exact operation-phrase text.
func Parse(src string, toks []token.Token) (*ast.Program, error) {
	p := &parser{src: src, toks: toks}
	return p.parseProgram()
}

func (p *parser) cur() token.Token  { return p.toks[p.pos] }
func (p *parser) peek() token.Token { return p.toks[min(p.pos+1, len(p.toks)-1)] }

func (p *parser) advance() token.Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) errf(format string, args ...any) error {
	return &Error{Pos: p.cur().Pos, Msg: fmt.Sprintf(format, args...)}
}

// errBlockf is errf for the "…must be followed by an indented body" family —
// the errors an indented block would fix. See Error.NeedsBlock.
func (p *parser) errBlockf(format string, args ...any) error {
	return &Error{Pos: p.cur().Pos, Msg: fmt.Sprintf(format, args...), NeedsBlock: true}
}

func (p *parser) expect(k token.Kind) (token.Token, error) {
	if p.cur().Kind != k {
		return token.Token{}, p.errf("expected %s, got %s", k, p.cur())
	}
	return p.advance(), nil
}

// skipNewlines consumes any stray NEWLINE tokens (e.g. blank logical breaks).
func (p *parser) skipNewlines() {
	for p.cur().Kind == token.NEWLINE {
		p.advance()
	}
}

func (p *parser) parseProgram() (*ast.Program, error) {
	prog := &ast.Program{}
	var errs ErrorList
	p.skipNewlines()
	for p.cur().Kind != token.EOF {
		var err error
		switch {
		case p.cur().Kind == token.DEDENT:
			err = p.errf("unexpected dedent at top level")
			p.advance() // consume it so recovery makes progress
		// A Shikigami definition is `Shikigami "Name" ...`; a call is
		// `Shikigami: Name`. Distinguish by the token after the keyword.
		case p.cur().Kind == token.IDENT && p.cur().Literal == "Shikigami" && p.peek().Kind == token.STRING:
			var def *ast.ShikigamiDef
			if def, err = p.parseShikigamiDef(); err == nil {
				prog.Shikigamis = append(prog.Shikigamis, def)
			}
		default:
			var stmt *ast.Statement
			if stmt, err = p.parseStatement(); err == nil {
				// `Innate Domain: lib` is a declaration, not a pipeline step:
				// hoist it out of the statement list like a Shikigami
				// definition, so its position in the file does not matter.
				if stmt.Keyword == "Innate Domain" {
					imp, ierr := importOf(stmt)
					if ierr != nil {
						err = ierr
					} else {
						prog.Imports = append(prog.Imports, imp)
					}
				} else {
					prog.Statements = append(prog.Statements, stmt)
				}
			}
		}
		if err != nil {
			pe, ok := err.(*Error)
			if !ok {
				pe = &Error{Pos: p.cur().Pos, Msg: err.Error()}
			}
			errs = append(errs, pe)
			if len(errs) >= maxParseErrors {
				errs = append(errs, &Error{Pos: p.cur().Pos,
					Msg: fmt.Sprintf("too many errors (stopped after %d)", maxParseErrors)})
				break
			}
			p.synchronize()
		}
		p.skipNewlines()
	}
	switch len(errs) {
	case 0:
		return prog, nil
	case 1:
		return nil, errs[0]
	default:
		return nil, errs
	}
}

// importOf converts a parsed `Innate Domain:` statement into an Import. The
// target is the phrase's raw source text, so a path with separators
// (`grids/hex`) survives exactly as written.
func importOf(stmt *ast.Statement) (*ast.Import, error) {
	if stmt.Op == nil || strings.TrimSpace(stmt.Op.Raw) == "" {
		return nil, &Error{Pos: stmt.Pos,
			Msg: "Innate Domain needs a library name, e.g. Innate Domain: aoc"}
	}
	if len(stmt.Block) > 0 || len(stmt.Args) > 0 {
		return nil, &Error{Pos: stmt.Pos, Msg: "Innate Domain takes no arguments or block"}
	}
	return &ast.Import{Target: strings.TrimSpace(stmt.Op.Raw), Pos: stmt.Pos}, nil
}

// synchronize skips ahead to the start of the next top-level line after a
// statement failed mid-parse: it consumes tokens until a NEWLINE is passed
// with every INDENT opened *during the skip* balanced, and the following
// token starts a fresh column-0 line (not the failed statement's own block,
// and not a DEDENT still unwinding indentation consumed before the error).
// It always advances at least one token unless already at EOF, so the
// parseProgram loop is guaranteed to make progress.
func (p *parser) synchronize() {
	depth := 0
	atTopLevel := func() bool {
		return depth == 0 &&
			p.cur().Kind != token.INDENT && p.cur().Kind != token.DEDENT && p.cur().Kind != token.NEWLINE
	}
	for p.cur().Kind != token.EOF {
		switch p.advance().Kind {
		case token.INDENT:
			depth++
		case token.DEDENT:
			// A DEDENT closes a block — either one opened during the skip or
			// one consumed before the error (depth already 0).
			if depth > 0 {
				depth--
			}
			if atTopLevel() {
				return
			}
		case token.NEWLINE:
			if atTopLevel() {
				return
			}
		}
	}
}

// parseShikigamiDef parses `Shikigami "Name" (params) : In -> Out` and its
// indented body. Both the parameter list and the signature are optional.
func (p *parser) parseShikigamiDef() (*ast.ShikigamiDef, error) {
	startPos := p.cur().Pos
	p.advance()            // "Shikigami"
	nameTok := p.advance() // STRING
	def := &ast.ShikigamiDef{Name: nameTok.Literal, Pos: startPos}

	params, err := p.parseParamsOpt()
	if err != nil {
		return nil, err
	}
	def.Params = params

	sig, err := p.parseSignatureOpt()
	if err != nil {
		return nil, err
	}
	def.Sig = sig

	if _, err := p.expect(token.NEWLINE); err != nil {
		return nil, err
	}
	if p.cur().Kind != token.INDENT {
		return nil, p.errBlockf("Shikigami %q must be followed by an indented body", nameTok.Literal)
	}
	body, err := p.parseBody()
	if err != nil {
		return nil, err
	}
	def.Body = body
	return def, nil
}

// parseParamsOpt parses an optional `(name: Type, ...)` parameter list.
func (p *parser) parseParamsOpt() ([]ast.Param, error) {
	if p.cur().Kind != token.LPAREN {
		return nil, nil
	}
	p.advance() // (
	var params []ast.Param
	if p.cur().Kind != token.RPAREN {
		for {
			name, err := p.expect(token.IDENT)
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(token.COLON); err != nil {
				return nil, err
			}
			typ, err := p.parseTypeExpr(true)
			if err != nil {
				return nil, err
			}
			params = append(params, ast.Param{Name: name.Literal, Type: typ, Pos: name.Pos})
			if p.cur().Kind == token.COMMA {
				p.advance()
				continue
			}
			break
		}
	}
	if _, err := p.expect(token.RPAREN); err != nil {
		return nil, err
	}
	return params, nil
}

// parseBody parses an indented block of pipeline statements (Shikigami body).
func (p *parser) parseBody() ([]*ast.Statement, error) {
	if _, err := p.expect(token.INDENT); err != nil {
		return nil, err
	}
	var body []*ast.Statement
	for p.cur().Kind != token.DEDENT && p.cur().Kind != token.EOF {
		p.skipNewlines()
		if p.cur().Kind == token.DEDENT || p.cur().Kind == token.EOF {
			break
		}
		s, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		body = append(body, s)
	}
	if _, err := p.expect(token.DEDENT); err != nil {
		return nil, err
	}
	return body, nil
}

// parseStatement parses one keyword line and any indented block beneath it.
func (p *parser) parseStatement() (*ast.Statement, error) {
	startPos := p.cur().Pos

	// Special forms: `Channel "name":` and `Part "1":` open a labelled
	// sub-pipeline block. Both are `IDENT STRING COLON`, which is what
	// distinguishes them from a named argument (`IDENT COLON`).
	if p.cur().Kind == token.IDENT && p.peek().Kind == token.STRING {
		switch p.cur().Literal {
		case "Channel":
			return p.parseChannel(startPos)
		case "Part":
			return p.parsePart(startPos)
		}
	}

	// The themed keyword is optional: a line that does not open with one is a
	// bare operation phrase, and prims.Infer recovers the keyword from it.
	var keyword string
	if p.keywordLine() {
		var err error
		if keyword, err = p.parseKeyword(); err != nil {
			return nil, err
		}
	} else if !startsPhrase(p.cur().Kind) {
		return nil, p.errf("expected a keyword or an operation, got %s", p.cur())
	}

	stmt := &ast.Statement{Keyword: keyword, Pos: startPos}

	// Operation phrase up to NEWLINE (may be empty -> block opener).
	if p.cur().Kind != token.NEWLINE {
		op, err := p.parsePhrase()
		if err != nil {
			return nil, err
		}
		stmt.Op = op
	}
	if _, err := p.expect(token.NEWLINE); err != nil {
		return nil, err
	}

	// Optional indented block: either named args or a nested pipeline.
	if p.cur().Kind == token.INDENT {
		if err := p.parseBlock(stmt); err != nil {
			return nil, err
		}
	}
	return stmt, nil
}

// parseChannel parses `Channel "name":` followed by an indented sub-pipeline.
func (p *parser) parseChannel(startPos token.Position) (*ast.Statement, error) {
	p.advance()            // "Channel"
	nameTok := p.advance() // STRING
	if _, err := p.expect(token.COLON); err != nil {
		return nil, err
	}
	stmt := &ast.Statement{Keyword: "Channel", ChannelName: nameTok.Literal, Pos: startPos}
	if _, err := p.expect(token.NEWLINE); err != nil {
		return nil, err
	}
	if p.cur().Kind != token.INDENT {
		return nil, p.errBlockf("Channel %q must be followed by an indented sub-pipeline", nameTok.Literal)
	}
	if err := p.parseBlock(stmt); err != nil {
		return nil, err
	}
	return stmt, nil
}

// parsePart parses `Part "label":` followed by an indented sub-pipeline. A Part
// branches from the current value like a Channel, but its body's Reveal output
// is labelled instead of being stored under a name — the two-answers-per-input
// shape. Whether a Part is in a legal position is a resolve-time question, so
// the parser accepts one anywhere a statement can appear.
func (p *parser) parsePart(startPos token.Position) (*ast.Statement, error) {
	p.advance()            // "Part"
	nameTok := p.advance() // STRING
	if _, err := p.expect(token.COLON); err != nil {
		return nil, err
	}
	stmt := &ast.Statement{Keyword: "Part", PartName: nameTok.Literal, Pos: startPos}
	if _, err := p.expect(token.NEWLINE); err != nil {
		return nil, err
	}
	if p.cur().Kind != token.INDENT {
		return nil, p.errBlockf("Part %q must be followed by an indented sub-pipeline", nameTok.Literal)
	}
	if err := p.parseBlock(stmt); err != nil {
		return nil, err
	}
	return stmt, nil
}

// startsPhrase reports whether a token can open a keyword-less statement: an
// operation name, or a source target written as a bare path. Paths are why the
// set is wider than IDENT — `16_no_prefixes.input` lexes as an INT first, and
// `./day1.txt` as a DOT. Anything else (an arrow, a bracket, a stray operator)
// cannot begin a statement, and saying so as a syntax error beats letting it
// through to fail later as an unrecognizable operation phrase.
func startsPhrase(k token.Kind) bool {
	switch k {
	case token.IDENT, token.INT, token.DOT, token.SLASH:
		return true
	}
	return false
}

// keywordLine reports whether the statement at the cursor opens with an
// explicit themed keyword, i.e. whether parseKeyword should run at all.
//
// A keyword is written when a colon closes the leading run of identifier words
// (`Cursed Technique: Split Text by "\n"`). It counts as written *also* when
// those words begin with a themed keyword but no colon follows: `Reveal
// stdout` is the classic forgotten-colon mistake, and keeping it on the
// keyword path preserves the precise syntax error (and its auto-fix) instead
// of quietly re-reading the line as the prefix-free phrase "Reveal stdout",
// which names no operation at all.
func (p *parser) keywordLine() bool {
	i := p.pos
	var words []string
	for p.toks[i].Kind == token.IDENT {
		words = append(words, p.toks[i].Literal)
		i++
	}
	if len(words) == 0 {
		return false
	}
	if p.toks[i].Kind == token.COLON {
		return true
	}
	_, _, ok := ast.KeywordPrefix(words)
	return ok
}

// parseKeyword reads one or more IDENT words up to the COLON and joins them.
func (p *parser) parseKeyword() (string, error) {
	if p.cur().Kind != token.IDENT {
		return "", p.errf("expected a keyword, got %s", p.cur())
	}
	var words []string
	for p.cur().Kind == token.IDENT {
		words = append(words, p.advance().Literal)
	}
	if _, err := p.expect(token.COLON); err != nil {
		return "", p.errf("expected ':' after keyword %q, got %s",
			strings.Join(words, " "), p.cur())
	}
	return strings.Join(words, " "), nil
}

// parsePhrase parses an operation phrase: everything from just after the colon
// to the end of the line, split on top-level commas.
func (p *parser) parsePhrase() (*ast.Operation, error) {
	start := p.cur()
	op := &ast.Operation{Pos: start.Pos}

	var endByte int
	segment := 0 // 0 = primary segment, >0 = modifier segments
	var modWords []string

	flushModifier := func() {
		if len(modWords) > 0 {
			op.Modifiers = append(op.Modifiers, strings.Join(modWords, " "))
			modWords = nil
		}
	}

	for p.cur().Kind != token.NEWLINE && p.cur().Kind != token.EOF {
		// A MINUS immediately followed by INT is a negative integer literal,
		// not a bare operator; fold it into a single signed value here since
		// neither branch below otherwise records a sign (unary minus is only
		// implemented in the separate expression-layer Pratt parser).
		if p.cur().Kind == token.MINUS && p.peek().Kind == token.INT {
			p.advance() // MINUS
			intTok := p.advance()
			endByte = intTok.End
			n, err := parseNegInt(intTok)
			if err != nil {
				return nil, err
			}
			op.Ints = append(op.Ints, n)
			if segment > 0 {
				modWords = append(modWords, "-"+intTok.Literal)
			}
			continue
		}
		t := p.advance()
		endByte = t.End
		if t.Kind == token.COMMA {
			flushModifier()
			segment++
			continue
		}
		// The themed phrase layer is integer-only by design (see ast.Operation):
		// there is no Floats field to record a decimal into, so a FLOAT here
		// must be a hard error rather than a silent drop that only surfaces as
		// a confusing resolver message later ("... requires a count").
		if t.Kind == token.FLOAT {
			return nil, &Error{Pos: t.Pos, Msg: fmt.Sprintf(
				"decimal literal %q is not allowed in an operation phrase: phrase arguments are integer-only (decimals are only allowed in expressions and named arguments)",
				t.Literal)}
		}
		if segment == 0 {
			switch t.Kind {
			case token.IDENT:
				op.Words = append(op.Words, t.Literal)
			case token.STRING:
				op.Strings = append(op.Strings, t.Literal)
			case token.INT:
				n, err := parseInt(t)
				if err != nil {
					return nil, err
				}
				op.Ints = append(op.Ints, n)
			case token.EQ, token.LT, token.GT, token.LE, token.GE:
				op.OpSyms = append(op.OpSyms, t.Literal)
			case token.DOT:
				// part of a dotted target like input.txt; ignored structurally
			default:
				// other operators are tolerated in the raw phrase
			}
		} else {
			switch t.Kind {
			case token.IDENT:
				modWords = append(modWords, t.Literal)
			case token.INT:
				n, err := parseInt(t)
				if err != nil {
					return nil, err
				}
				op.Ints = append(op.Ints, n)
				modWords = append(modWords, t.Literal)
			}
		}
	}
	flushModifier()

	op.Raw = strings.TrimSpace(p.src[start.Pos.Offset:endByte])
	return op, nil
}

// parseBlock parses an indented block, deciding per-line whether it is a named
// argument (`Mode:`/`Using:`) or a nested pipeline statement. A block may
// contain both (e.g. `Simple Domain: While` carries a Using: predicate and a
// body sub-pipeline); the resolver validates the combination per keyword.
func (p *parser) parseBlock(stmt *ast.Statement) error {
	if _, err := p.expect(token.INDENT); err != nil {
		return err
	}
	for p.cur().Kind != token.DEDENT && p.cur().Kind != token.EOF {
		p.skipNewlines()
		if p.cur().Kind == token.DEDENT || p.cur().Kind == token.EOF {
			break
		}
		// Look ahead: an `IDENT:` line is a named argument unless the
		// identifier is a single-word themed keyword (Reveal/Channel/Shikigami).
		if p.cur().Kind == token.IDENT && p.peek().Kind == token.COLON &&
			!singleWordThemed[p.cur().Literal] {
			arg, err := p.parseArg()
			if err != nil {
				return err
			}
			stmt.Args = append(stmt.Args, arg)
		} else {
			child, err := p.parseStatement()
			if err != nil {
				return err
			}
			stmt.Block = append(stmt.Block, child)
		}
	}
	if _, err := p.expect(token.DEDENT); err != nil {
		return err
	}
	return nil
}

// parseArg parses one `Name: value` argument line.
func (p *parser) parseArg() (*ast.Arg, error) {
	nameTok := p.advance() // IDENT
	if _, err := p.expect(token.COLON); err != nil {
		return nil, err
	}
	arg := &ast.Arg{Name: nameTok.Literal, Pos: nameTok.Pos}

	val, err := p.parseArgValue()
	if err != nil {
		return nil, err
	}
	arg.Value = val
	if _, err := p.expect(token.NEWLINE); err != nil {
		return nil, err
	}
	return arg, nil
}

func (p *parser) parseArgValue() (ast.ArgValue, error) {
	switch p.cur().Kind {
	case token.STRING:
		return ast.StringArg{Value: p.advance().Literal}, nil
	case token.INT:
		n, err := parseInt(p.cur())
		if err != nil {
			return nil, err
		}
		p.advance()
		return ast.IntArg{Value: n}, nil
	case token.FLOAT:
		f, err := parseFloat(p.cur())
		if err != nil {
			return nil, err
		}
		p.advance()
		return ast.FloatArg{Value: f}, nil
	case token.MINUS:
		switch p.peek().Kind {
		case token.INT:
			p.advance() // MINUS
			n, err := parseNegInt(p.cur())
			if err != nil {
				return nil, err
			}
			p.advance()
			return ast.IntArg{Value: n}, nil
		case token.FLOAT:
			p.advance() // MINUS
			f, err := parseFloat(p.cur())
			if err != nil {
				return nil, err
			}
			p.advance()
			return ast.FloatArg{Value: -f}, nil
		}
		return nil, p.errf("expected a number after '-', got %s", p.peek())
	case token.LPAREN:
		lam, err := p.parseLambda()
		if err != nil {
			return nil, err
		}
		return ast.LambdaArg{Lambda: lam}, nil
	case token.IDENT:
		first := p.advance().Literal
		// A comma after the first identifier makes this an ident list
		// (e.g. From: moves, stacks).
		if p.cur().Kind == token.COMMA {
			values := []string{first}
			for p.cur().Kind == token.COMMA {
				p.advance()
				id, err := p.expect(token.IDENT)
				if err != nil {
					return nil, err
				}
				values = append(values, id.Literal)
			}
			return ast.IdentListArg{Values: values}, nil
		}
		return ast.IdentArg{Value: first}, nil
	default:
		return nil, p.errf("expected an argument value, got %s", p.cur())
	}
}

// parseLambda parses `(a, b) -> expr`.
func (p *parser) parseLambda() (*ast.Lambda, error) {
	startPos := p.cur().Pos
	if _, err := p.expect(token.LPAREN); err != nil {
		return nil, err
	}
	var params []string
	if p.cur().Kind != token.RPAREN {
		seen := map[string]bool{}
		for {
			id, err := p.expect(token.IDENT)
			if err != nil {
				return nil, err
			}
			if seen[id.Literal] {
				return nil, &Error{Pos: id.Pos, Msg: fmt.Sprintf("duplicate lambda parameter %q", id.Literal)}
			}
			seen[id.Literal] = true
			params = append(params, id.Literal)
			if p.cur().Kind == token.COMMA {
				p.advance()
				continue
			}
			break
		}
	}
	if _, err := p.expect(token.RPAREN); err != nil {
		return nil, err
	}
	if _, err := p.expect(token.ARROW); err != nil {
		return nil, err
	}
	body, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.Lambda{Params: params, Body: body, Pos: startPos}, nil
}

func parseInt(t token.Token) (int64, error) {
	n, err := strconv.ParseInt(t.Literal, 10, 64)
	if err != nil {
		return 0, &Error{Pos: t.Pos, Msg: fmt.Sprintf("integer literal %q out of range", t.Literal)}
	}
	return n, nil
}

// parseNegInt parses the INT token t as the magnitude of a negative integer
// literal, i.e. it parses "-"+t.Literal directly via strconv.ParseInt. This
// must not be done by parsing the unsigned magnitude and negating the
// result: math.MinInt64's magnitude ("9223372036854775808") exceeds
// math.MaxInt64, so an unsigned parse of it would spuriously fail even
// though the signed literal itself ("-9223372036854775808") is valid.
func parseNegInt(t token.Token) (int64, error) {
	n, err := strconv.ParseInt("-"+t.Literal, 10, 64)
	if err != nil {
		return 0, &Error{Pos: t.Pos, Msg: fmt.Sprintf("integer literal %q out of range", "-"+t.Literal)}
	}
	return n, nil
}

// parseFloat parses a FLOAT token's literal (digits '.' digits by
// construction in the lexer).
func parseFloat(t token.Token) (float64, error) {
	f, err := strconv.ParseFloat(t.Literal, 64)
	if err != nil {
		return 0, &Error{Pos: t.Pos, Msg: fmt.Sprintf("invalid decimal literal %q", t.Literal)}
	}
	return f, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
