package ir

// Sliding-window reductions over int64 slices. They live in ir next to the
// other runtime collections (see deque.go) because two layers need the same
// implementation: the Sliding Reduce primitive, which lets a program ask for
// them by name, and the optimizer's Window + Map Each fusion, which arrives at
// them from a naive pipeline. One copy means one thing to property-test.
//
// Both are O(n) in the length of the input regardless of the window size — the
// point of the exercise, since the naive form is O(n·size) and materializes
// every window besides.

// WindowedSums returns the sums of every fully-contained window, via prefix
// sums: O(n) regardless of window size or step.
func WindowedSums(xs []int64, size, step int64) []int64 {
	pre := make([]int64, len(xs)+1)
	for i, x := range xs {
		pre[i+1] = pre[i] + x
	}
	out := []int64{}
	for i := int64(0); i+size <= int64(len(xs)); i += step {
		out = append(out, pre[i+size]-pre[i])
	}
	return out
}

// WindowedExtrema returns the max (or min) of every fully-contained window
// using a monotonic deque of candidate indices: every element is pushed and
// popped at most once, so the whole scan is O(n) regardless of window size.
func WindowedExtrema(xs []int64, size, step int64, min bool) []int64 {
	beats := func(a, b int64) bool {
		if min {
			return a < b
		}
		return a > b
	}
	out := []int64{}
	deque := []int64{} // indices into xs; xs[deque[0]] is the current extremum
	next := int64(0)   // next index to admit into the deque
	for s := int64(0); s+size <= int64(len(xs)); s += step {
		for ; next < s+size; next++ {
			for len(deque) > 0 && !beats(xs[deque[len(deque)-1]], xs[next]) {
				deque = deque[:len(deque)-1]
			}
			deque = append(deque, next)
		}
		for deque[0] < s {
			deque = deque[1:]
		}
		out = append(out, xs[deque[0]])
	}
	return out
}

// WindowedProducts returns the product of every fully-contained window. Unlike
// sums there is no prefix trick that survives zeros, so this is the honest
// O(n·size) scan — it exists so Sliding Reduce covers the same Mode set as the
// other reductions, and it still avoids materializing the windows themselves.
func WindowedProducts(xs []int64, size, step int64) []int64 {
	out := []int64{}
	for i := int64(0); i+size <= int64(len(xs)); i += step {
		p := int64(1)
		for _, x := range xs[i : i+size] {
			p *= x
		}
		out = append(out, p)
	}
	return out
}
