// Hand-written Go counterpart of float_sum.domain: the total, rendered the
// way Reveal renders a Float.
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
	var total float64
	for sc.Scan() {
		f, err := strconv.ParseFloat(strings.TrimSpace(sc.Text()), 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		total += f
	}
	fmt.Println(strconv.FormatFloat(total, 'g', -1, 64))
}
