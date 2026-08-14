package parser

import (
	"domain/ast"
	"domain/token"
)

// The type grammar, used by Shikigami parameter declarations and declared
// signatures:
//
//	type    := named | tuple | record | lambda
//	named   := IDENT ( '<' type (',' type)* '>' )?
//	tuple   := '(' type (',' type)* ')'          -- one element is grouping
//	record  := '{' IDENT ':' type (',' IDENT ':' type)* '}'
//	lambda  := '(' [ type (',' type)* ] ')' '->' type
//
// `<` and `>` are ordinary comparison tokens, so generics need no lexer change.
//
// Records used to be absent here because the lexer had no brace tokens, so a
// Shikigami operating on Match Pattern records simply declared no signature.
// The record-literal syntax brought the braces, and the written form is the one
// ir.Type.String() has always printed, so the two now agree.

// parseTypeExpr parses one type. allowLambda controls whether a parenthesized
// group followed by `->` is read as a lambda type. It is false on the left of a
// signature's arrow, where that arrow belongs to the signature itself: in
// `: (Int, Int) -> Int` the input is the tuple, not a lambda.
func (p *parser) parseTypeExpr(allowLambda bool) (*ast.TypeExpr, error) {
	// `List<List<List<…>>>` nests like an expression does, and lowerTypeExpr
	// and TypeString walk it just as recursively (see maxNestDepth).
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()

	pos := p.cur().Pos

	if p.cur().Kind == token.LPAREN {
		p.advance() // (
		var elems []*ast.TypeExpr
		if p.cur().Kind != token.RPAREN {
			for {
				el, err := p.parseTypeExpr(true)
				if err != nil {
					return nil, err
				}
				elems = append(elems, el)
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
		// `(A, B) -> C` is a lambda type; `(A, B)` alone is a tuple; `(A)` alone
		// is just a grouped type.
		if allowLambda && p.cur().Kind == token.ARROW {
			p.advance() // ->
			result, err := p.parseTypeExpr(true)
			if err != nil {
				return nil, err
			}
			return &ast.TypeExpr{
				Lambda: &ast.LambdaType{Params: elems, Result: result},
				Pos:    pos,
			}, nil
		}
		switch len(elems) {
		case 0:
			return nil, p.errf("empty type ()")
		case 1:
			return elems[0], nil
		default:
			return &ast.TypeExpr{Tuple: elems, Pos: pos}, nil
		}
	}

	if p.cur().Kind == token.LBRACE {
		return p.parseRecordType(pos)
	}

	name, err := p.expect(token.IDENT)
	if err != nil {
		return nil, err
	}
	te := &ast.TypeExpr{Name: name.Literal, Pos: pos}
	if p.cur().Kind != token.LT {
		return te, nil
	}
	p.advance() // <
	for {
		arg, err := p.parseTypeExpr(true)
		if err != nil {
			return nil, err
		}
		te.Args = append(te.Args, arg)
		if p.cur().Kind == token.COMMA {
			p.advance()
			continue
		}
		break
	}
	if _, err := p.expect(token.GT); err != nil {
		return nil, err
	}
	return te, nil
}

// parseRecordType parses `{a: Int, b: Text}`. The caller has already taken a
// depth level and confirmed the `{`.
//
// It mirrors the value literal's grammar deliberately: the same field-name
// rule, the same duplicate check, the same refusal of an empty one, so the type
// and the value are written the same way.
func (p *parser) parseRecordType(pos token.Position) (*ast.TypeExpr, error) {
	open := p.advance() // {
	if p.cur().Kind == token.RBRACE {
		return nil, p.errf("an empty record type has no fields; write at least one, e.g. {n: Int}")
	}

	te := &ast.TypeExpr{Pos: pos}
	seen := map[string]token.Position{}
	for {
		name := p.cur()
		if name.Kind != token.IDENT {
			return nil, p.errf("a record type's field needs a name: {name: Type}")
		}
		p.advance()
		if _, err := p.expect(token.COLON); err != nil {
			return nil, p.errf("record type field %q needs a colon before its type: {%s: Int}",
				name.Literal, name.Literal)
		}
		if at, dup := seen[name.Literal]; dup {
			return nil, p.errf("record type has a duplicate field %q; it was already given at %s",
				name.Literal, at)
		}
		seen[name.Literal] = name.Pos

		ft, err := p.parseTypeExpr(true)
		if err != nil {
			return nil, err
		}
		te.Fields = append(te.Fields, ast.TypeField{Name: name.Literal, Type: ft, Pos: name.Pos})

		if p.cur().Kind == token.COMMA {
			p.advance()
			if p.cur().Kind == token.RBRACE {
				return nil, p.errf("a record type has no trailing comma; remove it or add another field")
			}
			continue
		}
		break
	}
	if p.cur().Kind != token.RBRACE {
		return nil, p.errf("expected , or } in the record type opened at %s, got %s", open.Pos, p.cur())
	}
	p.advance() // }
	return te, nil
}

// parseSignatureOpt parses a declared `: In -> Out` after a Shikigami's
// parameter list, returning nil when there is none.
func (p *parser) parseSignatureOpt() (*ast.Signature, error) {
	if p.cur().Kind != token.COLON {
		return nil, nil
	}
	pos := p.cur().Pos
	p.advance() // :

	// allowLambda is false here: the next top-level arrow separates the
	// signature's input from its output.
	in, err := p.parseTypeExpr(false)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(token.ARROW); err != nil {
		return nil, p.errf("a Shikigami signature needs both sides, e.g. `: List<Int> -> Int`")
	}
	out, err := p.parseTypeExpr(true)
	if err != nil {
		return nil, err
	}
	return &ast.Signature{In: in, Out: out, Pos: pos}, nil
}
