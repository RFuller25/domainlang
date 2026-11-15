package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Every numeric and bit builtin declared in builtins/numeric.go, run by the
// interpreter and by the compiled binary over arguments the optimizer cannot
// fold (each is applied to the input, not to a literal), in both optimizer
// modes. The emitters are the half of each declaration that nothing in package
// builtins can check, so this is what pins them to the evaluators.
func TestCompiledNumericBuiltinsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
`
	const footer = `Maximum Technique: Join with "\n"
Reveal: stdout
`
	progs := []struct{ name, body string }{
		{"integer arithmetic", `    Using: (n) -> textjoin(list(
        totext(abs(n)), totext(abs(tofloat(n) * 0.5)), totext(sign(n)),
        totext(gcd(n, 12)), totext(lcm(abs(n) + 1, 6)), totext(isqrt(abs(n))),
        totext(factorial(mod(n, 7))), totext(choose(abs(n) + 5, 2)),
        totext(modpow(n, 3, 1000)), totext(modinv(abs(n) + 1, 11)),
        totext(pow(n, 2)), totext(pow(tofloat(n), 2.0)), totext(pow(n, 0.5 * 2.0))
    ), "|")
`},
		{"bit operations", `    Using: (n) -> textjoin(list(
        totext(band(n, 12)), totext(bor(n, 12)), totext(bxor(n, 12)), totext(bnot(n)),
        totext(shl(n, 2)), totext(shr(n, 1)), totext(popcount(n)),
        if testbit(n, 3) then "1" else "0",
        totext(bandall(list(n, 255))), totext(borall(list(n, 1))), totext(bxorall(list(n, 5, 3)))
    ), "|")
`},
		{"bases and number theory", `    Using: (n) -> textjoin(list(
        totext(frombase(tobase(abs(n), 7), 7)), totext(fromhex(tohex(abs(n)))),
        totext(frombin(tobin(abs(n)))), tobase(n, 36), tohex(n), tobin(n),
        totext(fromdigits(digits(n))), totext(length(digits(n))),
        if isprime(abs(n)) then "p" else "c",
        totext(length(divisors(abs(n) + 1))),
        totext(crt(list(mod(n, 3), mod(n, 5)), list(3, 5))),
        totext(prow(solve2x2(94, 22, 8400, 34, 67, 5400)) + pcol(solve2x2(94, 22, 8400, 34, 67, 5400)))
    ), "|")
`},
		{"floats", `    Using: (n) -> textjoin(list(
        totext(floor(tofloat(n) / 3.0)), totext(ceil(tofloat(n) / 3.0)), totext(round(tofloat(n) / 3.0)),
        totext(trunc(tofloat(n) / 3.0)), totext(trunc(n)),
        totext(sqrt(tofloat(abs(n)))), totext(sqrt(abs(n))), totext(tofloat(totext(n))), totext(tofloat(n)),
        totext(round(log(tofloat(abs(n) + 1)) * 1000.0)), totext(round(log2(tofloat(abs(n) + 1)) * 1000.0)),
        totext(round(log10(tofloat(abs(n) + 1)) * 1000.0)), totext(round(exp(tofloat(mod(n, 5))) * 1000.0)),
        totext(round(sin(tofloat(n)) * 1000.0)), totext(round(cos(n) * 1000.0)), totext(round(tan(tofloat(n)) * 1000.0)),
        totext(round(atan2(tofloat(n), 3) * 1000.0)), totext(round(hypot(n, 4) * 1000.0))
    ), "|")
`},
		{"connectives", `    Using: (n) -> textjoin(list(
        if and(n > 0, n < 100) then "1" else "0", if or(n > 0, n < 0 - 5) then "1" else "0",
        if xor(n > 0, n > 10) then "1" else "0", if not(n > 0) then "1" else "0"
    ), "|")
`},
	}
	const input = "-7\n0\n13\n250"
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, header+p.body+footer, optimize)
				want := runInterpreter(t, pipe, []byte(input))
				got := buildAndRun(t, pipe, []byte(input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}
