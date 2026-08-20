package parser

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/token"
)

// `Consider NAME As <value>` / `Consider NAME Of <source>` — the pipeline
// layer's local variables (see ast.Binding).
//
// The line is deliberately not an `IDENT:` argument: an argument's name is
// vocabulary (Mode:, Using:, Seed:), and a binding's name is the user's own, so
// giving bindings their own line shape keeps a misspelled argument reportable
// as a misspelled argument instead of quietly becoming a local nobody reads.
//
// `Consider` and `As`/`Of` are contextual words, matched case-insensitively
// like the themed keywords and recognized only in this exact shape, so a
// statement whose phrase happens to open with the word "Consider" still parses
// as the statement it always was.

// bindingLine reports whether the cursor is on a `Consider NAME As|Of …` line,
// returning the preposition ("As" or "Of") canonically spelled.
func (p *parser) bindingLine() (string, bool) {
	if !isWord(p.toks[p.pos], "consider") || p.pos+2 >= len(p.toks) {
		return "", false
	}
	if p.toks[p.pos+1].Kind != token.IDENT {
		return "", false
	}
	switch {
	case isWord(p.toks[p.pos+2], "as"):
		return "As", true
	case isWord(p.toks[p.pos+2], "of"):
		return "Of", true
	}
	return "", false
}

// isWord reports whether t is the identifier word w, case-insensitively.
func isWord(t token.Token, w string) bool {
	return t.Kind == token.IDENT && strings.EqualFold(t.Literal, w)
}

// parseBinding parses one binding line, with prep as returned by bindingLine.
func (p *parser) parseBinding(prep string) (*ast.Binding, error) {
	startPos := p.cur().Pos
	p.advance()            // "Consider"
	nameTok := p.advance() // the bound name
	p.advance()            // "As" / "Of"

	b := &ast.Binding{Name: nameTok.Literal, Of: prep == "Of", Pos: startPos}
	if err := p.checkBoundName(b.Name, "a binding name", nameTok.Pos); err != nil {
		return nil, err
	}

	if b.Of {
		return b, p.parseOfSource(b)
	}
	return b, p.parseAsValue(b)
}

// checkBoundName refuses a name that is also one of the expression layer's own
// keywords, which would read as that keyword everywhere it was used. The
// parser is where "this is not a usable name" belongs, so it is refused here
// rather than at resolve time.
//
// role names what is being declared, so the message reads correctly for both
// callers — a `Consider` binding and a `Cursed Object` global. The position is
// passed in rather than taken from the cursor because both callers have
// already advanced past the preposition by the time they ask, and an error
// about a name should point at the name.
func (p *parser) checkBoundName(name, role string, pos token.Position) error {
	switch strings.ToLower(name) {
	case "as", "of", "in", "if", "then", "else", "consider", "and", "or", "ikke", "also":
		return &Error{Pos: pos, Msg: fmt.Sprintf(
			"%q cannot be used as %s: it is an expression keyword", name, role)}
	}
	return nil
}

// parseAsValue parses the right-hand side of `Consider x As …`: a lambda (a
// function binding) or an expression. Neither can see the pipeline value, so
// there is nothing else it could be.
//
// The value may continue on indented lines beneath, exactly like an argument's
// (joinArgContinuation), which is what makes a long `consider … in if … then …
// else …` writable here too.
func (p *parser) parseAsValue(b *ast.Binding) error {
	p.joinArgContinuation()
	if p.lambdaAhead() {
		lam, err := p.parseLambda()
		if err != nil {
			return err
		}
		b.Lambda = lam
	} else {
		e, err := p.parseTopExpr()
		if err != nil {
			return err
		}
		b.Value = e
	}
	_, err := p.expect(token.NEWLINE)
	return err
}

// parseOfSource parses the right-hand side of `Consider x Of …`: a lambda
// applied to the current pipeline value, a single operation phrase, or — when
// the line ends after `Of` — an indented sub-pipeline.
//
// A bare expression is deliberately not accepted. `Of Sum` would otherwise be
// ambiguous between the primitive Sum and an identifier named sum, and the
// primitive is the reading worth having; a computation over the current value
// is written as the lambda it is.
func (p *parser) parseOfSource(b *ast.Binding) error {
	if p.lambdaAhead() {
		lam, err := p.parseLambda()
		if err != nil {
			return err
		}
		b.Lambda = lam
		_, err = p.expect(token.NEWLINE)
		return err
	}

	// `Consider x Of` alone: the source is the indented pipeline beneath.
	if p.cur().Kind == token.NEWLINE {
		p.advance()
		if p.cur().Kind != token.INDENT {
			return p.errBlockf("`Consider %s Of` must be followed by an operation or an indented sub-pipeline", b.Name)
		}
		body, binds, err := p.parseBody()
		if err != nil {
			return err
		}
		if len(binds) > 0 {
			return &Error{Pos: binds[0].Pos, Msg: "a binding cannot be written directly inside another binding's sub-pipeline; write it under a statement there"}
		}
		b.Body = body
		return nil
	}

	// `Of Itself` is the value entering the scope, unchanged. Without it,
	// naming the current value took an identity Apply —
	// `Of Apply` + `Using: (l) -> l` — which is pure ceremony, and the one a
	// pipeline body needs to name its own element.
	if p.cur().Kind == token.IDENT && strings.EqualFold(p.cur().Literal, "itself") {
		p.advance()
		b.Identity = true
		_, err := p.expect(token.NEWLINE)
		return err
	}

	// Anything else on the line is an operation, keyworded or not — and it may
	// carry its own indented arguments, so a whole `Sort By` + `Using:` is one
	// binding source.
	stmt, err := p.parseStatement()
	if err != nil {
		return err
	}
	b.Body = []*ast.Statement{stmt}
	return nil
}

// lambdaAhead reports whether the cursor is on `(params) ->` rather than on a
// parenthesized expression. parseArgValue can assume a leading `(` opens a
// lambda because an argument value has no other parenthesized form; a binding's
// value does, so the two are told apart by looking for the arrow that closes
// the parameter list.
func (p *parser) lambdaAhead() bool {
	if p.cur().Kind != token.LPAREN {
		return false
	}
	depth := 0
	for i := p.pos; i < len(p.toks); i++ {
		switch p.toks[i].Kind {
		case token.LPAREN:
			depth++
		case token.RPAREN:
			depth--
			if depth == 0 {
				return i+1 < len(p.toks) && p.toks[i+1].Kind == token.ARROW
			}
		case token.NEWLINE, token.EOF:
			return false
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Global declarations: `Cursed Object` and `Cursed Tool`
// ---------------------------------------------------------------------------
//
// A declaration line is a binding line without the `Consider` word:
//
//	Cursed Object: matches As 0
//
//	Cursed Object:
//	    a As numa
//	    b As numb
//
// The prepositions carry exactly what they carry on `Consider` — `As` never
// sees the pipeline value and `Of` always does — so the right-hand side
// parsers are shared outright rather than reimplemented.
//
// Dropping the `Consider` word is safe *only* because the block form is
// entered from the keyword, not from lookahead: parseStatement routes a
// `Cursed Object:` / `Cursed Tool:` statement here, and nothing else in the
// language reads a bare `NAME As …` line. A prefix-free lookahead rule would
// have been a different and much worse thing, since `Sort As Text` is a
// perfectly good operation phrase.

// declLine reports whether the cursor is on a `NAME As|Of …` declaration line,
// returning the preposition canonically spelled.
func (p *parser) declLine() (string, bool) {
	if p.cur().Kind != token.IDENT || p.pos+1 >= len(p.toks) {
		return "", false
	}
	switch {
	case isWord(p.toks[p.pos+1], "as"):
		return "As", true
	case isWord(p.toks[p.pos+1], "of"):
		return "Of", true
	}
	return "", false
}

// parseDecl parses one `NAME As <value>` / `NAME Of <source>` declaration
// line. keyword names the statement it was written under, so the error says
// which of the two forms the writer was reaching for.
func (p *parser) parseDecl(keyword string) (*ast.Binding, error) {
	prep, ok := p.declLine()
	if !ok {
		return nil, p.errf("%s takes `NAME As <expression>` or `NAME Of <lambda>`, got %s",
			keyword, p.cur())
	}
	startPos := p.cur().Pos
	nameTok := p.advance() // the declared name
	p.advance()            // "As" / "Of"

	b := &ast.Binding{Name: nameTok.Literal, Of: prep == "Of", Pos: startPos}
	if err := p.checkBoundName(b.Name, "a global name", nameTok.Pos); err != nil {
		return nil, err
	}
	if b.Of {
		return b, p.parseOfSource(b)
	}
	return b, p.parseAsValue(b)
}

// parseDeclBlock parses the indented run of declaration lines beneath a bare
// `Cursed Object:` / `Cursed Tool:`.
func (p *parser) parseDeclBlock(stmt *ast.Statement) error {
	if err := p.enter(); err != nil {
		return err
	}
	defer p.leave()

	if _, err := p.expect(token.INDENT); err != nil {
		return err
	}
	for p.cur().Kind != token.DEDENT && p.cur().Kind != token.EOF {
		p.skipNewlines()
		if p.cur().Kind == token.DEDENT || p.cur().Kind == token.EOF {
			break
		}
		d, err := p.parseDecl(stmt.Keyword)
		if err != nil {
			return err
		}
		stmt.Decls = append(stmt.Decls, d)
	}
	if _, err := p.expect(token.DEDENT); err != nil {
		return err
	}
	if len(stmt.Decls) == 0 {
		return p.errBlockf("%s needs at least one declaration", stmt.Keyword)
	}
	return nil
}
