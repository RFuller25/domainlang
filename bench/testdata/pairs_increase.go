// Hand-written Go counterpart of pairs_increase.domain: count how many
// numbers are larger than the one before them.
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
	count := 0
	prev := 0
	first := true
	for sc.Scan() {
		n, err := strconv.Atoi(sc.Text())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if !first && n > prev {
			count++
		}
		prev, first = n, false
	}
	fmt.Println(count)
}
