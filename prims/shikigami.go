package prims

import (
	"errors"
	"fmt"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
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

	env, err := bindParams(def, ArgSet{args: stmt.Args}, stmt.Pos)
	if err != nil {
		return nil, nil, err
	}
	body := substituteBody(def.Body, env)

	// A declared signature is checked at the boundary, which is the whole point:
	// the mismatch is reported here, against the call, instead of surfacing as
	// an inlining trace from somewhere inside the body. Using typeErr's exact
	// wording also earns the diagnostics engine's single-step bridge advice
	// ("insert Convert To Integers") for free.
	var wantIn, wantOut *ir.Type
	if def.Sig != nil {
		if wantIn, err = lowerTypeExpr(def.Sig.In, def.Sig.Pos); err != nil {
			return nil, nil, r.sigErr(name, stmt.Pos, err)
		}
		if wantOut, err = lowerTypeExpr(def.Sig.Out, def.Sig.Pos); err != nil {
			return nil, nil, r.sigErr(name, stmt.Pos, err)
		}
		if !wantIn.Equal(cur) {
			return nil, nil, typeErr(stmt.Pos, fmt.Sprintf("Shikigami %q", name), wantIn, cur)
		}
	}

	// Termination without an arbitrary ceiling: a Shikigami may not appear
	// twice in the chain currently being inlined. Non-recursive composition is
	// unbounded — a fixed depth limit refused legal programs for no reason —
	// while genuine self-reference is reported precisely, naming the cycle.
	//
	// Recursion is refused rather than supported on purpose: Shikigami are
	// inlined, so a recursive one has no finite expansion, and the language's
	// answer for a problem that seems to need it is `Domain Expansion:
	// Explore`, which searches a state space iteratively.
	for i, prev := range r.inlining {
		if prev == name {
			return nil, nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
				"Shikigami %q is recursive (%s): a Shikigami is inlined at its "+
					"call site, so it has no finite expansion — use "+
					"`Domain Expansion: Explore` for a search over states",
				name, strings.Join(append(append([]string{}, r.inlining[i:]...), name), " -> "))}
		}
	}
	r.inlining = append(r.inlining, name)
	nodes, out, err := r.resolveSequence(body, cur, scopeNested)
	r.inlining = r.inlining[:len(r.inlining)-1]
	if err != nil {
		return nil, nil, r.wrapShikigamiErr(name, stmt.Pos, err)
	}

	// The body has to deliver what the definition promised. This catches a body
	// that drifted from its stated type — reported once per call, but the fault
	// is in the definition, so the message says where that is.
	if wantOut != nil && !wantOut.Equal(out) {
		return nil, nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
			"Shikigami %q declares it produces %s (%s), but its body produces %s",
			name, wantOut, r.whereDefined(name, def.Pos), out)}
	}

	// Inlined nodes carry positions from the *definition's* body — which may be
	// the prelude or a library file — so nothing in the pipeline otherwise points
	// at the call. Tag the last node with the call site so tooling can say what
	// type the call produced (the language server's inlay hints do).
	if len(nodes) > 0 {
		last := nodes[len(nodes)-1]
		if last.Meta == nil {
			last.Meta = map[string]any{}
		}
		last.Meta["callPos"] = stmt.Pos
	}

	// For the same reason, when the body came from the prelude or a library its
	// positions are coordinates in a file the user is not looking at. Mark every
	// node, so tooling that maps a node back to a source line knows not to point
	// at the program: the visualizer's source pane is the first consumer.
	// Innermost wins, so a prelude Shikigami called from a library stays
	// attributed to the prelude rather than the library that reached it.
	if where := r.foreignSource(name); where != "" {
		for _, n := range nodes {
			if _, already := n.Foreign(); already {
				continue
			}
			if n.Meta == nil {
				n.Meta = map[string]any{}
			}
			n.Meta[ir.MetaForeign] = where
		}
	}
	return nodes, out, nil
}

// foreignSource names the source a definition's positions belong to, or "" when
// they are real coordinates in the user's own file.
func (r *resolver) foreignSource(name string) string {
	switch r.origins[name].Origin {
	case "prelude":
		return "prelude"
	case "import":
		return r.displays[name]
	}
	return ""
}

// sigErr reports a malformed declared signature against the call site, naming
// where the definition lives.
func (r *resolver) sigErr(name string, callPos token.Position, err error) error {
	var re *ResolveError
	if errors.As(err, &re) {
		return &ResolveError{Pos: callPos, Msg: fmt.Sprintf(
			"Shikigami %q has an invalid signature (%s): %s",
			name, r.whereDefined(name, re.Pos), re.Msg)}
	}
	return &ResolveError{Pos: callPos, Msg: fmt.Sprintf("Shikigami %q: %v", name, err)}
}

// whereDefined names the file and position a definition lives at, the same way
// whereInBody does for errors raised inside a body.
func (r *resolver) whereDefined(name string, pos token.Position) string {
	switch r.origins[name].Origin {
	case "prelude":
		return fmt.Sprintf("prelude source %s", pos)
	case "import":
		return fmt.Sprintf("%s:%s", r.displays[name], pos)
	default:
		return fmt.Sprintf("defined at %s", pos)
	}
}

// wrapShikigamiErr builds the inlining trace for an error inside a Shikigami
// body: the outer position is the call site, and the message names the
// definition and where in its body the failure sits. Body positions are real
// coordinates in the user's file for user-defined Shikigami; for prelude and
// imported definitions they point into another source entirely, so they are
// labeled with that source rather than masquerading as user-file positions.
func (r *resolver) wrapShikigamiErr(name string, callPos token.Position, err error) error {
	var re *ResolveError
	if errors.As(err, &re) {
		return &ResolveError{Pos: callPos,
			Msg: fmt.Sprintf("in Shikigami %q (%s): %s", name, r.whereInBody(name, re.Pos), re.Msg)}
	}
	return &ResolveError{Pos: callPos, Msg: fmt.Sprintf("in Shikigami %q: %s", name, err.Error())}
}

// whereInBody names the file a body position belongs to. token.Position carries
// only line and column, so without this an error inside an imported library
// would print coordinates that look like the user's own file.
func (r *resolver) whereInBody(name string, pos token.Position) string {
	switch r.origins[name].Origin {
	case "prelude":
		return fmt.Sprintf("prelude source %s", pos)
	case "import":
		return fmt.Sprintf("%s:%s", r.displays[name], pos)
	default:
		return fmt.Sprintf("body at %s", pos)
	}
}

// paramVal is one bound parameter. Scalars substitute as literals; a lambda
// substitutes as a whole argument value.
type paramVal struct {
	Type   string // "Int", "Text", "Float", "Bool", or "Lambda"
	Int    int64
	Text   string
	Float  float64
	Bool   bool
	Lambda *ast.Lambda
}

// bindParams matches call arguments to declared parameters, checking presence
// and type.
func bindParams(def *ast.ShikigamiDef, args ArgSet, pos token.Position) (map[string]paramVal, error) {
	env := make(map[string]paramVal, len(def.Params))
	for _, p := range def.Params {
		missing := func(kind string) error {
			// A lambda where a scalar was declared is the one confusion worth
			// naming: it is what someone writes reaching for a *measured*
			// argument through a Shikigami. That works — but the parameter has
			// to be declared as the function it is, not as the Int it is not,
			// because a scalar parameter substitutes into the body as a literal
			// (including into lambda bodies) and a function has no literal form.
			if _, isLam := args.Lambda(p.Name); isLam {
				return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Shikigami %q: parameter %q is declared %s but was given a lambda — "+
						"to pass a measured argument through a Shikigami, declare the "+
						"parameter as a lambda type (e.g. %s: (List<Int>) -> Int) and hand "+
						"it to the measured slot in the body",
					def.Name, p.Name, kind, p.Name)}
			}
			return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Shikigami %q requires %s parameter %q", def.Name, kind, p.Name)}
		}
		if p.Type == nil {
			return nil, &ResolveError{Pos: p.Pos,
				Msg: fmt.Sprintf("Shikigami %q: parameter %q has no type", def.Name, p.Name)}
		}

		// A lambda-typed parameter takes a lambda argument, checked against its
		// declared type here — at the call — so the error names the call rather
		// than surfacing from somewhere inside the inlined body.
		if p.Type.Lambda != nil {
			lam, ok := args.Lambda(p.Name)
			if !ok {
				return nil, missing(typeExprString(p.Type))
			}
			if err := checkLambdaParam(def.Name, p, lam, pos); err != nil {
				return nil, err
			}
			env[p.Name] = paramVal{Type: "Lambda", Lambda: lam}
			continue
		}

		switch p.Type.Name {
		case "Int":
			v, ok := args.Int(p.Name)
			if !ok {
				return nil, missing("Int")
			}
			env[p.Name] = paramVal{Type: "Int", Int: v}
		case "Text":
			v, ok := args.Text(p.Name)
			if !ok {
				return nil, missing("Text")
			}
			env[p.Name] = paramVal{Type: "Text", Text: v}
		case "Float":
			v, ok := args.Float(p.Name)
			if !ok {
				return nil, missing("Float")
			}
			env[p.Name] = paramVal{Type: "Float", Float: v}
		case "Bool":
			// Domain has no bool literals in the expression layer, so a Bool
			// argument is written as the bare identifier true or false.
			id, ok := args.Ident(p.Name)
			if !ok {
				return nil, missing("Bool")
			}
			switch strings.ToLower(id) {
			case "true":
				env[p.Name] = paramVal{Type: "Bool", Bool: true}
			case "false":
				env[p.Name] = paramVal{Type: "Bool", Bool: false}
			default:
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Shikigami %q: Bool parameter %q takes true or false, got %q", def.Name, p.Name, id)}
			}
		default:
			return nil, &ResolveError{Pos: p.Pos, Msg: fmt.Sprintf(
				"Shikigami %q: parameter %q cannot have type %s — a parameter is a value written at the call site, so it must be Int, Text, Float, Bool, or a lambda type like (Int) -> Bool",
				def.Name, p.Name, typeExprString(p.Type))}
		}
	}
	return env, nil
}

// checkLambdaParam types a lambda argument against its declared parameter type.
func checkLambdaParam(defName string, p ast.Param, lam *ast.Lambda, pos token.Position) error {
	want, wantResult, err := lowerLambdaType(p.Type.Lambda, p.Pos)
	if err != nil {
		return err
	}
	if len(lam.Params) != len(want) {
		return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Shikigami %q: parameter %q is declared %s, so its lambda takes %d argument(s), got %d",
			defName, p.Name, typeExprString(p.Type), len(want), len(lam.Params))}
	}
	got, err := typecheck.LambdaType(lam, want...)
	if err != nil {
		return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Shikigami %q: parameter %q: %v", defName, p.Name, err)}
	}
	if !got.Equal(wantResult) {
		return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Shikigami %q: parameter %q is declared %s, but its lambda returns %s",
			defName, p.Name, typeExprString(p.Type), got)}
	}
	return nil
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
		// Only Int and Text have a place in an operation phrase (Ints and
		// Strings). Float, Bool and lambda parameters are expression-layer
		// values, so a word naming one is left alone rather than being dropped
		// from the phrase with nowhere to go.
		if pv, ok := env[w]; ok && phraseSubstitutable(pv.Type) &&
			dispatchSurvivesRemoval(keyword, op, i, wantPrim) {
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

// phraseSubstitutable reports whether a parameter kind can appear in an
// operation phrase.
func phraseSubstitutable(kind string) bool { return kind == "Int" || kind == "Text" }

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
	// The body a call resolves is a *copy*, so the copy is what records the
	// primitive's reads. Mark the original here: an argument written in a
	// Shikigami's definition is consumed by every call that inlines it, and
	// without this the unused-argument lint would flag every one of them.
	a.Used = true
	na := *a
	switch v := a.Value.(type) {
	case ast.LambdaArg:
		na.Value = ast.LambdaArg{Lambda: substituteLambda(v.Lambda, env)}
	case ast.IdentArg:
		// `Using: p` where p is a lambda parameter: the whole argument is
		// replaced by the lambda bound at the call site. This is what makes a
		// Shikigami higher-order.
		if pv, ok := env[v.Value]; ok && pv.Type == "Lambda" {
			na.Value = ast.LambdaArg{Lambda: pv.Lambda}
		}
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
			case "Float":
				return &ast.FloatLit{Value: pv.Float, Pos: x.Pos}
			case "Bool":
				return &ast.BoolLit{Value: pv.Bool, Pos: x.Pos}
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
	case *ast.CondExpr:
		// Without this case a conditional was returned untouched, so a
		// parameter used anywhere inside `if/then/else` was never substituted
		// and resolved as an unknown identifier. optimizer/walk.go's substIdent
		// always handled conditionals; this one had not caught up.
		return &ast.CondExpr{
			Cond: substExpr(x.Cond, env, shadowed),
			Then: substExpr(x.Then, env, shadowed),
			Else: substExpr(x.Else, env, shadowed),
			Pos:  x.Pos,
		}
	case *ast.LetExpr:
		// A `consider` binding shadows a Shikigami parameter of the same name
		// for its body, exactly the way a lambda parameter does — so the body
		// is substituted under an extended shadow set. The bound value is
		// still in the outer scope and substitutes normally.
		inner := shadowed
		if !shadowed[x.Name] {
			inner = make(map[string]bool, len(shadowed)+1)
			for k, v := range shadowed {
				inner[k] = v
			}
			inner[x.Name] = true
		}
		return &ast.LetExpr{
			Name:  x.Name,
			Value: substExpr(x.Value, env, shadowed),
			Body:  substExpr(x.Body, env, inner),
			Pos:   x.Pos,
		}
	default:
		return e
	}
}
