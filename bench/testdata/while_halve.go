// Hand-written Go counterpart of while_halve.domain: halve everything until
// nothing is above 1, then total it.
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
	for {
		max := xs[0]
		for _, x := range xs[1:] {
			if x > max {
				max = x
			}
		}
		if !(max > 1) {
			break
		}
		for i, x := range xs { // in place; the previous lap is not needed again
			if x > 1 {
				xs[i] = x / 2
			}
		}
	}
	var total int64
	for _, x := range xs {
		total += x
	}
	fmt.Println(total)
}
