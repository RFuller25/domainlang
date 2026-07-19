package optimizer

import (
	"math/rand"
	"testing"
)

// Naive oracles for the divisor-scan rewrite: the O(n²) pair scan the user
// actually named. Inputs are biased toward small spans so zeros and repeated
// values occur often — zero is the case the division shortcut must special-
// case (a zero element pairs with everything exactly when the target is 0).

func naiveProductCount(xs []int64, target int64) int64 {
	var c int64
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			if xs[i]*xs[j] == target {
				c++
			}
		}
	}
	return c
}

func naiveProductFirst(xs []int64, target int64) ([]int64, bool) {
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			if xs[i]*xs[j] == target {
				return []int64{xs[i], xs[j]}, true
			}
		}
	}
	return nil, false
}

func TestPairProductMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for iter := 0; iter < 5000; iter++ {
		xs := randInts(rng, 22, 6)
		// Half the iterations aim at reachable products (including 0), the
		// rest at arbitrary small targets that mostly miss.
		var target int64
		if iter%2 == 0 && len(xs) >= 2 {
			target = xs[rng.Intn(len(xs))] * xs[rng.Intn(len(xs))]
		} else {
			target = int64(rng.Intn(41) - 20)
		}
		if got, want := CountPairProduct(xs, target), naiveProductCount(xs, target); got != want {
			t.Fatalf("iter %d: CountPairProduct(%v, %d) = %d, want %d", iter, xs, target, got, want)
		}
		got, gok := FindPairProduct(xs, target)
		want, wok := naiveProductFirst(xs, target)
		if gok != wok || !equalSlices(got, want) {
			t.Fatalf("iter %d: FindPairProduct(%v, %d) = %v,%v want %v,%v", iter, xs, target, got, gok, want, wok)
		}
	}
}
