package format

import "testing"

func formatted(t *testing.T, src string) string {
	t.Helper()
	out, err := Format(src)
	if err != nil {
		t.Fatalf("Format(%q): %v", src, err)
	}
	// Formatting must be idempotent, foreign blocks included — and a block that
	// drifted by one column on each pass would still look right on the first.
	again, err := Format(out)
	if err != nil {
		t.Fatalf("Format is not idempotent, second pass failed: %v", err)
	}
	if again != out {
		t.Errorf("not idempotent:\nfirst  %q\nsecond %q", out, again)
	}
	return out
}

func TestFormatLeavesForeignBlockAlone(t *testing.T) {
	src := "Cursed Energy: input.txt\n" +
		"Domain Expansion: Python\n" +
		"    import sys   # trailing spaces and a # comment   \n" +
		"    d = {  'a' :1 }\n" +
		"\n" +
		"    if d:\n" +
		"            print( d )\n" +
		"Reveal: stdout\n"
	if got := formatted(t, src); got != src {
		t.Errorf("foreign block was reformatted:\ngot  %q\nwant %q", got, src)
	}
}

func TestFormatKeepsTabsInsideForeignBlock(t *testing.T) {
	src := "Domain Expansion: Go\n\tpackage main\n\n\tfunc main() {\n\t\tprintln(1)\n\t}\n"
	if got := formatted(t, src); got != src {
		t.Errorf("tabs did not survive:\ngot  %q\nwant %q", got, src)
	}
}

// The block belongs to its opener by indentation, so it has to travel with it.
// Here the opener is under-indented and moves right by one column.
func TestFormatShiftsForeignBlockWithItsOpener(t *testing.T) {
	src := "Part \"1\":\n" +
		"   Domain Expansion: Python\n" +
		"       print(1)\n" +
		"   Reveal: stdout\n"
	want := "Part \"1\":\n" +
		"    Domain Expansion: Python\n" +
		"        print(1)\n" +
		"    Reveal: stdout\n"
	if got := formatted(t, src); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// The dangerous direction: the opener moves *left*, and a block that stayed put
// would be fine, but one shifted naively could end up level with its opener and
// stop being a block at all. The shift is clamped to what the block can give.
func TestFormatShiftLeftPreservesForeignBlockStructure(t *testing.T) {
	src := "Part \"1\":\n" +
		"          Domain Expansion: Python\n" +
		"           if 1:\n" +
		"               print(1)\n"
	got := formatted(t, src)
	want := "Part \"1\":\n" +
		"    Domain Expansion: Python\n" +
		"     if 1:\n" +
		"         print(1)\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	// And the result still parses as one block: the interior relative
	// indentation (4 columns between the two Python lines) is untouched.
	if _, err := Format(got); err != nil {
		t.Fatalf("re-formatting the result failed: %v", err)
	}
}

// A blank line inside a foreign block is that language's blank line, not a
// separator the formatter may collapse.
func TestFormatKeepsRepeatedBlankLinesInsideForeignBlock(t *testing.T) {
	src := "Domain Expansion: Python\n    a = 1\n\n\n    b = 2\nReveal: stdout\n"
	if got := formatted(t, src); got != src {
		t.Errorf("got  %q\nwant %q", got, src)
	}
}

// Blank lines *after* the block are ordinary layout and collapse as usual.
func TestFormatCollapsesBlankLinesAfterForeignBlock(t *testing.T) {
	src := "Domain Expansion: Python\n    a = 1\n\n\nReveal: stdout\n"
	want := "Domain Expansion: Python\n    a = 1\n\nReveal: stdout\n"
	if got := formatted(t, src); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
