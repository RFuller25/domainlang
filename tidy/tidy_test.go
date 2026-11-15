package tidy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// run executes a program against an input and returns what it printed.
func run(t *testing.T, src, input string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "p.domain")
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v\n%s", err, src)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	pipe, err := prims.ResolveWith(prog, prims.FileOptions(path))
	if err != nil {
		t.Fatalf("resolve: %v\n%s", err, src)
	}
	var out bytes.Buffer
	if _, err := interp.RunScoped(pipe, &ir.Context{Stdout: &out, BaseDir: dir}); err != nil {
		t.Fatalf("run: %v\n%s", err, src)
	}
	return out.String()
}

// tidied applies the rewrites and checks the one promise they all make: the
// program prints what it printed before.
func tidied(t *testing.T, src, input string) Result {
	t.Helper()
	before := run(t, src, input)
	res := Apply("", src, Options{})
	if after := run(t, res.Src, input); after != before {
		t.Fatalf("output changed\n--- before\n%s--- after\n%s--- program\n%s", before, after, res.Src)
	}
	// And a second pass finds nothing left to do.
	if again := Apply("", res.Src, Options{}); again.Changed() {
		t.Fatalf("not idempotent: %v\n%s\n--- then\n%s", again.Applied, res.Src, again.Src)
	}
	return res
}

func wantContains(t *testing.T, got string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
}

func wantAbsent(t *testing.T, got string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(got, s) {
			t.Errorf("unexpected %q in:\n%s", s, got)
		}
	}
}

const jumps = "0\n3\n0\n1\n-3\n"

func TestDestructureFromConsiders(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Apply
    Using: (js) -> tuple(js, 0, 0)
Simple Domain: While
    Using: (st) -> item(st, 1) >= 0 and item(st, 1) < length(item(st, 0))
    Cursed Technique: Apply
        Using: (st) ->
            consider js as item(st, 0) in
            consider i as item(st, 1) in
            consider steps as item(st, 2) in
            consider j as item(js, i) in
            tuple(set(js, i, j + 1), i + j, steps + 1)
Cursed Technique: Apply
    Using: (st) -> item(st, 2)
Reveal: stdout
`
	res := tidied(t, src, jumps)
	wantContains(t, res.Src,
		"Using: ((js, i, _)) -> i >= 0 and i < length(js)",
		"Using: ((js, i, steps)) ->\n            consider j as item(js, i) in",
		"Using: ((_, _, steps)) -> steps",
	)
	wantAbsent(t, res.Src, "item(st")
	if run(t, res.Src, jumps) != "5\n" {
		t.Errorf("wrong answer")
	}
}

func TestDestructureLeavesUnnamedFieldsAlone(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Apply
    Using: (js) -> tuple(js, 0, 7)
Cursed Technique: Apply
    Using: (st) -> item(st, 2) + length(item(st, 0))
Reveal: stdout
`
	res := tidied(t, src, jumps)
	if res.Changed() {
		t.Fatalf("rewrote with no names to use:\n%s", res.Src)
	}
	if len(res.Notes) == 0 || !strings.Contains(res.Notes[0], "still indexed") {
		t.Errorf("no note: %v", res.Notes)
	}
}

func TestDestructureKeepsAWholeUse(t *testing.T) {
	// st is passed on whole, so it has to keep its name.
	src := `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Apply
    Using: (js) -> tuple(js, 0)
Cursed Technique: Apply
    Using: (st) -> consider n as item(st, 1) in if n = 0 then st else st
Cursed Technique: Apply
    Using: (st) -> consider n as item(st, 1) in n
Reveal: stdout
`
	res := tidied(t, src, jumps)
	wantContains(t, res.Src, "(st) -> consider n as item(st, 1) in if n = 0", "((_, n)) -> n")
}

func TestDestructureRespectsNameClashes(t *testing.T) {
	// `n` already means the global outside the consider's body.
	src := `Cursed Energy: input.txt
Shikigami: Ints
Cursed Object: n As 100
Cursed Technique: Apply
    Using: (js) -> tuple(js, 5)
Cursed Technique: Apply
    Using: (st) -> n + (consider n as item(st, 1) in n)
Reveal: stdout
`
	res := tidied(t, src, jumps)
	if res.Changed() {
		t.Fatalf("hid a global:\n%s", res.Src)
	}
}

func TestDestructureBesideAnotherParameter(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Enumerate
Maximum Technique: Fold
    Seed: 0
    Using: (acc, p) -> consider i as item(p, 0) in consider x as item(p, 1) in acc + i * x
Reveal: stdout
`
	res := tidied(t, src, jumps)
	wantContains(t, res.Src, "Using: (acc, (i, x)) -> acc + i * x")
}

func TestMergeCases(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Case: inc "{r:word} inc {n:int} if {c:word}"
    Case: dec "{r:word} dec {n:int} if {c:word}"
Cursed Technique: Map Each
    Using: (x) -> if x.kind = "inc" then x.n else 0 - x.n
Maximum Technique: Sum
Reveal: stdout
`
	res := tidied(t, src, "a inc 5 if b\nc dec 2 if a\nb inc 10 if c")
	wantContains(t, res.Src, `Using: "{r:word} {kind:word} {n:int} if {c:word}"`)
	wantAbsent(t, res.Src, "Case:")
}

func TestMergeCasesNeedsTagToBeTheWord(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Case: up "{r:word} inc {n:int}"
    Case: down "{r:word} dec {n:int}"
Cursed Technique: Map Each
    Using: (x) -> if x.kind = "up" then x.n else 0 - x.n
Maximum Technique: Sum
Reveal: stdout
`
	if res := tidied(t, src, "a inc 5\nc dec 2"); res.Changed() {
		t.Fatalf("merged cases whose tags are not their words:\n%s", res.Src)
	}
}

func TestMergeCasesLeavesTryAlone(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Try
    Case: inc "{r:word} inc {n:int}"
    Case: dec "{r:word} dec {n:int}"
Maximum Technique: Count
Reveal: stdout
`
	if res := tidied(t, src, "a inc 5\nc dec 2\nq nop 1"); res.Changed() {
		t.Fatalf("merged under Mode: Try:\n%s", res.Src)
	}
}

func TestLateGlobalBecomesConstant(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Object: n As 0
Shikigami: Ints
Cursed Tool: n Of Count
Cursed Technique: Map Each
    Using: (x) -> x + n
Maximum Technique: Sum
Reveal: stdout
`
	res := tidied(t, src, jumps)
	wantContains(t, res.Src, "Cursed Object: n Of Count")
	wantAbsent(t, res.Src, "Cursed Tool", "n As 0")
}

func TestLateGlobalReadInBetweenStays(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Object: n As 10
Shikigami: Ints
Cursed Technique: Map Each
    Using: (x) -> x + n
Cursed Tool: n Of Count
Cursed Technique: Map Each
    Using: (x) -> x + n
Maximum Technique: Sum
Reveal: stdout
`
	res := tidied(t, src, jumps)
	if res.Changed() {
		t.Fatalf("dropped a value that was read:\n%s", res.Src)
	}
	if len(res.Notes) == 0 {
		t.Errorf("a written global should be noted")
	}
}

func TestHoistSharedPrefix(t *testing.T) {
	src := `Cursed Energy: input.txt

# the first answer
Part "1":
    # read the numbers
    Shikigami: Ints
    Cursed Technique: Map Each
        Using: (x) -> x * 2
    Maximum Technique: Sum
    Reveal: stdout

Part "2":
    Shikigami: Ints
    Cursed Technique: Map Each
        Using: (x) -> x * 2
    Maximum Technique: Max
    Reveal: stdout
`
	res := tidied(t, src, jumps)
	want := `Cursed Energy: input.txt

# read the numbers
Shikigami: Ints
Cursed Technique: Map Each
    Using: (x) -> x * 2

# the first answer
Part "1":
    Maximum Technique: Sum
    Reveal: stdout

Part "2":
    Maximum Technique: Max
    Reveal: stdout
`
	if res.Src != want {
		t.Errorf("got:\n%s\nwant:\n%s", res.Src, want)
	}
}

func TestHoistStopsAtADifference(t *testing.T) {
	src := `Cursed Energy: input.txt
Part "1":
    Shikigami: Ints
    Cursed Technique: Map Each
        Using: (x) -> x * 2
    Maximum Technique: Sum
    Reveal: stdout
Part "2":
    Shikigami: Ints
    Cursed Technique: Map Each
        Using: (x) -> x * 3
    Maximum Technique: Sum
    Reveal: stdout
`
	res := tidied(t, src, jumps)
	wantContains(t, res.Src, "Shikigami: Ints\n\nPart \"1\":\n    Cursed Technique: Map Each")
	if strings.Count(res.Src, "Shikigami: Ints") != 1 || strings.Count(res.Src, "Map Each") != 2 {
		t.Errorf("hoisted the wrong amount:\n%s", res.Src)
	}
}

func TestHoistNeverMovesAReveal(t *testing.T) {
	src := `Cursed Energy: input.txt
Part "1":
    Reveal: stdout
    Shikigami: Ints
    Maximum Technique: Sum
    Reveal: stdout
Part "2":
    Reveal: stdout
    Shikigami: Ints
    Maximum Technique: Max
    Reveal: stdout
`
	if res := tidied(t, src, jumps); res.Changed() {
		t.Fatalf("moved a labelled print:\n%s", res.Src)
	}
}

func TestHoistNeedsOnlyPartsAfterTheFirst(t *testing.T) {
	src := `Cursed Energy: input.txt
Part "1":
    Shikigami: Ints
    Maximum Technique: Sum
    Reveal: stdout
Part "2":
    Shikigami: Ints
    Maximum Technique: Max
    Reveal: stdout
Reveal: stdout
`
	if res := tidied(t, src, jumps); res.Changed() {
		t.Fatalf("changed what the trailing Reveal prints:\n%s", res.Src)
	}
}

func TestKeywordSpellingFollowsTheMajority(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Ints
Map Each
    Using: (x) -> x * 2
Maximum Technique: Sum
Reveal: stdout
`
	res := tidied(t, src, jumps)
	wantContains(t, res.Src, "Cursed Technique: Map Each")

	bare := `input.txt
Ints
Map Each
    Using: (x) -> x * 2
Maximum Technique: Sum
stdout
`
	res = tidied(t, bare, jumps)
	wantContains(t, res.Src, "\nSum\n")
	wantAbsent(t, res.Src, "Maximum Technique")
}

func TestPhraseCase(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Apply
    Using: (xs) -> sum(xs)
Simple Domain: while
    Using: (v) -> v > 100
    Cursed Technique: apply
        Using: (v) -> v / 2
Simple Domain: While
    Using: (v) -> v > 100
    Cursed Technique: Apply
        Using: (v) -> v / 2
Reveal: stdout
`
	res := tidied(t, src, "400\n400\n")
	wantAbsent(t, res.Src, ": while", ": apply")
}

func TestBrokenProgramIsReturnedUntouched(t *testing.T) {
	src := "Cursed Energy: input.txt\nCursed Technique: Splt Text by\nReveal stdout\n"
	if res := Apply("", src, Options{Format: true}); res.Changed() || res.Src != src {
		t.Fatalf("rewrote a program that does not resolve")
	}
}

func TestFormatRunsLast(t *testing.T) {
	src := "Cursed Energy: input.txt   \nShikigami: Ints\nMaximum Technique: Sum\nReveal:   stdout"
	res := Apply("", src, Options{Format: true})
	if res.Src != "Cursed Energy: input.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout" {
		t.Errorf("got %q (%v)", res.Src, res.Applied)
	}
}

func TestSuggestVows(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("3\n1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `Cursed Energy: input.txt
Shikigami: Ints

Part "1":
    Maximum Technique: Sum
    Reveal: stdout

Part "2":
    Maximum Technique: Max
    Reveal: stdout
`
	// The cursor is inside a Part, so the vows are about the end of the parse.
	offer, err := SuggestVowsFor(filepath.Join(dir, "p.domain"), src, 5, dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if offer.AfterLine != 2 || offer.Stage != "Shikigami: Ints" {
		t.Errorf("target = line %d %q", offer.AfterLine, offer.Stage)
	}
	var got []string
	for _, v := range offer.Vows {
		got = append(got, strings.Join(v.Lines, "|"))
	}
	joined := strings.Join(got, "\n")
	wantContains(t, joined, "Binding Vow: Count Equals 3", "Binding Vow: All Values > 0")

	// Each one, inserted where it says, leaves the program running.
	lines := strings.Split(src, "\n")
	for _, v := range offer.Vows {
		with := append(append(append([]string{}, lines[:offer.AfterLine]...), v.Lines...), lines[offer.AfterLine:]...)
		if out := run(t, strings.Join(with, "\n"), "3\n1\n2\n"); out != "Part 1: 6\nPart 2: 3\n" {
			t.Errorf("with %v: %q", v.Lines, out)
		}
	}
}

func TestSuggestVowsForAGrid(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("#.\n.#\n"), 0o644)
	src := "Cursed Energy: input.txt\nCursed Technique: Split Text by \"\\n\"\nChanneled Energy: Convert To Grid\nMaximum Technique: Count Cells\n    Using: (c) -> c = \"#\"\nReveal: stdout\n"
	offer, err := SuggestVowsFor(filepath.Join(dir, "p.domain"), src, 1, dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if offer.AfterLine != 3 {
		t.Errorf("after line %d", offer.AfterLine)
	}
	wantContains(t, strings.Join(offer.Vows[0].Lines, "\n"), "rows(g) = 2 and cols(g) = 2")
}

// Every program in the repository survives a tidy: the result still resolves
// (Apply guarantees it), a second pass changes nothing, and the examples that
// carry an expected output still print it.
func TestCorpus(t *testing.T) {
	seen := 0
	filepath.Walk("..", func(p string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(p, ".domain") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		seen++
		res := Apply(p, string(b), Options{Format: true})
		if !res.Changed() {
			return nil
		}
		if again := Apply(p, res.Src, Options{Format: true}); again.Changed() {
			t.Errorf("%s: not idempotent: %v", p, again.Applied)
		}
		want, err := os.ReadFile(strings.TrimSuffix(p, ".domain") + ".expected")
		if err != nil || filepath.Base(filepath.Dir(p)) != "examples" {
			return nil
		}
		u, err := load(p, res.Src)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		var out bytes.Buffer
		if _, err := interp.RunScoped(u.pipe, &ir.Context{Stdout: &out, BaseDir: filepath.Dir(p)}); err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		if strings.TrimRight(out.String(), "\n") != strings.TrimRight(string(want), "\n") {
			t.Errorf("%s: tidied program printed %q, want %q", p, out.String(), want)
		}
		return nil
	})
	if seen < 50 {
		t.Fatalf("only %d programs found", seen)
	}
}

func TestNameFields(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Apply
    Using: (js) -> tuple(js, 0, 7)
Cursed Technique: Apply
    Using: (st) -> tuple(item(st, 0), item(st, 1) + 1, item(st, 2))
Cursed Technique: Apply
    Using: (st) -> item(st, 2) + length(item(st, 0))
Reveal: stdout
`
	asks := UnnamedFields("", src, 8)
	if len(asks) != 1 || asks[0].Type != "(List<Int>, Int, Int)" || len(asks[0].Fields) != 3 || asks[0].Line != 8 {
		t.Fatalf("asks = %+v", asks)
	}
	res, err := NameFields("", src, asks[0].Key, map[int]string{0: "js", 1: "i", 2: "limit"})
	if err != nil {
		t.Fatal(err)
	}
	wantContains(t, res.Src,
		"Using: ((js, i, limit)) -> tuple(js, i + 1, limit)",
		"Using: ((js, _, limit)) -> limit + length(js)")
	if run(t, res.Src, jumps) != run(t, src, jumps) {
		t.Error("output changed")
	}
	if _, err := NameFields("", src, asks[0].Key, map[int]string{0: "a", 1: "a"}); err == nil {
		t.Error("accepted a duplicate name")
	}
	if len(UnnamedFields("", res.Src, 1)) != 0 {
		t.Error("still asking after the fields were named")
	}
}

const twoParts = `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Apply
    Using: (js) -> tuple(js, 0, 0)

Part "1":
    Simple Domain: While
        Using: ((js, i, _)) -> i >= 0 and i < length(js)
        Cursed Technique: Apply
            Using: ((js, i, steps)) -> tuple(set(js, i, item(js, i) + 1), i + item(js, i), steps + 1)
    Cursed Technique: Apply
        Using: ((_, _, steps)) -> steps
    Reveal: stdout

Part "2":
    Simple Domain: While
        Using: ((js, i, _)) -> i >= 0 and i < length(js)
        Cursed Technique: Apply
            Using: ((js, i, steps)) -> tuple(set(js, i, item(js, i) + (if item(js, i) >= 3 then -1 else 1)), i + item(js, i), steps + 1)
    Cursed Technique: Apply
        Using: ((_, _, steps)) -> steps
    Reveal: stdout
`

func TestExtractShikigamiFromALambdaDifference(t *testing.T) {
	sp, ok := FindSharedParts("", twoParts)
	if !ok {
		t.Fatal("no shared computation found")
	}
	if sp.Statements != 2 || sp.ParamType != "((List<Int>, Int, Int)) -> (List<Int>, Int, Int)" {
		t.Errorf("plan = %+v", sp)
	}
	res, err := ExtractShikigami("", twoParts, "Jumps", "rule")
	if err != nil {
		t.Fatal(err)
	}
	wantContains(t, res.Src,
		`Shikigami "Jumps" (rule: ((List<Int>, Int, Int)) -> (List<Int>, Int, Int))`,
		"            Using: rule\n",
		"Part \"1\":\n    Shikigami: Jumps\n        rule: ((js, i, steps)) -> tuple(set(js, i, item(js, i) + 1)",
		"Part \"2\":\n    Shikigami: Jumps\n        rule: ((js, i, steps)) -> tuple(set(js, i, item(js, i) + (if")
	if got, want := run(t, res.Src, jumps), run(t, twoParts, jumps); got != want || want != "Part 1: 5\nPart 2: 10\n" {
		t.Errorf("got %q want %q", got, want)
	}
	if _, ok := FindSharedParts("", res.Src); ok {
		t.Error("still offering after the extraction")
	}
}

func TestExtractShikigamiFromALiteralDifference(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Ints
Part "1":
    Cursed Technique: Map Each
        Using: (x) -> x * 2
    Cursed Technique: Filter
        Using: (x) -> x > 1
    Maximum Technique: Sum
    Reveal: stdout
Part "2":
    Cursed Technique: Map Each
        Using: (x) -> x * 2
    Cursed Technique: Filter
        Using: (x) -> x > 4
    Maximum Technique: Sum
    Reveal: stdout
`
	// Tidying first is what a save would have done: the shared opening moves
	// above the Parts and what is left still differs by one number.
	src = Apply("", src, Options{}).Src
	sp, ok := FindSharedParts("", src)
	if !ok || sp.ParamType != "Int" {
		t.Fatalf("plan = %+v ok=%v\n%s", sp, ok, src)
	}
	res, err := ExtractShikigami("", src, "Total Over", "limit")
	if err != nil {
		t.Fatal(err)
	}
	wantContains(t, res.Src, `Shikigami "Total Over" (limit: Int)`, "Using: (x) -> x > limit", "limit: 4")
	if run(t, res.Src, jumps) != run(t, src, jumps) {
		t.Error("output changed")
	}
	if _, err := ExtractShikigami("", src, "Sum", "limit"); err == nil {
		t.Error("accepted a built-in's name")
	}
	if _, err := ExtractShikigami("", src, "Total Over", "x"); err == nil {
		t.Error("accepted a parameter name already in use")
	}
}

func TestNoExtractionForTwoDifferences(t *testing.T) {
	src := strings.Replace(twoParts, "Using: ((_, _, steps)) -> steps\n    Reveal: stdout\n\nPart", "Using: ((_, _, steps)) -> steps + 0\n    Reveal: stdout\n\nPart", 1)
	if _, ok := FindSharedParts("", src); ok {
		t.Error("offered an extraction over two differences")
	}
}

func TestCarryACounterInTheLoop(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Object: laps As 0
Simple Domain: While
    Using: (v) -> v > 1
    Cursed Technique: Apply
        Using: (n) -> consider h as n / 2 in h also laps := laps + 1
    Cursed Technique: Apply
        Using: (n) -> n + 0
Cursed Technique: Apply
    Using: (v) -> v * 100 + laps
Reveal: stdout
`
	cs := FindLoopCounters("", src)
	if len(cs) != 1 || cs[0].Name != "laps" || cs[0].Line != 5 {
		t.Fatalf("counters = %+v", cs)
	}
	res, err := CarryInLoop("", src, "laps")
	if err != nil {
		t.Fatal(err)
	}
	wantContains(t, res.Src,
		"Using: (v) -> tuple(v, 0)",
		"Using: ((v, laps)) -> v > 1",
		"Using: ((n, laps)) -> tuple(consider h as n / 2 in h, laps + 1)",
		"Using: ((n, laps)) -> tuple(n + 0, laps)",
		"Cursed Object: laps Of ((_, c)) -> c",
		"Using: ((v, _)) -> v\n")
	wantAbsent(t, res.Src, ":=", "laps As 0")
	if got, want := run(t, res.Src, "20"), run(t, src, "20"); got != want || want != "104\n" {
		t.Errorf("got %q want %q", got, want)
	}
	// Nothing writes it any more, so nothing is left to carry or to note.
	if len(FindLoopCounters("", res.Src)) != 0 {
		t.Error("still offered")
	}
	if notes := Apply("", res.Src, Options{}).Notes; len(notes) != 0 {
		t.Errorf("still noted as written: %v", notes)
	}
}

func TestCarryRefusesWhatItCannotThread(t *testing.T) {
	for name, src := range map[string]string{
		"a body stage that is not an Apply": `Cursed Energy: input.txt
Shikigami: Ints
Cursed Object: laps As 0
Simple Domain: Repeat 3
    Cursed Technique: Map Each
        Using: (n) -> n + 1 also laps := laps + 1
Reveal: stdout
`,
		"a read before the loop": `Cursed Energy: input.txt
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Object: laps As 0
Cursed Technique: Apply
    Using: (v) -> v + laps
Simple Domain: Repeat 3
    Cursed Technique: Apply
        Using: (n) -> n + 1 also laps := laps + 1
Reveal: stdout
`,
		"a write that is not the last step": `Cursed Energy: input.txt
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Object: laps As 0
Simple Domain: Repeat 3
    Cursed Technique: Apply
        Using: (n) -> if n > 0 then (n also laps := laps + 1) else n
Reveal: stdout
`,
	} {
		if cs := FindLoopCounters("", src); len(cs) != 0 {
			t.Errorf("%s: offered %+v", name, cs)
		}
		if _, err := CarryInLoop("", src, "laps"); err == nil {
			t.Errorf("%s: rewrote it", name)
		}
	}
}

const renames = `Shikigami "Over" (limit: Int) : List<Int> -> Int
    Cursed Technique: Filter
        Using: (x) -> x > limit
    Maximum Technique: Count

Cursed Energy: input.txt
Shikigami: Ints
Cursed Object: bump As 10
Cursed Technique: Map Each
    Consider twice As 2
    Using: (x) -> consider y as x * twice in y + bump
Cursed Technique: Map Each
    Using: (bump) -> bump - 1
Shikigami: Over
    limit: 11
Cursed Technique: Apply
    Using: (n) -> n + bump
Reveal: stdout
`

// at finds the position of the nth occurrence of word, for aiming a rename.
func at(t *testing.T, src, word string, nth int) (line, col int) {
	t.Helper()
	off := -1
	for i := 0; i < nth; i++ {
		j := strings.Index(src[off+1:], word)
		if j < 0 {
			t.Fatalf("no occurrence %d of %q", nth, word)
		}
		off += 1 + j
	}
	line = strings.Count(src[:off], "\n") + 1
	return line, off - (strings.LastIndex(src[:off], "\n") + 1)
}

func TestRename(t *testing.T) {
	want := run(t, renames, jumps)
	cases := []struct {
		word      string
		nth       int
		to, kind  string
		has, lack []string
	}{
		{"bump", 1, "extra", "global",
			[]string{"Cursed Object: extra As 10", "y + extra", "(n) -> n + extra", "(bump) -> bump - 1"}, nil},
		{"bump", 3, "v", "lambda parameter",
			[]string{"(v) -> v - 1", "Cursed Object: bump As 10", "y + bump"}, nil},
		{"y as", 1, "doubled", "local",
			[]string{"consider doubled as x * twice in doubled + bump"}, nil},
		{"twice", 2, "factor", "binding",
			[]string{"Consider factor As 2", "x * factor"}, []string{"twice"}},
		{"limit", 2, "floor", "Shikigami parameter",
			[]string{"(floor: Int)", "x > floor", "    floor: 11"}, []string{"limit"}},
		{"limit", 3, "floor", "Shikigami parameter",
			[]string{"(floor: Int)", "x > floor", "    floor: 11"}, nil},
		{"Over", 2, "Count Above", "Shikigami",
			[]string{`Shikigami "Count Above" (limit`, "Shikigami: Count Above\n"}, []string{"Over"}},
		{"Over", 1, "Count Above", "Shikigami",
			[]string{`Shikigami "Count Above" (limit`, "Shikigami: Count Above\n"}, nil},
	}
	for _, c := range cases {
		line, col := at(t, renames, c.word, c.nth)
		target, ok := FindRename("", renames, line, col)
		if !ok || target.Kind != c.kind {
			t.Errorf("%s #%d: target %+v ok=%v, want kind %q", c.word, c.nth, target, ok, c.kind)
			continue
		}
		res, err := Rename("", renames, line, col, c.to)
		if err != nil {
			t.Errorf("%s #%d: %v", c.word, c.nth, err)
			continue
		}
		wantContains(t, res.Src, c.has...)
		wantAbsent(t, res.Src, c.lack...)
		if got := run(t, res.Src, jumps); got != want {
			t.Errorf("%s #%d: output %q, want %q", c.word, c.nth, got, want)
		}
	}
}

func TestRenameRefusesCapture(t *testing.T) {
	// `x` is already a name in every scope `bump` reaches.
	line, col := at(t, renames, "bump", 1)
	if _, err := Rename("", renames, line, col, "x"); err == nil {
		t.Error("renamed a global onto a lambda parameter's name")
	}
	line, col = at(t, renames, "y as", 1)
	if _, err := Rename("", renames, line, col, "twice"); err == nil {
		t.Error("renamed a local onto a binding it reads")
	}
	if _, err := Rename("", renames, line, col, "not a name"); err == nil {
		t.Error("accepted a name with spaces")
	}
	// A prelude Shikigami is not this file's to rename.
	line, col = at(t, renames, "Ints", 1)
	if _, err := Rename("", renames, line, col, "Numbers"); err == nil {
		t.Error("renamed a prelude Shikigami")
	}
}

func TestVowsAreComparedAcrossInputs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "day.input"), []byte("3\n1\n2\n9\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "day.example"), []byte("5\n7\n"), 0o644)
	src := "Cursed Energy: day.input\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n"
	offer, err := SuggestVowsFor(filepath.Join(dir, "p.domain"), src, 2, dir, "day.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if offer.Other != "day.example" {
		t.Fatalf("not compared: %+v", offer)
	}
	// The count is a fact about one file; positivity is a fact about both.
	if !offer.Vows[0].Portable || strings.Contains(offer.Vows[0].Lines[0], "Count Equals") {
		t.Errorf("first vow is %+v, want one that holds on both", offer.Vows[0])
	}
	for _, v := range offer.Vows {
		isCount := strings.Contains(v.Lines[0], "Count Equals")
		if isCount == v.Portable {
			t.Errorf("%v: portable=%v", v.Lines, v.Portable)
		}
		if v.Elsewhere == "" {
			t.Errorf("%v: not marked", v.Lines)
		}
	}
}

func TestVowsBeforeAFailure(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("3\n\n2\n"), 0o644)
	src := "Cursed Energy: input.txt\nCursed Technique: Split Text by \"\\n\"\nChanneled Energy: Convert To Integers\nMaximum Technique: Sum\nReveal: stdout\n"
	offer, err := SuggestVowsBefore(filepath.Join(dir, "p.domain"), src, 3, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !offer.Broken || offer.AfterLine != 2 || len(offer.Vows) != 1 {
		t.Fatalf("offer = %+v", offer)
	}
	wantContains(t, strings.Join(offer.Vows[0].Lines, "\n"), `ikke contains(xs, "")`)

	// With the vow in place the run stops at the vow, on the line that says why.
	lines := strings.Split(src, "\n")
	with := strings.Join(append(append(append([]string{}, lines[:2]...), offer.Vows[0].Lines...), lines[2:]...), "\n")
	u, err := load(filepath.Join(dir, "p.domain"), with)
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := interp.RunScoped(u.pipe, &ir.Context{Stdout: &bytes.Buffer{}, BaseDir: dir})
	if runErr == nil || !strings.Contains(runErr.Error(), "vow") {
		t.Errorf("run error = %v, want the vow", runErr)
	}

	// A sound value has nothing to say.
	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("3\n2\n"), 0o644)
	if _, err := SuggestVowsBefore(filepath.Join(dir, "p.domain"), src, 3, dir, nil); err == nil {
		t.Error("offered vows against a value with nothing wrong with it")
	}
}

func TestGlobalWrites(t *testing.T) {
	src := "Cursed Object: n As 0\nCursed Object: m As 0\nApply\n    Using: (v) -> v also n := n + 1\nCursed Tool: n As 5\nCursed Tool:\n    m As 1\n    n As 2\n"
	if got := GlobalWrites(src, "n"); len(got) != 3 || got[0] != 4 || got[1] != 5 || got[2] != 8 {
		t.Errorf("writes of n = %v", got)
	}
}

// `consider` holds everything to its right together, so one that is an
// operand cannot be deleted without regrouping the arithmetic.
func TestDestructureLeavesAnOperandConsiderAlone(t *testing.T) {
	src := `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Apply
    Using: (js) -> tuple(10, 3)
Cursed Technique: Apply
    Using: (st) -> consider a as item(st, 0) in a - consider b as item(st, 1) in b + 1
Reveal: stdout
`
	res := tidied(t, src, jumps)
	if run(t, res.Src, jumps) != "6\n" {
		t.Fatalf("answer changed:\n%s", res.Src)
	}
	wantContains(t, res.Src, "consider b as")
}

func TestHoistKeepsCommentsItWouldLose(t *testing.T) {
	src := `Cursed Energy: input.txt
Part "1":
    Shikigami: Ints
    Maximum Technique: Sum
    Reveal: stdout
Part "2":
    # part two needs the numbers first
    Shikigami: Ints  # keep me
    Maximum Technique: Max
    Reveal: stdout
`
	res := tidied(t, src, jumps)
	wantContains(t, res.Src, "# part two needs the numbers first", "# keep me")
	if len(res.Notes) == 0 {
		t.Error("the refusal should be noted")
	}
}

// Drawing once above the Parts is not drawing once in each.
func TestHoistLeavesARandomShikigamiAlone(t *testing.T) {
	src := `Shikigami "Roll"
    Cursed Technique: Map Each
        Using: (x) -> x + random(1000000)

Cursed Energy: input.txt
Shikigami: Ints
Part "1":
    Shikigami: Roll
    Maximum Technique: Sum
    Reveal: stdout
Part "2":
    Shikigami: Roll
    Maximum Technique: Sum
    Reveal: stdout
`
	res := tidied(t, src, jumps)
	if strings.Count(res.Src, "Shikigami: Roll") != 2 {
		t.Errorf("hoisted a draw from the random stream:\n%s", res.Src)
	}
}

// Every vow offered against a failure must actually fail.
func TestBrokenVowsAreFalseOfTheValue(t *testing.T) {
	list := func(ss ...string) []ir.Value {
		out := make([]ir.Value, len(ss))
		for i, s := range ss {
			out[i] = s
		}
		return out
	}
	typ := ir.List(ir.Text())
	if vs := BrokenVows(list("a", "  ", "b"), typ); len(vs) != 0 {
		t.Errorf("offered %v for whitespace in the middle, which no expression here can test", vs[0].Lines)
	}
	vs := BrokenVows(list("a", "", "b"), typ)
	if len(vs) != 1 || !strings.Contains(vs[0].Why, "item 2 of 3") {
		t.Errorf("vows = %+v", vs)
	}
	vs = BrokenVows(list("a", "b", " "), typ)
	if len(vs) != 1 || !strings.Contains(vs[0].Lines[1], "trim(last(xs))") {
		t.Errorf("vows = %+v", vs)
	}
}
