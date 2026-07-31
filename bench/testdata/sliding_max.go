// Hand-written Go counterpart of sliding_max.domain: the sum of the maxima of
// every window one thousandth of the list long, via the usual monotonic deque.
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

	size := len(xs) / 1000
	if size < 1 {
		fmt.Fprintln(os.Stderr, "window size measured 0")
		os.Exit(1)
	}

	// Indices whose values decrease, head and tail tracked by index: dropping
	// the front with deque = deque[1:] instead would keep reallocating the
	// backing array as the window slides.
	var total int64
	deque := make([]int, len(xs))
	head, tail := 0, 0
	for i, v := range xs {
		for tail > head && xs[deque[tail-1]] <= v {
			tail--
		}
		deque[tail] = i
		tail++
		if deque[head] <= i-size {
			head++
		}
		if i >= size-1 {
			total += xs[deque[head]]
		}
	}
	fmt.Println(total)
}
