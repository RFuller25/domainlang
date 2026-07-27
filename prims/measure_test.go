package prims

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
)

// Measured arguments: an Int argument given as a lambda over the current value
// instead of a literal. These cover the shared reader in measure.go through the
// three primitives that take one, plus the errors that only exist because the
// value arrives late.

func TestMeasuredWindowSizesFromTheCurrentList(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Window\n" +
		"    Size: (xs) -> length(xs) / 2\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5,6")
	if got := ir.FormatValue(v); got != "[[1, 2, 3], [2, 3, 4], [3, 4, 5], [4, 5, 6]]" {
		t.Fatalf("measured window: got %s", got)
	}
	// The same program over a different input measures a different size —
	// which is the whole point, and what a literal cannot do.
	v, _ = runPipeline(t, src, "1,2,3,4")
	if got := ir.FormatValue(v); got != "[[1, 2], [2, 3], [3, 4]]" {
		t.Fatalf("measured window re-measures: got %s", got)
	}
}

func TestMeasuredWindowStep(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Window 2\n" +
		"    Step: (xs) -> length(xs) / 3\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5,6")
	if got := ir.FormatValue(v); got != "[[1, 2], [3, 4], [5, 6]]" {
		t.Fatalf("measured step: got %s", got)
	}
}

func TestMeasuredChunkAndSelectTopK(t *testing.T) {
	chunk := intsPrelude +
		"Cursed Technique: Chunk\n" +
		"    Size: (xs) -> length(xs) / 3\n"
	v, _ := runPipeline(t, chunk, "1,2,3,4,5,6")
	if got := ir.FormatValue(v); got != "[[1, 2], [3, 4], [5, 6]]" {
		t.Fatalf("measured chunk: got %s", got)
	}

	top := intsPrelude +
		"Domain Expansion: Quicksort, Descending\n" +
		"Maximum Technique: Select Top\n" +
		"    Count: (xs) -> length(xs) / 2\n"
	v, _ = runPipeline(t, top, "3,1,4,1,5,9")
	if got := ir.FormatValue(v); got != "[9, 5, 4]" {
		t.Fatalf("measured select top: got %s", got)
	}
}

// A measured argument accepts a plain Int in the same named slot, so there is
// one spelling rule rather than two.
func TestMeasuredSlotAcceptsALiteral(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Window\n" +
		"    Size: 2\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[[1, 2], [2, 3]]" {
		t.Fatalf("Size: literal: got %s", got)
	}
	// And it still lands in Meta as a literal, which is what keeps the
	// optimizer's constant-folding passes eligible.
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range pipe.Nodes {
		if n.Prim != "Window" {
			continue
		}
		if size, ok := n.Meta["size"].(int64); !ok || size != 2 {
			t.Fatalf("Size: 2 should be literal metadata, got %#v", n.Meta)
		}
		if _, measured := n.Meta["sizeExpr"]; measured {
			t.Fatalf("a literal Size: must not produce measured metadata: %#v", n.Meta)
		}
	}
}

// A measured argument lands under its own key so no pass can mistake a missing
// literal for zero; the literal key stays literal-only.
func TestMeasuredMetadataIsSeparateFromLiteral(t *testing.T) {
	pipe, err := resolveSrc(t, intsPrelude+
		"Cursed Technique: Window\n"+
		"    Size: (xs) -> length(xs) / 2\n")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range pipe.Nodes {
		if n.Prim != "Window" {
			continue
		}
		found = true
		if _, ok := n.Meta["size"]; ok {
			t.Fatalf("a measured size must not write the literal key: %#v", n.Meta)
		}
		if _, ok := n.Meta["sizeExpr"].(*ast.Lambda); !ok {
			t.Fatalf("missing measured metadata: %#v", n.Meta)
		}
		if step, ok := n.Meta["step"].(int64); !ok || step != 1 {
			t.Fatalf("an unwritten step stays the literal default: %#v", n.Meta)
		}
	}
	if !found {
		t.Fatal("no Window node resolved")
	}
}

func TestMeasuredArgumentErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "both forms at one slot",
			src:  intsPrelude + "Cursed Technique: Window 3\n    Size: (xs) -> 2\n",
			want: "written twice",
		},
		{
			name: "lambda producing the wrong type",
			src:  intsPrelude + "Cursed Technique: Window\n    Size: (xs) -> \"two\"\n",
			want: "must produce Int",
		},
		{
			name: "lambda with the wrong arity",
			src:  intsPrelude + "Cursed Technique: Window\n    Size: (xs, y) -> 2\n",
			want: "must take 1 parameter(s)",
		},
		{
			name: "an ill-typed body still reports through the argument",
			src:  intsPrelude + "Cursed Technique: Window\n    Size: (xs) -> nosuchfn(xs)\n",
			want: "Window: Size:",
		},
		{
			name: "a missing size still names the literal spelling",
			src:  intsPrelude + "Cursed Technique: Window\n",
			want: "requires a size, e.g. Window 3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveSrc(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// A bound a literal fails at resolve time can only be checked once a measured
// argument has been measured. It is an error rather than a clamp: a window
// silently widened to 1 is a wrong answer that looks right.
func TestMeasuredBoundsFailAtRuntime(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Window\n" +
		"    Size: (xs) -> length(xs) / 2\n"
	if _, err := resolveSrc(t, src); err != nil {
		t.Fatalf("a measured size must resolve; the bound is a runtime question: %v", err)
	}
	_, err := runErr(t, src, "7")
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("want a measured-bound runtime error, got %v", err)
	}
	if !strings.Contains(err.Error(), "measured 0") {
		t.Fatalf("the error should say what it measured, got %v", err)
	}
}

func TestMeasuredSelectTopKRejectsNegativeAtRuntime(t *testing.T) {
	src := intsPrelude +
		"Maximum Technique: Select Top\n" +
		"    Count: (xs) -> 0 - length(xs)\n"
	_, err := runErr(t, src, "1,2")
	if err == nil || !strings.Contains(err.Error(), "must be >= 0") {
		t.Fatalf("want a negative-count runtime error, got %v", err)
	}
}

// Inside a For loop a measuring lambda takes the lap's binding as a trailing
// parameter, the same rule requireLambda applies to Using:.
func TestMeasuredArgumentBindsAmbientLoopVariables(t *testing.T) {
	src := intsPrelude +
		"Simple Domain: For k in range(2)\n" +
		"    Cursed Technique: Window\n" +
		"        Size: (xs, k) -> k + 1\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (w, k) -> sum(w)\n"
	v, _ := runPipeline(t, src, "1,2,3")
	// Lap 0 windows by 1, lap 1 by 2 over the previous lap's sums.
	if got := ir.FormatValue(v); got != "[3, 5]" {
		t.Fatalf("ambient measured size: got %s", got)
	}

	bad := intsPrelude +
		"Simple Domain: For k in range(2)\n" +
		"    Cursed Technique: Window\n" +
		"        Size: (xs) -> 1\n"
	if _, err := resolveSrc(t, bad); err == nil ||
		!strings.Contains(err.Error(), "must take 2 parameter(s)") {
		t.Fatalf("a lambda missing its ambient parameter should be rejected, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// The rest of the counts: every phrase Int in the language that carries data
// ---------------------------------------------------------------------------

func TestMeasuredCountsAcrossTheVocabulary(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		stdin string
		want  string
	}{
		{
			name: "sliding reduce size",
			src: intsPrelude + "Domain Expansion: Sliding Reduce\n" +
				"    Size: (xs) -> length(xs) / 2\n    Mode: Sum\n",
			stdin: "1,2,3,4,5,6",
			want:  "[6, 9, 12, 15]",
		},
		{
			name:  "take item index",
			src:   intsPrelude + "Cursed Technique: Take Item\n    Index: (xs) -> length(xs) - 1\n",
			stdin: "4,8,15,16",
			want:  "16",
		},
		{
			name: "iterate times",
			src: intsPrelude + "Maximum Technique: Sum\n" +
				"Cursed Technique: Iterate\n    Times: (n) -> 3\n    Using: (n) -> n * 2\n",
			stdin: "1,2,3",
			want:  "[12, 24, 48]",
		},
		{
			name: "repeat times",
			src: intsPrelude + "Simple Domain: Repeat\n    Times: (xs) -> length(xs)\n" +
				"    Cursed Technique: Map Each\n        Using: (n) -> n + 1\n",
			stdin: "1,2,3",
			want:  "[4, 5, 6]",
		},
		{
			// Range discards the current value, but the measuring lambda still
			// sees it — which is the point: no literal spelling can say this.
			name:  "range high from the input length",
			src:   intsPrelude + "Cursed Technique: Range\n    High: (xs) -> length(xs)\n",
			stdin: "7,7,7,7",
			want:  "[0, 1, 2, 3]",
		},
		{
			name: "range low and high",
			src: intsPrelude + "Cursed Technique: Range\n" +
				"    Low: (xs) -> first(xs)\n    High: (xs) -> last(xs)\n",
			stdin: "2,6",
			want:  "[2, 3, 4, 5]",
		},
		{
			name:  "count vow",
			src:   intsPrelude + "Binding Vow: Count Equals\n    Count: (xs) -> 3\n",
			stdin: "1,2,3",
			want:  "[1, 2, 3]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, _ := runPipeline(t, tc.src, tc.stdin)
			if got := ir.FormatValue(v); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

// Each of these bounds is checked where the value arrives, not where it is
// written, and reports what it measured.
func TestMeasuredCountRuntimeErrors(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		stdin string
		want  string
	}{
		{
			name:  "take item out of range",
			src:   intsPrelude + "Cursed Technique: Take Item\n    Index: (xs) -> 99\n",
			stdin: "1,2,3",
			want:  "index 99 out of range (length 3)",
		},
		{
			name:  "iterate negative count",
			src:   intsPrelude + "Cursed Technique: Iterate\n    Times: (xs) -> 0 - 1\n    Using: (xs) -> xs\n",
			stdin: "1,2",
			want:  "must be >= 0",
		},
		{
			name: "repeat negative count",
			src: intsPrelude + "Simple Domain: Repeat\n    Times: (xs) -> 0 - length(xs)\n" +
				"    Cursed Technique: Map Each\n        Using: (n) -> n\n",
			stdin: "1,2",
			want:  "must be >= 0",
		},
		{
			name:  "inverted range bounds",
			src:   intsPrelude + "Cursed Technique: Range\n    Low: (xs) -> 9\n    High: (xs) -> 2\n",
			stdin: "1",
			want:  "half-open [9, 2)",
		},
		{
			name:  "count vow violated",
			src:   intsPrelude + "Binding Vow: Count Equals\n    Count: (xs) -> 99\n",
			stdin: "1,2,3",
			want:  "expected count 99, got 3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveSrc(t, tc.src); err != nil {
				t.Fatalf("a measured argument must resolve; its bound is a runtime question: %v", err)
			}
			_, err := runErr(t, tc.src, tc.stdin)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// Take Item keeps a literal index in Meta as an int, the shape two optimizer
// passes match on. A measured one must not land there wearing that shape.
func TestMeasuredTakeItemKeepsTheLiteralMetadataShape(t *testing.T) {
	lit, err := resolveSrc(t, intsPrelude+"Cursed Technique: Take Item 1\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range lit.Nodes {
		if n.Prim != "Take Item" {
			continue
		}
		if idx, ok := n.Meta["index"].(int); !ok || idx != 1 {
			t.Fatalf("a literal index must stay an int in Meta: %#v", n.Meta)
		}
	}
	measured, err := resolveSrc(t, intsPrelude+"Cursed Technique: Take Item\n    Index: (xs) -> 1\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range measured.Nodes {
		if n.Prim != "Take Item" {
			continue
		}
		if _, ok := n.Meta["index"]; ok {
			t.Fatalf("a measured index must not write the literal key: %#v", n.Meta)
		}
	}
}

// ---------------------------------------------------------------------------
// Text slots: separators
// ---------------------------------------------------------------------------

// A separator is data, so it measures like a count does — the only difference
// being that its lambda returns Text.
func TestMeasuredSeparators(t *testing.T) {
	pick := `    By: (t) -> if indexof(t, "|") >= 0 then "|" else ","` + "\n"
	src := "Cursed Energy: stdin\nCursed Technique: Split\n" + pick

	v, _ := runPipeline(t, src, "a,b,c")
	if got := ir.FormatValue(v); got != "[a, b, c]" {
		t.Fatalf("measured comma split: got %s", got)
	}
	// The same program, a different input, a different separator.
	v, _ = runPipeline(t, src, "a|b|c")
	if got := ir.FormatValue(v); got != "[a, b, c]" {
		t.Fatalf("measured pipe split: got %s", got)
	}

	each := "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each\n    By: (xs) -> \",\"\n"
	v, _ = runPipeline(t, each, "a,b\nc,d")
	if got := ir.FormatValue(v); got != "[[a, b], [c, d]]" {
		t.Fatalf("measured Split Each: got %s", got)
	}

	join := "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\n" +
		"Maximum Technique: Join\n    With: (xs) -> if length(xs) > 2 then \" | \" else \"-\"\n"
	v, _ = runPipeline(t, join, "a,b,c")
	if got := ir.FormatValue(v); got != "a | b | c" {
		t.Fatalf("measured Join: got %s", got)
	}
	v, _ = runPipeline(t, join, "a,b")
	if got := ir.FormatValue(v); got != "a-b" {
		t.Fatalf("measured Join, short list: got %s", got)
	}
}

func TestMeasuredSeparatorErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "both forms at one slot",
			src:  "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\n    By: (t) -> \"-\"\n",
			want: "written twice",
		},
		{
			name: "lambda producing the wrong type",
			src:  "Cursed Energy: stdin\nCursed Technique: Split\n    By: (t) -> 3\n",
			want: "lambda must produce Text, got Int",
		},
		{
			name: "no separator in either form",
			src:  "Cursed Energy: stdin\nCursed Technique: Split\n",
			want: "requires a separator string",
		},
		{
			name: "the missing-separator error names the measured form too",
			src:  "Cursed Energy: stdin\nCursed Technique: Split\n",
			want: "measured `By:` lambda",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveSrc(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// A measured separator writes no literal key, which is what stands the
// compiler's separator-keyed fusions down. Three of them fire on `sep == ""`,
// so a missing literal read as "" would fire them wrongly rather than not at
// all — the same hazard the optimizer's guard exists for.
func TestMeasuredSeparatorWritesNoLiteralKey(t *testing.T) {
	pipe, err := resolveSrc(t, "Cursed Energy: stdin\nCursed Technique: Split\n    By: (t) -> \"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range pipe.Nodes {
		if n.Prim != "Split" {
			continue
		}
		if _, ok := n.Meta["sep"]; ok {
			t.Fatalf("a measured separator must not write the literal key: %#v", n.Meta)
		}
		if _, ok := n.Meta["sepExpr"].(*ast.Lambda); !ok {
			t.Fatalf("missing measured metadata: %#v", n.Meta)
		}
	}
}

// ---------------------------------------------------------------------------
// Value slots: fills and defaults
// ---------------------------------------------------------------------------

// A fill or a default is a value whose *type* is part of what it says, so a
// measured one answers with the type its lambda body produces — and is checked
// against the cells it has to match exactly as a literal is.
func TestMeasuredValueSlots(t *testing.T) {
	grid := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n"

	pad := grid + "Cursed Technique: Pad Grid 1\n    Fill: (g) -> at(g, 0, 0)\n"
	v, _ := runPipeline(t, pad, "ab\ncd")
	if got := ir.FormatValue(v); got != "aaaa\naaba\nacda\naaaa" {
		t.Fatalf("measured Fill:: got %q", got)
	}

	sparse := grid + "Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: (g) -> at(g, 0, 0)\n" + "Channeled Energy: Convert To Grid\n"
	v, _ = runPipeline(t, sparse, "ab\ncd")
	if got := ir.FormatValue(v); got != "ab\ncd" {
		t.Fatalf("measured Default:: got %q", got)
	}

	points := intsPrelude + "Cursed Technique: Chunk 2\n" +
		"Channeled Energy: Convert To Sparse Grid\n" +
		"    Default: (ps) -> \".\"\n    Mark: (ps) -> \"#\"\n" +
		"Channeled Energy: Convert To Grid\n"
	v, _ = runPipeline(t, points, "0,0,1,1")
	if got := ir.FormatValue(v); got != "#.\n.#" {
		t.Fatalf("measured Default:/Mark:: got %q", got)
	}
}

func TestMeasuredValueSlotErrors(t *testing.T) {
	grid := "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n"
	cases := []struct{ name, src, want string }{
		{
			// The type a measured argument produces is checked against the one
			// it has to match, exactly as a literal's is — at resolve time.
			name: "fill type must match the cells",
			src:  grid + "Cursed Technique: Pad Grid 1\n    Fill: (g) -> 3\n",
			want: "Fill: is Int but the grid holds Text",
		},
		{
			name: "sparse element types stay Int or Text",
			src:  grid + "Channeled Energy: Convert To Sparse Grid\n    Default: (g) -> list(1, 2)\n",
			want: "Default: must be Int or Text, got List<Int>",
		},
		{
			name: "a missing value slot names both forms",
			src:  grid + "Cursed Technique: Pad Grid 1\n",
			want: "an Int or Text literal, or a lambda over the current value",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveSrc(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Fold's seed — the slot where measuring widens what the primitive can do
// ---------------------------------------------------------------------------

// A literal seed can only be Int or Text, because those are the two a named
// argument can spell. A measured one takes its type from the lambda body, so
// the accumulator can be a composite — which docs/expressions.md lists as a
// thing the language could not express.
func TestMeasuredFoldSeedWidensTheAccumulator(t *testing.T) {
	tuple := intsPrelude + "Maximum Technique: Fold\n" +
		"    Seed: (xs) -> tuple(0, 0)\n" +
		"    Using: (acc, x) -> tuple(prow(acc) + x, pcol(acc) + 1)\n"
	v, _ := runPipeline(t, tuple, "3,1,4,1,5")
	if got := ir.FormatValue(v); got != "[14, 5]" {
		t.Fatalf("tuple accumulator: got %s", got)
	}

	list := intsPrelude + "Maximum Technique: Fold\n" +
		"    Seed: (xs) -> list(0)\n" +
		"    Using: (acc, x) -> concat(acc, list(last(acc) + x))\n"
	v, _ = runPipeline(t, list, "1,2,3")
	if got := ir.FormatValue(v); got != "[0, 1, 3, 6]" {
		t.Fatalf("list accumulator: got %s", got)
	}

	// And the ordinary win: a seed taken from the data itself.
	fromData := intsPrelude + "Maximum Technique: Fold\n" +
		"    Seed: (xs) -> first(xs)\n    Using: (acc, x) -> max(acc, x)\n"
	v, _ = runPipeline(t, fromData, "3,1,4,1,5")
	if got := ir.FormatValue(v); got != "5" {
		t.Fatalf("seed from the data: got %s", got)
	}

	scan := intsPrelude + "Cursed Technique: Scan\n" +
		"    Seed: (xs) -> length(xs)\n    Using: (acc, x) -> acc + x\n"
	v, _ = runPipeline(t, scan, "1,2,3")
	if got := ir.FormatValue(v); got != "[4, 6, 9]" {
		t.Fatalf("measured Scan seed: got %s", got)
	}
}

// The lambda's return type is still checked against the accumulator, so a
// measured seed cannot smuggle a mismatch past the type checker.
func TestMeasuredFoldSeedStillTypesTheLambda(t *testing.T) {
	src := intsPrelude + "Maximum Technique: Fold\n" +
		"    Seed: (xs) -> tuple(0, 0)\n    Using: (acc, x) -> x\n"
	_, err := resolveSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "must return the accumulator type (Int, Int)") {
		t.Fatalf("want an accumulator-type error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// The grid family
// ---------------------------------------------------------------------------

const gridPrelude = "Cursed Energy: stdin\nShikigami: Lines\nChanneled Energy: Convert To Grid\n"

// A grid can name its own crop, its own border and its own search start —
// the arguments that most obviously want to be relative to the data.
func TestMeasuredGridArguments(t *testing.T) {
	crop := gridPrelude + "Cursed Technique: Subgrid\n" +
		"    Row: (g) -> 0\n    Col: (g) -> 0\n" +
		"    Height: (g) -> rows(g) / 2\n    Width: (g) -> cols(g) / 2\n"
	v, _ := runPipeline(t, crop, "abcd\nefgh\nijkl\nmnop")
	if got := ir.FormatValue(v); got != "ab\nef" {
		t.Fatalf("measured Subgrid: got %q", got)
	}

	pad := gridPrelude + "Cursed Technique: Pad Grid\n" +
		"    Thickness: (g) -> rows(g)\n    Fill: (g) -> \".\"\n"
	v, _ = runPipeline(t, pad, "ab\ncd")
	if got := ir.FormatValue(v); got != "......\n......\n..ab..\n..cd..\n......\n......" {
		t.Fatalf("measured Pad Grid: got %q", got)
	}

	// A search that starts from the far corner, whichever corner that is.
	bfs := gridPrelude + "Domain Expansion: BFS\n" +
		"    Row: (g) -> rows(g) - 1\n    Col: (g) -> cols(g) - 1\n" +
		"    Using: (c) -> c = \".\"\n"
	v, _ = runPipeline(t, bfs, "...\n.#.\n...")
	if got := ir.FormatValue(v); got != "4 3 2\n3 -1 1\n2 1 0" {
		t.Fatalf("measured BFS start: got %q", got)
	}
}

func TestMeasuredGridRuntimeErrors(t *testing.T) {
	cases := []struct{ name, src, stdin, want string }{
		{
			name: "a crop that does not fit is still the fit error",
			src: gridPrelude + "Cursed Technique: Subgrid\n" +
				"    Row: (g) -> 0\n    Col: (g) -> 0\n" +
				"    Height: (g) -> rows(g) + 1\n    Width: (g) -> 1\n",
			stdin: "ab\ncd",
			want:  "does not fit a 2x2 grid",
		},
		{
			name: "an out-of-bounds start is still the start error",
			src: gridPrelude + "Domain Expansion: BFS\n" +
				"    Row: (g) -> rows(g)\n    Col: (g) -> 0\n    Using: (c) -> c = \".\"\n",
			stdin: "..\n..",
			want:  "start (2, 0) is out of bounds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveSrc(t, tc.src); err != nil {
				t.Fatalf("must resolve; the bound is a runtime question: %v", err)
			}
			_, err := runErr(t, tc.src, tc.stdin)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Measured arguments through a Shikigami
// ---------------------------------------------------------------------------

// A Shikigami parameterizes over a measured argument through the lambda
// parameters it already has: the parameter is declared as the function it is,
// and handed to the measured slot. No new form is needed, and this spelling is
// the honest one — a scalar parameter substitutes into the body as a *literal*
// (into lambda bodies included), which is exactly what a function has no form
// for.
func TestMeasuredArgumentThroughAShikigami(t *testing.T) {
	src := `Shikigami "Sized Windows" (size: (List<Int>) -> Int)
    Cursed Technique: Window
        Size: size

` + intsPrelude + "Shikigami: Sized Windows\n    size: (xs) -> length(xs) / 2\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5,6")
	if got := ir.FormatValue(v); got != "[[1, 2, 3], [2, 3, 4], [3, 4, 5], [4, 5, 6]]" {
		t.Fatalf("measured argument through a Shikigami: got %s", got)
	}

	// A body may also just measure, with no parameter at all.
	noParam := `Shikigami "Halves"
    Cursed Technique: Window
        Size: (xs) -> length(xs) / 2

` + intsPrelude + "Shikigami: Halves\n"
	v, _ = runPipeline(t, noParam, "1,2,3,4,5,6")
	if got := ir.FormatValue(v); got != "[[1, 2, 3], [2, 3, 4], [3, 4, 5], [4, 5, 6]]" {
		t.Fatalf("measuring Shikigami body: got %s", got)
	}
}

// Reaching for it with a scalar declaration is the one confusion worth naming,
// so the error points at the spelling that works.
func TestScalarParameterGivenALambdaPointsAtTheLambdaType(t *testing.T) {
	src := `Shikigami "Halves" (k: Int)
    Cursed Technique: Window k

` + intsPrelude + "Shikigami: Halves\n    k: (xs) -> length(xs) / 2\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected a resolve error")
	}
	for _, want := range []string{
		`parameter "k" is declared Int but was given a lambda`,
		"declare the parameter as a lambda type",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}
