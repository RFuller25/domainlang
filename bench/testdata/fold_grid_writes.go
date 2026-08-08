// Hand-written Go counterpart of fold_grid_writes.domain: read a digit grid,
// write -1 into rows*cols strided cells, then count the negative cells.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var cells []int64
	rows, cols := 0, 0
	for sc.Scan() {
		line := sc.Text()
		if rows == 0 {
			cols = len(line)
		}
		rows++
		for i := 0; i < len(line); i++ {
			cells = append(cells, int64(line[i]-'0'))
		}
	}
	for i := 0; i < rows*cols; i++ {
		cells[(i*7%rows)*cols+i*13%cols] = -1
	}
	var n int64
	for _, c := range cells {
		if c < 0 {
			n++
		}
	}
	fmt.Println(n)
}
