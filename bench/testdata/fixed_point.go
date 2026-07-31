// Hand-written Go counterpart of fixed_point.domain: halve until the list
// stops changing, comparing it against the previous one each lap.
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
	for {
		next := make([]int64, len(xs))
		for i, x := range xs {
			if x > 1 {
				next[i] = x / 2
			} else {
				next[i] = x
			}
		}
		if slices.Equal(next, xs) {
			break
		}
		xs = next
	}
	var total int64
	for _, x := range xs {
		total += x
	}
	fmt.Println(total)
}
