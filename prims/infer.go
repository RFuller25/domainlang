package prims

// Prefix inference: the themed keyword in front of an operation is optional.
//
// `Cursed Technique: Split Text by "\n"` and `Split Text by "\n"` are the same
// statement. The parser leaves a prefix-free line's Keyword empty; Infer walks
// the program and fills it in from the phrase alone, so every stage after it
// (resolution, the optimizer's AST rewrites, the linter, the LSP) sees a
// fully-keyworded statement and cannot tell the difference.
//
// The keyword is recovered in this order — each step is checked before the
// generic registry scan because its shape is more specific than any phrase a
// primitive matches:
//
//  1. a defined Shikigami name (exact); the naming rule below keeps this
//     unambiguous
//  2. a From: consumer (Combine / Difference / Fold / Zip over channels)
//  3. a loop kind: Repeat N / While / Iterate Until Fixed Point
//  4. a Binding Vow shape: Count Equals N / All Values <cmp> N
//  5. the Reveal sink: stdout
//  6. a source target on the first stage: input.txt, stdin, data/day1.txt
//  7. the primitive registry — every primitive whose matcher accepts the
//     phrase. Matches under one keyword are ordered by specificity already
//     (Split Each before Split), so the first wins; matches under *different*
//     keywords are a genuine ambiguity and an error that asks for the keyword.
//  8. failing all of that, the first stage of a program is a source
//
// Steps 1 and 7 are why a Shikigami may no longer be named after a built-in:
// see checkShikigamiName.

import (
	"fmt"
	"slices"
	"strings"

	"domain/ast"
	"domain/token"
	"domain/typecheck"
)

// catchAllKeywords are the keywords whose primitive matches every phrase —
// they carry a target or a predicate rather than an operation name. They are
// excluded from the registry scan (they would swallow every prefix-free line)
// and recognized by shape instead.
var catchAllKeywords = map[string]bool{
	"Cursed Energy": true,
	"Reveal":        true,
	"Binding Vow":   true,
}

// Infer fills in the themed keyword of every statement written without one,
// mutating prog in place. It is idempotent, and a statement that already
// carries a keyword is left untouched — a program written the long way
// resolves through exactly the path it always did.
//
// Resolve calls Infer first; tools that walk the AST without resolving (the
// linter, the source-level optimizer) get it through Resolve, or may call it
// directly.
//
// Only the first failure is reported (matching the rest of the resolver), but
// inference always runs to the end of the program: a statement the analyzer
// could not name must not cost the statements after it their keywords, or the
// linter would go on to misjudge a pipeline it can no longer read.
func Infer(prog *ast.Program) error {
	return InferWith(prog, nil)
}

// InferWith is Infer with extra callable names in scope — the Shikigami an
// `Innate Domain` import brought in. An imported operation is called by its
// bare name like any other, so inference has to know it exists.
func InferWith(prog *ast.Program, extra []string) error {
	names := map[string]bool{}
	for _, n := range PreludeNames() {
		names[strings.ToLower(n)] = true
	}
	for _, n := range extra {
		names[strings.ToLower(n)] = true
	}
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, def := range prog.Shikigamis {
		keep(checkShikigamiName(def))
		names[strings.ToLower(def.Name)] = true
	}
	// A Shikigami body is always entered with an upstream value, so no
	// statement inside one can be the program's source stage.
	for _, def := range prog.Shikigamis {
		keep(inferSequence(def.Body, names, false))
		keep(inferBinds(def.Binds, names))
	}
	keep(inferSequence(prog.Statements, names, true))
	return firstErr
}

// inferSequence infers a run of statements and their nested blocks, returning
// the first failure. source is true only for the top-level sequence, where the
// first statement is the one position in a program that may name an input to
// read.
func inferSequence(stmts []*ast.Statement, names map[string]bool, source bool) error {
	var firstErr error
	for i, s := range stmts {
		if err := inferStatement(s, names, source && i == 0); err != nil && firstErr == nil {
			firstErr = err
		}
		// Nested blocks (channel and loop bodies) always run on an upstream
		// value, so they never contain the source stage.
		if err := inferSequence(s.Block, names, false); err != nil && firstErr == nil {
			firstErr = err
		}
		// A `Consider x Of <operation>` source is an ordinary statement whose
		// keyword may have been left out like any other's, and it runs on the
		// current value rather than reading an input. A `Cursed Object` /
		// `Cursed Tool` declaration takes the same right-hand side and so
		// needs the same pass — they share parseOfSource, so anything writable
		// after one preposition is writable after the other.
		if err := inferBinds(s.Binds, names); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := inferBinds(s.Decls, names); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// inferBinds infers the keywords of the statements behind an `Of` right-hand
// side — a `Consider x Of …` binding or a `Cursed Object` / `Cursed Tool`
// declaration, which are the same shape.
func inferBinds(binds []*ast.Binding, names map[string]bool) error {
	var firstErr error
	for _, b := range binds {
		if err := inferSequence(b.Body, names, false); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func inferStatement(s *ast.Statement, names map[string]bool, source bool) error {
	if s.Keyword != "" || s.Op == nil {
		return nil // already keyworded, or a pure block opener with nothing to read
	}
	s.KeywordInferred = true
	op := s.Op
	// A statement carrying foreign source needs no inference: the parser
	// already established what it is, and the shape rule that let it capture a
	// block is stricter than anything below. Deciding here also keeps it away
	// from the source-target case, which would otherwise claim a foreign block
	// written as the first line of a program.
	if s.Foreign != nil {
		s.Keyword = "Domain Expansion"
		return nil
	}
	switch {
	case names[strings.ToLower(strings.TrimSpace(op.Raw))]:
		s.Keyword = "Shikigami"
	case hasFrom(s):
		s.Keyword = "Maximum Technique"
	case isLoopPhrase(op):
		s.Keyword = "Simple Domain"
	case isVowPhrase(op):
		s.Keyword = "Binding Vow"
	case isSinkPhrase(op):
		s.Keyword = "Reveal"
	case source && isSourcePhrase(op):
		s.Keyword = "Cursed Energy"
	default:
		prim, err := inferPrimitive(op, s.Pos)
		switch {
		case err != nil:
			return err
		case prim != nil:
			s.Keyword = prim.Keyword
		case source:
			// Nothing in the vocabulary names this phrase and it opens the
			// program: it is the input to read.
			s.Keyword = "Cursed Energy"
		default:
			return cannotInferError(op, s.Pos)
		}
	}
	return nil
}

// inferPrimitive finds the primitive a prefix-free phrase names. It returns a
// nil primitive and a nil error when nothing in the registry matches, and an
// error only for a phrase that is ambiguous across keywords.
func inferPrimitive(op *ast.Operation, pos token.Position) (*Primitive, error) {
	var hits []*Primitive
	for _, p := range Registry {
		if !catchAllKeywords[p.Keyword] && p.Match(op) {
			hits = append(hits, p)
		}
	}
	if len(hits) == 0 {
		return nil, nil
	}
	// Several primitives under one keyword matching is ordinary: the registry
	// is ordered specific-first (Split Each before Split), so the first hit is
	// the intended one. Hits under different keywords are not resolvable from
	// the phrase, and asking for the keyword beats guessing.
	var choices []string
	for _, p := range hits {
		if p.Keyword != hits[0].Keyword {
			choices = append(choices, fmt.Sprintf("%s: %s", p.Keyword, p.ID))
		}
	}
	if len(choices) > 0 {
		choices = append([]string{fmt.Sprintf("%s: %s", hits[0].Keyword, hits[0].ID)}, choices...)
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"ambiguous operation %q without a keyword; it matches %s — write the keyword to choose",
			op.Raw, strings.Join(choices, " and "))}
	}
	return hits[0], nil
}

func cannotInferError(op *ast.Operation, pos token.Position) error {
	return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
		"cannot infer a keyword for %q: no operation matches this phrase", op.Raw)}
}

// isLoopPhrase recognizes the three Simple Domain loop kinds, mirroring the
// dispatch in resolveLoop so that a bare loop head and a keyworded one accept
// exactly the same phrases.
//
// Two families of primitives borrow the loop kinds' words and are excluded
// here, because this check runs before the registry scan and would otherwise
// swallow them: `Take While` / `Drop While` are prefix transforms, not the
// `While` loop, and `Iterate n` is the generator, where the loop is spelled
// `Iterate Until Fixed Point`.
func isLoopPhrase(op *ast.Operation) bool {
	switch {
	case hasWord(op, "Repeat"):
		return true
	case hasWord(op, "While"):
		return !hasWord(op, "Take") && !hasWord(op, "Drop")
	case hasWord(op, "Iterate"):
		return hasWord(op, "Until") || hasWord(op, "Fixed")
	default:
		return hasWord(op, "Fixed") && hasWord(op, "Point")
	}
}

// isVowPhrase recognizes the two Binding Vow predicate shapes, mirroring
// buildVowCheck. `Count Equals 3` is therefore a vow rather than the Count
// reduction — Count itself never takes a number.
func isVowPhrase(op *ast.Operation) bool {
	if hasWord(op, "Count") && (hasWord(op, "Equals") || hasSym(op, "=")) {
		return true
	}
	return hasWord(op, "All") && hasWord(op, "Values")
}

// isSinkPhrase recognizes the Reveal sink: stdout, or stderr for output that
// deliberately stays out of the program's answer.
func isSinkPhrase(op *ast.Operation) bool {
	return len(op.Words) == 1 && len(op.Strings) == 0 && len(op.Ints) == 0 &&
		(strings.EqualFold(op.Words[0], "stdout") || strings.EqualFold(op.Words[0], "stderr"))
}

// isSourcePhrase recognizes a phrase that reads as an input target rather than
// an operation: stdin, or anything shaped like a path. Checked before the
// registry so that a file whose name collides with a primitive word
// (`sum.txt`) still reads as a source on the first line.
func isSourcePhrase(op *ast.Operation) bool {
	raw := strings.TrimSpace(op.Raw)
	return strings.EqualFold(raw, "stdin") ||
		strings.ContainsAny(raw, "./\\") && !strings.ContainsAny(raw, " \t")
}

// ---------------------------------------------------------------------------
// Shikigami naming

// checkShikigamiName rejects a definition named after something a prefix-free
// line already means. Without the themed keyword a call site is just the name
// (`Top K Sum`), so a Shikigami called `Sum` — or `Repeat`, or `stdout` —
// would shadow the built-in meaning of that line with no way to ask for the
// other one.
func checkShikigamiName(def *ast.ShikigamiDef) error {
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return &ResolveError{Pos: def.Pos, Msg: "Shikigami needs a name"}
	}
	if what, taken := reservedMeaning(name); taken {
		return &ResolveError{Pos: def.Pos, Msg: fmt.Sprintf(
			"Shikigami %q is named after %s; the themed keyword is optional, so a call to it would be indistinguishable from the built-in — pick another name",
			def.Name, what)}
	}
	return nil
}

// checkDeclaredName refuses a declared name that already means something else
// in the language. It is the same test checkShikigamiName applies, phrased for
// a declaration rather than a definition: role names what is being declared
// ("a global"), and the position is the declaration's.
//
// The two share reservedMeaning rather than each keeping a list, because a
// name that would be ambiguous as a Shikigami is ambiguous as a global for the
// same reason — both are read from an expression or a prefix-free line, and
// neither has a slot to disambiguate it.
func checkDeclaredName(name, role string, pos token.Position) error {
	if strings.TrimSpace(name) == "" {
		return &ResolveError{Pos: pos, Msg: role + " needs a name"}
	}
	if what, taken := reservedMeaning(name); taken {
		return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%q is already %s, so it cannot also be %s — pick another name", name, what, role)}
	}
	return nil
}

// reservedMeaning reports what a name already means in the language, if it
// means anything: a primitive (by id or by a phrase that spells it), a themed
// keyword, a loop kind, a vow predicate, a Reveal sink, an input source, or an
// expression builtin.
func reservedMeaning(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	op := namePhrase(name)
	for _, p := range Registry {
		// A primitive's own ID is reserved outright; beyond that, so is any
		// phrase that spells it (`Quicksort` for Sort). Both are needed: an ID
		// with a filler word ("Convert To Grid") is not load-bearing word for
		// word, and a matcher's alternative spellings are not IDs.
		if strings.EqualFold(name, p.ID) || (!catchAllKeywords[p.Keyword] && namesPrimitive(p, op.Words)) {
			return fmt.Sprintf("the built-in operation %q (%s)", p.ID, p.Keyword), true
		}
	}
	if _, _, ok := ast.KeywordPrefix(strings.Fields(name)); ok {
		return "a themed keyword", true
	}
	switch {
	case isLoopPhrase(op):
		return "a Simple Domain loop kind", true
	case isVowPhrase(op):
		return "a Binding Vow predicate", true
	case isSinkPhrase(op):
		return "the Reveal sink", true
	case isSourcePhrase(op):
		return "an input source", true
	}
	for _, b := range typecheck.Builtins {
		if strings.EqualFold(name, b) {
			return fmt.Sprintf("the expression builtin %q", b), true
		}
	}
	return "", false
}

// namePhrase reads a Shikigami name as the operation phrase a prefix-free call
// site would produce, so the same matchers decide both.
func namePhrase(name string) *ast.Operation {
	return &ast.Operation{Words: strings.Fields(name), Raw: name}
}

// namesPrimitive reports whether words *is* a name for p, as opposed to merely
// mentioning one of its words. p must match the phrase, and every word in it
// must be load-bearing: drop any one of them and p stops matching.
//
// Primitive matchers test for words anywhere in the phrase, so a plain match
// is far too coarse a reservation rule — it would take "Scaled Sum" and "Sort
// Text" away from users because Sum and Sort appear inside them. Requiring
// every word to carry the match reserves exactly the phrases that spell a
// built-in and nothing else: "Sum", "Quicksort", "Sort By", "Convert To Grid".
func namesPrimitive(p *Primitive, words []string) bool {
	if len(words) == 0 || !p.Match(&ast.Operation{Words: words}) {
		return false
	}
	for i := range words {
		trial := slices.Concat(words[:i], words[i+1:])
		if p.Match(&ast.Operation{Words: trial}) {
			return false // this word is decoration, not part of the built-in's name
		}
	}
	return true
}

// ReservedNames lists, in sorted order, the names a Shikigami may not take:
// every primitive ID, every themed keyword, and every expression-layer
// builtin. Diagnostics use it to explain the rule.
func ReservedNames() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, p := range Registry {
		add(p.ID)
	}
	for _, k := range ast.Keywords {
		add(k)
	}
	for _, b := range typecheck.Builtins {
		add(b)
	}
	slices.Sort(out)
	return out
}
