// One spelling per file.
//
// Every themed keyword is optional, and the two spellings may mix line by
// line — which is a feature when it is a choice and noise when it is an
// accident. The formatter deliberately never adds or removes a keyword,
// because it cannot know which it is looking at. This pass takes the file's
// own majority as the answer: whichever spelling most of its statements use is
// the one the rest are brought to, and a tie goes to writing the keyword.
//
// Phrase words get the same treatment. Operation phrases match without regard
// to case, so `while` and `While` are one loop spelled twice; the minority
// spelling follows the majority, with a tie going to the capital.
//
// Both passes are held to the strictest check there is: the resolved pipeline
// must be identical afterwards, node for node. Removing a keyword is the only
// direction that can fail it — a bare phrase that is ambiguous needs its
// keyword — and when it does, the file is unified the other way instead.
package tidy

import (
	"strings"

	"domain/ast"
	"domain/token"
)

// optionalKeywords are the ones a statement may be written without.
//
// `Cursed Energy` is left out on purpose. The source line is the one statement
// people keep the keyword on in an otherwise bare file — `Cursed Energy:
// stdin` reads as a header, and a first line that is just `i15` reads as
// nothing — so it is neither counted as evidence nor rewritten.
var optionalKeywords = map[string]bool{
	"Cursed Technique": true, "Channeled Energy": true,
	"Maximum Technique": true, "Domain Expansion": true,
	"Reverse Cursed Technique": true, "Simple Domain": true,
	"Shikigami": true, "Binding Vow": true, "Reveal": true,
}

// spelled is a statement whose keyword could be written either way.
type spelled struct {
	s    *ast.Statement
	bare bool
}

func (u *unit) spelledStatements() []spelled {
	var out []spelled
	u.eachStatement(func(s *ast.Statement, _ *ast.ShikigamiDef) {
		if s.Op == nil || s.Foreign != nil || !optionalKeywords[s.Keyword] {
			return
		}
		// Only a statement that begins its line; one behind an `Of` does not.
		if strings.TrimSpace(u.src[lineStart(u.src, s.Pos.Offset):s.Pos.Offset]) != "" {
			return
		}
		out = append(out, spelled{s, s.KeywordInferred})
	})
	return out
}

func passKeywordSpelling(u *unit) (edits []edit, summary string, notes []string) {
	all := u.spelledStatements()
	bare := 0
	for _, sp := range all {
		if sp.bare {
			bare++
		}
	}
	written := len(all) - bare
	if bare == 0 || written == 0 {
		return nil, "", nil
	}
	if bare > written {
		if es := u.dropKeywords(all); es != nil {
			// Dropping is the direction that can be refused, so it is tried
			// here rather than left to the driver: a refusal should fall back
			// to the other spelling, not leave the file mixed.
			if next, ok := applyEdits(u.src, es); ok {
				if nu, err := load(u.path, next); err == nil && fingerprint(nu.pipe) == fingerprint(u.pipe) {
					return es, "dropped " + plural(written, "keyword", "keywords") + " to match the file", nil
				}
			}
		}
	}
	for _, sp := range all {
		if sp.bare {
			edits = append(edits, edit{start: sp.s.Pos.Offset, end: sp.s.Pos.Offset, text: sp.s.Keyword + ": "})
		}
	}
	return edits, "wrote " + plural(bare, "missing keyword", "missing keywords"), nil
}

// dropKeywords removes the keyword and its colon from every statement that
// wrote one.
func (u *unit) dropKeywords(all []spelled) []edit {
	var es []edit
	for _, sp := range all {
		if sp.bare {
			continue
		}
		i, ok := u.tokAt[sp.s.Pos.Offset]
		if !ok {
			return nil
		}
		n := len(strings.Fields(sp.s.Keyword))
		for j := 0; j < n; j++ {
			if u.toks[i+j].Kind != token.IDENT {
				return nil
			}
		}
		if u.toks[i+n].Kind != token.COLON || isLayout(u.toks[i+n+1].Kind) || u.toks[i+n+1].Kind == token.EOF {
			return nil
		}
		es = append(es, edit{start: sp.s.Pos.Offset, end: u.toks[i+n+1].Pos.Offset})
	}
	return es
}

// connectives are the small words a phrase is conventionally written with in
// lower case (`Split Text by`, `For x in xs`), which are no evidence of which
// way a file capitalises.
var connectives = map[string]bool{
	"by": true, "to": true, "in": true, "of": true, "with": true, "from": true,
	"at": true, "on": true, "as": true, "a": true, "an": true, "the": true,
	"and": true, "or": true, "into": true, "per": true, "until": true,
}

// phraseWord is one word of an operation phrase that has a case to choose.
type phraseWord struct {
	tok   token.Token
	upper bool
}

func (u *unit) phraseWords() []phraseWord {
	var out []phraseWord
	u.eachStatement(func(s *ast.Statement, def *ast.ShikigamiDef) {
		if s.Op == nil || s.Foreign != nil {
			return
		}
		// A source is a file name, a Shikigami call is a name someone chose,
		// and a Reveal names a stream: none is a phrase with a canonical case.
		switch s.Keyword {
		case "Cursed Technique", "Channeled Energy", "Maximum Technique", "Domain Expansion",
			"Reverse Cursed Technique", "Simple Domain":
		default:
			return
		}
		if strings.TrimSpace(u.src[lineStart(u.src, s.Pos.Offset):s.Pos.Offset]) != "" {
			return
		}
		params := map[string]bool{}
		if def != nil {
			for _, p := range def.Params {
				params[p.Name] = true
			}
		}
		i, ok := u.tokAt[s.Pos.Offset]
		if !ok {
			return
		}
		if !s.KeywordInferred {
			i += len(strings.Fields(s.Keyword)) + 1 // the keyword and its colon
		}
		loop := s.Keyword == "Simple Domain"
		// The primary segment: up to the first comma or the end of the line.
		for n := 0; i < len(u.toks); i, n = i+1, n+1 {
			t := u.toks[i]
			if isLayout(t.Kind) || t.Kind == token.EOF || t.Kind == token.COMMA {
				break
			}
			if t.Kind != token.IDENT || !isAlpha(t.Literal) || connectives[strings.ToLower(t.Literal)] || params[t.Literal] {
				continue
			}
			// A `For` loop's words after the first are a variable and a
			// channel, both the author's names.
			if loop && n > 0 && strings.EqualFold(u.toks[u.tokAt[s.Pos.Offset]+phraseStart(s)].Literal, "For") {
				if !(n == 1 && strings.EqualFold(t.Literal, "Each")) {
					continue
				}
			}
			switch {
			case t.Literal == strings.ToLower(t.Literal):
				out = append(out, phraseWord{t, false})
			case t.Literal[:1] == strings.ToUpper(t.Literal[:1]) && t.Literal[1:] == strings.ToLower(t.Literal[1:]):
				out = append(out, phraseWord{t, true})
			}
		}
	})
	return out
}

// phraseStart is how many tokens of a statement precede its phrase.
func phraseStart(s *ast.Statement) int {
	if s.KeywordInferred {
		return 0
	}
	return len(strings.Fields(s.Keyword)) + 1
}

func isAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i] | 0x20
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return s != ""
}

func passPhraseCase(u *unit) (edits []edit, summary string, notes []string) {
	words := u.phraseWords()
	upper := 0
	for _, w := range words {
		if w.upper {
			upper++
		}
	}
	lower := len(words) - upper
	if upper == 0 || lower == 0 {
		return nil, "", nil
	}
	toUpper := upper >= lower
	n := 0
	for _, w := range words {
		if w.upper == toUpper {
			continue
		}
		text := strings.ToLower(w.tok.Literal)
		if toUpper {
			text = strings.ToUpper(text[:1]) + text[1:]
		}
		edits = append(edits, edit{start: w.tok.Pos.Offset, end: w.tok.End, text: text})
		n++
	}
	return edits, "matched the case of " + plural(n, "phrase word", "phrase words"), nil
}
