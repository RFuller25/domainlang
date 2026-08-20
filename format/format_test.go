package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

func mustFormat(t *testing.T, src string) string {
	t.Helper()
	got, err := Format(src)
	if err != nil {
		t.Fatalf("Format(%q) error: %v", src, err)
	}
	return got
}

func TestNormalizations(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"indent to four spaces",
			"Cursed Energy: x\nCursed Technique: Map Each\n  Using: (x) -> x\nReveal: stdout\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (x) -> x\nReveal: stdout\n",
		},
		{
			"over-indented block pulled back",
			"Cursed Energy: x\nCursed Technique: Map Each\n        Using: (x) -> x\nReveal: stdout\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (x) -> x\nReveal: stdout\n",
		},
		{
			"nested depth",
			"Cursed Energy: x\nSimple Domain: Repeat 3\n  Cursed Technique: Map Each\n      Using: (x) -> x\nReveal: stdout\n",
			"Cursed Energy: x\nSimple Domain: Repeat 3\n    Cursed Technique: Map Each\n        Using: (x) -> x\nReveal: stdout\n",
		},
		{
			"keyword colon gap",
			"Cursed Energy:    x\nReveal:stdout\n",
			"Cursed Energy: x\nReveal: stdout\n",
		},
		{
			"keyword internal whitespace",
			"Cursed    Technique: Unique\n",
			"Cursed Technique: Unique\n",
		},
		{
			"argument line canonicalized",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using:(a,b)->a+b\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (a, b) -> a + b\n",
		},
		{
			"argument line operators spaced",
			"Cursed Energy: x\nMaximum Technique: Count Matching\n    Using: (x)->x>2 and x<10\n",
			"Cursed Energy: x\nMaximum Technique: Count Matching\n    Using: (x) -> x > 2 and x < 10\n",
		},
		{
			"argument line field access and calls hug",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (g) -> sum( take( g , 2 ) ) + g . n\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (g) -> sum(take(g, 2)) + g.n\n",
		},
		{
			"unary minus hugs its operand",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (p) -> padd(p, point(-1, -1))\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (p) -> padd(p, point(-1, -1))\n",
		},
		{
			"binary minus keeps its spaces",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (g, r, c) -> at(g, r, cols(g)-1-c)\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (g, r, c) -> at(g, r, cols(g) - 1 - c)\n",
		},
		{
			// `and`/`or`/`if`/`then`/`else` lex as IDENT but are operators, not
			// function names, so their parenthesis must not be hugged.
			"word operators keep their space before a paren",
			"Cursed Energy: x\nCursed Technique: Filter\n    Using: (r) -> (r.a <= r.c) or (r.c <= r.a)\n",
			"Cursed Energy: x\nCursed Technique: Filter\n    Using: (r) -> (r.a <= r.c) or (r.c <= r.a)\n",
		},
		{
			"else keeps its space before a parenthesized arm",
			"Cursed Energy: x\nCursed Technique: Apply\n    Using: (v) -> if v = 0 then \".\" else (if v = 1 then \",\" else \"!\")\n",
			"Cursed Energy: x\nCursed Technique: Apply\n    Using: (v) -> if v = 0 then \".\" else (if v = 1 then \",\" else \"!\")\n",
		},
		{
			"minus after a word operator is unary",
			"Cursed Energy: x\nCursed Technique: Apply\n    Using: (v) -> if v = 0 then -1 else v\n",
			"Cursed Energy: x\nCursed Technique: Apply\n    Using: (v) -> if v = 0 then -1 else v\n",
		},
		{
			"trailing whitespace stripped",
			"Cursed Energy: x   \nReveal: stdout\t\n",
			"Cursed Energy: x\nReveal: stdout\n",
		},
		{
			"blank line runs collapsed",
			"Cursed Energy: x\n\n\n\nReveal: stdout\n",
			"Cursed Energy: x\n\nReveal: stdout\n",
		},
		{
			"leading and trailing blank lines dropped",
			"\n\nCursed Energy: x\nReveal: stdout\n\n\n",
			"Cursed Energy: x\nReveal: stdout\n",
		},
		{
			"missing final newline added",
			"Cursed Energy: x\nReveal: stdout",
			"Cursed Energy: x\nReveal: stdout\n",
		},
		{
			"empty file",
			"",
			"",
		},
		{
			"comment only file",
			"# just a note\n",
			"# just a note\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mustFormat(t, c.in); got != c.want {
				t.Errorf("Format:\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

// The phrase after a keyword is copied byte for byte, because prims reads
// Operation.Raw verbatim as a Shikigami call name and as a file target. These
// are the cases a token-rendering formatter would silently corrupt.
func TestPhraseInteriorIsVerbatim(t *testing.T) {
	cases := []struct{ name, in string }{
		{"slashed path", "Cursed Energy: data/day1.txt\nReveal: stdout\n"},
		{"dotted path", "Cursed Energy: ./day1.txt\nReveal: stdout\n"},
		{"numeric leading path", "Cursed Energy: 16_no_prefixes.input\nReveal: stdout\n"},
		{"shikigami call", "Cursed Energy: x\nShikigami: Top K Sum\n    k: 3\n"},
		{"modifier comma", "Cursed Energy: x\nDomain Expansion: Quicksort, Descending\n"},
		{"separator string with spaces", "Cursed Energy: x\nCursed Technique: Split Text by \"  \"\n"},
		{"vow phrase", "Cursed Energy: x\nBinding Vow: All Values > 0\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mustFormat(t, c.in)
			if got != c.in {
				t.Errorf("phrase was rewritten:\n got %q\nwant %q", got, c.in)
			}
		})
	}
}

func TestCommentHandling(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"header comment stays at column zero",
			"# a header\nCursed Energy: x\n",
			"# a header\nCursed Energy: x\n",
		},
		{
			"comment takes the depth of the line it documents",
			"Cursed Energy: x\nSimple Domain: Repeat 3\n# explain\n    Cursed Technique: Unique\n",
			"Cursed Energy: x\nSimple Domain: Repeat 3\n    # explain\n    Cursed Technique: Unique\n",
		},
		{
			"comment at end of file keeps the previous depth",
			"Cursed Energy: x\nSimple Domain: Repeat 3\n    Cursed Technique: Unique\n        # trailing note\n",
			"Cursed Energy: x\nSimple Domain: Repeat 3\n    Cursed Technique: Unique\n    # trailing note\n",
		},
		{
			"trailing comment gap preserved",
			"Cursed Energy: x      # the input\nReveal: stdout\n",
			"Cursed Energy: x      # the input\nReveal: stdout\n",
		},
		{
			"trailing comment gets at least one space",
			"Cursed Energy: x# the input\n",
			"Cursed Energy: x # the input\n",
		},
		{
			"hash inside a string is not a comment",
			"Cursed Energy: x\nMaximum Technique: Count Cells\n    Using: (c) -> c = \"#\"\n",
			"Cursed Energy: x\nMaximum Technique: Count Cells\n    Using: (c) -> c = \"#\"\n",
		},
		{
			"escaped quote before a hash",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (s) -> \"\\\"#\\\"\"\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Using: (s) -> \"\\\"#\\\"\"\n",
		},
		{
			"a technically comment is a comment",
			"technically a header\nCursed Energy: x\n",
			"technically a header\nCursed Energy: x\n",
		},
		{
			"a trailing technically comment keeps its gap",
			"Cursed Energy: x      technically the input\nReveal: stdout\n",
			"Cursed Energy: x      technically the input\nReveal: stdout\n",
		},
		{
			"a technically comment takes the depth of the line it documents",
			"Cursed Energy: x\nSimple Domain: Repeat 3\ntechnically explain\n    Cursed Technique: Unique\n",
			"Cursed Energy: x\nSimple Domain: Repeat 3\n    technically explain\n    Cursed Technique: Unique\n",
		},
		{
			"technically inside a string is not a comment",
			"Cursed Energy: x\nMaximum Technique: Count Cells\n    Using: (c) -> c = \"technically\"\n",
			"Cursed Energy: x\nMaximum Technique: Count Cells\n    Using: (c) -> c = \"technically\"\n",
		},
		{
			"a name that merely contains the word is not a comment",
			"Cursed Energy: x\nMaximum Technique: Count Matching\n    Consider technicallyOK As 1\n    Using: (n) -> n = technicallyOK\n",
			"Cursed Energy: x\nMaximum Technique: Count Matching\n    Consider technicallyOK As 1\n    Using: (n) -> n = technicallyOK\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mustFormat(t, c.in); got != c.want {
				t.Errorf("Format:\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

// A file the formatter cannot fully understand comes back untouched, so fmt can
// never make a broken program worse.
func TestBrokenSourceUnchanged(t *testing.T) {
	cases := []struct{ name, in string }{
		{"lex error: tab indent", "Cursed Energy: x\n\tReveal: stdout\n"},
		{"lex error: unterminated string", "Cursed Technique: Split Text by \"oops\n"},
		{"parse error: missing colon", "Reveal stdout\nCursed Energy:\n    :\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Format(c.in)
			if err == nil {
				t.Fatalf("expected an error for %q", c.in)
			}
			if got != c.in {
				t.Errorf("source was modified despite the error:\n got %q\nwant %q", got, c.in)
			}
		})
	}
}

// repoPrograms returns every .domain program shipped in the repository.
func repoPrograms(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{"../examples", "../challenges", "../testdata"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%s): %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".domain") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", path, err)
			}
			out[path] = string(b)
		}
	}
	if len(out) == 0 {
		t.Fatal("found no repository programs")
	}
	return out
}

func TestIdempotent(t *testing.T) {
	for path, src := range repoPrograms(t) {
		t.Run(path, func(t *testing.T) {
			once := mustFormat(t, src)
			twice := mustFormat(t, once)
			if once != twice {
				t.Errorf("Format is not idempotent for %s", path)
			}
		})
	}
}

// Every program in the repository is already formatted — otherwise `domain fmt
// --check` would fail on a clean checkout.
func TestRepoIsFormatted(t *testing.T) {
	for path, src := range repoPrograms(t) {
		t.Run(path, func(t *testing.T) {
			if got := mustFormat(t, src); got != src {
				t.Errorf("%s is not formatted; run `domain fmt -w` on it", path)
			}
		})
	}
}

// pipelineShape renders the resolved pipeline as a stable string, so two
// programs can be compared for having lowered to the same operations.
func pipelineShape(t *testing.T, src string) string {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		return "unresolved: " + err.Error()
	}
	var b strings.Builder
	var walk func(nodes []*ir.Node, depth int)
	walk = func(nodes []*ir.Node, depth int) {
		for _, n := range nodes {
			b.WriteString(strings.Repeat(" ", depth))
			b.WriteString(n.Prim + " | " + n.Display + " | " + n.Out.String() + "\n")
			if sub, _ := n.Meta["nodes"].([]*ir.Node); sub != nil {
				walk(sub, depth+1)
			}
		}
	}
	walk(pipe.Nodes, 0)
	return b.String()
}

// The property that makes the formatter trustworthy on a whitespace-significant
// language: formatting never changes the program that the source lowers to.
func TestFormattingPreservesTheProgram(t *testing.T) {
	for path, src := range repoPrograms(t) {
		t.Run(path, func(t *testing.T) {
			before := pipelineShape(t, src)
			after := pipelineShape(t, mustFormat(t, src))
			if before != after {
				t.Errorf("%s lowered differently after formatting:\n--- before ---\n%s\n--- after ---\n%s",
					path, before, after)
			}
		})
	}
}

// Formatting is robust to hostile whitespace: mangling every program's
// indentation and spacing, then formatting, must recover a program that lowers
// identically to the original.
func TestFormattingRecoversMangledWhitespace(t *testing.T) {
	for path, src := range repoPrograms(t) {
		t.Run(path, func(t *testing.T) {
			want := pipelineShape(t, src)
			mangled := mangle(src)
			got, err := Format(mangled)
			if err != nil {
				// Mangling can legitimately break a file (an inconsistent
				// dedent); the formatter must then leave it alone.
				if got != mangled {
					t.Errorf("errored but still modified the source")
				}
				return
			}
			if shape := pipelineShape(t, got); shape != want {
				t.Errorf("%s lowered differently after mangle+format:\n--- want ---\n%s\n--- got ---\n%s",
					path, want, shape)
			}
		})
	}
}

// mangle doubles every indentation level and adds trailing whitespace, leaving
// line structure (and therefore relative nesting) intact.
func mangle(src string) string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		lead := len(l) - len(strings.TrimLeft(l, " "))
		if lead > 0 {
			l = strings.Repeat(" ", lead*2) + l[lead:]
		}
		if strings.TrimSpace(l) != "" {
			l += "  "
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

// `Consider x As/Of …` lines: the head is canonical either way, an expression
// or lambda value is re-rendered like any argument's, and an operation phrase
// after `Of` is left exactly as written — the same rule statements follow, and
// for the same reason (a phrase is read as a Shikigami name and as a path).
func TestBindingLinesFormatted(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"head canonicalized and value re-rendered",
			"Cursed Energy: x\nCursed Technique: Map Each\n  consider   n   as   3+4\n  Using: (v) -> v + n\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Consider n As 3 + 4\n    Using: (v) -> v + n\n",
		},
		{
			"a lambda value is expression-layer text",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Consider f As (a,b)->a*b\n    Using: (v) -> f(v, 2)\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Consider f As (a, b) -> a * b\n    Using: (v) -> f(v, 2)\n",
		},
		{
			"an Of lambda too",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Consider t of (xs)->sum(xs)\n    Using: (v) -> v + t\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Consider t Of (xs) -> sum(xs)\n    Using: (v) -> v + t\n",
		},
		{
			"an Of phrase is verbatim",
			"Cursed Energy: x\nCursed Technique: Map Each\n  Consider t of Split Text by \"\\n\"\n  Using: (v) -> v\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Consider t Of Split Text by \"\\n\"\n    Using: (v) -> v\n",
		},
		{
			"an Of sub-pipeline keeps its body",
			"Cursed Energy: x\nCursed Technique: Map Each\n  Consider t of\n     Maximum Technique: Sum\n  Using: (v) -> v + t\n",
			"Cursed Energy: x\nCursed Technique: Map Each\n    Consider t Of\n        Maximum Technique: Sum\n    Using: (v) -> v + t\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mustFormat(t, c.in)
			if got != c.want {
				t.Errorf("Format:\n got %q\nwant %q", got, c.want)
			}
			if again := mustFormat(t, got); again != got {
				t.Errorf("not idempotent: %q then %q", got, again)
			}
		})
	}
}
