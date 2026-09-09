package format

import "testing"

// The formatter is line-oriented and copies a statement's interior verbatim,
// so a Part carrying a role word needs no special handling — but "needs none"
// is a claim, and a mangled game file would be a quiet, annoying way to
// discover it was wrong. These are the shapes the parser accepts.
func TestFormatLeavesPartRolesIntact(t *testing.T) {
	for _, src := range []string{
		"Part World:\n    Cursed Energy: stdin\n",
		"Part Draw:\n    Cursed Energy: stdin\n",
		"Part Entity \"Creep\":\n    Cursed Energy: stdin\n",
		"Part Every 120:\n    Cursed Energy: stdin\n",
		"Part \"1\":\n    Reveal: stdout\n",
	} {
		out, err := Format(src)
		if err != nil {
			t.Errorf("Format(%q): %v", src, err)
			continue
		}
		if out != src {
			t.Errorf("Format(%q) = %q, want it unchanged", src, out)
		}
	}
}

// The two declaration keywords normalize their own segment like any other.
func TestFormatNormalizesDeclarationKeywords(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Innate Domain:   Advent of Code\nReveal: stdout\n", "Innate Domain: Advent of Code\nReveal: stdout\n"},
		{"Inherited Technique:  lib/shapes\nReveal: stdout\n", "Inherited Technique: lib/shapes\nReveal: stdout\n"},
	}
	for _, c := range cases {
		out, err := Format(c.in)
		if err != nil {
			t.Errorf("Format(%q): %v", c.in, err)
			continue
		}
		if out != c.want {
			t.Errorf("Format(%q) = %q, want %q", c.in, out, c.want)
		}
	}
}
