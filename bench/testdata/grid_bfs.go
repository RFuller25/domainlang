// Hand-written Go counterpart of grid_bfs.domain: how many walkable cells the
// breadth-first search from the top-left corner reaches, the start excluded.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var grid [][]byte
	for sc.Scan() {
		grid = append(grid, append([]byte(nil), sc.Bytes()...))
	}
	rows, cols := len(grid), len(grid[0])
	dist := make([]int32, rows*cols)
	for i := range dist {
		dist[i] = -1
	}
	dist[0] = 0
	queue := make([]int, 1, rows*cols)
	for head := 0; head < len(queue); head++ {
		p := queue[head]
		pr, pc := p/cols, p%cols
		step := func(q, qr, qc int) {
			if qr < 0 || qr >= rows || qc < 0 || qc >= cols {
				return
			}
			if grid[qr][qc] != '.' || dist[q] >= 0 {
				return
			}
			dist[q] = dist[p] + 1
			queue = append(queue, q)
		}
		step(p-cols, pr-1, pc)
		step(p+cols, pr+1, pc)
		step(p-1, pr, pc-1)
		step(p+1, pr, pc+1)
	}
	count := 0
	for _, d := range dist {
		if d > 0 {
			count++
		}
	}
	fmt.Println(count)
}
