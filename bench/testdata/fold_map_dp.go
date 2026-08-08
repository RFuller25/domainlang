// Hand-written Go counterpart of fold_map_dp.domain: build a frequency map
// keyed by n % 50000, then report its size plus its largest bucket.
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
	counts := make(map[int64]int64)
	for sc.Scan() {
		n, err := strconv.ParseInt(sc.Text(), 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		counts[n%50000]++
	}
	var best int64
	for _, n := range counts {
		if n > best {
			best = n
		}
	}
	fmt.Println(int64(len(counts)) + best)
}
