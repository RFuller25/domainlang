// Hand-written Go counterpart of sort_gaps.domain: sort descending, count the
// neighbours that are equal. slices.Sort is the fastest sort in the standard
// library, which is what a Go programmer would reach for.
package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strconv"
)

func readInts() []int64 {
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
	return xs
}

func main() {
	xs := readInts()
	slices.Sort(xs)
	slices.Reverse(xs)
	count := 0
	for i := 1; i < len(xs); i++ {
		if xs[i-1]-xs[i] == 0 {
			count++
		}
	}
	fmt.Println(count)
}
