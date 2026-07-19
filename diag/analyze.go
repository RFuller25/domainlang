// The analyzer: runs the static front end, converts every failure into an
// enriched Diagnostic, and — the trick that surfaces more than one error per
// stage — applies each confident fix to a private working copy and re-runs,
// so errors hiding behind an earlier failure are found in the same pass.
package diag

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
	"domain/token"
)

// Report is the full result of analyzing one program.
type Report struct {
	Path     string
	Src      string // the original source, untouched
	FixedSrc string // Src with every confident fix applied (== Src when none)
	Applied  int    // number of confident fixes folded into FixedSrc
	Diags    []Diagnostic
	Program  *ast.Program // parse of the final working source, when it parses
	Pipe     *ir.Pipeline // full resolution, when the final working source is clean
}

// Counts tallies the diagnostics by severity.
func (r *Report) Counts() (errs, warns, hints int) {
	for _, d := range r.Diags {
		switch d.Severity {
		case Error:
			errs++
		case Warning:
			warns++
		default:
			hints++
		}
	}
	return
}

// maxRepairRounds bounds the fix-and-re-analyze loop; each round either fixes
// at least one error (progress) or stops, so this is a safety net, not a knob.
const maxRepairRounds = 30

// Analyze runs the intelligent front end over src. The user's file is never
// written; fixes are applied only to the in-memory working copy so that the
// analysis can see past each repaired error to the next one.
func Analyze(path, src string) *Report {
	r := &Report{Path: path, Src: src, FixedSrc: src}
	working := src
	seen := map[string]bool{}

	for round := 0; round < maxRepairRounds; round++ {
		stage, prog, pipe := frontEnd(working)
		if prog != nil {
			r.Program = prog
		}
		if len(stage) == 0 {
			r.Pipe = pipe
			break
		}

		var fixes []Fix
		for _, d := range stage {
			key := fmt.Sprintf("%d|%s", d.Pos.Line, d.Msg)
			if !seen[key] {
				seen[key] = true
				r.Diags = append(r.Diags, d)
			}
			if d.HasConfidentFix() {
				fixes = append(fixes, *d.Fix)
			}
		}
		if len(fixes) == 0 {
			break // nothing more we can see past
		}
		working = applyFixes(working, fixes)
		r.Applied += len(fixes)
	}

	r.FixedSrc = working
	if r.Program != nil {
		r.Diags = append(r.Diags, Lint(r.Program, working)...)
	}
	sortDiags(r.Diags)
	return r
}

// frontEnd runs lex → parse → resolve on src and returns the enriched
// diagnostics of the first failing stage (empty when clean), plus whatever
// later artifacts were reached.
func frontEnd(src string) ([]Diagnostic, *ast.Program, *ir.Pipeline) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return []Diagnostic{lexDiag(err, src)}, nil, nil
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return parseDiags(err, src), nil, nil
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		return resolveDiags(err, prog, src), prog, nil
	}
	return nil, prog, pipe
}

// applyFixes splices the replacements into src, right to left so earlier
// offsets stay valid; overlapping fixes are dropped after the first.
func applyFixes(src string, fixes []Fix) string {
	// Sort by start descending.
	sort.Slice(fixes, func(i, j int) bool { return fixes[i].Start > fixes[j].Start })
	prevStart := len(src) + 1
	for _, f := range fixes {
		if f.Start < 0 || f.End > len(src) || f.End < f.Start || f.End > prevStart {
			continue // out of bounds or overlapping the previous splice
		}
		src = src[:f.Start] + f.Replacement + src[f.End:]
		prevStart = f.Start
	}
	return src
}

// ---------------------------------------------------------------------------
// Lexical errors

func lexDiag(err error, src string) Diagnostic {
	le, ok := err.(*lexer.Error)
	if !ok {
		return Diagnostic{Severity: Error, Code: "lex", Msg: err.Error()}
	}
	d := Diagnostic{
		Severity: Error, Code: "syntax",
		Pos: le.Pos, Msg: le.Msg,
		LineText: lineAt(src, le.Pos.Line),
	}
	switch {
	case strings.Contains(le.Msg, "tabs are not allowed"):
		end := le.Pos.Offset
		for end < len(src) && (src[end] == '\t' || src[end] == ' ') {
			end++
		}
		tabs := strings.Count(src[le.Pos.Offset:end], "\t")
		spaces := strings.Count(src[le.Pos.Offset:end], " ")
		d.Help = "Domain indentation is spaces only; replace each tab with four spaces"
		d.Fix = &Fix{Start: le.Pos.Offset, End: end,
			Replacement: strings.Repeat(" ", tabs*4+spaces), Confident: true}

	case strings.Contains(le.Msg, "newline in string"):
		nl := strings.IndexByte(src[le.Pos.Offset:], '\n')
		if nl >= 0 {
			at := le.Pos.Offset + nl
			d.Help = `close the string with '"' before the end of the line`
			d.Fix = &Fix{Start: at, End: at, Replacement: `"`, Confident: true}
		}

	case strings.Contains(le.Msg, "unterminated escape"):
		d.Help = `the source ends in the middle of a backslash escape; finish the escape (\n, \t, \", \\) and close the string`

	case strings.Contains(le.Msg, "unterminated string"):
		at := le.Pos.Offset
		for at < len(src) && src[at] != '\n' {
			at++
		}
		d.Help = `close the string with '"'`
		d.Fix = &Fix{Start: at, End: at, Replacement: `"`, Confident: true}

	case strings.Contains(le.Msg, "unknown escape sequence"):
		d.Help = `valid escapes are \n, \t, \r, \\, \", and \0; write \\ for a literal backslash`

	case strings.Contains(le.Msg, "unexpected character"):
		enrichUnexpectedChar(&d, src)

	case strings.Contains(le.Msg, "inconsistent dedent"):
		enrichDedent(&d, src)
	}
	return d
}

// enrichUnexpectedChar recognizes characters that sneak in from word
// processors and chat apps — smart quotes, long dashes, non-breaking spaces —
// and offers the ASCII repair.
func enrichUnexpectedChar(d *Diagnostic, src string) {
	rest := src[d.Pos.Offset:]
	repairs := []struct {
		bad, good, why string
	}{
		{"“", `"`, "a curly left quote"},
		{"”", `"`, "a curly right quote"},
		{"„", `"`, "a low curly quote"},
		{"‘", `"`, "a curly single quote"},
		{"’", `"`, "a curly single quote"},
		{"–", "-", "an en dash"},
		{"—", "-", "an em dash"},
		{" ", " ", "a non-breaking space"},
		{"：", ":", "a full-width colon"},
		{";", "", "a semicolon (Domain statements end at the newline)"},
	}
	for _, r := range repairs {
		if strings.HasPrefix(rest, r.bad) {
			d.Help = fmt.Sprintf("this is %s; Domain expects %s", r.why, describeRepl(r.good))
			d.Fix = &Fix{Start: d.Pos.Offset, End: d.Pos.Offset + len(r.bad),
				Replacement: r.good, Confident: true}
			// An opening curly quote usually has its closing partner later on
			// the same line; repair the pair in one spanning fix so the fixed
			// string doesn't keep a stray curly quote as content.
			if close, ok := curlyPartner[r.bad]; ok {
				line := rest
				if nl := strings.IndexByte(line, '\n'); nl >= 0 {
					line = line[:nl]
				}
				if at := strings.Index(line[len(r.bad):], close); at >= 0 {
					inner := line[len(r.bad) : len(r.bad)+at]
					d.Fix = &Fix{
						Start:       d.Pos.Offset,
						End:         d.Pos.Offset + len(r.bad) + at + len(close),
						Replacement: `"` + inner + `"`,
						Confident:   true,
					}
				}
			}
			return
		}
	}
	d.Help = "this character is not part of Domain's syntax; see docs/language.md for the grammar"
}

// curlyPartner maps an opening curly quote to the closing partner that likely
// ends the intended string on the same line.
var curlyPartner = map[string]string{
	"“": "”",
	"‘": "’",
	"„": "“",
}

func describeRepl(good string) string {
	switch good {
	case "":
		return "nothing there"
	case " ":
		return "a plain space"
	default:
		return fmt.Sprintf("plain %q", good)
	}
}

// enrichDedent reconstructs the indentation widths in effect above the
// offending line and proposes aligning to the nearest enclosing block.
func enrichDedent(d *Diagnostic, src string) {
	widths := indentWidthsAbove(src, d.Pos.Line)
	got := d.Pos.Col - 1
	d.Notes = append(d.Notes, fmt.Sprintf(
		"this line is indented %d space(s); enclosing blocks sit at %v", got, widths))
	// Nearest enclosing width strictly informs the fix; unique nearest → confident.
	best, bestDist, ties := -1, 1<<30, 0
	for _, w := range widths {
		dist := got - w
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			best, bestDist, ties = w, dist, 1
		} else if dist == bestDist {
			ties++
		}
	}
	if best >= 0 {
		d.Help = fmt.Sprintf("re-indent this line to %d space(s) to match its block", best)
		lineStart := d.Pos.Offset - got
		d.Fix = &Fix{Start: lineStart, End: d.Pos.Offset,
			Replacement: strings.Repeat(" ", best), Confident: ties == 1}
	}
}

// indentWidthsAbove returns the distinct indentation widths of content lines
// before the given line, in increasing order — an approximation of the
// lexer's indent stack that is good enough for a suggestion.
func indentWidthsAbove(src string, line int) []int {
	seen := map[int]bool{0: true}
	cur := 1
	width, counting, blank, inComment := 0, true, true, false
	flush := func() {
		if !blank {
			seen[width] = true
		}
		width, counting, blank, inComment = 0, true, true, false
	}
	for i := 0; i < len(src) && cur < line; i++ {
		switch c := src[i]; {
		case c == '\n':
			flush()
			cur++
		case inComment:
			// Inside a comment-only line: everything up to the newline is
			// comment text, not content, and must not reset blank or count
			// toward this line's width.
		case counting && c == ' ':
			width++
		case c == '#' && blank:
			inComment = true
			counting = false
		case c != '\r':
			counting = false
			blank = false
		}
	}
	var out []int
	for w := range seen {
		out = append(out, w)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Parse errors

var reMissingColon = regexp.MustCompile(`expected ':' after keyword "([^"]+)"`)

func parseDiags(err error, src string) []Diagnostic {
	var errs parser.ErrorList
	switch e := err.(type) {
	case parser.ErrorList:
		errs = e
	case *parser.Error:
		errs = parser.ErrorList{e}
	default:
		return []Diagnostic{{Severity: Error, Code: "syntax", Msg: err.Error()}}
	}
	out := make([]Diagnostic, 0, len(errs))
	for _, pe := range errs {
		out = append(out, parseDiag(pe, src))
	}
	return out
}

func parseDiag(pe *parser.Error, src string) Diagnostic {
	d := Diagnostic{
		Severity: Error, Code: "syntax",
		Pos: pe.Pos, Msg: pe.Msg,
		LineText: lineAt(src, pe.Pos.Line),
	}
	switch {
	case reMissingColon.MatchString(pe.Msg):
		enrichMissingColon(&d, src, reMissingColon.FindStringSubmatch(pe.Msg)[1])

	case strings.Contains(pe.Msg, "expected a keyword"):
		d.Help = "every pipeline line starts with a themed keyword, e.g. `Cursed Technique: Split Text by \"\\n\"`"

	case strings.Contains(pe.Msg, "unexpected dedent"):
		d.Help = "this line dedents past every open block; check the indentation of the lines above"

	case strings.Contains(pe.Msg, "must be followed by an indented body"),
		strings.Contains(pe.Msg, "must be followed by an indented sub-pipeline"):
		d.Help = "indent the following lines by four spaces to form the body"

	case strings.Contains(pe.Msg, "expected NEWLINE"):
		d.Help = "unexpected trailing content — each statement ends at the newline"

	case strings.Contains(pe.Msg, "expected an argument value"):
		d.Help = "argument values are a string, an integer, a name, or a lambda like `(x) -> x * 2`"

	case strings.Contains(pe.Msg, "expected an integer after '-'"):
		d.Help = "only integer literals may be negated here"
	}
	return d
}

// enrichMissingColon handles the classic `Reveal stdout` mistake. The parser
// swallowed every word into the keyword; if a known keyword is a prefix of
// those words, the colon belongs right after that prefix.
func enrichMissingColon(d *Diagnostic, src string, keyword string) {
	words := strings.Fields(keyword)
	var match string
	for _, k := range knownKeywords() {
		kw := strings.Fields(k)
		if phraseStartsWith(words, kw) && len(k) > len(match) {
			match = k
		}
	}
	if match == "" {
		d.Help = "every pipeline line is `Keyword: operation` — add a ':' after the keyword"
		return
	}
	// Find the end of the matched prefix in the source line and insert ':'.
	line := d.LineText
	idx := strings.Index(strings.ToLower(line), strings.ToLower(match))
	if idx < 0 {
		d.Help = fmt.Sprintf("add a ':' after %q", match)
		return
	}
	lineStart := d.Pos.Offset - (d.Pos.Col - 1)
	at := lineStart + idx + len(match)
	d.Help = fmt.Sprintf("add a ':' after %q — e.g. `%s: %s`",
		match, match, strings.TrimSpace(line[idx+len(match):]))
	d.Fix = &Fix{Start: at, End: at, Replacement: ":", Confident: true}
}

// ---------------------------------------------------------------------------
// Resolve errors

var (
	reUnknownKeyword = regexp.MustCompile(`^unknown keyword "([^"]+)"`)
	// The raw phrase may itself contain quotes (`Split by "\n"`), so the
	// first group is greedy and the anchor is the literal ` under "` that
	// unknownOpMessage always emits after it.
	reUnknownOp      = regexp.MustCompile(`unknown operation "(.*)" under "([^"]+)"(?:; known operations: (.*))?$`)
	reUnknownShiki   = regexp.MustCompile(`unknown Shikigami "([^"]+)"`)
	reUnknownChannel = regexp.MustCompile(`unknown channel "([^"]+)" in From:`)
	reTypeMismatch   = regexp.MustCompile(`^(.+?) expects input of type (.+), but the pipeline produced (.+)$`)
	reExpectsGot     = regexp.MustCompile(`^(.+?) expects (.+?), got (.+)$`)
)

func resolveDiags(err error, prog *ast.Program, src string) []Diagnostic {
	re, ok := err.(*prims.ResolveError)
	if !ok {
		return []Diagnostic{{Severity: Error, Code: "resolve", Msg: err.Error()}}
	}
	d := Diagnostic{
		Severity: Error, Code: "resolve",
		Pos: re.Pos, Msg: re.Msg,
		LineText: lineAt(src, re.Pos.Line),
	}
	msg := re.Msg
	switch {
	case reUnknownKeyword.MatchString(msg):
		d.Code = "name"
		enrichUnknownKeyword(&d, src, reUnknownKeyword.FindStringSubmatch(msg)[1])

	case reUnknownOp.MatchString(msg):
		d.Code = "name"
		m := reUnknownOp.FindStringSubmatch(msg)
		enrichUnknownOp(&d, prog, src, m[1], m[2], m[3])

	case reUnknownShiki.MatchString(msg):
		d.Code = "name"
		got := reUnknownShiki.FindStringSubmatch(msg)[1]
		names := prims.PreludeNames()
		for _, def := range prog.Shikigamis {
			names = append(names, def.Name)
		}
		if s, _ := closest(got, names); s != "" {
			d.Help = fmt.Sprintf("did you mean %q?", s)
			if fix := replaceWordInLine(&d, src, got, s); fix != nil {
				d.Fix = fix
			}
		} else {
			d.Help = "define it with `Shikigami \"Name\"` above this line, or check docs/language.md for the prelude"
		}

	case reUnknownChannel.MatchString(msg):
		d.Code = "name"
		got := reUnknownChannel.FindStringSubmatch(msg)[1]
		var names []string
		for _, s := range prog.Statements {
			if s.Keyword == "Channel" && s.ChannelName != "" {
				names = append(names, s.ChannelName)
			}
		}
		if s, dist := closest(got, names); s != "" {
			d.Help = fmt.Sprintf("did you mean channel %q?", s)
			if fix := replaceWordAfterStmt(&d, src, got, s); fix != nil {
				fix.Confident = dist <= 2
				d.Fix = fix
			}
		} else if len(names) == 0 {
			d.Help = "no Channels are defined; open one with `Channel \"name\":` and an indented sub-pipeline"
		} else {
			d.Notes = append(d.Notes, "defined channels: "+strings.Join(names, ", "))
		}

	case reTypeMismatch.MatchString(msg):
		d.Code = "type"
		m := reTypeMismatch.FindStringSubmatch(msg)
		enrichTypeMismatch(&d, m[1], m[2], m[3])

	case reExpectsGot.MatchString(msg):
		d.Code = "type"
		m := reExpectsGot.FindStringSubmatch(msg)
		enrichTypeMismatch(&d, m[1], m[2], m[3])

	case strings.Contains(msg, "requires a Using: lambda"):
		d.Help = "add an indented argument line under this statement, e.g. `Using: (x) -> x * 2`"

	case strings.Contains(msg, "requires a separator string"):
		d.Help = `give the separator as a string, e.g. Split Text by "\n"`

	case strings.Contains(msg, "has no operation"):
		d.Help = "write the operation after the colon, e.g. `Cursed Technique: Split Text by \"\\n\"`"

	case strings.Contains(msg, "is already defined"):
		d.Help = "each Channel name may be defined once; rename one of the definitions"

	case strings.Contains(msg, "requires start coordinates"):
		d.Help = "give the start as two integers, e.g. `BFS from 0 0`"

	case strings.Contains(msg, "inlining too deep"):
		d.Help = "a Shikigami cannot call itself (directly or indirectly); unroll the recursion into a Simple Domain loop"
	}
	return []Diagnostic{d}
}

func enrichUnknownKeyword(d *Diagnostic, src string, got string) {
	d.Msg = fmt.Sprintf("unknown keyword %q", got)
	s, confident := suggestKeyword(got)
	if s == "" {
		d.Help = "known keywords: " + strings.Join(knownKeywords(), ", ")
		return
	}
	d.Help = fmt.Sprintf("did you mean %q?", s)
	// Replace the source text from the statement start up to the colon.
	rel, kwEnd, ok := keywordSpan(d)
	if !ok {
		return
	}
	lineStart := d.Pos.Offset - rel
	d.EndCol = kwEnd + 1
	d.Fix = &Fix{Start: lineStart + rel, End: lineStart + kwEnd,
		Replacement: s, Confident: confident}
}

// keywordSpan locates the keyword text on the diagnostic's line: from the
// statement start column to just before the colon, trailing spaces trimmed.
// Returned values are 0-based indexes into the line.
func keywordSpan(d *Diagnostic) (start, end int, ok bool) {
	line := d.LineText
	rel := d.Pos.Col - 1
	if rel < 0 || rel >= len(line) {
		return 0, 0, false
	}
	colon := strings.Index(line[rel:], ":")
	if colon < 0 {
		return 0, 0, false
	}
	return rel, rel + len(strings.TrimRight(line[rel:rel+colon], " ")), true
}

func enrichUnknownOp(d *Diagnostic, prog *ast.Program, src, raw, keyword, known string) {
	d.Msg = fmt.Sprintf("unknown operation %q under %q", raw, keyword)
	if known != "" {
		d.Notes = append(d.Notes, fmt.Sprintf("operations available under %q: %s", keyword, known))
	}
	stmt := stmtAt(prog, d.Pos)
	var op *ast.Operation
	if stmt != nil {
		op = stmt.Op
	}
	s := suggestOperation(keyword, op)
	if s == nil {
		if known == "" {
			d.Help = "no operations are registered under this keyword; see docs/primitives.md for the vocabulary"
		}
		return
	}
	if s.Keyword != keyword {
		d.Help = fmt.Sprintf("%q is a %s operation — write `%s: %s`", s.Op, s.Keyword, s.Keyword, raw)
		// The repair swaps the keyword, leaving the phrase alone.
		if rel, kwEnd, ok := keywordSpan(d); ok {
			lineStart := d.Pos.Offset - rel
			d.EndCol = kwEnd + 1
			d.Fix = &Fix{Start: lineStart + rel, End: lineStart + kwEnd,
				Replacement: s.Keyword, Confident: s.Confident}
		}
		return
	}
	d.Help = fmt.Sprintf("did you mean %q?", s.Op)
	if op == nil {
		return
	}
	// Replace the leading words of the phrase with the corrected ID.
	n := len(strings.Fields(s.Op))
	start, end, ok := spanOfLeadingWords(src, op.Pos.Offset, n)
	if ok {
		d.Fix = &Fix{Start: start, End: end, Replacement: s.Op, Confident: s.Confident}
	}
}

func enrichTypeMismatch(d *Diagnostic, prim, want, got string) {
	d.Notes = append(d.Notes, fmt.Sprintf(
		"the value flowing into this line is %s, but %s needs %s", got, strings.TrimSpace(prim), want))
	if advice := adviseConversion(strings.TrimSpace(got), strings.TrimSpace(want)); advice != "" {
		d.Help = advice
	}
}

// spanOfLeadingWords returns the byte range covering the first n
// identifier words starting at offset.
func spanOfLeadingWords(src string, offset, n int) (int, int, bool) {
	i := offset
	end := offset
	for w := 0; w < n; w++ {
		for i < len(src) && src[i] == ' ' {
			i++
		}
		start := i
		for i < len(src) && (isIdentByte(src[i])) {
			i++
		}
		if i == start {
			return 0, 0, false
		}
		end = i
	}
	return offset, end, true
}

func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// replaceWordInLine builds a fix replacing the first whole-word occurrence of
// got on the diagnostic's line.
func replaceWordInLine(d *Diagnostic, src, got, repl string) *Fix {
	line := d.LineText
	lineStart := d.Pos.Offset - (d.Pos.Col - 1)
	if at := wordIndex(line, got); at >= 0 {
		return &Fix{Start: lineStart + at, End: lineStart + at + len(got),
			Replacement: repl, Confident: true}
	}
	return nil
}

// replaceWordAfterStmt builds a fix replacing the first whole-word occurrence
// of got in the statement's following lines (e.g. an indented `From:` line).
func replaceWordAfterStmt(d *Diagnostic, src, got, repl string) *Fix {
	from := d.Pos.Offset
	seg := src[from:]
	// Search at most the next 20 lines — a statement block is small.
	if cut := nthNewline(seg, 20); cut >= 0 {
		seg = seg[:cut]
	}
	if at := wordIndex(seg, got); at >= 0 {
		return &Fix{Start: from + at, End: from + at + len(got), Replacement: repl}
	}
	return nil
}

func nthNewline(s string, n int) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if n--; n == 0 {
				return i
			}
		}
	}
	return -1
}

// wordIndex finds s as a whole word (identifier boundaries) inside text.
func wordIndex(text, s string) int {
	for at := 0; ; {
		i := strings.Index(text[at:], s)
		if i < 0 {
			return -1
		}
		i += at
		beforeOK := i == 0 || !isIdentByte(text[i-1])
		afterOK := i+len(s) >= len(text) || !isIdentByte(text[i+len(s)])
		if beforeOK && afterOK {
			return i
		}
		at = i + 1
	}
}

// stmtAt finds the statement whose position matches pos, searching nested
// blocks and Shikigami bodies.
func stmtAt(prog *ast.Program, pos token.Position) *ast.Statement {
	var find func(stmts []*ast.Statement) *ast.Statement
	find = func(stmts []*ast.Statement) *ast.Statement {
		for _, s := range stmts {
			if s.Pos.Offset == pos.Offset {
				return s
			}
			if hit := find(s.Block); hit != nil {
				return hit
			}
		}
		return nil
	}
	if hit := find(prog.Statements); hit != nil {
		return hit
	}
	for _, def := range prog.Shikigamis {
		if hit := find(def.Body); hit != nil {
			return hit
		}
	}
	return nil
}
