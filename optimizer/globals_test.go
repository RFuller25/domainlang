package optimizer

import (
	"strings"
	"testing"
)

// A lambda that touches a global is left exactly as written (optimizer/
// globals.go). These pin the conservative rule the design asks to ship first,
// and the answers pin that standing down is not itself a behaviour change.

// The rule bites: a stage that would otherwise be rewritten is not, because
// its lambda reads a global something writes.
func TestGlobalReadStandsRewritesDown(t *testing.T) {
	const src = listHeader + `Cursed Object: g As 3
Cursed Tool: g As 4
Cursed Technique: Map Each
    Using: (x) -> x + g * 0
Maximum Technique: Sum
Reveal: stdout
`
	_, rewrites := resolveProgram(t, src, true)
	for _, r := range rewrites {
		if strings.Contains(r.Message, "Sum By") || strings.Contains(r.Message, "simplified") {
			t.Errorf("a stage reading a mutable global was rewritten: %q", r.Message)
		}
	}

	// The same program without the read is rewritten, which is what makes the
	// case above evidence of the rule rather than of the pass never firing.
	const pure = listHeader + `Cursed Technique: Map Each
    Using: (x) -> x + 3 * 0
Maximum Technique: Sum
Reveal: stdout
`
	_, pureRewrites := resolveProgram(t, pure, true)
	if len(pureRewrites) == 0 {
		t.Fatal("the control program was not rewritten; the test proves nothing")
	}
}

// The case the old reasoning missed. `effectful` asked ast.HasUpdate, which
// looks for a `:=` — and a `Cursed Tool` is a statement, so a lambda whose
// body is a sub-pipeline containing one carries a write that no expression
// walk would find.
func TestStatementLevelWriteInABlockBodyIsEffectful(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ","
Channeled Energy: Convert Each List to Integers
Cursed Object: g As 0
Cursed Technique: Map Each
    Cursed Tool: g As g + 1
    Maximum Technique: Sum
Maximum Technique: Sum
Reveal: stdout
`
	pipe, _ := resolveProgram(t, src, true)
	for _, n := range pipe.Nodes {
		lam := nodeLambda(n)
		if lam == nil {
			continue
		}
		if !effectful(lam) && lambdaImpure(lam) {
			t.Errorf("node %q touches a global but is not treated as effectful", n.Prim)
		}
	}
	// And the whole thing must still agree with the naive pipeline.
	naive, _ := resolveProgram(t, src, false)
	const input = "1,2\n3,4\n5,6"
	got, err := interpret(pipe, input)
	if err != nil {
		t.Fatalf("optimized: %v", err)
	}
	want, err := interpret(naive, input)
	if err != nil {
		t.Fatalf("naive: %v", err)
	}
	if got != want {
		t.Errorf("optimized %q, naive %q", got, want)
	}
}

// Standing rewrites down must not change any answer. Every shape here pairs a
// global with something the optimizer would otherwise reach for.
func TestGlobalProgramsAgreeWithTheNaivePipeline(t *testing.T) {
	cases := []struct{ name, src, input string }{
		{"read beside a fusable pair", listHeader + `Cursed Object: g As 3
Cursed Technique: Map Each
    Using: (x) -> x * g
Maximum Technique: Sum
Reveal: stdout
`, "1\n2\n3"},
		{"write then read in a later stage", listHeader + `Cursed Object: g As 0
Cursed Technique: Map Each
    Using: (x) -> x also g := g + 1
Cursed Technique: Map Each
    Using: (x) -> x * 100 + g
Reveal: stdout
`, "1\n2\n3"},
		{"a write between two readers", listHeader + `Cursed Object: g As 2
Cursed Technique: Map Each
    Using: (x) -> x * g
Cursed Tool: g As 100
Cursed Technique: Map Each
    Using: (x) -> x + g
Maximum Technique: Sum
Reveal: stdout
`, "1\n2\n3"},
		{"a global beside an algorithm substitution", listHeader + `Cursed Object: g As 1
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 2, Sum
Cursed Technique: Apply
    Using: (s) -> s + g
Reveal: stdout
`, "5\n1\n9\n3"},
		{"a statement write inside a body", `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ","
Channeled Energy: Convert Each List to Integers
Cursed Object: g As 0
Cursed Technique: Map Each
    Cursed Tool: g As g + 1
    Maximum Technique: Sum
Cursed Technique: Apply
    Using: (xs) -> sum(xs) * 1000 + g
Reveal: stdout
`, "1,2\n3,4\n5,6"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opt, _ := resolveProgram(t, c.src, true)
			naive, _ := resolveProgram(t, c.src, false)
			got, gerr := interpret(opt, c.input)
			want, werr := interpret(naive, c.input)
			if (gerr == nil) != (werr == nil) {
				t.Fatalf("optimized err=%v, naive err=%v", gerr, werr)
			}
			if got != want {
				t.Errorf("optimized %q, naive %q", got, want)
			}
		})
	}
}

// The stand-down has to be *sayable*: the failure mode of the blunt rule is a
// program that silently got slower, and a reader with no idea why their
// pipeline stopped fusing has nowhere to look.
func TestGlobalStandDownsAreReported(t *testing.T) {
	const src = listHeader + `Cursed Object: g As 3
Cursed Tool: g As 4
Cursed Technique: Map Each
    Using: (x) -> x + g
Maximum Technique: Sum
Reveal: stdout
`
	pipe, _ := resolveProgram(t, src, true)
	got := GlobalStandDowns(pipe)
	if len(got) != 1 {
		t.Fatalf("%d stand-downs, want 1: %+v", len(got), got)
	}
	if got[0].Prim != "Map Each" {
		t.Errorf("Prim = %q, want Map Each", got[0].Prim)
	}
	if len(got[0].Names) != 1 || got[0].Names[0] != "g" {
		t.Errorf("Names = %v, want [g]", got[0].Names)
	}
	if got[0].Pos.Line == 0 {
		t.Error("a stand-down with no position cannot be pointed at")
	}

	// A program with no globals reports nothing, so the hint never appears on
	// the programs that existed before this feature.
	clean, _ := resolveProgram(t, listHeader+"Maximum Technique: Sum\nReveal: stdout\n", true)
	if n := len(GlobalStandDowns(clean)); n != 0 {
		t.Errorf("%d stand-downs on a program with no globals, want 0", n)
	}

	// Nor on a stage reading a global nothing writes: that stage kept its
	// rewrites, so reporting it would send the reader after a cost that is not
	// there.
	immutable, _ := resolveProgram(t, listHeader+`Cursed Object: g As 3
Cursed Technique: Map Each
    Using: (x) -> x + g
Maximum Technique: Sum
Reveal: stdout
`, true)
	if n := len(GlobalStandDowns(immutable)); n != 0 {
		t.Errorf("%d stand-downs for a constant global, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// The precise rule: an immutable global is a constant of the run
// ---------------------------------------------------------------------------

// A global declared once at the top level and never written again costs its
// readers nothing. This is the half of the rule that makes the feature worth
// using, and the half that has to earn its way in — getting it wrong is a
// wrong answer, not a slow one.
func TestImmutableGlobalKeepsItsRewrites(t *testing.T) {
	const src = listHeader + `Cursed Object: g As 3
Cursed Technique: Map Each
    Using: (x) -> x * g
Maximum Technique: Sum
Reveal: stdout
`
	_, rewrites := resolveProgram(t, src, true)
	if len(rewrites) == 0 {
		t.Fatal("a stage reading an unwritten global was stood down; it reads a constant")
	}
}

// Every way a global can become mutable has to stop that. Each case reads the
// global in a stage the optimizer would otherwise rewrite, so a miss here
// shows up as a rewrite that should not have happened.
func TestMutableGlobalStandsRewritesDown(t *testing.T) {
	body := `Cursed Technique: Map Each
    Using: (x) -> x * g
Maximum Technique: Sum
Reveal: stdout
`
	cases := []struct{ name, src string }{
		{"Cursed Tool writes it", listHeader + "Cursed Object: g As 3\nCursed Tool: g As 4\n" + body},
		{"a walrus writes it", listHeader + `Cursed Object: g As 3
Cursed Technique: Map Each
    Using: (x) -> x also g := g + 1
` + body},
		{"declared inside a loop body", listHeader + `Cursed Object: seed As 1
Simple Domain: Repeat 2
    Cursed Object: g As seed
` + body},
		{"written inside a loop body", listHeader + `Cursed Object: g As 3
Simple Domain: Repeat 2
    Cursed Tool: g As g + 1
` + body},
		{"written inside a Part", listHeader + `Cursed Object: g As 3

Part "1":
    Cursed Tool: g As 9
    Reveal: stdout

` + body},
		{"written from a Shikigami body", `Shikigami "Bump"
    Cursed Tool: g As g + 1

` + listHeader + "Cursed Object: g As 3\nShikigami: Bump\n" + body},
		{"written inside a pipeline body", `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ","
Channeled Energy: Convert Each List to Integers
Cursed Object: g As 3
Cursed Technique: Map Each
    Cursed Tool: g As g + 1
    Maximum Technique: Sum
Cursed Technique: Map Each
    Using: (x) -> x * g
Maximum Technique: Sum
Reveal: stdout
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pipe, rewrites := resolveProgram(t, c.src, true)
			for _, r := range rewrites {
				if strings.Contains(r.Message, "Sum By") {
					t.Errorf("a stage reading a mutable global was fused: %q", r.Message)
				}
			}
			// Whatever the passes did, the answer must not move.
			naive, _ := resolveProgram(t, c.src, false)
			input := "1\n2\n3"
			if strings.Contains(c.src, "Split Each") {
				input = "1,2\n3,4"
			}
			got, gerr := interpret(pipe, input)
			want, werr := interpret(naive, input)
			if (gerr == nil) != (werr == nil) {
				t.Fatalf("optimized err=%v, naive err=%v", gerr, werr)
			}
			if got != want {
				t.Errorf("optimized %q, naive %q", got, want)
			}
		})
	}
}

// The mutability flag is over-approximated on purpose, and the direction
// matters: a `:=` is counted by spelling even when something nearer actually
// captured it. Being wrong that way costs a rewrite; being wrong the other way
// is a wrong answer.
func TestMutabilityOverApproximatesSafely(t *testing.T) {
	// `g` here is a stage binding that shadows the global, so the write never
	// reaches the global at all — and the global is still treated as mutable.
	const src = listHeader + `Cursed Object: g As 3
Cursed Technique: Map Each
    Consider g As 0
    Using: (x) -> x also g := g + 1
Cursed Technique: Map Each
    Using: (x) -> x * g
Maximum Technique: Sum
Reveal: stdout
`
	pipe, _ := resolveProgram(t, src, true)
	naive, _ := resolveProgram(t, src, false)
	got, err := interpret(pipe, "1\n2\n3")
	if err != nil {
		t.Fatalf("optimized: %v", err)
	}
	want, err := interpret(naive, "1\n2\n3")
	if err != nil {
		t.Fatalf("naive: %v", err)
	}
	if got != want {
		t.Errorf("optimized %q, naive %q", got, want)
	}
}
