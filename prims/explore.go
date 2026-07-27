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
)

var exploreModeNames = map[string]exploreMode{
	"Collect":   exploreCollect,
	"Count":     exploreCount,
	"Distances": exploreDistances,
	"Steps":     exploreSteps,
}

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
					"Explore: unknown Mode %q (Collect, Count, Distances, Steps)", name)}
			}
			mode = m
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

		out := ir.List(in)
		switch mode {
		case exploreCount, exploreSteps:
			out = ir.Int()
		case exploreDistances:
			out = ir.Map(in, ir.Int())
		}

		return &ir.Node{
			Prim:      "Explore",
			In:        in,
			Out:       out,
			Display:   "Explore (" + modeName(mode) + ")",
			Swappable: true,
			Meta: map[string]any{
				"lambda": lam, "until": until, "mode": modeName(mode), "state": in,
			},
			Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				return runExplore(lam, until, mode, in, v, pos)
			},
		}, nil
	},
}

func modeName(m exploreMode) string {
	for n, v := range exploreModeNames {
		if v == m {
			return n
		}
	}
	return "Collect"
}

// runExplore is breadth-first search over the implicit graph the successor
// lambda describes. The visited set is the memo — it is what bounds the
// search over a cyclic state space, and what makes "how many distinct
// configurations" answerable at all.
//
// BFS order (rather than depth-first) is what makes Distances and Steps the
// *shortest* step counts, which is the question AoC actually asks.
func runExplore(lam, until *ast.Lambda, mode exploreMode, state *ir.Type,
	seed ir.Value, pos token.Position) (ir.Value, error) {

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

	hit := func(s ir.Value) (bool, error) {
		if until == nil {
			return false, nil
		}
		r, err := eval.EvalLambdaTyped(until,
			append([]*ir.Type{state}, ambientTypes()...),
			append([]ir.Value{s}, ambientArgs()...)...)
		if err != nil {
			return false, runtimeErr("Explore", pos, "Until: %v", err)
		}
		b, ok := r.(bool)
		if !ok {
			return false, runtimeErr("Explore", pos, "Until: predicate did not return a Bool")
		}
		return b, nil
	}

	// The seed itself can satisfy Until:, in which case the answer is zero
	// steps — the search never expands anything.
	if done, err := hit(seed); err != nil {
		return nil, err
	} else if done {
		return exploreResult(mode, order, dist, 0), nil
	}

	for {
		cur, ok := q.Pop()
		if !ok {
			break
		}
		d, _ := depth.Pop()
		nexts, err := eval.EvalLambdaTyped(lam,
			append([]*ir.Type{state}, ambientTypes()...),
			append([]ir.Value{cur}, ambientArgs()...)...)
		if err != nil {
			return nil, runtimeErr("Explore", pos, "%v", err)
		}
		succ, err := ir.AsList(nexts)
		if err != nil {
			return nil, runtimeErr("Explore", pos, "successors: %v", err)
		}
		for _, s := range succ {
			k := ir.KeyOf(s)
			if seen[k] {
				continue
			}
			seen[k] = true
			order = append(order, s)
			dist.Put(s, d+1)
			done, err := hit(s)
			if err != nil {
				return nil, err
			}
			if done {
				// Until: prunes: the satisfying state is recorded but never
				// expanded, so a search for "the first state like this" stops
				// the moment it is found.
				if mode == exploreSteps {
					return d + 1, nil
				}
				continue
			}
			q.Push(s)
			depth.Push(d + 1)
		}
	}
	return exploreResult(mode, order, dist, -1), nil
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
