package codegen

import (
	"fmt"
	"regexp"
	"strings"

	"domain/ir"
	"domain/pattern"
)

// Match Pattern compilation. Every template gets a generated
// `func dmParseN(s string) (T, bool)`. Templates whose holes are all ints,
// separated by literals a greedy scan cannot mis-split (see fastEligible),
// compile to a hand-rolled scanner — no regexp at runtime. Everything else
// falls back to a package-level compiled regexp with identical semantics.

// tryFuse recognizes parse-then-reduce adjacencies and lowers them to a single
// streaming loop, so the intermediate []string / []Record is never
// materialized. Returns ok=false (nothing consumed) when the shape does not
// match, leaving the ordinary per-node path to run.
//
//	Split(sep) + Match Pattern(Each) + Count Matching  -> walk the string,
//	    parse each line, count inline (no []string, no []Record)
//	Match Pattern(Each) + Count Matching               -> range the []string,
//	    parse + count inline (no []Record)
func (g *gen) tryFuse(nodes []*ir.Node, in string) (int, string, bool, error) {
	// Every rule below keys on nodes[0]'s separator, and reads it with a type
	// assertion whose zero value — the empty string — is a *meaningful*
	// separator here: three of the rules fire on `sep == ""` (the
	// character-split fast paths). A measured separator has no literal to
	// read, so without this guard those three would fire wrongly rather than
	// merely fail to fire. Same rule as the optimizer's hasMeasuredArg: a pass
	// that folds a literal stands down when there is none.
	if len(nodes) > 0 && hasMeasured(nodes[0], "sep") {
		return 0, "", false, nil
	}
	// Mode: Try and Mode: Scan are deliberately not fusible: every fused path
	// below assumes each line parses to exactly one value and calls dmFail when
	// one does not, which is the whole behaviour those two modes exist to
	// replace. Standing the fusion down leaves the ordinary per-node path,
	// which honours them.
	matchEach := func(n *ir.Node) bool {
		if n.Prim != "Match Pattern" {
			return false
		}
		if try, _ := n.Meta["try"].(bool); try {
			return false
		}
		// Scan stands fusion down for the same reason Try does, one step
		// further: a fused loop produces exactly one value per line, and Scan
		// produces however many the line contains — including none.
		if scan, _ := n.Meta["scan"].(bool); scan {
			return false
		}
		each, _ := n.Meta["each"].(bool)
		return each
	}
	if len(nodes) >= 3 && nodes[0].Prim == "Split" && matchEach(nodes[1]) && nodes[2].Prim == "Count Matching" {
		if sep, _ := nodes[0].Meta["sep"].(string); sep != "" {
			out, err := g.emitParseCount(sep, nodes[1], nodes[2], in)
			if err != nil {
				return 0, "", false, err
			}
			return 3, out, true, nil
		}
	}
	if len(nodes) >= 2 && matchEach(nodes[0]) && nodes[1].Prim == "Count Matching" {
		out, err := g.emitParseCount("", nodes[0], nodes[1], in)
		if err != nil {
			return 0, "", false, err
		}
		return 2, out, true, nil
	}
	// Split(single-byte sep) + Convert To Integers + Enumerate + Map Each(scalar)
	// + Sum: parse each int, form its (index, value) tuple, and accumulate in one
	// pass — neither the []int64 nor the []tuple is built.
	if len(nodes) >= 5 && nodes[0].Prim == "Split" && nodes[1].Prim == "Convert To Integers" &&
		nodes[1].In != nil && nodes[1].In.Equal(ir.List(ir.Text())) &&
		nodes[2].Prim == "Enumerate" && nodes[3].Prim == "Map Each" && nodes[4].Prim == "Sum" &&
		nodes[3].Out != nil && nodes[3].Out.Elem != nil &&
		(nodes[3].Out.Elem.Kind == ir.KInt || nodes[3].Out.Elem.Kind == ir.KFloat) {
		if sep, _ := nodes[0].Meta["sep"].(string); len(sep) == 1 {
			out, err := g.emitSplitIntsEnumerateMapSum(sep[0], nodes[2], nodes[3], nodes[4], in)
			if err != nil {
				return 0, "", false, err
			}
			return 5, out, true, nil
		}
	}
	// Split(single-byte sep) + Convert To Integers + HashSetPairScan(Count):
	// stream the O(n) complement-count over the parse, so the []int64 is never
	// built and the count map grows to the true key cardinality instead of being
	// presized to the (much larger) element count.
	if len(nodes) >= 3 && nodes[0].Prim == "Split" && nodes[1].Prim == "Convert To Integers" &&
		nodes[1].In != nil && nodes[1].In.Equal(ir.List(ir.Text())) &&
		nodes[2].Prim == "HashSetPairScan" {
		mode, _ := nodes[2].Meta["mode"].(string)
		sep, _ := nodes[0].Meta["sep"].(string)
		if mode == "Count" && len(sep) == 1 {
			out, err := g.emitSplitIntsPairCount(sep[0], nodes[2], in)
			if err != nil {
				return 0, "", false, err
			}
			return 3, out, true, nil
		}
	}
	// Split(single-byte sep) + Convert To Integers + Fold(int seed): fold each
	// parsed int straight into the accumulator, so the []int64 is never built.
	if len(nodes) >= 3 && nodes[0].Prim == "Split" && nodes[1].Prim == "Convert To Integers" &&
		nodes[1].In != nil && nodes[1].In.Equal(ir.List(ir.Text())) && nodes[2].Prim == "Fold" {
		sep, _ := nodes[0].Meta["sep"].(string)
		_, intSeed := nodes[2].Meta["seed"].(int64)
		if len(sep) == 1 && intSeed && !hasMeasured(nodes[2], "seed") {
			out, err := g.emitSplitIntsFold(sep[0], nodes[2], in)
			if err != nil {
				return 0, "", false, err
			}
			return 3, out, true, nil
		}
	}
	// Split(single-byte sep) + Convert To Integers (flat): scan the string for
	// the separator and parse each segment straight into []int64, skipping the
	// intermediate []string (16 bytes/line of slice headers on large inputs).
	if len(nodes) >= 2 && nodes[0].Prim == "Split" && nodes[1].Prim == "Convert To Integers" &&
		nodes[1].In != nil && nodes[1].In.Equal(ir.List(ir.Text())) {
		if sep, _ := nodes[0].Meta["sep"].(string); len(sep) == 1 {
			out := g.emitSplitInts(sep[0], in)
			return 2, out, true, nil
		}
	}
	// Split Fields + Convert To Integers + Union + Count: count distinct integers
	// with a single map, streaming each line's fields in — no [][]int64, no
	// per-group set, no ordered elems slice. An optional leading Split(sep)
	// walks lines in the raw string.
	unionCount := func(fieldsN, convN, unionN, countN *ir.Node) bool {
		return fieldsN.Prim == "Split Fields" && convN.Prim == "Convert To Integers" &&
			unionN.Prim == "Union" && countN.Prim == "Count" &&
			fieldsN.In != nil && fieldsN.In.Kind == ir.KList &&
			unionN.Out != nil && unionN.Out.Elem != nil && unionN.Out.Elem.Kind == ir.KInt
	}
	if len(nodes) >= 5 && nodes[0].Prim == "Split" && unionCount(nodes[1], nodes[2], nodes[3], nodes[4]) {
		if sep, _ := nodes[0].Meta["sep"].(string); sep != "" {
			out := g.emitFieldsUnionCount(sep, in)
			return 5, out, true, nil
		}
	}
	if len(nodes) >= 4 && unionCount(nodes[0], nodes[1], nodes[2], nodes[3]) {
		out := g.emitFieldsUnionCount("", in)
		return 4, out, true, nil
	}
	// Split Fields + Convert To Integers + (Max By | Min By): stream a keyed
	// extremum through one reused per-line buffer, copying only the current
	// winner, so the [][]int64 of per-line records is never built. An optional
	// leading Split(sep) walks lines in the raw string too.
	keyedExt := func(fieldsN, convN, extN *ir.Node) bool {
		return fieldsN.Prim == "Split Fields" && convN.Prim == "Convert To Integers" &&
			(extN.Prim == "Max By" || extN.Prim == "Min By") &&
			fieldsN.In != nil && fieldsN.In.Kind == ir.KList
	}
	if len(nodes) >= 4 && nodes[0].Prim == "Split" && keyedExt(nodes[1], nodes[2], nodes[3]) {
		if sep, _ := nodes[0].Meta["sep"].(string); sep != "" {
			out, err := g.emitFieldsKeyedExtremum(sep, nodes[3], in)
			if err != nil {
				return 0, "", false, err
			}
			return 4, out, true, nil
		}
	}
	if len(nodes) >= 3 && keyedExt(nodes[0], nodes[1], nodes[2]) {
		out, err := g.emitFieldsKeyedExtremum("", nodes[2], in)
		if err != nil {
			return 0, "", false, err
		}
		return 3, out, true, nil
	}
	// Split Fields + Convert To Integers + Map Each(->scalar) + Sum: stream the
	// whole chain through one reused per-line buffer, accumulating the lambda's
	// result. No [][]string, no [][]int64, no mapped list, and no per-line
	// allocation. Falls back per line to strings.Fields + dmParseInt.
	fieldsMapSumScalar := func(fieldsN, convN, mapN, sumN *ir.Node) bool {
		return fieldsN.Prim == "Split Fields" && convN.Prim == "Convert To Integers" &&
			mapN.Prim == "Map Each" && sumN.Prim == "Sum" &&
			fieldsN.In != nil && fieldsN.In.Kind == ir.KList &&
			mapN.Out != nil && mapN.Out.Elem != nil &&
			(mapN.Out.Elem.Kind == ir.KInt || mapN.Out.Elem.Kind == ir.KFloat)
	}
	// Split(sep) + Split Fields + Convert To Integers + Map Each(scalar) + Sum:
	// walk lines in the raw string, so the []string of lines is skipped too.
	if len(nodes) >= 5 && nodes[0].Prim == "Split" && fieldsMapSumScalar(nodes[1], nodes[2], nodes[3], nodes[4]) {
		if sep, _ := nodes[0].Meta["sep"].(string); sep != "" {
			out, err := g.emitFieldsMapSum(sep, nodes[3], nodes[4], in)
			if err != nil {
				return 0, "", false, err
			}
			return 5, out, true, nil
		}
	}
	if len(nodes) >= 4 && fieldsMapSumScalar(nodes[0], nodes[1], nodes[2], nodes[3]) {
		out, err := g.emitFieldsMapSum("", nodes[2], nodes[3], in)
		if err != nil {
			return 0, "", false, err
		}
		return 4, out, true, nil
	}
	// Split Fields + Convert To Integers: parse each line's whitespace-separated
	// integers directly, skipping the intermediate [][]string. Falls back per
	// line to strings.Fields + dmParseInt for any non-fast-path line.
	if len(nodes) >= 2 && nodes[0].Prim == "Split Fields" && nodes[1].Prim == "Convert To Integers" &&
		nodes[0].In != nil && nodes[0].In.Kind == ir.KList {
		out := g.emitFieldsInts(in)
		return 2, out, true, nil
	}
	// Split Each("") + Convert To Integers + Convert To Grid: a digit grid fed
	// straight into a dmGrid[int64]. Fill the flat cell slice byte-by-byte from
	// the lines, skipping both the [][]string of one-rune substrings and the
	// intermediate [][]int64 that Convert To Grid would otherwise flatten.
	if len(nodes) >= 3 && nodes[0].Prim == "Split Each" && nodes[1].Prim == "Convert To Integers" &&
		nodes[2].Prim == "Convert To Grid" {
		if sep, _ := nodes[0].Meta["sep"].(string); sep == "" &&
			nodes[2].Out != nil && nodes[2].Out.Kind == ir.KGrid {
			out := g.emitDigitGridToGrid(in)
			return 3, out, true, nil
		}
	}
	// Split Each("") + Intersect: the character-set intersection over lines.
	// Build each line's membership directly from its bytes/runes (skipping the
	// [][]string of one-rune substrings and the per-line hash set), and probe
	// the shrinking accumulator against it — the same algorithm emitSetReduce
	// runs, with a [256]bool fast path when the accumulator is all single-byte
	// characters (an exact fallback covers multibyte accumulators).
	if len(nodes) >= 2 && nodes[0].Prim == "Split Each" && nodes[1].Prim == "Intersect" {
		if sep, _ := nodes[0].Meta["sep"].(string); sep == "" {
			out := g.emitSplitEachIntersect(in)
			return 2, out, true, nil
		}
	}
	// Split Each("") + Convert To Integers: a digit grid. Build [][]int64
	// straight from each line's bytes (int64(b-'0')) instead of splitting into
	// a []string of one-rune substrings and re-parsing each. Falls back per
	// line to the exact split+parse path when a line is not pure ASCII digits,
	// so semantics (values and dmFail behaviour) are unchanged.
	if len(nodes) >= 2 && nodes[0].Prim == "Split Each" && nodes[1].Prim == "Convert To Integers" {
		if sep, _ := nodes[0].Meta["sep"].(string); sep == "" &&
			nodes[1].In != nil && nodes[1].In.Kind == ir.KList &&
			nodes[1].In.Elem != nil && nodes[1].In.Elem.Kind == ir.KList {
			out := g.emitDigitGrid(in)
			return 2, out, true, nil
		}
	}
	// Map Each(list literal) + Flatten + Count By: stream each input element's
	// literal-list expansion straight into the count map, so neither the
	// per-element list nor the flattened list is ever built. Count By is an
	// order-insensitive accumulation and the elements are visited in the same
	// order (input order, then literal order) the unfused path inserts them, so
	// the resulting map — keys and values — is identical.
	if len(nodes) >= 3 && nodes[0].Prim == "Map Each" && nodes[1].Prim == "Flatten" &&
		nodes[2].Prim == "Count By" {
		if lam, err := g.nodeLambda(nodes[0]); err == nil {
			if inner, iname := callName(lam.Body); iname == "list" && len(inner.Args) >= 1 {
				out, err := g.emitFlatMapCountBy(nodes[0], nodes[2], inner.Args, in)
				if err != nil {
					return 0, "", false, err
				}
				return 3, out, true, nil
			}
		}
	}
	// Split(sep) + Match Pattern(Each) + Map Each(scalar) + Sum: walk the raw
	// string line by line and parse-map-accumulate, skipping the []string too.
	mapSumScalar := func(mapN, sumN *ir.Node) bool {
		return mapN.Prim == "Map Each" && sumN.Prim == "Sum" &&
			mapN.Out != nil && mapN.Out.Elem != nil &&
			(mapN.Out.Elem.Kind == ir.KInt || mapN.Out.Elem.Kind == ir.KFloat)
	}
	if len(nodes) >= 4 && nodes[0].Prim == "Split" && matchEach(nodes[1]) && mapSumScalar(nodes[2], nodes[3]) {
		if sep, _ := nodes[0].Meta["sep"].(string); sep != "" {
			out, err := g.emitParseMapSum(sep, nodes[1], nodes[2], nodes[3], in)
			if err != nil {
				return 0, "", false, err
			}
			return 4, out, true, nil
		}
	}
	// Match Pattern(Each) + Map Each(scalar) + Sum: parse each line into a
	// record/tuple locally, apply the map lambda, and accumulate — no []Record
	// array and no mapped list. (The record's fields, e.g. a word hole's name
	// substring, are never retained.)
	if len(nodes) >= 3 && matchEach(nodes[0]) && mapSumScalar(nodes[1], nodes[2]) {
		out, err := g.emitParseMapSum("", nodes[0], nodes[1], nodes[2], in)
		if err != nil {
			return 0, "", false, err
		}
		return 3, out, true, nil
	}
	// Enumerate + Map Each(scalar) + Sum: fold the index into the map loop,
	// constructing each (index, value) tuple locally, so the []tuple is never
	// built.
	if len(nodes) >= 3 && nodes[0].Prim == "Enumerate" && nodes[1].Prim == "Map Each" && nodes[2].Prim == "Sum" &&
		nodes[1].Out != nil && nodes[1].Out.Elem != nil &&
		(nodes[1].Out.Elem.Kind == ir.KInt || nodes[1].Out.Elem.Kind == ir.KFloat) {
		out, err := g.emitEnumerateMapSum(nodes[0], nodes[1], nodes[2], in)
		if err != nil {
			return 0, "", false, err
		}
		return 3, out, true, nil
	}
	// Split(sep) + Map Each(scalar) + Sum: walk lines in the raw string and
	// accumulate the lambda's result, skipping the []string of lines.
	if len(nodes) >= 3 && nodes[0].Prim == "Split" && nodes[1].Prim == "Map Each" && nodes[2].Prim == "Sum" &&
		nodes[1].Out != nil && nodes[1].Out.Elem != nil &&
		(nodes[1].Out.Elem.Kind == ir.KInt || nodes[1].Out.Elem.Kind == ir.KFloat) {
		if sep, _ := nodes[0].Meta["sep"].(string); sep != "" {
			out, err := g.emitSplitMapSum(sep, nodes[1], nodes[2], in)
			if err != nil {
				return 0, "", false, err
			}
			return 3, out, true, nil
		}
	}
	// Map Each + Sum: accumulate the lambda's result into a scalar rather than
	// building the mapped list. Only for scalar (Int/Float) element outputs.
	if len(nodes) >= 2 && nodes[0].Prim == "Map Each" && nodes[1].Prim == "Sum" &&
		nodes[0].Out != nil && nodes[0].Out.Elem != nil &&
		(nodes[0].Out.Elem.Kind == ir.KInt || nodes[0].Out.Elem.Kind == ir.KFloat) {
		out, err := g.emitMapSum(nodes[0], in)
		if err != nil {
			return 0, "", false, err
		}
		return 2, out, true, nil
	}
	// WindowedReduce + Sum: accumulate each window's extremum into a scalar,
	// never building the per-window []int64.
	if len(nodes) >= 2 && nodes[0].Prim == "WindowedReduce" && nodes[1].Prim == "Sum" {
		out, err := g.emitWindowedReduceSum(nodes[0], in)
		if err != nil {
			return 0, "", false, err
		}
		return 2, out, true, nil
	}
	// Convert To Grid (from text) + BFS/Connected Components: build the mask
	// straight from lines, never materializing the string grid.
	if len(nodes) >= 2 && nodes[0].Prim == "Convert To Grid" && gridSearchFusable(nodes[1]) &&
		nodes[0].In != nil && nodes[0].In.Kind == ir.KList && nodes[0].In.Elem != nil && nodes[0].In.Elem.Kind == ir.KText {
		out, err := g.emitGridSearchFromLines(nodes[1], in)
		if err != nil {
			return 0, "", false, err
		}
		return 2, out, true, nil
	}
	return 0, "", false, nil
}

// emitParseCount lowers a fused Match-Each + Count-Matching. When sep != ""
// the input `in` is the un-split string and the loop walks it exactly as
// strings.Split(in, sep) would (including a lone empty element for empty
// input); when sep == "" the input is an already-split []string.
func (g *gen) emitParseCount(sep string, matchNode, countNode *ir.Node, in string) (string, error) {
	tmplStr, _ := matchNode.Meta["template"].(string)
	tmpl, err := pattern.ParseTemplate(tmplStr)
	if err != nil {
		return "", unsupported(matchNode, "template: %v", err)
	}
	elemType := tmpl.OutputType()
	elemGo, err := g.goType(elemType)
	if err != nil {
		return "", unsupported(matchNode, "%v", err)
	}
	fn, err := g.matchParseFunc(tmpl, elemType, elemGo)
	if err != nil {
		return "", unsupported(matchNode, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")

	lam, err := g.nodeLambda(countNode)
	if err != nil {
		return "", err
	}
	v := g.fresh("v")
	r, ok := g.fresh("r"), g.fresh("ok")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: r, typ: countNode.In.Elem}})
	if err != nil {
		return "", unsupported(countNode, "lambda: %v", err)
	}

	emitBody := func(line string) {
		g.wl("%s, %s := %s(%s)", r, ok, fn, line)
		g.wl("if !%s {", ok)
		g.in()
		g.wl(`dmFail("input %%q does not match template %%q", %s, %s)`, line, goStr(tmplStr))
		g.out()
		g.wl("}")
		g.wl("if %s {", body)
		g.in()
		g.wl("%s++", v)
		g.out()
		g.wl("}")
	}

	g.wl("var %s int64", v)
	if sep == "" {
		s := g.fresh("s")
		g.wl("for _, %s := range %s {", s, in)
		g.in()
		emitBody(s)
		g.out()
		g.wl("}")
		return v, nil
	}

	g.imp("strings")
	s := g.fresh("s")
	idx := g.fresh("idx")
	line := g.fresh("line")
	g.wl("%s := %s", s, in)
	g.wl("for {")
	g.in()
	if len(sep) == 1 {
		g.wl("%s := strings.IndexByte(%s, %q)", idx, s, sep[0])
	} else {
		g.wl("%s := strings.Index(%s, %s)", idx, s, goStr(sep))
	}
	g.wl("%s := %s", line, s)
	g.wl("if %s >= 0 {", idx)
	g.in()
	g.wl("%s = %s[:%s]", line, s, idx)
	g.out()
	g.wl("}")
	emitBody(line)
	g.wl("if %s < 0 {", idx)
	g.in()
	g.wl("break")
	g.out()
	g.wl("}")
	g.wl("%s = %s[%s+%d:]", s, s, idx, len(sep))
	g.out()
	g.wl("}")
	return v, nil
}

// emitParseMapSum lowers Match Pattern(Each) + Map Each(scalar) + Sum. When
// sep == "" the input is an already-split []string; when sep != "" the input is
// the un-split string and the loop walks it exactly as strings.Split(in, sep)
// would (including a lone empty element for empty input), so neither the
// []string, the []Record, nor the mapped list is materialized.
func (g *gen) emitParseMapSum(sep string, matchNode, mapNode, sumNode *ir.Node, in string) (string, error) {
	tmplStr, _ := matchNode.Meta["template"].(string)
	tmpl, err := pattern.ParseTemplate(tmplStr)
	if err != nil {
		return "", unsupported(matchNode, "template: %v", err)
	}
	elemType := tmpl.OutputType()
	elemGo, err := g.goType(elemType)
	if err != nil {
		return "", unsupported(matchNode, "%v", err)
	}
	fn, err := g.matchParseFunc(tmpl, elemType, elemGo)
	if err != nil {
		return "", unsupported(matchNode, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	lam, err := g.nodeLambda(mapNode)
	if err != nil {
		return "", err
	}
	acc, err := g.goType(sumNode.Out)
	if err != nil {
		return "", unsupported(sumNode, "%v", err)
	}
	v := g.fresh("v")
	r, ok := g.fresh("r"), g.fresh("ok")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: r, typ: mapNode.In.Elem}})
	if err != nil {
		return "", unsupported(mapNode, "lambda: %v", err)
	}
	emitBody := func(line string) {
		g.wl("%s, %s := %s(%s)", r, ok, fn, line)
		g.wl("if !%s {", ok)
		g.in()
		g.wl(`dmFail("input %%q does not match template %%q", %s, %s)`, line, goStr(tmplStr))
		g.out()
		g.wl("}")
		g.wl("%s += %s", v, body)
	}

	g.wl("var %s %s", v, acc)
	if sep == "" {
		s := g.fresh("s")
		g.wl("for _, %s := range %s {", s, in)
		g.in()
		emitBody(s)
		g.out()
		g.wl("}")
		return v, nil
	}

	g.imp("strings")
	s, idx, line := g.fresh("s"), g.fresh("idx"), g.fresh("line")
	g.wl("%s := %s", s, in)
	g.wl("for {")
	g.in()
	if len(sep) == 1 {
		g.wl("%s := strings.IndexByte(%s, %q)", idx, s, sep[0])
	} else {
		g.wl("%s := strings.Index(%s, %s)", idx, s, goStr(sep))
	}
	g.wl("%s := %s", line, s)
	g.wl("if %s >= 0 {", idx)
	g.in()
	g.wl("%s = %s[:%s]", line, s, idx)
	g.out()
	g.wl("}")
	emitBody(line)
	g.wl("if %s < 0 {", idx)
	g.in()
	g.wl("break")
	g.out()
	g.wl("}")
	g.wl("%s = %s[%s+%d:]", s, s, idx, len(sep))
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitMatchPattern(n *ir.Node, in string) (string, error) {
	each, _ := n.Meta["each"].(bool)

	// Case: alternatives carry no single template, so their element type comes
	// from the node the resolver already typed rather than from re-deriving it.
	if cases, _ := n.Meta["cases"].([][2]string); len(cases) > 0 {
		elemType := n.Out
		if each {
			elemType = n.Out.Elem
		}
		elemGo, err := g.goType(elemType)
		if err != nil {
			return "", unsupported(n, "%v", err)
		}
		g.helper("dmFail", declFail, "fmt", "os")
		fn, err := g.matchCaseFunc(cases, elemType, elemGo)
		if err != nil {
			return "", unsupported(n, "%v", err)
		}
		return g.emitMatchOver(n, fn, elemGo, in,
			fmt.Sprintf("matches none of the %d Case: templates", len(cases)))
	}

	tmplStr, _ := n.Meta["template"].(string)
	tmpl, err := pattern.ParseTemplate(tmplStr)
	if err != nil {
		return "", unsupported(n, "template: %v", err)
	}

	elemType := tmpl.OutputType()
	elemGo, err := g.goType(elemType)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}

	if scan, _ := n.Meta["scan"].(bool); scan {
		// Scan concatenates every occurrence from every line, so a line
		// contributes as many values as it contains rather than exactly one.
		g.helper("dmFail", declFail, "fmt", "os")
		fn, err := g.matchScanFunc(tmpl, elemType, elemGo)
		if err != nil {
			return "", unsupported(n, "%v", err)
		}
		v, s := g.fresh("v"), g.fresh("s")
		g.wl("%s := []%s{}", v, elemGo)
		g.wl("for _, %s := range %s {", s, in)
		g.in()
		g.wl("%s = append(%s, %s(%s)...)", v, v, fn, s)
		g.out()
		g.wl("}")
		return v, nil
	}

	fn, err := g.matchParseFunc(tmpl, elemType, elemGo)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")

	return g.emitMatchOver(n, fn, elemGo, in, "does not match template %q", goStr(tmplStr))
}

// emitMatchOver applies a generated `(T, bool)` parse function to the current
// value the way the node's mode says: once for Mode: One, over every line for
// Each, and over every line skipping the misses for Try.
//
// `what` completes the refusal message — a Case: stage says what it actually
// tried — and `extra` carries any operands it needs. The template goes through
// as an *operand* rather than being pasted into the message, because a
// template contains quotes and may contain a `%`, either of which turns the
// generated dmFail call into Go that does not parse.
func (g *gen) emitMatchOver(n *ir.Node, fn, elemGo, in, what string, extra ...string) (string, error) {
	tail := ""
	for _, e := range extra {
		tail += ", " + e
	}
	each, _ := n.Meta["each"].(bool)
	if !each {
		v, ok := g.fresh("v"), g.fresh("ok")
		g.wl("%s, %s := %s(%s)", v, ok, fn, in)
		g.wl("if !%s {", ok)
		g.in()
		g.wl(`dmFail("input %%q %s", %s%s)`, what, in, tail)
		g.out()
		g.wl("}")
		return v, nil
	}

	try, _ := n.Meta["try"].(bool)
	v, s := g.fresh("v"), g.fresh("s")
	r, ok := g.fresh("r"), g.fresh("ok")
	g.wl("%s := make([]%s, 0, len(%s))", v, elemGo, in)
	g.wl("for _, %s := range %s {", s, in)
	g.in()
	g.wl("%s, %s := %s(%s)", r, ok, fn, s)
	g.wl("if !%s {", ok)
	g.in()
	if try {
		// Mode: Try keeps the lines that fit and drops the rest, which is what
		// makes a file of two shapes parseable at all — one pass per shape.
		g.wl("continue")
	} else {
		g.wl(`dmFail("input %%q %s", %s%s)`, what, s, tail)
	}
	g.out()
	g.wl("}")
	g.wl("%s = append(%s, %s)", v, v, r)
	g.out()
	g.wl("}")
	return v, nil
}

// matchParseFunc interns the parse function for one template.
func (g *gen) matchParseFunc(tmpl *pattern.Template, elemType *ir.Type, elemGo string) (string, error) {
	g.parsen++
	name := fmt.Sprintf("dmParse%d", g.parsen)
	var src string
	if fastEligible(tmpl, elemType) {
		// tmpl.Holes rather than the segments: an int hole inside a group is
		// still an int hole, and reading the segments only ever saw the ones
		// at the top level.
		hasInt := false
		for _, h := range tmpl.Holes {
			if h.Group == nil && !h.Flag && h.Type == pattern.HoleInt {
				hasInt = true
			}
		}
		src = genFastParser(name, tmpl, elemType, elemGo)
		if hasInt {
			g.imp("strconv") // int holes re-parse overflowing runs via strconv
		}
		if hasLongLiteral(tmpl) {
			// genFastParser only calls strings.HasPrefix for literal segments
			// longer than 4 bytes; shorter ones compile to direct byte
			// comparisons and never touch the strings package.
			g.imp("strings")
		}
	} else {
		// The regex path declares its own imports as it emits each conversion,
		// so a hole kind that needs strconv or strings cannot be added to
		// emitCapture and forgotten here.
		var err error
		if src, err = g.genRegexParser(name, tmpl, elemType, elemGo); err != nil {
			return "", err
		}
		g.imp("regexp")
	}
	g.decls = append(g.decls, src)
	return name, nil
}

// isWSByte reports whether b is one of Go regexp's \s bytes ([\t\n\f\r ]);
// note \v (0x0b) is deliberately excluded, matching RE2.
func isWSByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\f' || b == '\r'
}

// fastEligible reports whether a greedy left-to-right scan is provably
// equivalent to the template's regex. That holds when:
//   - every Int hole (`-?\d+`) is followed by end-of-template or a non-empty
//     literal that cannot begin with a digit — the only class a greedy digit
//     scan could steal; and
//   - every Word hole (`\S+`) is followed by end-of-template or a non-empty
//     literal beginning with whitespace, so the greedy non-whitespace run
//     stops exactly where the regex would with no backtracking. Word holes are
//     taken in a Record (named) template, where each lands in its own field,
//     and in a positional one whose holes are all words — a List<Text>, where
//     each lands at its own index. A positional template mixing words with
//     ints is a Tuple of unlike types and stays on the regex path.
//
// Adjacent holes always need backtracking, so any hole immediately followed by
// another hole disqualifies the template.
func fastEligible(tmpl *pattern.Template, elemType *ir.Type) bool {
	// An optional group is a run the scan may or may not have to consume, and
	// a greedy left-to-right scan has no way to decide which; a repeated group
	// is a run of unknown length with structure inside it. Both take the regex
	// engine. Checked before the loop because an optional group's segment
	// carries no hole of its own, and the loop below would walk straight past
	// it and pronounce the template eligible.
	if len(tmpl.Opts) > 0 {
		return false
	}
	for _, h := range tmpl.Holes {
		if h.Group != nil {
			return false
		}
	}
	segs := tmpl.Segments
	for i, seg := range segs {
		if seg.Hole == nil {
			continue
		}
		if seg.Hole.Space {
			return false // a {~} gap is a run the scan would have to size
		}
		if seg.Hole.Rep {
			// A repeated hole is a run with its own internal separators; the
			// greedy scan has no notion of where one element ends.
			return false
		}
		switch seg.Hole.Type {
		case pattern.HoleInt:
			// fall through to the shared successor check below
		case pattern.HoleWord:
			named := elemType.Kind == ir.KRecord
			allWords := elemType.Kind == ir.KList && elemType.Elem != nil &&
				elemType.Elem.Kind == ir.KText
			if !named && !allWords {
				return false
			}
		default:
			// Text (.*), and the classes the scanner has no hand-written
			// reader for: hex, digits, char, and a {~} gap. Each is a
			// correctness-neutral fallback to the documented regex path.
			return false
		}
		if i+1 < len(segs) {
			next := segs[i+1]
			if next.Hole != nil {
				return false // adjacent holes need backtracking
			}
			if next.Literal == "" {
				return false
			}
			switch seg.Hole.Type {
			case pattern.HoleInt:
				if next.Literal[0] >= '0' && next.Literal[0] <= '9' {
					return false
				}
			case pattern.HoleWord:
				if !isWSByte(next.Literal[0]) {
					return false
				}
			}
		}
	}
	return true
}

// hasLongLiteral reports whether genFastParser will emit at least one
// strings.HasPrefix call for tmpl, i.e. whether any literal segment is
// longer than the 4-byte threshold below which it instead emits direct byte
// comparisons. Callers use this to decide whether the "strings" import is
// actually needed, rather than the coarser "has any literal" test.
func hasLongLiteral(tmpl *pattern.Template) bool {
	for _, seg := range tmpl.Segments {
		if seg.Hole == nil && len(seg.Literal) > 4 {
			return true
		}
	}
	return false
}

// genFastParser emits a hand-rolled scanner for an all-int template.
func genFastParser(name string, tmpl *pattern.Template, elemType *ir.Type, elemGo string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "func %s(s string) (%s, bool) {\n", name, elemGo)
	if elemType.Kind == ir.KRecord {
		fmt.Fprintf(&b, "\tvar out %s\n", elemGo)
	} else {
		fmt.Fprintf(&b, "\tout := make(%s, %d)\n", elemGo, len(tmpl.Holes))
	}
	b.WriteString("\ti := 0\n")
	holeIdx := 0
	for _, seg := range tmpl.Segments {
		if seg.Hole == nil {
			lit := seg.Literal
			if k := len(lit); k > 0 && k <= 4 {
				// Direct byte comparisons beat a HasPrefix call (no substring
				// header, no bounds re-checks) for the short literal separators
				// that dominate AoC templates ("-", ",", ": ").
				fmt.Fprintf(&b, "\tif i+%d > len(s) {\n\t\treturn out, false\n\t}\n", k)
				conds := make([]string, k)
				for o := 0; o < k; o++ {
					idxExpr := "i"
					if o > 0 {
						idxExpr = fmt.Sprintf("i+%d", o)
					}
					conds[o] = fmt.Sprintf("s[%s] != %s", idxExpr, goByte(lit[o]))
				}
				fmt.Fprintf(&b, "\tif %s {\n\t\treturn out, false\n\t}\n", strings.Join(conds, " || "))
			} else {
				fmt.Fprintf(&b, "\tif !strings.HasPrefix(s[i:], %s) {\n\t\treturn out, false\n\t}\n", goStr(lit))
			}
			fmt.Fprintf(&b, "\ti += %d\n", len(lit))
			continue
		}
		if seg.Hole.Type == pattern.HoleWord {
			// \S+: consume a maximal run of non-whitespace bytes (>= 1). The
			// following literal, if any, begins with whitespace, so this stops
			// exactly where the regex would. Restricted to Record templates.
			wj := fmt.Sprintf("j%d", holeIdx)
			fmt.Fprintf(&b, "\t%s := i\n", wj)
			fmt.Fprintf(&b, "\tfor %s < len(s) && !(s[%s] == ' ' || s[%s] == '\\t' || s[%s] == '\\n' || s[%s] == '\\f' || s[%s] == '\\r') {\n\t\t%s++\n\t}\n", wj, wj, wj, wj, wj, wj, wj)
			fmt.Fprintf(&b, "\tif %s == i {\n\t\treturn out, false\n\t}\n", wj)
			if elemType.Kind == ir.KRecord {
				fmt.Fprintf(&b, "\tout.%s = s[i:%s]\n", fieldName(seg.Hole.Name), wj)
			} else {
				fmt.Fprintf(&b, "\tout[%d] = s[i:%s]\n", holeIdx, wj)
			}
			fmt.Fprintf(&b, "\ti = %s\n", wj)
			holeIdx++
			continue
		}
		j := fmt.Sprintf("j%d", holeIdx)
		d := fmt.Sprintf("d%d", holeIdx)
		nv := fmt.Sprintf("n%d", holeIdx)
		ev := fmt.Sprintf("e%d", holeIdx)
		ng := fmt.Sprintf("g%d", holeIdx)
		fmt.Fprintf(&b, "\t%s := i\n", j)
		fmt.Fprintf(&b, "\t%s := false\n", ng)
		fmt.Fprintf(&b, "\tif %s < len(s) && s[%s] == '-' {\n\t\t%s = true\n\t\t%s++\n\t}\n", j, j, ng, j)
		fmt.Fprintf(&b, "\t%s := %s\n", d, j)
		// Accumulate the value during the digit scan instead of a strconv call.
		fmt.Fprintf(&b, "\tvar %s int64\n", nv)
		fmt.Fprintf(&b, "\tfor %s < len(s) && s[%s] >= '0' && s[%s] <= '9' {\n\t\t%s = %s*10 + int64(s[%s]-'0')\n\t\t%s++\n\t}\n", j, j, j, nv, nv, j, j)
		fmt.Fprintf(&b, "\tif %s == %s {\n\t\treturn out, false\n\t}\n", j, d)
		// A run of 19+ digits may overflow int64; re-parse those exactly (and
		// preserve strconv's out-of-range rejection). Short runs stay inline.
		tv := fmt.Sprintf("t%d", holeIdx)
		// A capture that fits the template's shape and then fails to convert is
		// a broken line, not a different kind of line: it stops the program in
		// every mode, exactly as prims/match.go does it. Returning false here
		// would report it as a shape mismatch, which Mode: Try then drops —
		// turning a corrupt input into a quietly short answer that the
		// interpreter refuses to produce.
		fmt.Fprintf(&b, "\tif %s-%s > 18 {\n\t\t%s, %s := strconv.ParseInt(s[i:%s], 10, 64)\n\t\tif %s != nil {\n\t\t\tdmFail(\"captured %%q is not a valid integer\", s[i:%s])\n\t\t}\n\t\t%s = %s\n\t} else if %s {\n\t\t%s = -%s\n\t}\n", j, d, tv, ev, j, ev, j, nv, tv, ng, nv, nv)
		if elemType.Kind == ir.KRecord {
			fmt.Fprintf(&b, "\tout.%s = %s\n", fieldName(seg.Hole.Name), nv)
		} else {
			fmt.Fprintf(&b, "\tout[%d] = %s\n", holeIdx, nv)
		}
		fmt.Fprintf(&b, "\ti = %s\n", j)
		holeIdx++
	}
	b.WriteString("\tif i != len(s) {\n\t\treturn out, false\n\t}\n")
	b.WriteString("\treturn out, true\n}")
	return b.String()
}

// genRegexParser emits the fallback: a package-level compiled regexp with the
// exact same lowering the interpreter uses (pattern.Template.CompileRegex),
// minus group names.
func (g *gen) genRegexParser(name string, tmpl *pattern.Template, elemType *ir.Type, elemGo string) (string, error) {
	// The template's own lowering, unnamed. Building a second one here is how
	// the compiled parse and the interpreted one would drift apart.
	src := tmpl.RegexSource(false)
	if _, err := regexp.Compile(src); err != nil {
		return "", fmt.Errorf("template regex: %v", err)
	}

	reVar := name + "Re"
	var b strings.Builder
	fmt.Fprintf(&b, "var %s = regexp.MustCompile(%s)\n\n", reVar, goStr(src))
	fmt.Fprintf(&b, "func %s(s string) (%s, bool) {\n", name, elemGo)
	if elemType.Kind == ir.KRecord || elemType.Kind == ir.KTuple {
		fmt.Fprintf(&b, "\tvar out %s\n", elemGo)
	} else {
		fmt.Fprintf(&b, "\tout := make(%s, %d)\n", elemGo, len(tmpl.Holes))
	}
	// Indices, not FindStringSubmatch: an optional group that did not
	// participate has to be told from one that matched the empty string, and
	// the text form reports both as "". Same reason prims/match.go reads the
	// match this way — a divergence here is a program that parses differently
	// once compiled.
	fmt.Fprintf(&b, "\tm := %s.FindStringSubmatchIndex(s)\n", reVar)
	b.WriteString("\tif m == nil {\n\t\treturn out, false\n\t}\n")

	if err := g.emitCaptures(&b, tmpl, elemType, "out", "\t", "return out, false"); err != nil {
		return "", err
	}
	b.WriteString("\treturn out, true\n}")
	return b.String(), nil
}

// emitCaptures writes the whole per-capture assembly for one match: every hole
// converted into its slot of outVar, and every optional group's flag set from
// whether its own capture participated.
//
// It is shared by the anchored parse function and the unanchored scan one, so
// the two cannot come to disagree about which capture holds what — they already
// share the capture plan, and this is the other half of reading it.
// onFail is the statement a conversion that cannot proceed runs: the parse
// function reports it to its caller, while the scan function has no caller to
// report to and stops the program.
func (g *gen) emitCaptures(b *strings.Builder, tmpl *pattern.Template, elemType *ir.Type,
	outVar, ind, onFail string) error {
	target := func(slot int) string {
		switch elemType.Kind {
		case ir.KRecord:
			return outVar + "." + fieldName(tmpl.Holes[slot].Name)
		case ir.KTuple:
			return outVar + "." + tupleField(slot)
		default:
			return fmt.Sprintf("%s[%d]", outVar, slot)
		}
	}
	for i, c := range tmpl.Captures() {
		lo := fmt.Sprintf("m[%d]", 2*c.Group)
		hi := fmt.Sprintf("m[%d]", 2*c.Group+1)
		if c.Kind == pattern.CapOpt {
			// The flag is the only thing a group's own capture carries; the
			// holes inside have captures of their own.
			if c.Slot >= 0 {
				fmt.Fprintf(b, "%s%s = %s >= 0\n", ind, target(c.Slot), lo)
			}
			continue
		}
		// A hole whose optional group stood down keeps its zero, which is what
		// the freshly declared out value already holds.
		inner := ind
		if c.Opt != nil {
			fmt.Fprintf(b, "%sif %s >= 0 {\n", ind, lo)
			inner = ind + "\t"
		}
		capExpr := fmt.Sprintf("s[%s:%s]", lo, hi)
		if err := g.emitCapture(b, inner, target(c.Slot), capExpr, *c.Hole, i, onFail); err != nil {
			return err
		}
		if c.Opt != nil {
			fmt.Fprintf(b, "%s}\n", ind)
		}
	}
	return nil
}

// genScanFunc emits `Mode: Scan`'s reader: every occurrence of the template
// inside a line, in order. The pattern is the template's own lowering with the
// anchors left off — pattern.Template.ScanSource, from the same walk, rather
// than this file trimming "^" and "$" off a string and hoping.
func (g *gen) genScanFunc(name string, tmpl *pattern.Template, elemType *ir.Type, elemGo string) (string, error) {
	src := tmpl.ScanSource(false)
	if _, err := regexp.Compile(src); err != nil {
		return "", fmt.Errorf("template regex: %v", err)
	}
	reVar := name + "Re"
	var b strings.Builder
	fmt.Fprintf(&b, "var %s = regexp.MustCompile(%s)\n\n", reVar, goStr(src))
	fmt.Fprintf(&b, "func %s(s string) []%s {\n", name, elemGo)
	fmt.Fprintf(&b, "\tvar out []%s\n", elemGo)
	fmt.Fprintf(&b, "\tfor _, m := range %s.FindAllStringSubmatchIndex(s, -1) {\n", reVar)
	if elemType.Kind == ir.KRecord || elemType.Kind == ir.KTuple {
		fmt.Fprintf(&b, "\t\tvar v %s\n", elemGo)
	} else {
		fmt.Fprintf(&b, "\t\tv := make(%s, %d)\n", elemGo, len(tmpl.Holes))
	}
	if err := g.emitCaptures(&b, tmpl, elemType, "v", "\t\t",
		`dmFail("input %q does not match template %q", s, `+goStr(tmpl.Raw)+`)`); err != nil {
		return "", err
	}
	b.WriteString("\t\tout = append(out, v)\n\t}\n\treturn out\n}")
	return b.String(), nil
}

// matchScanFunc interns the scan function for one template.
func (g *gen) matchScanFunc(tmpl *pattern.Template, elemType *ir.Type, elemGo string) (string, error) {
	g.parsen++
	name := fmt.Sprintf("dmScan%d", g.parsen)
	src, err := g.genScanFunc(name, tmpl, elemType, elemGo)
	if err != nil {
		return "", err
	}
	g.imp("regexp")
	g.decls = append(g.decls, src)
	return name, nil
}

// numericHole reports a hole whose capture converts to an Int.
func numericHole(h pattern.Hole) bool {
	_, numeric := pattern.CaptureBase(h.Type)
	return numeric
}

// emitCapture writes the conversion of one capture into its target: a scalar,
// the list a repeated hole holds, or the list of values a repeated group holds.
// It mirrors prims/match.go's convertHole case for case.
func (g *gen) emitCapture(b *strings.Builder, ind, target, capExpr string, h pattern.Hole, i int, onFail string) error {
	switch {
	case h.Group != nil:
		// A repeated group is the scalar repetition one level down: split the
		// run, then run the inner template's own parse function over each
		// element. Generating that function by recursion is what keeps the two
		// levels from being two implementations.
		inner := h.Group.OutputType()
		innerGo, err := g.goType(inner)
		if err != nil {
			return err
		}
		fn, err := g.matchParseFunc(h.Group, inner, innerGo)
		if err != nil {
			return err
		}
		g.imp("strings")
		pv, jv, ev, ov := fmt.Sprintf("p%d", i), fmt.Sprintf("j%d", i),
			fmt.Sprintf("e%d", i), fmt.Sprintf("o%d", i)
		fmt.Fprintf(b, "%s%s := strings.Split(%s, %s)\n", ind, pv, capExpr, goStr(h.Sep))
		fmt.Fprintf(b, "%s%s := make([]%s, len(%s))\n", ind, ov, innerGo, pv)
		fmt.Fprintf(b, "%sfor %s := range %s {\n", ind, jv, pv)
		fmt.Fprintf(b, "%s\t%s, ok%d := %s(%s[%s])\n", ind, ev, i, fn, pv, jv)
		fmt.Fprintf(b, "%s\tif !ok%d {\n%s\t\t%s\n%s\t}\n", ind, i, ind, onFail, ind)
		fmt.Fprintf(b, "%s\t%s[%s] = %s\n%s}\n", ind, ov, jv, ev, ind)
		fmt.Fprintf(b, "%s%s = %s\n", ind, target, ov)

	case h.Rep:
		// The regex captured the whole run — a Go regexp keeps only the last
		// match of a repeated group — so the split happens here, exactly as
		// prims/match.go's convertHole does it.
		g.imp("strings")
		pv := fmt.Sprintf("p%d", i)
		fmt.Fprintf(b, "%s%s := strings.Split(%s, %s)\n", ind, pv, capExpr, goStr(h.Sep))
		if base, numeric := pattern.CaptureBase(h.Type); numeric {
			g.imp("strconv")
			fmt.Fprintf(b, "%s%s = make([]int64, len(%s))\n", ind, target, pv)
			fmt.Fprintf(b, "%sfor j%d, q%d := range %s {\n", ind, i, i, pv)
			fmt.Fprintf(b, "%s\tn%d, e%d := strconv.ParseInt(q%d, %d, 64)\n", ind, i, i, i, base)
			fmt.Fprintf(b, "%s\tif e%d != nil {\n%s\t\tdmFail(\"captured %%q is not a valid integer\", q%d)\n%s\t}\n",
				ind, i, ind, i, ind)
			fmt.Fprintf(b, "%s\t%s[j%d] = n%d\n%s}\n", ind, target, i, i, ind)
			return nil
		}
		fmt.Fprintf(b, "%s%s = %s\n", ind, target, pv)

	case numericHole(h):
		base, _ := pattern.CaptureBase(h.Type)
		g.imp("strconv")
		nv, ev := fmt.Sprintf("n%d", i), fmt.Sprintf("e%d", i)
		fmt.Fprintf(b, "%s%s, %s := strconv.ParseInt(%s, %d, 64)\n", ind, nv, ev, capExpr, base)
		fmt.Fprintf(b, "%sif %s != nil {\n%s\tdmFail(\"captured %%q is not a valid integer\", %s)\n%s}\n",
			ind, ev, ind, capExpr, ind)
		fmt.Fprintf(b, "%s%s = %s\n", ind, target, nv)

	default:
		fmt.Fprintf(b, "%s%s = %s\n", ind, target, capExpr)
	}
	return nil
}

// matchCaseFunc interns the parse function for a `Case:` stage: one regexp per
// alternative, tried in the order the program wrote them, each assembling the
// same output type plus the `kind` field naming which one won.
//
// The alternatives share a Go function rather than one per case with a
// dispatcher, because every case produces the *same* type — that is the rule
// the resolver enforces — so there is exactly one struct to fill.
func (g *gen) matchCaseFunc(cases [][2]string, elemType *ir.Type, elemGo string) (string, error) {
	g.parsen++
	name := fmt.Sprintf("dmCase%d", g.parsen)

	var b strings.Builder
	var body strings.Builder
	fmt.Fprintf(&body, "func %s(s string) (%s, bool) {\n", name, elemGo)
	fmt.Fprintf(&body, "\tvar out %s\n", elemGo)
	for i, c := range cases {
		tag, src := c[0], c[1]
		tmpl, err := pattern.ParseTemplate(src)
		if err != nil {
			return "", fmt.Errorf("case %q: %v", tag, err)
		}
		re := tmpl.RegexSource(false)
		if _, err := regexp.Compile(re); err != nil {
			return "", fmt.Errorf("case %q regex: %v", tag, err)
		}
		reVar := fmt.Sprintf("%sRe%d", name, i)
		fmt.Fprintf(&b, "var %s = regexp.MustCompile(%s)\n\n", reVar, goStr(re))
		fmt.Fprintf(&body, "\tif m := %s.FindStringSubmatchIndex(s); m != nil {\n", reVar)
		fmt.Fprintf(&body, "\t\tout.%s = %s\n", fieldName(kindFieldName), goStr(tag))
		if err := g.emitCaptures(&body, tmpl, elemType, "out", "\t\t", "return out, false"); err != nil {
			return "", err
		}
		body.WriteString("\t\treturn out, true\n\t}\n")
	}
	body.WriteString("\treturn out, false\n}")
	b.WriteString(body.String())
	g.imp("regexp")
	g.decls = append(g.decls, b.String())
	return name, nil
}

// kindFieldName is the field a Case: stage adds, naming which alternative
// matched. It mirrors prims.kindField; the two are one contract.
const kindFieldName = "kind"
