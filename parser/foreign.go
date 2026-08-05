package parser

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/token"
)

// Foreign blocks on the parser side are almost nothing: the lexer has already
// decided where the block ends and captured it as one token.RAW, so all that
// is left is to read the opening line and hang the block on the statement.
//
// The opening line cannot go through parsePhrase, though. A declared signature
// (`Domain Expansion: Python : List<Int> -> Int`) is types, not an operation
// phrase — its `<` and `>` would be collected as comparison operators and its
// arrow dropped entirely — so the line is read here instead, with the shared
// shape rule (ast.ForeignOpener) deciding where the phrase ends and the
// signature begins.

// lineTokens returns the content tokens of the line at the cursor, up to but
// not including its NEWLINE.
func (p *parser) lineTokens() []token.Token {
	end := p.pos
	for end < len(p.toks) &&
		p.toks[end].Kind != token.NEWLINE && p.toks[end].Kind != token.EOF {
		end++
	}
	return p.toks[p.pos:end]
}

// parseForeignStatement parses a foreign block opener and the block beneath it.
// rest is the index within the line's tokens just past the language name, where
// a declared signature would begin.
func (p *parser) parseForeignStatement(startPos token.Position, lang string, rest int) (*ast.Statement, error) {
	line := p.lineTokens()
	stmt := &ast.Statement{Pos: startPos}

	// Everything ahead of the language name is the themed keyword, when one was
	// written at all; ForeignOpener has already checked that it is exactly one.
	if rest > 1 {
		var err error
		if stmt.Keyword, err = p.parseKeyword(); err != nil {
			return nil, err
		}
	}
	langTok, err := p.expect(token.IDENT)
	if err != nil {
		return nil, err
	}
	// The phrase is the language name alone. Keeping an Operation here (rather
	// than leaving Op nil) is what lets every stage that reads a statement's
	// phrase — prims.Infer, the linter's suggestions, the formatter — go on
	// treating this like any other statement.
	stmt.Op = &ast.Operation{
		Words: []string{langTok.Literal},
		Raw:   langTok.Literal,
		Pos:   langTok.Pos,
	}

	sig, err := p.parseSignatureOpt()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(token.NEWLINE); err != nil {
		return nil, err
	}

	canonical, _ := ast.ForeignLanguage(lang)
	if p.cur().Kind != token.RAW {
		return nil, &Error{Pos: startPos, NeedsBlock: true, Msg: fmt.Sprintf(
			"%s must be followed by an indented block of %s code",
			phraseOf(line), canonical)}
	}
	raw := p.advance()
	stmt.Foreign = &ast.ForeignBlock{
		Language: canonical,
		Source:   raw.Literal,
		Sig:      sig,
		Pos:      raw.Pos,
	}
	return stmt, nil
}

// phraseOf reassembles an opening line for an error message, the way its author
// wrote it.
func phraseOf(line []token.Token) string {
	var b strings.Builder
	for i, t := range line {
		if i > 0 && t.Kind != token.COLON {
			b.WriteByte(' ')
		}
		b.WriteString(t.Literal)
	}
	return b.String()
}
