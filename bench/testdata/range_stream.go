// Hand-written Go counterpart of range_stream.domain: a counting loop.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var total int64
	for x := int64(0); x < n; x++ {
		if x%3 == 0 || x%5 == 0 {
			total += x % 1000
		}
	}
	fmt.Println(total)
}
