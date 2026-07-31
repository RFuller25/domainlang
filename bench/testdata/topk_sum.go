// Hand-written Go counterpart of topk_sum.domain: sort descending, sum the
// top three — the spelling the Domain program also asks for, before the
// optimizer substitutes a quickselect for it.
package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
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
	// slices.Sort (pdqsort over int64 directly) rather than sort.Slice: no
	// per-comparison closure call, and the fastest sort the standard library
	// offers. Descending is the ascending sort read from the back.
	slices.Sort(xs)
	var total int64
	for _, v := range xs[max(0, len(xs)-3):] {
		total += v
	}
	fmt.Println(total)
}
