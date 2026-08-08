package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
)

// Template groups, through both backends. A group changes what the capture
// indices mean — an optional group owns a wrapper capture that the holes it
// guards sit inside, and a repeated group's inner holes own no outer capture at
// all — so the plan the interpreter reads and the plan the compiler emits have
// to be the same plan. They are: pattern.Template.lower walks once and returns
// both the regex and the capture layout.
func TestMatchGroupsOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const lines = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
`
	cases := []struct {
		name, src, input string
	}{
		// AoC 2017 D7, the shape the feature exists for: two line kinds
		// differing by a trailing clause, in one pass and in input order.
		{"optional group, absent and present", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{name:word} ({w:int})[? -> {kids:word+ sep=\", \"}]"
Reveal: stdout
`, "pbga (66)\nfwft (72) -> ktlj, cntj, xhth\nugml (68) -> gyxo\nqoyq (66)\n"},

		// An absent group leaves its holes at their zero, and the zero of a
		// List is the empty one — which the compiled rendering has to agree
		// about, a nil slice being the likeliest place to differ.
		{"an absent list hole renders as empty", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{name:word}[?: {ns:int+ sep=\",\"}]"
Cursed Technique: Map Each
    Using: (r) -> length(r.ns)
Reveal: stdout
`, "a\nb: 1,2,3\nc\nd: 9\n"},

		{"an absent int hole is zero", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{v:int}[?/{d:int}]"
Cursed Technique: Map Each
    Using: (r) -> r.v + r.d
Maximum Technique: Sum
Reveal: stdout
`, "1/2\n3\n4/5\n"},

		// The flag is what tells an absent zero from a captured one.
		{"a presence flag", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{v:int}[? (was {n:int}){?changed}]"
Reveal: stdout
`, "7 (was 0)\n7\n0 (was 3)\n"},

		{"two optional groups on one line", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{a:int}[?/{b:int}][?:{c:int}]"
Reveal: stdout
`, "1\n1/2\n1:3\n1/2:3\n"},

		// AoC 2023 D2: a repeating {int} {word} pair, which no spelling of a
		// repeated *hole* captures.
		{"repeated group", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "Game {id:int}: {draws:( {n:int} {color:word} )+ sep=\", \"}"
Reveal: stdout
`, "Game 1: 3 blue, 4 red, 2 green\nGame 2: 1 red\n"},

		{"a repeated group summed through its records", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "Game {id:int}: {draws:( {n:int} {color:word} )+ sep=\", \"}"
Cursed Technique: Map Each
    Using: (g) -> g.id * length(g.draws)
Maximum Technique: Sum
Reveal: stdout
`, "Game 1: 3 blue, 4 red, 2 green\nGame 2: 1 red\nGame 3: 2 a, 2 b\n"},

		// A positional group is a Tuple per element rather than a Record.
		{"a positional repeated group", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{ps:( {int},{int} )+ sep=\" \"}"
Reveal: stdout
`, "1,2 3,4\n5,6\n"},

		// Groups and Try together: the shape mismatch is still the only thing
		// Try drops, and an optional group is not a mismatch.
		{"an optional group under Try", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "{name:word} ({w:int})[? -> {kids:word+ sep=\", \"}]"
Maximum Technique: Count
Reveal: stdout
`, "pbga (66)\nnot this line at all\nfwft (72) -> ktlj, cntj\n"},

		// Mode: One, the Text branch, so the group work is not tied to Each.
		{"a group under Mode: One", `Cursed Energy: stdin
Cursed Technique: Match Pattern
    Mode: One
    Using: "seeds: {vals:( {int} )+ sep=\" \"}"
Reveal: stdout
`, "seeds: 79 14 55 13\n"},

		// A bare bracket is still a literal: only `[?` opens a group, and a
		// template that matched this before groups existed still has to.
		{"a bare bracket stays literal", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "[{a:int},{b:int}]"
Reveal: stdout
`, "[1,2]\n[3,4]\n"},
	}

	for _, c := range cases {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				input := []byte(c.input)
				pipe, want := oracleFront(t, c.src, optimize, input)
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q\n\n%s", want, got, c.src)
				}
			})
		}
	}
}

// A group is a run whose length the scan cannot bound, and an optional group is
// a run the scan cannot know whether to consume — so both take the regex path.
// The optional case is the one worth pinning: its segment carries no hole, so
// the eligibility walk used to step straight over it and call the template
// eligible, which would have compiled a scanner that ignored the group.
func TestGroupsTakeTheRegexPath(t *testing.T) {
	for _, c := range []struct{ name, tmpl string }{
		{"optional group", `"{a:int}[?/{b:int}]"`},
		{"repeated group", `"{ps:( {n:int} )+ sep=\" \"}"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: ` + c.tmpl + `
Reveal: stdout
`
			pipe := compilePipeline(t, src, true)
			got, err := codegen.EmitProgram(pipe, codegen.Options{})
			if err != nil {
				t.Fatalf("EmitProgram: %v", err)
			}
			if !strings.Contains(got, "regexp.MustCompile") {
				t.Errorf("a %s should fall back to regexp, generated source:\n%s", c.name, got)
			}
		})
	}
}

// A record whose field is a record — which a repeated group produces, and which
// record("a", record(...)) could always write — used to emit two `type R1
// struct` declarations and fail to build: the struct name was numbered from the
// intern table's length, and the inner type was interned while the outer one's
// declaration was still being generated, before it had been inserted.
func TestNestedRecordTypesGetDistinctNames(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (l) -> record("outer", length(l), "inner", record("a", 1, "b", 2))
Reveal: stdout
`
	got, err := codegen.EmitProgram(compilePipeline(t, src, true), codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "type ") || !strings.HasSuffix(line, " struct {") {
			continue
		}
		name := strings.Fields(line)[1]
		if seen[name] {
			t.Fatalf("%s is declared twice:\n%s", name, got)
		}
		seen[name] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected an outer and an inner struct, found %d: %v", len(seen), seen)
	}
}
