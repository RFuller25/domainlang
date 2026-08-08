package pattern

import (
	"strings"
	"testing"

	"domain/ir"
)

// Groups: `[? … ]` makes a run of the template optional, and `( … )+` repeats
// one. Phase 5 gave holes both of those; this is the same two features one
// level up, where the unit is a run of structure rather than a single value.

// AoC 2017 D7, the shape this exists for: two line kinds differing by a
// trailing clause. Before optional groups the only single-pass spelling was a
// `{text}` sponge plus a hand-written re-parse in the expression layer.
func TestOptionalGroupTypesAndMatches(t *testing.T) {
	tmpl, err := ParseTemplate(`{name:word} ({w:int})[? -> {kids:word+ sep=", "}]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := ir.Record(
		ir.Field{Name: "name", Type: ir.Text()},
		ir.Field{Name: "w", Type: ir.Int()},
		ir.Field{Name: "kids", Type: ir.List(ir.Text())},
	)
	if got := tmpl.OutputType(); !got.Equal(want) {
		t.Errorf("output type: got %s, want %s", got, want)
	}
	re, err := tmpl.CompileRegex()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, tc := range []struct {
		line string
		kids string // "" means the group must be absent
	}{
		{"pbga (66)", ""},
		{"fwft (72) -> ktlj, cntj, xhth", "ktlj, cntj, xhth"},
		{"ugml (68) -> gyxo", "gyxo"},
	} {
		m := re.FindStringSubmatchIndex(tc.line)
		if m == nil {
			t.Errorf("%q did not match", tc.line)
			continue
		}
		got, present := capture(tmpl, m, tc.line, "kids")
		if (tc.kids != "") != present {
			t.Errorf("%q: group present = %v, want %v", tc.line, present, tc.kids != "")
			continue
		}
		if present && got != tc.kids {
			t.Errorf("%q: kids = %q, want %q", tc.line, got, tc.kids)
		}
	}
}

// Absence has to be *exact*. FindStringSubmatch reports a non-participating
// group as "", which is also what a group that legitimately matched empty
// reports — so the whole feature rests on reading indices instead, where a
// group that did not participate is -1.
func TestAbsenceIsDistinctFromAnEmptyMatch(t *testing.T) {
	// The optional group's hole is `{rest:text}`, which matches the empty
	// string. Present-but-empty and absent are then indistinguishable by the
	// captured text alone, and only the index form tells them apart.
	tmpl, err := ParseTemplate(`{n:int}[?:{rest:text}]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	re, _ := tmpl.CompileRegex()

	absent := re.FindStringSubmatchIndex("5")
	empty := re.FindStringSubmatchIndex("5:")
	if absent == nil || empty == nil {
		t.Fatal("both lines should match")
	}
	if _, present := capture(tmpl, absent, "5", "rest"); present {
		t.Error(`"5" has no ":" so the group is absent, but it read as present`)
	}
	got, present := capture(tmpl, empty, "5:", "rest")
	if !present || got != "" {
		t.Errorf(`"5:" matched the group with an empty rest; got %q present=%v`, got, present)
	}
	// The distinction is only visible through indices: both captures are "".
	if m := re.FindStringSubmatch("5"); m[2] != "" {
		t.Errorf("precondition: the absent capture should read as empty text, got %q", m[2])
	}
}

// A presence flag is the answer to "the zero value and a real zero look the
// same" — an absent {n:int} reads as 0, which a matched 0 also does.
func TestPresenceFlag(t *testing.T) {
	tmpl, err := ParseTemplate(`{v:int}[? (was {n:int}){?changed}]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := ir.Record(
		ir.Field{Name: "v", Type: ir.Int()},
		ir.Field{Name: "n", Type: ir.Int()},
		ir.Field{Name: "changed", Type: ir.Bool()},
	)
	if got := tmpl.OutputType(); !got.Equal(want) {
		t.Errorf("output type: got %s, want %s", got, want)
	}
	re, _ := tmpl.CompileRegex()
	if re.FindStringSubmatchIndex("7 (was 0)") == nil {
		t.Error("the present form should match")
	}
	if re.FindStringSubmatchIndex("7") == nil {
		t.Error("the absent form should match")
	}
}

// AoC 2023 D2: a repeating {int} {word} *pair*, which no spelling of a
// repeated hole captures. This is the case phase 5 wrote down as still open.
func TestRepeatedGroupTypesAndMatches(t *testing.T) {
	tmpl, err := ParseTemplate(`Game {id:int}: {draws:( {n:int} {color:word} )+ sep=", "}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := ir.Record(
		ir.Field{Name: "id", Type: ir.Int()},
		ir.Field{Name: "draws", Type: ir.List(ir.Record(
			ir.Field{Name: "n", Type: ir.Int()},
			ir.Field{Name: "color", Type: ir.Text()},
		))},
	)
	if got := tmpl.OutputType(); !got.Equal(want) {
		t.Errorf("output type: got %s, want %s", got, want)
	}
	re, _ := tmpl.CompileRegex()
	m := re.FindStringSubmatchIndex("Game 1: 3 blue, 4 red, 2 green")
	if m == nil {
		t.Fatal("no match")
	}
	run, present := capture(tmpl, m, "Game 1: 3 blue, 4 red, 2 green", "draws")
	if !present || run != "3 blue, 4 red, 2 green" {
		t.Fatalf("the run capture is %q (present=%v), want the whole run", run, present)
	}
	// One element still matches: `+` is one-or-more here too.
	if re.FindStringSubmatchIndex("Game 2: 1 red") == nil {
		t.Error("a single element should still match a repeated group")
	}
}

// Padding inside the parentheses is formatting: `( {n:int} )` and `({n:int})`
// have to mean the same thing, or the prettier spelling silently matches
// something else.
func TestGroupPaddingIsNotPartOfTheElement(t *testing.T) {
	padded, err := ParseTemplate(`{ds:( {n:int} {c:word} )+ sep=", "}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tight, err := ParseTemplate(`{ds:({n:int} {c:word})+ sep=", "}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a, b := padded.RegexSource(true), tight.RegexSource(true); a != b {
		t.Errorf("padding changed the lowering\npadded: %s\ntight:  %s", a, b)
	}
}

// A bare `[` is an ordinary character — AoC input is full of them, and a
// template that matched "[1,2]" before groups existed has to keep doing so.
// Only `[?` opens a group.
func TestABareBracketStaysALiteral(t *testing.T) {
	tmpl, err := ParseTemplate(`[{a:int},{b:int}]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tmpl.Opts) != 0 {
		t.Fatalf("a bare bracket opened %d optional group(s)", len(tmpl.Opts))
	}
	re, _ := tmpl.CompileRegex()
	if re.FindStringSubmatch("[1,2]") == nil {
		t.Error(`"[1,2]" should still match`)
	}
	if re.FindStringSubmatch("1,2") != nil {
		t.Error("the brackets are literal, so a line without them must not match")
	}
}

// The capture plan and the regex come out of one walk, so a group cannot
// renumber one without the other. This checks the plan against the regex the
// walk actually produced.
func TestCapturePlanMatchesTheRegexGroupCount(t *testing.T) {
	for _, src := range []string{
		`{a:int}-{b:int}`,
		`{name:word} ({w:int})[? -> {kids:word+ sep=", "}]`,
		`Game {id:int}: {draws:( {n:int} {c:word} )+ sep=", "}`,
		`{v:int}[? (was {n:int}){?changed}]`,
		`{a:int}[?/{b:int}][?:{c:int}]`,
	} {
		tmpl, err := ParseTemplate(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		re, err := tmpl.CompileRegex()
		if err != nil {
			t.Fatalf("%s: compile: %v", src, err)
		}
		plan := tmpl.Captures()
		if got, want := re.NumSubexp(), len(plan); got != want {
			t.Errorf("%s: the regex has %d capture groups, the plan describes %d\n%s",
				src, got, want, tmpl.RegexSource(true))
			continue
		}
		for i, c := range plan {
			if c.Group != i+1 {
				t.Errorf("%s: plan entry %d claims group %d", src, i, c.Group)
			}
		}
	}
}

// Both spellings of the lowering stay identical but for the group names — the
// property phase 5 added when it collapsed the compiler's second regex into
// this one, now carrying groups too.
func TestGroupLoweringsAgree(t *testing.T) {
	for _, src := range []string{
		`{name:word} ({w:int})[? -> {kids:word+ sep=", "}]`,
		`Game {id:int}: {draws:( {n:int} {c:word} )+ sep=", "}`,
		`{v:int}[? (was {n:int}){?changed}]`,
	} {
		tmpl, err := ParseTemplate(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		named, plain := tmpl.RegexSource(true), tmpl.RegexSource(false)
		stripped := named
		for _, h := range tmpl.Holes {
			if h.Flag {
				continue // a flag owns no capture group
			}
			stripped = strings.Replace(stripped, "(?P<"+h.Name+">", "(", 1)
		}
		if stripped != plain {
			t.Errorf("%s: the two lowerings differ\nnamed→plain: %s\nplain:       %s",
				src, stripped, plain)
		}
	}
}

func TestGroupRefusals(t *testing.T) {
	for _, tc := range []struct{ tmpl, want string }{
		// One level only: the inner template is a plain Template, and keeping
		// it that way is what lets groups reuse every existing rule.
		{`{a:( {b:( {c:int} )+ sep="," } )+ sep=";"}`, "do not nest"},
		{`{a:( x[?y{b:int}] )+ sep=","}`, "do not nest"},
		// A group is only useful repeated; a bare one would just be literals.
		{`{a:( {b:int} )}`, "groups without repeating it"},
		{`{a:( {b:int} )+}`, "names no separator"},
		// A flag outside an optional group has nothing to report on.
		{`{?loose} {a:int}`, "only means something inside an optional group"},
		// An optional group that records nothing cannot be told from a literal.
		{`{a:int}[? just literal text]`, "records whether it matched"},
		{`{a:int}[?]`, "nothing for it to make optional"},
		{`{a:int}[?{?one}{?two} x]`, "two presence flags"},
		// Unterminated brackets, which the depth-aware scanners have to notice
		// rather than run off the end of.
		{`{a:int}[? -> {b:int}`, "unterminated optional group"},
		{`{a:( {b:int} + sep=","}`, "unterminated"},
	} {
		if _, err := ParseTemplate(tc.tmpl); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected an error containing %q, got %v", tc.tmpl, tc.want, err)
		}
	}
}

// capture reads a named hole out of a FindStringSubmatchIndex result, reporting
// whether its group participated at all.
func capture(t *Template, m []int, s, name string) (string, bool) {
	for _, c := range t.Captures() {
		if c.Kind != CapHole || c.Hole.Name != name {
			continue
		}
		lo, hi := m[2*c.Group], m[2*c.Group+1]
		if lo < 0 {
			return "", false
		}
		return s[lo:hi], true
	}
	return "", false
}
