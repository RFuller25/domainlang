package parser

import (
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
	// A name that is also one of the expression layer's own keywords would
	// read as that keyword everywhere it was used, so it is refused here
	// rather than at resolve time — the parser is where "this is not a usable
	// name" belongs.
	switch strings.ToLower(b.Name) {
	case "as", "of", "in", "if", "then", "else", "consider", "and", "or", "ikke":
		return nil, p.errf("%q cannot be used as a binding name: it is an expression keyword", b.Name)
	}

	if b.Of {
		return b, p.parseOfSource(b)
	}
	return b, p.parseAsValue(b)
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
		e, err := p.parseExpr(0)
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
