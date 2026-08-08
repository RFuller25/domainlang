// Domain Expansion: Explore — a bounded search over a state space.
//
// Domain has no recursion: a Shikigami is inlined at its call site, so a
// self-referential one has no finite expansion, and the resolver refuses it
// (prims/shikigami.go). Explore is the language's answer for the problems
// that seem to need recursion — reachability, fewest-moves, "how many
// distinct configurations" — expressed declaratively so it stays a named
// algorithm the optimizer is free to substitute.
//
// It is also the non-grid half of graph search. BFS/Dijkstra/Flood Fill all
// take a Grid; Explore takes a *state* and a successor lambda, so the graph
// can be implicit — nodes named in a text file, tuples of coordinates and
// facing, whatever the problem is actually about.
package prims

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// exploreMode selects what a completed search reports.
type exploreMode int

const (
	exploreCollect   exploreMode = iota // List<S>, BFS order, distinct
	exploreCount                        // Int: how many distinct states
	exploreDistances                    // Map<S, Int>: steps from the seed
	exploreSteps                        // Int: steps to the first Until: hit, or -1
	exploreCheapest                     // Int: Cost: to the first Until: hit, or -1
	exploreCosts                        // Map<S, Int>: cheapest Cost: to each state
	exploreTally                        // V: Value:/Combine: folded over the reachable DAG
)

var exploreModeNames = map[string]exploreMode{
	"Collect":   exploreCollect,
	"Count":     exploreCount,
	"Distances": exploreDistances,
	"Steps":     exploreSteps,
	"Cheapest":  exploreCheapest,
	"Costs":     exploreCosts,
	"Tally":     exploreTally,
}

// modeList is the name list the unknown-Mode error prints, in the order this
// file introduces them rather than a map's.
const modeList = "Collect, Count, Distances, Steps, Cheapest, Costs, Tally"

// weighted reports the modes that pay a Cost: per edge and therefore search
// with a priority queue instead of a plain queue.
func (m exploreMode) weighted() bool { return m == exploreCheapest || m == exploreCosts }

var explore = &Primitive{
	ID:      "Explore",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Explore") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Explore has no seed state"}
		}
		// The seed is the current pipeline value, so a state is whatever the
		// pipeline already carries. It must be keyable: the visited set is
		// what makes the search terminate, and an unkeyable state could not
		// be recognized on a second visit.
		if !ir.Keyable(in) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Explore needs a keyable state (Int, Text, or a Tuple/Record of them), got %s — "+
					"build a compound state with tuple(...)", in)}
		}
		lam, err := requireLambda(args, 1, "Explore", pos)
		if err != nil {
			return nil, err
		}
		nextT, err := typecheck.LambdaType(lam, append([]*ir.Type{in}, ambientTypes()...)...)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Explore: " + err.Error()}
		}
		if nextT == nil || nextT.Kind != ir.KList || !nextT.Elem.Equal(in) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Explore's Using: lambda must return the successor states as List<%s>, got %s", in, nextT)}
		}

		mode := exploreCollect
		if name, ok := args.Ident("Mode"); ok {
			m, known := exploreModeNames[name]
			if !known {
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Explore: unknown Mode %q (%s)", name, modeList)}
			}
			mode = m
		}

		// Cost: makes the search weighted. Its lambda comes in two arities,
		// because both questions are asked: `(t) -> Int` is the cost of
		// *entering* a state — the convention grid Dijkstra already follows,
		// where the start's own value is not paid — and `(s, t) -> Int` is the
		// cost of the edge between two, which a graph with weighted edges
		// needs and a node weight cannot express.
		cost, costArity, err := exploreCost(args, in, mode, pos)
		if err != nil {
			return nil, err
		}

		// Tally folds the reachable DAG instead of walking it: a state with no
		// successors takes Value:, and every other state is its successors'
		// values folded with Combine:.
		value, combine, tallyT, err := exploreTallyArgs(args, in, mode, pos)
		if err != nil {
			return nil, err
		}

		// Until: stops the search early. It is required by Steps (which
		// reports the distance to the first state satisfying it) and optional
		// elsewhere, where it simply prunes.
		var until *ast.Lambda
		if u, ok := args.Lambda("Until"); ok {
			ut, err := typecheck.LambdaType(u, append([]*ir.Type{in}, ambientTypes()...)...)
			if err != nil {
				return nil, &ResolveError{Pos: pos, Msg: "Explore Until: " + err.Error()}
			}
			if !ut.Equal(ir.Bool()) {
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Explore's Until: predicate must return Bool, got %s", ut)}
			}
			until = u
		}
		if mode == exploreSteps && until == nil {
			return nil, &ResolveError{Pos: pos,
				Msg: "Explore Mode: Steps needs an Until: predicate saying which state to measure to"}
		}
		if mode == exploreCheapest && until == nil {
			return nil, &ResolveError{Pos: pos,
				Msg: "Explore Mode: Cheapest needs an Until: predicate saying which state to measure to"}
		}

		out := ir.List(in)
		switch mode {
		case exploreCount, exploreSteps, exploreCheapest:
			out = ir.Int()
		case exploreDistances, exploreCosts:
			out = ir.Map(in, ir.Int())
		case exploreTally:
			out = tallyT
		}

		return &ir.Node{
			Prim:      "Explore",
			In:        in,
			Out:       out,
			Display:   "Explore (" + modeName(mode) + ")",
			Swappable: true,
			Meta: map[string]any{
				"lambda": lam, "until": until, "mode": modeName(mode), "state": in,
				"cost": cost, "costArity": costArity,
				"value": value, "combine": combine, "tally": tallyT,
			},
			Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				s := &search{
					lam: lam, until: until, cost: cost, costArity: costArity,
					value: value, combine: combine, tallyT: tallyT,
					mode: mode, state: in, pos: pos,
				}
				return s.run(v)
			},
		}, nil
	},
}

// exploreCost reads and checks the Cost: lambda, returning it with the arity
// it was written at (1 for a node weight, 2 for an edge weight).
//
// It is required by the weighted modes and refused by the rest, the same
// shape of rule as Until: being required by Steps: the step-counting modes
// weigh every edge the same by definition, so a Cost: there names something
// the answer does not depend on.
func exploreCost(args ArgSet, state *ir.Type, mode exploreMode, pos token.Position) (*ast.Lambda, int, error) {
	lam, ok := args.Lambda("Cost")
	if !ok {
		if mode.weighted() {
			return nil, 0, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Explore Mode: %s needs a Cost: lambda — (t) -> Int for the cost of entering a state, "+
					"or (s, t) -> Int for the cost of an edge", modeName(mode))}
		}
		return nil, 0, nil
	}
	if !mode.weighted() {
		return nil, 0, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Explore: Cost: applies to Mode: Cheapest and Mode: Costs — Mode: %s counts steps, "+
				"which weighs every edge the same", modeName(mode))}
	}
	arity := len(lam.Params) - ambientDepth()
	if arity != 1 && arity != 2 {
		return nil, 0, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Explore's Cost: lambda takes 1 parameter (the state being entered) or 2 (the edge, "+
				"from and to), got %d", arity)}
	}
	params := []*ir.Type{state}
	if arity == 2 {
		params = []*ir.Type{state, state}
	}
	ct, err := typecheck.LambdaType(lam, append(params, ambientTypes()...)...)
	if err != nil {
		return nil, 0, &ResolveError{Pos: pos, Msg: "Explore Cost: " + err.Error()}
	}
	if !ct.Equal(ir.Int()) {
		return nil, 0, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Explore's Cost: lambda must return Int, got %s", ct)}
	}
	return lam, arity, nil
}

// exploreTallyArgs reads and checks Value: and Combine:, returning them and
// the type they fold to. Both are required by Tally and refused elsewhere.
func exploreTallyArgs(args ArgSet, state *ir.Type, mode exploreMode, pos token.Position) (
	value, combine *ast.Lambda, out *ir.Type, err error) {

	value, hasValue := args.Lambda("Value")
	combine, hasCombine := args.Lambda("Combine")
	if mode != exploreTally {
		for name, present := range map[string]bool{"Value": hasValue, "Combine": hasCombine} {
			if present {
				return nil, nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Explore: %s: applies to Mode: Tally, which folds the reachable states — "+
						"Mode: %s reports the states themselves", name, modeName(mode))}
			}
		}
		return nil, nil, nil, nil
	}
	if !hasValue || !hasCombine {
		return nil, nil, nil, &ResolveError{Pos: pos, Msg: "Explore Mode: Tally needs both a " +
			"Value: lambda (what a state with no successors contributes) and a Combine: lambda " +
			"(how a state's successors fold together)"}
	}
	valueT, err := typecheck.LambdaType(value, append([]*ir.Type{state}, ambientTypes()...)...)
	if err != nil {
		return nil, nil, nil, &ResolveError{Pos: pos, Msg: "Explore Value: " + err.Error()}
	}
	combineT, err := typecheck.LambdaType(combine, append([]*ir.Type{valueT, valueT}, ambientTypes()...)...)
	if err != nil {
		return nil, nil, nil, &ResolveError{Pos: pos, Msg: "Explore Combine: " + err.Error()}
	}
	if !combineT.Equal(valueT) {
		return nil, nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"Explore's Combine: lambda must fold two %s into one, got %s — it is applied to what "+
				"Value: produced", valueT, combineT)}
	}
	return value, combine, valueT, nil
}

func modeName(m exploreMode) string {
	for n, v := range exploreModeNames {
		if v == m {
			return n
		}
	}
	return "Collect"
}

// search carries one Explore's resolved arguments. The three algorithms below
// share the successor lambda, the Until: pruning rule and the keyable-state
// memo; they differ in what orders the frontier and what they accumulate.
type search struct {
	lam, until     *ast.Lambda
	cost           *ast.Lambda
	costArity      int
	value, combine *ast.Lambda
	tallyT         *ir.Type // what Value: produces, so Combine: can be typed
	mode           exploreMode
	state          *ir.Type
	pos            token.Position
}

func (s *search) run(seed ir.Value) (ir.Value, error) {
	switch {
	case s.mode == exploreTally:
		return s.tally(seed)
	case s.mode.weighted():
		return s.cheapest(seed)
	}
	return s.breadthFirst(seed)
}

// call applies a lambda to the given arguments, threading the ambient For
// variables the way every other primitive here does.
func (s *search) call(lam *ast.Lambda, params []*ir.Type, args ...ir.Value) (ir.Value, error) {
	return eval.EvalLambdaTyped(lam,
		append(params, ambientTypes()...),
		append(args, ambientArgs()...)...)
}

// successors evaluates the Using: lambda.
func (s *search) successors(cur ir.Value) ([]ir.Value, error) {
	nexts, err := s.call(s.lam, []*ir.Type{s.state}, cur)
	if err != nil {
		return nil, runtimeErr("Explore", s.pos, "%v", err)
	}
	succ, err := ir.AsList(nexts)
	if err != nil {
		return nil, runtimeErr("Explore", s.pos, "successors: %v", err)
	}
	return succ, nil
}

// hit evaluates Until:, which is absent for most searches.
func (s *search) hit(v ir.Value) (bool, error) {
	if s.until == nil {
		return false, nil
	}
	r, err := s.call(s.until, []*ir.Type{s.state}, v)
	if err != nil {
		return false, runtimeErr("Explore", s.pos, "Until: %v", err)
	}
	b, ok := r.(bool)
	if !ok {
		return false, runtimeErr("Explore", s.pos, "Until: predicate did not return a Bool")
	}
	return b, nil
}

// edgeCost is what entering `to` from `from` costs. A negative one is a
// runtime error rather than a wrong answer: Dijkstra settles a state the first
// time it is popped, which a negative edge can invalidate after the fact. Grid
// Dijkstra refuses negative cells for the same reason.
func (s *search) edgeCost(from, to ir.Value) (int64, error) {
	params := []*ir.Type{s.state}
	args := []ir.Value{to}
	if s.costArity == 2 {
		params = []*ir.Type{s.state, s.state}
		args = []ir.Value{from, to}
	}
	r, err := s.call(s.cost, params, args...)
	if err != nil {
		return 0, runtimeErr("Explore", s.pos, "Cost: %v", err)
	}
	c, ok := r.(int64)
	if !ok {
		return 0, runtimeErr("Explore", s.pos, "Cost: lambda did not return an Int")
	}
	if c < 0 {
		return 0, runtimeErr("Explore", s.pos,
			"Cost: returned %d — a negative cost has no cheapest path to settle on", c)
	}
	return c, nil
}

// breadthFirst is the unweighted search: every edge costs one step, so a plain
// queue visits states in nondecreasing distance and the first time a state is
// seen is its shortest distance.
//
// The visited set is the memo — it is what bounds the search over a cyclic
// state space, and what makes "how many distinct configurations" answerable
// at all.
func (s *search) breadthFirst(seed ir.Value) (ir.Value, error) {
	seen := map[any]bool{}
	order := []ir.Value{}
	dist := ir.NewMapValue()

	var q ir.Queue[ir.Value]
	var depth ir.Queue[int64]
	q.Push(seed)
	depth.Push(0)
	seen[ir.KeyOf(seed)] = true
	order = append(order, seed)
	dist.Put(seed, int64(0))

	// The seed itself can satisfy Until:, in which case the answer is zero
	// steps — the search never expands anything.
	if done, err := s.hit(seed); err != nil {
		return nil, err
	} else if done {
		return exploreResult(s.mode, order, dist, 0), nil
	}

	for {
		cur, ok := q.Pop()
		if !ok {
			break
		}
		d, _ := depth.Pop()
		succ, err := s.successors(cur)
		if err != nil {
			return nil, err
		}
		for _, n := range succ {
			k := ir.KeyOf(n)
			if seen[k] {
				continue
			}
			seen[k] = true
			order = append(order, n)
			dist.Put(n, d+1)
			done, err := s.hit(n)
			if err != nil {
				return nil, err
			}
			if done {
				// Until: prunes: the satisfying state is recorded but never
				// expanded, so a search for "the first state like this" stops
				// the moment it is found.
				if s.mode == exploreSteps {
					return d + 1, nil
				}
				continue
			}
			q.Push(n)
			depth.Push(d + 1)
		}
	}
	return exploreResult(s.mode, order, dist, -1), nil
}

// cheapest is Dijkstra over the same implicit graph: the frontier is a
// min-heap keyed by cost so far, and a state is *settled* the first time it is
// popped rather than the first time it is seen. That is the whole difference
// from breadthFirst, and it is why Cost: must be non-negative.
//
// ir.PQ breaks equal priorities by insertion order, so two runs of the same
// program settle states in the same order and the Costs map renders
// identically — in both backends.
func (s *search) cheapest(seed ir.Value) (ir.Value, error) {
	settled := map[any]bool{}
	best := map[any]int64{}
	costs := ir.NewMapValue()

	var q ir.PQ[ir.Value]
	q.Push(seed, 0)
	best[ir.KeyOf(seed)] = 0

	for {
		cur, c, ok := q.Pop()
		if !ok {
			break
		}
		k := ir.KeyOf(cur)
		if settled[k] {
			// A cheaper route reached this state first; the stale entry is
			// left in the heap rather than searched for and removed.
			continue
		}
		settled[k] = true
		costs.Put(cur, c)

		done, err := s.hit(cur)
		if err != nil {
			return nil, err
		}
		if done {
			// Until: prunes here too, and because states settle in cost order
			// the first hit is the cheapest one.
			if s.mode == exploreCheapest {
				return c, nil
			}
			continue
		}

		succ, err := s.successors(cur)
		if err != nil {
			return nil, err
		}
		for _, n := range succ {
			nk := ir.KeyOf(n)
			if settled[nk] {
				continue
			}
			w, err := s.edgeCost(cur, n)
			if err != nil {
				return nil, err
			}
			nc := c + w
			if prev, known := best[nk]; known && prev <= nc {
				continue
			}
			best[nk] = nc
			q.Push(n, nc)
		}
	}
	if s.mode == exploreCheapest {
		return int64(-1), nil
	}
	return costs, nil
}

// tally folds the reachable DAG rather than walking it: a state with no
// successors contributes Value:, and every other state is its successors'
// values folded with Combine:. That is what a memo table *is*, which is why
// this is the mode that answers "how many ways".
//
// The walk is an explicit stack rather than Go recursion, so a deep DAG is
// bounded by the heap instead of by the goroutine stack — the same reason
// Explore exists at all.
func (s *search) tally(seed ir.Value) (ir.Value, error) {
	memo := map[any]ir.Value{}
	kids := map[any][]ir.Value{}
	onStack := map[any]bool{}

	type frame struct {
		v        ir.Value
		expanded bool
	}
	stack := []frame{{v: seed}}

	for len(stack) > 0 {
		f := stack[len(stack)-1]
		k := ir.KeyOf(f.v)

		if !f.expanded {
			if _, done := memo[k]; done {
				stack = stack[:len(stack)-1]
				continue
			}
			// A state reached again while still on the stack closes a cycle,
			// and a cycle has no finite fold. Name it: "there is a cycle"
			// alone leaves it to be found by hand in a large search space,
			// which is the same reason Topological Sort names a blocked node.
			if onStack[k] {
				return nil, runtimeErr("Explore", s.pos,
					"Mode: Tally needs an acyclic search, but %s is reachable from itself",
					ir.FormatValue(f.v))
			}
			onStack[k] = true
			stack[len(stack)-1].expanded = true

			// Until: marks a leaf: a satisfying state is never expanded, so it
			// contributes its Value: and stops. That is the same pruning rule
			// the other modes follow, and it is what makes "count the paths
			// that reach the goal" the natural spelling.
			done, err := s.hit(f.v)
			if err != nil {
				return nil, err
			}
			var succ []ir.Value
			if !done {
				if succ, err = s.successors(f.v); err != nil {
					return nil, err
				}
			}
			kids[k] = succ
			for i := len(succ) - 1; i >= 0; i-- {
				stack = append(stack, frame{v: succ[i]})
			}
			continue
		}

		stack = stack[:len(stack)-1]
		onStack[k] = false
		if _, done := memo[k]; done {
			continue
		}
		succ := kids[k]
		if len(succ) == 0 {
			v, err := s.call(s.value, []*ir.Type{s.state}, f.v)
			if err != nil {
				return nil, runtimeErr("Explore", s.pos, "Value: %v", err)
			}
			memo[k] = v
			continue
		}
		acc := memo[ir.KeyOf(succ[0])]
		for _, n := range succ[1:] {
			v, err := s.call(s.combine, []*ir.Type{s.tallyT, s.tallyT}, acc, memo[ir.KeyOf(n)])
			if err != nil {
				return nil, runtimeErr("Explore", s.pos, "Combine: %v", err)
			}
			acc = v
		}
		memo[k] = acc
	}
	return memo[ir.KeyOf(seed)], nil
}

// exploreResult shapes a finished search per the mode. steps is the answer for
// Mode: Steps — -1 when the search exhausted without an Until: hit, the same
// "not there" sentinel Find Index uses.
func exploreResult(mode exploreMode, order []ir.Value, dist *ir.MapValue, steps int64) ir.Value {
	switch mode {
	case exploreCount:
		return int64(len(order))
	case exploreDistances:
		return dist
	case exploreSteps:
		return steps
	}
	return order
}
