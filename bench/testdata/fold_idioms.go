// Hand-written Go counterpart of fold_idioms.domain: the map and the
// conditional sum as the one loop they amount to.
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
		v := n * 7 % 1009
		if v%3 == 0 {
			total += v * v
		}
	}
	fmt.Println(total)
}
