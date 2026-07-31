// Hand-written Go counterpart of join_output.domain: upper-case every line,
// append "!", and write the lot back out.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	first := true
	for sc.Scan() {
		if !first {
			w.WriteByte('\n')
		}
		first = false
		w.WriteString(strings.ToUpper(sc.Text()))
		w.WriteByte('!')
	}
	w.WriteByte('\n') // Reveal ends the rendered value with a newline
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
