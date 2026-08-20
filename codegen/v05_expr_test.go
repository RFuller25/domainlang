package codegen_test

import (
	"testing"

	"domain/codegen"
)

// TestCompiledV05ExpressionsMatchInterpreter pins interpreter/binary byte
// parity for the v0.5 expression-layer additions: Euclidean mod and the `%`
// operator, the integer-math group, `ikke`, heterogeneous tuples, and the
// text builtins — in both optimizer modes.
//
// The repo's rule is that a builtin implemented in eval but not codegen must
// fail compilation rather than produce differing output; these programs are
// what proves the codegen half actually landed.
func TestCompiledV05ExpressionsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			// Negative operands are the whole point of Euclidean mod: the
			// truncated form would give a negative index here.
			name: "euclidean mod over negatives",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> totext(mod(n, 7) * 1000 + (n % 7) * 10 + mod(0 - n, 7))
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "-9\n-1\n0\n1\n15",
		},
		{
			name: "divmod round trip",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> prow(divmod(n, 5)) * 5 + pcol(divmod(n, 5)) - n
Maximum Technique: Sum
Reveal: stdout
`,
			input: "-17\n-3\n0\n4\n23",
		},
		{
			name: "integer math group",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> totext(pow(2, mod(n, 8)) + isqrt(abs(n) * 7) + clamp(n, 0, 9) + factorial(mod(n, 6)) + choose(10, mod(n, 5)) + min(n, 4) + max(n, 4))
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "-9\n0\n3\n17\n40",
		},
		{
			// The same scalar group again, but with *every* argument a literal.
			// The case above always passes a variable, which is why it never
			// caught this: a variable is int64-typed and fixes the helper's type
			// parameter, while an all-constant argument list leaves Go inferring
			// from untyped constants — and abs, clamp and the two-argument
			// min/max then failed to compile at all, on programs the interpreter
			// ran happily. Integer literals are pinned to int64 in codegen now;
			// this is the shape that regresses if that ever comes undone.
			name: "integer math group on literals only",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Apply
    Using: (xs) -> textjoin(list(
        totext(abs(0 - 5)),
        totext(clamp(12, 0, 10)),
        totext(min(3, 9)),
        totext(max(3, 9)),
        totext(pow(2, 10)),
        totext(gcd(12, 18)),
        totext(isqrt(49))
    ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			name: "ikke in filters and conditionals",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Using: (x) -> ikke x = 3 and ikke (x > 20 or x < 0)
Cursed Technique: Map Each
    Using: (x) -> totext(if ikke (x > 5) then x * 100 else x)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "-4\n0\n3\n5\n9\n21",
		},
		{
			// Tuples carry mixed types and compare structurally, which is
			// what makes them usable as Group By keys.
			name: "heterogeneous tuples",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (s) -> tuple(s, length(s))
Cursed Technique: Map Each
    Using: (t) -> item(t, 0) + ":" + totext(item(t, 1))
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "a\nbb\nccc",
		},
		{
			name: "tuple keys group and compare",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Group By
    Using: (s) -> tuple(charat(s, 0), length(s))
Reveal: stdout
`,
			input: "ax\nay\nbz\na\nbq",
		},
		{
			name: "text builtins",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (s) -> textjoin(list(totext(length(s)), slice(s, 1, 4), charat(s, 0), upper(s), lower(s), totext(indexof(s, "o")), replace(s, "l", "L"), trim(s)), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			input: "hello\nWORLD\nab",
		},
		{
			// Rune indexing, not bytes: a byte-indexed charat would split
			// this input mid-character and the two backends would diverge.
			name: "text positions count runes",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (s) -> textjoin(list(totext(length(s)), charat(s, 1), slice(s, 1, 3), totext(indexof(s, "llo"))), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			input: "héllo\nnaïve",
		},
		{
			name: "text predicates and chars",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Filter
    Using: (s) -> startswith(s, "a") or endswith(s, "z")
Cursed Technique: Map Each
    Using: (s) -> textjoin(chars(s), "-")
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "abc\nxyz\nqqq\naz",
		},
		{
			// slice and indexof clamp / answer -1 rather than erroring, and
			// both backends must agree on every boundary.
			name: "slice clamping and indexof sentinel",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (s) -> textjoin(list(slice(s, 0, 99), slice(s, 3, 1), slice(s, 50, 60), totext(indexof(s, "zz"))), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			input: "abcd\nx",
		},
		{
			name: "consider binds once and nests",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> consider d as n * n in consider e as d + n in (if d > 50 then e else 0 - e)
Maximum Technique: Sum
Reveal: stdout
`,
			input: "-9\n0\n3\n8\n17",
		},
		{
			// The shadowing case: the inner binding must not leak past its
			// body, and the lambda parameter must come back afterwards.
			name: "consider shadowing",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> totext((consider x as 100 in x) + x + (consider a as 1 in (consider a as 9 in a) + a))
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			// A `consider` inside a Shikigami body: the binding shadows a
			// parameter of the same name for its body only, so `k` here is
			// the local, and the second `k` is the substituted argument.
			name: "consider inside a shikigami",
			src: `Shikigami "Scaled" (k: Int) : List<Int> -> List<Int>
    Cursed Technique: Map Each
        Using: (x) -> (consider k as 2 in x * k) + k

Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Shikigami: Scaled
    k: 10
Cursed Technique: Map Each
    Using: (x) -> totext(x)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			// Sort over Text: alphabetical by default, which v0.4 could not
			// express at all (Sort was List<Int> only).
			name: "sort text alphabetically",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Domain Expansion: Sort
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "pear\napple\nfig\nBanana\nfig",
		},
		{
			name: "sort text descending",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Domain Expansion: Sort, Descending
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "pear\napple\nfig\nBanana",
		},
		{
			// A tuple key is how a tiebreak is written: length first, then
			// alphabetical within a length. Ties must keep input order in
			// both backends, which is why the compiled sort is stable.
			name: "sort by tuple key tiebreak",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Domain Expansion: Sort By
    Using: (s) -> tuple(length(s), s)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "pear\nfig\napple\nkiwi\nfig\nbanana",
		},
		{
			name: "sort by text key descending",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Domain Expansion: Sort By, Descending
    Using: (s) -> charat(s, 0)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "pear\nfig\napple\nkiwi\nfig",
		},
		{
			name: "indexof and slice over lists",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> sum(slice(xs, 1, 3)) * 100 + indexof(xs, 9) * 10 + indexof(xs, 12345)
Reveal: stdout
`,
			input: "4\n9\n2\n7",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			p := p
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}
