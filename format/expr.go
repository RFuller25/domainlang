package format

import (
	"strconv"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Rendering an expression back to Domain source.
//
// Format itself never uses this — it is line-oriented precisely so that
// comments survive (see the package comment). This is for the tools that hold
// an expression with no source text behind it: the visualizer's breakdown pane
// names every subexpression it evaluated, and the optimizer rewrites bodies
// into trees that were never written down at all.
//
// The output is canonical rather than original: the same spacing rules
// renderTokens applies, and parentheses only where precedence needs them, so
// `(a + b) * c` keeps its parentheses and `a + (b * c)` loses them.

// Expr renders an expression as Domain source.
func Expr(e ast.Expr) string { return expr(e, 0) }

// Binding powers, mirroring parser/expr.go. `if` and `consider` extend as far
// right as they can, so they sit below every operator and are parenthesized
// wherever an operator would otherwise capture their tail.
const (
	bpLowest = 0
	bpAlso   = 1 // e also c1, c2   (looser than everything, and the commas mean
	//                                 it is parenthesized wherever it is not
	//                                 the whole of what is being written)
	bpSpread  = 2 // if … then … else …, consider … as … in …
	bpAssign  = 3 // x := e
	bpOr      = 4
	bpAnd     = 6
	bpNot     = 8
	bpCompare = 10
	bpSum     = 20
	bpProduct = 30
	bpUnary   = 40
	bpAtom    = 50 // literals, names, calls, field access
)

// expr renders e, parenthesizing it when its own binding power is looser than
// the position it is being written into.
func expr(e ast.Expr, min int) string {
	s, bp := render(e)
	if bp < min {
		return "(" + s + ")"
	}
	return s
}

func render(e ast.Expr) (string, int) {
	switch x := e.(type) {
	case *ast.IntLit:
		return strconv.FormatInt(x.Value, 10), bpAtom
	case *ast.FloatLit:
		return ir.FormatFloat(x.Value), bpAtom
	case *ast.BoolLit:
		// Domain has no boolean literals to write, but the optimizer's constant
		// folder produces them, so they have to render as something. These are
		// the names the language uses for the values everywhere else.
		if x.Value {
			return "true", bpAtom
		}
		return "false", bpAtom
	case *ast.StringLit:
		return strconv.Quote(x.Value), bpAtom
	case *ast.Ident:
		return x.Name, bpAtom
	case *ast.FieldAccess:
		return expr(x.Target, bpAtom) + "." + x.Field, bpAtom
	case *ast.CallExpr:
		if s, ok := renderBraced(x); ok {
			return s, bpAtom
		}
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			// One power above `also`, because an argument list is the one
			// place its clause commas cannot be told from the argument commas
			// — the parser refuses a bare one there, so this never writes one.
			args[i] = expr(a, bpAlso+1)
		}
		return expr(x.Fn, bpAtom) + "(" + strings.Join(args, ", ") + ")", bpAtom
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return "ikke " + expr(x.X, bpNot), bpNot
		}
		return "-" + expr(x.X, bpUnary), bpUnary
	case *ast.BinaryExpr:
		bp := binaryBP(x.Op)
		// Left-associative: the right operand needs parentheses at equal
		// binding power, the left one does not.
		return expr(x.Left, bp) + " " + opText(x.Op) + " " + expr(x.Right, bp+1), bp
	case *ast.CondExpr:
		return "if " + expr(x.Cond, bpSpread) + " then " + expr(x.Then, bpSpread) +
			" else " + expr(x.Else, bpSpread), bpSpread
	case *ast.LetExpr:
		return "consider " + x.Name + " as " + expr(x.Value, bpSpread) +
			" in " + expr(x.Body, bpSpread), bpSpread
	case *ast.AssignExpr:
		// The value is written at the spread power rather than at `:=`'s own:
		// `:=` is right-associative and looser than every operator, so an `if`,
		// a `consider` and a further `:=` all read correctly unparenthesized
		// after it. Only an `also` list has to be bracketed.
		return x.Name + " := " + expr(x.Value, bpSpread), bpAssign
	case *ast.AlsoExpr:
		// Everything is written one power above `also` itself, which is what
		// puts the parentheses around a nested one: `(a also b) also c` is the
		// only reading its own parser accepts, so it is the only one written.
		clauses := make([]string, len(x.Clauses))
		for i, c := range x.Clauses {
			clauses[i] = expr(c, bpAlso+1)
		}
		return expr(x.Body, bpAlso+1) + " also " + strings.Join(clauses, ", "), bpAlso
	case *ast.BlockBody:
		// A body is a sub-pipeline, not an expression; there is no expression
		// source to give back, so it says what it is.
		return "(an indented pipeline body)", bpAtom
	}
	return "(unrenderable expression)", bpAtom
}

// renderBraced writes a record literal back as `{a: 1}` when the call is one
// the parser desugared from that syntax, reporting false when it is not.
//
// The check is deliberately structural rather than trusting the flag alone.
// Braced is presentation only, so no pass is obliged to maintain it across a
// rebuild; a call that carries it but no longer has the name/value alternation
// the syntax requires — an optimizer-folded field name, say — must fall back to
// `record(...)` rather than emit source that will not parse. Formatting is the
// one operation that has to give back something valid whatever it is handed.
func renderBraced(x *ast.CallExpr) (string, bool) {
	if !x.Braced || len(x.Args) == 0 || len(x.Args)%2 != 0 {
		return "", false
	}
	if id, ok := x.Fn.(*ast.Ident); !ok || id.Name != "record" {
		return "", false
	}
	fields := make([]string, 0, len(x.Args)/2)
	for i := 0; i < len(x.Args); i += 2 {
		name, ok := x.Args[i].(*ast.StringLit)
		if !ok || !isFieldName(name.Value) {
			return "", false
		}
		fields = append(fields, name.Value+": "+expr(x.Args[i+1], bpAlso+1))
	}
	return "{" + strings.Join(fields, ", ") + "}", true
}

// isFieldName reports whether a field name is one the literal syntax can write
// — an identifier the lexer would give back whole. A record built by a written
// record("has space", 1) call is perfectly legal and has no braced spelling, so
// it keeps the call form.
func isFieldName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func binaryBP(op token.Kind) int {
	switch op {
	case token.OR:
		return bpOr
	case token.AND:
		return bpAnd
	case token.STAR, token.SLASH, token.PERCENT:
		return bpProduct
	case token.PLUS, token.MINUS:
		return bpSum
	}
	return bpCompare // = < > <= >=
}

func opText(op token.Kind) string {
	switch op {
	case token.PLUS:
		return "+"
	case token.MINUS:
		return "-"
	case token.STAR:
		return "*"
	case token.SLASH:
		return "/"
	case token.PERCENT:
		return "%"
	case token.EQ:
		return "="
	case token.LT:
		return "<"
	case token.GT:
		return ">"
	case token.LE:
		return "<="
	case token.GE:
		return ">="
	case token.AND:
		return "and"
	case token.OR:
		return "or"
	}
	return op.String()
}
