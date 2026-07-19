package ir

// UnionFind is a disjoint-set forest over the integers [0, n), with path
// compression and union by size. It backs the Connected Components primitive
// (grid regions) and is available to any future primitive that needs to merge
// equivalence classes incrementally.
type UnionFind struct {
	parent []int
	size   []int
	count  int // number of distinct sets
}

// NewUnionFind creates n singleton sets, one per integer in [0, n).
func NewUnionFind(n int) *UnionFind {
	u := &UnionFind{
		parent: make([]int, n),
		size:   make([]int, n),
		count:  n,
	}
	for i := range u.parent {
		u.parent[i] = i
		u.size[i] = 1
	}
	return u
}

// Find returns the canonical representative of x's set, compressing the path
// along the way.
func (u *UnionFind) Find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]] // path halving
		x = u.parent[x]
	}
	return x
}

// Union merges the sets containing x and y, reporting whether they were
// previously distinct.
func (u *UnionFind) Union(x, y int) bool {
	rx, ry := u.Find(x), u.Find(y)
	if rx == ry {
		return false
	}
	if u.size[rx] < u.size[ry] {
		rx, ry = ry, rx
	}
	u.parent[ry] = rx
	u.size[rx] += u.size[ry]
	u.count--
	return true
}

// Connected reports whether x and y are in the same set.
func (u *UnionFind) Connected(x, y int) bool { return u.Find(x) == u.Find(y) }

// Count reports the number of distinct sets remaining.
func (u *UnionFind) Count() int { return u.count }
