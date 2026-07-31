// Hand-written Go counterpart of partition_parts.domain: twice the sum of the
// multiples of three, and the index of the first 424242.
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
	var sum int64
	found := -1
	i := 0
	for sc.Scan() {
		n, err := strconv.ParseInt(sc.Text(), 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if n%3 == 0 {
			sum += n * 2
		}
		if found < 0 && n == 424242 {
			found = i
		}
		i++
	}
	fmt.Printf("Part 1: %d\n", sum)
	fmt.Printf("Part 2: %d\n", found)
}
