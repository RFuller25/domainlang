package optimizer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

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
//   - `del`, and `deledge` for the same reason. Removing a key has to shift
//     the key order, which *would* be visible through a list taken earlier,
//     and there is no existing mutating Without to reuse. Removing an arc
//     shifts the arc indices behind it identically. Both are rare in an
//     accumulator fold, so they buy little for the only genuinely new
//     reasoning on the page.
//   - `with` (Record). A record copy is O(fields) and was never the cost.
//
// `set` (List) is here, and it is the one entry that needs a guard rather than
// an argument. A List is a Go slice at run time and `take`, `drop` and `slice`
// hand out a subslice of the *same backing array* in both backends, so an
// in-place write is visible through any subslice taken earlier and "is the
// accumulator dead?" stops being a question about the accumulator alone. The
// guard is aliasSafe below: no subslice-producing builtin may be applied to
// anything rooted at the accumulator, anywhere in the lambda. A next-pointer
// fold — the reason this matters, and how a circular list is written in a
// language without one — reads only through `item` and passes.
//   - `union`/`intersect`/`difference`, which read two collections and build a
//     third; there is no single receiver being copied.
var inPlaceUpdates = map[string]int{
	"insert": 0, // Map or Set
	"put":    0, // Sparse
	"setat":  0, // Grid
	"set":    0, // List — only behind aliasSafe; see the note above
	// Graph. Both are their functional form minus the clone — AddEdge is
	// Clone-then-addEdge — and neither mutates anything an alias can see:
	// adding a node appends to the node order, and adding an arc appends to
	// (or re-weights in place) one node's arc list. `nodes` and `edges` both
	// copy, so a list taken from the accumulator earlier still reads the same.
	"addnode": 0,
	"addedge": 0,
}

// mutableAcc reports whether an accumulator of this type is worth (and safe)
// to update in place: a pointer-to-struct collection at run time, and a struct
// with its own storage in a compiled binary. List is excluded with `set` above.
func mutableAcc(t *ir.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case ir.KMap, ir.KSet, ir.KGrid, ir.KSparse, ir.KGraph:
		return true
	case ir.KList:
		// Behind aliasSafe, which the caller checks. A slice is the one
		// accumulator whose interior another builtin can hand out.
		return true
	}
	return false
}

// subsliceBuiltins hand out storage the accumulator shares: in both backends
// these are `xs[a:b]` on the same backing array, not a copy.
//
// `concat` is deliberately absent — it allocates a new slice — and so is
// `item`, which reads one element out. Those are the two shapes a next-pointer
// fold is written in, which is why the guard costs it nothing.
var subsliceBuiltins = map[string]bool{
	"take": true, "drop": true, "slice": true,
}

// aliasSafe reports whether a lambda body keeps the accumulator's storage to
// itself: no subslice-producing builtin is applied to anything rooted at it.
//
// Conservative in the direction that matters. It asks nothing about where the
// subslice *goes* — a `take(acc, 3)` whose result is summed and dropped is
// perfectly safe and is refused anyway — because the alternative is an escape
// analysis, and the cost of being wrong here is a program that answers
// differently for reasons no reader could see.
func aliasSafe(body ast.Expr, acc string) bool {
	safe := true
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		if !safe || e == nil {
			return
		}
		switch x := e.(type) {
		case *ast.CallExpr:
			if id, ok := x.Fn.(*ast.Ident); ok && subsliceBuiltins[id.Name] {
				for _, a := range x.Args {
					// touchesAcc, not reads: this asks whether the
					// accumulator's storage is handed out at all, which is a
					// question about any field of it. reads discriminates by
					// field against the writes a body makes, and a marker built
					// here has none recorded — so it would answer "no" to
					// everything and the guard would pass whatever it was shown.
					if touchesAcc(a, acc) {
						safe = false
						return
					}
				}
			}
			for _, a := range x.Args {
				walk(a)
			}
		case *ast.BinaryExpr:
			walk(x.Left)
			walk(x.Right)
		case *ast.CondExpr:
			walk(x.Cond)
			walk(x.Then)
			walk(x.Else)
		case *ast.LetExpr:
			walk(x.Value)
			// A `consider acc as …` shadows the name, and everything under it
			// is about a different value. Refusing to look is the conservative
			// reading and costs nothing real.
			walk(x.Body)
		case *ast.AlsoExpr:
			walk(x.Body)
			for _, c := range x.Clauses {
				walk(c)
			}
		case *ast.UnaryExpr:
			walk(x.X)
		case *ast.FieldAccess:
			walk(x.Target)
		case *ast.AssignExpr:
			walk(x.Value)
		}
	}
	walk(body)
	return safe
}

// touchesAcc reports whether e mentions the accumulator at all, through any
// projection. It is the question aliasSafe asks — has this storage been handed
// out — as opposed to the one `reads` answers, which is whether a particular
// field could observe a particular write.
func touchesAcc(e ast.Expr, acc string) bool {
	found := false
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		if found || e == nil {
			return
		}
		switch x := e.(type) {
		case *ast.Ident:
			if x.Name == acc {
				found = true
			}
		case *ast.UnaryExpr:
			walk(x.X)
		case *ast.FieldAccess:
			walk(x.Target)
		case *ast.BinaryExpr:
			walk(x.Left)
			walk(x.Right)
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
			// A binding of the same name shadows the accumulator inside its
			// body; any other name may carry it in, so the body is walked.
			if x.Name != acc {
				walk(x.Body)
			}
		case *ast.AssignExpr:
			walk(x.Value)
		case *ast.AlsoExpr:
			walk(x.Body)
			for _, c := range x.Clauses {
				walk(c)
			}
		}
	}
	walk(e)
	return found
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

// LinearLoopPrims are the loop primitives that thread a *value* which dies at
// the end of each lap, rather than a lambda parameter.
//
// A loop body is a sub-pipeline, not a lambda, which is why these needed a
// second entry point rather than a row in LinearAccPrims: the accumulator is
// the value entering the body, and the lambda to analyse belongs to the body's
// own stage.
//
// `Iterate` and `Iterate Until Fixed Point` are excluded here for exactly the
// reasons they are excluded there — the first keeps every intermediate in its
// output, the second compares the previous value against the new one — and
// `For` is excluded because its body sees an ambient parameter as well as the
// threaded value, which is a second reader this analysis has not been taught
// about.
var LinearLoopPrims = map[string]bool{
	"Simple Domain (While)":  true,
	"Simple Domain (Repeat)": true,
}

// loopBodyLambdas returns the lambdas a loop's state flows through: one per
// body stage that takes the state and gives the state back.
//
// Every stage, not just the first, and the reason is a measurement. A loop whose
// body is one `Apply` was all this used to accept, on the reasoning that with
// two stages the first stage's output is the second's input and *that* value,
// not the loop's, is what a write would alias. But the second stage's input is
// the first stage's *return value* — the state the loop threads — so the
// question each stage asks is the same one, about its own lambda: can anything
// read the accumulator after this write. Nothing in stage two can observe a copy
// stage one removed, because stage two never sees stage one's input.
//
// What it cost to exclude them: day 6 of the AoC suite is an outer search whose
// body is an `Apply` *and* a nested redistribution loop, and its map insert —
// the whole reason that program is slow — was never even considered. The
// restriction was invisible, which is the property that makes it worth removing
// rather than documenting.
//
// A stage that is not a state-preserving lambda stops the list: the analysis
// only reasons about stages it can see the shape of, and a mark in a later stage
// would be reasoning past one it cannot.
func loopBodyLambdas(n *ir.Node) []*ast.Lambda {
	body, _ := n.Meta["nodes"].([]*ir.Node)
	var out []*ast.Lambda
	for _, stage := range body {
		// Apply, and only Apply. A node's Meta["lambda"] does not mean the same
		// thing for every primitive — a nested loop stores its *predicate*
		// there, which takes the state and returns a Bool — so a stage whose
		// lambda is not the transform must stop the chain rather than be
		// analysed as though it were one. Marking an update inside a predicate
		// would write through a value the loop re-reads on the next lap.
		if stage.Prim != "Apply" {
			return out
		}
		lam, _ := stage.Meta["lambda"].(*ast.Lambda)
		if lam == nil || len(lam.Params) < 1 {
			return out
		}
		// The stage has to take the state and give the state back. Anything
		// else is not the shape this reasons about.
		if stage.In == nil || stage.Out == nil || !stage.In.Equal(stage.Out) || !stage.In.Equal(n.In) {
			return out
		}
		out = append(out, lam)
	}
	return out
}

// markLinearAccumulators is the pass.
func markLinearAccumulators(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	seen := map[*ast.Lambda]bool{}
	for _, list := range nodeLists(p) {
		for _, n := range list {
			if LinearLoopPrims[n.Prim] {
				if r, ok := markLoopState(n, seen); ok {
					rewrites = append(rewrites, r)
				}
				continue
			}
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
			// The alias guard, asked once per lambda: a List accumulator whose
			// storage the body hands out through take/drop/slice is not one
			// this pass may write into. Asked for every accumulator kind
			// rather than only for List, because it is free and because a
			// future collection with the same property should fail closed.
			if !aliasSafe(lam.Body, lam.Params[0]) {
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

// markLoopState is markLinearAccumulators for a loop: the accumulator is the
// value threaded through the body, and the update may be rooted at a *field*
// of it rather than at the value itself.
//
// The simulation shape this exists for keeps its mutable list inside a state
// tuple — `set(item(state, 0), i, v)` — because a loop threads one value and
// a program needs somewhere to put the rest of its variables. Following the
// receiver through constant tuple projections is what makes that shape
// reachable, and restricting the chain to *tuple* fields is what makes the
// clone on entry able to promise it owns every list a mark can reach.
func markLoopState(n *ir.Node, seen map[*ast.Lambda]bool) (Rewrite, bool) {
	if n.In == nil || !ownableLoopState(n.In) {
		return Rewrite{}, false
	}
	marked := 0
	owned := map[string]bool{}
	for _, lam := range loopBodyLambdas(n) {
		if seen[lam] {
			continue
		}
		if effectful(lam) || !aliasSafe(lam.Body, lam.Params[0]) {
			continue
		}
		seen[lam] = true
		m := &linearMarker{acc: lam.Params[0], accType: n.In}
		m.collectWritten(lam.Body)
		m.walk(lam.Body, false)
		marked += m.marked
		// The copy on entry is taken once for the whole loop, so what it has to
		// cover is the union over the stages that write.
		for p := range m.owned {
			owned[p] = true
		}
	}
	if marked == 0 {
		return Rewrite{}, false
	}
	m := &linearMarker{owned: owned}
	// Tell the backends which fields the copy on entry actually has to cover.
	// Without this they own the whole state, and an inner loop that writes only
	// a short list pays to clone every collection beside it on every lap of the
	// loop containing it — which is the quadratic cost this pass exists to
	// remove, reintroduced one level up.
	n.Meta[ir.OwnedFields] = sortedKeys(m.owned)
	return Rewrite{Message: fmt.Sprintf(
		"Domain made %d state update(s) in %s write in place — the copy was "+
			"never read. Guaranteed hit.", marked, n.Display)}, true
}

// ownableLoopState reports whether a loop's state is one the backends can take
// their own copy of on entry: a List, or a tuple of things that are themselves
// ownable.
//
// It is the analysis half of a promise the backends keep. A mark may only be
// rooted at a list this returns true for, so "the clone owns every list an
// in-place write can reach" holds by construction rather than by review.
func ownableLoopState(t *ir.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case ir.KList:
		return true
	case ir.KTuple:
		// Elems, not Fields: a tuple's element types live in Elems and Fields
		// is the record's. Reading the wrong one is silent — the type prints
		// the same either way — and costs every mark in the pass.
		for _, ft := range t.Elems {
			if ft != nil && (ownableNested(ft) || ft.Kind == ir.KTuple) && !ownableLoopState(ft) {
				return false
			}
		}
		return true
	}
	return ownableNested(t)
}

// ownableNested is the collection kinds a state field may hold and the clone on
// entry can copy. It is deliberately the same set as mutableAcc: a mark may be
// rooted at any of them through projectedCollection, so anything this admits
// the backends' ownValue must copy.
func ownableNested(t *ir.Type) bool {
	return mutableAcc(t)
}

// markBody annotates every qualifying update in one lambda body and returns
// how many it marked. acc is the accumulator parameter's name.
func markBody(body ast.Expr, acc string) int {
	m := &linearMarker{acc: acc}
	m.collectWritten(body)
	// Nothing follows the body, so nothing can read the accumulator after it.
	m.walk(body, false)
	return m.marked
}

type linearMarker struct {
	acc    string
	marked int
	// accType is the accumulator's type, set only for a loop state. With it,
	// rootedAtAcc may follow a constant tuple projection to the list inside;
	// without it — the Fold case — the receiver has to be the accumulator
	// itself, exactly as before.
	accType *ir.Type
	// aliases maps a `consider`-bound name to the accumulator expression it is
	// another spelling of — the accumulator itself, or a constant tuple field
	// of it. Without this a receiver written against a bound name defeated the
	// pass silently, so `insert(item(s, 1), k, v)` was rewritten and
	// `consider tape as item(s, 1) in insert(tape, k, v)` was not, though the
	// two mean the same thing and nothing in the program looks different.
	//
	// Every target stored here is already resolved through this map, so it
	// never holds a name pointing at another name and following one terminates.
	aliases map[string]ast.Expr
	// owned is the set of state fields a *marked* update writes through, as
	// projection paths. It is what the clone on entry has to copy, and only
	// that: owning the whole state instead is what turned an inner loop that
	// writes a sixteen-element list into one that also copies the twelve
	// thousand entry map beside it, once per lap of the loop outside it.
	//
	// Distinct from `written` below, which is every *candidate* receiver
	// whether or not the mark survived, and answers a different question — what
	// a read through an alias might observe.
	owned map[string]bool
	// written is the set of state fields some update in this body writes to,
	// as projection paths ("" for the accumulator itself, "1" for item(s, 1)).
	// Collected in a pre-pass, because whether a read *through an alias* can
	// observe a write depends on which field the alias names.
	//
	// Without it, counting every alias read as a read of the accumulator makes
	// the pass refuse the one idiom that earns it: binding each state field to
	// a name before writing, then rebuilding the tuple from those names. Those
	// reads name *other* fields and cannot observe the write, and treating them
	// as if they could took back the whole Map-in-loop-state rewrite.
	written map[string]bool
}

// projPath is the canonical path of a projection chain rooted at the
// accumulator: "" for the accumulator itself, "1" for item(s, 1), "1.0" for
// item(item(s, 1), 0). The second result is false for anything else.
//
// A name aliased to the accumulator resolves through the alias map first, so
// two spellings of one field produce one path.
func (m *linearMarker) projPath(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		if x.Name == m.acc {
			return "", true
		}
		if t, ok := m.aliases[x.Name]; ok {
			return m.projPath(t)
		}
	case *ast.CallExpr:
		id, ok := x.Fn.(*ast.Ident)
		if !ok || id.Name != "item" || len(x.Args) != 2 {
			return "", false
		}
		lit, ok := x.Args[1].(*ast.IntLit)
		if !ok {
			return "", false
		}
		outer, ok := m.projPath(x.Args[0])
		if !ok {
			return "", false
		}
		if outer == "" {
			return strconv.FormatInt(lit.Value, 10), true
		}
		return outer + "." + strconv.FormatInt(lit.Value, 10), true
	}
	return "", false
}

// recordOwned notes which state field a marked update writes through, so the
// clone on entry can copy that field and leave the rest of the state alone.
//
// A receiver reached through a chain of updates — `insert(insert(acc, …), …)` —
// bottoms out at the same field as the innermost one, so the chain is followed
// the way receiverRooted follows it. A receiver this cannot resolve is recorded
// as the whole accumulator, which is the answer that is never wrong.
func (m *linearMarker) recordOwned(c *ast.CallExpr) {
	if m.owned == nil {
		m.owned = map[string]bool{}
	}
	pos, ok := inPlaceUpdates[updateName(c)]
	if !ok || pos >= len(c.Args) {
		m.owned[ir.OwnsEverything] = true
		return
	}
	e := c.Args[pos]
	for {
		if id, ok := e.(*ast.Ident); ok {
			if t, ok := m.aliases[id.Name]; ok {
				e = t
				continue
			}
			break
		}
		inner, ok := e.(*ast.CallExpr)
		if !ok {
			break
		}
		p, ok := inPlaceUpdates[updateName(inner)]
		if !ok || p >= len(inner.Args) {
			break
		}
		e = inner.Args[p]
	}
	if path, ok := m.projPath(e); ok {
		m.owned[path] = true
		return
	}
	m.owned[ir.OwnsEverything] = true
}

// sortedKeys renders a path set in a stable order, so the generated Go does not
// change from run to run.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// overlapsWritten reports whether a read of the field at path p could observe
// one of the writes this body makes. Paths overlap when either contains the
// other: writing the whole accumulator is visible through any field of it, and
// writing a field is visible through the accumulator.
func (m *linearMarker) overlapsWritten(p string) bool {
	for w := range m.written {
		if w == p || w == "" || p == "" ||
			strings.HasPrefix(p, w+".") || strings.HasPrefix(w, p+".") {
			return true
		}
	}
	return false
}

// collectWritten records the path of every update receiver in a body, keeping
// the same alias scoping the marking walk uses so a receiver written against a
// bound name lands on the field it really names.
func (m *linearMarker) collectWritten(e ast.Expr) {
	switch x := e.(type) {
	case *ast.CallExpr:
		if id, ok := x.Fn.(*ast.Ident); ok {
			if pos, ok := inPlaceUpdates[id.Name]; ok && pos < len(x.Args) {
				if p, ok := m.projPath(x.Args[pos]); ok {
					if m.written == nil {
						m.written = map[string]bool{}
					}
					m.written[p] = true
				}
			}
		}
		for _, a := range x.Args {
			m.collectWritten(a)
		}
	case *ast.BinaryExpr:
		m.collectWritten(x.Left)
		m.collectWritten(x.Right)
	case *ast.UnaryExpr:
		m.collectWritten(x.X)
	case *ast.FieldAccess:
		m.collectWritten(x.Target)
	case *ast.CondExpr:
		m.collectWritten(x.Cond)
		m.collectWritten(x.Then)
		m.collectWritten(x.Else)
	case *ast.LetExpr:
		if x.Name == m.acc {
			m.collectWritten(x.Value)
			return
		}
		target := m.aliasTarget(x.Value)
		m.collectWritten(x.Value)
		prev, had := m.bindAlias(x.Name, target)
		m.collectWritten(x.Body)
		m.restoreAlias(x.Name, prev, had)
	case *ast.AssignExpr:
		m.collectWritten(x.Value)
	case *ast.AlsoExpr:
		m.collectWritten(x.Body)
		for _, c := range x.Clauses {
			m.collectWritten(c)
		}
	}
}

// aliasTarget is the accumulator expression a `consider` value is another name
// for, or nil when the binding names something else.
//
// Deliberately narrow: the accumulator, a constant tuple projection from it, or
// a name already established as one of those. A binding whose value is an
// *update* is not aliased — the receiver chain in receiverRooted already
// handles `insert(insert(acc, …), …)`, and a name for a copy would need the
// read-after-write question asked about the copy rather than the accumulator.
func (m *linearMarker) aliasTarget(v ast.Expr) ast.Expr {
	switch x := v.(type) {
	case *ast.Ident:
		if x.Name == m.acc {
			return v
		}
		if t, ok := m.aliases[x.Name]; ok {
			return t
		}
	case *ast.CallExpr:
		if m.accType != nil && m.projectionType(v) != nil {
			return v
		}
	}
	return nil
}

// bindAlias points name at target for the rest of a scope, and returns what it
// meant before so the caller can put it back. A nil target still binds — the
// name is shadowing whatever it meant outside, and must stop aliasing.
func (m *linearMarker) bindAlias(name string, target ast.Expr) (ast.Expr, bool) {
	prev, had := m.aliases[name]
	if target == nil {
		delete(m.aliases, name)
		return prev, had
	}
	if m.aliases == nil {
		m.aliases = map[string]ast.Expr{}
	}
	m.aliases[name] = target
	return prev, had
}

// restoreAlias undoes bindAlias.
func (m *linearMarker) restoreAlias(name string, prev ast.Expr, had bool) {
	if had {
		m.aliases[name] = prev
		return
	}
	delete(m.aliases, name)
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
			m.recordOwned(x)
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
		// `consider tape as item(s, 1)` gives a state field a second name, and
		// an update written against that name means what the projection means.
		//
		// The order here is the scoping: the binding has to be in force while
		// the *body* is examined — so that a read through the new name counts
		// as a read of the accumulator, which is what keeps the mark honest —
		// and out of force while the *value* is walked, which belongs to the
		// enclosing scope and may refer to an outer binding of the same name.
		target := m.aliasTarget(x.Value)
		prev, had := m.bindAlias(x.Name, target)
		bodyReads := m.reads(x.Body)
		m.restoreAlias(x.Name, prev, had)

		m.walk(x.Value, usedAfter || bodyReads)

		m.bindAlias(x.Name, target)
		m.walk(x.Body, usedAfter)
		m.restoreAlias(x.Name, prev, had)
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
	c, ok := x.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := c.Fn.(*ast.Ident)
	if !ok {
		return false
	}
	pos, ok := inPlaceUpdates[id.Name]
	if !ok || pos >= len(c.Args) {
		return false
	}
	return m.receiverRooted(c.Args[pos])
}

// receiverRooted reports whether an update's receiver bottoms out at the
// accumulator: directly, through another qualifying update, or — for a loop
// state — through a constant tuple projection reaching a list.
//
// The outermost expression is tested by rootedAtAcc and has to *be* an update.
// Folding that test in here instead would mark the projection itself: a bare
// `item(state, 0)` bottoms out at the accumulator too, and annotating it would
// claim an in-place write on a read.
func (m *linearMarker) receiverRooted(e ast.Expr) bool {
	for {
		if id, ok := e.(*ast.Ident); ok {
			if id.Name == m.acc {
				return true
			}
			// A name `consider`-bound to the accumulator or one of its fields.
			// Targets are pre-resolved, so this follows at most one hop.
			if t, ok := m.aliases[id.Name]; ok {
				e = t
				continue
			}
			return false
		}
		c, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		id, ok := c.Fn.(*ast.Ident)
		if !ok {
			return false
		}
		if pos, ok := inPlaceUpdates[id.Name]; ok && pos < len(c.Args) {
			e = c.Args[pos]
			continue
		}
		// A loop state's collection may sit inside the tuple the loop threads,
		// and the projection to it is part of the receiver rather than a read
		// of something else.
		if m.accType != nil && m.projectedCollection(c) {
			return true
		}
		return false
	}
}

// projectedCollection reports whether c is `item(…, k)` reaching a collection
// this pass may write into, through a chain of constant tuple fields from the
// accumulator.
//
// Constant indices only, and tuples only. A variable index would name a
// different collection on different laps, and a projection through a *list*
// would reach storage the clone on entry does not own — both are refusals
// rather than problems to solve, because the cost of being wrong is a silently
// different answer.
//
// The kind test is mutableAcc, the same one the fold path asks of an
// accumulator, rather than KList alone. A loop threads one value, so anything
// carrying more than a single collection has to put it in a tuple — and a Map
// or Set in loop state was deep-copied on every lap for want of this. Widening
// it needs no new argument about aliasing: the reasoning for a Map is the one
// already recorded above inPlaceUpdates and is *weaker* than the one accepted
// for List, since no builtin hands out a Map's or Set's interior — keys, values
// and tolist all copy — whereas take/drop/slice hand out a list's backing
// array, which is why List alone still needs aliasSafe.
//
// What this does depend on is the other half of the promise: ownValueExpr in
// codegen and ownValue in prims clone every collection kind reachable here, not
// just the lists. Widen one without the other and the loop writes through to
// storage its caller still holds.
func (m *linearMarker) projectedCollection(c *ast.CallExpr) bool {
	return mutableAcc(m.projectionType(c))
}

// projectionType is the type of e, when e is the accumulator or a chain of
// constant tuple projections from it, and nil for anything else.
func (m *linearMarker) projectionType(e ast.Expr) *ir.Type {
	switch x := e.(type) {
	case *ast.Ident:
		if x.Name == m.acc {
			return m.accType
		}
		return nil
	case *ast.CallExpr:
		id, ok := x.Fn.(*ast.Ident)
		if !ok || id.Name != "item" || len(x.Args) != 2 {
			return nil
		}
		lit, ok := x.Args[1].(*ast.IntLit)
		if !ok {
			return nil
		}
		recv := m.projectionType(x.Args[0])
		if recv == nil || recv.Kind != ir.KTuple {
			return nil
		}
		if lit.Value < 0 || int(lit.Value) >= len(recv.Elems) {
			return nil
		}
		return recv.Elems[lit.Value]
	}
	return nil
}

// reads reports whether evaluating e can read the accumulator. A shadowing
// `consider` of the same name hides it, exactly as it does for the walk.
//
// A name aliased to the accumulator counts as a read of it. That is not an
// optional companion to following aliases in receiverRooted — it is what makes
// following them safe, since otherwise `size(tape)` after a write through
// `tape` would look like a read of something unrelated and the copy would be
// removed out from under it.
//
// Bindings nested inside e are given the same scoping the walk gives them, so a
// name shadowed deeper down stops aliasing there and a new alias introduced
// deeper down starts. Getting that wrong in the other direction would only cost
// marks — an over-reported read is safe, an under-reported one is the answer —
// but the walk and this have to agree about what a name means or the pass is
// reasoning about two different programs.
func (m *linearMarker) reads(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		if x.Name == m.acc {
			return true
		}
		// An aliased name counts only when the field it names could show one of
		// this body's writes. A name for another field holds a value captured
		// when the binding ran and cannot observe anything written later.
		if t, ok := m.aliases[x.Name]; ok {
			p, ok := m.projPath(t)
			return !ok || m.overlapsWritten(p)
		}
		return false
	case *ast.UnaryExpr:
		return m.reads(x.X)
	case *ast.FieldAccess:
		return m.reads(x.Target)
	case *ast.BinaryExpr:
		return m.reads(x.Left) || m.reads(x.Right)
	case *ast.CallExpr:
		// A read of one state field cannot observe a write to another, so
		// `item(s, 2)` after a write to field 0 is not a read of anything this
		// body changed. Without this the rule is all-or-nothing across the whole
		// state, and the rewrite depends on the order the `consider` lines are
		// written in: binding every other field before the update earns it and
		// rebuilding the tuple from `item(s, k)` afterwards does not, for a
		// difference no reader can see.
		//
		// Only constant tuple projections qualify. A variable index names a
		// different element on different laps, and projPath refuses it, which
		// leaves the conservative answer below.
		if p, ok := m.projPath(x); ok {
			return m.overlapsWritten(p)
		}
		return m.readsAny(x.Args)
	case *ast.CondExpr:
		return m.reads(x.Cond) || m.reads(x.Then) || m.reads(x.Else)
	case *ast.LetExpr:
		if x.Name == m.acc {
			return m.reads(x.Value)
		}
		if m.reads(x.Value) {
			return true
		}
		// The value is in the enclosing scope; the body sees the binding, which
		// either aliases a state field or shadows whatever the name meant.
		target := m.aliasTarget(x.Value)
		prev, had := m.bindAlias(x.Name, target)
		got := m.reads(x.Body)
		m.restoreAlias(x.Name, prev, had)
		return got
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
