package prims

import (
	"strings"
	"testing"
)

// The refusals `Cursed Object` / `Cursed Tool` are specified with. Each one is
// a rule from the design doc, and each says what to write instead — a global
// is a program-wide name, so an error about one that only said "no" would
// leave the reader guessing which of the three near-miss forms was meant.
func TestGlobalDeclarationErrors(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "a second declaration of a live name",
			src:  "Cursed Energy: stdin\nCursed Object: n As 1\nCursed Object: n As 2\nReveal: stdout\n",
			want: "is already a global, declared at",
		},
		{
			name: "assigning a name that was never declared",
			src:  "Cursed Energy: stdin\nCursed Object: total As 1\nCursed Tool: totl As 2\nReveal: stdout\n",
			want: `declared above this line: "total"`,
		},
		{
			name: "assigning with nothing declared at all",
			src:  "Cursed Energy: stdin\nCursed Tool: n As 2\nReveal: stdout\n",
			want: "no globals are declared above this line",
		},
		{
			name: "the type may not change",
			src:  "Cursed Energy: stdin\nCursed Object: n As 1\nCursed Tool: n As 1.5\nReveal: stdout\n",
			want: "holds Int, so `Cursed Tool` cannot write Float to it",
		},
		{
			name: "a global cannot be a function",
			src:  "Cursed Energy: stdin\nCursed Object: f As (x) -> x + 1\nReveal: stdout\n",
			want: "a global cannot be a function",
		},
		{
			name: "a global cannot take a builtin's name",
			src:  "Cursed Energy: stdin\nCursed Object: length As 1\nReveal: stdout\n",
			want: `is already the expression builtin "length"`,
		},
		{
			name: "a global cannot take a primitive's name",
			src:  "Cursed Energy: stdin\nCursed Object: Sum As 1\nReveal: stdout\n",
			want: "is already the built-in operation",
		},
		{
			name: "a global is a value, not something to call",
			src:  "Cursed Energy: stdin\nCursed Object: n As 1\nCursed Technique: Apply\n    Using: (t) -> n(t)\nReveal: stdout\n",
			want: "is a global, which is a value rather than a function",
		},
		{
			name: "walrus may not change the type either",
			src:  "Cursed Energy: stdin\nCursed Object: n As 1\nCursed Technique: Apply\n    Using: (t) -> n := 1.5\nReveal: stdout\n",
			want: "so := cannot write Float to it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveSrc(t, tc.src)
			if err == nil {
				t.Fatalf("%s resolved, want an error", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A global is in scope from its own line onward. Reading one above its
// declaration fails — and says why, rather than leaving "unknown identifier"
// to stand on its own when the name is right there further down the file.
func TestGlobalIsNotVisibleAboveItsDeclaration(t *testing.T) {
	src := "Cursed Energy: stdin\nCursed Technique: Apply\n    Using: (t) -> n\nCursed Object: n As 1\nReveal: stdout\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("a forward reference resolved, want an error")
	}
	for _, want := range []string{`unknown identifier "n"`, "is a global declared at", "onward"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// The slot count travels on the pipeline, because that is what a run sizes its
// array from. A program that declares none must not claim any.
func TestPipelineCarriesTheSlotCount(t *testing.T) {
	none, err := resolveSrc(t, "Cursed Energy: stdin\nReveal: stdout\n")
	if err != nil {
		t.Fatal(err)
	}
	if none.Globals != 0 {
		t.Errorf("Globals = %d for a program with none, want 0", none.Globals)
	}
	three, err := resolveSrc(t,
		"Cursed Energy: stdin\nCursed Object:\n    a As 1\n    b As 2\nCursed Object: c As 3\nReveal: stdout\n")
	if err != nil {
		t.Fatal(err)
	}
	if three.Globals != 3 {
		t.Errorf("Globals = %d, want 3", three.Globals)
	}
}

// The two constructs globals are unreachable from, in both directions. Each is
// sealed for its own reason and says so: a Channel because its evaluation
// order would become observable, an imported definition because its author
// never saw the calling program's names.
func TestGlobalsAreSealedFrom(t *testing.T) {
	const chanHead = "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Integers\nCursed Object: n As 0\n\n"
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "reading in a Channel",
			src:  chanHead + "Channel \"c\":\n    Cursed Technique: Apply\n        Using: (xs) -> n\n\nReveal: stdout\n",
			want: "cannot be read inside a Channel",
		},
		{
			name: "writing in a Channel",
			src:  chanHead + "Channel \"c\":\n    Cursed Tool: n As 7\n    Maximum Technique: Sum\n\nReveal: stdout\n",
			want: "Cursed Tool is not allowed inside a Channel",
		},
		{
			name: "declaring in a Channel",
			src:  chanHead + "Channel \"c\":\n    Cursed Object: m As 7\n    Maximum Technique: Sum\n\nReveal: stdout\n",
			want: "Cursed Object is not allowed inside a Channel",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveSrc(t, tc.src)
			if err == nil {
				t.Fatal("resolved, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			// The reason has to travel with the refusal: a bare "not allowed"
			// leaves the reader with no idea what to write instead.
			if !strings.Contains(err.Error(), "computed once") {
				t.Errorf("error = %q, want it to explain why", err)
			}
		})
	}
}

// An expression containing a global must never be folded. A fold runs while
// the program is still being lowered, when the slot array belongs to whatever
// ran last — folding one would read a stale value or none at all.
func TestGlobalsAreNotConstantFolded(t *testing.T) {
	src := "Cursed Energy: stdin\nCursed Object: n As 7\n" +
		"Cursed Technique: Apply\n    Consider k As n + 1\n    Using: (x) -> k\nReveal: stdout\n"
	if _, err := resolveSrc(t, src); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// A global's type is whatever its initialiser produces, and every type in the
// model is allowed — the slot holds an ir.Value like any other. These are the
// composite ones, since the scalars are covered everywhere else.
func TestGlobalTypesAcrossTheModel(t *testing.T) {
	for _, tc := range []struct{ name, src, want, bad string }{
		{
			name: "List",
			src:  "Cursed Object: xs Of Itself\n",
			want: "List<Int>", bad: `"nope"`,
		},
		{
			name: "Set",
			src:  "Cursed Object: s Of Convert To Set\n",
			want: "Set<Int>", bad: `"nope"`,
		},
		{
			name: "Map",
			src:  "Cursed Object: m Of Count By\n    Using: (x) -> x\n",
			want: "Map<Int, Int>", bad: `"nope"`,
		},
		{
			name: "Int from a sub-pipeline",
			src:  "Cursed Object: n Of\n    Maximum Technique: Sum\n",
			want: "Int", bad: `"nope"`,
		},
		{
			name: "Float",
			src:  "Cursed Object: f As 1.5\n",
			want: "Float", bad: `"nope"`,
		},
		{
			name: "Text",
			src:  "Cursed Object: t As \"hi\"\n",
			want: "Text", bad: "1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\n" +
				"Channeled Energy: Convert To Integers\n" + tc.src + "Reveal: stdout\n"
			pipe, err := resolveSrc(t, src)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if pipe.Globals != 1 {
				t.Fatalf("Globals = %d, want 1", pipe.Globals)
			}
			// The declared type is what a `Cursed Tool` and a `:=` are checked
			// against, so a write of the wrong type is refused and the error
			// names the type the global actually holds.
			bad := "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\n" +
				"Channeled Energy: Convert To Integers\n" + tc.src +
				"Cursed Tool: " + declaredName(tc.src) + " As " + tc.bad + "\nReveal: stdout\n"
			_, err = resolveSrc(t, bad)
			if err == nil {
				t.Fatalf("writing %s to a %s global resolved, want a type error", tc.bad, tc.want)
			}
			if !strings.Contains(err.Error(), "holds "+tc.want) {
				t.Errorf("error = %q, want it to say the global holds %s", err, tc.want)
			}
		})
	}
}

// declaredName is the name on the first declaration line of a snippet.
func declaredName(src string) string {
	line := strings.TrimPrefix(strings.SplitN(src, "\n", 2)[0], "Cursed Object: ")
	return strings.Fields(line)[0]
}
