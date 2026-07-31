// Hand-written Go counterpart of connected_components.domain: how many
// 4-connected regions of '#' the grid holds.
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
	seen := make([]bool, rows*cols)
	stack := make([]int, 0, rows*cols)
	count := 0
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if grid[r][c] != '#' || seen[r*cols+c] {
				continue
			}
			count++
			seen[r*cols+c] = true
			stack = append(stack[:0], r*cols+c)
			for len(stack) > 0 {
				p := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				pr, pc := p/cols, p%cols
				if pr > 0 && grid[pr-1][pc] == '#' && !seen[p-cols] {
					seen[p-cols] = true
					stack = append(stack, p-cols)
				}
				if pr < rows-1 && grid[pr+1][pc] == '#' && !seen[p+cols] {
					seen[p+cols] = true
					stack = append(stack, p+cols)
				}
				if pc > 0 && grid[pr][pc-1] == '#' && !seen[p-1] {
					seen[p-1] = true
					stack = append(stack, p-1)
				}
				if pc < cols-1 && grid[pr][pc+1] == '#' && !seen[p+1] {
					seen[p+1] = true
					stack = append(stack, p+1)
				}
			}
		}
	}
	fmt.Println(count)
}
