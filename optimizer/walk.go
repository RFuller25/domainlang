package optimizer

import (
	"domain/ast"
	"domain/ir"
	"domain/token"
)

// This file holds the shared AST/IR walking utilities the passes are built on.

// nodeLists returns every node list in the pipeline: the top-level list plus,
// recursively, the sub-pipelines stashed in Meta["nodes"] (Channel bodies,
// Simple Domain loop bodies). Passes that rewrite nodes *in place* (swapping
// Prim/Meta/Eval on an existing *ir.Node) may safely fire on every list;
// passes that change a list's length must restrict themselves to p.Nodes,
// because nested lists are also captured by their parents' Eval closures and
// a re-sliced Meta["nodes"] would diverge from what the interpreter runs.
func nodeLists(p *ir.Pipeline) [][]*ir.Node {
	lists := [][]*ir.Node{p.Nodes}
	for i := 0; i < len(lists); i++ {
		for _, n := range lists[i] {
			if sub, _ := n.Meta["nodes"].([]*ir.Node); sub != nil {
				lists = append(lists, sub)
			}
		}
	}
	return lists
}

// substIdent returns e with every free occurrence of the identifier name
// replaced by repl. Builtin names in call position are left alone. Shared
// subtrees are fine: the expression layer is side-effect free, and repl is
// inserted by reference (not cloned), so later in-place simplification of a
// shared subtree stays sound.
func substIdent(e ast.Expr, name string, repl ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.Ident:
		if x.Name == name {
			return repl
		}
		return x
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: x.Op, X: substIdent(x.X, name, repl), Pos: x.Pos}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Op:   x.Op,
			Left: substIdent(x.Left, name, repl), Right: substIdent(x.Right, name, repl),
			Pos: x.Pos,
		}
	case *ast.FieldAccess:
		return &ast.FieldAccess{Target: substIdent(x.Target, name, repl), Field: x.Field, Pos: x.Pos}
	case *ast.CallExpr:
		args := make([]ast.Expr, len(x.Args))
		for i, a := range x.Args {
			args[i] = substIdent(a, name, repl)
		}
		return &ast.CallExpr{Fn: x.Fn, Args: args, Pos: x.Pos}
	case *ast.CondExpr:
		return &ast.CondExpr{
			Cond: substIdent(x.Cond, name, repl),
			Then: substIdent(x.Then, name, repl),
			Else: substIdent(x.Else, name, repl),
			Pos:  x.Pos,
		}
	default:
		return e // literals
	}
}

// isTotal reports whether evaluating e can never fail at runtime (assuming it
// typechecked, which every Meta lambda did at resolve time). Division can
// fail (÷0), and the partial builtins (item, first, last, min, max, get, at)
// fail on out-of-range/empty/missing inputs. A pass may only *discard* an
// expression when it is total, otherwise the rewrite would swallow an error
// the naive pipeline reports.
func isTotal(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.IntLit, *ast.StringLit, *ast.BoolLit, *ast.Ident:
		return true
	case *ast.UnaryExpr:
		return isTotal(x.X)
	case *ast.BinaryExpr:
		if x.Op == token.SLASH {
			// Safe only when the divisor is a nonzero literal.
			if lit, ok := x.Right.(*ast.IntLit); !ok || lit.Value == 0 {
				return false
			}
		}
		return isTotal(x.Left) && isTotal(x.Right)
	case *ast.FieldAccess:
		// Field existence was proven by typecheck.
		return isTotal(x.Target)
	case *ast.CallExpr:
		id, ok := x.Fn.(*ast.Ident)
		if !ok {
			return false
		}
		switch id.Name {
		case "length", "take", "drop", "reverse", "concat", "sum", "contains":
			// total builtins (take/drop clamp)
		default:
			return false // item, first, last, min, max, get, at are partial
		}
		for _, a := range x.Args {
			if !isTotal(a) {
				return false
			}
		}
		return true
	case *ast.CondExpr:
		return isTotal(x.Cond) && isTotal(x.Then) && isTotal(x.Else)
	default:
		return false
	}
}

// linearForm recognizes bodies of the shape a*x + b over the single parameter
// param, built from +, -, * and integer literals. It returns the coefficients
// (a, b). Division is deliberately excluded, and multiplication is only
// accepted when one side is constant (a*x * b*x would be quadratic).
func linearForm(e ast.Expr, param string) (a, b int64, ok bool) {
	switch x := e.(type) {
	case *ast.IntLit:
		return 0, x.Value, true
	case *ast.Ident:
		if x.Name == param {
			return 1, 0, true
		}
		return 0, 0, false
	case *ast.UnaryExpr:
		if x.Op != token.MINUS {
			return 0, 0, false
		}
		a, b, ok = linearForm(x.X, param)
		return -a, -b, ok
	case *ast.BinaryExpr:
		la, lb, lok := linearForm(x.Left, param)
		ra, rb, rok := linearForm(x.Right, param)
		if !lok || !rok {
			return 0, 0, false
		}
		switch x.Op {
		case token.PLUS:
			return la + ra, lb + rb, true
		case token.MINUS:
			return la - ra, lb - rb, true
		case token.STAR:
			if la == 0 {
				return lb * ra, lb * rb, true
			}
			if ra == 0 {
				return la * rb, lb * rb, true
			}
			return 0, 0, false
		}
		return 0, 0, false
	default:
		return 0, 0, false
	}
}
