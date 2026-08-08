package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/ir"
)

// Linear accumulators: the pass that stops a Fold from being quadratic.
//
// `insert`, `del`, `put` and `setat` are functional — each returns a new
// collection and leaves its argument untouched — and they are implemented by
// copying. That makes building a collection one write at a time quadratic in
// the *collection*, not in the writes: a 20,000-step DP over a Map took 30 s
// interpreted and 12 s compiled, and 20,000 setat writes into a 300x300 grid
// took 44 s, because each of them copied all 90,000 cells.
//
// The semantics are right and do not change. What this pass observes is that a
// Fold's accumulator is *dead* the instant the lambda returns, so when nothing
// in the body can read the copied-from value after the update, the copy is
// unobservable and can be skipped. Sites where that holds are marked
// ast.CallExpr.InPlace, and both backends write through instead of copying.
//
// Two things make that safe rather than merely plausible:
//
//   - The last-use test is **path-sensitive**. `if wanted(x) then insert(acc,
//     k, x) else acc` is the ordinary shape of a conditional record, and a
//     positional "is this the textually last mention" rule refuses it, because
//     the `else acc` comes after. Conditional arms are mutually exclusive, so
//     a use in one is not a use after a site in the other.
//   - The accumulator is **cloned once on entry** by whichever primitive
//     drives the fold. That is the firewall against aliasing this pass cannot
//     see: a Part or a Channel branches from one value, so the seed may be
//     held elsewhere. One O(size) copy amortized over n writes, instead of n
//     of them.
//
// The pass runs after the rewrite cascade reaches its fixpoint, so no later
// pass can duplicate an annotated call — expression simplification folds a
// constant by applying a lambda twice, which is exactly what an in-place
// update must not have done to it.

// inPlaceUpdates are the builtins this pass may mark, mapped to the position
// of the receiver they would copy. Each one is exactly its existing functional
// implementation minus the clone — `With` is `Clone` then `Put`, so the
// in-place form is `Put` — which is what makes them correct by inspection
// rather than by a second implementation.
//
// They also share the property the analysis quietly depends on: **none of them
// mutates a cell an existing alias can see.** Inserting a new key appends to
// the key order and writes a map entry; overwriting one touches only the map;
// `setat` and `put` write a cell nothing hands out. No builtin returns a
// collection's internal storage — `keys`, `values` and `tolist` all copy — so
// a list taken from the accumulator before an update still reads the same.
//
// Deliberately not here:
//
//   - `del`. Removing a key has to shift the key order, which *would* be
//     visible through a list taken earlier, and there is no existing mutating
//     Without to reuse. It is also rare in an accumulator fold, so it buys
//     little for the only genuinely new reasoning on the page.
//   - `set` (List) and `with` (Record). A List is a Go slice at run time and
//     `take`/`drop`/`slice` hand out subslices of the same backing array, so
//     "who else can see this" stops being a question about the accumulator
//     alone. A Record copy is O(fields) and was never the cost.
//   - `union`/`intersect`/`difference`, which read two collections and build a
//     third; there is no single receiver being copied.
var inPlaceUpdates = map[string]int{
	"insert": 0, // Map or Set
	"put":    0, // Sparse
	"setat":  0, // Grid
}

// mutableAcc reports whether an accumulator of this type is worth (and safe)
// to update in place: a pointer-to-struct collection at run time, and a struct
// with its own storage in a compiled binary. List is excluded with `set` above.
func mutableAcc(t *ir.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case ir.KMap, ir.KSet, ir.KGrid, ir.KSparse:
		return true
	}
	return false
}

// LinearAccPrims are the primitives whose lambda threads an accumulator that
// dies at the end of each step. Exported because the two backends have to
// clone the accumulator for exactly these, and agreeing on the list by reading
// it beats agreeing on it by remembering.
//
// `Scan` and `Iterate` are excluded *by construction* rather than by a check
// that could rot: both keep every intermediate accumulator in their output, so
// the value is still live when the next step begins. So is `Iterate Until
// Fixed Point`, which compares the previous value against the new one to
// detect convergence and would end up comparing a value against itself.
var LinearAccPrims = map[string]bool{
	"Fold":     true,
	"FoldOver": true, // Fold From: a channel — the instruction-driven simulation
	"Reduce":   true,
}

// markLinearAccumulators is the pass.
func markLinearAccumulators(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	seen := map[*ast.Lambda]bool{}
	for _, list := range nodeLists(p) {
		for _, n := range list {
			if !LinearAccPrims[n.Prim] {
				continue
			}
			if !mutableAcc(n.Out) {
				continue
			}
			lam, _ := n.Meta["lambda"].(*ast.Lambda)
			if lam == nil || seen[lam] || len(lam.Params) < 2 {
				continue
			}
			// A body that writes a binding with `:=` stands every rewrite
			// down, this one included: the pass reasons about evaluation
			// order, and a write is the one thing that makes the order
			// observable in a way the tree alone does not show.
			if effectful(lam) {
				continue
			}
			seen[lam] = true
			marked := markBody(lam.Body, lam.Params[0])
			if marked == 0 {
				continue
			}
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain made %d accumulator update(s) in %s write in place — "+
					"the copy was never read. Guaranteed hit.", marked, n.Prim)})
		}
	}
	return rewrites
}

// markBody annotates every qualifying update in one lambda body and returns
// how many it marked. acc is the accumulator parameter's name.
func markBody(body ast.Expr, acc string) int {
	m := &linearMarker{acc: acc}
	// Nothing follows the body, so nothing can read the accumulator after it.
	m.walk(body, false)
	return m.marked
}

type linearMarker struct {
	acc    string
	marked int
}

// walk visits e knowing whether the accumulator may be read *after* e finishes
// on some path that runs e. It annotates the update sites it finds on the way
// down, since a site's own safety depends only on what comes after it.
//
// The traversal mirrors eval's evaluation order exactly, which is the whole
// content of the analysis:
//
//	CallExpr    args left to right, then the call
//	BinaryExpr  left, then right (and/or short-circuit, so the right side is
//	            conditional — assuming it runs is the conservative reading)
//	CondExpr    the condition, then exactly one arm
//	LetExpr     the value, then the body
//	AlsoExpr    the body, then each clause in order
func (m *linearMarker) walk(e ast.Expr, usedAfter bool) {
	switch x := e.(type) {
	case *ast.CallExpr:
		if m.rootedAtAcc(x) && !usedAfter {
			x.InPlace = true
			m.marked++
		}
		for i, a := range x.Args {
			// Later arguments run after this one, so their reads count.
			after := usedAfter
			for _, later := range x.Args[i+1:] {
				if m.reads(later) {
					after = true
					break
				}
			}
			m.walk(a, after)
		}
	case *ast.BinaryExpr:
		m.walk(x.Left, usedAfter || m.reads(x.Right))
		m.walk(x.Right, usedAfter)
	case *ast.CondExpr:
		// The arms are mutually exclusive: a read in one is not a read after a
		// site in the other. This is what lets the conditional-record shape
		// update in place.
		m.walk(x.Cond, usedAfter || m.reads(x.Then) || m.reads(x.Else))
		m.walk(x.Then, usedAfter)
		m.walk(x.Else, usedAfter)
	case *ast.LetExpr:
		if x.Name == m.acc {
			// The body rebinds the name, so no site inside it is rooted at the
			// accumulator and no read inside it is a read of it. The value
			// expression is still in the outer scope.
			m.walk(x.Value, usedAfter)
			inner := &linearMarker{acc: "\x00shadowed"}
			inner.walk(x.Body, usedAfter)
			return
		}
		m.walk(x.Value, usedAfter || m.reads(x.Body))
		m.walk(x.Body, usedAfter)
	case *ast.AlsoExpr:
		m.walk(x.Body, usedAfter || m.readsAny(x.Clauses))
		for i, c := range x.Clauses {
			m.walk(c, usedAfter || m.readsAny(x.Clauses[i+1:]))
		}
	case *ast.UnaryExpr:
		m.walk(x.X, usedAfter)
	case *ast.FieldAccess:
		m.walk(x.Target, usedAfter)
	case *ast.AssignExpr:
		m.walk(x.Value, usedAfter)
	}
}

// rootedAtAcc reports whether x is an update whose receiver bottoms out at the
// accumulator: either directly, or through another qualifying update, so
// `insert(insert(acc, …), …)` chains without an intermediate copy.
func (m *linearMarker) rootedAtAcc(x ast.Expr) bool {
	for {
		c, ok := x.(*ast.CallExpr)
		if !ok {
			id, ok := x.(*ast.Ident)
			return ok && id.Name == m.acc
		}
		id, ok := c.Fn.(*ast.Ident)
		if !ok {
			return false
		}
		pos, ok := inPlaceUpdates[id.Name]
		if !ok || pos >= len(c.Args) {
			return false
		}
		x = c.Args[pos]
	}
}

// reads reports whether evaluating e can read the accumulator. A shadowing
// `consider` of the same name hides it, exactly as it does for the walk.
func (m *linearMarker) reads(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == m.acc
	case *ast.UnaryExpr:
		return m.reads(x.X)
	case *ast.FieldAccess:
		return m.reads(x.Target)
	case *ast.BinaryExpr:
		return m.reads(x.Left) || m.reads(x.Right)
	case *ast.CallExpr:
		return m.readsAny(x.Args)
	case *ast.CondExpr:
		return m.reads(x.Cond) || m.reads(x.Then) || m.reads(x.Else)
	case *ast.LetExpr:
		if x.Name == m.acc {
			return m.reads(x.Value)
		}
		return m.reads(x.Value) || m.reads(x.Body)
	case *ast.AssignExpr:
		return m.reads(x.Value)
	case *ast.AlsoExpr:
		return m.reads(x.Body) || m.readsAny(x.Clauses)
	}
	return false
}

func (m *linearMarker) readsAny(es []ast.Expr) bool {
	for _, e := range es {
		if m.reads(e) {
			return true
		}
	}
	return false
}
