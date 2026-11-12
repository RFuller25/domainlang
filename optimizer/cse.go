package optimizer

import (
	"fmt"
	"slices"

	"domain/ast"
	"domain/format"
	"domain/token"
)

// Common subexpressions inside a lambda body.
//
// Three rules, in increasing order of how much they have to prove:
//
//  1. Available expressions. `consider a as E in … E …`: inside the body the
//     second E is `a`. The binding has already evaluated E successfully, E has
//     no effects and no randomness, and nothing it reads can have changed, so
//     it needs no totality at all — a partial E is shared exactly as safely as
//     a total one, because the first evaluation is the one that would have
//     failed. This is what `consider steps as item(item(x, 0), i) in
//     consider current as item(item(x, 0), i) in …` is.
//
//  2. A member of its own list. `consider m as max(xs) in consider i as
//     indexof(xs, m) in consider v as item(xs, i) in …`: over a List<Int>, the
//     maximum is one of the elements (and max has already failed on an empty
//     list by the time m exists), so indexof finds it and item(xs, i) is m.
//     The Int restriction is real: NaN is not equal to itself, so over Floats
//     indexof answers -1 and item fails.
//
//  3. A total, costly subexpression written twice with no binding. It is bound
//     once at the start of the scope that holds both. Totality is what makes
//     moving it earlier safe; the placement rules below are what make sure the
//     move never evaluates it somewhere it was not evaluated before.
//
// Every binding these rules synthesize is an ordinary `consider`, which both
// backends already run and compile. The linear-accumulator pass runs after all
// of this and sees only the final body: a read that moved earlier is a read
// that no longer follows a write, and a binding is an alias the pass already
// follows (linearMarker.aliasTarget).

// scope is what the simplifier knows about the `consider` bindings enclosing
// the expression it is rewriting. It is replaced, never mutated, so leaving a
// binding restores the outer scope by assignment.
type scope struct {
	avail   []availEntry
	members map[string]string  // m → xs: m is an element of the Int list xs
	pos     map[string]posFact // i → where in xs the member m sits
	bound   map[string]int     // names some enclosing `consider` has bound
}

type availEntry struct {
	key  string
	expr ast.Expr
	name string
}

type posFact struct{ list, elem string }

// projection reports whether e is a name or a path of constant projections
// from one: cheap to repeat, and — for the linear pass — the shape a receiver
// is followed through, which an alias would hide.
func projection(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return true
	case *ast.FieldAccess:
		return projection(x.Target)
	case *ast.CallExpr:
		if builtinName(x) == "item" && len(x.Args) == 2 {
			if _, ok := x.Args[1].(*ast.IntLit); ok {
				return projection(x.Args[0])
			}
		}
	}
	return false
}

// shareable is an expression worth naming: a builtin call that is not a
// projection and draws no randomness.
func shareable(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	return ok && builtinName(c) != "" && !projection(e) && !ast.HasNondeterminism(e)
}

// enterLet is the scope inside `consider name as val in …`, val being the
// already simplified value.
func (s *simplifier) enterLet(name string, val ast.Expr) scope {
	old := s.scope
	ns := scope{members: map[string]string{}, pos: map[string]posFact{}, bound: map[string]int{}}
	for _, a := range old.avail {
		// The binding shadows `name`: an entry that read the outer one, or that
		// was bound to that name, is about a different value in the body.
		if a.name != name && !mentions(a.expr, name) {
			ns.avail = append(ns.avail, a)
		}
	}
	for m, xs := range old.members {
		if m != name && xs != name {
			ns.members[m] = xs
		}
	}
	for i, f := range old.pos {
		if i != name && f.list != name && f.elem != name {
			ns.pos[i] = f
		}
	}
	for k, v := range old.bound {
		ns.bound[k] = v
	}
	ns.bound[name]++
	if shareable(val) && !mentions(val, name) {
		ns.avail = append(ns.avail, availEntry{key: format.Expr(val), expr: val, name: name})
	}
	if c, ok := val.(*ast.CallExpr); ok {
		switch fn := builtinName(c); {
		case (fn == "max" || fn == "min") && len(c.Args) == 1:
			if xs, ok := c.Args[0].(*ast.Ident); ok && xs.Name != name && s.intListName(xs, old) {
				ns.members[name] = xs.Name
			}
		case fn == "indexof" && len(c.Args) == 2:
			xs, ok1 := c.Args[0].(*ast.Ident)
			m, ok2 := c.Args[1].(*ast.Ident)
			if ok1 && ok2 && xs.Name != name && m.Name != name && ns.members[m.Name] == xs.Name {
				ns.pos[name] = posFact{list: xs.Name, elem: m.Name}
			}
		}
	}
	return ns
}

// intListName: xs is a List<Int> the environment really types — a name some
// `consider` has rebound is not one, since the environment knows only the
// stage's own parameters.
func (s *simplifier) intListName(xs *ast.Ident, sc scope) bool {
	return sc.bound[xs.Name] == 0 && s.intList(xs)
}

// availName is the binding that already holds e, if one does.
func (s *simplifier) availName(e ast.Expr) (string, bool) {
	if len(s.scope.avail) == 0 || !shareable(e) {
		return "", false
	}
	key := format.Expr(e)
	for i := len(s.scope.avail) - 1; i >= 0; i-- {
		if s.scope.avail[i].key == key {
			return s.scope.avail[i].name, true
		}
	}
	return "", false
}

// itemOfMember is rule 2: item(xs, indexof(xs, m)), and item(xs, i) for an `i`
// bound to that, where m is known to be an element of the Int list xs.
func (s *simplifier) itemOfMember(x *ast.CallExpr) (ast.Expr, bool) {
	if builtinName(x) != "item" || len(x.Args) != 2 {
		return x, false
	}
	xs, ok := x.Args[0].(*ast.Ident)
	if !ok || !s.intListName(xs, s.scope) {
		return x, false
	}
	switch a := x.Args[1].(type) {
	case *ast.Ident:
		if f, ok := s.scope.pos[a.Name]; ok && f.list == xs.Name {
			s.mark("cheaper equivalent builtin")
			return &ast.Ident{Name: f.elem, Pos: x.Pos}, true
		}
	case *ast.CallExpr:
		if builtinName(a) != "indexof" || len(a.Args) != 2 {
			return x, false
		}
		if l, ok := a.Args[0].(*ast.Ident); !ok || l.Name != xs.Name {
			return x, false
		}
		switch e := a.Args[1].(type) {
		case *ast.Ident:
			if s.scope.members[e.Name] == xs.Name {
				s.mark("cheaper equivalent builtin")
				return e, true
			}
		case *ast.CallExpr:
			// item(xs, indexof(xs, max(xs))): max fails on an empty list in
			// both forms, and is one of the elements otherwise.
			if fn := builtinName(e); (fn == "max" || fn == "min") && len(e.Args) == 1 {
				if arg, ok := e.Args[0].(*ast.Ident); ok && arg.Name == xs.Name {
					s.mark("cheaper equivalent builtin")
					return e, true
				}
			}
		}
	}
	return x, false
}

// ---------------------------------------------------------------------------
// Rule 3: a total, costly subexpression written more than once
// ---------------------------------------------------------------------------

// share binds repeated total subexpressions once, at the start of the
// narrowest scope that holds every occurrence — the body, a `consider`'s body,
// a conditional's arm, the right of an and/or — so a binding is never
// evaluated on a path that did not evaluate the expression before.
//
// A candidate must be total (moving a partial expression earlier changes which
// failure is reported first), pure, costly, and written at least once on
// *every* path through the scope and at least twice on some: with one
// occurrence per path nothing is saved, and with none on some path the binding
// would add work there. A candidate that reads a name bound inside the scope
// is shared inside that binding's own body instead.
func (s *simplifier) share(e ast.Expr) ast.Expr {
	for range 8 {
		cand := s.pickShared(e)
		if cand == nil {
			break
		}
		name := freshName(e)
		repl := replaceExpr(e, format.Expr(cand), &ast.Ident{Name: name}, cand)
		s.mark("common subexpression")
		e = &ast.LetExpr{Name: name, Value: cand, Body: repl, Pos: exprPos(cand)}
	}
	return s.descend(e)
}

// descend shares inside every narrower scope of e.
func (s *simplifier) descend(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: x.Op, X: s.descend(x.X), Pos: x.Pos}
	case *ast.BinaryExpr:
		right := s.descend(x.Right)
		if x.Op == token.AND || x.Op == token.OR {
			right = s.share(x.Right)
		}
		return &ast.BinaryExpr{Op: x.Op, Left: s.descend(x.Left), Right: right, Pos: x.Pos}
	case *ast.FieldAccess:
		return &ast.FieldAccess{Target: s.descend(x.Target), Field: x.Field, Pos: x.Pos}
	case *ast.CallExpr:
		args := make([]ast.Expr, len(x.Args))
		for i, a := range x.Args {
			args[i] = s.descend(a)
		}
		return &ast.CallExpr{Fn: x.Fn, Args: args, Pos: x.Pos, InPlace: x.InPlace}
	case *ast.CondExpr:
		return &ast.CondExpr{Cond: s.descend(x.Cond), Then: s.share(x.Then), Else: s.share(x.Else), Pos: x.Pos}
	case *ast.LetExpr:
		return &ast.LetExpr{Name: x.Name, Value: s.descend(x.Value), Body: s.share(x.Body), Pos: x.Pos}
	}
	return e
}

// pickShared finds the largest expression worth binding at the top of e.
func (s *simplifier) pickShared(e ast.Expr) ast.Expr {
	counts := map[string]int{}
	exprs := map[string]ast.Expr{}
	var scan func(e ast.Expr, bound []string)
	scan = func(e ast.Expr, bound []string) {
		switch x := e.(type) {
		case *ast.UnaryExpr:
			scan(x.X, bound)
		case *ast.BinaryExpr:
			scan(x.Left, bound)
			scan(x.Right, bound)
		case *ast.FieldAccess:
			scan(x.Target, bound)
		case *ast.CallExpr:
			if shareable(e) && costly(e) && isTotalIn(e, s.env) &&
				!slices.ContainsFunc(bound, func(n string) bool { return mentions(e, n) }) {
				k := format.Expr(e)
				counts[k]++
				exprs[k] = e
			}
			for _, a := range x.Args {
				scan(a, bound)
			}
		case *ast.CondExpr:
			scan(x.Cond, bound)
			scan(x.Then, bound)
			scan(x.Else, bound)
		case *ast.LetExpr:
			scan(x.Value, bound)
			scan(x.Body, append(slices.Clone(bound), x.Name))
		}
	}
	scan(e, nil)
	var best ast.Expr
	bestKey := ""
	for k, n := range counts {
		if n < 2 {
			continue
		}
		lo, hi := occurrences(e, k, exprs[k])
		if lo < 1 || hi < 2 {
			continue
		}
		if best == nil || len(k) > len(bestKey) || len(k) == len(bestKey) && k < bestKey {
			best, bestKey = exprs[k], k
		}
	}
	return best
}

// occurrences counts how often key is evaluated along the cheapest and the
// dearest path through e. An occurrence under a `consider` that rebinds a name
// the expression reads is a different expression and does not count.
func occurrences(e ast.Expr, key string, cand ast.Expr) (lo, hi int) {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		return occurrences(x.X, key, cand)
	case *ast.FieldAccess:
		return occurrences(x.Target, key, cand)
	case *ast.BinaryExpr:
		ll, lh := occurrences(x.Left, key, cand)
		rl, rh := occurrences(x.Right, key, cand)
		if x.Op == token.AND || x.Op == token.OR {
			rl = 0 // the right operand is only sometimes evaluated
		}
		return ll + rl, lh + rh
	case *ast.CallExpr:
		if shareable(e) && format.Expr(e) == key {
			return 1, 1
		}
		for _, a := range x.Args {
			l, h := occurrences(a, key, cand)
			lo, hi = lo+l, hi+h
		}
		return lo, hi
	case *ast.CondExpr:
		cl, ch := occurrences(x.Cond, key, cand)
		tl, th := occurrences(x.Then, key, cand)
		el, eh := occurrences(x.Else, key, cand)
		return cl + min(tl, el), ch + max(th, eh)
	case *ast.LetExpr:
		vl, vh := occurrences(x.Value, key, cand)
		if mentions(cand, x.Name) {
			return vl, vh
		}
		bl, bh := occurrences(x.Body, key, cand)
		return vl + bl, vh + bh
	}
	return 0, 0
}

// replaceExpr rebuilds e with every occurrence of key replaced by repl, except
// under a `consider` that rebinds a name cand reads.
func replaceExpr(e ast.Expr, key string, repl ast.Expr, cand ast.Expr) ast.Expr {
	rec := func(x ast.Expr) ast.Expr { return replaceExpr(x, key, repl, cand) }
	switch x := e.(type) {
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: x.Op, X: rec(x.X), Pos: x.Pos}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{Op: x.Op, Left: rec(x.Left), Right: rec(x.Right), Pos: x.Pos}
	case *ast.FieldAccess:
		return &ast.FieldAccess{Target: rec(x.Target), Field: x.Field, Pos: x.Pos}
	case *ast.CallExpr:
		if shareable(e) && format.Expr(e) == key {
			return repl
		}
		args := make([]ast.Expr, len(x.Args))
		for i, a := range x.Args {
			args[i] = rec(a)
		}
		return &ast.CallExpr{Fn: x.Fn, Args: args, Pos: x.Pos, InPlace: x.InPlace}
	case *ast.CondExpr:
		return &ast.CondExpr{Cond: rec(x.Cond), Then: rec(x.Then), Else: rec(x.Else), Pos: x.Pos}
	case *ast.LetExpr:
		body := x.Body
		if !mentions(cand, x.Name) {
			body = rec(body)
		}
		return &ast.LetExpr{Name: x.Name, Value: rec(x.Value), Body: body, Pos: x.Pos}
	}
	return e
}

// freshName is a binding name no part of e uses. It contains a `#`, which no
// source name can, so it cannot capture or be captured; the counter makes a
// second round's name differ from the first's.
func freshName(e ast.Expr) string {
	for i := 0; ; i++ {
		name := fmt.Sprintf("cse#%d", i)
		if !mentions(e, name) && !bindsName(e, name) {
			return name
		}
	}
}

// bindsName reports whether e contains a `consider` of name anywhere.
func bindsName(e ast.Expr, name string) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		return bindsName(x.X, name)
	case *ast.BinaryExpr:
		return bindsName(x.Left, name) || bindsName(x.Right, name)
	case *ast.FieldAccess:
		return bindsName(x.Target, name)
	case *ast.CallExpr:
		return slices.ContainsFunc(x.Args, func(a ast.Expr) bool { return bindsName(a, name) })
	case *ast.CondExpr:
		return bindsName(x.Cond, name) || bindsName(x.Then, name) || bindsName(x.Else, name)
	case *ast.LetExpr:
		return x.Name == name || bindsName(x.Value, name) || bindsName(x.Body, name)
	}
	return false
}
