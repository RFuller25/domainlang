package prims

import (
	"errors"
	"fmt"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Shikigami (M7): user-defined, parameterized operations composed from
// primitives. A call inlines the definition's body after substituting the
// call's arguments for the declared parameters, so a Shikigami is exactly "a
// name for a composition of primitives" — no undefined magic.

// resolveShikigamiCall looks up the named Shikigami, binds and substitutes its
// parameters, and resolves the resulting body inline at the current type.
func (r *resolver) resolveShikigamiCall(stmt *ast.Statement, cur *ir.Type) ([]*ir.Node, *ir.Type, error) {
	if stmt.Op == nil {
		return nil, nil, &ResolveError{Pos: stmt.Pos, Msg: "Shikigami call needs a name, e.g. Shikigami: Top K Sum"}
	}
	name := stmt.Op.Raw
	def, ok := r.shikigamis[name]
	if !ok {
		return nil, nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("unknown Shikigami %q", name)}
	}

	env, err := bindParams(def, ArgSet{stmt.Args}, stmt.Pos)
	if err != nil {
		return nil, nil, err
	}
	body := substituteBody(def.Body, env)

	r.depth++
	if r.depth > 64 {
		r.depth--
		return nil, nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("Shikigami %q: inlining too deep (recursive definition?)", name)}
	}
	nodes, out, err := r.resolveSequence(body, cur, false)
	r.depth--
	if err != nil {
		return nil, nil, r.wrapShikigamiErr(name, stmt.Pos, err)
	}
	return nodes, out, nil
}

// wrapShikigamiErr builds the inlining trace for an error inside a Shikigami
// body: the outer position is the call site, and the message names the
// definition and where in its body the failure sits. Body positions are real
// coordinates in the user's file for user-defined Shikigami; for prelude
// definitions they point into the embedded prelude source, so they are
// labeled as such rather than masquerading as user-file positions.
func (r *resolver) wrapShikigamiErr(name string, callPos token.Position, err error) error {
	var re *ResolveError
	if errors.As(err, &re) {
		where := fmt.Sprintf("body at %s", re.Pos)
		if r.preludeNames[name] {
			where = fmt.Sprintf("prelude source %s", re.Pos)
		}
		return &ResolveError{Pos: callPos,
			Msg: fmt.Sprintf("in Shikigami %q (%s): %s", name, where, re.Msg)}
	}
	return &ResolveError{Pos: callPos, Msg: fmt.Sprintf("in Shikigami %q: %s", name, err.Error())}
}

type paramVal struct {
	Type string
	Int  int64
	Text string
}

// bindParams matches call arguments to declared parameters, checking presence
// and type.
func bindParams(def *ast.ShikigamiDef, args ArgSet, pos token.Position) (map[string]paramVal, error) {
	env := make(map[string]paramVal, len(def.Params))
	for _, p := range def.Params {
		switch p.Type {
		case "Int":
			v, ok := args.Int(p.Name)
			if !ok {
				return nil, &ResolveError{Pos: pos,
					Msg: fmt.Sprintf("Shikigami %q requires Int parameter %q", def.Name, p.Name)}
			}
			env[p.Name] = paramVal{Type: "Int", Int: v}
		case "Text":
			v, ok := args.Text(p.Name)
			if !ok {
				return nil, &ResolveError{Pos: pos,
					Msg: fmt.Sprintf("Shikigami %q requires Text parameter %q", def.Name, p.Name)}
			}
			env[p.Name] = paramVal{Type: "Text", Text: v}
		default:
			return nil, &ResolveError{Pos: p.Pos,
				Msg: fmt.Sprintf("Shikigami %q: unsupported parameter type %q (use Int or Text)", def.Name, p.Type)}
		}
	}
	return env, nil
}

// substituteBody returns a copy of the statements with parameters substituted.
func substituteBody(body []*ast.Statement, env map[string]paramVal) []*ast.Statement {
	out := make([]*ast.Statement, len(body))
	for i, s := range body {
		out[i] = substituteStatement(s, env)
	}
	return out
}

func substituteStatement(stmt *ast.Statement, env map[string]paramVal) *ast.Statement {
	ns := *stmt
	if stmt.Op != nil {
		ns.Op = substituteOp(stmt.Keyword, stmt.Op, env)
	}
	if len(stmt.Args) > 0 {
		ns.Args = make([]*ast.Arg, len(stmt.Args))
		for i, a := range stmt.Args {
			ns.Args[i] = substituteArg(a, env)
		}
	}
	if len(stmt.Block) > 0 {
		ns.Block = substituteBody(stmt.Block, env)
	}
	return &ns
}

// substituteOp replaces identifier words that name parameters with the
// parameter's literal value: an Int param moves into Ints, a Text param into
// Strings (e.g. `Select Top k` with k=3 becomes `Select Top` + Ints[3]).
//
// A word is only substituted if doing so leaves primitive dispatch unchanged.
// Without this guard, a parameter name that happens to collide with a
// dispatch keyword for its own statement (e.g. a param named "Matching" on a
// `Count Matching` line) would be silently stripped from the phrase by name
// alone, re-dispatching the statement to an unrelated primitive (`Count`
// instead of `Count Matching`) with no error anywhere in the pipeline.
func substituteOp(keyword string, op *ast.Operation, env map[string]paramVal) *ast.Operation {
	no := *op
	no.Ints = append([]int64(nil), op.Ints...)
	no.Strings = append([]string(nil), op.Strings...)
	no.Modifiers = append([]string(nil), op.Modifiers...)
	no.Words = nil

	wantPrim := findPrimitive(&ast.Statement{Keyword: keyword, Op: op})
	for i, w := range op.Words {
		if pv, ok := env[w]; ok && dispatchSurvivesRemoval(keyword, op, i, wantPrim) {
			switch pv.Type {
			case "Int":
				no.Ints = append(no.Ints, pv.Int)
			case "Text":
				no.Strings = append(no.Strings, pv.Text)
			}
			continue
		}
		no.Words = append(no.Words, w)
	}
	return &no
}

// dispatchSurvivesRemoval reports whether dropping op.Words[idx] still
// resolves to the same primitive as the original phrase (or no primitive at
// all, if the phrase didn't resolve to begin with — nothing to protect).
func dispatchSurvivesRemoval(keyword string, op *ast.Operation, idx int, want *Primitive) bool {
	if want == nil {
		return true
	}
	trial := *op
	trial.Words = make([]string, 0, len(op.Words)-1)
	trial.Words = append(trial.Words, op.Words[:idx]...)
	trial.Words = append(trial.Words, op.Words[idx+1:]...)
	return findPrimitive(&ast.Statement{Keyword: keyword, Op: &trial}) == want
}

func substituteArg(a *ast.Arg, env map[string]paramVal) *ast.Arg {
	na := *a
	if la, ok := a.Value.(ast.LambdaArg); ok {
		na.Value = ast.LambdaArg{Lambda: substituteLambda(la.Lambda, env)}
	}
	return &na
}

func substituteLambda(lam *ast.Lambda, env map[string]paramVal) *ast.Lambda {
	shadowed := make(map[string]bool, len(lam.Params))
	for _, p := range lam.Params {
		shadowed[p] = true
	}
	return &ast.Lambda{Params: lam.Params, Body: substExpr(lam.Body, env, shadowed), Pos: lam.Pos}
}

// substExpr replaces free identifiers that name parameters with the parameter's
// literal, leaving lambda-bound names (shadowed) alone.
func substExpr(e ast.Expr, env map[string]paramVal, shadowed map[string]bool) ast.Expr {
	switch x := e.(type) {
	case *ast.Ident:
		if shadowed[x.Name] {
			return x
		}
		if pv, ok := env[x.Name]; ok {
			switch pv.Type {
			case "Int":
				return &ast.IntLit{Value: pv.Int, Pos: x.Pos}
			case "Text":
				return &ast.StringLit{Value: pv.Text, Pos: x.Pos}
			}
		}
		return x
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: x.Op, X: substExpr(x.X, env, shadowed), Pos: x.Pos}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{Op: x.Op,
			Left:  substExpr(x.Left, env, shadowed),
			Right: substExpr(x.Right, env, shadowed),
			Pos:   x.Pos}
	case *ast.FieldAccess:
		return &ast.FieldAccess{Target: substExpr(x.Target, env, shadowed), Field: x.Field, Pos: x.Pos}
	case *ast.CallExpr:
		args := make([]ast.Expr, len(x.Args))
		for i, a := range x.Args {
			args[i] = substExpr(a, env, shadowed)
		}
		return &ast.CallExpr{Fn: substExpr(x.Fn, env, shadowed), Args: args, Pos: x.Pos}
	default:
		return e
	}
}
