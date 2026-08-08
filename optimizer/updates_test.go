package optimizer

import (
	"strings"
	"testing"
)

// The optimizer's stand-down on a lambda that writes to a binding.
//
// Every pass here is written against a pure expression layer and is aggressive
// in exactly the ways a write would notice: fusion changes the order effects
// happen in, algorithm substitution changes how many times a lambda runs, and
// the expression rules drop and duplicate subexpressions. So a stage that
// updates a binding keeps the pipeline it was written as — and these tests are
// the ones that say the rewrites really do stop, and that the answer is still
// right when they do.

// rewriteMessages runs the front end plus the optimizer and returns the
// rewrites' messages joined, for asserting a pass did or did not fire.
func rewriteMessages(t *testing.T, src string) string {
	t.Helper()
	_, rewrites := resolveProgram(t, src, true)
	var msgs []string
	for _, r := range rewrites {
		msgs = append(msgs, r.Message)
	}
	return strings.Join(msgs, "\n")
}

// TestUpdatingLambdaStandsDownFusion: two adjacent Map Each stages normally
// fuse into one composed lambda, which would interleave the two stages' writes
// per element instead of running one stage and then the other.
func TestUpdatingLambdaStandsDownFusion(t *testing.T) {
	fusable := listHeader + `Cursed Technique: Map Each
    Using: (x) -> x + 1
Cursed Technique: Map Each
    Using: (x) -> x * 2
Reveal: stdout
`
	if msgs := rewriteMessages(t, fusable); !strings.Contains(msgs, "Map Each") {
		t.Fatalf("the control program should have fused; got %q", msgs)
	}

	updating := listHeader + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> x + (n := n + 1)
Cursed Technique: Map Each
    Using: (x) -> x * 2
Reveal: stdout
`
	if msgs := rewriteMessages(t, updating); msgs != "" {
		t.Fatalf("expected no rewrites over an updating lambda, got %q", msgs)
	}
}

// TestUpdatingLambdaStandsDownExpressionRules: the expression simplifier drops
// and duplicates subexpressions, and `x * 0` is exactly the rule that would
// drop a write.
func TestUpdatingLambdaStandsDownExpressionRules(t *testing.T) {
	simplifiable := listHeader + `Cursed Technique: Map Each
    Using: (x) -> x * 1 + 0
Reveal: stdout
`
	if msgs := rewriteMessages(t, simplifiable); !strings.Contains(msgs, "simplified") {
		t.Fatalf("the control program should have simplified; got %q", msgs)
	}

	updating := listHeader + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> (n := x) * 0 + 0
Reveal: stdout
`
	if msgs := rewriteMessages(t, updating); msgs != "" {
		t.Fatalf("expected no rewrites over an updating lambda, got %q", msgs)
	}
}

// TestUpdatingLambdaStandsDownAlgorithmSubstitution: the All Pairs sum-target
// scan is replaced by a hash-set pass that never applies the lambda to most of
// the pairs, so a lambda that counted them would count something else.
func TestUpdatingLambdaStandsDownAlgorithmSubstitution(t *testing.T) {
	substitutable := listHeader + `Domain Expansion: All Pairs
    Mode: First
    Using: (a, b) -> a + b = 2020
Maximum Technique: Product
Reveal: stdout
`
	if msgs := rewriteMessages(t, substitutable); !strings.Contains(msgs, "Cursed") {
		t.Fatalf("the control program should have substituted; got %q", msgs)
	}

	updating := listHeader + `Domain Expansion: All Pairs
    Mode: First
    Consider seen As 0
    Using: (a, b) -> a + b = (2020 also seen := seen + 1)
Maximum Technique: Product
Reveal: stdout
`
	if msgs := rewriteMessages(t, updating); msgs != "" {
		t.Fatalf("expected no rewrites over an updating lambda, got %q", msgs)
	}
}

// TestUpdatingPipelineAgreesWithItsNaiveSelf is the property that matters
// underneath the stand-downs: whatever the optimizer decides, the answer is
// the one the unoptimized pipeline gives.
func TestUpdatingPipelineAgreesWithItsNaiveSelf(t *testing.T) {
	srcs := []string{
		listHeader + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> x + (n := n + 1)
Maximum Technique: Sum
Reveal: stdout
`,
		listHeader + `Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> (consider t as x * 2 in (t := t + n)) also n := n + 1
Reveal: stdout
`,
		listHeader + `Cursed Technique: Filter
    Consider kept As 0
    Using: (x) -> x > 2 and (kept := kept + 1) > 0
Maximum Technique: Count
Reveal: stdout
`,
	}
	input := intsInput([]int64{1, 2, 3, 4, 5})
	for _, src := range srcs {
		naive, _ := resolveProgram(t, src, false)
		opt, _ := resolveProgram(t, src, true)
		want, err := interpret(naive, input)
		if err != nil {
			t.Fatalf("naive: %v\n%s", err, src)
		}
		got, err := interpret(opt, input)
		if err != nil {
			t.Fatalf("optimized: %v\n%s", err, src)
		}
		if got != want {
			t.Fatalf("optimized %q, naive %q\n%s", got, want, src)
		}
	}
}
