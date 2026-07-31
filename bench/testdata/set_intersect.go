// Hand-written Go counterpart of set_intersect.domain: how many characters
// appear on every line.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var common map[rune]bool
	for sc.Scan() {
		line := make(map[rune]bool)
		for _, r := range sc.Text() {
			line[r] = true
		}
		if common == nil {
			common = line
			continue
		}
		for r := range common {
			if !line[r] {
				delete(common, r)
			}
		}
	}
	fmt.Println(len(common))
}
