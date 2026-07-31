// Hand-written Go counterpart of merge_ranges.domain: how many ranges are
// left once every overlapping "lo-hi" pair is merged.
package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	type span struct{ lo, hi int64 }
	var spans []span
	for sc.Scan() {
		l, h, ok := strings.Cut(sc.Text(), "-")
		if !ok {
			fmt.Fprintln(os.Stderr, "bad line")
			os.Exit(1)
		}
		lo, err1 := strconv.ParseInt(l, 10, 64)
		hi, err2 := strconv.ParseInt(h, 10, 64)
		if err1 != nil || err2 != nil {
			fmt.Fprintln(os.Stderr, "bad number")
			os.Exit(1)
		}
		spans = append(spans, span{lo, hi})
	}
	slices.SortFunc(spans, func(x, y span) int {
		if x.lo != y.lo {
			return int(x.lo - y.lo)
		}
		return int(x.hi - y.hi)
	})
	count := 0
	var cur span
	for i, s := range spans {
		if i == 0 {
			cur = s
			count = 1
			continue
		}
		if s.lo <= cur.hi+1 {
			if s.hi > cur.hi {
				cur.hi = s.hi
			}
			continue
		}
		cur = s
		count++
	}
	fmt.Println(count)
}
