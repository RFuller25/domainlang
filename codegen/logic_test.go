package codegen_test

import (
	"testing"

	"domain/codegen"
)

// The bitwise reducers and the logical connectives in function form, through
// both backends. The connectives are the interesting ones: infix `and`/`or`
// compile to Go's `&&`/`||` and short-circuit, while the *function* forms must
// not — the interpreter evaluates every argument before dispatching, so a
// compiled `&&` would skip work the interpreter performs.
func TestLogicAndBitwiseReducersOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
`
	cases := []struct{ name, src, input string }{
		{"bitwise reducers", header + `Cursed Technique: Apply
    Using: (xs) -> list(bxorall(xs), borall(xs), bandall(xs))
Reveal: stdout
`, "12\n10\n6\n"},

		// Each operator's identity is what the empty list gives, so a later
		// fold is unchanged by it — and for `and` that is -1, not 0.
		{"bitwise reducers over an empty list", header + `Cursed Technique: Apply
    Using: (xs) -> list(bxorall(emptylist(0)), borall(emptylist(0)), bandall(emptylist(0)))
Reveal: stdout
`, "1\n"},

		{"a single element", header + `Cursed Technique: Apply
    Using: (xs) -> list(bxorall(take(xs, 1)), bandall(take(xs, 1)))
Reveal: stdout
`, "7\n9\n"},

		// The AoC shape: xor-reduce a whole column.
		{"xor-reduce a parsed column", header + `Maximum Technique: Fold
    Seed: (xs) -> 0
    Using: (acc, x) -> bxor(acc, x)
Reveal: stdout
`, "12\n10\n6\n"},

		{"logical connectives", header + `Cursed Technique: Apply
    Using: (xs) -> list(and(first(xs) > 0, last(xs) > 0), or(first(xs) > 99, last(xs) > 0), xor(first(xs) > 0, last(xs) > 0), not(first(xs) > 99))
Reveal: stdout
`, "12\n10\n"},

		// Both spellings in one expression, so the parser's positional rule —
		// `and` is an operator in infix position and a call in operand
		// position — is exercised by a compiled program too.
		{"infix and call spellings together", header + `Cursed Technique: Apply
    Using: (xs) -> first(xs) > 0 and and(last(xs) > 0, not(first(xs) < 0))
Reveal: stdout
`, "12\n10\n"},
	}

	for _, c := range cases {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				input := []byte(c.input)
				pipe, want := oracleFront(t, c.src, optimize, input)
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q\n\n%s", want, got, c.src)
				}
			})
		}
	}
}

// The function forms evaluate both arguments; the infix operators do not. Both
// backends have to agree about *that*, not just about the result — so the
// oracle here is a program whose right operand fails, where short-circuiting
// is the difference between an answer and a refusal.
func TestShortCircuitDifferenceAgreesAcrossBackends(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
`
	for _, c := range []struct {
		name, expr string
		wantFail   bool
	}{
		{"infix and skips a failing right operand", "first(xs) > 99 and item(xs, 5) > 0", false},
		{"and(...) evaluates it", "and(first(xs) > 99, item(xs, 5) > 0)", true},
		{"infix or skips a failing right operand", "first(xs) > 0 or item(xs, 5) > 0", false},
		{"or(...) evaluates it", "or(first(xs) > 0, item(xs, 5) > 0)", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := header + "Cursed Technique: Apply\n    Using: (xs) -> " + c.expr + "\nReveal: stdout\n"
			input := []byte("1\n2\n")
			var pipe = compilePipeline(t, src, true)
			wantOut, wantFail, wantMsg := interpreterOutcome(t, pipe, input)
			gotOut, gotFail, gotMsg := binaryOutcome(t, pipe, input)

			if wantFail != c.wantFail {
				t.Fatalf("interpreter failed=%v, expected %v (%s)", wantFail, c.wantFail, wantMsg)
			}
			if gotFail != wantFail {
				t.Fatalf("the backends disagree about whether this fails\n"+
					"interpreter: failed=%v %s\nbinary:      failed=%v %s",
					wantFail, wantMsg, gotFail, gotMsg)
			}
			if !wantFail && gotOut != wantOut {
				t.Errorf("stdout mismatch\ninterpreter: %q\nbinary: %q", wantOut, gotOut)
			}
		})
	}
}
