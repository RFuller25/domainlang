// Hand-written Go counterpart of toposort_words.domain: Kahn's algorithm over
// the parsed edge list, printing the first node of the order. Nodes and
// successors are kept in first-seen order, which is the tie-break Domain's
// Topological Sort documents.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	index := make(map[string]int)
	var order []string // nodes in first-seen order
	var succ [][]int   // successors, in edge order
	var indeg []int

	id := func(name string) int {
		if i, ok := index[name]; ok {
			return i
		}
		i := len(order)
		index[name] = i
		order = append(order, name)
		succ = append(succ, nil)
		indeg = append(indeg, 0)
		return i
	}

	for sc.Scan() {
		from, to, ok := strings.Cut(sc.Text(), " -> ")
		if !ok {
			fmt.Fprintln(os.Stderr, "bad line")
			os.Exit(1)
		}
		f, t := id(from), id(to)
		succ[f] = append(succ[f], t)
		indeg[t]++
	}

	queue := make([]int, 0, len(order))
	for i := range order {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	var sorted []string
	for head := 0; head < len(queue); head++ {
		n := queue[head]
		sorted = append(sorted, order[n])
		for _, m := range succ[n] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	if len(sorted) != len(order) {
		fmt.Fprintln(os.Stderr, "cycle")
		os.Exit(1)
	}
	fmt.Println(sorted[0])
}
