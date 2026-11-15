// Destructuring a tuple parameter instead of indexing it.
//
//	(st) -> consider j as item(st, 1) in tuple(item(st, 0), j + 1)
//
// becomes
//
//	((js, j)) -> tuple(js, j + 1)
//
// The rewrite is mechanical — a pattern is defined as shorthand for exactly
// those `item` calls — except for one thing a machine cannot supply: the
// names. `item(st, 9)` does not say what field nine is. So names are only ever
// taken from the program:
//
//   - a `consider NAME as item(p, k) in …` in the lambda names field k, and the
//     line goes away with the indexing;
//   - failing that, a name another lambda already gave the same field of the
//     same tuple type is borrowed.
//
// A lambda with a field nobody has named is left alone and reported, because
// `((st0, _, _, st3))` is not the readability win this is for. Naming a field
// once, anywhere, is enough for the rest to follow on the next save.
package tidy

import (
	"fmt"
	"sort"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// lamToks is where a lambda's pieces are in the token stream.
type lamToks struct {
	params   [][2]int // token range of each parameter, [first, last)
	simple   []bool   // whether the parameter is a bare name rather than a pattern
	body     []int    // indices of the body's tokens, layout excluded
	ok       bool
	bodyFrom int
}

func (u *unit) lambdaTokens(l *ast.Lambda) lamToks {
	var lt lamToks
	i, ok := u.tokAt[l.Pos.Offset]
	if !ok || u.toks[i].Kind != token.LPAREN {
		return lt
	}
	i++
	for u.toks[i].Kind != token.RPAREN {
		switch u.toks[i].Kind {
		case token.IDENT:
			lt.params = append(lt.params, [2]int{i, i + 1})
			lt.simple = append(lt.simple, true)
			i++
		case token.LPAREN:
			start, depth := i, 0
			for ; i < len(u.toks); i++ {
				if u.toks[i].Kind == token.LPAREN {
					depth++
				} else if u.toks[i].Kind == token.RPAREN {
					depth--
					if depth == 0 {
						i++
						break
					}
				} else if u.toks[i].Kind == token.EOF {
					return lt
				}
			}
			lt.params = append(lt.params, [2]int{start, i})
			lt.simple = append(lt.simple, false)
		default:
			return lt
		}
		if u.toks[i].Kind == token.COMMA {
			i++
		}
	}
	i++ // the closing parenthesis
	if u.toks[i].Kind != token.ARROW {
		return lt
	}
	i++
	lt.bodyFrom = i

	// The body runs to the end of its logical line. A value continued on
	// indented lines is one INDENT deeper and ends where that indentation
	// does; inside a parenthesis the lexer emits no layout at all.
	depth := 0
	for ; i < len(u.toks); i++ {
		t := u.toks[i]
		switch t.Kind {
		case token.EOF:
			lt.ok = true
			return lt
		case token.INDENT:
			depth++
		case token.DEDENT:
			depth--
			if depth <= 0 {
				lt.ok = true
				return lt
			}
		case token.NEWLINE:
			if depth == 0 && u.toks[i+1].Kind != token.INDENT {
				lt.ok = true
				return lt
			}
		default:
			lt.body = append(lt.body, i)
		}
	}
	return lt
}

// itemAt reports whether the body tokens at position j spell `item(p, K)`,
// returning K.
func (u *unit) itemAt(body []int, j int, p string) (k int, ok bool) {
	if j+6 > len(body) {
		return 0, false
	}
	t := func(n int) token.Token { return u.toks[body[j+n]] }
	if t(0).Kind != token.IDENT || t(0).Literal != "item" ||
		t(1).Kind != token.LPAREN ||
		t(2).Kind != token.IDENT || t(2).Literal != p ||
		t(3).Kind != token.COMMA ||
		t(4).Kind != token.INT ||
		t(5).Kind != token.RPAREN {
		return 0, false
	}
	if _, err := fmt.Sscanf(t(4).Literal, "%d", &k); err != nil {
		return 0, false
	}
	return k, true
}

// lamFacts is what a lambda's body says about its names.
type lamFacts struct {
	idents   map[string]int
	lets     []*ast.LetExpr
	letCount map[string]int
	assigned map[string]bool
}

func factsOf(l *ast.Lambda) lamFacts {
	f := lamFacts{idents: map[string]int{}, letCount: map[string]int{}, assigned: map[string]bool{}}
	walkExpr(l.Body, func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.Ident:
			f.idents[x.Name]++
		case *ast.LetExpr:
			f.lets = append(f.lets, x)
			f.letCount[x.Name]++
		case *ast.AssignExpr:
			f.assigned[x.Name] = true
		}
	})
	return f
}

// itemIndex reports whether e is `item(p, K)` with a literal K.
func itemIndex(e ast.Expr, p string) (int, bool) {
	c, ok := e.(*ast.CallExpr)
	if !ok || len(c.Args) != 2 {
		return 0, false
	}
	if fn, ok := c.Fn.(*ast.Ident); !ok || fn.Name != "item" {
		return 0, false
	}
	if id, ok := c.Args[0].(*ast.Ident); !ok || id.Name != p {
		return 0, false
	}
	lit, ok := c.Args[1].(*ast.IntLit)
	if !ok {
		return 0, false
	}
	return int(lit.Value), true
}

// tupleParam is one parameter that could become a pattern.
type tupleParam struct {
	site   lambdaSite
	lt     lamToks
	facts  lamFacts
	index  int
	name   string
	typ    *ir.Type
	used   map[int]bool
	names  map[int]string       // field → name, from this lambda's own considers
	naming map[int]*ast.LetExpr // the consider that supplied each of those
}

// scopeKey is where a borrowed name may come from. A tuple of three or more
// fields is specific enough that two lambdas over the same type mean the same
// thing by it. A pair is not — `(Int, Int)` is a point in one statement and a
// range in the next — so a pair's names are only shared within one top-level
// statement.
func (c *tupleParam) scopeKey() string {
	if len(c.typ.Elems) >= 3 {
		return c.typ.String()
	}
	return fmt.Sprintf("%s@%d", c.typ, c.site.top)
}

func passDestructure(u *unit) (edits []edit, summary string, notes []string) {
	edits, summary, notes, _ = u.destructure(nil)
	return edits, summary, notes
}

// destructure is the pass, with names supplied from outside taking the place
// of names the program never gave: seed maps a scope key to field names. It
// also returns the parameters still waiting for one.
func (u *unit) destructure(seed map[string]map[int]string) (edits []edit, summary string, notes []string, waiting []*tupleParam) {
	table := map[string]map[int]string{}
	for key, names := range seed {
		table[key] = map[int]string{}
		for k, n := range names {
			table[key][k] = n
		}
	}
	learn := func(key string, k int, name string) {
		if table[key] == nil {
			table[key] = map[int]string{}
		}
		if _, seen := table[key][k]; !seen {
			table[key][k] = name
		}
	}

	var cands []*tupleParam
	for _, site := range u.lambdas() {
		l := site.lam
		types := u.paramTypes(l)
		if types == nil {
			continue
		}
		// Patterns already written teach the table what their fields are called.
		for i := range l.Params {
			if i >= len(l.Patterns) || l.Patterns[i] == nil || types[i].Kind != ir.KTuple {
				continue
			}
			probe := &tupleParam{site: site, typ: types[i]}
			for k, f := range l.Patterns[i].Fields {
				if f.Sub == nil && f.Name != "_" && k < len(types[i].Elems) {
					learn(probe.scopeKey(), k, f.Name)
				}
			}
		}

		lt := u.lambdaTokens(l)
		if !lt.ok || len(lt.params) != len(l.Params) {
			continue
		}
		facts := factsOf(l)
		for i, p := range l.Params {
			if !lt.simple[i] || types[i].Kind != ir.KTuple {
				continue
			}
			if c := u.tupleCandidate(site, lt, facts, i, p, types[i]); c != nil {
				cands = append(cands, c)
				for k, name := range c.names {
					learn(c.scopeKey(), k, name)
				}
			}
		}
	}

	done, unnamed := 0, 0
	taken := map[*ast.Lambda]map[string]bool{}
	for _, c := range cands {
		l := c.site.lam
		if taken[l] == nil {
			taken[l] = map[string]bool{}
			for _, n := range l.BoundNames() {
				taken[l][n] = true
			}
		}
		names := map[int]string{}
		complete := true
		for k := range c.used {
			if n, ok := c.names[k]; ok && !taken[l][n] {
				names[k] = n
				continue
			}
			// A borrowed name must mean nothing in this lambda already.
			n, ok := table[c.scopeKey()][k]
			if !ok || taken[l][n] || c.facts.idents[n] > 0 || c.facts.letCount[n] > 0 || c.facts.assigned[n] {
				complete = false
				break
			}
			names[k] = n
		}
		// Two fields under one name would be a duplicate parameter.
		seen := map[string]bool{}
		for _, n := range names {
			if seen[n] {
				complete = false
			}
			seen[n] = true
		}
		if !complete {
			unnamed++
			waiting = append(waiting, c)
			continue
		}
		es, ok := u.destructureEdits(c, names)
		if !ok {
			continue
		}
		for _, n := range names {
			taken[l][n] = true
		}
		edits = append(edits, es...)
		done++
	}

	if unnamed > 0 {
		notes = append(notes, fmt.Sprintf(
			"%s still indexed with item(): name a field once with `consider NAME as item(p, k) in` and the pattern follows",
			plural(unnamed, "tuple parameter is", "tuple parameters are")))
	}
	if done == 0 {
		return nil, "", notes, waiting
	}
	return edits, "destructured " + plural(done, "tuple parameter", "tuple parameters"), notes, waiting
}

// FieldAsk is a tuple whose fields need names before the lambdas indexing it
// can be destructured.
type FieldAsk struct {
	// Key identifies the tuple to NameFields.
	Key string
	// Param and Line are one lambda parameter of this type, for saying which
	// tuple is meant; Type is how the type checker spells it.
	Param string
	Line  int
	Type  string
	// Fields are the ones something reads and nothing has named, in order.
	Fields []FieldInfo
}

// FieldInfo is one field of a FieldAsk.
type FieldInfo struct {
	Index int
	Type  string
}

// UnnamedFields lists the tuples that destructuring is waiting on names for,
// the one whose lambda is nearest line first.
func UnnamedFields(path, src string, line int) []FieldAsk {
	u, err := load(path, src)
	if err != nil {
		return nil
	}
	_, _, _, waiting := u.destructure(nil)
	byKey := map[string]*FieldAsk{}
	var order []string
	have := map[string]map[int]bool{}
	for _, c := range waiting {
		key := c.scopeKey()
		ask := byKey[key]
		if ask == nil {
			ask = &FieldAsk{Key: key, Param: c.name, Line: c.site.lam.Pos.Line, Type: c.typ.String()}
			byKey[key], have[key] = ask, map[int]bool{}
			order = append(order, key)
		}
		if abs(c.site.lam.Pos.Line-line) < abs(ask.Line-line) {
			ask.Param, ask.Line = c.name, c.site.lam.Pos.Line
		}
		for k := range c.used {
			if _, named := c.names[k]; named || have[key][k] {
				continue
			}
			have[key][k] = true
			ask.Fields = append(ask.Fields, FieldInfo{Index: k, Type: c.typ.Elems[k].String()})
		}
	}
	var out []FieldAsk
	for _, key := range order {
		ask := byKey[key]
		if len(ask.Fields) == 0 {
			continue
		}
		sort.Slice(ask.Fields, func(i, j int) bool { return ask.Fields[i].Index < ask.Fields[j].Index })
		out = append(out, *ask)
	}
	sort.SliceStable(out, func(i, j int) bool { return abs(out[i].Line-line) < abs(out[j].Line-line) })
	return out
}

// NameFields destructures with names supplied for one tuple's fields, which
// is the half of the rewrite a machine cannot do: `item(st, 9)` does not say
// what field nine is, and whoever wrote it does.
func NameFields(path, src, key string, names map[int]string) (Result, error) {
	seen := map[string]bool{}
	for _, n := range names {
		if !IsName(n) {
			return Result{Src: src}, fmt.Errorf("%q is not a name", n)
		}
		if seen[n] {
			return Result{Src: src}, fmt.Errorf("two fields cannot both be %q", n)
		}
		seen[n] = true
	}
	return applyOne(path, src, func(u *unit) ([]edit, string, error) {
		edits, summary, _, _ := u.destructure(map[string]map[int]string{key: names})
		if len(edits) == 0 {
			return nil, "", fmt.Errorf("those names are already taken where the fields are read")
		}
		return edits, summary, nil
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// paramTypes is the parameter types a lambda was checked against, or nil when
// it never was (a Shikigami nobody calls) or was checked against different
// types at different calls.
func (u *unit) paramTypes(l *ast.Lambda) []*ir.Type {
	seen := u.lamTypes[l.Pos.Offset]
	if len(seen) == 0 {
		return nil
	}
	for _, ts := range seen {
		if len(ts) != len(l.Params) {
			return nil
		}
		for i, t := range ts {
			if t == nil || t.String() != seen[0][i].String() {
				return nil
			}
		}
	}
	return seen[0]
}

// tupleCandidate decides whether parameter i can be destructured at all,
// leaving the question of names for later.
func (u *unit) tupleCandidate(site lambdaSite, lt lamToks, facts lamFacts, i int, p string, typ *ir.Type) *tupleParam {
	if strings.HasPrefix(p, "$") || p == "_" || len(typ.Elems) < 2 {
		return nil
	}
	// The parameter must be used for nothing but its fields: passed whole to
	// anything, it has to keep its name. Rebound or assigned, the same
	// spelling means two things and the token rewrite cannot tell them apart.
	if facts.letCount[p] > 0 || facts.assigned[p] {
		return nil
	}
	c := &tupleParam{site: site, lt: lt, facts: facts, index: i, name: p, typ: typ,
		used: map[int]bool{}, names: map[int]string{}, naming: map[int]*ast.LetExpr{}}

	uses := 0
	walkExpr(site.lam.Body, func(e ast.Expr) {
		if k, ok := itemIndex(e, p); ok {
			uses++
			c.used[k] = true
		}
	})
	if uses == 0 || uses != facts.idents[p] {
		return nil
	}
	for k := range c.used {
		if k < 0 || k >= len(typ.Elems) {
			return nil
		}
	}
	// The tokens must agree with the tree about how many there are, or an
	// `item(` written some way the matcher does not see would be left behind
	// reading a parameter that no longer exists.
	inTokens := 0
	for j := range lt.body {
		if _, ok := u.itemAt(lt.body, j, p); ok {
			inTokens++
		}
	}
	if inTokens != uses {
		return nil
	}

	bound := map[string]bool{}
	for _, n := range site.lam.BoundNames() {
		bound[n] = true
	}
	// Only a consider that opens the lambda's value may be removed. `consider`
	// is a primary expression whose body runs as far right as it can, so one
	// sitting as the right operand of an operator — `a - consider b as … in
	// b + 1` — is holding `b + 1` together, and deleting it would regroup the
	// arithmetic into `a - b + 1` without changing a single type.
	opening := map[*ast.LetExpr]bool{}
	head := site.lam.Body
	if also, ok := head.(*ast.AlsoExpr); ok {
		head = also.Body
	}
	for {
		let, ok := head.(*ast.LetExpr)
		if !ok {
			break
		}
		opening[let] = true
		head = let.Body
	}
	for _, let := range facts.lets {
		if let.Pattern != "" || !opening[let] {
			continue
		}
		k, ok := itemIndex(let.Value, p)
		if !ok {
			continue
		}
		if _, have := c.names[k]; have {
			continue
		}
		n := let.Name
		// The name is about to be in scope for the whole lambda rather than
		// for the consider's body, so it must not mean anything out there.
		if n == "_" || bound[n] || facts.assigned[n] || facts.letCount[n] != 1 ||
			facts.idents[n] != countIdent(let.Body, n) {
			continue
		}
		if _, _, ok := u.considerSpan(lt.body, n, p, k); !ok {
			continue
		}
		c.names[k] = n
		c.naming[k] = let
	}
	return c
}

// considerSpan finds `consider NAME as item(p, K) in` in the body tokens,
// returning the byte range from `consider` through `in`. It must occur exactly
// once.
func (u *unit) considerSpan(body []int, name, p string, k int) (start, end int, ok bool) {
	found := 0
	for j := 0; j+10 <= len(body); j++ {
		t := func(n int) token.Token { return u.toks[body[j+n]] }
		if t(0).Kind != token.IDENT || !strings.EqualFold(t(0).Literal, "consider") ||
			t(1).Kind != token.IDENT || t(1).Literal != name ||
			t(2).Kind != token.IDENT || !strings.EqualFold(t(2).Literal, "as") {
			continue
		}
		if kk, isItem := u.itemAt(body, j+3, p); !isItem || kk != k {
			continue
		}
		if t(9).Kind != token.IDENT || !strings.EqualFold(t(9).Literal, "in") {
			continue
		}
		start, end = t(0).Pos.Offset, t(9).End
		found++
	}
	return start, end, found == 1
}

// destructureEdits is the rewrite itself: the parameter becomes a pattern, the
// considers that named its fields are removed, and every remaining
// `item(p, k)` becomes the field's name.
func (u *unit) destructureEdits(c *tupleParam, names map[int]string) ([]edit, bool) {
	fields := make([]string, len(c.typ.Elems))
	for k := range fields {
		fields[k] = "_"
		if n, ok := names[k]; ok {
			fields[k] = n
		}
	}
	pr := c.lt.params[c.index]
	es := []edit{{
		start: u.toks[pr[0]].Pos.Offset, end: u.toks[pr[1]-1].End,
		text: "(" + strings.Join(fields, ", ") + ")",
	}}

	var gone [][2]int
	for k, let := range c.naming {
		if names[k] != let.Name {
			continue
		}
		start, end, ok := u.considerSpan(c.lt.body, let.Name, c.name, k)
		if !ok {
			return nil, false
		}
		gone = append(gone, [2]int{start, end})
		es = append(es, u.removeSpan(start, end))
	}

	body := c.lt.body
	for j := range body {
		k, ok := u.itemAt(body, j, c.name)
		if !ok {
			continue
		}
		start, end := u.toks[body[j]].Pos.Offset, u.toks[body[j+5]].End
		inside := false
		for _, g := range gone {
			if start >= g[0] && end <= g[1] {
				inside = true
			}
		}
		if inside {
			continue
		}
		es = append(es, edit{start: start, end: end, text: names[k]})
	}
	return es, true
}

// removeSpan deletes src[start:end] and tidies what it leaves: the spaces
// after it, and the whole line when nothing else was on it.
func (u *unit) removeSpan(start, end int) edit {
	src := u.src
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	atEOL := end == len(src) || src[end] == '\n'
	ls := lineStart(src, start)
	if atEOL {
		if strings.TrimSpace(src[ls:start]) == "" {
			// The line held nothing else. Take it, newline and all.
			if end < len(src) {
				end++
			}
			return edit{start: ls, end: end}
		}
		// Something precedes it on the line; do not leave a trailing space.
		for start > ls && (src[start-1] == ' ' || src[start-1] == '\t') {
			start--
		}
	}
	return edit{start: start, end: end}
}
