package prims

import (
	"bytes"
	"strings"
	"testing"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
)

// runProgramWithInput resolves and interprets src against the given stdin,
// returning what it printed.
func runProgramWithInput(t *testing.T, src, stdin string) (string, error) {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		return "", err
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return "", err
	}
	pipe, err := Resolve(prog)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader(stdin), Stdout: &out}
	if _, err := interp.Run(pipe, ctx); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// runProgram interprets src with empty stdin.
func runProgram(t *testing.T, src string) (string, error) {
	t.Helper()
	return runProgramWithInput(t, src, "")
}

func TestPartLabelsOutput(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers

Part "1":
    Maximum Technique: Max
    Reveal: stdout

Part "2":
    Maximum Technique: Sum
    Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "3,1,4,1,5")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "Part 1: 5\nPart 2: 14\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// Sibling Parts branch from the same upstream value: the parse above them
// happens once, and Part 1 cannot disturb what Part 2 sees.
func TestPartsSeeTheSameUpstreamValue(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers

Part "1":
    Domain Expansion: Quicksort, Descending
    Cursed Technique: Take Item 0
    Reveal: stdout

Part "2":
    Cursed Technique: Take Item 0
    Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "3,1,4,1,5")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Part 1 sorts (5); Part 2 must still see the original order (3).
	if want := "Part 1: 5\nPart 2: 3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// The main pipeline value flows past a Part untouched, so a top-level Reveal
// after Parts still prints the upstream value.
func TestPartIsPassthrough(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers

Part "1":
    Maximum Technique: Sum
    Reveal: stdout

Maximum Technique: Count
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "3,1,4")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "Part 1: 8\n3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// A multi-line value goes on the lines after its label so a grid stays aligned.
func TestPartMultiLineValueFormat(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Lines

Part "picture":
    Channeled Energy: Convert To Grid
    Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "ab\ncd")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "Part picture:\nab\ncd\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// A Part with no Reveal is legal (the linter warns) and prints nothing.
func TestPartWithoutRevealPrintsNothing(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","

Part "quiet":
    Maximum Technique: Count

Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "a,b")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[a, b]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// A Part may consume a Channel defined above it — that is the whole point of
// scopePart, and it is what lets one parse feed both answers.
func TestPartConsumesChannel(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","

Channel "counts":
    Maximum Technique: Count

Part "1":
    Maximum Technique: Combine
        From: counts
        Using: (c) -> c * 10
    Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "a,b,c")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "Part 1: 30\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestPartResolveErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			"duplicate label",
			"Cursed Energy: stdin\nPart \"1\":\n    Reveal: stdout\nPart \"1\":\n    Reveal: stdout\n",
			"already defined",
		},
		{
			"no upstream value",
			"Part \"1\":\n    Reveal: stdout\n",
			"no upstream value",
		},
		{
			"channel defined inside a Part",
			"Cursed Energy: stdin\nPart \"1\":\n    Channel \"c\":\n        Maximum Technique: Count\n    Reveal: stdout\n",
			"Channels cannot be defined inside a Part",
		},
		{
			"part nested in a channel",
			"Cursed Energy: stdin\nChannel \"c\":\n    Part \"1\":\n        Reveal: stdout\n",
			"only allowed at the top level",
		},
		{
			"part nested in a part",
			"Cursed Energy: stdin\nPart \"1\":\n    Part \"2\":\n        Reveal: stdout\n",
			"only allowed at the top level",
		},
		{
			"part nested in a loop",
			"Cursed Energy: stdin\nSimple Domain: Repeat 2\n    Part \"1\":\n        Reveal: stdout\n",
			"only allowed at the top level",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runProgram(t, c.src)
			if err == nil {
				t.Fatalf("expected an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.want)
			}
		})
	}
}

func TestPartParseErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"missing body",
			"Cursed Energy: stdin\nPart \"1\":\nReveal: stdout\n",
			"must be followed by an indented sub-pipeline",
		},
		{
			"missing colon",
			"Cursed Energy: stdin\nPart \"1\"\n    Reveal: stdout\n",
			"expected",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runProgram(t, c.src)
			if err == nil {
				t.Fatalf("expected an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.want)
			}
		})
	}
}

// A Part is reached through its parent's Eval closure, so the label has to live
// on the Context. Confirm it is restored afterwards, leaving no leakage into a
// following top-level Reveal.
func TestPartLabelIsRestored(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","

Part "1":
    Maximum Technique: Count
    Reveal: stdout

Maximum Technique: Count
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "a,b")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "Part 1: 2\n2\n"; got != want {
		t.Errorf("label leaked past the Part: output = %q, want %q", got, want)
	}
}

func TestLabelledOutput(t *testing.T) {
	cases := []struct{ label, rendered, want string }{
		{"", "5", "5"},
		{"1", "5", "Part 1: 5"},
		{"totals", "45000", "Part totals: 45000"},
		{"picture", "ab\ncd", "Part picture:\nab\ncd"},
		{"", "ab\ncd", "ab\ncd"},
	}
	for _, c := range cases {
		if got := ir.LabelledOutput(c.label, c.rendered); got != c.want {
			t.Errorf("LabelledOutput(%q, %q) = %q, want %q", c.label, c.rendered, got, c.want)
		}
	}
}
