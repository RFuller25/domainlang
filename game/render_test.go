package game

import (
	"strings"
	"testing"

	"domain/ir"
)

// Styling decides how a run of text is drawn and nothing else. If it could
// change where a column lands, a golden frame would be evidence about the
// renderer rather than about the program — so the styled painting and the
// plain one must be the same picture once the escapes are taken off.
func TestStyledRenderKeepsPlainGeometry(t *testing.T) {
	cases := []*ir.ViewValue{
		ir.TextView("plain"),
		ir.BoxView(ir.StyledView(ir.TextView("hi"), ir.ViewStyle{Bold: true, FG: "red"})),
		ir.BesideView(
			ir.StyledView(ir.TextView("AB"), ir.ViewStyle{FG: "green", BG: "blue"}),
			ir.TextView("|"),
			ir.StackView(ir.TextView("x"), ir.TextView("yy")),
		),
		ir.StackView(
			ir.StyledView(ir.TextView("title"), ir.ViewStyle{Underline: true}),
			ir.MarginView(ir.TextView("body"), 2, 1),
		),
	}
	for i, v := range cases {
		plain := ir.RenderViewPlain(v)
		styled := stripANSI(renderViewStyled(v))
		if styled != plain {
			t.Errorf("case %d: styled render differs from plain\n--- styled ---\n%s\n--- plain ---\n%s",
				i, styled, plain)
		}
	}
}

// An unstyled picture costs no escape codes at all: most of a frame is plain,
// and paying for it everywhere would make every frame larger and slower for
// nothing.
func TestUnstyledRenderEmitsNoEscapes(t *testing.T) {
	v := ir.BoxView(ir.StackView(ir.TextView("one"), ir.TextView("two")))
	if got := renderViewStyled(v); got != ir.RenderViewPlain(v) {
		t.Errorf("an unstyled view was rewritten:\n%q", got)
	}
}

// Every colour a program may name has to be one the painter knows. The
// language's list is closed (ir.ParseViewStyle), and this is the other end of
// it: a name that parses but does not paint would be a colour that silently
// does nothing.
func TestEveryNamedColourPaints(t *testing.T) {
	for _, name := range []string{
		"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
		"bright-black", "bright-red", "bright-green", "bright-yellow",
		"bright-blue", "bright-magenta", "bright-cyan", "bright-white",
	} {
		st, err := ir.ParseViewStyle(name)
		if err != nil {
			t.Errorf("%q does not parse as a style: %v", name, err)
			continue
		}
		if _, ok := viewColours[st.FG]; !ok {
			t.Errorf("%q parses but the painter has no colour for it", name)
		}
	}
}

// stripANSI removes escape sequences, so a painted frame can be compared with
// the plain one.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// The comparison above is only meaningful if the painter is actually emitting
// escapes. This says so rather than letting the suite look stronger than it
// is: in an environment that renders no colour, styled and plain are trivially
// equal and prove nothing.
func TestPainterIsExercised(t *testing.T) {
	got := renderViewStyled(ir.StyledView(ir.TextView("x"), ir.ViewStyle{FG: "red", Bold: true}))
	if !strings.ContainsRune(got, 0x1b) {
		t.Skipf("this environment renders no colour (%q), so the styled/plain comparison is vacuous here", got)
	}
	if stripANSI(got) != "x" {
		t.Errorf("stripANSI(%q) = %q, want %q", got, stripANSI(got), "x")
	}
}
