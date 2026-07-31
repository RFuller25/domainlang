// Hand-written Go counterpart of match_pattern.domain: how many "a-b,c-d"
// lines hold one range fully inside the other.
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
	count := 0
	for sc.Scan() {
		left, right, ok := strings.Cut(sc.Text(), ",")
		if !ok {
			fmt.Fprintln(os.Stderr, "bad line")
			os.Exit(1)
		}
		a, b := parseRange(left)
		c, d := parseRange(right)
		if (a <= c && b >= d) || (c <= a && d >= b) {
			count++
		}
	}
	fmt.Println(count)
}

func parseRange(s string) (int64, int64) {
	lo, hi, ok := strings.Cut(s, "-")
	if !ok {
		fmt.Fprintln(os.Stderr, "bad range")
		os.Exit(1)
	}
	a, err1 := strconv.ParseInt(lo, 10, 64)
	b, err2 := strconv.ParseInt(hi, 10, 64)
	if err1 != nil || err2 != nil {
		fmt.Fprintln(os.Stderr, "bad number")
		os.Exit(1)
	}
	return a, b
}
