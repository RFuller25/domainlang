// Folding `Case:` lines that differ in one word into one template.
//
//	Case: inc "{r:word} inc {n:int} if {c:word}"
//	Case: dec "{r:word} dec {n:int} if {c:word}"
//
// becomes
//
//	Using: "{r:word} {kind:word} {n:int} if {c:word}"
//
// The hole is named `kind` because that is the field `Case:` was already
// producing, so everything downstream that reads `r.kind` reads the same text
// it always did. That only holds when each case's tag *is* its differing word,
// which is the condition the whole rewrite hangs on: `Case: on "turn on …"`
// yields `kind = "on"` from a line whose word is not the only difference
// worth keeping, and is left alone.
//
// One thing does change, and it is why `Mode: Try` is excluded. Two cases
// accept two verbs; a `{kind:word}` hole accepts any. Under `Each` a line with
// a third verb used to stop the program and now parses — which only matters to
// an input the program could not read before. Under `Try` those lines were
// being dropped on purpose, and merging would quietly keep them.
package tidy

import (
	"strings"

	"domain/ast"
	"domain/token"
)

func passCases(u *unit) (edits []edit, summary string, notes []string) {
	done := 0
	u.eachStatement(func(s *ast.Statement, _ *ast.ShikigamiDef) {
		if es, ok := u.mergeCases(s); ok {
			edits = append(edits, es...)
			done++
		}
	})
	if done == 0 {
		return nil, "", nil
	}
	return edits, "merged the Case lines of " + plural(done, "Match Pattern", "Match Patterns"), nil
}

type caseLine struct {
	tag      string
	raw      string // the template as written, between its quotes
	from, to int    // the byte range of `Case: tag "template"`
}

func (u *unit) mergeCases(s *ast.Statement) ([]edit, bool) {
	var cases []caseLine
	for _, a := range s.Args {
		switch v := a.Value.(type) {
		case ast.CaseArg:
			i, ok := u.tokAt[a.Pos.Offset]
			if !ok || i+3 >= len(u.toks) {
				return nil, false
			}
			name, colon, tag, tmpl := u.toks[i], u.toks[i+1], u.toks[i+2], u.toks[i+3]
			if name.Kind != token.IDENT || colon.Kind != token.COLON ||
				tag.Kind != token.IDENT || tmpl.Kind != token.STRING || tmpl.End-tmpl.Pos.Offset < 2 {
				return nil, false
			}
			// A comment after a case would go when its line does.
			if !restIsBlank(u.src, tmpl.End) || strings.TrimSpace(u.src[lineStart(u.src, name.Pos.Offset):name.Pos.Offset]) != "" {
				return nil, false
			}
			cases = append(cases, caseLine{
				tag: v.Tag, raw: u.src[tmpl.Pos.Offset+1 : tmpl.End-1],
				from: name.Pos.Offset, to: tmpl.End,
			})
		case ast.IdentArg:
			if strings.EqualFold(a.Name, "Mode") && !strings.EqualFold(v.Value, "Each") && !strings.EqualFold(v.Value, "One") {
				return nil, false
			}
		}
		if strings.EqualFold(a.Name, "Using") {
			return nil, false
		}
	}
	if len(cases) < 2 {
		return nil, false
	}

	first := cases[0].raw
	pre, suf := len(first), len(first)
	for _, c := range cases[1:] {
		pre = min(pre, commonPrefix(first, c.raw))
		suf = min(suf, commonSuffix(first, c.raw))
	}
	for _, c := range cases {
		pre = min(pre, len(c.raw))
	}
	// "inc" and "dec" share their last letter; the difference is the word, not
	// the bytes, so both ends back off to a word boundary.
	for pre > 0 && isWordByte(first[pre-1]) {
		pre--
	}
	for suf > 0 && isWordByte(first[len(first)-suf]) {
		suf--
	}
	prefix, suffix := first[:pre], first[len(first)-suf:]

	for _, c := range cases {
		if len(c.raw) < pre+suf {
			return nil, false
		}
		word := c.raw[pre : len(c.raw)-suf]
		if word == "" || word != c.tag || !allWordBytes(word) {
			return nil, false
		}
		// Shapes whose matching depends on the literal that is about to
		// become a hole: a `text` hole is greedy to the next literal, and a
		// group's extent is its own business.
		for _, risky := range []string{"text}", "text+", "[?", ")+", "{~}"} {
			if strings.Contains(c.raw, risky) {
				return nil, false
			}
		}
	}
	// The word must stand alone between spaces (or the ends), outside any hole.
	if prefix != "" && !strings.HasSuffix(prefix, " ") || suffix != "" && !strings.HasPrefix(suffix, " ") {
		return nil, false
	}
	if strings.Count(prefix, "{") != strings.Count(prefix, "}") {
		return nil, false
	}

	es := []edit{{start: cases[0].from, end: cases[0].to,
		text: `Using: "` + prefix + `{kind:word}` + suffix + `"`}}
	for _, c := range cases[1:] {
		ls := lineStart(u.src, c.from)
		le := lineEnd(u.src, c.to)
		if le < len(u.src) {
			le++
		}
		es = append(es, edit{start: ls, end: le})
	}
	return es, true
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

func isWordByte(c byte) bool { return token.IsIdentByte(c) }

func allWordBytes(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isWordByte(s[i]) {
			return false
		}
	}
	return true
}
