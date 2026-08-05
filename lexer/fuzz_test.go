package lexer

import "testing"

// FuzzLex asserts the lexer never panics on arbitrary input — it must either
// return a token stream or a well-formed *Error, no matter what bytes it is
// given.
func FuzzLex(f *testing.F) {
	seeds := []string{
		"",
		"Reveal: stdout\n",
		"Cursed Energy: input.txt\nDomain Expansion: Quicksort, Descending\n",
		"A:\n\tB: x\n",
		`"unterminated`,
		`"bad \q escape"` + "\n",
		"café \xc3\n",
		"\x00\x01\x02",
		"# comment\n\nReveal: stdout\n",
		"(a, b) -> a + b = 2020\n",
		// Foreign blocks: the region the lexer copies instead of reading, so
		// the seeds are the shapes that decide where it starts and stops.
		"Domain Expansion: Python\n    print({1: [2]})\n",
		"Domain Expansion: Go\n\tpackage main\n",
		"Python\n    \xff\x00 # \"\n",
		"Domain Expansion: Python\n",
		"Domain Expansion: Python\n\n\n",
		"Part \"1\":\n    Python\n        x\n    Reveal: stdout\n",
		"Using: Python\n    x\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Lex panicked on %q: %v", src, r)
			}
		}()
		_, _ = Lex(src)
	})
}
