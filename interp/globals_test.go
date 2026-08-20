package interp

import (
	"strings"
	"testing"

	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// End-to-end behaviour of `Cursed Object` / `Cursed Tool`, run through Run
// rather than a hand-rolled node loop — the slot array is sized and cleared
// there, so a test that drove the nodes itself would pass with that call
// deleted.
func runGlobals(t *testing.T, src, stdin string) string {
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
		t.Fatalf("resolve: %v", err)
	}
	var out strings.Builder
	ctx := &ir.Context{Stdin: strings.NewReader(stdin), Stdout: &out}
	if _, err := Run(pipe, ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(out.String(), "\n")
}

func TestGlobalOutlivesTheLoopThatWroteIt(t *testing.T) {
	// The whole point of the feature: `Consider` at a loop head accumulates
	// across laps too, but its name dies with the loop, so the count has to
	// ride out in the loop's own value. A global does not.
	src := `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Object: laps As 0
Simple Domain: While
    Using: (v) -> v > 1
    Cursed Technique: Apply
        Using: (n) -> (n / 2) also (laps := laps + 1)
Cursed Technique: Apply
    Using: (v) -> laps
Reveal: stdout
`
	if got := runGlobals(t, src, "20"); got != "4" {
		t.Errorf("laps = %s, want 4 (20 -> 10 -> 5 -> 2 -> 1)", got)
	}
}

func TestGlobalWriteIsVisibleToTheNextElement(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Object: running As 0
Cursed Technique: Map Each
    Using: (x) -> running := running + x
Reveal: stdout
`
	if got := runGlobals(t, src, "1,2,3"); got != "[1, 3, 6]" {
		t.Errorf("got %s, want [1, 3, 6]", got)
	}
}

// Declarations in one block are written in order and each sees the ones above
// it, so a later line reads the value the earlier one just wrote.
func TestGlobalDeclarationsSeeEarlierOnes(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Object:
    a As 2
    b As a * 5
Cursed Technique: Apply
    Using: (t) -> b
Reveal: stdout
`
	if got := runGlobals(t, src, "x"); got != "10" {
		t.Errorf("b = %s, want 10", got)
	}
}

// `Cursed Tool` writes in order too, which is what the generator idiom needs:
// the match test must see the values the two lines above it just produced.
func TestCursedToolWritesInOrder(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Object:
    a As 1
    seen As 0
Simple Domain: Repeat 3
    Cursed Tool:
        a As a * 10
        seen As seen + a
Cursed Technique: Apply
    Using: (t) -> seen
Reveal: stdout
`
	// a: 10, 100, 1000 — each read after its own write, so seen is 1110.
	if got := runGlobals(t, src, "x"); got != "1110" {
		t.Errorf("seen = %s, want 1110", got)
	}
}

// `Of` sees the value arriving at the declaration, exactly as it does on a
// Consider; `As` never does.
func TestGlobalOfReadsTheCurrentValue(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Object: n Of (xs) -> length(xs)
Cursed Technique: Apply
    Using: (xs) -> n
Reveal: stdout
`
	if got := runGlobals(t, src, "a,b,c,d"); got != "4" {
		t.Errorf("n = %s, want 4", got)
	}
}

// Inner wins, as it does for every other name in the language.
func TestGlobalsAreShadowedByNearerNames(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "lambda parameter",
			src: `Cursed Energy: stdin
Cursed Object: n As 99
Cursed Technique: Apply
    Using: (n) -> n
Reveal: stdout
`,
			want: "5",
		},
		{
			name: "stage binding",
			src: `Cursed Energy: stdin
Cursed Object: n As 99
Cursed Technique: Apply
    Consider n As 7
    Using: (x) -> n
Reveal: stdout
`,
			want: "7",
		},
		{
			name: "consider local",
			src: `Cursed Energy: stdin
Cursed Object: n As 99
Cursed Technique: Apply
    Using: (x) -> consider n as 3 in n
Reveal: stdout
`,
			want: "3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGlobals(t, tc.src, "5"); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// A declaration written inside a loop body re-runs every lap. That is the
// honest reading of a statement that is a statement, and `Cursed Tool` is what
// the accumulating case is written with instead.
func TestGlobalDeclaredInALoopBodyReinitialises(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Object: total As 0
Simple Domain: Repeat 3
    Cursed Object: perLap As 1
    Cursed Tool: total As total + perLap
Cursed Technique: Apply
    Using: (v) -> total
Reveal: stdout
`
	if got := runGlobals(t, src, "0"); got != "3" {
		t.Errorf("total = %s, want 3", got)
	}
}

// Two runs of the same pipeline must not see each other's writes. Run clears
// the slot array; without that, a global would carry over.
func TestGlobalsAreClearedBetweenRuns(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Object: total As 0
Cursed Technique: Map Each
    Using: (x) -> total := total + x
Cursed Technique: Apply
    Using: (xs) -> total
Reveal: stdout
`
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for i := range 3 {
		var out strings.Builder
		ctx := &ir.Context{Stdin: strings.NewReader("1,2,3"), Stdout: &out}
		if _, err := Run(pipe, ctx); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if got := strings.TrimRight(out.String(), "\n"); got != "6" {
			t.Fatalf("run %d gave %s, want 6 — a global leaked between runs", i, got)
		}
	}
}

// Sibling Parts branch from one state, and docs/language.md states the
// isolation as a guarantee — "Part 1 sorting cannot disturb what Part 2 sees".
// A mutable global would punch straight through it, so a Part's writes are
// discarded on the way out.
func TestPartsDoNotSeeEachOthersGlobalWrites(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Object: n As 0

Part "1":
    Cursed Tool: n As 100
    Cursed Technique: Apply
        Using: (xs) -> n
    Reveal: stdout

Part "2":
    Cursed Technique: Apply
        Using: (xs) -> n
    Reveal: stdout
`
	got := runGlobals(t, src, "1,2,3")
	want := "Part 1: 100\nPart 2: 0"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A Shikigami defined in the program's own file is inlined at its call site
// and reads globals like any other stage. (The imported case is refused; see
// prims' TestGlobalsAreSealedFrom.)
func TestLocalShikigamiReadsAGlobal(t *testing.T) {
	src := `Shikigami "Show N"
    Cursed Technique: Apply
        Using: (xs) -> n

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Object: n As 5
Shikigami: Show N
Reveal: stdout
`
	if got := runGlobals(t, src, "1,2,3"); got != "5" {
		t.Errorf("got %s, want 5", got)
	}
}

// A declaration's right-hand side is the same three forms a `Consider` takes,
// so everything that walks a statement's Binds has to walk its Decls too.
// Every case below is a path that walked one and not the other.
func TestDeclarationsShareTheBindingMachinery(t *testing.T) {
	for _, tc := range []struct{ name, src, stdin, want string }{
		{
			// prims.Infer fills in the keyword of a statement written as a
			// bare phrase. Without the Decls walk, `Of Convert To Set` under a
			// declaration failed with `unknown keyword ""`.
			name: "keyword inference inside an Of body",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Object: s Of Convert To Set
Cursed Technique: Apply
    Using: (xs) -> size(s)
Reveal: stdout
`,
			stdin: "1,1,2", want: "2",
		},
		{
			// A Shikigami parameter is substituted into the body at each call
			// site. Without the Decls walk, a global declared in a body could
			// not use the definition's own parameters.
			name: "Shikigami parameter in a declaration",
			src: `Shikigami "Setup" (k: Int)
    Cursed Object: base As k
    Cursed Technique: Map Each
        Using: (n) -> n + base

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Shikigami: Setup
    k: 100
Reveal: stdout
`,
			stdin: "1,2,3", want: "[101, 102, 103]",
		},
		{
			// The `:=` scan decides whether a binding keeps a cell or is
			// folded to a literal. Without the Decls walk this program was
			// refused with "was folded to a constant and cannot be updated".
			name: "a walrus written in a declaration's value",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Simple Domain: Repeat 3
    Consider acc As 0
    Cursed Object: g As (acc := acc + 1)
    Cursed Technique: Apply
        Using: (n) -> n + acc
Reveal: stdout
`,
			stdin: "0", want: "6",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGlobals(t, tc.src, tc.stdin); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
