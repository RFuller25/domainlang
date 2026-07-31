// Hand-written Go counterpart of scan_mod.domain: how many prefix sums are
// divisible by 7.
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
	var acc int64
	count := 0
	for sc.Scan() {
		n, err := strconv.ParseInt(sc.Text(), 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		acc += n
		if acc%7 == 0 {
			count++
		}
	}
	fmt.Println(count)
}
