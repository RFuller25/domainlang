package codegen

import (
	"domain/ir"
	"domain/prims"
)

// Lowerings for the AoC-toolbox primitives (B.f5): Extract Integers, Split
// Fields, Convert To Set, Find Cells, Merge Ranges, Permutations, Subsets.
// Each mirrors its interpreter twin in prims/toolbox.go / prims/grid.go —
// success output must stay byte-identical (the oracle tests compare).

// declExtractInts mirrors prims.extractInts: every integer in the text, with
// a '-' that directly follows a digit demoted to a separator ("36-92" is 36
// and 92, "x=-5" is -5).
const declExtractInts = `func dmExtractInts(s string) []int64 {
	out := []int64{}
	for i := 0; i < len(s); {
		start := i
		if s[i] == '-' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' &&
			(i == 0 || s[i-1] < '0' || s[i-1] > '9') {
			i++
		} else if s[i] < '0' || s[i] > '9' {
			i++
			continue
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		n, err := strconv.ParseInt(s[start:i], 10, 64)
		if err != nil {
			dmFail("%q overflows Int", s[start:i])
		}
		out = append(out, n)
	}
	return out
}`

func (g *gen) emitExtractIntegers(n *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmExtractInts", declExtractInts, "strconv")
	v := g.fresh("v")
	if n.In.Equal(ir.Text()) {
		g.wl("%s := dmExtractInts(%s)", v, in)
		return v, nil
	}
	line := g.fresh("line")
	g.wl("%s := make([][]int64, 0, len(%s))", v, in)
	g.wl("for _, %s := range %s {", line, in)
	g.in()
	g.wl("%s = append(%s, dmExtractInts(%s))", v, v, line)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitSplitFields(n *ir.Node, in string) (string, error) {
	g.imp("strings")
	v := g.fresh("v")
	if n.In.Equal(ir.Text()) {
		g.wl("%s := strings.Fields(%s)", v, in)
		return v, nil
	}
	line := g.fresh("line")
	g.wl("%s := make([][]string, 0, len(%s))", v, in)
	g.wl("for _, %s := range %s {", line, in)
	g.in()
	g.wl("%s = append(%s, strings.Fields(%s))", v, v, line)
	g.out()
	g.wl("}")
	return v, nil
}

// declRaggedCols mirrors prims.raggedColumns: rune columns of unpadded lines,
// skipping the cells short lines don't have.
const declRaggedCols = `func dmRaggedCols(lines []string) [][]string {
	rows := make([][]string, len(lines))
	width := 0
	for i, s := range lines {
		var runes []string
		for _, r := range s {
			runes = append(runes, string(r))
		}
		rows[i] = runes
		if len(runes) > width {
			width = len(runes)
		}
	}
	out := make([][]string, width)
	for c := 0; c < width; c++ {
		col := []string{}
		for _, runes := range rows {
			if c < len(runes) {
				col = append(col, runes[c])
			}
		}
		out[c] = col
	}
	return out
}`

func (g *gen) emitRaggedColumns(n *ir.Node, in string) (string, error) {
	g.helper("dmRaggedCols", declRaggedCols)
	v := g.fresh("v")
	g.wl("%s := dmRaggedCols(%s)", v, in)
	return v, nil
}

func (g *gen) emitJoin(n *ir.Node, in string) (string, error) {
	sep, _ := n.Meta["sep"].(string)
	g.imp("strings")
	v := g.fresh("v")
	g.wl("%s := strings.Join(%s, %s)", v, in, goStr(sep))
	return v, nil
}

// emitFoldOver lowers `Fold From: channel`: fold over the channel's compiled
// list with the current pipeline value as the seed.
func (g *gen) emitFoldOver(n *ir.Node, in string) (string, error) {
	froms, _ := n.Meta["from"].([]string)
	if len(froms) != 1 {
		return "", unsupported(n, "missing channel metadata")
	}
	cv, ok := g.chans[froms[0]]
	if !ok {
		return "", unsupported(n, "channel %q was not compiled", froms[0])
	}
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	accGo, err := g.goType(n.Out)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	acc, e := g.fresh("acc"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: acc, typ: n.Out},
		lam.Params[1]: {expr: e, typ: cv.typ.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("var %s %s = %s", acc, accGo, in)
	g.wl("for _, %s := range %s {", e, cv.v)
	g.in()
	g.wl("%s = %s", acc, body)
	g.out()
	g.wl("}")
	return acc, nil
}

func (g *gen) emitConvertToSet(n *ir.Node, in string) (string, error) {
	elemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmSet", declSet)
	v, e := g.fresh("v"), g.fresh("e")
	g.wl("%s := dmNewSet[%s]()", v, elemGo)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.wl("%s.add(%s)", v, e)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitFindCells(n *ir.Node, in string) (string, error) {
	if n.In.Kind == ir.KSparse {
		return g.emitFindCellsSparse(n, in)
	}
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	pt, err := g.pointGo()
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, r, c, e := g.fresh("v"), g.fresh("r"), g.fresh("c"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := []%s{}", v, pt)
	g.wl("for %s := int64(0); %s < int64(%s.rows); %s++ {", r, r, in, r)
	g.in()
	g.wl("for %s := int64(0); %s < int64(%s.cols); %s++ {", c, c, in, c)
	g.in()
	g.wl("%s := %s.cells[%s*int64(%s.cols)+%s]", e, in, r, in, c)
	g.wl("if %s {", body)
	g.in()
	g.wl("%s = append(%s, %s{%s, %s})", v, v, pt, r, c)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

// emitMergeRanges handles the three accepted shapes — (Int, Int) tuples,
// two-int rows ([]int64), and two-Int-field records — normalizing each range
// to a [2]int64 span, merging, and rebuilding the input's element shape.
func (g *gen) emitMergeRanges(n *ir.Node, in string) (string, error) {
	elem := n.In.Elem
	elemGo, err := g.goType(elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.imp("sort")
	g.imp("math")

	spans, i, x := g.fresh("spans"), g.fresh("i"), g.fresh("x")
	lo, hi := g.fresh("lo"), g.fresh("hi")
	g.wl("%s := make([][2]int64, len(%s))", spans, in)
	g.wl("for %s, %s := range %s {", i, x, in)
	g.in()
	switch {
	case elem.Kind == ir.KTuple:
		g.wl("%s, %s := %s.f0, %s.f1", lo, hi, x, x)
	case elem.Kind == ir.KList:
		g.wl("if len(%s) != 2 {", x)
		g.in()
		g.wl(`dmFail("range %%d: expected an (Int, Int) pair", %s)`, i)
		g.out()
		g.wl("}")
		g.wl("%s, %s := %s[0], %s[1]", lo, hi, x, x)
	default: // two-Int-field record
		g.wl("%s, %s := %s.%s, %s.%s", lo, hi,
			x, fieldName(elem.Fields[0].Name), x, fieldName(elem.Fields[1].Name))
	}
	g.wl("if %s > %s {", lo, hi)
	g.in()
	g.wl(`dmFail("range %%d is inverted: %%d > %%d", %s, %s, %s)`, i, lo, hi)
	g.out()
	g.wl("}")
	g.wl("%s[%s] = [2]int64{%s, %s}", spans, i, lo, hi)
	g.out()
	g.wl("}")

	g.wl("sort.Slice(%s, func(i, j int) bool {", spans)
	g.in()
	g.wl("if %s[i][0] != %s[j][0] {", spans, spans)
	g.in()
	g.wl("return %s[i][0] < %s[j][0]", spans, spans)
	g.out()
	g.wl("}")
	g.wl("return %s[i][1] < %s[j][1]", spans, spans)
	g.out()
	g.wl("})")

	merged, s := g.fresh("merged"), g.fresh("s")
	g.wl("%s := [][2]int64{}", merged)
	g.wl("for _, %s := range %s {", s, spans)
	g.in()
	// merged[k-1][1] is an arbitrary int64 upper bound from program input; if
	// it's math.MaxInt64, adding 1 would overflow and wrap to MinInt64,
	// silently breaking the adjacency test. Guard that case explicitly
	// instead of computing the sum.
	g.wl("if k := len(%s); k > 0 && (%s[k-1][1] == math.MaxInt64 || %s[0] <= %s[k-1][1]+1) {", merged, merged, s, merged)
	g.in()
	g.wl("if %s[1] > %s[k-1][1] {", s, merged)
	g.in()
	g.wl("%s[k-1][1] = %s[1]", merged, s)
	g.out()
	g.wl("}")
	g.wl("continue")
	g.out()
	g.wl("}")
	g.wl("%s = append(%s, %s)", merged, merged, s)
	g.out()
	g.wl("}")

	v := g.fresh("v")
	g.wl("%s := make([]%s, len(%s))", v, elemGo, merged)
	g.wl("for %s, %s := range %s {", i, s, merged)
	g.in()
	switch {
	case elem.Kind == ir.KTuple:
		g.wl("%s[%s] = %s{%s[0], %s[1]}", v, i, elemGo, s, s)
	case elem.Kind == ir.KList:
		g.wl("%s[%s] = []int64{%s[0], %s[1]}", v, i, s, s)
	default:
		g.wl("%s[%s] = %s{%s: %s[0], %s: %s[1]}", v, i, elemGo,
			fieldName(elem.Fields[0].Name), s, fieldName(elem.Fields[1].Name), s)
	}
	g.out()
	g.wl("}")
	return v, nil
}

const declPermutations = `func dmPermutations[T any](xs []T, bound int) [][]T {
	if bound > 0 && len(xs) > bound {
		dmFail("refusing to permute %d elements (n! explodes; the bound is %d)", len(xs), bound)
	}
	out := [][]T{}
	perm := make([]T, 0, len(xs))
	used := make([]bool, len(xs))
	var rec func()
	rec = func() {
		if len(perm) == len(xs) {
			out = append(out, append([]T(nil), perm...))
			return
		}
		for i, x := range xs {
			if used[i] {
				continue
			}
			used[i] = true
			perm = append(perm, x)
			rec()
			perm = perm[:len(perm)-1]
			used[i] = false
		}
	}
	rec()
	return out
}`

func (g *gen) emitPermutations(n *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmPermutations", declPermutations)
	v := g.fresh("v")
	g.wl("%s := dmPermutations(%s, %d)", v, in, prims.MaxPermutationInput)
	return v, nil
}

const declSubsets = `func dmSubsets[T any](xs []T, bound int) [][]T {
	if bound > 0 && len(xs) > bound {
		dmFail("refusing the power set of %d elements (2^n explodes; the bound is %d)", len(xs), bound)
	}
	total := 1 << len(xs)
	out := make([][]T, 0, total)
	for mask := 0; mask < total; mask++ {
		sub := []T{}
		for i, x := range xs {
			if mask&(1<<i) != 0 {
				sub = append(sub, x)
			}
		}
		out = append(out, sub)
	}
	return out
}`

func (g *gen) emitSubsets(n *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmSubsets", declSubsets)
	v := g.fresh("v")
	g.wl("%s := dmSubsets(%s, %d)", v, in, prims.MaxSubsetInput)
	return v, nil
}
