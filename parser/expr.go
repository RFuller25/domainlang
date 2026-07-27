package parser

import (
	"domain/ast"
	"domain/token"
)

// Binding powers for the Pratt expression parser. Higher binds tighter.
const (
	bpNone    = 0
	bpOr      = 4  // or
	bpAnd     = 6  // and
	bpNot     = 8  // ikke x   (tighter than `and`, looser than a comparison,
	//                          so `ikke a = b` reads as `ikke (a = b)`)
	bpCompare = 10 // = < > <= >=
	bpSum     = 20 // + -
	bpProduct = 30 // * / %
	bpUnary   = 40 // -x
	bpCall    = 50 // f(...) and x.field
)

// infixOp inspects the current token as a possible infix operator, returning
// the operator kind and its binding power. It rewrites IDENT "and"/"or" into
// the AND/OR kinds so logical connectives need no lexer changes.
func (p *parser) infixOp() (token.Kind, int) {
	t := p.cur()
	switch t.Kind {
	case token.EQ, token.LT, token.GT, token.LE, token.GE:
		return t.Kind, bpCompare
	case token.PLUS, token.MINUS:
		return t.Kind, bpSum
	case token.STAR, token.SLASH, token.PERCENT:
		return t.Kind, bpProduct
	case token.IDENT:
		switch t.Literal {
		case "and":
			return token.AND, bpAnd
		case "or":
			return token.OR, bpOr
		}
	}
	return token.ILLEGAL, bpNone
}

// parseExpr is a precedence-climbing parser for the plain expression layer.
func (p *parser) parseExpr(minBP int) (ast.Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		opKind, bp := p.infixOp()
		if bp == bpNone || bp < minBP {
			break
		}
		opPos := p.cur().Pos
		p.advance()                       // consume the operator (or the and/or IDENT)
		right, err := p.parseExpr(bp + 1) // left-associative
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{Op: opKind, Left: left, Right: right, Pos: opPos}
	}
	return left, nil
}

func (p *parser) parseUnary() (ast.Expr, error) {
	// `ikke` is prefix negation. Like `and`/`or` it arrives as an IDENT and is
	// rewritten here, so it stays a legal identifier anywhere else. Its operand
	// is parsed at bpNot, which pulls in a whole comparison (`ikke a = b`) but
	// stops at `and`, so `ikke a and b` is `(ikke a) and b`.
	if t := p.cur(); t.Kind == token.IDENT && t.Literal == "ikke" {
		opTok := p.advance()
		x, err := p.parseExpr(bpNot)
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: token.NOT, X: x, Pos: opTok.Pos}, nil
	}
	if p.cur().Kind == token.MINUS {
		opTok := p.advance()
		// A MINUS immediately followed by INT is folded into a single signed
		// IntLit here, rather than UnaryExpr{MINUS, IntLit}: math.MinInt64 has
		// no positive int64 representation, so an intermediate unsigned IntLit
		// could never hold its magnitude (see parseNegInt).
		if p.cur().Kind == token.INT {
			intTok := p.cur()
			n, err := parseNegInt(intTok)
			if err != nil {
				return nil, err
			}
			p.advance()
			return &ast.IntLit{Value: n, Pos: opTok.Pos}, nil
		}
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: opTok.Kind, X: x, Pos: opTok.Pos}, nil
	}
	return p.parsePostfix()
}

// parsePostfix handles field access (x.field) and calls (f(args)).
func (p *parser) parsePostfix() (ast.Expr, error) {
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.cur().Kind {
		case token.DOT:
			dot := p.advance()
			field, err := p.expect(token.IDENT)
			if err != nil {
				return nil, err
			}
			expr = &ast.FieldAccess{Target: expr, Field: field.Literal, Pos: dot.Pos}
		case token.LPAREN:
			lp := p.advance()
			var args []ast.Expr
			if p.cur().Kind != token.RPAREN {
				for {
					a, err := p.parseExpr(0)
					if err != nil {
						return nil, err
					}
					args = append(args, a)
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
			expr = &ast.CallExpr{Fn: expr, Args: args, Pos: lp.Pos}
		default:
			return expr, nil
		}
	}
}

func (p *parser) parsePrimary() (ast.Expr, error) {
	t := p.cur()
	switch t.Kind {
	case token.INT:
		n, err := parseInt(t)
		if err != nil {
			return nil, err
		}
		p.advance()
		return &ast.IntLit{Value: n, Pos: t.Pos}, nil
	case token.FLOAT:
		f, err := parseFloat(t)
		if err != nil {
			return nil, err
		}
		p.advance()
		return &ast.FloatLit{Value: f, Pos: t.Pos}, nil
	case token.STRING:
		p.advance()
		return &ast.StringLit{Value: t.Literal, Pos: t.Pos}, nil
	case token.IDENT:
		if t.Literal == "if" {
			return p.parseCond()
		}
		if t.Literal == "consider" {
			return p.parseLet()
		}
		p.advance()
		return &ast.Ident{Name: t.Literal, Pos: t.Pos}, nil
	case token.LPAREN:
		p.advance()
		inner, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(token.RPAREN); err != nil {
			return nil, err
		}
		return inner, nil
	default:
		return nil, p.errf("unexpected %s in expression", t)
	}
}

// parseCond parses `if cond then a else b`. The keywords are contextual
// (plain IDENTs), so no lexer change is needed; the arms extend as far right
// as possible (`if c then a else b + 1` puts `b + 1` in the else arm).
func (p *parser) parseCond() (ast.Expr, error) {
	ifTok := p.advance() // the "if" IDENT
	cond, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expectWord("then"); err != nil {
		return nil, err
	}
	thenE, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expectWord("else"); err != nil {
		return nil, err
	}
	elseE, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.CondExpr{Cond: cond, Then: thenE, Else: elseE, Pos: ifTok.Pos}, nil
}

// parseLet parses `consider NAME as VALUE in BODY`. Like `if`, the keywords
// are contextual IDENTs, so `consider` stays usable as an ordinary name. The
// body extends as far right as possible, and bindings nest:
// `consider a as 1 in consider b as 2 in a + b`.
func (p *parser) parseLet() (ast.Expr, error) {
	letTok := p.advance() // the "consider" IDENT
	nameTok := p.cur()
	if nameTok.Kind != token.IDENT {
		return nil, p.errf("expected a name after \"consider\", got %s", nameTok)
	}
	// Reject the keywords outright: `consider as ...` is a missing name, and
	// saying so beats a confusing failure further along.
	switch nameTok.Literal {
	case "as", "in", "if", "then", "else", "consider":
		return nil, p.errf("expected a name after \"consider\", got the keyword %q", nameTok.Literal)
	}
	p.advance()
	if err := p.expectWordIn("as", "consider binding"); err != nil {
		return nil, err
	}
	value, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expectWordIn("in", "consider binding"); err != nil {
		return nil, err
	}
	body, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.LetExpr{Name: nameTok.Literal, Value: value, Body: body, Pos: letTok.Pos}, nil
}

// expectWord consumes an IDENT with the exact literal, or errors.
func (p *parser) expectWord(word string) error {
	return p.expectWordIn(word, "conditional expression")
}

func (p *parser) expectWordIn(word, ctx string) error {
	t := p.cur()
	if t.Kind != token.IDENT || t.Literal != word {
		return p.errf("expected %q in %s, got %s", word, ctx, t)
	}
	p.advance()
	return nil
}
