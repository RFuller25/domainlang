package prims

import (
	"strings"
	"testing"
)

// TestKeywordWithBlockButNoOperation is a crash the fuzzer found behind the
// language server's boundary guard. `Cursed Energy:` with an indented line
// under it — a source stage someone started and has not finished naming — has
// no operation phrase, but it does have a block, and the check for a missing
// phrase used to accept that as good enough. Read Source matches any phrase at
// all, including the one that is not there, and then read it.
func TestKeywordWithBlockButNoOperation(t *testing.T) {
	for _, src := range []string{
		"Cursed Energy:\n    Sum\n",
		"Cursed Energy:\n Max\n",
		"Cursed Energy:\n    Using: (x) -> x\n",
		"Maximum Technique:\n    Sum\n",
		"Reveal:\n    stdout\n",
	} {
		_, err := resolveSrc(t, src)
		if err == nil {
			t.Errorf("%q: expected an error", src)
			continue
		}
		if !strings.Contains(err.Error(), "has no operation") {
			t.Errorf("%q: error = %v, want it to name the missing operation", src, err)
		}
	}
}

// TestStatementsWithBlocksStillResolve guards the other direction: a statement
// whose operation is present and whose body carries the rest of it is the
// normal way half the language is written.
func TestStatementsWithBlocksStillResolve(t *testing.T) {
	src := `Cursed Energy: in.txt
Shikigami: Lines
Channeled Energy: Convert List to Integers
Maximum Technique: Count Matching
    Using: (x) -> x > 2
Reveal: stdout
`
	if _, err := resolveSrc(t, src); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}
