package optimizer

import (
	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Why the linear-accumulator pass left a copy in place.
//
// The pass computes all of this and then throws it away, which is how a program
// deep-copying a map twelve thousand times gets told it is "clean — no errors,
// warnings, or hints". The largest performance fact the language has to offer
// about such a program was already known and simply not said.
//
// A declined site is not necessarily a bug in the program. Some of these
// refusals are the only correct answer — a genuinely observed copy has to
// happen — so these are reported as hints about a cost, never as errors.
const (
	// DeclinedNotRooted: the receiver is not the accumulator, nor a constant
	// tuple field of it. A copy of something else is a copy the pass has proved
	// nothing about.
	DeclinedNotRooted = "receiver-not-rooted"
	// DeclinedKind: the receiver reaches a state field, but of a kind with no
	// in-place update behind it.
	DeclinedKind = "collection-kind"
	// DeclinedReadAfter: the accumulator is read after this update, so the copy
	// is observable and removing it would change the answer.
	DeclinedReadAfter = "read-after-write"
	// DeclinedEffectful: the body writes a binding with `:=`. The pass reasons
	// about evaluation order and a write is what makes the order observable.
	DeclinedEffectful = "effectful-body"
	// DeclinedAliasEscape: a subslice builtin is applied to something rooted at
	// the accumulator, so an in-place write would be visible through storage
	// handed out earlier.
	DeclinedAliasEscape = "storage-escapes"
	// DeclinedUnownableState: the loop's state is not one the backends can take
	// their own copy of on entry, so no write into it could be made safe.
	DeclinedUnownableState = "unownable-state"
	// DeclinedUnreachableStage: an earlier body stage does not take the loop's
	// state and give it back, so the analysis stops before reaching this one.
	DeclinedUnreachableStage = "unreachable-stage"
)

// Declined is one update that could have written in place and did not.
type Declined struct {
	Prim   string         // the primitive whose body the update is in
	Update string         // the builtin: insert, setat, put, set, addnode, addedge
	Pos    token.Position // where the update is written
	Reason string         // one of the Declined* codes above
}

// DeclinedInPlace reports every update in a linear-accumulator position that
// the pass declined to mark, with the reason it declined.
//
// It re-runs the pass's own predicates rather than keeping a second opinion
// about them, so a change to what the pass accepts cannot leave this explaining
// a rule that is no longer there. It must run on a pipeline the pass has already
// been over: a site is "declined" precisely when it is a candidate and its
// InPlace flag is not set.
func DeclinedInPlace(p *ir.Pipeline) []Declined {
	var out []Declined
	seen := map[*ast.Lambda]bool{}
	for _, list := range nodeLists(p) {
		for _, n := range list {
			switch {
			case LinearLoopPrims[n.Prim]:
				// Every stage the pass would look at, and then the stages it
				// would not: a body stage that is not a state-preserving lambda
				// stops the analysis, and any update *after* that point is
				// declined for a reason the reader cannot see in the update
				// itself. Saying nothing about them is how a loop carrying a
				// growing map reads as fully optimized.
				lams := loopBodyLambdas(n)
				for _, lam := range lams {
					if seen[lam] {
						continue
					}
					seen[lam] = true
					out = append(out, declinedIn(n.Display, lam, n.In, !ownableLoopState(n.In))...)
				}
				out = append(out, declinedBeyondAnalysis(n, len(lams), seen)...)
			case LinearAccPrims[n.Prim]:
				lam, _ := n.Meta["lambda"].(*ast.Lambda)
				if lam == nil || seen[lam] || len(lam.Params) < 2 {
					continue
				}
				seen[lam] = true
				// A Fold's receiver has to be the accumulator itself, so the
				// state type is not passed: there are no projections to follow.
				out = append(out, declinedIn(n.Prim, lam, nil, !mutableAcc(n.Out))...)
			}
		}
	}
	return out
}

// declinedIn explains the unmarked candidate sites in one lambda body.
//
// The whole-body refusals come first and speak for every site, because a site's
// own reason is not the interesting one when the body was stood down before any
// site was considered.
func declinedIn(prim string, lam *ast.Lambda, accType *ir.Type, unownable bool) []Declined {
	if len(lam.Params) == 0 {
		return nil
	}
	acc := lam.Params[0]
	reason := ""
	switch {
	case unownable:
		reason = DeclinedUnownableState
	case effectful(lam):
		reason = DeclinedEffectful
	case !aliasSafe(lam.Body, acc):
		reason = DeclinedAliasEscape
	}

	m := &linearMarker{acc: acc, accType: accType}
	m.collectWritten(lam.Body)

	var out []Declined
	m.eachCandidate(lam.Body, func(c *ast.CallExpr, name string) {
		if c.InPlace {
			return
		}
		why := reason
		if why == "" {
			why = m.siteReason(c)
		}
		out = append(out, Declined{Prim: prim, Update: name, Pos: c.Pos, Reason: why})
	})
	return out
}

// siteReason distinguishes the three per-site refusals. It is only asked about a
// site the pass left unmarked in a body it did not stand down whole, so one of
// them holds.
func (m *linearMarker) siteReason(c *ast.CallExpr) string {
	pos, ok := inPlaceUpdates[updateName(c)]
	if !ok || pos >= len(c.Args) {
		return DeclinedNotRooted
	}
	recv := c.Args[pos]
	if m.receiverRooted(recv) {
		// The receiver is fine, so the only test left is the one about what
		// happens after the write.
		return DeclinedReadAfter
	}
	// A receiver that reaches a state field of the wrong kind is worth telling
	// apart from one that reaches nothing: the first is a fact about the type,
	// the second about how the program is written.
	if m.accType != nil {
		if _, isProjection := m.projPath(recv); isProjection {
			return DeclinedKind
		}
	}
	return DeclinedNotRooted
}

// eachCandidate calls fn for every update in e that the pass could in principle
// mark, keeping the alias scoping so that a receiver bound through `consider`
// is judged against the field it really names.
func (m *linearMarker) eachCandidate(e ast.Expr, fn func(*ast.CallExpr, string)) {
	switch x := e.(type) {
	case *ast.CallExpr:
		if name := updateName(x); name != "" {
			if _, ok := inPlaceUpdates[name]; ok {
				fn(x, name)
			}
		}
		for _, a := range x.Args {
			m.eachCandidate(a, fn)
		}
	case *ast.BinaryExpr:
		m.eachCandidate(x.Left, fn)
		m.eachCandidate(x.Right, fn)
	case *ast.UnaryExpr:
		m.eachCandidate(x.X, fn)
	case *ast.FieldAccess:
		m.eachCandidate(x.Target, fn)
	case *ast.CondExpr:
		m.eachCandidate(x.Cond, fn)
		m.eachCandidate(x.Then, fn)
		m.eachCandidate(x.Else, fn)
	case *ast.LetExpr:
		if x.Name == m.acc {
			// The body rebinds the accumulator, so nothing under it is a
			// candidate — the same reading the walk takes.
			m.eachCandidate(x.Value, fn)
			return
		}
		m.eachCandidate(x.Value, fn)
		prev, had := m.bindAlias(x.Name, m.aliasTarget(x.Value))
		m.eachCandidate(x.Body, fn)
		m.restoreAlias(x.Name, prev, had)
	case *ast.AssignExpr:
		m.eachCandidate(x.Value, fn)
	case *ast.AlsoExpr:
		m.eachCandidate(x.Body, fn)
		for _, c := range x.Clauses {
			m.eachCandidate(c, fn)
		}
	}
}

// declinedBeyondAnalysis reports the candidate updates in body stages the pass
// stopped before — a stage that does not take the state and give it back ends
// the chain, and everything past it is unreachable to the analysis however it
// is written.
func declinedBeyondAnalysis(n *ir.Node, analysed int, seen map[*ast.Lambda]bool) []Declined {
	body, _ := n.Meta["nodes"].([]*ir.Node)
	var out []Declined
	for _, stage := range body[min(analysed, len(body)):] {
		lam, _ := stage.Meta["lambda"].(*ast.Lambda)
		if lam == nil || len(lam.Params) == 0 || seen[lam] {
			continue
		}
		seen[lam] = true
		m := &linearMarker{acc: lam.Params[0]}
		m.eachCandidate(lam.Body, func(c *ast.CallExpr, name string) {
			if c.InPlace {
				return
			}
			out = append(out, Declined{
				Prim: n.Display, Update: name, Pos: c.Pos, Reason: DeclinedUnreachableStage,
			})
		})
	}
	return out
}

// updateName is the builtin a call names, or "" when the callee is not a name.
func updateName(c *ast.CallExpr) string {
	id, ok := c.Fn.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}
