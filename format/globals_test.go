package format

import "testing"

// A `Cursed Object:` / `Cursed Tool:` block is indented structure like any
// other, so the formatter normalizes it and leaves an already-canonical one
// alone. The declaration lines themselves are statement lines as far as the
// formatter is concerned — copied byte for byte — which is what keeps a
// hand-aligned expression on one from being rewritten.
func TestGlobalDeclFormatting(t *testing.T) {
	canonical := `Cursed Energy: stdin
Cursed Object: matches As 0
Cursed Object:
    a As 1
    doubled Of (x) -> length(x) * 2
Cursed Tool: matches As matches + 1
Reveal: stdout
`
	out, err := Format(canonical)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if out != canonical {
		t.Errorf("canonical source changed:\ngot:\n%s\nwant:\n%s", out, canonical)
	}
	again, err := Format(out)
	if err != nil {
		t.Fatalf("reformat: %v", err)
	}
	if again != out {
		t.Errorf("not idempotent:\ngot:\n%s\nwant:\n%s", again, out)
	}
}

func TestGlobalDeclFormattingNormalizesIndent(t *testing.T) {
	src := "Cursed Energy: stdin\nCursed Object:\n  a As 1\n  b As 2\nReveal: stdout\n"
	want := "Cursed Energy: stdin\nCursed Object:\n    a As 1\n    b As 2\nReveal: stdout\n"
	out, err := Format(src)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

// A declaration's `Of` body is an ordinary sub-pipeline, so a `Consider`
// written inside one is normalized like any other. The declaration line itself
// is deliberately left alone: renderBinding normalizes a `Consider NAME As|Of`
// head, and a declaration has no `Consider` to normalize.
func TestFormatterNormalizesInsideADeclarationBody(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Object: total Of
    Cursed Technique: Filter
        consider   biggest   of   Max
        Using: (x) -> x = biggest
    Maximum Technique: Sum
Reveal: stdout
`
	want := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Object: total Of
    Cursed Technique: Filter
        Consider biggest Of Max
        Using: (x) -> x = biggest
    Maximum Technique: Sum
Reveal: stdout
`
	got, err := Format(src)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
