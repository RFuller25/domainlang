package bench

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
)

// The generators below are deterministic: the same size always produces the
// same bytes, so a benchmark measured today is comparable with one measured a
// month ago, and the parity test checks the very input the benchmark times
// (only smaller).

// genInts writes n lines of one non-negative integer each.
func genInts(n int) []byte {
	r := rand.New(rand.NewSource(1))
	var sb strings.Builder
	sb.Grow(n * 6)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%d", r.Intn(100000))
	}
	return []byte(sb.String())
}

// genIntsNeedle is genInts with one 424242 planted nine tenths of the way in,
// so a Find Index has to scan most of the list but does find something.
func genIntsNeedle(n int) []byte {
	r := rand.New(rand.NewSource(2))
	needle := n * 9 / 10
	var sb strings.Builder
	sb.Grow(n * 6)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		if i == needle {
			sb.WriteString("424242")
			continue
		}
		fmt.Fprintf(&sb, "%d", r.Intn(100000))
	}
	return []byte(sb.String())
}

// genRows writes n lines of eight space-separated integers.
func genRows(n int) []byte {
	r := rand.New(rand.NewSource(3))
	var sb strings.Builder
	sb.Grow(n * 32)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		for j := 0; j < 8; j++ {
			if j > 0 {
				sb.WriteByte(' ')
			}
			fmt.Fprintf(&sb, "%d", r.Intn(1000))
		}
	}
	return []byte(sb.String())
}

// genWords writes n twelve-character lines over a six-letter alphabet, which
// is small enough that three-character prefixes collide thousands of times
// and wide enough to hit every branch of the text predicate.
func genWords(n int) []byte {
	r := rand.New(rand.NewSource(4))
	const alphabet = "abqxyz"
	var sb strings.Builder
	sb.Grow(n * 13)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		for j := 0; j < 12; j++ {
			sb.WriteByte(alphabet[r.Intn(len(alphabet))])
		}
	}
	return []byte(sb.String())
}

// genRanges writes n "a-b,c-d" lines, a third of which nest one side in the
// other so the predicate is not answered by one branch alone.
func genRanges(n int) []byte {
	r := rand.New(rand.NewSource(5))
	var sb strings.Builder
	sb.Grow(n * 14)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		a := r.Intn(90) + 1
		b := a + r.Intn(10)
		c, d := r.Intn(90)+1, 0
		if i%3 == 0 { // nested inside a-b
			c = a
			d = b - r.Intn(b-a+1)
		} else {
			d = c + r.Intn(10)
		}
		fmt.Fprintf(&sb, "%d-%d,%d-%d", a, b, c, d)
	}
	return []byte(sb.String())
}

// genMaze writes an n×n grid of '.' and '#', a fifth of it wall, with the
// top-left corner always walkable so a search from it has somewhere to go.
func genMaze(n int) []byte {
	r := rand.New(rand.NewSource(7))
	var sb strings.Builder
	sb.Grow(n * (n + 1))
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		for j := 0; j < n; j++ {
			if i == 0 && j == 0 || r.Intn(5) > 0 {
				sb.WriteByte('.')
			} else {
				sb.WriteByte('#')
			}
		}
	}
	return []byte(sb.String())
}

// genSeed writes a single integer: the seed of an implicit-graph search or a
// loop, where the input is not what the program spends its time on.
func genSeed(n int) []byte { return []byte(strconv.Itoa(n)) }

// genLetters writes n lines of thirty characters over a five-letter alphabet,
// small enough that every letter survives an intersection over all of them.
func genLetters(n int) []byte {
	r := rand.New(rand.NewSource(8))
	const alphabet = "abcde"
	var sb strings.Builder
	sb.Grow(n * 31)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		for j := 0; j < 30; j++ {
			sb.WriteByte(alphabet[r.Intn(len(alphabet))])
		}
	}
	return []byte(sb.String())
}

// genKeyed writes n "a-b" lines whose right-hand numbers are a permutation of
// 1..n, so the largest key is unique and the answer cannot depend on how ties
// are broken.
func genKeyed(n int) []byte {
	r := rand.New(rand.NewSource(9))
	keys := r.Perm(n)
	var sb strings.Builder
	sb.Grow(n * 14)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%d-%d", r.Intn(1000000), keys[i]+1)
	}
	return []byte(sb.String())
}

// genSpans writes n "lo-hi" ranges scattered over a space about half their
// total width, so merging them neither collapses everything into one nor
// leaves them all separate.
func genSpans(n int) []byte {
	r := rand.New(rand.NewSource(10))
	var sb strings.Builder
	sb.Grow(n * 16)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		lo := r.Intn(n * 4)
		fmt.Fprintf(&sb, "%d-%d", lo, lo+r.Intn(8))
	}
	return []byte(sb.String())
}

// genEdges writes n "nX -> nY" dependency edges with X < Y, which is what
// keeps the graph acyclic however the numbers fall.
func genEdges(n int) []byte {
	r := rand.New(rand.NewSource(11))
	nodes := n/4 + 2
	var sb strings.Builder
	sb.Grow(n * 14)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		from := r.Intn(nodes - 1)
		to := from + 1 + r.Intn(nodes-from-1)
		fmt.Fprintf(&sb, "n%d -> n%d", from, to)
	}
	return []byte(sb.String())
}

// genLife writes n distinct live cells scattered over a square about twice
// their count, which is dense enough for a Life soup to keep evolving.
func genLife(n int) []byte {
	r := rand.New(rand.NewSource(12))
	side := 1
	for side*side < n*2 {
		side++
	}
	seen := make(map[[2]int]bool, n)
	pts := make([][2]int, 0, n)
	for len(pts) < n {
		p := [2]int{r.Intn(side), r.Intn(side)}
		if !seen[p] {
			seen[p] = true
			pts = append(pts, p)
		}
	}
	var sb strings.Builder
	sb.Grow(n * 8)
	for i, p := range pts {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%d,%d", p[0], p[1])
	}
	return []byte(sb.String())
}

// genTriple writes n numbers holding exactly one triple that sums to 2020.
// Every other number is 1 mod 7 and the three planted ones are 3, 3 and 5, so
// no other combination of three can reach 2020 (which is 4 mod 7): a triple
// with one planted number is 2 mod 7 more than it, with two it is 1 more,
// and with none it is 3.
func genTriple(n int) []byte {
	r := rand.New(rand.NewSource(13))
	xs := make([]int64, 0, n)
	for i := 0; i < n-3; i++ {
		xs = append(xs, int64(r.Intn(400000)/7*7+1))
	}
	// 703 + 703 + 614 = 2020, with residues 3, 3 and 5.
	planted := []int64{703, 703, 614}
	for i, v := range planted {
		at := (i + 1) * n / 4
		xs = append(xs[:at], append([]int64{v}, xs[at:]...)...)
	}
	var sb strings.Builder
	sb.Grow(n * 7)
	for i, v := range xs {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%d", v)
	}
	return []byte(sb.String())
}

// genFloats writes n decimal numbers, one per line.
func genFloats(n int) []byte {
	r := rand.New(rand.NewSource(14))
	var sb strings.Builder
	sb.Grow(n * 9)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%.3f", r.Float64()*1000)
	}
	return []byte(sb.String())
}

// genGrid writes an n×n grid of the digits 1-9 — the AoC risk-map shape.
func genGrid(n int) []byte {
	r := rand.New(rand.NewSource(6))
	var sb strings.Builder
	sb.Grow(n * (n + 1))
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		for j := 0; j < n; j++ {
			sb.WriteByte(byte('1' + r.Intn(9)))
		}
	}
	return []byte(sb.String())
}
