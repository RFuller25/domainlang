// Hand-written Go counterpart of vows_hot.domain: the two assertions the
// Domain program states as Binding Vows, then the sum.
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
	for _, x := range xs { // Binding Vow: All Values >= 0
		if x < 0 {
			fmt.Fprintln(os.Stderr, "vow violated: all values >= 0")
			os.Exit(1)
		}
	}
	if !(len(xs) > 0) { // Binding Vow: Holds
		fmt.Fprintln(os.Stderr, "vow violated")
		os.Exit(1)
	}
	var total int64
	for _, x := range xs {
		total += x % 11
	}
	fmt.Println(total)
}
