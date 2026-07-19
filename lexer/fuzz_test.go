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
