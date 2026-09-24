package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/docs"
)

// data-model.md's Rendering section is a list of shapes: what each composite
// type looks like when a program prints it. Nothing ran any of them, and one
// was wrong — it promised a Tuple as `(v1, v2, …)`, which is how the *type*
// prints. A Tuple value is a []Value and comes out `[v1, v2]`, exactly like a
// List, which is what every tuple printed anywhere else in the documentation
// had been showing all along.
//
// Each bullet now has a program that produces that shape, and the test holds
// three things together: the program prints what the page says, the page still
// says it, and the shape is stable in both optimizer modes.

type renderCase struct {
	name    string
	program string
	input   string
	output  string
	// documented is the rendering as data-model.md spells it, with the page's
	// placeholder names — the substring the bullet has to keep containing.
	documented string
}

var renderCases = []renderCase{
	{
		name: "Tuple",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(item(xs, 0), item(xs, 1), 9)
Reveal: stdout
`,
		input: "1,2", output: "[1, 2, 9]", documented: "`[v1, v2, …]`",
	},
	{
		name: "Record",
		program: `Cursed Energy: stdin
Cursed Technique: Match Pattern
    Mode: One
    Using: "{a:int},{b:int}"
Reveal: stdout
`,
		input: "1,2", output: "{a: 1, b: 2}", documented: "`{a: v1, b: v2}`",
	},
	{
		name: "Map",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Maximum Technique: Count By
    Using: (w) -> w
Reveal: stdout
`,
		input: "a,b,a", output: "{a: 2, b: 1}", documented: "`{k1: v1, k2: v2}`",
	},
	{
		name: "Set",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Set
Reveal: stdout
`,
		input: "a,b,a", output: "{a, b}", documented: "`{v1, v2, …}`",
	},
	{
		name: "Grid",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Reveal: stdout
`,
		input: "ab\ncd", output: "ab\ncd", documented: "rows joined by newlines",
	},
	{
		name: "Sparse",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Channeled Energy: Convert To Sparse
    Default: "."
Reveal: stdout
`,
		input: ".#\n..", output: "{[0, 1]: #}", documented: "`{[r, c]: v, …}`",
	},
	{
		name: "Graph",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(item(p, 0), item(p, 1), toint(item(p, 2)))
Channeled Energy: Convert To Graph
Reveal: stdout
`,
		input: "a b 1\nb c 2", output: "{a: [(b, 1)], b: [(c, 2)], c: []}",
		documented: "`{a: [(b, 1), (c, 2)], b: [], c: []}`",
	},
}

func TestDocumentedRenderings(t *testing.T) {
	page, err := docs.FS.ReadFile("data-model.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range renderCases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			prog := filepath.Join(dir, "p.domain")
			if err := os.WriteFile(prog, []byte(c.program), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, opt := range []bool{true, false} {
				var out, errBuf bytes.Buffer
				if err := Execute(prog, Options{Optimize: opt},
					strings.NewReader(c.input), &out, &errBuf); err != nil {
					t.Fatalf("optimize=%v: %v\n%s", opt, err, errBuf.String())
				}
				if got := strings.TrimRight(out.String(), "\n"); got != c.output {
					t.Errorf("optimize=%v: printed %q, want %q", opt, got, c.output)
				}
			}
			if !strings.Contains(string(page), c.documented) {
				t.Errorf("data-model.md's Rendering section no longer describes %s as %s",
					c.name, c.documented)
			}
		})
	}

	// The trap this section fell into: the type's notation is not the value's.
	if !strings.Contains(string(page), "no value ever renders that way") {
		t.Error("data-model.md no longer warns that `(T1, T2)` is the type's spelling, not a value's")
	}
}
