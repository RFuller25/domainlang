// Hand-written Go counterpart of pipeline_body.domain: the largest per-row
// sum of squares.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var best int64
	first := true
	for sc.Scan() {
		var row int64
		for _, f := range strings.Split(sc.Text(), " ") {
			n, err := strconv.ParseInt(f, 10, 64)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			row += n * n
		}
		if first || row > best {
			best = row
		}
		first = false
	}
	fmt.Println(best)
}
