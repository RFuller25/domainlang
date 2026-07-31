// Hand-written Go counterpart of iterate_unfold.domain: the trajectory of a
// Lehmer generator, folded, then halved back down to 1.
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
	const steps = 2_000_000
	traj := make([]int64, 0, steps)
	for i := 0; i < steps; i++ {
		x = (x * 48271) % 2147483647
		traj = append(traj, x)
	}
	var sum int64
	for _, v := range traj {
		sum += v % 7
	}
	count := 0
	for v := sum; v > 1; v /= 2 {
		count++
	}
	fmt.Println(count)
}
