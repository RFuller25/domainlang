// Hand-written Go counterpart of sparse_life.domain: eight generations of
// Conway's Game of Life on an unbounded plane, counting the survivors.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type point struct{ r, c int64 }

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	live := make(map[point]bool)
	for sc.Scan() {
		a, b, ok := strings.Cut(sc.Text(), ",")
		if !ok {
			fmt.Fprintln(os.Stderr, "bad line")
			os.Exit(1)
		}
		r, err1 := strconv.ParseInt(a, 10, 64)
		c, err2 := strconv.ParseInt(b, 10, 64)
		if err1 != nil || err2 != nil {
			fmt.Fprintln(os.Stderr, "bad point")
			os.Exit(1)
		}
		live[point{r, c}] = true
	}

	// The same counting trick the Domain program uses, so the two are
	// running one algorithm and the benchmark is about the code generated
	// for it: a live cell scores 1 into each of its eight neighbours and 9
	// into itself, and the next generation is the cells scoring 3 (birth),
	// 11 or 12 (survival).
	for gen := 0; gen < 8; gen++ {
		score := make(map[point]int, len(live)*8)
		for p := range live {
			for dr := int64(-1); dr <= 1; dr++ {
				for dc := int64(-1); dc <= 1; dc++ {
					if dr == 0 && dc == 0 {
						continue
					}
					score[point{p.r + dr, p.c + dc}]++
				}
			}
			score[p] += 9
		}
		next := make(map[point]bool, len(live))
		for p, n := range score {
			if n == 3 || n == 11 || n == 12 {
				next[p] = true
			}
		}
		live = next
	}
	fmt.Println(len(live))
}
