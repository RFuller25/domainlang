// Hand-written Go counterpart of dead_sort.domain — written by someone who
// noticed that the sort cannot change a sum, and so left it out. This is the
// program Domain has to arrive at from a source that asks for the sort.
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
		if n%2 == 1 {
			total += n*3 + 1
		}
	}
	fmt.Println(total)
}
