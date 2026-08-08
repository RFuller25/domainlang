package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// The two remaining axes of Match Pattern: where in the line a template may
// match, and how many templates a stage may carry.

// AoC 2024 D3. Every mode before Scan anchored the template to the whole line,
// so input the template does not describe *exhaustively* had no spelling —
// Extract Integers could reach the numbers only by discarding the structure
// that says which numbers belong together.
func TestScanTakesEveryOccurrenceInALine(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n    Mode: Scan\n" +
		"    Using: \"mul({a:int},{b:int})\"\n" +
		"Cursed Technique: Map Each\n    Using: (m) -> m.a * m.b\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "xmul(2,4)%&mul[3,7]!@^do_not_mul(5,5)+mul(32,64]then(mul(11,8)mul(8,5))")
	if v.(int64) != 161 {
		t.Fatalf("got %v, want 161", v)
	}
}

// A line contributes as many values as it holds — including none, which is an
// answer rather than a failure. That is the whole difference from Each: Scan is
// asked for precisely when the line is not expected to be all template.
func TestScanFlattensAcrossLinesAndToleratesNone(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n    Mode: Scan\n" +
		"    Using: \"<{n:int}>\"\n"
	v, _ := runPipeline(t, src, "a<1>b<2>\nnothing here\n<3>")
	if got := ir.FormatValue(v); got != "[{n: 1}, {n: 2}, {n: 3}]" {
		t.Fatalf("got %s", got)
	}
	// Nothing anywhere is an empty list, not an error.
	v, _ = runPipeline(t, src, "nothing\nat all")
	if got := ir.FormatValue(v); got != "[]" {
		t.Fatalf("no occurrences: got %s, want []", got)
	}
}

// Scan is never inferred, for the reason Try is not: it drops input the
// template did not describe, and a typo would then parse nothing in silence.
func TestScanIsNeverInferred(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n    Using: \"<{n:int}>\"\nReveal: stdout\n"
	_, err := runErr(t, src, "a<1>b")
	if err == nil || !strings.Contains(err.Error(), "does not match template") {
		t.Fatalf("an omitted Mode: should infer Each, not Scan; got %v", err)
	}
}

// AoC 2015 D6. Case: is what alternation looks like without sum types: the
// branches live at the stage, where "every case produces the same fields" is
// checkable, and what varies is recorded in a `kind` field.
func TestCasesTagTheLineThatMatched(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n    Mode: Each\n" +
		"    Case: on     \"turn on {a:int},{b:int} through {c:int},{d:int}\"\n" +
		"    Case: off    \"turn off {a:int},{b:int} through {c:int},{d:int}\"\n" +
		"    Case: toggle \"toggle {a:int},{b:int} through {c:int},{d:int}\"\n"
	v, _ := runPipeline(t, src, "turn on 0,0 through 9,9\ntoggle 1,1 through 2,2\nturn off 3,3 through 4,4")
	want := "[{kind: on, a: 0, b: 0, c: 9, d: 9}, " +
		"{kind: toggle, a: 1, b: 1, c: 2, d: 2}, " +
		"{kind: off, a: 3, b: 3, c: 4, d: 4}]"
	if got := ir.FormatValue(v); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// The point of Case: over three Try passes is that it keeps the file's own
// order. A simulation is the order it is run in.
func TestCasesKeepInputOrder(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n    Mode: Each\n" +
		"    Case: a \"a {n:int}\"\n    Case: b \"b {n:int}\"\n" +
		"Cursed Technique: Map Each\n    Using: (r) -> r.kind\n" +
		"Maximum Technique: Join with \"\"\n"
	v, _ := runPipeline(t, src, "a 1\nb 2\na 3\nb 4")
	if got, ok := v.(string); !ok || got != "abab" {
		t.Fatalf("got %v, want abab", v)
	}
}

// Cases are tried in the order written, so a program controls priority when
// two templates could both match — `turn on` before a catch-all `turn {w:word}`.
func TestCasesAreTriedInOrder(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n    Mode: Each\n" +
		"    Case: specific \"turn on {n:int}\"\n" +
		"    Case: general  \"turn {n:int}\"\n" +
		"Cursed Technique: Map Each\n    Using: (r) -> r.kind\n"
	v, _ := runPipeline(t, src, "turn on 5\nturn 7")
	if got := ir.FormatValue(v); got != "[specific, general]" {
		t.Fatalf("got %s", got)
	}
}

// Case: composes with the modes: Try drops a line no case matched, Each
// refuses it.
func TestCasesUnderEachAndTry(t *testing.T) {
	body := "Cursed Technique: Match Pattern\n    Mode: %s\n" +
		"    Case: a \"a {n:int}\"\n    Case: b \"b {n:int}\"\n"
	const input = "a 1\nnope\nb 2"

	_, err := runErr(t, linesHead+strings.Replace(body, "%s", "Each", 1)+"Reveal: stdout\n", input)
	if err == nil || !strings.Contains(err.Error(), "matches none of the 2 Case: templates") {
		t.Fatalf("Each: expected a refusal naming the cases, got %v", err)
	}
	v, _ := runPipeline(t, linesHead+strings.Replace(body, "%s", "Try", 1)+"Maximum Technique: Count\n", input)
	if v.(int64) != 2 {
		t.Errorf("Try: got %v, want 2", v)
	}
}

func TestCaseRefusals(t *testing.T) {
	for _, tc := range []struct{ name, stage, want string }{
		{"cases that disagree about their fields",
			"    Case: a \"a {n:int}\"\n    Case: b \"b {m:int}\"\n",
			"every case has to produce the same fields"},
		{"a positional case",
			"    Case: a \"a {int}\"\n    Case: b \"b {int}\"\n",
			"need named holes"},
		{"a hole called kind",
			"    Case: a \"a {kind:int}\"\n",
			`has a hole named "kind"`},
		{"the same tag twice",
			"    Case: a \"a {n:int}\"\n    Case: a \"b {n:int}\"\n",
			`names the case "a" twice`},
		{"a template beside the cases",
			"    Using: \"x {n:int}\"\n    Case: a \"a {n:int}\"\n",
			"not both"},
		{"Scan with cases",
			"    Mode: Scan\n    Case: a \"a {n:int}\"\n",
			"takes a single template"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := linesHead + "Cursed Technique: Match Pattern\n" + tc.stage + "Reveal: stdout\n"
			_, err := runErr(t, src, "a 1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// The new scalar hole types, and the `{~}` gap for input whose columns are
// aligned with a variable number of spaces.
func TestExtraHoleTypes(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n    Mode: Each\n" +
		"    Using: \"{k:char} #{c:hex}{~}{d:digits}\"\n"
	v, _ := runPipeline(t, src, "R #70c710   007\nL #0dc571 42")
	// hex arrives as an Int, digits as Text with its leading zeros intact, and
	// the gap matches one space or many.
	want := "[{k: R, c: 7390992, d: 007}, {k: L, c: 902513, d: 42}]"
	if got := ir.FormatValue(v); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestExtraHoleTypeRefusals(t *testing.T) {
	for _, tc := range []struct{ tmpl, want string }{
		{`{x:blah}`, "want int, hex, digits, word, char, or text"},
		// A separator the class itself matches leaves the run no boundary, and
		// unlike word/char the class cannot be narrowed to exclude it.
		{`{hs:hex+ sep=\"a\"}`, "which is itself one"},
		{`{ds:digits+ sep=\"1\"}`, "which is itself one"},
	} {
		src := linesHead + "Cursed Technique: Match Pattern\n    Mode: Each\n" +
			"    Using: \"" + tc.tmpl + "\"\nReveal: stdout\n"
		_, err := runErr(t, src, "x")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected an error containing %q, got %v", tc.tmpl, tc.want, err)
		}
	}
}
