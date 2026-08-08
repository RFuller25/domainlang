package pattern

import (
	"strings"
	"testing"

	"domain/ir"
)

// The scalar hole types past int/word/text, and the `{~}` gap.

func TestHoleTypesAndTheirTypes(t *testing.T) {
	for _, tc := range []struct {
		tmpl, line, re string
		want           *ir.Type
	}{
		{`#{c:hex}`, "#70c710", `^#(?P<c>[0-9a-fA-F]+)$`,
			ir.Record(ir.Field{Name: "c", Type: ir.Int()})},
		// digits is Text, not Int: leading zeros are the only reason to prefer
		// it over {int}, and Int cannot hold them.
		{`{d:digits}`, "007", `^(?P<d>[0-9]+)$`,
			ir.Record(ir.Field{Name: "d", Type: ir.Text()})},
		{`{c:char}{n:int}`, "R5", `^(?P<c>.)(?P<n>-?\d+)$`,
			ir.Record(ir.Field{Name: "c", Type: ir.Text()}, ir.Field{Name: "n", Type: ir.Int()})},
	} {
		tmpl, err := ParseTemplate(tc.tmpl)
		if err != nil {
			t.Errorf("%s: %v", tc.tmpl, err)
			continue
		}
		if got := tmpl.OutputType(); !got.Equal(tc.want) {
			t.Errorf("%s: type %s, want %s", tc.tmpl, got, tc.want)
		}
		if got := tmpl.RegexSource(true); got != tc.re {
			t.Errorf("%s: regex %s, want %s", tc.tmpl, got, tc.re)
		}
		re, _ := tmpl.CompileRegex()
		if re.FindStringSubmatch(tc.line) == nil {
			t.Errorf("%s did not match %q", tc.tmpl, tc.line)
		}
	}
}

// A `{~}` is structure, not data: it matches a run of whitespace and owns
// neither a capture nor an output field, so a template's fields do not change
// when a gap is added to it.
func TestFlexibleGapOwnsNoField(t *testing.T) {
	with, err := ParseTemplate(`{a:word}:{~}{b:int}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	without, err := ParseTemplate(`{a:word}: {b:int}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !with.OutputType().Equal(without.OutputType()) {
		t.Errorf("the gap changed the output type: %s vs %s",
			with.OutputType(), without.OutputType())
	}
	if n := len(with.Captures()); n != 2 {
		t.Errorf("the gap owns a capture: %d groups for 2 holes", n)
	}
	re, _ := with.CompileRegex()
	for _, line := range []string{"x: 1", "x:    1", "x:\t1"} {
		if re.FindStringSubmatch(line) == nil {
			t.Errorf("%q should match a flexible gap", line)
		}
	}
	// One-or-more, not zero-or-more: the template author wrote a gap, so
	// there is a gap.
	if re.FindStringSubmatch("x:1") != nil {
		t.Error(`"x:1" has no gap and should not match`)
	}
}

// The unanchored lowering Mode: Scan uses comes from the same walk as the
// anchored one, so the two cannot describe different patterns.
func TestScanSourceIsTheAnchoredOneWithoutAnchors(t *testing.T) {
	for _, src := range []string{
		`mul({a:int},{b:int})`,
		`{name:word} ({w:int})[? -> {kids:word+ sep=", "}]`,
		`{k:char} #{c:hex}`,
	} {
		tmpl, err := ParseTemplate(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		anchored, scan := tmpl.RegexSource(true), tmpl.ScanSource(true)
		if want := "^" + scan + "$"; anchored != want {
			t.Errorf("%s: the two lowerings differ by more than the anchors\nanchored: %s\nscan:     %s",
				src, anchored, scan)
		}
		if _, err := tmpl.CompileScan(); err != nil {
			t.Errorf("%s: the unanchored form does not compile: %v", src, err)
		}
	}
}

func TestHoleTypeRefusals(t *testing.T) {
	for _, tc := range []struct{ tmpl, want string }{
		{`{x:blah}`, "want int, hex, digits, word, char, or text"},
		// `word` and `char` are narrowed to exclude the separator; a fixed
		// digit class cannot be, since excluding `a` from hex leaves something
		// that is no longer hex.
		{`{hs:hex+ sep="a"}`, "which is itself one"},
		{`{hs:hex+ sep="F"}`, "which is itself one"},
		{`{ds:digits+ sep="1"}`, "which is itself one"},
	} {
		if _, err := ParseTemplate(tc.tmpl); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected an error containing %q, got %v", tc.tmpl, tc.want, err)
		}
	}
	// A separator outside the class is fine.
	for _, ok := range []string{`{hs:hex+ sep=","}`, `{ds:digits+ sep=" "}`, `{cs:char+ sep=","}`} {
		if _, err := ParseTemplate(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}
