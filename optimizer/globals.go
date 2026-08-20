package optimizer

import (
	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Globals and the rewrites (prims/globals.go).
//
// Every pass in this package asks `effectful` before it fires, because all of
// them are aggressive in ways a write would notice: fusion turns "all of f,
// then all of g" into "f then g, per element", algorithm substitution applies a
// lambda to different elements a different number of times, and constant
// folding applies one twice to see what it does.
//
// Globals widen what "a write" means in two ways the original question did not
// cover:
//
//   - A `Cursed Tool` is a **statement**, not a `:=`. `ast.HasUpdate` looks for
//     an AssignExpr and finds nothing in a lambda whose body is a sub-pipeline
//     containing one — and `effectful`'s doc used to say, correctly at the
//     time, that a BlockBody "carries no `:=` to find: the statements inside it
//     have their own lambdas, which are checked wherever they are reached".
//     That reasoning stopped holding the moment a statement could write.
//   - A write is visible to *other stages*, not only to the next element. A
//     stage that merely reads a global is a pure function of its input only for
//     as long as nothing else writes that global.
//
// Nothing in the current pass set can actually be provoked into a wrong answer
// — a block body cannot be composed into another lambda, and a declaration node
// is an unrecognized primitive that breaks the adjacency fusion needs. But that
// is an accident of which passes exist, not a property anyone stated, and the
// failure mode when it stops holding is a silently wrong answer rather than a
// slow one. So it is stated here.
//
// The rule has two halves, and only the second one is a judgement call:
//
//   - a lambda that **writes** a global is left exactly as written, always.
//   - a lambda that **reads** one is left alone only if that global is
//     *mutable* — something in the program can change it after it is declared.
//     A global declared once at the top level and never written again is a
//     constant of the run, every reader of it sits after that declaration
//     (visibility is forward-only), and so reading it is as pure as reading a
//     literal. Those stages keep every rewrite they would have had.
//
// The second half is what makes the feature worth using: `Cursed Object:
// target As 2020` followed by a stage that reads `target` still gets its
// algorithm substitution, exactly as the same program with the number written
// inline would.
//
// Mutability is decided at resolve time and rides on ast.GlobalRef.Mutable —
// see there for why that is sound where recording a node's *read set* would
// not be. It over-approximates in the safe direction: a `:=` counts by
// spelling alone, and a declaration written anywhere that can run twice counts
// as a write.
//
// Everything else here is **derived from the tree, never annotated onto it**. A
// pass that rewrites a lambda changes which globals it touches, so a recorded
// answer would go stale exactly when a pass is doing the thing that makes it
// matter.

// impure reports whether an expression stops its stage being a pure function
// of its input: a `:=` anywhere in it, or anything it does with a global that
// counts — writing one, or reading one that something can change.
//
// One type switch answers both. Writing it as `ast.HasUpdate(e) ||
// touchesGlobal(e)` walked the whole tree twice, and splitting the assignment
// check out in front of the switch still cost a type assertion per node; both
// showed up as a few percent of optimize time on every program, globals or
// not. The two questions have the same shape, so they share the traversal.
func impure(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.AssignExpr:
		// A `:=` at all, whatever it targets.
		return true
	case *ast.GlobalRef:
		// A read of a global nothing writes is a read of a constant.
		return x.Mutable
	case *ast.BlockBody:
		// A sub-pipeline body: its statements may declare or assign globals,
		// which no expression walk would find. This is the case the original
		// reasoning missed — `ast.HasUpdate` answers "no write here" perfectly
		// correctly and perfectly uselessly for one of these.
		if x.Pipe == nil {
			return false
		}
		return nodesTouchGlobals(x.Pipe.BlockNodes())
	case *ast.UnaryExpr:
		return impure(x.X)
	case *ast.BinaryExpr:
		return impure(x.Left) || impure(x.Right)
	case *ast.FieldAccess:
		return impure(x.Target)
	case *ast.CallExpr:
		for _, a := range x.Args {
			if impure(a) {
				return true
			}
		}
		return false
	case *ast.CondExpr:
		return impure(x.Cond) || impure(x.Then) || impure(x.Else)
	case *ast.LetExpr:
		return impure(x.Value) || impure(x.Body)
	case *ast.AlsoExpr:
		if impure(x.Body) {
			return true
		}
		for _, c := range x.Clauses {
			if impure(c) {
				return true
			}
		}
		return false
	}
	return false
}

// nodesTouchGlobals reports whether any node in a sub-pipeline declares,
// assigns, or reads a global — including through a nested body of its own.
func nodesTouchGlobals(nodes []*ir.Node) bool {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if n.Meta == nil {
			continue
		}
		if _, ok := n.Meta[ir.MetaGlobals]; ok {
			return true // a Cursed Object / Cursed Tool node
		}
		for _, v := range n.Meta {
			switch m := v.(type) {
			case *ast.Lambda:
				if m != nil && impure(m.Body) {
					return true
				}
			case []*ir.Node:
				if nodesTouchGlobals(m) {
					return true
				}
			case [][]*ir.Node:
				for _, sub := range m {
					if nodesTouchGlobals(sub) {
						return true
					}
				}
			}
		}
	}
	return false
}

// lambdaImpure answers both halves of `effectful` for a whole lambda in one
// traversal of its body.
func lambdaImpure(l *ast.Lambda) bool {
	return l != nil && impure(l.Body)
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

// GlobalStandDown is one stage that kept its lambda exactly as written because
// that lambda touches a global.
type GlobalStandDown struct {
	Prim  string         // the stage's primitive
	Pos   token.Position // where it was written
	Names []string       // the globals it touches, in first-seen order
}

// GlobalStandDowns reports every stage whose rewrites were stood down for
// touching a global.
//
// The failure mode of globals is a program that silently got slower: a stage
// that would have been fused or substituted quietly is not, and nothing says
// so. That is the same gap DeclinedInPlace was written to close for the
// linear-accumulator pass, and it is worth closing here for the same reason.
//
// Only mutable globals appear. A stage reading a constant keeps its rewrites,
// so naming it here would send the reader hunting for a cost that does not
// exist — and the useful message is precisely the one that distinguishes the
// two, since "make this global stop changing" is the fix.
//
// It re-derives the answer from the tree rather than recording it when the
// decision was made, for the reason at the top of this file: a pass that
// rewrites a lambda changes which globals it touches.
func GlobalStandDowns(p *ir.Pipeline) []GlobalStandDown {
	if p == nil {
		return nil
	}
	var out []GlobalStandDown
	for _, list := range nodeLists(p) {
		for _, n := range list {
			// Read the lambda straight off Meta rather than through
			// nodeLambda, which returns nil for an effectful one — that is
			// exactly the set this reports on, so going through it would find
			// nothing every time.
			lam, _ := n.Meta["lambda"].(*ast.Lambda)
			if lam == nil {
				continue
			}
			if names := globalNames(lam.Body); len(names) > 0 {
				out = append(out, GlobalStandDown{Prim: n.Prim, Pos: n.Pos, Names: names})
			}
		}
	}
	return out
}

// globalNames is the globals an expression reads or writes, in first-seen
// order and without repeats.
func globalNames(e ast.Expr) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(ast.Expr)
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	walk = func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.GlobalRef:
			// Only a mutable one costs the stage anything; reporting a
			// constant read would send the reader looking for a problem that
			// is not there.
			if x.Mutable {
				add(x.Name)
			}
		case *ast.AssignExpr:
			if x.Target != nil {
				add(x.Target.Name)
			}
			walk(x.Value)
		case *ast.UnaryExpr:
			walk(x.X)
		case *ast.BinaryExpr:
			walk(x.Left)
			walk(x.Right)
		case *ast.FieldAccess:
			walk(x.Target)
		case *ast.CallExpr:
			for _, a := range x.Args {
				walk(a)
			}
		case *ast.CondExpr:
			walk(x.Cond)
			walk(x.Then)
			walk(x.Else)
		case *ast.LetExpr:
			walk(x.Value)
			walk(x.Body)
		case *ast.AlsoExpr:
			walk(x.Body)
			for _, c := range x.Clauses {
				walk(c)
			}
		case *ast.BlockBody:
			// A body's own stages are reported in their own right — nodeLists
			// reaches them — so this only has to say that the *enclosing*
			// lambda is one that touches a global.
			if x.Pipe != nil && nodesTouchGlobals(x.Pipe.BlockNodes()) {
				add("(a global written in the body)")
			}
		}
	}
	walk(e)
	return out
}
