package parser

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/lexer"
)

func parse(t *testing.T, src string) *ast.Program {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

func TestParseDay1(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n\n"
Channeled Energy: Convert Each List to Integers
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
`
	prog := parse(t, src)
	if len(prog.Statements) != 6 {
		t.Fatalf("expected 6 statements, got %d", len(prog.Statements))
	}

	s0 := prog.Statements[0]
	if s0.Keyword != "Cursed Energy" {
		t.Fatalf("keyword: got %q", s0.Keyword)
	}
	if s0.Op.Raw != "input.txt" {
		t.Fatalf("raw: got %q", s0.Op.Raw)
	}

	split := prog.Statements[1]
	if len(split.Op.Strings) != 1 || split.Op.Strings[0] != "\n\n" {
		t.Fatalf("split separator: got %v", split.Op.Strings)
	}

	sortStmt := prog.Statements[3]
	if len(sortStmt.Op.Modifiers) != 1 || sortStmt.Op.Modifiers[0] != "Descending" {
		t.Fatalf("sort modifiers: got %v", sortStmt.Op.Modifiers)
	}

	sel := prog.Statements[4]
	if len(sel.Op.Ints) != 1 || sel.Op.Ints[0] != 3 {
		t.Fatalf("select K: got %v", sel.Op.Ints)
	}
	foundSum := false
	for _, m := range sel.Op.Modifiers {
		if m == "Sum" {
			foundSum = true
		}
	}
	if !foundSum {
		t.Fatalf("expected Sum modifier, got %v", sel.Op.Modifiers)
	}
}

func TestParseLambda(t *testing.T) {
	src := "Domain Expansion: All Pairs\n    Using: (a, b) -> a + b = 2020\n"
	prog := parse(t, src)
	stmt := prog.Statements[0]
	if len(stmt.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(stmt.Args))
	}
	la, ok := stmt.Args[0].Value.(ast.LambdaArg)
	if !ok {
		t.Fatalf("expected lambda arg, got %T", stmt.Args[0].Value)
	}
	if len(la.Lambda.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(la.Lambda.Params))
	}
	if _, ok := la.Lambda.Body.(*ast.BinaryExpr); !ok {
		t.Fatalf("expected binary expr body, got %T", la.Lambda.Body)
	}
}

func TestParseVowPredicate(t *testing.T) {
	prog := parse(t, "Binding Vow: All Values > 0\n")
	op := prog.Statements[0].Op
	if len(op.OpSyms) != 1 || op.OpSyms[0] != ">" {
		t.Fatalf("expected '>' op sym, got %v", op.OpSyms)
	}
	if len(op.Ints) != 1 || op.Ints[0] != 0 {
		t.Fatalf("expected int 0, got %v", op.Ints)
	}
}

func TestParseNestedBlock(t *testing.T) {
	src := "Channel:\n    Reveal: stdout\n"
	prog := parse(t, src)
	stmt := prog.Statements[0]
	if len(stmt.Block) != 1 {
		t.Fatalf("expected 1 nested statement, got %d", len(stmt.Block))
	}
	if stmt.Block[0].Keyword != "Reveal" {
		t.Fatalf("nested keyword: got %q", stmt.Block[0].Keyword)
	}
}

func TestParseChannelAndFrom(t *testing.T) {
	src := "Channel \"moves\":\n" +
		"    Cursed Technique: Take Item 1\n" +
		"Maximum Technique: Combine\n" +
		"    From: moves, rows\n" +
		"    Using: (m, r) -> m + r\n"
	prog := parse(t, src)

	ch := prog.Statements[0]
	if ch.Keyword != "Channel" || ch.ChannelName != "moves" {
		t.Fatalf("channel header: keyword=%q name=%q", ch.Keyword, ch.ChannelName)
	}
	if len(ch.Block) != 1 || ch.Block[0].Keyword != "Cursed Technique" {
		t.Fatalf("channel body: %+v", ch.Block)
	}

	combine := prog.Statements[1]
	var fromArg *ast.Arg
	for _, a := range combine.Args {
		if a.Name == "From" {
			fromArg = a
		}
	}
	if fromArg == nil {
		t.Fatal("missing From: arg")
	}
	idl, ok := fromArg.Value.(ast.IdentListArg)
	if !ok {
		t.Fatalf("From value type: %T", fromArg.Value)
	}
	if len(idl.Values) != 2 || idl.Values[0] != "moves" || idl.Values[1] != "rows" {
		t.Fatalf("From idents: %v", idl.Values)
	}
}

func TestParseShikigamiDef(t *testing.T) {
	src := "Shikigami \"Top K Sum\" (k: Int)\n" +
		"    Domain Expansion: Quicksort, Descending\n" +
		"    Maximum Technique: Select Top k, Sum\n" +
		"Reveal: stdout\n"
	prog := parse(t, src)

	if len(prog.Shikigamis) != 1 {
		t.Fatalf("expected 1 shikigami def, got %d", len(prog.Shikigamis))
	}
	def := prog.Shikigamis[0]
	if def.Name != "Top K Sum" {
		t.Fatalf("name: %q", def.Name)
	}
	if len(def.Params) != 1 || def.Params[0].Name != "k" || def.Params[0].Type != "Int" {
		t.Fatalf("params: %+v", def.Params)
	}
	if len(def.Body) != 2 {
		t.Fatalf("body length: %d", len(def.Body))
	}
	// The definition is not a pipeline statement; only `Reveal` is.
	if len(prog.Statements) != 1 || prog.Statements[0].Keyword != "Reveal" {
		t.Fatalf("statements: %+v", prog.Statements)
	}
}

func TestParseErrorMissingColon(t *testing.T) {
	toks, _ := lexer.Lex("Cursed Energy input.txt\n")
	if _, err := Parse("Cursed Energy input.txt\n", toks); err == nil {
		t.Fatal("expected parse error for missing colon")
	}
}

// TestParseErrorPositions asserts the *position*, not just the presence, of
// a parse error.
func TestParseErrorPositions(t *testing.T) {
	toks, err := lexer.Lex("Cursed Energy input.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse("Cursed Energy input.txt\n", toks)
	perr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *parser.Error, got %T", err)
	}
	if perr.Pos.Line != 1 {
		t.Fatalf("expected error on line 1, got line %d", perr.Pos.Line)
	}
}

// TestParseRecoveryReportsMultipleErrors pins §G's error recovery: two
// independent mistakes on different top-level lines are both reported (with
// their own positions), separated by a statement that parses fine — proof
// the parser resynchronized rather than giving up.
func TestParseRecoveryReportsMultipleErrors(t *testing.T) {
	src := "Cursed Energy input.txt\n" + // line 1: missing colon
		"Maximum Technique: Sum\n" + // fine
		"Reveal stdout\n" // line 3: missing colon again
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(src, toks)
	if err == nil {
		t.Fatal("expected parse errors")
	}
	list, ok := err.(ErrorList)
	if !ok {
		t.Fatalf("expected ErrorList for two errors, got %T: %v", err, err)
	}
	if len(list) != 2 {
		t.Fatalf("expected exactly 2 errors, got %d: %v", len(list), list)
	}
	if list[0].Pos.Line != 1 || list[1].Pos.Line != 3 {
		t.Fatalf("expected errors on lines 1 and 3, got %d and %d", list[0].Pos.Line, list[1].Pos.Line)
	}
	if !strings.Contains(err.Error(), "1:") || !strings.Contains(err.Error(), "3:") {
		t.Fatalf("aggregate message should carry both positions, got %q", err.Error())
	}
}

// TestParseRecoverySkipsBrokenStatementsBlock proves recovery jumps over a
// failed statement's own indented block instead of misreading it as new
// top-level statements.
func TestParseRecoverySkipsBrokenStatementsBlock(t *testing.T) {
	src := "Cursed Technique Filter\n" + // line 1: missing colon
		"    Using: (x) -> x > 0\n" + // its block: must be skipped silently
		"Reveal stdout\n" // line 3: second real error
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(src, toks)
	list, ok := err.(ErrorList)
	if !ok {
		t.Fatalf("expected ErrorList, got %T: %v", err, err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 errors (block skipped, not re-parsed), got %d: %v", len(list), list)
	}
	if list[1].Pos.Line != 3 {
		t.Fatalf("second error should be on line 3, got line %d", list[1].Pos.Line)
	}
}

// TestParseRecoveryNeverAcceptsABrokenProgram: recovery improves reporting,
// never acceptance — a program with any bad line still fails overall.
func TestParseRecoveryNeverAcceptsABrokenProgram(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Maximum Technique Sum\n" + // missing colon
		"Reveal: stdout\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Parse(src, toks)
	if err == nil {
		t.Fatal("a program with a broken line must not parse")
	}
	if prog != nil {
		t.Fatal("a failed parse must not hand back a partial program")
	}
	// A single error keeps its plain *Error type (no ErrorList wrapper).
	if _, ok := err.(*Error); !ok {
		t.Fatalf("single error should stay *Error, got %T", err)
	}
}

// TestParseRecoveryErrorCap: a file that is wrong on every line stops at the
// cap instead of producing an avalanche.
func TestParseRecoveryErrorCap(t *testing.T) {
	src := strings.Repeat("Broken Line Here\n", 25)
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(src, toks)
	list, ok := err.(ErrorList)
	if !ok {
		t.Fatalf("expected ErrorList, got %T", err)
	}
	if len(list) != maxParseErrors+1 {
		t.Fatalf("expected %d errors (cap + notice), got %d", maxParseErrors+1, len(list))
	}
	if !strings.Contains(list[len(list)-1].Msg, "too many errors") {
		t.Fatalf("last entry should be the cap notice, got %q", list[len(list)-1].Msg)
	}
}

// TestNegativeIntInPhrase is a regression test: parsePhrase had no MINUS case
// in either the primary or modifier segment, so a negative integer literal
// inside an operation phrase (e.g. a Binding Vow bound) silently lost its
// sign — the MINUS token was discarded and the following INT was recorded
// as positive, with no error.
func TestNegativeIntInPhrase(t *testing.T) {
	prog := parse(t, "Binding Vow: All Values > -5\n")
	op := prog.Statements[0].Op
	if len(op.Ints) != 1 || op.Ints[0] != -5 {
		t.Fatalf("expected Ints=[-5], got %v", op.Ints)
	}
}

func TestNegativeIntInModifierSegment(t *testing.T) {
	prog := parse(t, "Domain Expansion: Combinations 2, Offset -3\n")
	op := prog.Statements[0].Op
	found := false
	for _, n := range op.Ints {
		if n == -3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected -3 among Ints in a modifier segment, got %v", op.Ints)
	}
}

// TestFloatInPhraseIsAParseError is a regression test: parsePhrase's segment
// switches had no FLOAT case, so a decimal literal in an operation phrase
// (e.g. `Select Top 3.5`) was silently dropped from Words/Ints — the program
// parsed "successfully" with Ints=[] and the mistake only surfaced later as a
// resolver error that never mentioned the float. The phrase layer is
// integer-only by design (ast.Operation has no Floats field), so a FLOAT
// there must be an immediate parse error naming the literal and its position.
func TestFloatInPhraseIsAParseError(t *testing.T) {
	src := "Maximum Technique: Select Top 3.5, Sum\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := Parse(src, toks)
	if err == nil {
		t.Fatal("expected a parse error for a decimal literal in an operation phrase")
	}
	if prog != nil {
		t.Fatal("a failed parse must not hand back a partial program")
	}
	perr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *parser.Error, got %T: %v", err, err)
	}
	if !strings.Contains(perr.Msg, `"3.5"`) {
		t.Fatalf("error should name the float literal, got %q", perr.Msg)
	}
	// The position must point at the literal itself: line 1, at "3.5"
	// (column of the '3' in `Select Top 3.5`).
	wantCol := strings.Index(src, "3.5") + 1
	if perr.Pos.Line != 1 || perr.Pos.Col != wantCol {
		t.Fatalf("error position: got %d:%d, want 1:%d", perr.Pos.Line, perr.Pos.Col, wantCol)
	}
}

// A decimal in a modifier segment (after a comma) must be rejected too.
func TestFloatInModifierSegmentIsAParseError(t *testing.T) {
	src := "Domain Expansion: Combinations 2, Offset 1.5\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	if _, err := Parse(src, toks); err == nil {
		t.Fatal("expected a parse error for a decimal literal in a modifier segment")
	}
}

// TestNegativeIntArgValue is a regression test: parseArgValue had no MINUS
// case at all, so a negative integer as a named-argument value (e.g.
// `Threshold: -5`) was a hard parse error ("expected an argument value, got
// MINUS") instead of parsing to IntArg{-5}.
func TestNegativeIntArgValue(t *testing.T) {
	src := "Cursed Technique: Filter\n    Using: (n) -> n > 0\n    Threshold: -5\n"
	prog := parse(t, src)
	var thresh *ast.Arg
	for _, a := range prog.Statements[0].Args {
		if a.Name == "Threshold" {
			thresh = a
		}
	}
	if thresh == nil {
		t.Fatal("missing Threshold: arg")
	}
	ia, ok := thresh.Value.(ast.IntArg)
	if !ok {
		t.Fatalf("expected IntArg, got %T", thresh.Value)
	}
	if ia.Value != -5 {
		t.Fatalf("got %d want -5", ia.Value)
	}
}

func TestNegativeIntArgValueRequiresDigit(t *testing.T) {
	src := "Cursed Technique: Filter\n    Threshold: -x\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(src, toks); err == nil {
		t.Fatal("expected an error for '-' not followed by a digit in an argument value")
	}
}

// TestIntegerOverflowIsAnError is a regression test: parseInt used to
// accumulate digits with `n = n*10 + digit` and no overflow check, so an
// arbitrarily long integer literal would silently wrap around int64 instead
// of producing an error.
func TestIntegerOverflowIsAnError(t *testing.T) {
	src := "Cursed Energy: stdin\nBinding Vow: Count Equals 99999999999999999999\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(src, toks); err == nil {
		t.Fatal("expected an out-of-range error for a 20-digit integer literal")
	}
}

func TestIntegerAtInt64Boundary(t *testing.T) {
	src := "Binding Vow: Count Equals 9223372036854775807\n" // math.MaxInt64
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Parse(src, toks)
	if err != nil {
		t.Fatalf("MaxInt64 literal should parse cleanly: %v", err)
	}
	if prog.Statements[0].Op.Ints[0] != 9223372036854775807 {
		t.Fatalf("got %d", prog.Statements[0].Op.Ints[0])
	}
}

func TestNegativeIntegerAtMinInt64Boundary(t *testing.T) {
	src := "Binding Vow: Count Equals -9223372036854775808\n" // math.MinInt64
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Parse(src, toks)
	if err != nil {
		t.Fatalf("MinInt64 literal should parse cleanly: %v", err)
	}
	if prog.Statements[0].Op.Ints[0] != -9223372036854775808 {
		t.Fatalf("got %d", prog.Statements[0].Op.Ints[0])
	}
}

func TestNegativeIntLitAtMinInt64BoundaryInExpr(t *testing.T) {
	// Exercises the Pratt-parser unary-minus path (parseUnary in expr.go),
	// which folds a leading MINUS immediately followed by INT into a single
	// signed IntLit rather than UnaryExpr{MINUS, IntLit}.
	src := "Domain Expansion: All Pairs\n    Using: (a) -> a = -9223372036854775808\n"
	prog := parse(t, src)
	stmt := prog.Statements[0]
	la, ok := stmt.Args[0].Value.(ast.LambdaArg)
	if !ok {
		t.Fatalf("expected lambda arg, got %T", stmt.Args[0].Value)
	}
	bin, ok := la.Lambda.Body.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected binary expr body, got %T", la.Lambda.Body)
	}
	lit, ok := bin.Right.(*ast.IntLit)
	if !ok {
		t.Fatalf("expected int literal on the right, got %T", bin.Right)
	}
	if lit.Value != -9223372036854775808 {
		t.Fatalf("got %d", lit.Value)
	}
}

func TestMalformedLambdaErrors(t *testing.T) {
	cases := []string{
		"X:\n    Using: (a, b -> a + b\n",  // missing ')'
		"X:\n    Using: (a, b) a + b\n",    // missing '->'
		"X:\n    Using: (a, ) -> a\n",      // trailing comma, no param
		"X:\n    Using: (a, b) ->\n",       // missing body
		"X:\n    Using: (x, x) -> x + 1\n", // duplicate param name
	}
	for _, src := range cases {
		toks, err := lexer.Lex(src)
		if err != nil {
			continue // a lex error also satisfies "this is malformed"
		}
		if _, err := Parse(src, toks); err == nil {
			t.Fatalf("expected a parse error for %q", src)
		}
	}
}

// TestDuplicateLambdaParamRejected is a regression test: a lambda with a
// repeated parameter name (e.g. "(x, x) -> x + 1") used to parse successfully.
// Both LambdaType (typecheck) and EvalLambda (eval) build their Env by
// iterating l.Params and assigning env[p] = ..., so a duplicate name silently
// shadowed the earlier occurrence, making it permanently inaccessible with no
// error anywhere in the pipeline (e.g. a 2-arg fold lambda like
// "(acc, acc) -> acc + 1" would silently ignore its first argument). The
// parser now rejects duplicate parameter names at parse time, the earliest
// point in the pipeline, so every consumer is protected.
func TestDuplicateLambdaParamRejected(t *testing.T) {
	src := "X:\n    Using: (acc, acc) -> acc + 1\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	_, err = Parse(src, toks)
	if err == nil {
		t.Fatal("expected a parse error for a lambda with a duplicate parameter name")
	}
	if !strings.Contains(err.Error(), "duplicate lambda parameter") {
		t.Fatalf("expected error to mention 'duplicate lambda parameter', got: %v", err)
	}
	if !strings.Contains(err.Error(), `"acc"`) {
		t.Fatalf("expected error to name the offending parameter %q, got: %v", "acc", err)
	}
}

func TestMalformedShikigamiParamList(t *testing.T) {
	cases := []string{
		"Shikigami \"X\" (k Int)\n    Reveal: stdout\n", // missing ':'
		"Shikigami \"X\" (k: )\n    Reveal: stdout\n",   // missing type
		"Shikigami \"X\" (k: Int\n    Reveal: stdout\n", // missing ')'
		"Shikigami \"X\" (: Int)\n    Reveal: stdout\n", // missing name
	}
	for _, src := range cases {
		toks, err := lexer.Lex(src)
		if err != nil {
			continue
		}
		if _, err := Parse(src, toks); err == nil {
			t.Fatalf("expected a parse error for %q", src)
		}
	}
}

func TestFromWithTrailingCommaIsAnError(t *testing.T) {
	src := "Maximum Technique: Combine\n    From: moves,\n    Using: (m) -> m\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(src, toks); err == nil {
		t.Fatal("expected an error for From: with a trailing comma and no following identifier")
	}
}

// TestMixingArgsAndBlockStatements documents that a block may contain both
// named-argument lines and nested pipeline statements (e.g. `Simple Domain:
// While` carries both a Using: predicate and a body sub-pipeline).
func TestMixingArgsAndBlockStatements(t *testing.T) {
	src := "Simple Domain: While\n" +
		"    Using: (v) -> v > 0\n" +
		"    Cursed Technique: Apply\n" +
		"        Using: (v) -> v\n"
	prog := parse(t, src)
	stmt := prog.Statements[0]
	if len(stmt.Args) != 1 {
		t.Fatalf("expected 1 named arg (Using), got %d", len(stmt.Args))
	}
	if len(stmt.Block) != 1 {
		t.Fatalf("expected 1 nested pipeline statement, got %d", len(stmt.Block))
	}
}

func TestParseIntArgValueOnEOF(t *testing.T) {
	// A dangling '-' at end of input with nothing after it.
	src := "Cursed Technique: Filter\n    Threshold: -"
	toks, err := lexer.Lex(src)
	if err != nil {
		return // lexer rejecting this outright is also acceptable
	}
	if _, err := Parse(src, toks); err == nil {
		t.Fatal("expected an error for a dangling '-' with no following digit")
	}
}
