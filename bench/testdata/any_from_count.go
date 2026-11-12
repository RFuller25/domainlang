// Hand-written Go counterpart of any_from_count.domain: stop at the first
// match. Both sides still read and parse the whole input — Domain parses the
// list before any stage runs, and this program is written the same way so the
// comparison is about the search, not about how much input was skipped.
package main

import (
	"bufio"
	"fmt"
	"os"
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
	found := false
	for _, x := range xs {
		if x%1000 == 777 {
			found = true
			break
		}
	}
	fmt.Println(found)
}
