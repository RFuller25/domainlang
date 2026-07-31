// Hand-written Go counterpart of combinations3.domain: the product of the
// three entries that sum to 2020 (AoC 2020 Day 1 Part 2), found the way a
// Go programmer who did not want the O(n^3) scan would find them.
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
	// For each pair, ask a set whether the complement is present: O(n^2)
	// rather than the O(n^3) the problem statement suggests.
	seen := make(map[int64]int, len(xs))
	for i, v := range xs {
		if _, ok := seen[v]; !ok {
			seen[v] = i
		}
	}
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			want := 2020 - xs[i] - xs[j]
			if k, ok := seen[want]; ok && k != i && k != j {
				fmt.Println(xs[i] * xs[j] * want)
				return
			}
		}
	}
	fmt.Fprintln(os.Stderr, "no triple sums to 2020")
	os.Exit(1)
}
