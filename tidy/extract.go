// Extracting what two Parts share into a Shikigami.
//
// Hoisting handles Parts that begin the same way. This handles Parts that
// *are* the same, except for one thing:
//
//	Part "1":                              Shikigami "Jumps" (rule: (Int) -> Int)
//	    While                                  While
//	        Using: …                               Using: …
//	        Apply                                  Apply
//	            Using: (j) -> j + 1                    Using: rule
//	    Reveal: stdout
//	Part "2":                          →   Part "1":
//	    While                                  Shikigami: Jumps
//	        Using: …                               rule: (j) -> j + 1
//	        Apply                              Reveal: stdout
//	            Using: (j) -> if j > 2 …   Part "2":
//	    Reveal: stdout                         Shikigami: Jumps
//	                                               rule: (j) -> if j > 2 …
//	                                           Reveal: stdout
//
// The one thing is either a literal — a number or a string, which becomes an
// `Int` or `Text` parameter — or a whole lambda, which becomes a function
// parameter whose type is read off the type checker.
//
// It is not applied on save, because it needs two names nobody has written
// yet: the Shikigami's and its parameter's. The editor asks for them.
package tidy

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/token"
)

// SharedParts describes an extraction that is available.
type SharedParts struct {
	// Labels are the Parts involved, as written.
	Labels []string
	// Line is where the shared statements begin in the first of them.
	Line int
	// Statements is how many each Part would hand to the Shikigami.
	Statements int
	// ParamType is the type of the one thing that differs, and DefaultParam a
	// reasonable name for it.
	ParamType    string
	DefaultParam string
}

// share is the plan behind a SharedParts.
type share struct {
	parts    []*ast.Statement
	from, to int // the run of body statements, [from, to)
	// span is, per Part, the byte range of the thing that differs.
	span      [][2]int
	paramType string
	isLambda  bool
}

// FindSharedParts reports whether the program's Parts are one computation
// written out once per Part.
func FindSharedParts(path, src string) (SharedParts, bool) {
	u, err := load(path, src)
	if err != nil {
		return SharedParts{}, false
	}
	sh := u.sharedParts()
	if sh == nil {
		return SharedParts{}, false
	}
	out := SharedParts{
		Line: sh.parts[0].Block[sh.from].Pos.Line, Statements: sh.to - sh.from,
		ParamType: sh.paramType, DefaultParam: "n",
	}
	switch {
	case sh.isLambda:
		out.DefaultParam = "rule"
	case sh.paramType == "Text":
		out.DefaultParam = "s"
	}
	for _, p := range sh.parts {
		out.Labels = append(out.Labels, p.PartDescription())
	}
	return out, true
}

// ExtractShikigami moves the shared statements into a Shikigami called name,
// taking the difference as a parameter called param.
func ExtractShikigami(path, src, name, param string) (Result, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "\"\n") {
		return Result{Src: src}, fmt.Errorf("a Shikigami needs a name")
	}
	if !IsName(param) {
		return Result{Src: src}, fmt.Errorf("%q is not a parameter name", param)
	}
	return applyOne(path, src, func(u *unit) ([]edit, string, error) {
		sh := u.sharedParts()
		if sh == nil {
			return nil, "", fmt.Errorf("the Parts no longer share a computation")
		}
		return u.extractEdits(sh, name, param)
	})
}

func (u *unit) sharedParts() *share {
	var parts []*ast.Statement
	for _, s := range u.prog.Statements {
		if s.Keyword == "Part" {
			parts = append(parts, s)
		}
	}
	if len(parts) < 2 {
		return nil
	}
	n := len(parts[0].Block)
	for _, p := range parts {
		if len(p.Block) != n || n == 0 {
			return nil
		}
	}

	// Where each statement of each body is in the token stream.
	starts := u.topLevelStarts()
	ranges := make([][][2]int, len(parts))
	for pi, p := range parts {
		end := nextStart(starts, p.Pos.Offset)
		for j, s := range p.Block {
			limit := end
			if j+1 < n {
				limit = p.Block[j+1].Pos.Offset
			}
			f, l := u.tokRange(s.Pos.Offset, limit)
			ranges[pi] = append(ranges[pi], [2]int{f, l})
		}
	}
	same := make([]bool, n)
	diff := -1
	for j := 0; j < n; j++ {
		same[j] = true
		for pi := 1; pi < len(parts); pi++ {
			if u.spell(ranges[pi][j][0], ranges[pi][j][1]) != u.spell(ranges[0][j][0], ranges[0][j][1]) {
				same[j] = false
			}
		}
		if !same[j] {
			if diff >= 0 {
				return nil // two differences are two parameters, and a different tool
			}
			diff = j
		}
	}
	if diff < 0 {
		return nil
	}

	sh := &share{parts: parts}
	if !u.literalDiff(sh, ranges, diff) && !u.lambdaDiff(sh, ranges, diff) {
		return nil
	}

	movable := func(j int) bool {
		s := parts[0].Block[j]
		return u.hoistable(s, u.spell(ranges[0][j][0], ranges[0][j][1])) && !readsChannel(s)
	}
	if !movable(diff) {
		return nil
	}
	sh.from, sh.to = diff, diff+1
	for sh.from > 0 && same[sh.from-1] && movable(sh.from-1) {
		sh.from--
	}
	for sh.to < n && same[sh.to] && movable(sh.to) {
		sh.to++
	}
	// One statement swapped for one call carrying the same lambda is not
	// sharing anything.
	if sh.to-sh.from < 2 {
		return nil
	}
	return sh
}

// readsChannel reports whether a statement reads a channel anywhere inside
// it, which a Shikigami body may not.
func readsChannel(s *ast.Statement) bool {
	for _, a := range s.Args {
		if strings.EqualFold(a.Name, "From") {
			return true
		}
	}
	for _, c := range s.Block {
		if readsChannel(c) {
			return true
		}
	}
	return false
}

// literalDiff recognises statements that differ in exactly one number or one
// string.
func (u *unit) literalDiff(sh *share, ranges [][][2]int, j int) bool {
	f0, l0 := ranges[0][j][0], ranges[0][j][1]
	at := -1
	for pi := 1; pi < len(ranges); pi++ {
		f, l := ranges[pi][j][0], ranges[pi][j][1]
		if l-f != l0-f0 {
			return false
		}
		for k := 0; k < l0-f0; k++ {
			a, b := u.toks[f0+k], u.toks[f+k]
			if a.Kind == b.Kind && a.Literal == b.Literal {
				continue
			}
			if at >= 0 && at != k {
				return false
			}
			at = k
		}
	}
	if at < 0 {
		return false
	}
	kind := u.toks[f0+at].Kind
	if kind != token.INT && kind != token.STRING {
		return false
	}
	for pi := range ranges {
		t := u.toks[ranges[pi][j][0]+at]
		if t.Kind != kind {
			return false
		}
		sh.span = append(sh.span, [2]int{t.Pos.Offset, t.End})
	}
	sh.paramType = "Int"
	if kind == token.STRING {
		sh.paramType = "Text"
	}
	return true
}

// lambdaDiff recognises statements that differ only inside one lambda.
func (u *unit) lambdaDiff(sh *share, ranges [][][2]int, j int) bool {
	// The lambdas of statement j, in each Part, in source order.
	lams := make([][]*ast.Lambda, len(sh.parts))
	for pi, p := range sh.parts {
		var walk func(s *ast.Statement)
		walk = func(s *ast.Statement) {
			for _, a := range s.Args {
				if la, ok := a.Value.(ast.LambdaArg); ok && la.Lambda != nil {
					lams[pi] = append(lams[pi], la.Lambda)
				}
			}
			for _, c := range s.Block {
				walk(c)
			}
		}
		walk(p.Block[j])
		if len(lams[pi]) != len(lams[0]) {
			return false
		}
	}

	for li := range lams[0] {
		var spans [][2]int
		typ, ok := "", true
		outer := ""
		for pi := range sh.parts {
			l := lams[pi][li]
			lt := u.lambdaTokens(l)
			first, found := u.tokAt[l.Pos.Offset]
			if !lt.ok || !found || len(lt.body) == 0 {
				ok = false
				break
			}
			last := lt.body[len(lt.body)-1] + 1
			// One line: the value is about to be written after `name: `.
			if u.toks[first].Pos.Line != u.toks[last-1].Pos.Line {
				ok = false
				break
			}
			f, e := ranges[pi][j][0], ranges[pi][j][1]
			if first < f || last > e {
				ok = false
				break
			}
			around := u.spell(f, first) + "│" + u.spell(last, e)
			t := u.lambdaTypeText(l)
			if t == "" || pi > 0 && (around != outer || t != typ) {
				ok = false
				break
			}
			outer, typ = around, t
			spans = append(spans, [2]int{u.toks[first].Pos.Offset, u.toks[last-1].End})
		}
		if ok {
			sh.span, sh.paramType, sh.isLambda = spans, typ, true
			return true
		}
	}
	return false
}

// lambdaTypeText spells a lambda's type as a Shikigami parameter declares
// one, or "" when the checker saw it more than one way.
func (u *unit) lambdaTypeText(l *ast.Lambda) string {
	types := u.paramTypes(l)
	results := u.lamResult[l.Pos.Offset]
	if types == nil || len(results) == 0 || results[0] == nil {
		return ""
	}
	for _, r := range results {
		if r == nil || r.String() != results[0].String() {
			return ""
		}
	}
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = t.String()
	}
	return "(" + strings.Join(parts, ", ") + ") -> " + results[0].String()
}

func (u *unit) extractEdits(sh *share, name, param string) ([]edit, string, error) {
	lines := strings.Split(u.src, "\n")
	starts := u.topLevelStarts()

	// The extent of the run in each Part, in lines.
	type extent struct{ first, last int }
	exts := make([]extent, len(sh.parts))
	for pi, p := range sh.parts {
		limit := nextStart(starts, p.Pos.Offset)
		if sh.to < len(p.Block) {
			limit = p.Block[sh.to].Pos.Offset
		}
		f, l := u.tokRange(p.Block[sh.to-1].Pos.Offset, limit)
		exts[pi] = extent{p.Block[sh.from].Pos.Line, u.lastLine(f, l)}
	}

	// The body is the first Part's run with the difference replaced by the
	// parameter. The name must not already mean something in there.
	bodyFrom, bodyTo := lineOffset(u.src, exts[0].first), lineOffset(u.src, exts[0].last+1)
	for _, t := range u.toks {
		if t.Kind == token.IDENT && t.Literal == param && t.Pos.Offset >= bodyFrom && t.Pos.Offset < bodyTo {
			return nil, "", fmt.Errorf("%q is already a name in those statements", param)
		}
	}
	sp := sh.span[0]
	body := strings.TrimRight(u.src[bodyFrom:sp[0]]+param+u.src[sp[1]:bodyTo], "\n")
	def := fmt.Sprintf("Shikigami %q (%s: %s)\n%s\n\n", name, param, sh.paramType, body)

	head := sh.parts[0].Pos.Line
	at := head
	for at > 1 && token.IsCommentLine(lines[at-2]) && indentOf(lines[at-2]) == 0 {
		at--
	}
	edits := []edit{{start: lineOffset(u.src, at), end: lineOffset(u.src, at), text: def}}
	for pi := range sh.parts {
		ind := strings.Repeat(" ", indentOf(lines[exts[pi].first-1]))
		value := u.src[sh.span[pi][0]:sh.span[pi][1]]
		call := fmt.Sprintf("%sShikigami: %s\n%s    %s: %s\n", ind, name, ind, param, value)
		edits = append(edits, edit{
			start: lineOffset(u.src, exts[pi].first), end: lineOffset(u.src, exts[pi].last+1), text: call,
		})
	}
	return edits, fmt.Sprintf("extracted Shikigami %q from %s", name, plural(len(sh.parts), "Part", "Parts")), nil
}
