// Hand-written Go counterpart of shikigami_calls.domain: the two weightings
// and the filter, written out where Domain inlines them.
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
	var total int64
	for sc.Scan() {
		n, err := strconv.ParseInt(sc.Text(), 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		v := (n*3+1)*2 + 1
		if v%7 > 2 {
			total += v
		}
	}
	fmt.Println(total)
}
