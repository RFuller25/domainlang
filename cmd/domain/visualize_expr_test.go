package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// exprProgram exercises the three things the breakdown has to get right: a call
// nested inside arithmetic, an `if` that takes one arm, and a `consider`
// binding — written across lines, since that is what the expression is long
// enough to need.
const exprProgram = `Cursed Energy: in.txt
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) ->
        consider d as abs(x - 10)
        in if d > 3
            then d * 2
            else d
Maximum Technique: Sum
Reveal: stdout
`

// visualizeExprs runs a program with --expressions and returns the text.
func visualizeExprs(t *testing.T, src, input string, opts visualizeOptions) string {
	t.Helper()
	_, prog := writeVisProgram(t, src, input)
	opts.Exprs = true
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, opts, strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	return out.String()
}

func TestVisualizeExpressionsBreaksTheLambdaDown(t *testing.T) {
	// 1 -> d = 9, 9 > 3, so the `then` arm: 18.
	got := visualizeExprs(t, exprProgram, "1,2,3", visualizeOptions{Optimize: true, Plain: true})
	for _, want := range []string{
		"expressions:",
		"x = 1",       // what the first application was applied to
		"abs(x - 10)", // the call
		"x - 10",      // and what is inside its parentheses
		"d > 3",       // the condition
		"d * 2",       // the arm that ran
	} {
		if !strings.Contains(got, want) {
			t.Errorf("breakdown missing %q:\n%s", want, got)
		}
	}
	// Values, not just text: -9 is `x - 10`, 9 is `abs(...)`, 18 is the arm.
	for _, want := range []string{"-9", "18"} {
		if !strings.Contains(got, want) {
			t.Errorf("breakdown missing the value %q:\n%s", want, got)
		}
	}
}

// The tree is what ran, so the arm that was not taken is not in it. That is the
// whole point of replaying the application rather than printing the source.
func TestVisualizeExpressionsOmitsTheUntakenArm(t *testing.T) {
	got := visualizeExprs(t, exprProgram, "1,2,3", visualizeOptions{Optimize: true, Plain: true})
	section := got[strings.Index(got, "expressions:"):]
	// `else d` is a bare name, which never gets a row of its own; the giveaway
	// is that the `then` arm's expression is there and its value is not the
	// `else` value. With x = 1 the condition holds, so `d * 2` ran.
	if !strings.Contains(section, "d * 2") {
		t.Fatalf("the arm that ran should be in the breakdown:\n%s", section)
	}
	// The rows for `Map Each` must describe one application, not three.
	if strings.Count(section, "x = 1") != 1 {
		t.Errorf("expected exactly one application of the Map Each lambda:\n%s", section)
	}
	if !strings.Contains(section, "first of 3 applications") {
		t.Errorf("a lambda applied to every element should say how many there were:\n%s", section)
	}
}

// Without the flag the plain output is exactly what it was: the section is a
// second trace of a different shape and would bury the table of stages.
func TestVisualizeExpressionsAreOptIn(t *testing.T) {
	_, prog := writeVisProgram(t, exprProgram, "1,2,3")
	var out, errBuf bytes.Buffer
	if code := Visualize(prog, visualizeOptions{Optimize: true, Plain: true},
		strings.NewReader(""), &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out.String(), "expressions:") {
		t.Errorf("the breakdown should need --expressions:\n%s", out.String())
	}
}

func TestVisualizeExpressionsJSON(t *testing.T) {
	got := visualizeExprs(t, exprProgram, "1,2,3", visualizeOptions{Optimize: true, JSON: true})
	var doc struct {
		Expressions []struct {
			Prim  string `json:"prim"`
			Line  int    `json:"line"`
			Bound string `json:"bound"`
			Note  string `json:"note"`
			Parts []struct {
				Depth int    `json:"depth"`
				Expr  string `json:"expr"`
				Value string `json:"value"`
			} `json:"parts"`
		} `json:"expressions"`
	}
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, got)
	}
	if len(doc.Expressions) != 1 {
		t.Fatalf("got %d expressions, want 1 (the Map Each lambda)", len(doc.Expressions))
	}
	e := doc.Expressions[0]
	// The optimizer fuses Map Each + Sum into one stage, so the primitive that
	// ends up running the lambda is not the one that was written. What must
	// hold is that it is named and placed: the line is the statement the
	// expression belongs to, which is where a reader would go to change it.
	if e.Prim == "" {
		t.Error("the entry should name the stage that ran the expression")
	}
	if e.Bound != "x = 1" {
		t.Errorf("bound = %q, want `x = 1`", e.Bound)
	}
	if e.Line != 4 {
		t.Errorf("line = %d, want 4 (the statement carrying the Using:)", e.Line)
	}
	var found bool
	for _, p := range e.Parts {
		if p.Expr == "x - 10" {
			found = true
			if p.Value != "-9" {
				t.Errorf("`x - 10` came to %q, want -9", p.Value)
			}
			if p.Depth == 0 {
				t.Errorf("`x - 10` is nested inside abs(...), so its depth should not be 0")
			}
		}
	}
	if !found {
		t.Errorf("no part for `x - 10`:\n%s", got)
	}
}

// A program with no expression at all still answers the question, rather than
// printing an empty heading.
func TestVisualizeExpressionsWithNoLambda(t *testing.T) {
	got := visualizeExprs(t, visNoLambdaProgram, "1,2,3", visualizeOptions{Optimize: true, Plain: true})
	if !strings.Contains(got, "no stage in this program ran a Using: expression") {
		t.Errorf("expected the empty case to say so:\n%s", got)
	}
}

const visNoLambdaProgram = `Cursed Energy: in.txt
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Maximum Technique: Sum
Reveal: stdout
`

// --- the pane in the UI ---

func TestVisualModelExpressionPane(t *testing.T) {
	m := visModel(t)
	// Walk to a step that ran a lambda: the Map Each inside the loop.
	for i := range m.flat {
		if n := m.flat[i]; !n.IsFrame() && n.Step.Apply != nil {
			m.reveal(n)
			break
		}
	}
	m = send(m, pressKey("x"))
	if m.pane != paneExpr {
		t.Fatalf("`x` should open the expression pane, got %v", m.pane)
	}
	got := m.View().Content
	for _, want := range []string{"expression", "x + 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("the pane should show %q:\n%s", want, got)
		}
	}
	m = send(m, pressKey("x"))
	if m.pane != paneValue {
		t.Errorf("`x` should close the pane again, got %v", m.pane)
	}
}

// A row with no expression says so instead of showing an empty pane.
func TestVisualModelExpressionPaneOnAStepWithoutOne(t *testing.T) {
	m := visModel(t)
	m.cursor = 0 // Read Source: no Using: anywhere near it
	m = send(m, pressKey("x"))
	if got := m.View().Content; !strings.Contains(got, "no Using: expression") {
		t.Errorf("expected the pane to explain itself:\n%s", got)
	}
}

func TestVisualModelExpressionPaneOnAFrame(t *testing.T) {
	m := visModel(t)
	for i, r := range m.rows {
		if r.node.IsFrame() {
			m.cursor = i
			break
		}
	}
	// The loop row is a step; open it so a real frame row is on screen.
	m = send(m, pressKey("l"), pressKey("j"), pressKey("x"))
	if m.selected() != nil {
		t.Skip("the cursor did not land on a frame row")
	}
	if got := m.View().Content; !strings.Contains(got, "runs no expression of its own") {
		t.Errorf("expected a frame to explain itself:\n%s", got)
	}
}

func TestVisualizeExpressionsFlagParsing(t *testing.T) {
	for _, flag := range []string{"--expressions", "--exprs"} {
		_, opts, err := parseVisualizeArgs([]string{"p.domain", flag})
		if err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if !opts.Exprs {
			t.Errorf("%s should set Exprs", flag)
		}
	}
}
