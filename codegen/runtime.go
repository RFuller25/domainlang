package codegen

// The runtime snippets below are emitted into the generated program on
// demand (see gen.helper). They must stay dependency-free beyond the stdlib
// imports registered at their call sites, and their observable behavior must
// match the interpreter primitives they mirror.

const declFail = `func dmFail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "domain: "+format+"\n", args...)
	os.Exit(1)
}`

// declReadSource mirrors prims.readSourceData with the working directory as
// the base: read the named file, falling back to stdin when it does not exist
// (so 'day1 < input.txt' works like 'domain run day1.domain < input.txt').
const declReadSource = `func dmReadInto(r io.Reader, sizeHint int64) (string, error) {
	// Accumulate directly into a strings.Builder: with the size known (a
	// redirected regular file), Grow reserves the exact backing array once and
	// Builder.String() returns it with no final copy. This avoids the classic
	// io.ReadAll([]byte) + string(data) pattern, whose second full-input copy
	// doubles live heap and stalls the GC on large inputs. TrimRight below is a
	// substring, not a copy.
	var b strings.Builder
	if sizeHint > 0 {
		b.Grow(int(sizeHint))
	}
	if _, err := io.Copy(&b, r); err != nil {
		return "", err
	}
	return b.String(), nil
}

func dmStdinSize() int64 {
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode().IsRegular() {
		return fi.Size()
	}
	return 0
}

func dmReadSource(target string) string {
	var s string
	var err error
	if target == "" || strings.EqualFold(target, "stdin") {
		s, err = dmReadInto(os.Stdin, dmStdinSize())
	} else if f, oerr := os.Open(target); oerr == nil {
		var size int64
		if fi, e := f.Stat(); e == nil {
			size = fi.Size()
		}
		s, err = dmReadInto(f, size)
		f.Close()
	} else if os.IsNotExist(oerr) {
		s, err = dmReadInto(os.Stdin, dmStdinSize())
	} else {
		err = oerr
	}
	if err != nil {
		dmFail("Read Source: %v", err)
	}
	return strings.TrimRight(s, "\r\n")
}`

const declParseInt = `func dmParseInt(s string) int64 {
	t := strings.TrimSpace(s)
	// Fast path: a plain decimal that provably fits in int64 (<=18 digits)
	// parses inline; anything else defers to strconv for exact semantics.
	if n := len(t); n > 0 && n <= 19 {
		i, neg := 0, false
		if t[0] == '-' || t[0] == '+' {
			neg = t[0] == '-'
			i = 1
		}
		if d := n - i; d >= 1 && d <= 18 {
			var v int64
			ok := true
			for ; i < n; i++ {
				c := t[i]
				if c < '0' || c > '9' {
					ok = false
					break
				}
				v = v*10 + int64(c-'0')
			}
			if ok {
				if neg {
					return -v
				}
				return v
			}
		}
	}
	m, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		dmFail("%q is not an integer", s)
	}
	return m
}`

const declDiv = `func dmDiv[T int64 | float64](a, b T) T {
	if b == 0 {
		dmFail("division by zero")
	}
	return a / b
}`

const declParseFloat = `func dmParseFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		dmFail("%q is not a number", s)
	}
	return f
}`

const declFmtFloat = `func dmFmtFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}`

// dmLabel mirrors ir.LabelledOutput: Reveal inside a Part block prefixes its
// output with the Part's label, putting a multi-line value (a grid, a sparse
// picture) on the lines after the label instead of beside it. The interpreter
// and this helper must agree byte for byte; a Part oracle test pins that.
const declLabel = `func dmLabel(label, s string) string {
	if label == "" {
		return s
	}
	if strings.Contains(s, "\n") {
		return "Part " + label + ":\n" + s
	}
	return "Part " + label + ": " + s
}`

const declSqrt = `func dmSqrt(f float64) float64 {
	if f < 0 {
		dmFail("sqrt of a negative number (%s)", dmFmtFloat(f))
	}
	return math.Sqrt(f)
}`

// declTopK is the compiled counterpart of optimizer.TopK: quickselect the k
// front-most elements, then sort just those k.
const declTopK = `func dmTopK(xs []int64, k int, desc bool) []int64 {
	if k <= 0 || len(xs) == 0 {
		return []int64{}
	}
	if k > len(xs) {
		k = len(xs)
	}
	a := append([]int64(nil), xs...)
	front := func(x, y int64) bool {
		if desc {
			return x > y
		}
		return x < y
	}
	lo, hi := 0, len(a)-1
	for lo < hi {
		mid := lo + (hi-lo)/2
		a[mid], a[hi] = a[hi], a[mid]
		pivot := a[hi]
		i := lo
		for j := lo; j < hi; j++ {
			if front(a[j], pivot) {
				a[i], a[j] = a[j], a[i]
				i++
			}
		}
		a[i], a[hi] = a[hi], a[i]
		switch {
		case i == k-1:
			lo, hi = 0, -1 // done
		case i < k-1:
			lo = i + 1
		default:
			hi = i - 1
		}
	}
	res := a[:k]
	sort.Slice(res, func(i, j int) bool { return front(res[i], res[j]) })
	return res
}`

// declSelectItem returns the kth order statistic (ascending, or descending when
// desc) via an in-place Hoare quickselect on a copy of the input — no sort. The
// kth order statistic is a unique value regardless of partition scheme or ties,
// so the result is identical to dmTopK(xs, k+1, desc)[k] without materializing
// and sorting the whole k-front.
const declSelectItem = `func dmSelectItem(xs []int64, k int, desc bool) int64 {
	a := append([]int64(nil), xs...)
	less := func(x, y int64) bool {
		if desc {
			return x > y
		}
		return x < y
	}
	lo, hi := 0, len(a)-1
	for lo < hi {
		pivot := a[lo+(hi-lo)/2]
		i, j := lo, hi
		for i <= j {
			for less(a[i], pivot) {
				i++
			}
			for less(pivot, a[j]) {
				j--
			}
			if i <= j {
				a[i], a[j] = a[j], a[i]
				i++
				j--
			}
		}
		if k <= j {
			hi = j
		} else if k >= i {
			lo = i
		} else {
			break
		}
	}
	return a[k]
}`

// declTripleCount mirrors optimizer.CountTripleSum: index triples i<j<k
// summing to target, counted in O(n²) via a prefix pair-sum multiset.
const declTripleCount = `func dmTripleCount(xs []int64, target int64) int64 {
	pairSums := make(map[int64]int64)
	var count int64
	for k := 0; k < len(xs); k++ {
		count += pairSums[target-xs[k]]
		for i := 0; i < k; i++ {
			pairSums[xs[i]+xs[k]]++
		}
	}
	return count
}`

// declTripleFirst mirrors optimizer.FindTripleSum: the lexicographically-
// first index triple i<j<k summing to target.
const declTripleFirst = `func dmTripleFirst(xs []int64, target int64) ([]int64, bool) {
	// last[v] is the highest index where v occurs. A value 'need' appears at
	// some k>j exactly when last[need] > j, which is the same condition the
	// per-i multiset expressed -- so this returns the identical first triple in
	// O(n) preprocessing + O(n^2) O(1)-lookups instead of rebuilding a map per i.
	last := make(map[int64]int, len(xs))
	for idx, v := range xs {
		last[v] = idx
	}
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			need := target - xs[i] - xs[j]
			if k, ok := last[need]; ok && k > j {
				return []int64{xs[i], xs[j], need}, true
			}
		}
	}
	return nil, false
}`

// declDiffCount mirrors optimizer.CountPairDiff: index pairs i<j whose
// difference hits target (xs[j]-xs[i] when flipped).
const declDiffCount = `func dmDiffCount(xs []int64, target int64, flipped bool) int64 {
	seen := make(map[int64]int64, len(xs))
	var count int64
	for _, x := range xs {
		if flipped {
			count += seen[x-target]
		} else {
			count += seen[x+target]
		}
		seen[x]++
	}
	return count
}`

// declDiffFirst mirrors optimizer.FindPairDiff: the lexicographically-first
// index pair i<j whose difference hits target.
const declDiffFirst = `func dmDiffFirst(xs []int64, target int64, flipped bool) ([]int64, bool) {
	remaining := make(map[int64]int, len(xs))
	for _, x := range xs {
		remaining[x]++
	}
	for i := 0; i < len(xs); i++ {
		remaining[xs[i]]--
		need := xs[i] - target
		if flipped {
			need = xs[i] + target
		}
		if remaining[need] > 0 {
			return []int64{xs[i], need}, true
		}
	}
	return nil, false
}`

// declProductCount mirrors optimizer.CountPairProduct: index pairs i<j whose
// product hits target, with zeros handled outside the division shortcut.
const declProductCount = `func dmProductCount(xs []int64, target int64) int64 {
	seen := make(map[int64]int64, len(xs))
	var count, earlier int64
	for _, x := range xs {
		if x == 0 {
			if target == 0 {
				count += earlier
			}
		} else if target%x == 0 {
			count += seen[target/x]
		}
		seen[x]++
		earlier++
	}
	return count
}`

// declProductFirst mirrors optimizer.FindPairProduct: the lexicographically-
// first index pair i<j whose product hits target.
const declProductFirst = `func dmProductFirst(xs []int64, target int64) ([]int64, bool) {
	remaining := make(map[int64]int, len(xs))
	for _, x := range xs {
		remaining[x]++
	}
	for i := 0; i < len(xs); i++ {
		x := xs[i]
		remaining[x]--
		if x == 0 {
			if target == 0 && i+1 < len(xs) {
				return []int64{0, xs[i+1]}, true
			}
			continue
		}
		if target%x == 0 && remaining[target/x] > 0 {
			return []int64{x, target / x}, true
		}
	}
	return nil, false
}`

// declSlidingExtremum mirrors optimizer.WindowedExtrema: the max/min of every
// fully-contained window via a monotonic deque, O(n) for any window size.
const declSlidingExtremum = `func dmSlidingExtremum(xs []int64, size, step int64, min bool) []int64 {
	beats := func(a, b int64) bool {
		if min {
			return a < b
		}
		return a > b
	}
	out := []int64{}
	deque := []int64{}
	next := int64(0)
	for s := int64(0); s+size <= int64(len(xs)); s += step {
		for ; next < s+size; next++ {
			for len(deque) > 0 && !beats(xs[deque[len(deque)-1]], xs[next]) {
				deque = deque[:len(deque)-1]
			}
			deque = append(deque, next)
		}
		for deque[0] < s {
			deque = deque[1:]
		}
		out = append(out, xs[deque[0]])
	}
	return out
}`

// declSlidingExtremumSum is the WindowedReduce(max/min) + Sum fusion: it sums
// every window's extremum without materializing the per-window slice.
const declSlidingExtremumSum = `func dmSlidingExtremumSum(xs []int64, size, step int64, min bool) int64 {
	beats := func(a, b int64) bool {
		if min {
			return a < b
		}
		return a > b
	}
	var total int64
	deque := []int64{}
	next := int64(0)
	for s := int64(0); s+size <= int64(len(xs)); s += step {
		for ; next < s+size; next++ {
			for len(deque) > 0 && !beats(xs[deque[len(deque)-1]], xs[next]) {
				deque = deque[:len(deque)-1]
			}
			deque = append(deque, next)
		}
		for deque[0] < s {
			deque = deque[1:]
		}
		total += xs[deque[0]]
	}
	return total
}`

// declParseFieldsInt is the Split Fields + Convert To Integers fusion's fast
// path: parse a line's whitespace-separated signed decimals directly. Returns
// ok=false (so the caller falls back to strings.Fields + dmParseInt with
// identical results) for any line that is not pure ASCII or holds a field that
// is not a clean <=18-digit integer.
const declParseFieldsInt = `func dmASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\v' || b == '\f' || b == '\r'
}

func dmParseFieldsInt(line string) ([]int64, bool) {
	return dmParseFieldsIntInto(line, nil)
}

func dmParseFieldsIntInto(line string, out []int64) ([]int64, bool) {
	// A non-ASCII byte can only appear as a field terminator or a field's first
	// byte; both drive the not-ok (fallback) return below, so no separate
	// pure-ASCII pre-scan is needed — one pass suffices.
	out = out[:0]
	i, n := 0, len(line)
	for i < n {
		for i < n && dmASCIISpace(line[i]) {
			i++
		}
		if i >= n {
			break
		}
		neg := false
		if line[i] == '+' || line[i] == '-' {
			neg = line[i] == '-'
			i++
		}
		var v int64
		digits := 0
		for i < n && line[i] >= '0' && line[i] <= '9' {
			v = v*10 + int64(line[i]-'0')
			i++
			digits++
		}
		if digits == 0 || digits > 18 || (i < n && !dmASCIISpace(line[i])) {
			return out, false
		}
		if neg {
			v = -v
		}
		out = append(out, v)
	}
	return out, true
}`

// declParseIntSeg parses one already-delimited segment: a no-whitespace signed
// decimal fitting in int64 parses inline (no strings.TrimSpace, no strconv);
// anything else defers to dmParseInt for byte-identical values and errors.
const declParseIntSeg = `func dmParseIntSeg(s string) int64 {
	n := len(s)
	if n >= 1 && n <= 19 {
		i, neg := 0, false
		if s[0] == '-' || s[0] == '+' {
			neg = s[0] == '-'
			i = 1
		}
		if d := n - i; d >= 1 && d <= 18 {
			var v int64
			ok := true
			for ; i < n; i++ {
				c := s[i]
				if c < '0' || c > '9' {
					ok = false
					break
				}
				v = v*10 + int64(c-'0')
			}
			if ok {
				if neg {
					return -v
				}
				return v
			}
		}
	}
	return dmParseInt(s)
}`

const declGrid = `type dmGrid[T any] struct {
	rows, cols int
	cells      []T
}`

// declSparse mirrors ir.SparseValue: an unbounded plane of set cells with a
// default value and exact (grow-only) bounds. pts() is the canonical sorted
// row-major iteration order — rendering and the cell primitives must visit
// cells in the same order the interpreter does.
const declSparse = `type dmSPt struct {
	r, c int64
}

type dmSparse[T any] struct {
	def                    T
	cells                  map[dmSPt]T
	minR, maxR, minC, maxC int64
}

func dmNewSparse[T any](def T) dmSparse[T] {
	return dmSparse[T]{def: def, cells: map[dmSPt]T{}}
}

func (s *dmSparse[T]) put(r, c int64, v T) {
	if len(s.cells) == 0 {
		s.minR, s.maxR, s.minC, s.maxC = r, r, c, c
	} else {
		if r < s.minR {
			s.minR = r
		}
		if r > s.maxR {
			s.maxR = r
		}
		if c < s.minC {
			s.minC = c
		}
		if c > s.maxC {
			s.maxC = c
		}
	}
	s.cells[dmSPt{r, c}] = v
}

func (s dmSparse[T]) at(r, c int64) T {
	if v, ok := s.cells[dmSPt{r, c}]; ok {
		return v
	}
	return s.def
}

func (s dmSparse[T]) has(r, c int64) bool {
	_, ok := s.cells[dmSPt{r, c}]
	return ok
}

func (s dmSparse[T]) pts() []dmSPt {
	out := make([]dmSPt, 0, len(s.cells))
	for k := range s.cells {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].r != out[j].r {
			return out[i].r < out[j].r
		}
		return out[i].c < out[j].c
	})
	return out
}`

const declSparsePut = `func dmSparsePut[T any](s dmSparse[T], r, c int64, v T) dmSparse[T] {
	out := dmSparse[T]{def: s.def, cells: make(map[dmSPt]T, len(s.cells)+1),
		minR: s.minR, maxR: s.maxR, minC: s.minC, maxC: s.maxC}
	for k, e := range s.cells {
		out.cells[k] = e
	}
	out.put(r, c, v)
	return out
}`

const declSparseBound = `func dmSparseBound[T any](s dmSparse[T], which string) int64 {
	if len(s.cells) == 0 {
		dmFail("%s of an empty sparse grid is undefined", which)
	}
	switch which {
	case "minrow":
		return s.minR
	case "maxrow":
		return s.maxR
	case "mincol":
		return s.minC
	}
	return s.maxC
}`

// Expression-layer builtins (typecheck.Builtins). Generic, so one
// declaration serves every element type; partial ones share dmFail and use
// the same message wording as the interpreter in eval.evalCall.

const declItem = `func dmItem[T any](xs []T, i int64) T {
	if i < 0 || i >= int64(len(xs)) {
		dmFail("item: index %d out of range (length %d)", i, len(xs))
	}
	return xs[i]
}`

const declTake = `func dmTake[T any](xs []T, n int64) []T {
	if n < 0 {
		n = 0
	}
	if n > int64(len(xs)) {
		n = int64(len(xs))
	}
	return xs[:n]
}`

const declDrop = `func dmDrop[T any](xs []T, n int64) []T {
	if n < 0 {
		n = 0
	}
	if n > int64(len(xs)) {
		n = int64(len(xs))
	}
	return xs[n:]
}`

const declRev = `func dmRev[T any](xs []T) []T {
	out := make([]T, len(xs))
	for i, e := range xs {
		out[len(xs)-1-i] = e
	}
	return out
}`

const declConcat = `func dmConcat[T any](a, b []T) []T {
	out := make([]T, 0, len(a)+len(b))
	out = append(out, a...)
	return append(out, b...)
}`

const declFirst = `func dmFirst[T any](xs []T) T {
	if len(xs) == 0 {
		dmFail("first of an empty list is undefined")
	}
	return xs[0]
}`

const declLast = `func dmLast[T any](xs []T) T {
	if len(xs) == 0 {
		dmFail("last of an empty list is undefined")
	}
	return xs[len(xs)-1]
}`

const declSumInts = `func dmSum[T int64 | float64](xs []T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}`

const declMinInts = `func dmMin[T int64 | float64](xs []T) T {
	if len(xs) == 0 {
		dmFail("min of an empty list is undefined")
	}
	acc := xs[0]
	for _, x := range xs[1:] {
		if x < acc {
			acc = x
		}
	}
	return acc
}`

// declMax2 / declMin2 are the scalar forms emitted when max/min is applied to a
// list *literal* (list(a, b, ...)), so no slice is allocated and no loop runs.
const declMax2 = `func dmMax2[T int64 | float64](a, b T) T {
	if a > b {
		return a
	}
	return b
}`

const declMin2 = `func dmMin2[T int64 | float64](a, b T) T {
	if a < b {
		return a
	}
	return b
}`

const declMaxInts = `func dmMax[T int64 | float64](xs []T) T {
	if len(xs) == 0 {
		dmFail("max of an empty list is undefined")
	}
	acc := xs[0]
	for _, x := range xs[1:] {
		if x > acc {
			acc = x
		}
	}
	return acc
}`

const declContains = `func dmContains[T comparable](xs []T, v T) bool {
	for _, e := range xs {
		if e == v {
			return true
		}
	}
	return false
}`

const declAbs = `func dmAbs[T int64 | float64](n T) T {
	if n < 0 {
		return -n
	}
	return n
}`

// dmMod mirrors eval.euclidMod exactly, including the failure wording: the
// result is non-negative for a positive modulus whatever the sign of a.
const declMod = `func dmMod(a, b int64) int64 {
	if b == 0 {
		dmFail("mod by zero")
	}
	r := a % b
	if r != 0 && (r < 0) != (b < 0) {
		r += b
	}
	return r
}`

const declPow = `func dmPow(b, e int64) int64 {
	if e < 0 {
		dmFail("pow: exponent must be non-negative, got %d", e)
	}
	r := int64(1)
	for e > 0 {
		if e&1 == 1 {
			r *= b
		}
		b *= b
		e >>= 1
	}
	return r
}`

const declISqrt = `func dmISqrt(x int64) int64 {
	if x < 0 {
		dmFail("isqrt: negative input %d", x)
	}
	if x < 2 {
		return x
	}
	n := x
	g := x/2 + 1
	for g < n {
		n = g
		g = (g + x/g) / 2
	}
	return n
}`

const declFactorial = `func dmFactorial(n int64) int64 {
	if n < 0 {
		dmFail("factorial: negative input %d", n)
	}
	if n > 20 {
		dmFail("factorial: %d! overflows Int (max is 20!)", n)
	}
	r := int64(1)
	for i := int64(2); i <= n; i++ {
		r *= i
	}
	return r
}`

const declChoose = `func dmChoose(n, k int64) int64 {
	if n < 0 {
		dmFail("choose: negative n %d", n)
	}
	if k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k
	}
	r := int64(1)
	for i := int64(1); i <= k; i++ {
		r = r * (n - k + i) / i
	}
	return r
}`

const declClamp = `func dmClamp[T int64 | float64](v, lo, hi T) T {
	if lo > hi {
		dmFail("clamp: low bound %v exceeds high bound %v", lo, hi)
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}`

const declReverseText = `func dmReverseText(s string) string {
	rs := []rune(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}`

const declChars = `func dmChars(s string) []string {
	rs := []rune(s)
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}`

// dmIndexOfText answers in runes, matching dmCharAt and dmSliceText, so every
// text position in a compiled binary means the same thing it does under the
// interpreter.
const declIndexOfText = `func dmIndexOfText(s, sub string) int64 {
	b := strings.Index(s, sub)
	if b < 0 {
		return -1
	}
	return int64(utf8.RuneCountInString(s[:b]))
}`

const declIndexOf = `func dmIndexOf[T comparable](xs []T, v T) int64 {
	for i, e := range xs {
		if e == v {
			return int64(i)
		}
	}
	return -1
}`

const declCharAt = `func dmCharAt(s string, i int64) string {
	rs := []rune(s)
	if i < 0 || i >= int64(len(rs)) {
		dmFail("charat: index %d out of range (length %d)", i, len(rs))
	}
	return string(rs[i])
}`

const declClampRange = `func dmClampRange(lo, hi, n int64) (int64, int64) {
	if lo < 0 {
		lo = 0
	}
	if hi > n {
		hi = n
	}
	if hi < lo {
		hi = lo
	}
	if lo > n {
		lo, hi = n, n
	}
	return lo, hi
}`

const declSliceText = `func dmSliceText(s string, lo, hi int64) string {
	rs := []rune(s)
	l, h := dmClampRange(lo, hi, int64(len(rs)))
	return string(rs[l:h])
}`

const declSliceList = `func dmSliceList[T any](xs []T, lo, hi int64) []T {
	l, h := dmClampRange(lo, hi, int64(len(xs)))
	out := make([]T, h-l)
	copy(out, xs[l:h])
	return out
}`

const declSign = `func dmSign(n int64) int64 {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}`

const declGcd = `func dmGcd(a, b int64) int64 {
	a, b = dmAbs(a), dmAbs(b)
	for b != 0 {
		a, b = b, a%b
	}
	return a
}`

const declLcm = `func dmLcm(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	return dmAbs(a / dmGcd(a, b) * b)
}`

const declModPow = `func dmModPow(base, exp, m int64) int64 {
	if m <= 0 {
		dmFail("modpow: modulus must be positive, got %d", m)
	}
	if exp < 0 {
		dmFail("modpow: exponent must be non-negative, got %d", exp)
	}
	result := int64(1) % m
	base = ((base % m) + m) % m
	for exp > 0 {
		if exp&1 == 1 {
			result = result * base % m
		}
		base = base * base % m
		exp >>= 1
	}
	return result
}`

const declModInv = `func dmModInv(a, m int64) int64 {
	if m <= 0 {
		dmFail("modinv: modulus must be positive, got %d", m)
	}
	a = ((a % m) + m) % m
	r0, r1 := a, m
	x0, x1 := int64(1), int64(0)
	for r1 != 0 {
		q := r0 / r1
		r0, r1 = r1, r0-q*r1
		x0, x1 = x1, x0-q*x1
	}
	if r0 != 1 {
		dmFail("modinv: %d has no inverse modulo %d (not coprime)", a, m)
	}
	return ((x0 % m) + m) % m
}`

const declToInt = `func dmToInt(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		dmFail("toint: %q is not an integer", s)
	}
	return n
}`

const declRepeats = `func dmRepeats(s string) bool {
	if len(s) < 2 {
		return false
	}
	return strings.Contains((s + s)[1:2*len(s)-1], s)
}`

const declInBounds = `func dmInBounds[T any](g dmGrid[T], r, c int64) bool {
	return r >= 0 && r < int64(g.rows) && c >= 0 && c < int64(g.cols)
}`

const declSetHas = `func dmSetHas[T comparable](s dmSet[T], v T) bool {
	_, ok := s.has[v]
	return ok
}`

const declSetAt = `func dmSetAt[T any](xs []T, i int64, v T) []T {
	if i < 0 || i >= int64(len(xs)) {
		dmFail("set: index %d out of range (length %d)", i, len(xs))
	}
	out := append([]T(nil), xs...)
	out[i] = v
	return out
}`

const declGridRow = `func dmGridRow[T any](g dmGrid[T], r int64) []T {
	if r < 0 || r >= int64(g.rows) {
		dmFail("row: row %d out of range (grid %dx%d)", r, g.rows, g.cols)
	}
	lo, hi := r*int64(g.cols), (r+1)*int64(g.cols)
	// Rows are read-only under Domain's immutable value model, so alias the
	// backing store instead of copying; the capped cap makes any append on the
	// result reallocate rather than clobber the next row.
	return g.cells[lo:hi:hi]
}`

const declShl = `func dmShl(a, n int64) int64 {
	if n < 0 {
		dmFail("shl: negative shift count %d", n)
	}
	return a << n
}`

const declShr = `func dmShr(a, n int64) int64 {
	if n < 0 {
		dmFail("shr: negative shift count %d", n)
	}
	return a >> n
}`

const declFromBin = `func dmFromBin(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 2, 64)
	if err != nil {
		dmFail("frombin: %q is not a binary number", s)
	}
	return n
}`

const declGridCol = `func dmGridCol[T any](g dmGrid[T], c int64) []T {
	if c < 0 || c >= int64(g.cols) {
		dmFail("col: column %d out of range (grid %dx%d)", c, g.rows, g.cols)
	}
	out := make([]T, g.rows)
	for r := 0; r < g.rows; r++ {
		out[r] = g.cells[int64(r)*int64(g.cols)+c]
	}
	return out
}`

const declMapGet = `func dmMapGet[K comparable, V any](m dmMap[K, V], k K) V {
	v, ok := m.vals[k]
	if !ok {
		dmFail("get: map has no key %v", k)
	}
	return v
}`

// declMaxAtLeast / declMaxAbove short-circuit `max(xs) >= s` / `max(xs) > s`
// without scanning past the first satisfying element. They fail on an empty
// slice exactly as dmMax does, so the fusion in tryMaxCompare preserves the
// "max of an empty list is undefined" semantics under the same guards.
const declMaxAtLeast = `func dmMaxAtLeast[T int64 | float64](xs []T, s T) bool {
	if len(xs) == 0 {
		dmFail("max of an empty list is undefined")
	}
	for _, x := range xs {
		if x >= s {
			return true
		}
	}
	return false
}`

const declMaxAbove = `func dmMaxAbove[T int64 | float64](xs []T, s T) bool {
	if len(xs) == 0 {
		dmFail("max of an empty list is undefined")
	}
	for _, x := range xs {
		if x > s {
			return true
		}
	}
	return false
}`

// declColMaxAtLeast / declColMaxAbove are the column-strided counterparts:
// they scan a take(col,n) / drop(col,n) range directly over the grid backing
// store, so the visibility idiom never materializes a column.
const declColMaxAtLeast = `func dmColMaxAtLeast[T int64 | float64](g dmGrid[T], c, n int64, drop bool, s T) bool {
	if c < 0 || c >= int64(g.cols) {
		dmFail("col: column %d out of range (grid %dx%d)", c, g.rows, g.cols)
	}
	if n < 0 {
		n = 0
	}
	if n > int64(g.rows) {
		n = int64(g.rows)
	}
	lo, hi := int64(0), int64(g.rows)
	if drop {
		lo = n
	} else {
		hi = n
	}
	if lo >= hi {
		dmFail("max of an empty list is undefined")
	}
	for r := lo; r < hi; r++ {
		if g.cells[r*int64(g.cols)+c] >= s {
			return true
		}
	}
	return false
}`

const declColMaxAbove = `func dmColMaxAbove[T int64 | float64](g dmGrid[T], c, n int64, drop bool, s T) bool {
	if c < 0 || c >= int64(g.cols) {
		dmFail("col: column %d out of range (grid %dx%d)", c, g.rows, g.cols)
	}
	if n < 0 {
		n = 0
	}
	if n > int64(g.rows) {
		n = int64(g.rows)
	}
	lo, hi := int64(0), int64(g.rows)
	if drop {
		lo = n
	} else {
		hi = n
	}
	if lo >= hi {
		dmFail("max of an empty list is undefined")
	}
	for r := lo; r < hi; r++ {
		if g.cells[r*int64(g.cols)+c] > s {
			return true
		}
	}
	return false
}`

const declGridAt = `func dmGridAt[T any](g dmGrid[T], r, c int64) T {
	if r < 0 || r >= int64(g.rows) || c < 0 || c >= int64(g.cols) {
		dmFail("at: position (%d, %d) out of range (grid %dx%d)", r, c, g.rows, g.cols)
	}
	return g.cells[r*int64(g.cols)+c]
}`

// declMap and declSet mirror ir.MapValue / ir.SetValue: a Go map for lookup
// paired with an order slice, because the interpreter renders (and therefore
// the oracle tests compare) collections in insertion order.
const declMap = `type dmMap[K comparable, V any] struct {
	keys []K
	vals map[K]V
}

func dmNewMap[K comparable, V any]() dmMap[K, V] {
	return dmMap[K, V]{vals: map[K]V{}}
}

func (m *dmMap[K, V]) put(k K, v V) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}`

const declSet = `type dmSet[T comparable] struct {
	elems []T
	has   map[T]struct{}
}

func dmNewSet[T comparable]() dmSet[T] {
	return dmSet[T]{has: map[T]struct{}{}}
}

func (s *dmSet[T]) add(v T) {
	if _, ok := s.has[v]; !ok {
		s.has[v] = struct{}{}
		s.elems = append(s.elems, v)
	}
}

func (s *dmSet[T]) contains(v T) bool {
	_, ok := s.has[v]
	return ok
}`
