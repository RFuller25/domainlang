// Hand-written Go counterpart of sort_by_key.domain: the left number of the
// "a-b" line with the largest right number, via the sort the Domain program
// also asks for.
package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

type pair struct{ a, b int64 }

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var rows []pair
	for sc.Scan() {
		lo, hi, ok := strings.Cut(sc.Text(), "-")
		if !ok {
			fmt.Fprintln(os.Stderr, "bad line")
			os.Exit(1)
		}
		a, err1 := strconv.ParseInt(lo, 10, 64)
		b, err2 := strconv.ParseInt(hi, 10, 64)
		if err1 != nil || err2 != nil {
			fmt.Fprintln(os.Stderr, "bad number")
			os.Exit(1)
		}
		rows = append(rows, pair{a, b})
	}
	// Stable, so equal keys keep input order — the tie-break Sort By has.
	slices.SortStableFunc(rows, func(x, y pair) int {
		switch {
		case x.b > y.b:
			return -1
		case x.b < y.b:
			return 1
		}
		return 0
	})
	fmt.Println(rows[0].a)
}
