// Carrying a counter in a loop's own state instead of writing a global.
//
//	Cursed Object: laps As 0
//	Simple Domain: While
//	    Using: (v) -> v > 1
//	    Cursed Technique: Apply
//	        Using: (n) -> (n / 2) also laps := laps + 1
//
// becomes
//
//	Cursed Technique: Apply
//	    Using: (v) -> tuple(v, 0)
//	Simple Domain: While
//	    Using: ((v, laps)) -> v > 1
//	    Cursed Technique: Apply
//	        Using: ((n, laps)) -> tuple((n / 2), laps + 1)
//	Cursed Object: laps Of ((_, c)) -> c
//	Cursed Technique: Apply
//	    Using: ((v, _)) -> v
//
// Afterwards `laps` is declared once and never written, so it is a constant
// of the run and every stage that reads it keeps its optimizer rewrites.
//
// This is a redesign of the loop, which is why it is asked for rather than
// applied on save, and why it only takes the one shape it can be sure of: a
// `While` or `Repeat` at the top level whose body is nothing but `Apply`
// stages, each writing the global at most once, as the last thing its lambda
// does. Any other statement in the body sees one element at a time, or a
// channel, and has nowhere to carry a second value.
package tidy

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/token"
)

// LoopCounter is a global that could be carried in its loop instead.
type LoopCounter struct {
	Name string
	// Line is the loop's line, and Loop its first line as written.
	Line int
	Loop string
}

// carry is the plan behind a LoopCounter.
type carry struct {
	name     string
	declLine int
	init     string
	loop     *ast.Statement
	loopEnd  int // last line of the loop
	lams     []*ast.Lambda
}

// FindLoopCounters lists the written globals that have the shape CarryInLoop
// can rewrite.
func FindLoopCounters(path, src string) []LoopCounter {
	u, err := load(path, src)
	if err != nil {
		return nil
	}
	var out []LoopCounter
	seen := map[string]bool{}
	for _, s := range u.prog.Statements {
		if s.Keyword != "Cursed Object" {
			continue
		}
		for _, d := range s.Decls {
			if seen[d.Name] {
				continue
			}
			seen[d.Name] = true
			c, err := u.carryPlan(d.Name)
			if err == nil {
				_, err = u.carryEdits(c)
			}
			if err == nil {
				out = append(out, LoopCounter{
					Name: d.Name, Line: c.loop.Pos.Line,
					Loop: strings.TrimSpace(u.src[c.loop.Pos.Offset:lineEnd(u.src, c.loop.Pos.Offset)]),
				})
			}
		}
	}
	return out
}

// CarryInLoop rewrites the loop that writes the global name so that it
// carries the value in its own state.
func CarryInLoop(path, src, name string) (Result, error) {
	return applyOne(path, src, func(u *unit) ([]edit, string, error) {
		c, err := u.carryPlan(name)
		if err != nil {
			return nil, "", err
		}
		edits, err := u.carryEdits(c)
		if err != nil {
			return nil, "", err
		}
		return edits, fmt.Sprintf("carried %s in the loop's state", name), nil
	})
}

func (u *unit) carryPlan(name string) (*carry, error) {
	no := func(format string, args ...any) (*carry, error) {
		return nil, fmt.Errorf(format, args...)
	}
	starts := u.topLevelStarts()

	// One declaration, on a line of its own at the top level, with a plain
	// value: that value is about to be written into a lambda.
	var declStmt *ast.Statement
	var decl *ast.Binding
	count, tools := 0, 0
	top := map[*ast.Statement]bool{}
	for _, s := range u.prog.Statements {
		top[s] = true
	}
	u.eachStatement(func(s *ast.Statement, def *ast.ShikigamiDef) {
		for _, d := range s.Decls {
			if d.Name != name {
				continue
			}
			if s.Keyword == "Cursed Tool" {
				tools++
				continue
			}
			count++
			if top[s] && def == nil {
				declStmt, decl = s, d
			}
		}
	})
	if count != 1 || decl == nil {
		return no("%s is not declared exactly once at the top level", name)
	}
	if tools > 0 {
		return no("%s is written by a Cursed Tool statement", name)
	}
	f, l := u.tokRange(declStmt.Pos.Offset, nextStart(starts, declStmt.Pos.Offset))
	if len(declStmt.Decls) != 1 || decl.Of || decl.Value == nil || u.lastLine(f, l) != declStmt.Pos.Line {
		return no("%s is not declared on one line with a plain value", name)
	}
	ls := lineStart(u.src, declStmt.Pos.Offset)
	if token.CommentStart(u.src[ls:lineEnd(u.src, ls)]) >= 0 {
		return no("the declaration of %s carries a comment", name)
	}
	c := &carry{name: name, declLine: declStmt.Pos.Line}
	for i := f; i < l; i++ {
		if u.toks[i].Kind == token.IDENT && strings.EqualFold(u.toks[i].Literal, "As") && i > f && u.toks[i-1].Literal == name {
			c.init = strings.TrimSpace(u.src[u.toks[i+1].Pos.Offset:u.toks[l-1].End])
		}
	}
	if c.init == "" {
		return no("could not read the value %s starts with", name)
	}

	// Every write is a `:=`, and they are all in one loop.
	var writes []int
	for i, t := range u.toks {
		if t.Kind == token.ASSIGN && i > 0 && u.toks[i-1].Kind == token.IDENT && u.toks[i-1].Literal == name {
			writes = append(writes, t.Pos.Offset)
		}
	}
	if len(writes) == 0 {
		return no("nothing writes %s", name)
	}
	for _, s := range u.prog.Statements {
		end := nextStart(starts, s.Pos.Offset)
		if s.Pos.Offset < writes[0] && writes[0] < end {
			c.loop = s
		}
	}
	if c.loop == nil || c.loop.Keyword != "Simple Domain" || c.loop.Op == nil || len(c.loop.Op.Words) == 0 ||
		c.loop.Pos.Offset < declStmt.Pos.Offset {
		return no("%s is not written by a top-level loop", name)
	}
	kind := strings.ToLower(c.loop.Op.Words[0])
	if kind != "while" && kind != "repeat" {
		return no("only a While or a Repeat can carry a value")
	}
	loopFrom, loopTo := c.loop.Pos.Offset, nextStart(starts, c.loop.Pos.Offset)
	lf, ll := u.tokRange(loopFrom, loopTo)
	c.loopEnd = u.lastLine(lf, ll)
	for _, w := range writes {
		if w < loopFrom || w >= loopTo {
			return no("%s is written in more than one place", name)
		}
	}

	// The loop is its test and a body of Apply stages, nothing else.
	if len(c.loop.Binds) > 0 || len(c.loop.Decls) > 0 {
		return no("the loop has bindings of its own")
	}
	for _, a := range c.loop.Args {
		la, ok := a.Value.(ast.LambdaArg)
		if !ok || !strings.EqualFold(a.Name, "Using") || kind != "while" {
			return no("the loop takes an argument this cannot carry a value past")
		}
		c.lams = append(c.lams, la.Lambda)
	}
	if len(c.loop.Block) == 0 {
		return no("the loop has no body")
	}
	for _, s := range c.loop.Block {
		if s.Keyword != "Cursed Technique" || s.Op == nil || !strings.EqualFold(strings.TrimSpace(s.Op.Raw), "Apply") ||
			len(s.Args) != 1 || len(s.Binds) > 0 || len(s.Block) > 0 || len(s.Decls) > 0 {
			return no("line %d is not a plain Apply, and only an Apply sees the whole value", s.Pos.Line)
		}
		la, ok := s.Args[0].Value.(ast.LambdaArg)
		if !ok || !strings.EqualFold(s.Args[0].Name, "Using") {
			return no("line %d is not a plain Apply", s.Pos.Line)
		}
		c.lams = append(c.lams, la.Lambda)
	}

	// Each lambda takes one parameter, and the name is free inside it.
	type span struct{ from, to int }
	var covered []span
	for _, l := range c.lams {
		lt := u.lambdaTokens(l)
		first, ok := u.tokAt[l.Pos.Offset]
		if !lt.ok || !ok || len(lt.params) != 1 || len(lt.body) == 0 {
			return no("the lambda on line %d does not take exactly one parameter", l.Pos.Line)
		}
		facts := factsOf(l)
		if facts.letCount[name] > 0 {
			return no("the lambda on line %d has a local called %s", l.Pos.Line, name)
		}
		for _, n := range l.BoundNames() {
			if n == name {
				return no("the lambda on line %d has a parameter called %s", l.Pos.Line, name)
			}
		}
		covered = append(covered, span{u.toks[first].Pos.Offset, u.toks[lt.body[len(lt.body)-1]].End})
	}

	// Nothing else may read it before the loop has finished: not between the
	// declaration and the loop, not the loop's own phrase, not a Shikigami.
	declEnd := lineEnd(u.src, declStmt.Pos.Offset)
	var defs []span
	for _, d := range u.prog.Shikigamis {
		defs = append(defs, span{d.Pos.Offset, nextStart(starts, d.Pos.Offset)})
	}
	for i, t := range u.toks {
		if t.Kind != token.IDENT || t.Literal != name || i > 0 && u.toks[i-1].Kind == token.DOT {
			continue
		}
		off := t.Pos.Offset
		for _, d := range defs {
			if off >= d.from && off < d.to {
				return no("a Shikigami reads %s", name)
			}
		}
		if off < declEnd || off >= loopTo {
			continue
		}
		inLambda := false
		for _, s := range covered {
			if off >= s.from && off < s.to {
				inLambda = true
			}
		}
		if !inLambda {
			return no("%s is read on line %d, before the loop has finished", name, t.Pos.Line)
		}
	}
	return c, nil
}

func (u *unit) carryEdits(c *carry) ([]edit, error) {
	var edits []edit
	name := c.name
	for li, l := range c.lams {
		lt := u.lambdaTokens(l)
		pr := lt.params[0]
		pFrom, pTo := u.toks[pr[0]].Pos.Offset, u.toks[pr[1]-1].End
		edits = append(edits, edit{start: pFrom, end: pTo, text: "(" + u.src[pFrom:pTo] + ", " + name + ")"})

		isTest := li == 0 && len(c.lams) > len(c.loop.Block)
		assigns := 0
		walkExpr(l.Body, func(e ast.Expr) {
			if a, ok := e.(*ast.AssignExpr); ok && a.Name == name {
				assigns++
			}
		})
		if isTest {
			if assigns > 0 {
				return nil, fmt.Errorf("the loop's test writes %s", name)
			}
			continue
		}

		// Past the considers that open the body is the value the lambda
		// returns; that is what becomes the tuple's first field.
		lets := 0
		tail := l.Body
		for {
			let, ok := tail.(*ast.LetExpr)
			if !ok {
				break
			}
			if let.Pattern == "" {
				lets++
			}
			tail = let.Body
		}
		writes := false
		if also, ok := tail.(*ast.AlsoExpr); ok {
			if len(also.Clauses) != 1 {
				return nil, fmt.Errorf("the lambda on line %d does more than one thing after `also`", l.Pos.Line)
			}
			a, ok := also.Clauses[0].(*ast.AssignExpr)
			if !ok || a.Name != name {
				return nil, fmt.Errorf("the lambda on line %d writes something other than %s", l.Pos.Line, name)
			}
			writes = true
		}
		if want := map[bool]int{true: 1, false: 0}[writes]; assigns != want {
			return nil, fmt.Errorf("the lambda on line %d writes %s somewhere other than its last step", l.Pos.Line, name)
		}

		body := lt.body
		tailAt, depth, open, closed := 0, 0, 0, 0
		alsoAt := -1
		for j, bi := range body {
			t := u.toks[bi]
			switch t.Kind {
			case token.LPAREN, token.LBRACE:
				depth++
			case token.RPAREN, token.RBRACE:
				depth--
			case token.IDENT:
				if depth != 0 {
					continue
				}
				switch {
				case closed < lets && strings.EqualFold(t.Literal, "consider"):
					open++
				case closed < lets && strings.EqualFold(t.Literal, "in") && open > 0:
					open--
					if open == 0 {
						closed++
						tailAt = j + 1
					}
				case closed == lets && strings.EqualFold(t.Literal, "also"):
					if alsoAt >= 0 {
						return nil, fmt.Errorf("the lambda on line %d has more than one `also`", l.Pos.Line)
					}
					alsoAt = j
				}
			}
		}
		if closed != lets || tailAt >= len(body) {
			return nil, fmt.Errorf("could not find what the lambda on line %d returns", l.Pos.Line)
		}
		last := u.toks[body[len(body)-1]]
		edits = append(edits, edit{start: u.toks[body[tailAt]].Pos.Offset, end: u.toks[body[tailAt]].Pos.Offset, text: "tuple("})
		if !writes {
			edits = append(edits, edit{start: last.End, end: last.End, text: ", " + name + ")"})
			continue
		}
		if alsoAt <= tailAt || alsoAt+3 >= len(body) ||
			u.toks[body[alsoAt+1]].Literal != name || u.toks[body[alsoAt+2]].Kind != token.ASSIGN {
			return nil, fmt.Errorf("could not find the write on line %d", l.Pos.Line)
		}
		edits = append(edits,
			edit{start: u.toks[body[alsoAt-1]].End, end: u.toks[body[alsoAt+3]].Pos.Offset, text: ", "},
			edit{start: last.End, end: last.End, text: ")"})
	}

	// A parameter name for the two wrappers that the starting value does not
	// already use.
	v := "v"
	for _, cand := range []string{"v", "s", "state"} {
		v = cand
		if !containsWord(c.init, cand) && cand != name {
			break
		}
	}
	field := "c"
	if name == "c" {
		field = "k"
	}
	loopAt := lineOffset(u.src, c.loop.Pos.Line)
	after := lineOffset(u.src, c.loopEnd+1)
	post := fmt.Sprintf("Cursed Object: %s Of ((_, %s)) -> %s\nCursed Technique: Apply\n    Using: ((%s, _)) -> %s\n",
		name, field, field, v, v)
	if after == len(u.src) && !strings.HasSuffix(u.src, "\n") {
		post = "\n" + strings.TrimSuffix(post, "\n")
	}
	edits = append(edits,
		deleteLines(u.src, c.declLine, c.declLine),
		edit{start: loopAt, end: loopAt, text: fmt.Sprintf("Cursed Technique: Apply\n    Using: (%s) -> tuple(%s, %s)\n", v, v, c.init)},
		edit{start: after, end: after, text: post})
	return edits, nil
}

// containsWord reports whether word appears in s as a whole identifier.
func containsWord(s, word string) bool {
	for i := 0; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] != word {
			continue
		}
		if (i == 0 || !token.IsIdentByte(s[i-1])) && (i+len(word) == len(s) || !token.IsIdentByte(s[i+len(word)])) {
			return true
		}
	}
	return false
}
