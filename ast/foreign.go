package ast

import (
	"slices"
	"strings"

	"domain/token"
)

// Foreign-language blocks: a statement whose body is a block of source in
// another language, run as a subprocess with the current pipeline value on its
// stdin and its stdout becoming the next stage's input.
//
//	Domain Expansion: Python
//	    import sys
//	    print(sum(int(x) for x in sys.stdin))
//
// The body is *not* Domain, so it never becomes an AST: it is captured
// verbatim by the lexer as a single token.RAW and lands here as text. Nothing
// downstream of the parser tries to read it — it is a string handed to another
// language's runtime.
//
// ForeignLanguages lives in this package for the same reason Keywords does:
// the lexer has to recognize a block opener *before* anything with an opinion
// about semantics has seen the line, because from the next line onward the
// source is not Domain and cannot be tokenized as Domain. That makes the list
// part of the syntax, which is what this package is for.

// ForeignLanguages is the closed set of languages a foreign block may be
// written in, in canonical spelling. It is closed on purpose: a name here
// changes how the *lexer* reads the lines beneath it, so the set has to be
// small, known, and unable to grow by accident.
var ForeignLanguages = []string{
	"Python",
	"Go",
	"rask",
	"cRust",
}

// ForeignLanguage reports whether name is a foreign language, returning its
// canonical spelling. Matching is case-insensitive, so `Domain Expansion:
// python` and `Domain Expansion: PYTHON` both name Python — Domain's operation
// phrases are matched case-insensitively everywhere else (see prims.hasWord),
// and a lexical rule that cared about case would be the only one that did.
func ForeignLanguage(name string) (string, bool) {
	i := slices.IndexFunc(ForeignLanguages, func(l string) bool {
		return strings.EqualFold(l, name)
	})
	if i < 0 {
		return "", false
	}
	return ForeignLanguages[i], true
}

// ForeignBlock is the body of a foreign-language statement: source text in
// another language, already dedented to column zero, ready to be written to a
// file and run.
//
// Pos is the first line of the *block*, not of the statement that opened it,
// so an error reported against the block points into the foreign source rather
// than at the Domain line above it.
type ForeignBlock struct {
	Language string     // canonical, from ForeignLanguage
	Source   string     // the block, dedented, ending in a newline
	Sig      *Signature // declared `: In -> Out`; nil when not written
	Pos      token.Position
}

// ForeignOpener reports whether line — the content tokens of one logical line,
// with layout tokens already removed and no trailing NEWLINE — opens a foreign
// block, returning the language as written and the index just past its name
// (where a declared signature would begin).
//
// Two shapes qualify, and nothing else:
//
//	Domain Expansion: Python              a themed keyword, then a language
//	Python                                the same line with the keyword left off
//
// followed optionally by `: In -> Out`. Requiring the words ahead of the colon
// to be *exactly* a themed keyword is what keeps a named argument out: the
// words ahead of the colon in `Using: Python` name no keyword, so that line is
// the ordinary argument it has always been.
//
// The lexer and the parser must agree here to the token, which is why the rule
// lives in one function rather than in each of them: the lexer emits a RAW only
// for lines this accepts, so a parser reading a wider set would demand blocks
// the lexer gave the author no chance to write, and a narrower one would strand
// a RAW token mid-stream.
func ForeignOpener(line []token.Token) (lang string, rest int, ok bool) {
	switch {
	case len(line) > 0 && line[0].Kind == token.IDENT:
		if _, isLang := ForeignLanguage(line[0].Literal); isLang {
			lang, rest = line[0].Literal, 1
			break
		}
		// A themed keyword, then the language: scan the leading run of
		// identifier words up to a colon.
		var words []string
		i := 0
		for ; i < len(line) && line[i].Kind == token.IDENT; i++ {
			words = append(words, line[i].Literal)
		}
		if i+1 >= len(line) || line[i].Kind != token.COLON || line[i+1].Kind != token.IDENT {
			return "", 0, false
		}
		if kw, n, isKw := KeywordPrefix(words); !isKw || n != len(words) || kw == "" {
			return "", 0, false
		}
		if _, isLang := ForeignLanguage(line[i+1].Literal); !isLang {
			return "", 0, false
		}
		lang, rest = line[i+1].Literal, i+2
	default:
		return "", 0, false
	}
	// Past the language name the line must either end or turn into a declared
	// signature. Anything else is an operation phrase that merely begins with a
	// language name — `Python Sort`, say — and names no foreign block.
	if rest != len(line) && line[rest].Kind != token.COLON {
		return "", 0, false
	}
	return lang, rest, true
}
