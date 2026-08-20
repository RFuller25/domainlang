package codegen_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
)

// TestCompiledV06ExpressionsMatchInterpreter pins interpreter/binary byte
// parity for the v0.6 expression-layer additions: collection construction and
// update, list generation, the text splitters and code points, the float tower
// past sqrt, named-field records, and the base/bit/number-theory group.
//
// The repo's rule is that a builtin implemented in eval but not codegen must
// fail compilation rather than produce differing output; these programs are
// what proves the codegen half actually landed. They matter more here than for
// earlier groups, because several of these are *algorithms* written twice —
// Miller-Rabin, the divisor walk, CRT — rather than wrappers over one stdlib
// call, so "both backends do the same thing" is a real claim to check.
func TestCompiledV06ExpressionsMatchInterpreter(t *testing.T) {
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
			// A Map accumulated in a Fold: the shape that was impossible before
			// insert/emptymap existed, since a measured Seed: is an expression too.
			name: "map accumulated in a fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Fold
    Seed: (xs) -> emptymap("", 0)
    Using: (acc, w) -> insert(acc, w, getor(acc, w, 0) + 1)
Cursed Technique: Apply
    Using: (m) -> textjoin(list(totext(size(m)), totext(getor(m, "a", 0)), totext(getor(m, "zz", 0))), "|")
Reveal: stdout
`,
			input: "a\nb\na\nc\na\nb",
		},
		{
			// entries/tomap round-trip, and the insertion order both backends
			// must agree on.
			name: "map entries round trip",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Apply
    Using: (ws) ->
        consider m as tomap(list(tuple("x", 1), tuple("y", 2), tuple("x", 3)))
        in textjoin(list(
            totext(size(m)),
            totext(get(m, "x")),
            totext(length(entries(m))),
            item(first(entries(m)), 0),
            totext(item(last(entries(m)), 1)),
            totext(size(del(m, "x"))),
            totext(size(del(m, "nope")))
        ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			name: "set algebra",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) ->
        consider a as toset(xs)
        in consider b as toset(list(2, 4, 6, 99))
        in textjoin(list(
            totext(size(a)),
            totext(size(union(a, b))),
            totext(size(intersect(a, b))),
            totext(size(difference(a, b))),
            totext(size(insert(a, 1000))),
            totext(size(del(a, 2))),
            totext(size(emptyset(0))),
            totext(length(emptylist(0))),
            totext(length(concat(emptylist(0), tolist(a)))),
            textjoin(list(totext(first(tolist(intersect(a, b)))), totext(last(tolist(union(a, b))))), ",")
        ), "|")
Reveal: stdout
`,
			input: "1\n2\n3\n4\n2\n5",
		},
		{
			// Functional update: the original must be untouched, or the two
			// backends diverge the moment a lambda is applied twice.
			name: "grid setat is functional",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Apply
    Using: (g) ->
        consider h as setat(g, 0, 0, "Z")
        in textjoin(list(at(g, 0, 0), at(h, 0, 0), at(h, 1, 1), totext(rows(h) * cols(h))), "|")
Reveal: stdout
`,
			input: "ab\ncd",
		},
		{
			name: "sparse cellpoints walks in row-major order",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider g as put(put(put(sparse(0), 2, 1, 7), 0, 3, 5), 1, 0, 9)
        in textjoin(list(
            totext(cells(g)),
            totext(length(cellpoints(g))),
            totext(prow(first(cellpoints(g)))),
            totext(pcol(first(cellpoints(g)))),
            totext(prow(last(cellpoints(g)))),
            totext(sum(list(at(g, 2, 1), at(g, 0, 3), at(g, 1, 0))))
        ), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			// fill's result stored in a tuple field, which is the shape that
			// caught a backend divergence: dmFill is generic, and letting Go
			// infer T from an Int literal — an untyped constant — gave `int`
			// and produced a []int where the rest of the program had agreed on
			// []int64. Ranging over the list compiles either way, so a test
			// that only sums it passes against the broken codegen; storing it
			// in a tuple field is what fails to compile. The interpreter has
			// no such split, hence a divergence rather than a type error.
			name: "fill in a tuple field",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) ->
        consider st as tuple(fill(4, 7), first(xs))
        in textjoin(list(
            totext(sum(item(st, 0))),
            totext(length(item(st, 0))),
            totext(item(st, 1))
        ), "|")
Reveal: stdout
`,
			input: "5",
		},
		{
			name: "range and fill",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> textjoin(list(
        totext(sum(range(1, 11))),
        totext(length(range(5, 5))),
        totext(length(range(3, 1))),
        totext(first(range(0 - 3, 3))),
        textjoin(fill(4, "ab"), "-"),
        totext(length(fill(0 - 2, "x")))
    ), "|")
Reveal: stdout
`,
			input: "1",
		},
		{
			name: "text splitting and code points",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (s) -> textjoin(list(
        textjoin(split(s, ","), "/"),
        textjoin(words(s), "+"),
        if contains(s, "a") then "A" else "-",
        totext(ord(s)),
        chr(ord(s) + 1),
        padleft(s, 8, ".") ,
        padright(s, 8, "xy"),
        repeat(charat(s, 0), 3),
        trimprefix(trimsuffix(s, "z"), "a")
    ), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			input: "abc,d e\nzz z,z\nq",
		},
		{
			// Rune-counted padding: a byte-counted implementation would pad
			// these to different widths in the two backends.
			name: "padding counts runes",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (s) -> textjoin(list(padleft(s, 6, "-"), padright(s, 6, "-"), totext(length(padleft(s, 6, "-")))), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			input: "héllo\nnaïve\nab",
		},
		{
			name: "text classification",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (s) -> textjoin(list(
        if isdigit(s) then "d" else "-",
        if isalpha(s) then "a" else "-",
        if isupper(s) then "U" else "-",
        if islower(s) then "l" else "-"
    ), "")
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "123\nabc\nABC\nAb1\n1a\n",
		},
		{
			name: "float tower",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> textjoin(list(
        totext(round(log2(tofloat(pow(2, mod(n, 20)))))),
        totext(round(log(exp(tofloat(mod(n, 5)))))),
        totext(round(log10(tofloat(1000)))),
        totext(round(hypot(tofloat(mod(n, 4)) * 3.0, tofloat(mod(n, 4)) * 4.0))),
        totext(round(atan2(0.0, 1.0))),
        totext(round(sin(0.0) + cos(0.0) + tan(0.0))),
        totext(trunc(0.0 - 2.7)),
        totext(trunc(2.7)),
        totext(round(pow(tofloat(mod(n, 9) + 1), 0.5) * 100.0))
    ), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			input: "1\n4\n7\n11",
		},
		{
			// A named accumulator: record + with is the whole reason a fold can
			// carry more than one number without positional item() indexing.
			name: "record accumulator in a fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> record("lo", 1000000, "hi", 0 - 1000000, "n", 0)
    Using: (acc, v) ->
        with(with(with(acc, "lo", min(acc.lo, v)), "hi", max(acc.hi, v)), "n", acc.n + 1)
Cursed Technique: Apply
    Using: (r) -> textjoin(list(totext(r.lo), totext(r.hi), totext(r.n)), "|")
Reveal: stdout
`,
			input: "5\n-3\n12\n0\n7",
		},
		{
			// with must copy: if it aliased, the original record read after it
			// would differ between a struct-copying backend and a pointer one.
			name: "record with is functional",
			src: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) ->
        consider a as record("k", "one", "v", 1)
        in consider b as with(a, "v", 99)
        in textjoin(list(a.k, totext(a.v), b.k, totext(b.v)), "|")
Reveal: stdout
`,
			input: "ignored",
		},
		{
			name: "bases and bits",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> textjoin(list(
        tohex(n), tobin(n), tobase(n, 36),
        totext(fromhex(tohex(n))),
        totext(frombase(tobase(n, 7), 7)),
        totext(popcount(n)),
        totext(bnot(n)),
        if testbit(n, 0) then "b0" else "-"
    ), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			input: "0\n1\n255\n-9\n1048576",
		},
		{
			// Miller-Rabin and the divisor walk, written twice — this is the
			// case where "the two backends agree" is a claim about algorithms
			// rather than about two calls to the same stdlib function.
			name: "number theory",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> textjoin(list(
        if isprime(n) then "P" else "-",
        totext(length(divisors(abs(n) + 1))),
        totext(first(divisors(abs(n) + 1))),
        totext(last(divisors(abs(n) + 1))),
        totext(length(digits(n))),
        totext(fromdigits(digits(abs(n))))
    ), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			// 2147483647 is a Mersenne prime and 4295098369 is its square's
			// neighbour — both past where a 32-bit mulmod would silently wrap.
			input: "1\n2\n91\n97\n0\n-28\n2147483647\n4295098369",
		},
		{
			// textjoin no longer requires List<Text>: any element type renders
			// exactly as Reveal would (Int, Float, Bool, Record) and gets joined.
			name: "textjoin over non-Text elements",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> textjoin(list(
        textjoin(list(n, n * 2, n * 3), "-"),
        textjoin(list(tofloat(n), tofloat(n) / 2.0), "-"),
        textjoin(list(n > 0, n = 0), "-"),
        textjoin(list(record("a", n, "b", n * 2)), "-")
    ), "|")
Maximum Technique: Join with "\n"
Reveal: stdout
`,
			input: "3\n-2\n0",
		},
		{
			name: "crt over non-coprime moduli",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> textjoin(list(
        totext(crt(list(2, 3), list(3, 5))),
        totext(crt(list(1, 1), list(4, 6))),
        totext(crt(list(0), list(7))),
        totext(crt(list(6, 13), list(7, 15)))
    ), "|")
Reveal: stdout
`,
			input: "1",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
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

// A builtin whose argument is only a type witness still has to *evaluate* it:
// the interpreter evaluates every argument before dispatching, so an
// `emptyset(first(xs))` over an empty list must fail in both backends or the
// two disagree about whether the program runs at all.
func TestCompiledTypeWitnessesAreStillEvaluated(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> size(emptyset(first(drop(xs, 99)))) + length(emptylist(0))
Reveal: stdout
`
	pipe := compilePipeline(t, src, false)
	input := []byte("1\n2")

	// The interpreter fails on `first` of an empty list.
	var out bytes.Buffer
	if _, err := interp.Run(pipe, &ir.Context{Stdin: bytes.NewReader(input), Stdout: &out}); err == nil {
		t.Fatal("the interpreter should fail on first of an empty list")
	}

	// So must the binary — a witness the compiler dropped would exit 0.
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		t.Fatalf("BuildBinary: %v\n%s", err, goSrc)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		t.Error("the binary exited 0: the type witness was dropped instead of evaluated")
	}
}
