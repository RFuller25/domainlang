// Hand-written Go counterpart of explore_states.domain: how many distinct
// states the successor rule reaches from the seed.
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
	seed, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	seen := map[int64]bool{seed: true}
	queue := []int64{seed}
	for head := 0; head < len(queue); head++ {
		n := queue[head]
		var next []int64
		if n > 2000000 {
			next = []int64{n}
		} else {
			next = []int64{n * 2, n + 3}
		}
		for _, m := range next {
			if !seen[m] {
				seen[m] = true
				queue = append(queue, m)
			}
		}
	}
	fmt.Println(len(seen))
}
