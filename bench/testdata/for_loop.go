// Hand-written Go counterpart of for_loop.domain: eight passes, each adding
// the loop variable to every element.
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
	// In place: the loop body rewrites every element, and nothing needs the
	// previous lap's list afterwards.
	for k := int64(0); k < 8; k++ {
		for i := range xs {
			xs[i] += k
		}
	}
	var total int64
	for _, x := range xs {
		total += x
	}
	fmt.Println(total)
}
