package optimizer

import (
	"math/rand"
	"testing"
)

// Naive oracles for the sliding-window rewrite: materialize every window and
// reduce it, exactly like Window + Map Each does.

func naiveWindows(xs []int64, size, step int64) [][]int64 {
	out := [][]int64{}
	for i := int64(0); i+size <= int64(len(xs)); i += step {
		out = append(out, append([]int64(nil), xs[i:i+size]...))
	}
	return out
}

func naiveWindowedSums(xs []int64, size, step int64) []int64 {
	out := []int64{}
	for _, w := range naiveWindows(xs, size, step) {
		var s int64
		for _, x := range w {
			s += x
		}
		out = append(out, s)
	}
	return out
}

func naiveWindowedExtrema(xs []int64, size, step int64, min bool) []int64 {
	out := []int64{}
	for _, w := range naiveWindows(xs, size, step) {
		ext := w[0]
		for _, x := range w[1:] {
			if (min && x < ext) || (!min && x > ext) {
				ext = x
			}
		}
		out = append(out, ext)
	}
	return out
}

func TestWindowedSumsMatchNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for iter := 0; iter < 5000; iter++ {
		xs := randInts(rng, 30, 12)
		size := int64(rng.Intn(6) + 1)
		step := int64(rng.Intn(4) + 1)
		got := WindowedSums(xs, size, step)
		want := naiveWindowedSums(xs, size, step)
		if !equalSlices(got, want) {
			t.Fatalf("iter %d: WindowedSums(%v, %d, %d) = %v, want %v", iter, xs, size, step, got, want)
		}
	}
}

func TestWindowedExtremaMatchNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	for iter := 0; iter < 5000; iter++ {
		xs := randInts(rng, 30, 12)
		size := int64(rng.Intn(6) + 1)
		step := int64(rng.Intn(4) + 1)
		for _, min := range []bool{false, true} {
			got := WindowedExtrema(xs, size, step, min)
			want := naiveWindowedExtrema(xs, size, step, min)
			if !equalSlices(got, want) {
				t.Fatalf("iter %d: WindowedExtrema(%v, %d, %d, min=%v) = %v, want %v",
					iter, xs, size, step, min, got, want)
			}
		}
	}
}
