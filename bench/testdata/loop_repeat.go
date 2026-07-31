// Hand-written Go counterpart of loop_repeat.domain: five million laps of a
// Lehmer generator, threading one integer through.
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
	x, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for i := 0; i < 5_000_000; i++ {
		x = (x * 48271) % 2147483647
	}
	fmt.Println(x)
}
