package optimizer

import (
	"math/rand"
	"sort"
	"testing"
)

// BenchmarkTopKQuickselectVsFullSort demonstrates the thesis behind
// fuseSortThenTopK: quickselect (TopK) should beat a full sort + slice at
// large N.
func BenchmarkTopKQuickselectVsFullSort(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	n := 200_000
	xs := make([]int64, n)
	for i := range xs {
		xs[i] = rng.Int63n(1_000_000_000)
	}
	const k = 10

	b.Run("Quickselect", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = TopK(xs, k, true)
		}
	})

	b.Run("FullSortThenSlice", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cp := append([]int64(nil), xs...)
			sort.Slice(cp, func(i, j int) bool { return cp[i] > cp[j] })
			_ = cp[:k]
		}
	})
}

// BenchmarkHashSetPairScanVsNaive demonstrates the thesis behind
// fuseAllPairsSum: the O(n) hash-set scan should beat the O(n²) pair scan at
// moderate-to-large N.
func BenchmarkHashSetPairScanVsNaive(b *testing.B) {
	rng := rand.New(rand.NewSource(2))
	n := 5_000
	xs := make([]int64, n)
	for i := range xs {
		xs[i] = rng.Int63n(1_000_000)
	}
	// An unreachable target forces both algorithms into their worst case:
	// the naive scan must check every pair (no early hit to short-circuit
	// on), and the hash-set scan must still walk the whole slice once.
	target := int64(1) << 40

	b.Run("HashSetScan", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			FindPairSum(xs, target)
		}
	})

	b.Run("NaiveO(n^2)", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bruteFirstPair(xs, target)
		}
	})
}
