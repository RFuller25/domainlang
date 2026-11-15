package eval

import "fmt"

// maxBuildable bounds the collections a single builtin will construct. Like
// ir.maxRepresentableCells it is a *physical* limit rather than a policy one —
// a slice this long cannot be allocated on any machine, and Go's makeslice
// panics rather than returning an error, so a clean message is the only way a
// program gets told what happened.
const maxBuildable = 1 << 40

// intRange builds the half-open [lo, hi), matching the Range primitive.
func intRange(lo, hi int64) ([]any, error) {
	if hi <= lo {
		return []any{}, nil
	}
	// Computed in uint64 so a range spanning zero (say, math.MinInt64 to
	// math.MaxInt64) reports its size instead of overflowing to a negative
	// count and silently building nothing.
	n := uint64(hi) - uint64(lo)
	if n > uint64(buildLimit()) {
		return nil, fmt.Errorf("range: [%d, %d) has %d elements, which is more than can be built", lo, hi, n)
	}
	out := make([]any, n)
	for i := range out {
		out[i] = lo + int64(i)
	}
	return out, nil
}
