// Hand-written Go counterpart of list_shaping.domain: chunk, total each
// block, stop at the first big one, deduplicate, count.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var xs []int64
	for sc.Scan() {
		n, err := strconv.ParseInt(sc.Text(), 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		xs = append(xs, n)
	}
	// Chunk 8, keeping a short final block, and total each one.
	sums := make([]int64, 0, len(xs)/8+1)
	for i := 0; i < len(xs); i += 8 {
		end := i + 8
		if end > len(xs) {
			end = len(xs)
		}
		var s int64
		for _, v := range xs[i:end] {
			s += v
		}
		sums = append(sums, s)
	}
	// Take While: the leading run, stopping at the first failure.
	cut := len(sums)
	for i, s := range sums {
		if !(s < 500000) {
			cut = i
			break
		}
	}
	// Unique, first-seen order, then reverse — neither changes the count,
	// but the Domain program does both, so this does too.
	seen := make(map[int64]struct{}, cut)
	uniq := make([]int64, 0, cut)
	for _, s := range sums[:cut] {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			uniq = append(uniq, s)
		}
	}
	for i, j := 0, len(uniq)-1; i < j; i, j = i+1, j-1 {
		uniq[i], uniq[j] = uniq[j], uniq[i]
	}
	fmt.Println(len(uniq))
}
