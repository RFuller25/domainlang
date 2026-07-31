// Hand-written Go counterpart of group_map_values.domain: the largest sum of
// any bucket when the numbers are grouped by their value mod 1000.
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
	sums := make(map[int64]int64)
	for sc.Scan() {
		n, err := strconv.ParseInt(sc.Text(), 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		sums[((n%1000)+1000)%1000] += n
	}
	best, first := int64(0), true
	for _, s := range sums {
		if first || s > best {
			best, first = s, false
		}
	}
	fmt.Println(best)
}
