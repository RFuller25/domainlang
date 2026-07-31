// Hand-written Go counterpart of dijkstra_grid.domain: cheapest path from the
// top-left to the bottom-right of a digit grid, where entering a cell costs
// that cell's digit.
package main

import (
	"bufio"
	"container/heap"
	"fmt"
	"os"
)

type node struct {
	cost int64
	r, c int
}

type pq []node

func (p pq) Len() int           { return len(p) }
func (p pq) Less(i, j int) bool { return p[i].cost < p[j].cost }
func (p pq) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p *pq) Push(x any)        { *p = append(*p, x.(node)) }
func (p *pq) Pop() any          { old := *p; n := len(old); v := old[n-1]; *p = old[:n-1]; return v }

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var grid [][]byte
	for sc.Scan() {
		line := sc.Bytes()
		row := make([]byte, len(line))
		copy(row, line)
		grid = append(grid, row)
	}
	rows, cols := len(grid), len(grid[0])

	const inf = int64(1) << 62
	dist := make([]int64, rows*cols)
	for i := range dist {
		dist[i] = inf
	}
	dist[0] = 0
	q := &pq{{0, 0, 0}}
	dr := [4]int{-1, 1, 0, 0}
	dc := [4]int{0, 0, -1, 1}
	for q.Len() > 0 {
		cur := heap.Pop(q).(node)
		if cur.r == rows-1 && cur.c == cols-1 {
			fmt.Println(cur.cost)
			return
		}
		if cur.cost > dist[cur.r*cols+cur.c] {
			continue
		}
		for k := 0; k < 4; k++ {
			nr, nc := cur.r+dr[k], cur.c+dc[k]
			if nr < 0 || nr >= rows || nc < 0 || nc >= cols {
				continue
			}
			next := cur.cost + int64(grid[nr][nc]-'0')
			if next < dist[nr*cols+nc] {
				dist[nr*cols+nc] = next
				heap.Push(q, node{next, nr, nc})
			}
		}
	}
	fmt.Println(-1)
}
