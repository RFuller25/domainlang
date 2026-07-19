package optimizer

import (
	"math/rand"
	"testing"
)

// Naive oracles for the early-exit search rewrite: the full-grid BFS and
// Dijkstra the user actually named, read at the target afterwards — exactly
// what prims/search.go computes.

func naiveBFSAt(rows, cols int, mask []bool, sr, sc, tr, tc int64) int64 {
	w := int64(cols)
	dist := make([]int64, rows*cols)
	for i := range dist {
		dist[i] = -1
	}
	dist[sr*w+sc] = 0
	queue := [][2]int64{{sr, sc}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		d := dist[cur[0]*w+cur[1]]
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur[0]+dl[0], cur[1]+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= w {
				continue
			}
			i := nr*w + nc
			if !mask[i] || dist[i] != -1 {
				continue
			}
			dist[i] = d + 1
			queue = append(queue, [2]int64{nr, nc})
		}
	}
	return dist[tr*w+tc]
}

func naiveDijkstraAt(rows, cols int, costs []int64, sr, sc, tr, tc int64) int64 {
	w := int64(cols)
	dist := make([]int64, rows*cols)
	for i := range dist {
		dist[i] = -1
	}
	type item struct{ d, r, c int64 }
	h := []item{{0, sr, sc}}
	for len(h) > 0 {
		// O(n) extract-min is fine for oracle-sized grids.
		m := 0
		for i := range h {
			if h[i].d < h[m].d {
				m = i
			}
		}
		cur := h[m]
		h = append(h[:m], h[m+1:]...)
		i := cur.r*w + cur.c
		if dist[i] != -1 {
			continue
		}
		dist[i] = cur.d
		for _, dl := range [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur.r+dl[0], cur.c+dl[1]
			if nr < 0 || nr >= int64(rows) || nc < 0 || nc >= w {
				continue
			}
			j := nr*w + nc
			if dist[j] != -1 {
				continue
			}
			h = append(h, item{cur.d + costs[j], nr, nc})
		}
	}
	return dist[tr*w+tc]
}

func TestBFSTargetMatchesFullSearch(t *testing.T) {
	rng := rand.New(rand.NewSource(21))
	for iter := 0; iter < 4000; iter++ {
		rows, cols := rng.Intn(7)+1, rng.Intn(7)+1
		mask := make([]bool, rows*cols)
		for i := range mask {
			mask[i] = rng.Intn(3) > 0 // ~2/3 walkable: mixes reachable and not
		}
		// Start must be walkable (the pass validates before searching).
		sr, sc := int64(rng.Intn(rows)), int64(rng.Intn(cols))
		mask[sr*int64(cols)+sc] = true
		tr, tc := int64(rng.Intn(rows)), int64(rng.Intn(cols))
		got := BFSTarget(rows, cols, mask, sr, sc, tr, tc)
		want := naiveBFSAt(rows, cols, mask, sr, sc, tr, tc)
		if got != want {
			t.Fatalf("iter %d: BFSTarget %dx%d mask=%v start=(%d,%d) target=(%d,%d): got %d want %d",
				iter, rows, cols, mask, sr, sc, tr, tc, got, want)
		}
	}
}

func TestDijkstraTargetMatchesFullSearch(t *testing.T) {
	rng := rand.New(rand.NewSource(22))
	for iter := 0; iter < 4000; iter++ {
		rows, cols := rng.Intn(7)+1, rng.Intn(7)+1
		costs := make([]int64, rows*cols)
		for i := range costs {
			costs[i] = int64(rng.Intn(9))
		}
		sr, sc := int64(rng.Intn(rows)), int64(rng.Intn(cols))
		tr, tc := int64(rng.Intn(rows)), int64(rng.Intn(cols))
		got := DijkstraTarget(rows, cols, costs, sr, sc, tr, tc)
		want := naiveDijkstraAt(rows, cols, costs, sr, sc, tr, tc)
		if got != want {
			t.Fatalf("iter %d: DijkstraTarget %dx%d costs=%v start=(%d,%d) target=(%d,%d): got %d want %d",
				iter, rows, cols, costs, sr, sc, tr, tc, got, want)
		}
	}
}
