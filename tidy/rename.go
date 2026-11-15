// Renaming.
//
// A name in Domain is one of a few things, and each has a scope the parser
// already knows: a lambda's parameter or `consider` local lives in its lambda;
// a `Consider` binding lives on its statement; a Shikigami's parameter lives
// in its definition and at every call that passes it; a `Cursed Object` global
// and a Shikigami's own name live in the whole file.
//
// Rename finds which of those the cursor is on and rewrites exactly that
// scope. It is deliberately more careful than it needs to be about capture:
// the new name must not already be written anywhere in the scope, even where
// it would turn out to be harmless, because "this rename silently changed
// which x that x is" is the one failure a rename tool is not forgiven for. As
// everywhere in this package, the result must still check.
package tidy

import (
	"fmt"
	"sort"
	"strings"

	"domain/ast"
	"domain/token"
)

// RenameTarget is the name under the cursor, as far as renaming is concerned.
type RenameTarget struct {
	Name string
	Kind string
	// Uses is how many places would change.
	Uses int
}

type byteRange struct{ from, to int }

func (r byteRange) has(off int) bool { return off >= r.from && off < r.to }

// renamePlan is every piece of text that spells the name.
type renamePlan struct {
	name, kind string
	scope      byteRange
	idents     []int       // token indices
	phrases    []byteRange // a Shikigami's name, where it is called
	defName    int         // the STRING token of a Shikigami definition, or -1
}

// FindRename reports what Rename would rename at a position. line is 1-based
// and col a 0-based byte column.
func FindRename(path, src string, line, col int) (RenameTarget, bool) {
	u, err := load(path, src)
	if err != nil {
		return RenameTarget{}, false
	}
	p, err := u.renamePlan(lineOffset(src, line) + col)
	if err != nil {
		return RenameTarget{}, false
	}
	n := len(p.idents) + len(p.phrases)
	if p.defName >= 0 {
		n++
	}
	return RenameTarget{Name: p.name, Kind: p.kind, Uses: n}, true
}

// Rename renames the name at a position to newName throughout its scope.
func Rename(path, src string, line, col int, newName string) (Result, error) {
	newName = strings.TrimSpace(newName)
	return applyOne(path, src, func(u *unit) ([]edit, string, error) {
		p, err := u.renamePlan(lineOffset(src, line) + col)
		if err != nil {
			return nil, "", err
		}
		if newName == p.name {
			return nil, "", fmt.Errorf("that is already its name")
		}
		var edits []edit
		if p.defName >= 0 {
			if newName == "" || strings.ContainsAny(newName, "\"\n") {
				return nil, "", fmt.Errorf("a Shikigami needs a name")
			}
			t := u.toks[p.defName]
			edits = append(edits, edit{start: t.Pos.Offset, end: t.End, text: `"` + newName + `"`})
			for _, r := range p.phrases {
				edits = append(edits, edit{start: r.from, end: r.to, text: newName})
			}
		} else {
			if !IsName(newName) {
				return nil, "", fmt.Errorf("%q is not a name", newName)
			}
			for i, t := range u.toks {
				if t.Kind == token.IDENT && t.Literal == newName && p.scope.has(t.Pos.Offset) &&
					!(i > 0 && u.toks[i-1].Kind == token.DOT) {
					return nil, "", fmt.Errorf("%s is already a name here (line %d)", newName, t.Pos.Line)
				}
			}
			for _, i := range p.idents {
				edits = append(edits, edit{start: u.toks[i].Pos.Offset, end: u.toks[i].End, text: newName})
			}
		}
		return edits, fmt.Sprintf("renamed %s to %s in %s", p.name, newName, plural(len(edits), "place", "places")), nil
	})
}

// allStatements visits every statement, including the ones behind an `Of`.
func (u *unit) allStatements(f func(s *ast.Statement)) {
	var walk func(ss []*ast.Statement)
	walk = func(ss []*ast.Statement) {
		for _, s := range ss {
			if s == nil {
				continue
			}
			f(s)
			walk(s.Block)
			for _, b := range s.Binds {
				walk(b.Body)
			}
			for _, b := range s.Decls {
				walk(b.Body)
			}
		}
	}
	for _, d := range u.prog.Shikigamis {
		for _, b := range d.Binds {
			walk(b.Body)
		}
		walk(d.Body)
	}
	walk(u.prog.Statements)
}

// extents is the byte range of every statement that stands on its own line,
// and of every definition.
func (u *unit) extents() (stmts map[*ast.Statement]byteRange, defs map[*ast.ShikigamiDef]byteRange) {
	stmts, defs = map[*ast.Statement]byteRange{}, map[*ast.ShikigamiDef]byteRange{}
	starts := u.topLevelStarts()
	var walk func(ss []*ast.Statement, end int)
	walk = func(ss []*ast.Statement, end int) {
		for i, s := range ss {
			to := end
			if i+1 < len(ss) {
				to = ss[i+1].Pos.Offset
			}
			stmts[s] = byteRange{s.Pos.Offset, to}
			walk(s.Block, to)
		}
	}
	for _, s := range u.prog.Statements {
		walk([]*ast.Statement{s}, nextStart(starts, s.Pos.Offset))
	}
	for _, d := range u.prog.Shikigamis {
		r := byteRange{d.Pos.Offset, nextStart(starts, d.Pos.Offset)}
		defs[d] = r
		walk(d.Body, r.to)
	}
	return stmts, defs
}

// lambdaRange is the bytes of a lambda, parameter list included.
func (u *unit) lambdaRange(l *ast.Lambda) (byteRange, bool) {
	lt := u.lambdaTokens(l)
	first, ok := u.tokAt[l.Pos.Offset]
	if !lt.ok || !ok || len(lt.body) == 0 {
		return byteRange{}, false
	}
	return byteRange{u.toks[first].Pos.Offset, u.toks[lt.body[len(lt.body)-1]].End}, true
}

// localIn reports how a lambda binds name, if it does: as a parameter, as a
// `consider` whose every read is inside its own body, or ambiguously.
func localIn(l *ast.Lambda, name string) (binds bool, kind string, err error) {
	facts := factsOf(l)
	for _, n := range l.BoundNames() {
		if n == name {
			if facts.letCount[name] > 0 {
				return true, "", fmt.Errorf("%s is both a parameter and a local on line %d", name, l.Pos.Line)
			}
			return true, "lambda parameter", nil
		}
	}
	userLets := 0
	var let *ast.LetExpr
	for _, x := range facts.lets {
		if x.Name == name && x.Pattern == "" {
			userLets++
			let = x
		}
	}
	if userLets == 0 {
		return false, "", nil
	}
	if userLets > 1 || facts.idents[name] != countIdent(let.Body, name) {
		return true, "", fmt.Errorf("%s means two things in the lambda on line %d", name, l.Pos.Line)
	}
	return true, "local", nil
}

func (u *unit) renamePlan(off int) (*renamePlan, error) {
	// The token under the cursor, or the one it has just passed.
	ti := -1
	for i, t := range u.toks {
		if (t.Kind == token.IDENT || t.Kind == token.STRING) && off >= t.Pos.Offset && off <= t.End {
			ti = i
			if off < t.End {
				break
			}
		}
	}
	if ti < 0 {
		return nil, fmt.Errorf("there is no name here")
	}
	tok := u.toks[ti]
	stmtExt, defExt := u.extents()

	// A Shikigami's own name: on its definition, or on a call.
	defByName := func(name string) *ast.ShikigamiDef {
		for _, d := range u.prog.Shikigamis {
			if strings.EqualFold(d.Name, strings.TrimSpace(name)) {
				return d
			}
		}
		return nil
	}
	var def *ast.ShikigamiDef
	for _, d := range u.prog.Shikigamis {
		if tok.Kind == token.STRING && tok.Pos.Line == d.Pos.Line && tok.Literal == d.Name {
			def = d
		}
	}
	callName := ""
	u.allStatements(func(s *ast.Statement) {
		if s.Keyword != "Shikigami" || s.Op == nil {
			return
		}
		from := s.Op.Pos.Offset
		if off >= from && off <= from+len(s.Op.Raw) {
			callName = s.Op.Raw
		}
	})
	if def == nil && callName != "" {
		if def = defByName(callName); def == nil {
			return nil, fmt.Errorf("%s is defined in another file", strings.TrimSpace(callName))
		}
	}
	if def != nil {
		p := &renamePlan{name: def.Name, kind: "Shikigami", defName: -1}
		first, _ := u.tokAt[def.Pos.Offset]
		for i := first; i < len(u.toks) && u.toks[i].Pos.Line == def.Pos.Line; i++ {
			if u.toks[i].Kind == token.STRING && u.toks[i].Literal == def.Name {
				p.defName = i
				break
			}
		}
		if p.defName < 0 {
			return nil, fmt.Errorf("could not find where %s is defined", def.Name)
		}
		bad := false
		u.allStatements(func(s *ast.Statement) {
			if s.Keyword != "Shikigami" || s.Op == nil || !strings.EqualFold(strings.TrimSpace(s.Op.Raw), def.Name) {
				return
			}
			from := s.Op.Pos.Offset
			lead := len(s.Op.Raw) - len(strings.TrimLeft(s.Op.Raw, " "))
			text := strings.TrimSpace(s.Op.Raw)
			if from+lead+len(text) > len(u.src) || u.src[from+lead:from+lead+len(text)] != text {
				bad = true
				return
			}
			p.phrases = append(p.phrases, byteRange{from + lead, from + lead + len(text)})
		})
		if bad {
			return nil, fmt.Errorf("could not find every call of %s", def.Name)
		}
		return p, nil
	}

	if tok.Kind != token.IDENT || ti > 0 && u.toks[ti-1].Kind == token.DOT {
		return nil, fmt.Errorf("there is no name here that can be renamed")
	}
	name := tok.Literal
	p := &renamePlan{name: name, defName: -1}
	var skip []byteRange
	var extra []int // tokens outside the usual rule: a parameter's declaration, its call-site labels

	sites := u.lambdas()
	// 1. the lambda the cursor is in
	for _, site := range sites {
		r, ok := u.lambdaRange(site.lam)
		if !ok || !(off >= r.from && off <= r.to) {
			continue
		}
		binds, kind, err := localIn(site.lam, name)
		if err != nil {
			return nil, err
		}
		if binds {
			p.kind, p.scope = kind, r
		}
	}

	// 2. a `Consider` on a statement the cursor is inside, innermost first
	if p.kind == "" {
		best := byteRange{0, len(u.src) + 1}
		found := false
		consider := func(binds []*ast.Binding, r byteRange) {
			if !(off >= r.from && off <= r.to) || r.to-r.from >= best.to-best.from {
				return
			}
			for _, b := range binds {
				if b.Name == name {
					best, found = r, true
				}
			}
		}
		for s, r := range stmtExt {
			consider(s.Binds, r)
		}
		for d, r := range defExt {
			consider(d.Binds, r)
		}
		if found {
			p.kind, p.scope = "binding", best
		}
	}

	// 3. a Shikigami parameter: inside the definition, or on a call's label
	if p.kind == "" {
		var owner *ast.ShikigamiDef
		for d, r := range defExt {
			if !r.has(off) {
				continue
			}
			for _, prm := range d.Params {
				if prm.Name == name {
					owner = d
				}
			}
		}
		u.allStatements(func(s *ast.Statement) {
			if s.Keyword != "Shikigami" || s.Op == nil {
				return
			}
			for _, a := range s.Args {
				if a.Pos.Offset == tok.Pos.Offset && a.Name == name {
					if d := defByName(s.Op.Raw); d != nil {
						for _, prm := range d.Params {
							if prm.Name == name {
								owner = d
							}
						}
					}
				}
			}
		})
		if owner != nil {
			p.kind, p.scope = "Shikigami parameter", defExt[owner]
			// The declaration `(name: Type)` is a name before a colon, which
			// everywhere else is a label rather than a use.
			first := u.tokAt[owner.Pos.Offset]
			for i := first; i < len(u.toks) && u.toks[i].Pos.Line == owner.Pos.Line; i++ {
				if u.toks[i].Kind == token.IDENT && u.toks[i].Literal == name && u.toks[i+1].Kind == token.COLON {
					extra = append(extra, i)
				}
			}
			u.allStatements(func(s *ast.Statement) {
				if s.Keyword != "Shikigami" || s.Op == nil || !strings.EqualFold(strings.TrimSpace(s.Op.Raw), owner.Name) {
					return
				}
				for _, a := range s.Args {
					if i, ok := u.tokAt[a.Pos.Offset]; ok && a.Name == name && u.toks[i].Literal == name {
						extra = append(extra, i)
					}
				}
			})
		}
	}

	// 4. a global
	if p.kind == "" {
		declared := false
		u.allStatements(func(s *ast.Statement) {
			if s.Keyword != "Cursed Object" {
				return
			}
			for _, d := range s.Decls {
				if d.Name == name {
					declared = true
				}
			}
		})
		if !declared {
			return nil, fmt.Errorf("%s is not a name this program declares", name)
		}
		p.kind, p.scope = "global", byteRange{0, len(u.src)}
		for d, r := range defExt {
			for _, prm := range d.Params {
				if prm.Name == name {
					skip = append(skip, r)
				}
			}
			for _, b := range d.Binds {
				if b.Name == name {
					skip = append(skip, r)
				}
			}
		}
	}

	// Inside the scope, anything that rebinds the name owns its own uses.
	if p.kind != "lambda parameter" && p.kind != "local" {
		for _, site := range sites {
			r, ok := u.lambdaRange(site.lam)
			if !ok || !p.scope.has(r.from) {
				continue
			}
			binds, _, err := localIn(site.lam, name)
			if err != nil {
				return nil, err
			}
			if binds {
				skip = append(skip, r)
			}
		}
		for s, r := range stmtExt {
			if r == p.scope || !p.scope.has(r.from) {
				continue
			}
			for _, b := range s.Binds {
				if b.Name == name {
					skip = append(skip, r)
				}
			}
		}
	}

	for i, t := range u.toks {
		if t.Kind != token.IDENT || t.Literal != name || !(t.Pos.Offset >= p.scope.from && t.Pos.Offset <= p.scope.to) {
			continue
		}
		if i > 0 && u.toks[i-1].Kind == token.DOT || u.toks[i+1].Kind == token.COLON {
			continue // a field, or a label
		}
		skipped := false
		for _, r := range skip {
			if r.has(t.Pos.Offset) {
				skipped = true
			}
		}
		if !skipped {
			p.idents = append(p.idents, i)
		}
	}
	p.idents = append(p.idents, extra...)
	sort.Ints(p.idents)
	if len(p.idents) == 0 {
		return nil, fmt.Errorf("could not find where %s is used", name)
	}
	return p, nil
}
