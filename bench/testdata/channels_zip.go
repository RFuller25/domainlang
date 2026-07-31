// Hand-written Go counterpart of channels_zip.domain: pair the even numbers
// with the odd ones in order and sum the products.
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
	var evens, odds []int64
	for _, x := range xs {
		if x%2 == 0 {
			evens = append(evens, x)
		} else {
			odds = append(odds, x)
		}
	}
	n := len(evens)
	if len(odds) < n {
		n = len(odds)
	}
	var total int64
	for i := 0; i < n; i++ {
		total += evens[i] * odds[i]
	}
	fmt.Println(total)
}
