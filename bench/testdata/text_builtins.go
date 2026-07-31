// Hand-written Go counterpart of text_builtins.domain: how many lines start
// with "ab", contain "zq" past position 0, or upper-case to "XYZ..." .
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
	count := 0
	for sc.Scan() {
		s := sc.Text()
		head := s
		if len(head) > 3 {
			head = head[:3]
		}
		if strings.HasPrefix(s, "ab") || strings.Index(s, "zq") > 0 || strings.ToUpper(head) == "XYZ" {
			count++
		}
	}
	fmt.Println(count)
}
