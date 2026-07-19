package optimizer

import (
	"math/rand"
	"sort"
	"testing"
)

// Naive oracles for the scan rewrites: the O(n log n) / O(n²) / O(n³)
// algorithms the user actually named. Each property test drives the fast
// implementation against these over thousands of random inputs.

func naiveKth(xs []int64, k int, desc bool) int64 {
	a := append([]int64(nil), xs...)
	if desc {
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	} else {
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	}
	return a[k]
}

func naiveTripleCount(xs []int64, target int64) int64 {
	var c int64
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			for k := j + 1; k < len(xs); k++ {
				if xs[i]+xs[j]+xs[k] == target {
					c++
				}
			}
		}
	}
	return c
}

func naiveTripleFirst(xs []int64, target int64) ([]int64, bool) {
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			for k := j + 1; k < len(xs); k++ {
				if xs[i]+xs[j]+xs[k] == target {
					return []int64{xs[i], xs[j], xs[k]}, true
				}
			}
		}
	}
	return nil, false
}

func naiveDiffCount(xs []int64, target int64, flipped bool) int64 {
	var c int64
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			d := xs[i] - xs[j]
			if flipped {
				d = xs[j] - xs[i]
			}
			if d == target {
				c++
			}
		}
	}
	return c
}

func naiveDiffFirst(xs []int64, target int64, flipped bool) ([]int64, bool) {
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			d := xs[i] - xs[j]
			if flipped {
				d = xs[j] - xs[i]
			}
			if d == target {
				return []int64{xs[i], xs[j]}, true
			}
		}
	}
	return nil, false
}

func randInts(rng *rand.Rand, maxLen, span int) []int64 {
	xs := make([]int64, rng.Intn(maxLen+1))
	for i := range xs {
		xs[i] = int64(rng.Intn(2*span+1) - span)
	}
	return xs
}

func TestKthOrderStatisticMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for iter := 0; iter < 3000; iter++ {
		xs := randInts(rng, 25, 10)
		if len(xs) == 0 {
			continue
		}
		k := rng.Intn(len(xs))
		for _, desc := range []bool{false, true} {
			got := KthOrderStatistic(xs, k, desc)
			want := naiveKth(xs, k, desc)
			if got != want {
				t.Fatalf("iter %d: Kth(%v, %d, desc=%v) = %d, want %d", iter, xs, k, desc, got, want)
			}
		}
	}
}

func TestTripleSumMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for iter := 0; iter < 2000; iter++ {
		xs := randInts(rng, 18, 6)
		target := int64(rng.Intn(31) - 15)
		if got, want := CountTripleSum(xs, target), naiveTripleCount(xs, target); got != want {
			t.Fatalf("iter %d: CountTripleSum(%v, %d) = %d, want %d", iter, xs, target, got, want)
		}
		got, gok := FindTripleSum(xs, target)
		want, wok := naiveTripleFirst(xs, target)
		if gok != wok || !equalSlices(got, want) {
			t.Fatalf("iter %d: FindTripleSum(%v, %d) = %v,%v want %v,%v", iter, xs, target, got, gok, want, wok)
		}
	}
}

func TestPairDiffMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for iter := 0; iter < 3000; iter++ {
		xs := randInts(rng, 22, 8)
		target := int64(rng.Intn(21) - 10)
		for _, flipped := range []bool{false, true} {
			if got, want := CountPairDiff(xs, target, flipped), naiveDiffCount(xs, target, flipped); got != want {
				t.Fatalf("iter %d: CountPairDiff(%v, %d, %v) = %d, want %d", iter, xs, target, flipped, got, want)
			}
			got, gok := FindPairDiff(xs, target, flipped)
			want, wok := naiveDiffFirst(xs, target, flipped)
			if gok != wok || !equalSlices(got, want) {
				t.Fatalf("iter %d: FindPairDiff(%v, %d, %v) = %v,%v want %v,%v",
					iter, xs, target, flipped, got, gok, want, wok)
			}
		}
	}
}
