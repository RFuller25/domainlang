// Hand-written Go counterpart of grid_transform.domain: transpose, quarter
// turn right, mirror, blank the diagonal, count what is left above 4.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var g [][]int64
	for sc.Scan() {
		line := sc.Bytes()
		row := make([]int64, len(line))
		for i, b := range line {
			row[i] = int64(b - '0')
		}
		g = append(g, row)
	}

	// Transpose.
	rows, cols := len(g), len(g[0])
	t := make([][]int64, cols)
	for c := 0; c < cols; c++ {
		t[c] = make([]int64, rows)
		for r := 0; r < rows; r++ {
			t[c][r] = g[r][c]
		}
	}
	g, rows, cols = t, cols, rows

	// Rotate right: (r, c) -> (c, rows-1-r).
	rot := make([][]int64, cols)
	for i := range rot {
		rot[i] = make([]int64, rows)
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			rot[c][rows-1-r] = g[r][c]
		}
	}
	g, rows, cols = rot, cols, rows

	// Flip horizontal.
	for r := 0; r < rows; r++ {
		for c := 0; c < cols/2; c++ {
			g[r][c], g[r][cols-1-c] = g[r][cols-1-c], g[r][c]
		}
	}

	// Map Cells over (grid, row, col), reading the grid it was handed.
	count := 0
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			v := g[r][c]
			if r == c {
				v = 0
			}
			if v > 4 {
				count++
			}
		}
	}
	fmt.Println(count)
}
