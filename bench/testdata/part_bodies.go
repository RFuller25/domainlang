// Hand-written Go counterpart of part_bodies.domain: the top three by a single
// scan, and the odd-number sum as one loop — the sort a programmer who noticed
// would leave out of both.
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
	var a, b, c int64 = -1 << 62, -1 << 62, -1 << 62
	var total int64
	for _, x := range xs {
		switch {
		case x > a:
			a, b, c = x, a, b
		case x > b:
			b, c = x, b
		case x > c:
			c = x
		}
		if x%2 == 1 {
			total += x*3 + 1
		}
	}
	fmt.Println("Part top:", a+b+c)
	fmt.Println("Part sum:", total)
}
