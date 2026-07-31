// Hand-written Go counterpart of read_length.domain: the number of characters
// on stdin, trailing newlines excluded the way Read Source excludes them.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s := strings.TrimRight(string(data), "\r\n")
	fmt.Println(utf8.RuneCountInString(s))
}
