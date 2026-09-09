package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
)

// The stream has two independent implementations — ir/rand.go and the
// splitmix64 that codegen emits — and they must agree bit for bit. That is
// what lets an ordinary program draw a random number and still be compared
// between the backends, which is the oracle every test here rests on.
//
// It is also why splitmix64 was chosen over anything with tables: every line
// of it is a line the two sides could disagree on, and there are few enough
// lines to check by eye.
func TestRandomMatchesInterpreter(t *testing.T) {
	cases := []struct{ name, expr, input string }{
		{"one draw", `random(100)`, "1"},
		{"a draw per element", `sum(list(random(1000), random(1000), random(1000)))`, "1"},
		{"floats", `randomf() + randomf()`, "1"},
		{"a small range exercises the rejection path", `random(3) * 100 + random(3) * 10 + random(3)`, "1"},
		{"the clock reads zero outside a game", `frame() + elapsed()`, "1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "Cursed Energy: stdin\n" +
				"Cursed Technique: Split Text by \",\"\n" +
				"Channeled Energy: Convert To Integers\n" +
				"Cursed Technique: Apply\n" +
				"    Using: (xs) -> " + c.expr + "\n" +
				"Reveal: stdout\n"
			for _, opt := range []bool{true, false} {
				pipe := compilePipeline(t, src, opt)
				want := runInterpreter(t, pipe, []byte(c.input))
				got := buildAndRun(t, pipe, []byte(c.input), codegen.Options{})
				if got != want {
					t.Errorf("optimize=%v: binary %q, interpreter %q", opt, got, want)
				}
			}
		})
	}
}

// pick draws one element, and the argument is evaluated once — `pick(f())`
// must not compute its list twice.
func TestPickMatchesInterpreter(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (xs) -> pick(xs) + pick(xs) + pick(xs)\n" +
		"Reveal: stdout\n"
	for _, opt := range []bool{true, false} {
		pipe := compilePipeline(t, src, opt)
		want := runInterpreter(t, pipe, []byte("a,b,c,d,e"))
		got := buildAndRun(t, pipe, []byte("a,b,c,d,e"), codegen.Options{})
		if got != want {
			t.Errorf("optimize=%v: binary %q, interpreter %q", opt, got, want)
		}
	}
}

// A `Using:` lambda may ignore its parameter — `(n) -> random(6)` rolls a die
// per element without caring which. That compiled to a loop binding an element
// nothing read, and Go refuses a declared-and-unused variable: a correct
// program that ran and would not build. It was there before randomness gave
// anyone an easy way to write one.
func TestLambdaMayIgnoreItsParameter(t *testing.T) {
	for _, expr := range []string{`random(6)`, `1 + 1`} {
		src := "Cursed Energy: stdin\n" +
			"Cursed Technique: Split Text by \",\"\n" +
			"Channeled Energy: Convert To Integers\n" +
			"Cursed Technique: Map Each\n" +
			"    Using: (n) -> " + expr + "\n" +
			"Maximum Technique: Sum\n" +
			"Reveal: stdout\n"
		pipe := compilePipeline(t, src, false)
		goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
		if err != nil {
			t.Fatalf("%s: EmitProgram: %v", expr, err)
		}
		if strings.Contains(goSrc, "declared and not used") {
			t.Fatalf("%s: emitted source is broken", expr)
		}
		want := runInterpreter(t, pipe, []byte("1,2,3"))
		if got := buildAndRun(t, pipe, []byte("1,2,3"), codegen.Options{}); got != want {
			t.Errorf("%s: binary %q, interpreter %q", expr, got, want)
		}
	}
}
