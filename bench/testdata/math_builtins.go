// Hand-written Go counterpart of math_builtins.domain: the same three
// number-theory functions per element, with Domain's semantics (non-negative
// gcd, floor integer square root, modpow landing in [0, m)).
package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
)

func gcd(a, b int64) int64 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func isqrt(n int64) int64 {
	if n < 0 {
		fmt.Fprintln(os.Stderr, "isqrt of a negative number")
		os.Exit(1)
	}
	r := int64(math.Sqrt(float64(n)))
	for r > 0 && r*r > n {
		r--
	}
	for (r+1)*(r+1) <= n {
		r++
	}
	return r
}

func modpow(b, e, m int64) int64 {
	if m <= 0 || e < 0 {
		fmt.Fprintln(os.Stderr, "modpow: bad arguments")
		os.Exit(1)
	}
	b %= m
	if b < 0 {
		b += m
	}
	res := int64(1) % m
	for e > 0 {
		if e&1 == 1 {
			res = res * b % m
		}
		b = b * b % m
		e >>= 1
	}
	return res
}

func main() {
	sc := bufio.NewScanner(bufio.NewReaderSize(os.Stdin, 1<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var total int64
	for sc.Scan() {
		n, err := strconv.ParseInt(sc.Text(), 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		total += gcd(n, 360) + isqrt(n) + modpow(n, 3, 1000007)
	}
	fmt.Println(total)
}
