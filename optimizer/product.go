package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// fuseAllPairsProduct recognizes an All Pairs (k=2) over List<Int> whose
// Using: lambda tests a fixed product — `(a, b) -> a * b = K` — and lowers
// the O(n²) scan to an O(n) divisor scan: for each element x the only
// possible partner is K/x (when K divides evenly), looked up in a complement
// multiset. K = 0 is the case the division shortcut would get wrong — a
// zero element pairs with *everything* — so zeros are counted against the
// number of earlier elements instead. Like the sum and difference scans,
// this assumes Domain's numeric model (products stay within int64).
func fuseAllPairsProduct(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for _, list := range nodeLists(p) {
		for _, n := range list {
			mode, lam, ok := combinatorialScan(n, "All Pairs", 2)
			if !ok {
				continue
			}
			target, ok := matchProductPair(lam)
			if !ok {
				continue
			}

			n.Display = fmt.Sprintf("Cursed Divisor Scan (product = %d, Mode: %s)", target, mode)
			n.Meta["target"] = target
			lowerIntScan(n, "DivisorPairScan", mode,
				func(xs []int64) int64 { return CountPairProduct(xs, target) },
				func(xs []int64) ([]int64, bool) { return FindPairProduct(xs, target) })
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain rewrote All Pairs (product = %d) → Cursed Divisor Scan. Guaranteed hit.", target)})
		}
	}
	return rewrites
}

// matchProductPair recognizes `(a, b) -> a * b = K` (in either operand order,
// and with the literal on either side of '='), returning K.
func matchProductPair(lam *ast.Lambda) (int64, bool) {
	return matchCommutativePair(lam, token.STAR)
}

// CountPairProduct counts index pairs i<j with xs[i]*xs[j] == target in O(n).
// A nonzero element x pairs only with target/x (when target divides evenly);
// a zero element pairs with every earlier element exactly when target is 0.
func CountPairProduct(xs []int64, target int64) int64 {
	seen := make(map[int64]int64, len(xs))
	var count, earlier int64
	for _, x := range xs {
		if x == 0 {
			if target == 0 {
				count += earlier
			}
		} else if target%x == 0 {
			count += seen[target/x]
		}
		seen[x]++
		earlier++
	}
	return count
}

// FindPairProduct returns the values [xs[i], xs[j]] of the lexicographically-
// first index pair i<j with xs[i]*xs[j] == target — identical to the naive
// scan's First result — in O(n) via a complement multiset. When xs[i] is 0
// and target is 0 every later element matches, so the partner is xs[i+1].
func FindPairProduct(xs []int64, target int64) ([]int64, bool) {
	remaining := make(map[int64]int, len(xs))
	for _, x := range xs {
		remaining[x]++
	}
	for i, x := range xs {
		remaining[x]-- // now reflects indices > i
		if x == 0 {
			if target == 0 && i+1 < len(xs) {
				return []int64{0, xs[i+1]}, true
			}
			continue
		}
		if target%x == 0 && remaining[target/x] > 0 {
			return []int64{x, target / x}, true
		}
	}
	return nil, false
}
