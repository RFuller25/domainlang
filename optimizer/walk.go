package optimizer

import (
	"domain/ast"
	"domain/ir"
	"domain/token"
)

// This file holds the shared AST/IR walking utilities the passes are built on.

// combinatorialScan is the guard the four hash/divisor scan rewrites share: n
// must be the named combinatorial primitive at the expected arity, over
// List<Int>, in a Mode whose result the substituted algorithm can reproduce,
// with a Using: lambda to match against. It returns that mode and lambda.
//
// The arity is read as a literal, so a measured argument stands the rewrite
// down (see hasMeasuredArg): the substituted algorithm is written for one
// specific k, not for whatever the program measures at runtime.
func combinatorialScan(n *ir.Node, prim string, k int) (mode string, lam *ast.Lambda, ok bool) {
	if n.Prim != prim || hasMeasuredArg(n) {
		return "", nil, false
	}
	if got, _ := n.Meta["k"].(int); got != k {
		return "", nil, false
	}
	mode, _ = n.Meta["mode"].(string)
	if mode != "First" && mode != "Count" {
		return "", nil, false
	}
	if n.In == nil || !n.In.Equal(ir.List(ir.Int())) {
		return "", nil, false
	}
	if lam = nodeLambda(n); lam == nil {
		return "", nil, false
	}
	return mode, lam, true
}

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
			// A Consider node's `Of` bindings each hold a sub-pipeline of
			// their own — the operation or body the binding is computed by —
			// and they are node lists like any other. They are kept apart
			// rather than flattened together because they are separate
			// pipelines: concatenating them would put the last node of one
			// beside the first node of the next, and a pass that reads a
			// node's neighbour would see an adjacency that does not exist.
			if subs, _ := n.Meta[ir.MetaBindNodes].([][]*ir.Node); subs != nil {
				lists = append(lists, subs...)
			}
			// A `Cursed Object` / `Cursed Tool` declaration's `Of` body is the
			// same thing under a different keyword, and kept apart for the
			// same reason. Without this a whole sub-pipeline would run
			// unoptimized purely for having been written under a declaration.
			if subs, _ := n.Meta[ir.MetaGlobalNodes].([][]*ir.Node); subs != nil {
				lists = append(lists, subs...)
			}
			if sub := lambdaBodyNodes(n); sub != nil {
				lists = append(lists, sub)
			}
		}
	}
	return lists
}

// lambdaBodyNodes is the resolved sub-pipeline behind a `Using:` written as an
// indented body rather than an expression (ast.BlockBody). Such a body is a
// node list like a Channel's or a loop's and gets the same in-place rewrites —
// the pair scan inside a per-element `Map Each` body is exactly the O(n²)
// request those passes exist for. It hangs off the lambda instead of
// Meta["nodes"], so nodeLists has to look for it separately.
//
// Every Meta value is checked rather than the "lambda" key alone: a primitive
// that starts taking a second lambda gets this for free instead of silently
// hiding a body from the optimizer.
func lambdaBodyNodes(n *ir.Node) []*ir.Node {
	for _, v := range n.Meta {
		// A typed nil satisfies the assertion: primitives with optional lambdas
		// (Explore's Until:, say) store one whether or not it was written.
		lam, ok := v.(*ast.Lambda)
		if !ok || lam == nil {
			continue
		}
		if bb, ok := lam.Body.(*ast.BlockBody); ok {
			if sub := bb.Pipe.BlockNodes(); sub != nil {
				return sub
			}
		}
	}
	return nil
}

// substIdent returns e with every free occurrence of the identifier name
// replaced by repl. Builtin names in call position are left alone. Shared
// subtrees are fine when neither tree updates a binding: repl is inserted by
// reference (not cloned), so a duplicated subtree is evaluated once per
// occurrence, and every pass that substitutes has already refused a body
// carrying a `:=` (see effectful) — where duplicating one would turn one
// write into two.
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
		return &ast.CallExpr{Fn: x.Fn, Args: args, Pos: x.Pos, InPlace: x.InPlace}
	case *ast.CondExpr:
		return &ast.CondExpr{
			Cond: substIdent(x.Cond, name, repl),
			Then: substIdent(x.Then, name, repl),
			Else: substIdent(x.Else, name, repl),
			Pos:  x.Pos,
		}
	case *ast.LetExpr:
		// The bound value is always in the outer scope, so it substitutes.
		// The body does not when the binding shadows the name being replaced —
		// inside it, `name` refers to the local, not to what we are rewriting.
		body := x.Body
		if x.Name != name {
			body = substIdent(body, name, repl)
		}
		return &ast.LetExpr{
			Name:  x.Name,
			Value: substIdent(x.Value, name, repl),
			Body:  body,
			Pos:   x.Pos,
		}
	case *ast.AssignExpr:
		// The target is a binding, never the lambda parameter being
		// substituted (a parameter cannot be written to), so only the value
		// is rewritten.
		return &ast.AssignExpr{Name: x.Name, Value: substIdent(x.Value, name, repl), Pos: x.Pos}
	case *ast.AlsoExpr:
		clauses := make([]ast.Expr, len(x.Clauses))
		for i, c := range x.Clauses {
			clauses[i] = substIdent(c, name, repl)
		}
		return &ast.AlsoExpr{Body: substIdent(x.Body, name, repl), Clauses: clauses, Pos: x.Pos}
	default:
		return e // literals
	}
}

// effectful reports whether a lambda writes to a binding, which is the
// question every rewrite in this package has to ask before it fires.
//
// The passes here are written against a pure expression layer, and they are
// aggressive in exactly the ways a write would notice: fusion turns "all of f,
// then all of g" into "f then g, per element", algorithm substitution replaces
// a scan with one that applies the lambda to different elements a different
// number of times, and constant folding applies a lambda twice to see what it
// does. All of that is sound for a function of its arguments and none of it is
// sound for one that also updates a stage binding, so a lambda that does is
// left exactly as written.
//
// A lambda body written as a sub-pipeline (BlockBody) carries no expression to
// look at and no `:=` to find. That used to make it uninteresting — the
// statements inside it have their own lambdas, checked wherever they are
// reached — and it stopped being true when `Cursed Tool` made a *statement*
// able to write. A global is therefore asked about separately, and the
// BlockBody case is where it earns its keep; see optimizer/globals.go for what
// the question now is and why it is deliberately blunt.
func effectful(l *ast.Lambda) bool {
	if l == nil {
		return false
	}
	return lambdaImpure(l)
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
		if x.Op == token.SLASH || x.Op == token.PERCENT {
			// Safe only when the divisor is a nonzero literal — mod by zero
			// fails exactly like division by zero.
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
		case "length", "take", "drop", "reverse", "concat", "sum", "contains",
			// v0.5 total additions. slice clamps like take/drop, indexof
			// answers -1 rather than failing, and the text transforms cannot
			// fail on any input. mod/divmod/pow/isqrt/factorial/charat/clamp
			// are deliberately absent: each has a documented error case.
			"slice", "indexof", "startswith", "endswith", "replace", "trim",
			"upper", "lower", "chars", "textjoin", "tuple",
			// Graph. Every reader answers empty/false/0 for a node that is not
			// in the graph rather than failing, and every update is a functional
			// copy that cannot reject its input. `weight` is deliberately absent
			// — it errors on a missing arc, which is what `weightor` is for, the
			// same split as get/getor, and so is `root`, which errors unless
			// exactly one node has no incoming arc.
			"graph", "emptygraph", "addnode", "addedge", "deledge",
			"nodes", "edges", "neighbors", "edgesof", "hasedge",
			"weightor", "degree", "weightof", "flipedges", "subgraph",
			"roots", "leaves", "indegree", "delnode", "reachable",
			"hascycle", "undirected", "mergegraphs", "weightsum":
			// total builtins (take/drop/slice clamp)
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
	case *ast.LetExpr:
		return isTotal(x.Value) && isTotal(x.Body)
	case *ast.AssignExpr, *ast.AlsoExpr:
		// Not because either can fail — an update is as total as the value it
		// writes — but because the callers use this answer to decide whether a
		// subexpression may be dropped or left unevaluated, and dropping a
		// write loses the write. Stated rather than left to the default below,
		// since the default's reason is the other one.
		return false
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
