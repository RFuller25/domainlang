package eval

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

// The number-theory builtins against math/big. The interpreter and compiled
// backends are held to each other by parity tests, which cannot see a bug the
// two share — isqrt(2) = 2, a negative modpow above a ~3e9 modulus and a
// negative choose(62, 31) all passed them. This holds them to the answer.
func TestNumberTheoryMatchesBigInt(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	moduli := []int64{1, 2, 7, 1000007, 3037000499, 3037000500, 119315717514047, math.MaxInt64 - 24}
	for range 20000 {
		m := moduli[rng.Intn(len(moduli))]
		base, exp := rng.Int63()-rng.Int63(), rng.Int63n(1<<20)
		got, err := modPow(base, exp, m)
		want := new(big.Int).Exp(big.NewInt(base), big.NewInt(exp), big.NewInt(m))
		if err != nil || got != want.Int64() {
			t.Fatalf("modpow(%d, %d, %d) = %d, %v; want %s", base, exp, m, got, err, want)
		}

		a := rng.Int63() - rng.Int63()
		if inv := new(big.Int).ModInverse(big.NewInt(a), big.NewInt(m)); inv != nil && m > 1 {
			if got, err := modInverse(a, m); err != nil || got != inv.Int64() {
				t.Fatalf("modinv(%d, %d) = %d, %v; want %s", a, m, got, err, inv)
			}
		}

		x := rng.Int63() >> rng.Intn(63)
		if got, _ := isqrtInt(x); got != new(big.Int).Sqrt(big.NewInt(x)).Int64() {
			t.Fatalf("isqrt(%d) = %d", x, got)
		}
	}
	for n := int64(0); n <= 70; n++ {
		for k := int64(0); k <= n; k++ {
			want := new(big.Int).Binomial(n, k)
			got, err := chooseInt(n, k)
			if !want.IsInt64() {
				if err == nil {
					t.Fatalf("choose(%d, %d) = %d, want an overflow error", n, k, got)
				}
				continue
			}
			if err != nil || got != want.Int64() {
				t.Fatalf("choose(%d, %d) = %d, %v; want %s", n, k, got, err, want)
			}
		}
	}
}
