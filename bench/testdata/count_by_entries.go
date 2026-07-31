// Hand-written Go counterpart of count_by_entries.domain: the size of the
// largest bucket when lines are grouped by their first three characters.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	counts := make(map[string]int)
	for sc.Scan() {
		s := sc.Text()
		if len(s) > 3 {
			s = s[:3]
		}
		counts[s]++
	}
	best := 0
	for _, n := range counts {
		if n > best {
			best = n
		}
	}
	fmt.Println(best)
}
