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
	slices.SortFunc(out, func(a, b dmSPt) int {
		if a.r != b.r {
			if a.r < b.r {
				return -1
			}
			return 1
		}
		if a.c < b.c {
			return -1
		}
		if a.c > b.c {
			return 1
		}
		return 0
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

// The one unsigned comparison is the standard form of `i < 0 || i >= len(xs)`:
// a negative i converts to a very large unsigned value and fails the same test,
// so one branch does the work of two. Worth about 1.6% on an indexing-bound
// loop, which is small — the measured cost is having a check at all, not how it
// is written, and see bench/README.md for what removing it would take.
const declItem = `func dmItem[T any](xs []T, i int64) T {
	if uint64(i) >= uint64(len(xs)) {
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

// dmModNZ mirrors eval.euclidMod for a divisor already known not to be zero:
// the result is non-negative for a positive modulus whatever the sign of a.
//
// It is split out from dmMod because of its size. Go inlines a function whose
// body costs at most 80 points; the guard below is a call, and a call alone is
// 57, which puts the guarded version out of reach. That costs more than the
// call it leaves behind: a modulus the caller wrote as a constant is invisible
// inside a function that is never inlined, so the compiler emits a hardware
// divide every lap where it could have emitted a multiply. Unguarded, this
// costs 22 and folds into its caller.
const declModNZ = `func dmModNZ(a, b int64) int64 {
	r := a % b
	if r != 0 && (r < 0) != (b < 0) {
		r += b
	}
	return r
}`

// dmMod is dmModNZ with the guard eval performs, including its failure
// wording. Emitted where the divisor is not a literal, so zero is possible.
const declMod = `func dmMod(a, b int64) int64 {
	if b == 0 {
		dmFail("mod by zero")
	}
	return dmModNZ(a, b)
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

// dmASCII reports whether every byte of s is one rune, which is the case for
// essentially all puzzle input. The rune-indexed text builtins below check it
// first: when it holds, a position in runes is a position in bytes and the
// answer is a substring of s — no []rune conversion, no allocation. The scan
// costs one pass over a line, against two allocations and a full decode.
const declASCII = `func dmASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}`

const declCharAt = `func dmCharAt(s string, i int64) string {
	if dmASCII(s) {
		if i < 0 || i >= int64(len(s)) {
			dmFail("charat: index %d out of range (length %d)", i, len(s))
		}
		return s[i : i+1]
	}
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
	if dmASCII(s) {
		l, h := dmClampRange(lo, hi, int64(len(s)))
		return s[l:h]
	}
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
	if uint64(i) >= uint64(len(xs)) {
		dmFail("set: index %d out of range (length %d)", i, len(xs))
	}
	out := append([]T(nil), xs...)
	out[i] = v
	return out
}`

// declSetAtIn is dmSetAt minus the copy: the functional form's own body with
// the clone removed, which is what makes the two agree by inspection. The
// bounds check stays — it is the program's semantics, not the copy's.
const declSetAtIn = `func dmSetAtIn[T any](xs []T, i int64, v T) []T {
	if uint64(i) >= uint64(len(xs)) {
		dmFail("set: index %d out of range (length %d)", i, len(xs))
	}
	xs[i] = v
	return xs
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

// declMapBump is the counting path, and counting is the hottest thing anyone
// does to a Map: put(k, vals[k]+1) probes three times (read, membership test,
// store) where one does. A compound assignment on a missing key starts from
// the zero value, so it needs no membership test, and the length delta says
// whether the key was new without asking the map again.
const declMapBump = `func dmBump[K comparable](m *dmMap[K, int64], k K, d int64) {
	n := len(m.vals)
	m.vals[k] += d
	if len(m.vals) != n {
		m.keys = append(m.keys, k)
	}
}`

// declMapAppend is declMapBump's sibling for a Map whose values are lists:
// put(k, append(vals[k], v)) reads, probes and stores where two probes do.
const declMapAppend = `func dmAppend[K comparable, V any](m *dmMap[K, []V], k K, v V) {
	if cur, ok := m.vals[k]; ok {
		m.vals[k] = append(cur, v)
		return
	}
	m.keys = append(m.keys, k)
	m.vals[k] = []V{v}
}`

// declGraph mirrors ir.GraphValue: a directed, Int-weighted adjacency over
// keyable nodes, insertion-ordered, with arcs held as indices into the node
// list. Every semantic decision documented on the interpreter's type applies
// here unchanged — an edge brings its endpoints in, a repeat re-weights in
// place, and equality (dmGraphEq) ignores insertion order while rendering
// follows it.
const declGraph = `type dmGraphEdge struct {
	to int
	w  int64
}

type dmGraph[K comparable] struct {
	nodes []K
	index map[K]int
	adj   [][]dmGraphEdge
	edges map[[2]int]int
}

func dmNewGraph[K comparable]() dmGraph[K] {
	return dmGraph[K]{index: map[K]int{}, edges: map[[2]int]int{}}
}

func (g *dmGraph[K]) addNode(n K) int {
	if i, ok := g.index[n]; ok {
		return i
	}
	i := len(g.nodes)
	g.nodes = append(g.nodes, n)
	g.adj = append(g.adj, nil)
	g.index[n] = i
	return i
}

func (g *dmGraph[K]) addEdge(a, b K, w int64) {
	i, j := g.addNode(a), g.addNode(b)
	if at, ok := g.edges[[2]int{i, j}]; ok {
		g.adj[i][at].w = w
		return
	}
	g.edges[[2]int{i, j}] = len(g.adj[i])
	g.adj[i] = append(g.adj[i], dmGraphEdge{to: j, w: w})
}

func (g *dmGraph[K]) delEdge(a, b K) {
	i, ok := g.index[a]
	if !ok {
		return
	}
	j, ok := g.index[b]
	if !ok {
		return
	}
	at, ok := g.edges[[2]int{i, j}]
	if !ok {
		return
	}
	g.adj[i] = append(g.adj[i][:at], g.adj[i][at+1:]...)
	delete(g.edges, [2]int{i, j})
	for k := at; k < len(g.adj[i]); k++ {
		g.edges[[2]int{i, g.adj[i][k].to}] = k
	}
}

func (g dmGraph[K]) weight(a, b K) (int64, bool) {
	i, ok := g.index[a]
	if !ok {
		return 0, false
	}
	j, ok := g.index[b]
	if !ok {
		return 0, false
	}
	at, ok := g.edges[[2]int{i, j}]
	if !ok {
		return 0, false
	}
	return g.adj[i][at].w, true
}

func (g dmGraph[K]) clone() dmGraph[K] {
	out := dmGraph[K]{
		nodes: append([]K(nil), g.nodes...),
		index: make(map[K]int, len(g.index)),
		adj:   make([][]dmGraphEdge, len(g.adj)),
		edges: make(map[[2]int]int, len(g.edges)),
	}
	for k, v := range g.index {
		out.index[k] = v
	}
	for i, arcs := range g.adj {
		out.adj[i] = append([]dmGraphEdge(nil), arcs...)
	}
	for k, v := range g.edges {
		out.edges[k] = v
	}
	return out
}`

const declGraphNeighbors = `func dmGraphNeighbors[K comparable](g dmGraph[K], n K) []K {
	i, ok := g.index[n]
	if !ok {
		return []K{}
	}
	out := make([]K, len(g.adj[i]))
	for k, e := range g.adj[i] {
		out[k] = g.nodes[e.to]
	}
	return out
}`

const declGraphDegree = `func dmGraphDegree[K comparable](g dmGraph[K], n K) int64 {
	i, ok := g.index[n]
	if !ok {
		return 0
	}
	return int64(len(g.adj[i]))
}`

// dmGraphWeightOf mirrors ir.GraphValue.WeightOf: the total weight of a node's
// out-arcs, 0 for a node the graph does not have.
const declGraphWeightOf = `func dmGraphWeightOf[K comparable](g dmGraph[K], n K) int64 {
	i, ok := g.index[n]
	if !ok {
		return 0
	}
	var total int64
	for _, e := range g.adj[i] {
		total += e.w
	}
	return total
}`

// dmGraphRoot mirrors the interpreter's `root`: the one node with no incoming
// arc, and an error naming what it found instead when there is not exactly
// one. The node names are rendered with %v, the same latitude dmGraphWeight
// takes — the wording of a failure is not part of the byte parity the two
// backends keep, the answers are.
const declGraphRoot = `func dmGraphRoot[K comparable](g dmGraph[K]) K {
	var zero K
	incoming := make([]bool, len(g.nodes))
	for _, arcs := range g.adj {
		for _, e := range arcs {
			incoming[e.to] = true
		}
	}
	var roots []K
	for i, n := range g.nodes {
		if !incoming[i] {
			roots = append(roots, n)
		}
	}
	if len(roots) == 1 {
		return roots[0]
	}
	if len(g.nodes) == 0 {
		dmFail("root: the graph is empty")
		return zero
	}
	if len(roots) == 0 {
		dmFail("root: every node has an incoming arc, so the graph has no root")
		return zero
	}
	names := ""
	for i, n := range roots {
		if i == 3 {
			names += ", ..."
			break
		}
		if i > 0 {
			names += ", "
		}
		names += fmt.Sprintf("%v", n)
	}
	dmFail("root: %d nodes have no incoming arc (%s); a rooted graph has exactly one", len(roots), names)
	return zero
}`

const declGraphWeightAt = `func dmGraphWeight[K comparable](g dmGraph[K], a, b K) int64 {
	w, ok := g.weight(a, b)
	if !ok {
		dmFail("weight: no edge from %v to %v", a, b)
	}
	return w
}`

const declGraphWeightOr = `func dmGraphWeightOr[K comparable](g dmGraph[K], a, b K, d int64) int64 {
	if w, ok := g.weight(a, b); ok {
		return w
	}
	return d
}`

// The node-level readers. Each mirrors the ir.GraphValue method of the same
// name, walking the adjacency in the same order so both backends hand back the
// same list.
const declGraphRoots = `func dmGraphRoots[K comparable](g dmGraph[K]) []K {
	incoming := make([]bool, len(g.nodes))
	for _, arcs := range g.adj {
		for _, e := range arcs {
			incoming[e.to] = true
		}
	}
	out := []K{}
	for i, n := range g.nodes {
		if !incoming[i] {
			out = append(out, n)
		}
	}
	return out
}`

const declGraphLeaves = `func dmGraphLeaves[K comparable](g dmGraph[K]) []K {
	out := []K{}
	for i, n := range g.nodes {
		if len(g.adj[i]) == 0 {
			out = append(out, n)
		}
	}
	return out
}`

const declGraphInDegree = `func dmGraphInDegree[K comparable](g dmGraph[K], n K) int64 {
	j, ok := g.index[n]
	if !ok {
		return 0
	}
	var count int64
	for _, arcs := range g.adj {
		for _, e := range arcs {
			if e.to == j {
				count++
			}
		}
	}
	return count
}`

const declGraphWeightSum = `func dmGraphWeightSum[K comparable](g dmGraph[K]) int64 {
	var total int64
	for _, arcs := range g.adj {
		for _, e := range arcs {
			total += e.w
		}
	}
	return total
}`

// dmGraphDelNode is dmGraphSub over everything but one node, which is what
// deleting a node is: the adjacency holds indices, so the graph is rebuilt
// either way.
const declGraphDelNode = `func dmGraphDelNode[K comparable](g dmGraph[K], n K) dmGraph[K] {
	if _, ok := g.index[n]; !ok {
		return g.clone()
	}
	keep := make([]K, 0, len(g.nodes)-1)
	for _, node := range g.nodes {
		if node != n {
			keep = append(keep, node)
		}
	}
	return dmGraphSub(g, keep)
}`

// dmGraphReachable is a breadth-first walk from the start, which is included:
// it is reachable from itself by a path of no arcs.
const declGraphReachable = `func dmGraphReachable[K comparable](g dmGraph[K], start K) []K {
	i, ok := g.index[start]
	if !ok {
		return []K{}
	}
	seen := make([]bool, len(g.nodes))
	out := []K{}
	queue := []int{i}
	seen[i] = true
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, g.nodes[cur])
		for _, e := range g.adj[cur] {
			if seen[e.to] {
				continue
			}
			seen[e.to] = true
			queue = append(queue, e.to)
		}
	}
	return out
}`

// dmGraphHasCycle peels nodes with nothing pointing at them, the same measure
// Topological Sort uses: what cannot be peeled is a cycle.
const declGraphHasCycle = `func dmGraphHasCycle[K comparable](g dmGraph[K]) bool {
	indeg := make([]int, len(g.nodes))
	for _, arcs := range g.adj {
		for _, e := range arcs {
			indeg[e.to]++
		}
	}
	queue := []int{}
	for i, d := range indeg {
		if d == 0 {
			queue = append(queue, i)
		}
	}
	ordered := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		ordered++
		for _, e := range g.adj[cur] {
			indeg[e.to]--
			if indeg[e.to] == 0 {
				queue = append(queue, e.to)
			}
		}
	}
	return ordered != len(g.nodes)
}`

// dmGraphUndirected adds only the missing reverse of each arc: an arc that is
// already there in both directions keeps both weights.
const declGraphUndirected = `func dmGraphUndirected[K comparable](g dmGraph[K]) dmGraph[K] {
	out := g.clone()
	for i, arcs := range g.adj {
		for _, e := range arcs {
			if _, ok := out.weight(g.nodes[e.to], g.nodes[i]); !ok {
				out.addEdge(g.nodes[e.to], g.nodes[i], e.w)
			}
		}
	}
	return out
}`

// dmGraphMerge writes b's nodes and arcs into a copy of a, so an arc both
// carry takes b's weight — addEdge's last-write-wins rule.
const declGraphMerge = `func dmGraphMerge[K comparable](a, b dmGraph[K]) dmGraph[K] {
	out := a.clone()
	for _, n := range b.nodes {
		out.addNode(n)
	}
	for i, arcs := range b.adj {
		for _, e := range arcs {
			out.addEdge(b.nodes[i], b.nodes[e.to], e.w)
		}
	}
	return out
}`

// dmGraphMST mirrors prims.graphMST: Kruskal over the arcs read as undirected,
// cheapest first with ties broken by insertion order, so both backends choose
// the same tree. A graph in several pieces gives a spanning forest.
const declGraphMST = `func dmGraphMST[K comparable](g dmGraph[K]) dmGraph[K] {
	type dmMSTArc struct {
		w    int64
		from int
		to   int
	}
	arcs := []dmMSTArc{}
	for i := range g.nodes {
		for _, e := range g.adj[i] {
			if i == e.to {
				continue
			}
			arcs = append(arcs, dmMSTArc{w: e.w, from: i, to: e.to})
		}
	}
	sort.SliceStable(arcs, func(x, y int) bool {
		if arcs[x].w != arcs[y].w {
			return arcs[x].w < arcs[y].w
		}
		if arcs[x].from != arcs[y].from {
			return arcs[x].from < arcs[y].from
		}
		return arcs[x].to < arcs[y].to
	})
	parent := make([]int, len(g.nodes))
	rank := make([]int, len(g.nodes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	out := dmNewGraph[K]()
	for _, n := range g.nodes {
		out.addNode(n)
	}
	for _, a := range arcs {
		x, y := find(a.from), find(a.to)
		if x == y {
			continue
		}
		if rank[x] < rank[y] {
			x, y = y, x
		}
		parent[y] = x
		if rank[x] == rank[y] {
			rank[x]++
		}
		out.addEdge(g.nodes[a.from], g.nodes[a.to], a.w)
	}
	return out
}`

// dmGraphSCC mirrors prims.graphSCC: Kosaraju, both passes iterative and both
// taking adjacency in insertion order, each component's nodes in the graph's
// insertion order and the components in the order the second pass finds them.
const declGraphSCC = `func dmGraphSCC[K comparable](g dmGraph[K]) [][]K {
	n := len(g.nodes)
	radj := make([][]int, n)
	for i := range n {
		for _, e := range g.adj[i] {
			radj[e.to] = append(radj[e.to], i)
		}
	}
	seen := make([]bool, n)
	order := make([]int, 0, n)
	next := make([]int, n)
	for start := range n {
		if seen[start] {
			continue
		}
		seen[start] = true
		stack := []int{start}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			if next[cur] < len(g.adj[cur]) {
				to := g.adj[cur][next[cur]].to
				next[cur]++
				if !seen[to] {
					seen[to] = true
					stack = append(stack, to)
				}
				continue
			}
			order = append(order, cur)
			stack = stack[:len(stack)-1]
		}
	}
	comp := make([]int, n)
	for i := range comp {
		comp[i] = -1
	}
	out := [][]K{}
	for i := len(order) - 1; i >= 0; i-- {
		root := order[i]
		if comp[root] != -1 {
			continue
		}
		id := len(out)
		members := []int{}
		comp[root] = id
		stack := []int{root}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			members = append(members, cur)
			for _, from := range radj[cur] {
				if comp[from] == -1 {
					comp[from] = id
					stack = append(stack, from)
				}
			}
		}
		sort.Ints(members)
		nodes := make([]K, len(members))
		for k, m := range members {
			nodes[k] = g.nodes[m]
		}
		out = append(out, nodes)
	}
	return out
}`

const declGraphAddNode = `func dmGraphAddNode[K comparable](g dmGraph[K], n K) dmGraph[K] {
	out := g.clone()
	out.addNode(n)
	return out
}`

const declGraphAddEdge = `func dmGraphAddEdge[K comparable](g dmGraph[K], a, b K, w int64) dmGraph[K] {
	out := g.clone()
	out.addEdge(a, b, w)
	return out
}`

const declGraphDelEdge = `func dmGraphDelEdge[K comparable](g dmGraph[K], a, b K) dmGraph[K] {
	out := g.clone()
	out.delEdge(a, b)
	return out
}`

const declGraphAddNodeIn = `func dmGraphAddNodeIn[K comparable](g dmGraph[K], n K) dmGraph[K] {
	g.addNode(n)
	return g
}`

const declGraphAddEdgeIn = `func dmGraphAddEdgeIn[K comparable](g dmGraph[K], a, b K, w int64) dmGraph[K] {
	g.addEdge(a, b, w)
	return g
}`

const declGraphFlip = `func dmGraphFlip[K comparable](g dmGraph[K]) dmGraph[K] {
	out := dmNewGraph[K]()
	for _, n := range g.nodes {
		out.addNode(n)
	}
	for i, arcs := range g.adj {
		for _, e := range arcs {
			out.addEdge(g.nodes[e.to], g.nodes[i], e.w)
		}
	}
	return out
}`

const declGraphSub = `func dmGraphSub[K comparable](g dmGraph[K], keep []K) dmGraph[K] {
	want := make(map[K]bool, len(keep))
	out := dmNewGraph[K]()
	for _, n := range keep {
		if _, ok := g.index[n]; ok {
			want[n] = true
			out.addNode(n)
		}
	}
	for i, arcs := range g.adj {
		if !want[g.nodes[i]] {
			continue
		}
		for _, e := range arcs {
			if want[g.nodes[e.to]] {
				out.addEdge(g.nodes[i], g.nodes[e.to], e.w)
			}
		}
	}
	return out
}`

// dmGraphEq mirrors ir.GraphEqual: same nodes, same arcs, **independent of
// insertion order**. Two graphs built by reading the same edges in a different
// order are the same graph, so a fixed-point loop over one converges.
const declGraphEq = `func dmGraphEq[K comparable](a, b dmGraph[K]) bool {
	if len(a.nodes) != len(b.nodes) || len(a.edges) != len(b.edges) {
		return false
	}
	for k := range a.index {
		if _, ok := b.index[k]; !ok {
			return false
		}
	}
	for i, arcs := range a.adj {
		for _, e := range arcs {
			w, ok := b.weight(a.nodes[i], a.nodes[e.to])
			if !ok || w != e.w {
				return false
			}
		}
	}
	return true
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

// ---------------------------------------------------------------------------
// v0.6 expression-layer builtins.
//
// The interpreter's implementations are eval/eval.go and eval/numbers.go, and
// these must agree with them exactly — same results, same error wording, same
// iteration order. Where a helper is an algorithm rather than a wrapper
// (isprime, divisors, crt) the two are written the same way on purpose, so a
// reader can put them side by side.
// ---------------------------------------------------------------------------

// The collection updates are functional: each returns a new value and leaves
// its argument untouched, because a lambda may be applied to the same value
// twice (the optimizer folds constants by doing exactly that). Every one sizes
// its copy exactly, so building a map in a fold costs one allocation per step
// rather than a growth sequence.

const declMapClone = `func dmMapClone[K comparable, V any](m dmMap[K, V]) dmMap[K, V] {
	out := dmMap[K, V]{keys: make([]K, len(m.keys)), vals: make(map[K]V, len(m.vals))}
	copy(out.keys, m.keys)
	for k, v := range m.vals {
		out.vals[k] = v
	}
	return out
}`

const declMapWith = `func dmMapWith[K comparable, V any](m dmMap[K, V], k K, v V) dmMap[K, V] {
	out := dmMapClone(m)
	out.put(k, v)
	return out
}`

const declMapWithout = `func dmMapWithout[K comparable, V any](m dmMap[K, V], k K) dmMap[K, V] {
	if _, ok := m.vals[k]; !ok {
		return dmMapClone(m)
	}
	out := dmMap[K, V]{keys: make([]K, 0, len(m.keys)-1), vals: make(map[K]V, len(m.vals)-1)}
	for _, key := range m.keys {
		if key != k {
			out.keys = append(out.keys, key)
			out.vals[key] = m.vals[key]
		}
	}
	return out
}`

const declSetClone = `func dmSetClone[T comparable](s dmSet[T]) dmSet[T] {
	out := dmSet[T]{elems: make([]T, len(s.elems)), has: make(map[T]struct{}, len(s.has))}
	copy(out.elems, s.elems)
	for k := range s.has {
		out.has[k] = struct{}{}
	}
	return out
}`

const declSetWith = `func dmSetWith[T comparable](s dmSet[T], v T) dmSet[T] {
	out := dmSetClone(s)
	out.add(v)
	return out
}`

const declSetWithout = `func dmSetWithout[T comparable](s dmSet[T], v T) dmSet[T] {
	if _, ok := s.has[v]; !ok {
		return dmSetClone(s)
	}
	out := dmSet[T]{elems: make([]T, 0, len(s.elems)-1), has: make(map[T]struct{}, len(s.has)-1)}
	for _, e := range s.elems {
		if e != v {
			out.elems = append(out.elems, e)
			out.has[e] = struct{}{}
		}
	}
	return out
}`

const declToSet = `func dmToSet[T comparable](xs []T) dmSet[T] {
	out := dmSet[T]{elems: make([]T, 0, len(xs)), has: make(map[T]struct{}, len(xs))}
	for _, x := range xs {
		out.add(x)
	}
	return out
}`

// a is already deduplicated, so only b's elements need the membership test.
const declSetUnion = `func dmSetUnion[T comparable](a, b dmSet[T]) dmSet[T] {
	out := dmSet[T]{elems: make([]T, len(a.elems), len(a.elems)+len(b.elems)),
		has: make(map[T]struct{}, len(a.elems)+len(b.elems))}
	copy(out.elems, a.elems)
	for k := range a.has {
		out.has[k] = struct{}{}
	}
	for _, e := range b.elems {
		out.add(e)
	}
	return out
}`

const declSetIntersect = `func dmSetIntersect[T comparable](a, b dmSet[T]) dmSet[T] {
	n := len(a.elems)
	if len(b.elems) < n {
		n = len(b.elems)
	}
	out := dmSet[T]{elems: make([]T, 0, n), has: make(map[T]struct{}, n)}
	for _, e := range a.elems {
		if _, ok := b.has[e]; ok {
			out.add(e)
		}
	}
	return out
}`

const declSetDiff = `func dmSetDiff[T comparable](a, b dmSet[T]) dmSet[T] {
	out := dmSet[T]{elems: make([]T, 0, len(a.elems)), has: make(map[T]struct{}, len(a.elems))}
	for _, e := range a.elems {
		if _, ok := b.has[e]; !ok {
			out.add(e)
		}
	}
	return out
}`

const declGridWith = `func dmGridWith[T any](g dmGrid[T], r, c int64, v T) dmGrid[T] {
	if r < 0 || r >= int64(g.rows) || c < 0 || c >= int64(g.cols) {
		dmFail("setat: position (%d, %d) out of range (grid %dx%d)", r, c, g.rows, g.cols)
	}
	out := dmGrid[T]{rows: g.rows, cols: g.cols, cells: make([]T, len(g.cells))}
	copy(out.cells, g.cells)
	out.cells[r*int64(g.cols)+c] = v
	return out
}`

// dmRange is the half-open [lo, hi), matching the Range primitive. The count is
// computed in uint64 so a range spanning zero reports its size instead of
// overflowing to a negative count and silently building nothing.
const declRange = `func dmRange(lo, hi int64) []int64 {
	if hi <= lo {
		return []int64{}
	}
	n := uint64(hi) - uint64(lo)
	if n > 1<<40 {
		dmFail("range: [%d, %d) has %d elements, which is more than can be built", lo, hi, n)
	}
	out := make([]int64, n)
	for i := range out {
		out[i] = lo + int64(i)
	}
	return out
}`

// Total, like take and drop: a negative count is the empty list.
const declFill = `func dmFill[T any](n int64, v T) []T {
	if n <= 0 {
		return []T{}
	}
	if n > 1<<40 {
		dmFail("fill: %d elements is more than can be built", n)
	}
	out := make([]T, n)
	for i := range out {
		out[i] = v
	}
	return out
}`

const declOrd = `func dmOrd(s string) int64 {
	if s == "" {
		dmFail("ord of the empty text is undefined")
	}
	r, _ := utf8.DecodeRuneInString(s)
	return int64(r)
}`

const declChr = `func dmChr(n int64) string {
	if n < 0 || n > 0x10FFFF || (n >= 0xD800 && n <= 0xDFFF) {
		dmFail("chr: %d is not a character code", n)
	}
	return string(rune(n))
}`

const declRepeatText = `func dmRepeatText(s string, n int64) string {
	if n <= 0 || s == "" {
		return ""
	}
	if n > (1<<40)/int64(len(s)) {
		dmFail("repeat: %d copies of a %d-byte text is more than can be built", n, len(s))
	}
	return strings.Repeat(s, int(n))
}`

// Padding counts runes, not bytes, because every other text position in the
// language does — padding by bytes would disagree with length on exactly the
// input that makes padding worth doing.
const declPadText = `func dmPadText(s string, width int64, pad string, left bool) string {
	if pad == "" || width <= 0 {
		return s
	}
	have := int64(0)
	for range s {
		have++
	}
	if have >= width {
		return s
	}
	need := int(width - have)
	padRunes := []rune(pad)
	fill := make([]rune, need)
	for i := range fill {
		fill[i] = padRunes[i%len(padRunes)]
	}
	filled := string(fill)
	var b strings.Builder
	b.Grow(len(s) + len(filled))
	if left {
		b.WriteString(filled)
		b.WriteString(s)
	} else {
		b.WriteString(s)
		b.WriteString(filled)
	}
	return b.String()
}`

// The empty text is false for all four: "every rune is a digit" is vacuously
// true of it, which is never what a guard means. isupper/islower ask about the
// cased runes, so "A1" is upper and "1" is neither.
const declClassify = `func dmIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func dmUpperRune(r rune) bool {
	if r < 128 {
		return r >= 'A' && r <= 'Z'
	}
	return r != []rune(strings.ToLower(string(r)))[0]
}

func dmLowerRune(r rune) bool {
	if r < 128 {
		return r >= 'a' && r <= 'z'
	}
	return r != []rune(strings.ToUpper(string(r)))[0]
}

func dmIsAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		ascii := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !ascii && !(r > 127 && (dmUpperRune(r) || dmLowerRune(r))) {
			return false
		}
	}
	return true
}

func dmIsUpper(s string) bool {
	if s == "" {
		return false
	}
	cased := false
	for _, r := range s {
		if dmLowerRune(r) {
			return false
		}
		cased = cased || dmUpperRune(r)
	}
	return cased
}

func dmIsLower(s string) bool {
	if s == "" {
		return false
	}
	cased := false
	for _, r := range s {
		if dmUpperRune(r) {
			return false
		}
		cased = cased || dmLowerRune(r)
	}
	return cased
}`

// Domain has no infinity or NaN — there is no way to write one and no way to
// print one usefully — so a computation that leaves the reals fails where it
// happens rather than poisoning a value three stages later.
const declFloat1 = `func dmFloat1(name string, f float64) float64 {
	var r float64
	switch name {
	case "log", "log2", "log10":
		if f <= 0 {
			dmFail("%s of a non-positive number (%s)", name, strconv.FormatFloat(f, 'g', -1, 64))
		}
		switch name {
		case "log":
			r = math.Log(f)
		case "log2":
			r = math.Log2(f)
		default:
			r = math.Log10(f)
		}
	case "exp":
		r = math.Exp(f)
	case "sin":
		r = math.Sin(f)
	case "cos":
		r = math.Cos(f)
	case "tan":
		r = math.Tan(f)
	}
	if math.IsInf(r, 0) || math.IsNaN(r) {
		dmFail("%s(%s) has no finite value", name, strconv.FormatFloat(f, 'g', -1, 64))
	}
	return r
}`

const declParseBase = `func dmParseBase(name, s string, base int64) int64 {
	if base < 2 || base > 36 {
		dmFail("frombase: base must be between 2 and 36, got %d", base)
	}
	t := strings.TrimSpace(s)
	if name == "fromhex" {
		if rest, ok := strings.CutPrefix(t, "0x"); ok {
			t = rest
		} else if rest, ok := strings.CutPrefix(t, "0X"); ok {
			t = rest
		}
	}
	n, err := strconv.ParseInt(t, int(base), 64)
	if err != nil {
		dmFail("%s: %q is not a base-%d number", name, s, base)
	}
	return n
}`

const declToBase = `func dmToBase(n, base int64) string {
	if base < 2 || base > 36 {
		dmFail("tobase: base must be between 2 and 36, got %d", base)
	}
	return strconv.FormatInt(n, int(base))
}`

// The digit count is computed first so the slice is allocated once and filled
// from the back — the obvious "append then reverse" does the same work twice.
const declDigits = `func dmDigits(n int64) []int64 {
	if n == 0 {
		return []int64{0}
	}
	u := uint64(n)
	if n < 0 {
		u = -u
	}
	count := 0
	for v := u; v > 0; v /= 10 {
		count++
	}
	out := make([]int64, count)
	for i := count - 1; i >= 0; i-- {
		out[i] = int64(u % 10)
		u /= 10
	}
	return out
}`

const declFromDigits = `func dmFromDigits(ds []int64) int64 {
	var n int64
	for i, d := range ds {
		if d < 0 || d > 9 {
			dmFail("fromdigits: element %d is %d, not a decimal digit", i, d)
		}
		if n > (math.MaxInt64-d)/10 {
			dmFail("fromdigits: %d digits overflow Int", len(ds))
		}
		n = n*10 + d
	}
	return n
}`

// dmMulMod computes a*b mod m through the 128-bit product: Miller-Rabin on
// int64-sized inputs is impossible without it, since the naive product
// overflows for any modulus past 2^32.
const declModArith = `func dmMulMod(a, b, m uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	_, r := bits.Div64(hi%m, lo, m)
	return r
}

func dmPowMod(b, e, m uint64) uint64 {
	r := uint64(1) % m
	b %= m
	for e > 0 {
		if e&1 == 1 {
			r = dmMulMod(r, b, m)
		}
		b = dmMulMod(b, b, m)
		e >>= 1
	}
	return r
}`

// The first twelve primes make the test deterministic past 3.3e24, so isprime
// is exact for every Int rather than probabilistic — and O(log^3 n) rather than
// the three billion divisions a trial division would take on a 19-digit input.
const declIsPrime = `var dmMRBases = [...]uint64{2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37}

func dmIsPrime(n int64) bool {
	if n < 2 {
		return false
	}
	u := uint64(n)
	for _, p := range dmMRBases {
		if u == p {
			return true
		}
		if u%p == 0 {
			return false
		}
	}
	d := u - 1
	s := bits.TrailingZeros64(d)
	d >>= uint(s)
	for _, a := range dmMRBases {
		x := dmPowMod(a, d, u)
		if x == 1 || x == u-1 {
			continue
		}
		composite := true
		for range s - 1 {
			x = dmMulMod(x, x, u)
			if x == u-1 {
				composite = false
				break
			}
		}
		if composite {
			return false
		}
	}
	return true
}`

// One pass to sqrt(n): the small half of each divisor pair lands in order and
// the large half in reverse, so appending the second backwards yields a sorted
// result with no sort at all.
const declDivisors = `func dmDivisors(n int64) []int64 {
	if n <= 0 {
		dmFail("divisors: needs a positive number, got %d", n)
	}
	var small, large []int64
	for d := int64(1); d <= n/d; d++ {
		if n%d != 0 {
			continue
		}
		small = append(small, d)
		if q := n / d; q != d {
			large = append(large, q)
		}
	}
	out := make([]int64, 0, len(small)+len(large))
	out = append(out, small...)
	for i := len(large) - 1; i >= 0; i-- {
		out = append(out, large[i])
	}
	return out
}`

// The moduli need not be coprime: each pair is checked for agreement modulo
// their gcd and merged on their lcm, which is what makes crt usable on a system
// read out of a puzzle rather than one constructed to be coprime.
const declCRT = `func dmExtGCD(a, b int64) (int64, int64, int64) {
	if b == 0 {
		return a, 1, 0
	}
	g, x1, y1 := dmExtGCD(b, a%b)
	return g, y1, x1 - (a/b)*y1
}

func dmMulChecked(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	p := a * b
	if p/b != a {
		dmFail("crt: the combined modulus overflows Int")
	}
	return p
}

func dmCRT(rs, ms []int64) int64 {
	if len(rs) != len(ms) {
		dmFail("crt: %d residues but %d moduli", len(rs), len(ms))
	}
	if len(rs) == 0 {
		dmFail("crt: needs at least one congruence")
	}
	var r, m int64 = 0, 1
	for i := range rs {
		if ms[i] <= 0 {
			dmFail("crt: modulus %d must be positive, got %d", i, ms[i])
		}
		r2, m2 := ((rs[i]%ms[i])+ms[i])%ms[i], ms[i]
		g, p, _ := dmExtGCD(m, m2)
		diff := r2 - r
		if diff%g != 0 {
			dmFail("crt: no solution — %d (mod %d) and %d (mod %d) disagree", r, m, r2, m2)
		}
		lcm := dmMulChecked(m, m2/g)
		unit := m2 / g
		step := int64(dmMulMod(uint64(((diff/g)%unit+unit)%unit), uint64(((p%unit)+unit)%unit), uint64(unit)))
		r = r + dmMulChecked(step, m)
		r = ((r % lcm) + lcm) % lcm
		m = lcm
	}
	return r
}`

const declTestBit = `func dmTestBit(n, i int64) bool {
	if i < 0 || i > 63 {
		dmFail("testbit: bit %d is outside an Int (0 to 63)", i)
	}
	return n&(1<<uint(i)) != 0
}`

// The foreign-block runtime. It mirrors prims/foreign.go: same wire format,
// same runner resolution, same wording on failure — the differential tests
// require a foreign program to print the same bytes under `domain run` and as
// a compiled binary, and these are the two halves that have to agree.

const declForeignLine = `func dmForeignLine(s string) string {
	if s == "" {
		return ""
	}
	return s + "\n"
}`

const declForeignBody = `func dmForeignBody(out string) string {
	return strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r")
}`

const declForeignLines = `func dmForeignLines(out string) []string {
	body := dmForeignBody(out)
	if body == "" {
		return []string{}
	}
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}`

const declForeignInt = `func dmForeignInt(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		dmFail("the foreign block's output: expected an Int, got %q", s)
	}
	return n
}`

const declForeignFloat = `func dmForeignFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		dmFail("the foreign block's output: expected a Float, got %q", s)
	}
	return f
}`

const declForeignBool = `func dmForeignBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1":
		return true
	case "false", "0":
		return false
	}
	dmFail("the foreign block's output: expected a Bool (true/false), got %q", s)
	return false
}`

// declForeignRun writes the block into a throwaway directory and runs it. The
// temporary directory is removed on every path, including the failing ones —
// dmFail exits the process, so a deferred cleanup would not run.
const declForeignRun = `type dmForeignSpec struct {
	Lang, File, Source, Env string
	Cands, Tail             []string
	AppendProg              bool
	Extra                   map[string]string
}

func dmForeignRun(s dmForeignSpec, stdin string) string {
	dir, err := os.MkdirTemp("", "domain-foreign-*")
	if err != nil {
		dmFail("%v", err)
	}
	fail := func(format string, args ...any) {
		os.RemoveAll(dir)
		dmFail(format, args...)
	}
	files := map[string]string{s.File: s.Source}
	for name, content := range s.Extra {
		files[name] = content
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			fail("%v", err)
		}
	}

	var argv []string
	if override := strings.TrimSpace(os.Getenv(s.Env)); override != "" {
		argv = strings.Fields(override)
	} else {
		for _, c := range s.Cands {
			if p, err := exec.LookPath(c); err == nil {
				argv = []string{p}
				break
			}
		}
	}
	if len(argv) == 0 {
		fail("a %s block needs %s on PATH to run (set %s to name it differently)",
			s.Lang, strings.Join(s.Cands, " or "), s.Env)
	}
	argv = append(argv, s.Tail...)
	if s.AppendProg {
		argv = append(argv, filepath.Join(dir, s.File))
	}

	var out, errBuf bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if runErr := cmd.Run(); runErr != nil {
		msg := strings.TrimRight(errBuf.String(), "\n")
		var exit *exec.ExitError
		switch {
		case !errors.As(runErr, &exit):
			fail("could not run the %s block: %v", s.Lang, runErr)
		case msg == "":
			fail("the %s block exited with status %d and said nothing", s.Lang, exit.ExitCode())
		default:
			fail("the %s block failed with status %d\n%s", s.Lang, exit.ExitCode(), msg)
		}
	}
	os.RemoveAll(dir)
	if errBuf.Len() > 0 {
		os.Stderr.Write(errBuf.Bytes())
	}
	return out.String()
}`

// The in-place halves of the functional updates above: each is its own With
// minus the clone, which is what makes the two agree by inspection rather than
// by a second implementation. Both backends reach them only where
// optimizer/linear.go proved the copied-from value dead, and only after the
// fold that drives them has cloned its accumulator once on entry.
//
// They take and return the collection by value because a dmMap/dmSet/dmGrid is
// a struct: the maps and slices inside are shared, but an append that
// reallocates has to travel back out, so the caller must use the result.

const declMapPutIn = `func dmMapPutIn[K comparable, V any](m dmMap[K, V], k K, v V) dmMap[K, V] {
	m.put(k, v)
	return m
}`

const declSetAddIn = `func dmSetAddIn[T comparable](s dmSet[T], v T) dmSet[T] {
	s.add(v)
	return s
}`

const declGridSetIn = `func dmGridSetIn[T any](g dmGrid[T], r, c int64, v T) dmGrid[T] {
	if r < 0 || r >= int64(g.rows) || c < 0 || c >= int64(g.cols) {
		dmFail("setat: position (%d, %d) out of range (grid %dx%d)", r, c, g.rows, g.cols)
	}
	g.cells[r*int64(g.cols)+c] = v
	return g
}`

const declSparsePutIn = `func dmSparsePutIn[T any](s dmSparse[T], r, c int64, v T) dmSparse[T] {
	s.put(r, c, v)
	return s
}`

// The clones a fold makes once on entry, so the accumulator has storage of its
// own before anything writes through it. dmMapClone and dmSetClone are already
// declared above for the functional path; these two are the rest of the set.

const declGridClone = `func dmGridClone[T any](g dmGrid[T]) dmGrid[T] {
	out := dmGrid[T]{rows: g.rows, cols: g.cols, cells: make([]T, len(g.cells))}
	copy(out.cells, g.cells)
	return out
}`

const declSparseClone = `func dmSparseClone[T any](s dmSparse[T]) dmSparse[T] {
	out := dmSparse[T]{def: s.def, cells: make(map[dmSPt]T, len(s.cells)),
		minR: s.minR, maxR: s.maxR, minC: s.minC, maxC: s.maxC}
	for k, e := range s.cells {
		out.cells[k] = e
	}
	return out
}`

// dmPQ is the min-heap Explore's weighted modes settle states with, mirroring
// ir.PQ. The insertion-order tiebreak is not decoration: Mode: Costs renders
// its Map in settle order, so two equal-cost states have to come out in the
// same order here as in the interpreter or the two backends print different
// text for the same answer.
const declPQ = `type dmPQItem[T any] struct {
	v   T
	pri int64
	seq int64
}

type dmPQ[T any] struct {
	h   []dmPQItem[T]
	seq int64
}

func (q *dmPQ[T]) less(a, b dmPQItem[T]) bool {
	if a.pri != b.pri {
		return a.pri < b.pri
	}
	return a.seq < b.seq
}

func (q *dmPQ[T]) push(v T, pri int64) {
	q.h = append(q.h, dmPQItem[T]{v, pri, q.seq})
	q.seq++
	for i := len(q.h) - 1; i > 0; {
		p := (i - 1) / 2
		if !q.less(q.h[i], q.h[p]) {
			break
		}
		q.h[p], q.h[i] = q.h[i], q.h[p]
		i = p
	}
}

func (q *dmPQ[T]) pop() (T, int64, bool) {
	if len(q.h) == 0 {
		var zero T
		return zero, 0, false
	}
	top := q.h[0]
	n := len(q.h) - 1
	q.h[0] = q.h[n]
	q.h = q.h[:n]
	for i := 0; ; {
		l, r, m := 2*i+1, 2*i+2, i
		if l < n && q.less(q.h[l], q.h[m]) {
			m = l
		}
		if r < n && q.less(q.h[r], q.h[m]) {
			m = r
		}
		if m == i {
			break
		}
		q.h[i], q.h[m] = q.h[m], q.h[i]
		i = m
	}
	return top.v, top.pri, true
}`

// declAllocReport is the compiled half of the allocation-measurement protocol
// (runner/alloc.go holds the interpreter's half and the reader).
//
// A normal run pays one environment lookup at exit and returns. When
// DOMAIN_ALLOC_REPORT names a file — which only `domain expansion: bench` and
// its siblings ever set — the run writes four numbers into it: cumulative
// bytes allocated, cumulative allocation count, heap obtained from the OS, and
// GC cycles.
//
// The wire format is four space-separated integers on one line, and it is
// implemented twice, here and in runner.WriteReport, for the same reason the
// foreign block's format is: the process being measured and the process doing
// the measuring cannot share code. A test pins the two together.
const declAllocReport = `func dmAllocReport() {
	path := os.Getenv("DOMAIN_ALLOC_REPORT")
	if path == "" {
		return
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	line := fmt.Sprintf("%d %d %d %d\n", m.TotalAlloc, m.Mallocs, m.HeapSys, m.NumGC)
	_ = os.WriteFile(path, []byte(line), 0o644)
}`

// DeclAllocReport exposes the emitted allocation-report helper so package
// runner, which implements the other half of the same wire format, can pin
// the two together in a test without a Go toolchain.
func DeclAllocReport() string { return declAllocReport }

// declConstProbe is the reconnaissance half of Tuning.Constants: what each
// `Consider` binding actually held, reported by the binding itself.
//
// Only a probe build carries it, and a probe build is never timed — it is
// compiled, run once, read and thrown away. That is what lets it be a map
// write per binding evaluation rather than something clever: the cost lands
// on a run nobody is measuring.
//
// It records the *first* value and whether any later evaluation differed,
// because those are two different findings. A binding inside a loop body is
// evaluated once per lap, and one that held 16 on all fifty thousand of them
// is a constant of this run; one that held something new each time is the loop
// variable and must never be pinned. The count rides along so a reader can
// tell "held 16 once" from "held 16 fifty thousand times" — the second is
// worth a build and the first is not.
//
// The maximum is here for the other reader. The same hook reports how long a
// list accumulator grew (Tuning.ListCapacities), and a capacity wants the
// largest length the site ever reached, not the first — a loop that builds a
// list twice, short then long, must reserve for the long one. A capacity is a
// hint, so `varies` disqualifies nothing there; it disqualifies a pin.
const declConstProbe = `type dmProbeRec struct {
	first  int64
	max    int64
	calls  int64
	varies bool
}

var dmProbes = map[string]*dmProbeRec{}
var dmProbeOrder []string

func dmProbe(key string, v int64) int64 {
	r := dmProbes[key]
	if r == nil {
		r = &dmProbeRec{first: v, max: v}
		dmProbes[key] = r
		dmProbeOrder = append(dmProbeOrder, key)
	}
	r.calls++
	if v != r.first {
		r.varies = true
	}
	if v > r.max {
		r.max = v
	}
	return v
}

func dmProbeReport() {
	path := os.Getenv("DOMAIN_CONST_PROBE")
	if path == "" {
		return
	}
	var b strings.Builder
	for _, k := range dmProbeOrder {
		r := dmProbes[k]
		fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%t\n", k, r.first, r.max, r.calls, r.varies)
	}
	_ = os.WriteFile(path, []byte(b.String()), 0o644)
}`

// EnvConstProbe names the file a probe build writes its bindings into. The
// reader is mahoraga/probe.go; the two halves are pinned together by a test,
// like the allocation report's.
const EnvConstProbe = "DOMAIN_CONST_PROBE"

// DeclConstProbe exposes the emitted probe helper for that test.
func DeclConstProbe() string { return declConstProbe }

// declCPUProfile is the profile-collection half of what `domain expansion:
// mahoraga` needs to feed Go's profile-guided optimization.
//
// Like the allocation hook beside it, an ordinary run pays one environment
// lookup at exit and returns. When DOMAIN_CPU_PROFILE names a file, the run
// writes a pprof CPU profile into it, which `go build -pgo=<file>` then
// consumes on the rebuild — so the program is compiled against a profile of
// itself doing the actual work, on the actual input.
//
// It returns the stop function rather than taking one, so main can write
// `defer dmCPUProfile()()`: the outer call starts the profile immediately and
// the deferred inner call stops it, with one line and no state to thread.
// A failure to start is silently a no-op — a missing profile costs a
// measurement, and taking the program down over it would cost the answer.
const declCPUProfile = `func dmCPUProfile() func() {
	path := os.Getenv("DOMAIN_CPU_PROFILE")
	if path == "" {
		return func() {}
	}
	f, err := os.Create(path)
	if err != nil {
		return func() {}
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return func() {}
	}
	return func() {
		pprof.StopCPUProfile()
		_ = f.Close()
	}
}`

// DeclCPUProfile exposes the emitted profile helper so package runner, which
// names the same environment variable, can pin the two together in a test.
func DeclCPUProfile() string { return declCPUProfile }

// The graph search runtime. Each mirrors the interpreter's function of the
// same job in prims/graph.go — same traversal order, same insertion order into
// the result map, same refusal of a negative weight — so the two backends
// print the same bytes.

const declGraphBFS = `func dmGraphBFS[K comparable](g dmGraph[K], start int) dmMap[K, int64] {
	dist := make([]int64, len(g.nodes))
	seen := make([]bool, len(g.nodes))
	out := dmNewMap[K, int64]()
	q := []int{start}
	seen[start] = true
	out.put(g.nodes[start], 0)
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		for _, e := range g.adj[cur] {
			if seen[e.to] {
				continue
			}
			seen[e.to] = true
			dist[e.to] = dist[cur] + 1
			out.put(g.nodes[e.to], dist[e.to])
			q = append(q, e.to)
		}
	}
	return out
}`

const declGraphNoNeg = `func dmGraphNoNeg[K comparable](g dmGraph[K], prim string) {
	for i := range g.nodes {
		for _, e := range g.adj[i] {
			if e.w < 0 {
				dmFail("%s: negative edge weight %d from %v to %v; %s needs non-negative weights",
					prim, e.w, g.nodes[i], g.nodes[e.to], prim)
			}
		}
	}
}`

const declGraphDijkstra = `func dmGraphDijkstra[K comparable](g dmGraph[K], start int) dmMap[K, int64] {
	dmGraphNoNeg(g, "Dijkstra")
	best := make([]int64, len(g.nodes))
	for i := range best {
		best[i] = -1
	}
	settled := make([]bool, len(g.nodes))
	out := dmNewMap[K, int64]()
	var pq dmPQ[int]
	best[start] = 0
	pq.push(start, 0)
	for {
		cur, d, ok := pq.pop()
		if !ok {
			break
		}
		if settled[cur] {
			continue
		}
		settled[cur] = true
		out.put(g.nodes[cur], d)
		for _, e := range g.adj[cur] {
			nd := d + e.w
			if best[e.to] == -1 || nd < best[e.to] {
				best[e.to] = nd
				pq.push(e.to, nd)
			}
		}
	}
	return out
}`

const declGraphComponents = `func dmGraphComponents[K comparable](g dmGraph[K]) int64 {
	parent := make([]int, len(g.nodes))
	rank := make([]int, len(g.nodes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	count := len(g.nodes)
	for i := range g.nodes {
		for _, e := range g.adj[i] {
			a, b := find(i), find(e.to)
			if a == b {
				continue
			}
			if rank[a] < rank[b] {
				a, b = b, a
			}
			parent[b] = a
			if rank[a] == rank[b] {
				rank[a]++
			}
			count--
		}
	}
	return int64(count)
}`

const declGraphPath = `func dmGraphPath[K comparable](g dmGraph[K], start, goal int) []K {
	dmGraphNoNeg(g, "Shortest Path")
	best := make([]int64, len(g.nodes))
	prev := make([]int, len(g.nodes))
	for i := range best {
		best[i], prev[i] = -1, -1
	}
	settled := make([]bool, len(g.nodes))
	var pq dmPQ[int]
	best[start] = 0
	pq.push(start, 0)
	for {
		cur, d, ok := pq.pop()
		if !ok {
			break
		}
		if settled[cur] {
			continue
		}
		settled[cur] = true
		if cur == goal {
			break
		}
		for _, e := range g.adj[cur] {
			nd := d + e.w
			if best[e.to] == -1 || nd < best[e.to] {
				best[e.to], prev[e.to] = nd, cur
				pq.push(e.to, nd)
			}
		}
	}
	if best[goal] == -1 {
		return []K{}
	}
	var rev []K
	for at := goal; at != -1; at = prev[at] {
		rev = append(rev, g.nodes[at])
	}
	out := make([]K, len(rev))
	for i, n := range rev {
		out[len(rev)-1-i] = n
	}
	return out
}`
