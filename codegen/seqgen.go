package codegen

import (
	"domain/ast"
	"domain/ir"
	"fmt"
)

// Lowerings for the section-D remainder: Window, Flatten, Enumerate, the
// key-lambda reductions (Count By, Min By, Max By, Sort By), the standalone
// Difference, and the Zip consumer. Each mirrors its interpreter twin in
// prims/seq.go / prims/channel.go.

func (g *gen) emitWindow(n *ir.Node, in string) (string, error) {
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	// Both operands are read before the loop, in the order the interpreter
	// measures them, so a program whose Size: fails and whose Step: would also
	// fail reports the same one in both backends.
	size, err := g.measuredOperand(n, in, "size", "Size", 1)
	if err != nil {
		return "", err
	}
	step, err := g.measuredOperand(n, in, "step", "Step", 1)
	if err != nil {
		return "", err
	}
	v, i := g.fresh("v"), g.fresh("i")
	g.wl("%s := [][]%s{}", v, elemGo)
	g.wl("for %s := int64(0); %s+%s <= int64(len(%s)); %s += %s {", i, i, size, in, i, step)
	g.in()
	g.wl("%s = append(%s, append([]%s(nil), %s[%s:%s+%s]...))", v, v, elemGo, in, i, i, size)
	g.out()
	g.wl("}")
	return v, nil
}

// emitWindowedReduce lowers the optimizer's Window + Map Each (sum/max/min)
// streaming rewrite (optimizer.fuseWindowReduce): prefix sums inline for sum,
// the shared monotonic-deque helper for max/min.
func (g *gen) emitWindowedReduce(n *ir.Node, in string) (string, error) {
	op, _ := n.Meta["op"].(string)
	size, err := g.measuredOperand(n, in, "size", "Size", 1)
	if err != nil {
		return "", err
	}
	step, err := g.measuredOperand(n, in, "step", "Step", 1)
	if err != nil {
		return "", err
	}
	v := g.fresh("v")
	switch op {
	case "sum":
		pre, i, x := g.fresh("pre"), g.fresh("i"), g.fresh("x")
		g.wl("%s := make([]int64, len(%s)+1)", pre, in)
		g.wl("for %s, %s := range %s {", i, x, in)
		g.in()
		g.wl("%s[%s+1] = %s[%s] + %s", pre, i, pre, i, x)
		g.out()
		g.wl("}")
		g.wl("%s := []int64{}", v)
		g.wl("for %s := int64(0); %s+%s <= int64(len(%s)); %s += %s {", i, i, size, in, i, step)
		g.in()
		g.wl("%s = append(%s, %s[%s+%s]-%s[%s])", v, v, pre, i, size, pre, i)
		g.out()
		g.wl("}")
		return v, nil
	case "max", "min":
		g.helper("dmSlidingExtremum", declSlidingExtremum)
		g.wl("%s := dmSlidingExtremum(%s, %s, %s, %v)", v, in, size, step, op == "min")
		return v, nil
	case "product":
		// No prefix trick survives a zero, so this is the honest per-window
		// scan — it still never materializes the windows.
		i, p, x := g.fresh("i"), g.fresh("p"), g.fresh("x")
		g.wl("%s := []int64{}", v)
		g.wl("for %s := int64(0); %s+%s <= int64(len(%s)); %s += %s {", i, i, size, in, i, step)
		g.in()
		g.wl("%s := int64(1)", p)
		g.wl("for _, %s := range %s[%s : %s+%s] {", x, in, i, i, size)
		g.in()
		g.wl("%s *= %s", p, x)
		g.out()
		g.wl("}")
		g.wl("%s = append(%s, %s)", v, v, p)
		g.out()
		g.wl("}")
		return v, nil
	}
	return "", unsupported(n, "unknown windowed reduction %q", op)
}

// emitFlatMapCountBy lowers Map Each(x -> list(e1..eN)) + Flatten + Count By
// into one loop that increments the count map for each expanded element in
// place — no per-element list, no flattened list. Elements are visited in the
// same order (input order, then literal order) as the unfused path, so the
// resulting dmMap's key insertion order and values are byte-identical.
func (g *gen) emitFlatMapCountBy(mapNode, countNode *ir.Node, listArgs []ast.Expr, in string) (string, error) {
	mapLam, err := g.nodeLambda(mapNode)
	if err != nil {
		return "", err
	}
	countLam, err := g.nodeLambda(countNode)
	if err != nil {
		return "", err
	}
	keyGo, err := g.goType(countNode.Out.Key)
	if err != nil {
		return "", unsupported(countNode, "%v", err)
	}
	elemT := countNode.In.Elem
	g.helper("dmMap", declMap)
	g.helper("dmBump", declMapBump)
	v, e := g.fresh("v"), g.fresh("e")
	g.wl("%s := dmNewMap[%s, int64]()", v, keyGo)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	mapEnv := exprEnv{mapLam.Params[0]: {expr: e, typ: mapNode.In.Elem}}
	for _, arg := range listArgs {
		nbExpr, _, err := g.compileExpr(arg, mapEnv)
		if err != nil {
			return "", unsupported(mapNode, "lambda: %v", err)
		}
		nb := g.fresh("nb")
		g.wl("%s := %s", nb, nbExpr)
		kBody, _, err := g.compileExpr(countLam.Body, exprEnv{countLam.Params[0]: {expr: nb, typ: elemT}})
		if err != nil {
			return "", unsupported(countNode, "lambda: %v", err)
		}
		k := g.fresh("k")
		g.wl("%s := %s", k, kBody)
		g.wl("dmBump(&%s, %s, 1)", v, k)
	}
	g.out()
	g.wl("}")
	return v, nil
}

// emitDigitGridToGrid lowers Split Each("") + Convert To Integers + Convert To
// Grid into a dmGrid[int64] whose flat cell slice is filled directly from the
// input line bytes — no [][]string, no [][]int64, no flatten copy. Non-digit
// characters and ragged rows abort exactly as the unfused path would (both
// dmFail; the wording differs, but only on inputs the pipeline rejects).
func (g *gen) emitDigitGridToGrid(in string) string {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmGrid", declGrid)
	v := g.fresh("v")
	r, line, c := g.fresh("r"), g.fresh("line"), g.fresh("c")
	b := g.fresh("b")
	g.wl("var %s dmGrid[int64]", v)
	g.wl("if len(%s) > 0 {", in)
	g.in()
	g.wl("%s.rows, %s.cols = len(%s), len(%s[0])", v, v, in, in)
	g.wl("%s.cells = make([]int64, %s.rows*%s.cols)", v, v, v)
	g.wl("for %s, %s := range %s {", r, line, in)
	g.in()
	g.wl("if len(%s) != %s.cols {", line, v)
	g.in()
	g.wl(`dmFail("grid is not rectangular: row %%d has %%d cells, expected %%d", %s, len(%s), %s.cols)`, r, line, v)
	g.out()
	g.wl("}")
	g.wl("for %s := 0; %s < len(%s); %s++ {", c, c, line, c)
	g.in()
	g.wl("%s := %s[%s]", b, line, c)
	g.wl("if %s < '0' || %s > '9' {", b, b)
	g.in()
	g.wl(`dmFail("Convert To Integers: %%q is not an integer", string(%s))`, b)
	g.out()
	g.wl("}")
	g.wl("%s.cells[%s*%s.cols+%s] = int64(%s-'0')", v, r, v, c, b)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v
}

// emitDigitGrid lowers a fused Split Each("") + Convert To Integers over
// []string lines into [][]int64 without materializing the per-rune substrings.
// The common case (a line of ASCII digits) parses byte-by-byte; any other line
// falls back to the exact strings.Split + dmParseInt path for identical results.
func (g *gen) emitDigitGrid(in string) string {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	g.imp("strings")
	v := g.fresh("v")
	i, line := g.fresh("i"), g.fresh("line")
	k, fast := g.fresh("k"), g.fresh("fast")
	row := g.fresh("row")
	j, s := g.fresh("j"), g.fresh("s")
	parts := g.fresh("parts")
	g.wl("%s := make([][]int64, len(%s))", v, in)
	g.wl("for %s, %s := range %s {", i, line, in)
	g.in()
	g.wl("%s := true", fast)
	g.wl("for %s := 0; %s < len(%s); %s++ {", k, k, line, k)
	g.in()
	g.wl("if %s[%s] < '0' || %s[%s] > '9' { %s = false; break }", line, k, line, k, fast)
	g.out()
	g.wl("}")
	g.wl("if %s {", fast)
	g.in()
	g.wl("%s := make([]int64, len(%s))", row, line)
	g.wl("for %s := 0; %s < len(%s); %s++ { %s[%s] = int64(%s[%s]-'0') }", k, k, line, k, row, k, line, k)
	g.wl("%s[%s] = %s", v, i, row)
	g.out()
	g.wl("} else {")
	g.in()
	g.wl("%s := strings.Split(%s, \"\")", parts, line)
	g.wl("%s := make([]int64, len(%s))", row, parts)
	g.wl("for %s, %s := range %s { %s[%s] = dmParseIntSeg(%s) }", j, s, parts, row, j, s)
	g.wl("%s[%s] = %s", v, i, row)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v
}

// emitSplitScan walks a string's segments for a single-byte separator, calling
// body once per segment with the expression naming it. Four emitters shared
// this loop by copy; they share it by call now.
//
// The index stays strictly inside the string. The shape this replaced —
// `for i := 0; i <= len(s); i++ { if i == len(s) || s[i] == sep }` — reads
// s[i] under a condition the prover cannot combine with the loop bound, so the
// byte load kept a bounds check it did not need; `-d=ssa/check_bce/debug=1`
// reports IsInBounds on that line before and not after. Emitting the final
// segment after the loop rather than on a phantom last lap produces exactly
// the same segments, including the lone empty one for the empty string.
//
// Measured, the elimination is worth nothing on the benchmarks (±1%, inside
// the noise): the loop is memory-bound and the removed compare predicts
// perfectly. It is kept for the shared shape, not for a speedup.
func (g *gen) emitSplitScan(sep byte, in string, body func(seg string)) {
	start, i := g.fresh("start"), g.fresh("i")
	g.wl("%s := 0", start)
	g.wl("for %s := 0; %s < len(%s); %s++ {", i, i, in, i)
	g.in()
	g.wl("if %s[%s] == %s {", in, i, goByte(sep))
	g.in()
	body(fmt.Sprintf("%s[%s:%s]", in, start, i))
	g.wl("%s = %s + 1", start, i)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	body(fmt.Sprintf("%s[%s:]", in, start))
}

// emitSplitInts lowers Split(single-byte sep) + Convert To Integers over a
// string into []int64 by scanning for the separator and parsing each segment
// via dmParseInt — reproducing strings.Split + per-element parse exactly (the
// same segments, including the lone empty segment for an empty string) without
// materializing the []string.
func (g *gen) emitSplitInts(sep byte, in string) string {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	v := g.fresh("v")
	g.wl("%s := make([]int64, 0, len(%s)/2+1)", v, in)
	// Fast inline parse of the clean segment; dmParseIntSeg handles surrounding
	// whitespace / overflow exactly as dmParseInt(TrimSpace) would.
	g.emitSplitScan(sep, in, func(seg string) {
		g.wl("%s = append(%s, dmParseIntSeg(%s))", v, v, seg)
	})
	return v
}

// emitFieldsUnionCount streams Split Fields + Convert To Integers + Union +
// Count into a distinct-integer count over one map — no [][]int64, no per-group
// set, no ordered elems slice. Union+Count is exactly the number of distinct
// elements across all groups, which is order-independent.
func (g *gen) emitFieldsUnionCount(sep, in string) string {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	g.helper("dmParseFieldsInt", declParseFieldsInt)
	g.imp("strings")
	seen, buf, x := g.fresh("seen"), g.fresh("buf"), g.fresh("x")
	ok, fields, s := g.fresh("ok"), g.fresh("fields"), g.fresh("s")
	emitBody := func(line string) {
		g.wl("if r, %s := dmParseFieldsIntInto(%s, %s); %s {", ok, line, buf, ok)
		g.in()
		g.wl("%s = r", buf)
		g.out()
		g.wl("} else {")
		g.in()
		g.wl("%s := strings.Fields(%s)", fields, line)
		g.wl("%s = %s[:0]", buf, buf)
		g.wl("for _, %s := range %s { %s = append(%s, dmParseIntSeg(%s)) }", s, fields, buf, buf, s)
		g.out()
		g.wl("}")
		g.wl("for _, %s := range %s { %s[%s] = struct{}{} }", x, buf, seen, x)
	}
	g.wl("%s := make(map[int64]struct{})", seen)
	g.wl("var %s []int64", buf)
	if sep == "" {
		line := g.fresh("line")
		g.wl("for _, %s := range %s {", line, in)
		g.in()
		emitBody(line)
		g.out()
		g.wl("}")
	} else {
		str, idx, line := g.fresh("str"), g.fresh("idx"), g.fresh("line")
		g.wl("%s := %s", str, in)
		g.wl("for {")
		g.in()
		if len(sep) == 1 {
			g.wl("%s := strings.IndexByte(%s, %q)", idx, str, sep[0])
		} else {
			g.wl("%s := strings.Index(%s, %s)", idx, str, goStr(sep))
		}
		g.wl("%s := %s", line, str)
		g.wl("if %s >= 0 { %s = %s[:%s] }", idx, line, str, idx)
		emitBody(line)
		g.wl("if %s < 0 { break }", idx)
		g.wl("%s = %s[%s+%d:]", str, str, idx, len(sep))
		g.out()
		g.wl("}")
	}
	v := g.fresh("v")
	g.wl("%s := int64(len(%s))", v, seen)
	return v
}

// emitFieldsKeyedExtremum streams Split Fields + Convert To Integers +
// (Max By | Min By): each line's integers are parsed into a reused buffer, the
// key lambda is applied, and only the current winner is copied out — no
// [][]int64. sep == "" consumes []string lines; sep != "" walks the raw string.
func (g *gen) emitFieldsKeyedExtremum(sep string, extNode *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	g.helper("dmParseFieldsInt", declParseFieldsInt)
	g.imp("strings")
	lam, err := g.nodeLambda(extNode)
	if err != nil {
		return "", err
	}
	cmp := "<"
	if extNode.Prim == "Max By" {
		cmp = ">"
	}
	buf := g.fresh("buf")
	kbody, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: buf, typ: extNode.In.Elem}})
	if err != nil {
		return "", unsupported(extNode, "lambda: %v", err)
	}
	best, bestK, found := g.fresh("best"), g.fresh("bestK"), g.fresh("found")
	ok, fields, s, k := g.fresh("ok"), g.fresh("fields"), g.fresh("s"), g.fresh("k")
	emitBody := func(line string) {
		g.wl("if r, %s := dmParseFieldsIntInto(%s, %s); %s {", ok, line, buf, ok)
		g.in()
		g.wl("%s = r", buf)
		g.out()
		g.wl("} else {")
		g.in()
		g.wl("%s := strings.Fields(%s)", fields, line)
		g.wl("%s = %s[:0]", buf, buf)
		g.wl("for _, %s := range %s { %s = append(%s, dmParseIntSeg(%s)) }", s, fields, buf, buf, s)
		g.out()
		g.wl("}")
		g.wl("%s := %s", k, kbody)
		g.wl("if !%s || %s %s %s {", found, k, cmp, bestK)
		g.in()
		g.wl("%s = append(%s[:0], %s...)", best, best, buf)
		g.wl("%s = %s", bestK, k)
		g.wl("%s = true", found)
		g.out()
		g.wl("}")
	}
	g.wl("var %s []int64", buf)
	g.wl("var %s []int64", best)
	g.wl("var %s int64", bestK)
	g.wl("%s := false", found)
	if sep == "" {
		line := g.fresh("line")
		g.wl("for _, %s := range %s {", line, in)
		g.in()
		emitBody(line)
		g.out()
		g.wl("}")
	} else {
		str, idx, line := g.fresh("str"), g.fresh("idx"), g.fresh("line")
		g.wl("%s := %s", str, in)
		g.wl("for {")
		g.in()
		if len(sep) == 1 {
			g.wl("%s := strings.IndexByte(%s, %q)", idx, str, sep[0])
		} else {
			g.wl("%s := strings.Index(%s, %s)", idx, str, goStr(sep))
		}
		g.wl("%s := %s", line, str)
		g.wl("if %s >= 0 { %s = %s[:%s] }", idx, line, str, idx)
		emitBody(line)
		g.wl("if %s < 0 { break }", idx)
		g.wl("%s = %s[%s+%d:]", str, str, idx, len(sep))
		g.out()
		g.wl("}")
	}
	g.wl("if !%s {", found)
	g.in()
	g.wl(`dmFail("%s of an empty list is undefined")`, extNode.Prim)
	g.out()
	g.wl("}")
	return best, nil
}

// emitFieldsMapSum streams Split Fields + Convert To Integers + Map Each + Sum.
// When sep == "" the input is already-split []string lines; when sep != "" the
// input is the un-split string and the loop walks it exactly as
// strings.Split(in, sep) would, so the []string is skipped too.
func (g *gen) emitFieldsMapSum(sep string, mapNode, sumNode *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	g.helper("dmParseFieldsInt", declParseFieldsInt)
	g.imp("strings")
	lam, err := g.nodeLambda(mapNode)
	if err != nil {
		return "", err
	}
	acc, err := g.goType(sumNode.Out)
	if err != nil {
		return "", unsupported(sumNode, "%v", err)
	}
	// The map lambda's parameter is one line's integer list; bind it to the
	// reused buffer. mapNode.In.Elem is that List<Int> element type.
	buf := g.fresh("buf")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: buf, typ: mapNode.In.Elem}})
	if err != nil {
		return "", unsupported(mapNode, "lambda: %v", err)
	}
	v := g.fresh("v")
	ok := g.fresh("ok")
	fields, s := g.fresh("fields"), g.fresh("s")
	emitBody := func(line string) {
		g.wl("if r, %s := dmParseFieldsIntInto(%s, %s); %s {", ok, line, buf, ok)
		g.in()
		g.wl("%s = r", buf)
		g.out()
		g.wl("} else {")
		g.in()
		g.wl("%s := strings.Fields(%s)", fields, line)
		g.wl("%s = %s[:0]", buf, buf)
		g.wl("for _, %s := range %s { %s = append(%s, dmParseIntSeg(%s)) }", s, fields, buf, buf, s)
		g.out()
		g.wl("}")
		g.wl("%s += %s", v, body)
	}
	g.wl("var %s %s", v, acc)
	g.wl("var %s []int64", buf)
	if sep == "" {
		line := g.fresh("line")
		g.wl("for _, %s := range %s {", line, in)
		g.in()
		emitBody(line)
		g.out()
		g.wl("}")
		return v, nil
	}
	str, idx, line := g.fresh("str"), g.fresh("idx"), g.fresh("line")
	g.wl("%s := %s", str, in)
	g.wl("for {")
	g.in()
	if len(sep) == 1 {
		g.wl("%s := strings.IndexByte(%s, %q)", idx, str, sep[0])
	} else {
		g.wl("%s := strings.Index(%s, %s)", idx, str, goStr(sep))
	}
	g.wl("%s := %s", line, str)
	g.wl("if %s >= 0 { %s = %s[:%s] }", idx, line, str, idx)
	emitBody(line)
	g.wl("if %s < 0 { break }", idx)
	g.wl("%s = %s[%s+%d:]", str, str, idx, len(sep))
	g.out()
	g.wl("}")
	return v, nil
}

// emitFieldsInts lowers a fused Split Fields + Convert To Integers over
// []string lines into [][]int64 without materializing the per-line []string.
func (g *gen) emitFieldsInts(in string) string {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	g.helper("dmParseFieldsInt", declParseFieldsInt)
	g.imp("strings")
	v := g.fresh("v")
	i, line := g.fresh("i"), g.fresh("line")
	r, ok := g.fresh("r"), g.fresh("ok")
	fields, row := g.fresh("fields"), g.fresh("row")
	j, s := g.fresh("j"), g.fresh("s")
	g.wl("%s := make([][]int64, len(%s))", v, in)
	g.wl("for %s, %s := range %s {", i, line, in)
	g.in()
	g.wl("if %s, %s := dmParseFieldsInt(%s); %s {", r, ok, line, ok)
	g.in()
	g.wl("%s[%s] = %s", v, i, r)
	g.out()
	g.wl("} else {")
	g.in()
	g.wl("%s := strings.Fields(%s)", fields, line)
	g.wl("%s := make([]int64, len(%s))", row, fields)
	g.wl("for %s, %s := range %s { %s[%s] = dmParseInt(%s) }", j, s, fields, row, j, s)
	g.wl("%s[%s] = %s", v, i, row)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v
}

// emitSplitIntsEnumerateMapSum lowers Split(sep) + Convert To Integers +
// Enumerate + Map Each(scalar) + Sum into one pass: parse each segment, build
// its (running-index, value) tuple locally, and accumulate the map result — no
// []int64 and no []tuple.
func (g *gen) emitSplitIntsEnumerateMapSum(sep byte, enumNode, mapNode, sumNode *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	tupGo, err := g.goType(enumNode.Out.Elem)
	if err != nil {
		return "", unsupported(enumNode, "%v", err)
	}
	acc, err := g.goType(sumNode.Out)
	if err != nil {
		return "", unsupported(sumNode, "%v", err)
	}
	lam, err := g.nodeLambda(mapNode)
	if err != nil {
		return "", err
	}
	v, n := g.fresh("v"), g.fresh("n")
	e, p := g.fresh("e"), g.fresh("p")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: p, typ: mapNode.In.Elem}})
	if err != nil {
		return "", unsupported(mapNode, "lambda: %v", err)
	}
	g.wl("var %s %s", v, acc)
	g.wl("%s := 0", n) // running Enumerate index
	g.emitSplitScan(sep, in, func(seg string) {
		g.wl("%s := dmParseIntSeg(%s)", e, seg)
		g.wl("%s := %s{int64(%s), %s}", p, tupGo, n, e)
		g.wl("%s += %s", v, body)
		g.wl("%s++", n)
	})
	return v, nil
}

// emitSplitIntsPairCount lowers Split(sep) + Convert To Integers +
// HashSetPairScan(Count) into a one-pass O(n) complement count over the parse:
// no []int64, and the map grows to the real key cardinality rather than being
// presized to the element count.
func (g *gen) emitSplitIntsPairCount(sep byte, scanNode *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	target, ok := scanNode.Meta["target"].(int64)
	if !ok {
		return "", unsupported(scanNode, "missing target metadata")
	}
	v, seen, x := g.fresh("v"), g.fresh("seen"), g.fresh("x")
	g.wl("%s := make(map[int64]int64)", seen)
	g.wl("var %s int64", v)
	g.emitSplitScan(sep, in, func(seg string) {
		g.wl("%s := dmParseIntSeg(%s)", x, seg)
		g.wl("%s += %s[%d-%s]", v, seen, target, x)
		g.wl("%s[%s]++", seen, x)
	})
	return v, nil
}

// emitSplitIntsFold lowers Split(single-byte sep) + Convert To Integers +
// Fold(int seed) into one pass: scan for the separator, parse each segment, and
// fold it into the accumulator — no intermediate []int64. A left fold's visit
// order is the parse order, which is exactly the walk emitSplitInts emits.
func (g *gen) emitSplitIntsFold(sep byte, foldNode *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	lam, err := g.nodeLambda(foldNode)
	if err != nil {
		return "", err
	}
	seed, _ := foldNode.Meta["seed"].(int64)
	acc, e := g.fresh("acc"), g.fresh("e")
	g.wl("%s := int64(%d)", acc, seed)
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: acc, typ: foldNode.Out},
		lam.Params[1]: {expr: e, typ: foldNode.In.Elem},
	})
	if err != nil {
		return "", unsupported(foldNode, "lambda: %v", err)
	}
	g.emitSplitScan(sep, in, func(seg string) {
		g.wl("%s := dmParseIntSeg(%s)", e, seg)
		g.wl("%s = %s", acc, body)
	})
	return acc, nil
}

// emitWindowedReduceSum lowers WindowedReduce immediately followed by Sum: the
// per-window extremum/sum is accumulated into a scalar, never a []int64.
func (g *gen) emitWindowedReduceSum(n *ir.Node, in string) (string, error) {
	op, _ := n.Meta["op"].(string)
	size, err := g.measuredOperand(n, in, "size", "Size", 1)
	if err != nil {
		return "", err
	}
	step, err := g.measuredOperand(n, in, "step", "Step", 1)
	if err != nil {
		return "", err
	}
	v := g.fresh("v")
	switch op {
	case "sum":
		pre, i, x := g.fresh("pre"), g.fresh("i"), g.fresh("x")
		g.wl("%s := make([]int64, len(%s)+1)", pre, in)
		g.wl("for %s, %s := range %s {", i, x, in)
		g.in()
		g.wl("%s[%s+1] = %s[%s] + %s", pre, i, pre, i, x)
		g.out()
		g.wl("}")
		g.wl("var %s int64", v)
		g.wl("for %s := int64(0); %s+%s <= int64(len(%s)); %s += %s {", i, i, size, in, i, step)
		g.in()
		g.wl("%s += %s[%s+%s]-%s[%s]", v, pre, i, size, pre, i)
		g.out()
		g.wl("}")
		return v, nil
	case "max", "min":
		g.helper("dmSlidingExtremumSum", declSlidingExtremumSum)
		g.wl("%s := dmSlidingExtremumSum(%s, %s, %s, %v)", v, in, size, step, op == "min")
		return v, nil
	case "product":
		i, p, x := g.fresh("i"), g.fresh("p"), g.fresh("x")
		g.wl("var %s int64", v)
		g.wl("for %s := int64(0); %s+%s <= int64(len(%s)); %s += %s {", i, i, size, in, i, step)
		g.in()
		g.wl("%s := int64(1)", p)
		g.wl("for _, %s := range %s[%s : %s+%s] {", x, in, i, i, size)
		g.in()
		g.wl("%s *= %s", p, x)
		g.out()
		g.wl("}")
		g.wl("%s += %s", v, p)
		g.out()
		g.wl("}")
		return v, nil
	}
	return "", unsupported(n, "unknown windowed reduction %q", op)
}

func (g *gen) emitFlatten(n *ir.Node, in string) (string, error) {
	elemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, grp, total := g.fresh("v"), g.fresh("grp"), g.fresh("total")
	// The exact length is one pass over the outer list away, and the outer
	// list is orders of magnitude shorter than what it flattens to.
	g.wl("%s := 0", total)
	g.wl("for _, %s := range %s { %s += len(%s) }", grp, in, total, grp)
	g.wl("%s := make([]%s, 0, %s)", v, elemGo, total)
	g.wl("for _, %s := range %s {", grp, in)
	g.in()
	g.wl("%s = append(%s, %s...)", v, v, grp)
	g.out()
	g.wl("}")
	return v, nil
}

// emitEnumerateMapSum lowers Enumerate + Map Each(scalar) + Sum into one loop
// that constructs each (index, value) tuple locally and accumulates the map
// lambda's result — no []tuple is materialized.
func (g *gen) emitEnumerateMapSum(enumNode, mapNode, sumNode *ir.Node, in string) (string, error) {
	tupGo, err := g.goType(enumNode.Out.Elem)
	if err != nil {
		return "", unsupported(enumNode, "%v", err)
	}
	acc, err := g.goType(sumNode.Out)
	if err != nil {
		return "", unsupported(sumNode, "%v", err)
	}
	lam, err := g.nodeLambda(mapNode)
	if err != nil {
		return "", err
	}
	v, i, e, p := g.fresh("v"), g.fresh("i"), g.fresh("e"), g.fresh("p")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: p, typ: mapNode.In.Elem}})
	if err != nil {
		return "", unsupported(mapNode, "lambda: %v", err)
	}
	g.wl("var %s %s", v, acc)
	g.wl("for %s, %s := range %s {", i, e, in)
	g.in()
	g.wl("%s := %s{int64(%s), %s}", p, tupGo, i, e)
	g.wl("%s += %s", v, body)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitEnumerate(n *ir.Node, in string) (string, error) {
	tupGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, i, e := g.fresh("v"), g.fresh("i"), g.fresh("e")
	g.wl("%s := make([]%s, len(%s))", v, tupGo, in)
	g.wl("for %s, %s := range %s {", i, e, in)
	g.in()
	g.wl("%s[%s] = %s{int64(%s), %s}", v, i, tupGo, i, e)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitCountBy(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	keyGo, err := g.goType(n.Out.Key)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	e := g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.helper("dmMap", declMap)
	g.helper("dmBump", declMapBump)
	v, k := g.fresh("v"), g.fresh("k")
	g.wl("%s := dmNewMap[%s, int64]()", v, keyGo)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.wl("%s := %s", k, body)
	g.wl("dmBump(&%s, %s, 1)", v, k)
	g.out()
	g.wl("}")
	return v, nil
}

// emitKeyedExtremum lowers Min By / Max By: linear scan tracking the best
// key, first element winning ties.
func (g *gen) emitKeyedExtremum(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	elemGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	e := g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	cmp := "<"
	if n.Prim == "Max By" {
		cmp = ">"
	}
	g.helper("dmFail", declFail, "fmt", "os")
	v, bestK, i, k := g.fresh("v"), g.fresh("bestK"), g.fresh("i"), g.fresh("k")
	g.wl("if len(%s) == 0 {", in)
	g.in()
	g.wl(`dmFail("%s of an empty list is undefined")`, n.Prim)
	g.out()
	g.wl("}")
	g.wl("var %s %s", v, elemGo)
	g.wl("var %s int64", bestK)
	g.wl("for %s, %s := range %s {", i, e, in)
	g.in()
	g.wl("%s := %s", k, body)
	g.wl("if %s == 0 || %s %s %s {", i, k, cmp, bestK)
	g.in()
	g.wl("%s, %s = %s, %s", v, bestK, e, k)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitSortBy(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	desc, _ := n.Meta["desc"].(bool)
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	e := g.fresh("e")
	body, keyT, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	// The key may be Int, Float, Text, or a tuple of them — a tuple key is how
	// a tiebreak is written, and it compares lexicographically.
	keyGo, err := g.goType(keyT)
	if err != nil {
		return "", unsupported(n, "key: %v", err)
	}
	g.imp("slices")
	// Sort (key, original-index) pairs with an unstable pdqsort (slices.SortFunc,
	// no reflection): the ascending index tiebreak makes (key, idx) a strict
	// total order, so the result is the byte-identical permutation a *stable*
	// key sort produces — for both ascending and descending key order the equal-
	// key run keeps original order. Then materialize the permutation.
	kv, pairs, i := g.fresh("kv"), g.fresh("pairs"), g.fresh("i")
	g.wl("type %s struct { k %s; i int }", kv, keyGo)
	g.wl("%s := make([]%s, len(%s))", pairs, kv, in)
	g.wl("for %s, %s := range %s {", i, e, in)
	g.in()
	g.wl("%s[%s] = %s{%s, %s}", pairs, i, kv, body, i)
	g.out()
	g.wl("}")
	a, b := g.fresh("a"), g.fresh("b")
	lo, hi := a+".k", b+".k"
	if desc {
		lo, hi = hi, lo
	}
	keyLess, err := lessExpr(keyT, lo, hi)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	keyMore, err := lessExpr(keyT, hi, lo)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.wl("slices.SortFunc(%s, func(%s, %s %s) int {", pairs, a, b, kv)
	g.in()
	g.wl("if %s { return -1 }", keyLess)
	g.wl("if %s { return 1 }", keyMore)
	g.wl("if %s.i < %s.i { return -1 }", a, b)
	g.wl("if %s.i > %s.i { return 1 }", a, b)
	g.wl("return 0")
	g.out()
	g.wl("})")
	out := g.fresh("out")
	g.wl("%s := make([]%s, len(%s))", out, elemGo, in)
	g.wl("for %s, p := range %s {", i, pairs)
	g.in()
	g.wl("%s[%s] = %s[p.i]", out, i, in)
	g.out()
	g.wl("}")
	return out, nil
}

// emitDifferenceAll lowers the standalone Difference reduction: the first
// group minus the union of the rest, in the first group's order.
func (g *gen) emitDifferenceAll(n *ir.Node, in string) (string, error) {
	elemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmSet", declSet)
	v, rest := g.fresh("v"), g.fresh("rest")
	g.wl("%s := dmNewSet[%s]()", v, elemGo)
	g.wl("%s := dmNewSet[%s]()", rest, elemGo)
	g.wl("if len(%s) > 0 {", in)
	g.in()
	grp, e := g.fresh("grp"), g.fresh("e")
	g.wl("for _, %s := range %s[1:] {", grp, in)
	g.in()
	g.wl("for _, %s := range %s {", e, grp)
	g.in()
	g.wl("%s.add(%s)", rest, e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	e2 := g.fresh("e")
	g.wl("for _, %s := range %s[0] {", e2, in)
	g.in()
	g.wl("if !%s.contains(%s) {", rest, e2)
	g.in()
	g.wl("%s.add(%s)", v, e2)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

// emitZip pairs two compiled channel lists into a tuple slice.
func (g *gen) emitZip(n *ir.Node, in string) (string, error) {
	froms, _ := n.Meta["from"].([]string)
	if len(froms) != 2 {
		return "", unsupported(n, "missing channel metadata")
	}
	av, ok := g.chans[froms[0]]
	if !ok {
		return "", unsupported(n, "channel %q was not compiled", froms[0])
	}
	bv, ok := g.chans[froms[1]]
	if !ok {
		return "", unsupported(n, "channel %q was not compiled", froms[1])
	}
	tupGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, m, i := g.fresh("v"), g.fresh("m"), g.fresh("i")
	g.wl("%s := len(%s)", m, av.v)
	g.wl("if len(%s) < %s {", bv.v, m)
	g.in()
	g.wl("%s = len(%s)", m, bv.v)
	g.out()
	g.wl("}")
	g.wl("%s := make([]%s, %s)", v, tupGo, m)
	g.wl("for %s := 0; %s < %s; %s++ {", i, i, m, i)
	g.in()
	g.wl("%s[%s] = %s{%s[%s], %s[%s]}", v, i, tupGo, av.v, i, bv.v, i)
	g.out()
	g.wl("}")
	return v, nil
}
