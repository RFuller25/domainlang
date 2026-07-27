// Package codegen is the Go compiler backend (v0.3): it walks the same typed
// IR pipeline the interpreter runs — *after* the optimizer, so algorithm
// substitutions (PartialSelect, HashSetPairScan) compile for free — and emits
// a single, fully typed, self-contained Go program that depends only on the
// standard library.
//
// Design notes:
//   - Values are unboxed: Int→int64, Text→string, List<T>→[]T, Records→
//     generated structs, Grid<T>→a tiny generic struct. No []any anywhere.
//   - Using: lambdas are compiled to plain Go expressions and inlined directly
//     into the loops that consume them — no closures, no interface calls.
//   - Match Pattern templates compile to hand-rolled string scanners when the
//     template qualifies (see matchgen.go); otherwise to a package-level
//     compiled regexp.
//   - The interpreter is the correctness oracle: codegen_test.go compiles every
//     anchor program and diffs the binary's stdout against the interpreter's,
//     in both optimized and --no-optimize modes.
//
// Every v0.2 primitive now has a lowering (B.f1): Map/Set values compile to
// insertion-ordered dmMap/dmSet so rendering matches the interpreter,
// Simple Domain loops thread one mutable variable through their emitted
// bodies, Fixed Point convergence and composite `=` share generated dmEqN
// structural-equality functions, and tuple-shaped Match Pattern emits
// positional structs. A primitive added without a codegen case still fails
// with a positioned "not compilable yet" error; `domain run` handles it.
package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// gen accumulates one generated program.
type gen struct {
	main    bytes.Buffer            // statements of func main()
	indent  int                     // current indent depth inside main
	imports map[string]bool         // stdlib packages referenced by emitted code
	decls   []string                // top-level declarations, in creation order
	declSet map[string]bool         // names of helpers already declared
	records ir.Memo[string, string] // interned Record struct names by type key
	tuples  ir.Memo[string, string] // interned Tuple struct names by type key
	fmtFns  map[string]string
	eqFns   map[string]string
	chans   map[string]chanVar
	varn    int
	parsen  int
	release bool // strip Binding Vows (Options.Release)
	// partLabel is the label of the Part block currently being emitted, or ""
	// at the top level. Unlike the interpreter — which must carry the label on
	// ir.Context because an Emit node inside a Part is reached through the
	// Part's Eval closure — the compiler knows every label statically, so it
	// bakes the prefix straight into the emitted print. A compiled binary has
	// no label variable and no runtime branch.
	partLabel string
	// ambient is the stack of enclosing `Simple Domain: For` loop variables,
	// outermost first — the compiled mirror of prims/ambient.go's resolve-time
	// stack. Each entry is the Go variable holding the current lap's element.
	ambient []ambientVar
	// ambientNames maps the *current* lambda's trailing parameter names to
	// those variables. The resolver appends exactly len(ambient) extra
	// parameters to every lambda inside a For body, so the trailing ones are
	// the ambient ones — which is what lets nodeLambda bind them without
	// knowing any primitive's own arity. See compileExpr's Ident case.
	ambientNames exprEnv
}

// ambientVar is one enclosing For loop's current-lap binding in generated Go.
type ambientVar struct {
	v   string
	typ *ir.Type
}

type chanVar struct {
	v   string
	typ *ir.Type
}

// UnsupportedError marks a primitive the compiler backend cannot lower yet.
type UnsupportedError struct {
	Prim string
	Pos  token.Position
	Msg  string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s: the compiler backend cannot compile %s yet (%s); run the program with 'domain run' instead",
		e.Pos, e.Prim, e.Msg)
}

func unsupported(n *ir.Node, format string, args ...any) error {
	return &UnsupportedError{Prim: n.Prim, Pos: n.Pos, Msg: fmt.Sprintf(format, args...)}
}

// Options tunes code generation.
type Options struct {
	// Release compiles Binding Vows out entirely: vow nodes (top-level or
	// nested inside Channel/loop bodies) emit no code, so release binaries
	// carry zero assertion cost.
	Release bool
}

// EmitProgram compiles a resolved (and typically optimized) pipeline into the
// source text of a self-contained Go main package.
func EmitProgram(p *ir.Pipeline, opts Options) (string, error) {
	g := &gen{
		release: opts.Release,
		imports: map[string]bool{},
		declSet: map[string]bool{},
		fmtFns:  map[string]string{},
		eqFns:   map[string]string{},
		chans:   map[string]chanVar{},
	}

	cur, err := g.emitSequence(p.Nodes, "")
	if err != nil {
		return "", err
	}
	if len(p.Nodes) > 0 && p.Nodes[len(p.Nodes)-1].Prim != "Emit" && cur != "" {
		g.wl("_ = %s", cur)
	}

	var out bytes.Buffer
	out.WriteString("// Code generated by \"domain build\". DO NOT EDIT.\n\npackage main\n")
	if len(g.imports) > 0 {
		pkgs := make([]string, 0, len(g.imports))
		for p := range g.imports {
			pkgs = append(pkgs, p)
		}
		sort.Strings(pkgs)
		out.WriteString("\nimport (\n")
		for _, p := range pkgs {
			fmt.Fprintf(&out, "\t%q\n", p)
		}
		out.WriteString(")\n")
	}
	for _, d := range g.decls {
		out.WriteString("\n" + d + "\n")
	}
	out.WriteString("\nfunc main() {\n")
	out.Write(g.main.Bytes())
	out.WriteString("}\n")

	src, err := format.Source(out.Bytes())
	if err != nil {
		return "", fmt.Errorf("internal: generated Go does not parse: %v\n--- generated source ---\n%s", err, out.String())
	}
	return string(src), nil
}

// ---------------------------------------------------------------------------
// low-level emit helpers
// ---------------------------------------------------------------------------

// wl writes one indented line into main().
func (g *gen) wl(f string, args ...any) {
	g.main.WriteString(strings.Repeat("\t", g.indent+1))
	fmt.Fprintf(&g.main, f, args...)
	g.main.WriteByte('\n')
}

func (g *gen) in()  { g.indent++ }
func (g *gen) out() { g.indent-- }

func (g *gen) imp(pkgs ...string) {
	for _, p := range pkgs {
		g.imports[p] = true
	}
}

// helper declares a named top-level helper once, registering its imports.
func (g *gen) helper(name, src string, imports ...string) {
	if g.declSet[name] {
		return
	}
	g.declSet[name] = true
	g.imp(imports...)
	g.decls = append(g.decls, src)
}

func (g *gen) fresh(prefix string) string {
	g.varn++
	return fmt.Sprintf("%s%d", prefix, g.varn)
}

// goStr renders s as a Go string literal.
func goStr(s string) string { return strconv.Quote(s) }

// goByte renders one byte as a Go rune/byte literal (e.g. '-', '\n'), escaping
// as needed. Used when comparing s[i] against a fixed template separator.
func goByte(b byte) string { return strconv.QuoteRune(rune(b)) }

// ---------------------------------------------------------------------------
// node dispatch
// ---------------------------------------------------------------------------

// emitSequence lowers a node list, applying tryFuse's adjacency rewrites before
// each node. Used for the top-level pipeline and for Simple Domain loop bodies,
// so fusions fire identically inside loops.
func (g *gen) emitSequence(nodes []*ir.Node, in string) (string, error) {
	cur := in
	for i := 0; i < len(nodes); i++ {
		if consumed, v, ok, err := g.tryFuse(nodes[i:], cur); err != nil {
			return "", err
		} else if ok {
			cur = v
			i += consumed - 1
			continue
		}
		mark := g.main.Len()
		v, err := g.emitNode(nodes[i], cur)
		if err != nil {
			return "", err
		}
		g.keepAlive(cur, v, mark)
		cur = v
	}
	return cur, nil
}

// keepAlive blanks the previous pipeline variable when the node that just ran
// never mentioned it. A lambda is free to ignore its parameter — `Apply
// Using: (s) -> 5` is legal Domain — and the upstream Go variable is then
// declared and never read, which does not compile. Emitting `_ = prev` costs
// nothing at runtime and keeps the generated program valid.
//
// The check is textual over exactly the lines this node emitted (from mark),
// so the blank is only added when it is actually needed.
func (g *gen) keepAlive(prev, next string, mark int) {
	if prev == "" || prev == next {
		return
	}
	if bytes.Contains(g.main.Bytes()[mark:], []byte(prev)) {
		return
	}
	g.wl("_ = %s", prev)
}

// emitNode lowers one IR node. in is the Go variable holding the current
// pipeline value ("" for the source node); the returned variable holds the
// node's output. Passthrough nodes return in unchanged.
// setConsumers are the primitives that read their input as a sequence and so
// accept a Set wherever they accept a List (prims/higher_order.go's listElem
// is the resolve-time counterpart). A Set compiles to dmSet, which cannot be
// ranged over directly, so the emitted expression is its .elems slice.
//
// The list is explicit rather than "everything except Count": passthroughs
// (Channel, Part, Binding Vow) return their input variable unchanged, and
// rewriting it under them would hand a slice to a downstream node still typed
// as a Set.
// It is exactly the set of primitives whose Build calls listElem — the ones
// that reject anything else by type, like Join and Sum, still do.
var setConsumers = map[string]bool{
	"Chunk": true, "Convert To Set": true, "Count By": true,
	"Count Matching": true, "Enumerate": true, "Filter": true,
	"Find Cycle": true, "Fold": true, "Group By": true, "Map Each": true,
	"Merge Ranges": true, "Pairs": true, "Partition": true,
	"Permutations": true, "Reduce": true, "Scan": true, "Sort By": true,
	"Subsets": true, "Take Item": true, "Unique": true, "Window": true,
}

func (g *gen) emitNode(n *ir.Node, in string) (string, error) {
	if n.In != nil && n.In.Kind == ir.KSet && setConsumers[n.Prim] {
		g.helper("dmSet", declSet)
		in += ".elems"
	}
	switch n.Prim {
	case "Read Source":
		return g.emitReadSource(n)
	case "Split":
		return g.emitSplit(n, in)
	case "Split Each":
		return g.emitSplitEach(n, in)
	case "Convert To Integers":
		return g.emitConvertToIntegers(n, in)
	case "Convert To Floats":
		return g.emitConvertToFloats(n, in)
	case "Convert To Grid":
		return g.emitConvertToGrid(n, in)
	case "Convert To Sparse Grid":
		return g.emitConvertToSparseGrid(n, in)
	case "Sum":
		return g.emitSum(n, in)
	case "Sum Each Group":
		return g.emitSumEachGroup(n, in)
	case "Sort":
		return g.emitSort(n, in)
	case "SelectTopK":
		return g.emitSelectTopK(n, in)
	case "PartialSelect":
		return g.emitPartialSelect(n, in)
	case "All Pairs", "Combinations":
		return g.emitCombinations(n, in)
	case "HashSetPairScan":
		return g.emitHashSetPairScan(n, in)
	case "HashSetDiffScan":
		return g.emitHashSetDiffScan(n, in)
	case "HashSetTripleScan":
		return g.emitHashSetTripleScan(n, in)
	case "DivisorPairScan":
		return g.emitDivisorPairScan(n, in)
	case "WindowedReduce":
		return g.emitWindowedReduce(n, in)
	case "QuickselectItem":
		return g.emitQuickselectItem(n, in)
	case "LinearMapExtremum":
		return g.emitLinearMapExtremum(n, in)
	case "Max", "Min", "Product":
		return g.emitIntReduce(n, in)
	case "Count":
		return g.emitCount(n, in)
	case "Count Matching":
		return g.emitCountMatching(n, in)
	case "Count Cells":
		return g.emitCountCells(n, in)
	case "Map Each":
		return g.emitMapEach(n, in)
	case "Filter":
		return g.emitFilter(n, in)
	case "Apply":
		return g.emitApply(n, in)
	case "Unique":
		return g.emitUnique(n, in)
	case "Reverse":
		return g.emitReverse(n, in)
	case "Take Item":
		return g.emitTakeItem(n, in)
	case "Match Pattern":
		return g.emitMatchPattern(n, in)
	case "Map Cells":
		return g.emitMapCells(n, in)
	case "Transpose":
		return g.emitTranspose(n, in)
	case "Fold":
		return g.emitFold(n, in)
	case "Reduce":
		return g.emitReduce(n, in)
	case "Scan":
		return g.emitScan(n, in)
	case "Pairs":
		return g.emitPairs(n, in)
	case "Take While", "Drop While":
		return g.emitPrefixWhile(n, in)
	case "Chunk":
		return g.emitChunk(n, in)
	case "Partition":
		return g.emitPartition(n, in)
	case "Iterate":
		return g.emitIterate(n, in)
	case "Unfold":
		return g.emitUnfold(n, in)
	case "Any", "All":
		return g.emitQuantifier(n, in)
	case "Find", "Find Index":
		return g.emitFind(n, in)
	case "Sum By", "Product By":
		return g.emitKeyedArithmetic(n, in)
	case "Zip With", "ZipMap":
		return g.emitZipWith(n, in)
	case "Group By":
		return g.emitGroupBy(n, in)
	case "Intersect", "Union":
		return g.emitSetReduce(n, in)
	case "Difference":
		return g.emitDifference(n, in)
	case "Extract Integers":
		return g.emitExtractIntegers(n, in)
	case "Split Fields":
		return g.emitSplitFields(n, in)
	case "Ragged Columns":
		return g.emitRaggedColumns(n, in)
	case "Join":
		return g.emitJoin(n, in)
	case "FoldOver":
		return g.emitFoldOver(n, in)
	case "Window":
		return g.emitWindow(n, in)
	case "Flatten":
		return g.emitFlatten(n, in)
	case "Enumerate":
		return g.emitEnumerate(n, in)
	case "Count By":
		return g.emitCountBy(n, in)
	case "Min By", "Max By":
		return g.emitKeyedExtremum(n, in)
	case "Sort By":
		return g.emitSortBy(n, in)
	case "DifferenceAll":
		return g.emitDifferenceAll(n, in)
	case "Zip":
		return g.emitZip(n, in)
	case "Convert To Set":
		return g.emitConvertToSet(n, in)
	case "Find Cells":
		return g.emitFindCells(n, in)
	case "Merge Ranges":
		return g.emitMergeRanges(n, in)
	case "Permutations":
		return g.emitPermutations(n, in)
	case "Subsets":
		return g.emitSubsets(n, in)
	case "BFS":
		return g.emitBFS(n, in)
	case "Dijkstra":
		return g.emitDijkstra(n, in)
	case "Flood Fill":
		return g.emitFloodFill(n, in)
	case "Range":
		return g.emitRange(n, in)
	case "Topological Sort":
		return g.emitTopologicalSort(n, in)
	case "Subgrid":
		return g.emitSubgrid(n, in)
	case "Pad Grid":
		return g.emitPadGrid(n, in)
	case "Rotate Grid":
		return g.emitRotateGrid(n, in)
	case "Flip Grid":
		return g.emitFlipGrid(n, in)
	case "Convert To Rows":
		return g.emitConvertToRows(n, in)
	case "Find Cycle":
		return g.emitFindCycle(n, in)
	case "Convert To Entries":
		return g.emitConvertToEntries(n, in)
	case "Convert To Map":
		return g.emitConvertToMap(n, in)
	case "Map Values":
		return g.emitMapValues(n, in)
	case "Filter Entries":
		return g.emitFilterEntries(n, in)
	case "Explore":
		return g.emitExplore(n, in)
	case "Connected Components":
		return g.emitConnectedComponents(n, in)
	case "SearchTarget":
		return g.emitSearchTarget(n, in)
	case "Simple Domain (Repeat)", "Simple Domain (While)", "Simple Domain (Fixed Point)",
		"Simple Domain (For)":
		return g.emitLoop(n, in)
	case "Channel":
		return g.emitChannel(n, in)
	case "Part":
		return g.emitPart(n, in)
	case "Combine":
		return g.emitCombine(n, in)
	case "Binding Vow":
		return g.emitBindingVow(n, in)
	case "Emit":
		return g.emitEmit(n, in)
	default:
		return "", unsupported(n, "no Go lowering for this primitive in the MVP")
	}
}

// ---------------------------------------------------------------------------
// sources, splitting, conversion
// ---------------------------------------------------------------------------

func (g *gen) emitReadSource(n *ir.Node) (string, error) {
	target, _ := n.Meta["target"].(string)
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmReadSource", declReadSource, "io", "os", "strings")
	v := g.fresh("v")
	g.wl("%s := dmReadSource(%s)", v, goStr(target))
	return v, nil
}

func (g *gen) emitSplit(n *ir.Node, in string) (string, error) {
	sep, _ := n.Meta["sep"].(string)
	g.imp("strings")
	v := g.fresh("v")
	g.wl("%s := strings.Split(%s, %s)", v, in, goStr(sep))
	return v, nil
}

func (g *gen) emitSplitEach(n *ir.Node, in string) (string, error) {
	sep, _ := n.Meta["sep"].(string)
	g.imp("strings")
	v := g.fresh("v")
	i, s := g.fresh("i"), g.fresh("s")
	g.wl("%s := make([][]string, len(%s))", v, in)
	g.wl("for %s, %s := range %s {", i, s, in)
	g.in()
	g.wl("%s[%s] = strings.Split(%s, %s)", v, i, s, goStr(sep))
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitConvertToIntegers(n *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	v := g.fresh("v")
	if n.In.Equal(ir.List(ir.Text())) {
		i, s := g.fresh("i"), g.fresh("s")
		g.wl("%s := make([]int64, len(%s))", v, in)
		g.wl("for %s, %s := range %s {", i, s, in)
		g.in()
		g.wl("%s[%s] = dmParseInt(%s)", v, i, s)
		g.out()
		g.wl("}")
		return v, nil
	}
	// nested: List<List<Text>> -> List<List<Int>>
	i, grp := g.fresh("i"), g.fresh("g")
	j, s := g.fresh("j"), g.fresh("s")
	row := g.fresh("row")
	g.wl("%s := make([][]int64, len(%s))", v, in)
	g.wl("for %s, %s := range %s {", i, grp, in)
	g.in()
	g.wl("%s := make([]int64, len(%s))", row, grp)
	g.wl("for %s, %s := range %s {", j, s, grp)
	g.in()
	g.wl("%s[%s] = dmParseInt(%s)", row, j, s)
	g.out()
	g.wl("}")
	g.wl("%s[%s] = %s", v, i, row)
	g.out()
	g.wl("}")
	return v, nil
}

// emitConvertToFloats lowers Convert To Floats: Text parses through
// dmParseFloat, Int widens through float64().
func (g *gen) emitConvertToFloats(n *ir.Node, in string) (string, error) {
	conv := func(src string, elem *ir.Type) string {
		if elem.Kind == ir.KInt {
			return "float64(" + src + ")"
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmParseFloat", declParseFloat, "strconv", "strings")
		return "dmParseFloat(" + src + ")"
	}
	v := g.fresh("v")
	if n.In.Kind == ir.KList && n.In.Elem.Kind != ir.KList {
		i, s := g.fresh("i"), g.fresh("s")
		g.wl("%s := make([]float64, len(%s))", v, in)
		g.wl("for %s, %s := range %s {", i, s, in)
		g.in()
		g.wl("%s[%s] = %s", v, i, conv(s, n.In.Elem))
		g.out()
		g.wl("}")
		return v, nil
	}
	// nested: List<List<T>> -> List<List<Float>>
	i, grp := g.fresh("i"), g.fresh("g")
	j, s := g.fresh("j"), g.fresh("s")
	row := g.fresh("row")
	g.wl("%s := make([][]float64, len(%s))", v, in)
	g.wl("for %s, %s := range %s {", i, grp, in)
	g.in()
	g.wl("%s := make([]float64, len(%s))", row, grp)
	g.wl("for %s, %s := range %s {", j, s, grp)
	g.in()
	g.wl("%s[%s] = %s", row, j, conv(s, n.In.Elem.Elem))
	g.out()
	g.wl("}")
	g.wl("%s[%s] = %s", v, i, row)
	g.out()
	g.wl("}")
	return v, nil
}

// ---------------------------------------------------------------------------
// integer reductions
// ---------------------------------------------------------------------------

func (g *gen) emitSum(n *ir.Node, in string) (string, error) {
	acc, err := g.goType(n.Out)
	if err != nil {
		return "", err
	}
	v, x := g.fresh("v"), g.fresh("x")
	g.wl("var %s %s", v, acc)
	g.wl("for _, %s := range %s {", x, in)
	g.in()
	g.wl("%s += %s", v, x)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitSumEachGroup(n *ir.Node, in string) (string, error) {
	v := g.fresh("v")
	i, grp, s, x := g.fresh("i"), g.fresh("g"), g.fresh("s"), g.fresh("x")
	g.wl("%s := make([]int64, len(%s))", v, in)
	g.wl("for %s, %s := range %s {", i, grp, in)
	g.in()
	g.wl("var %s int64", s)
	g.wl("for _, %s := range %s {", x, grp)
	g.in()
	g.wl("%s += %s", s, x)
	g.out()
	g.wl("}")
	g.wl("%s[%s] = %s", v, i, s)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitIntReduce(n *ir.Node, in string) (string, error) {
	g.helper("dmFail", declFail, "fmt", "os")
	v, x := g.fresh("v"), g.fresh("x")
	g.wl("if len(%s) == 0 {", in)
	g.in()
	g.wl("dmFail(%s)", goStr(fmt.Sprintf("%s of an empty list is undefined", n.Prim)))
	g.out()
	g.wl("}")
	g.wl("%s := %s[0]", v, in)
	g.wl("for _, %s := range %s[1:] {", x, in)
	g.in()
	switch n.Prim {
	case "Max":
		g.wl("if %s > %s {", x, v)
		g.in()
		g.wl("%s = %s", v, x)
		g.out()
		g.wl("}")
	case "Min":
		g.wl("if %s < %s {", x, v)
		g.in()
		g.wl("%s = %s", v, x)
		g.out()
		g.wl("}")
	case "Product":
		g.wl("%s *= %s", v, x)
	}
	g.out()
	g.wl("}")
	return v, nil
}

// ---------------------------------------------------------------------------
// sorting and selection
// ---------------------------------------------------------------------------

func (g *gen) emitSort(n *ir.Node, in string) (string, error) {
	desc, _ := n.Meta["desc"].(bool)
	elem, err := g.goType(n.In.Elem)
	if err != nil {
		return "", err
	}
	g.imp("sort")
	v := g.fresh("v")
	g.wl("%s := append([]%s(nil), %s...)", v, elem, in)
	// Int, Float and Text lower to Go's own <, which is already the order the
	// interpreter uses. A tuple element needs the lexicographic chain.
	lt, err := lessExpr(n.In.Elem, v+"[i]", v+"[j]")
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	if desc {
		if lt, err = lessExpr(n.In.Elem, v+"[j]", v+"[i]"); err != nil {
			return "", unsupported(n, "%v", err)
		}
	}
	// SliceStable, not Slice: for a tuple element the comparison is not a
	// strict total order over *equal* keys, and an unstable sort would then
	// permute ties differently from the interpreter's stable one.
	g.wl("sort.SliceStable(%s, func(i, j int) bool { return %s })", v, lt)
	return v, nil
}

// lessExpr builds a Go expression for `a < b` over an ordered Domain type.
// Scalars use Go's own operator; a tuple compares lexicographically, first
// differing element deciding — matching ir.Compare exactly, which is what
// keeps a compiled sort byte-identical to the interpreter's.
func lessExpr(t *ir.Type, a, b string) (string, error) {
	if t == nil {
		return "", fmt.Errorf("sort needs an element type")
	}
	switch t.Kind {
	case ir.KInt, ir.KFloat, ir.KText:
		return a + " < " + b, nil
	case ir.KTuple:
		// Built right to left: f0 < f0 || (f0 == f0 && (f1 < f1 || ...)).
		expr := ""
		for i := len(t.Elems) - 1; i >= 0; i-- {
			af := a + "." + tupleField(i)
			bf := b + "." + tupleField(i)
			inner, err := lessExpr(t.Elems[i], af, bf)
			if err != nil {
				return "", err
			}
			if expr == "" {
				expr = inner
				continue
			}
			expr = "(" + inner + " || (" + af + " == " + bf + " && " + expr + "))"
		}
		if expr == "" {
			return "", fmt.Errorf("cannot sort by an empty tuple")
		}
		return expr, nil
	}
	return "", fmt.Errorf("cannot sort by %s (not an ordered type)", t)
}

func (g *gen) emitSelectTopK(n *ir.Node, in string) (string, error) {
	k, _ := n.Meta["k"].(int64)
	thenSum, _ := n.Meta["sum"].(bool)
	kk := int(k)
	if kk < 0 {
		kk = 0
	}
	cnt := g.fresh("n")
	g.wl("%s := %d", cnt, kk)
	g.wl("if %s > len(%s) {", cnt, in)
	g.in()
	g.wl("%s = len(%s)", cnt, in)
	g.out()
	g.wl("}")
	v := g.fresh("v")
	if thenSum {
		x := g.fresh("x")
		g.wl("var %s int64", v)
		g.wl("for _, %s := range %s[:%s] {", x, in, cnt)
		g.in()
		g.wl("%s += %s", v, x)
		g.out()
		g.wl("}")
		return v, nil
	}
	g.wl("%s := append([]int64(nil), %s[:%s]...)", v, in, cnt)
	return v, nil
}

func (g *gen) emitPartialSelect(n *ir.Node, in string) (string, error) {
	k, _ := n.Meta["k"].(int64)
	desc, _ := n.Meta["desc"].(bool)
	thenSum, _ := n.Meta["sum"].(bool)
	g.helper("dmTopK", declTopK, "sort")
	if !thenSum {
		v := g.fresh("v")
		g.wl("%s := dmTopK(%s, %d, %v)", v, in, int(k), desc)
		return v, nil
	}
	top := g.fresh("top")
	g.wl("%s := dmTopK(%s, %d, %v)", top, in, int(k), desc)
	v, x := g.fresh("v"), g.fresh("x")
	g.wl("var %s int64", v)
	g.wl("for _, %s := range %s {", x, top)
	g.in()
	g.wl("%s += %s", v, x)
	g.out()
	g.wl("}")
	return v, nil
}

// ---------------------------------------------------------------------------
// pair/combination scans
// ---------------------------------------------------------------------------

// emitCombinations lowers the naive All Pairs / Combinations node to k nested
// loops with the predicate/mapper inlined — the loop nest is fully unrolled at
// compile time because k is a program constant.
func (g *gen) emitCombinations(n *ir.Node, in string) (string, error) {
	k, _ := n.Meta["k"].(int)
	mode, _ := n.Meta["mode"].(string)
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil {
		return "", unsupported(n, "missing lambda metadata")
	}
	elem := n.In.Elem
	elemGo, err := g.goType(elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}

	// Bind each lambda parameter to a per-depth element variable.
	idxVars := make([]string, k)
	elemVars := make([]string, k)
	env := exprEnv{}
	for d := 0; d < k; d++ {
		idxVars[d] = g.fresh("i")
		elemVars[d] = g.fresh("c")
		env[lam.Params[d]] = exprBinding{expr: elemVars[d], typ: elem}
	}
	body, _, err := g.compileExpr(lam.Body, env)
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}

	v := g.fresh("v")
	var found, label string
	switch mode {
	case "Count":
		g.wl("var %s int64", v)
	case "Filter":
		g.wl("%s := [][]%s{}", v, elemGo)
	case "First":
		found = g.fresh("found")
		label = g.fresh("scan")
		g.wl("var %s []%s", v, elemGo)
		g.wl("%s := false", found)
		g.wl("%s:", label)
	case "Map":
		outElemGo, err := g.goType(n.Out.Elem)
		if err != nil {
			return "", unsupported(n, "%v", err)
		}
		g.wl("%s := []%s{}", v, outElemGo)
	default:
		return "", unsupported(n, "mode %q", mode)
	}

	for d := 0; d < k; d++ {
		start := "0"
		if d > 0 {
			start = idxVars[d-1] + " + 1"
		}
		g.wl("for %s := %s; %s < len(%s); %s++ {", idxVars[d], start, idxVars[d], in, idxVars[d])
		g.in()
		g.wl("%s := %s[%s]", elemVars[d], in, idxVars[d])
	}

	combo := "[]" + elemGo + "{" + strings.Join(elemVars, ", ") + "}"
	switch mode {
	case "Count":
		g.wl("if %s {", body)
		g.in()
		g.wl("%s++", v)
		g.out()
		g.wl("}")
	case "Filter":
		g.wl("if %s {", body)
		g.in()
		g.wl("%s = append(%s, %s)", v, v, combo)
		g.out()
		g.wl("}")
	case "First":
		g.wl("if %s {", body)
		g.in()
		g.wl("%s = %s", v, combo)
		g.wl("%s = true", found)
		g.wl("break %s", label)
		g.out()
		g.wl("}")
	case "Map":
		g.wl("%s = append(%s, %s)", v, v, body)
	}

	for d := 0; d < k; d++ {
		g.out()
		g.wl("}")
	}
	if mode == "First" {
		g.helper("dmFail", declFail, "fmt", "os")
		g.wl("if !%s {", found)
		g.in()
		g.wl("dmFail(%s)", goStr("no combination satisfied the predicate"))
		g.out()
		g.wl("}")
	}
	return v, nil
}

// emitHashSetPairScan lowers the optimizer's O(n) rewrite of the
// sum-to-constant pair search.
func (g *gen) emitHashSetPairScan(n *ir.Node, in string) (string, error) {
	mode, _ := n.Meta["mode"].(string)
	target, ok := n.Meta["target"].(int64)
	if !ok {
		return "", unsupported(n, "missing target metadata")
	}
	v := g.fresh("v")
	x := g.fresh("x")
	if mode == "Count" {
		seen := g.fresh("seen")
		g.wl("%s := make(map[int64]int64, len(%s))", seen, in)
		g.wl("var %s int64", v)
		g.wl("for _, %s := range %s {", x, in)
		g.in()
		g.wl("%s += %s[%d-%s]", v, seen, target, x)
		g.wl("%s[%s]++", seen, x)
		g.out()
		g.wl("}")
		return v, nil
	}
	// First
	g.helper("dmFail", declFail, "fmt", "os")
	rem := g.fresh("rem")
	found := g.fresh("found")
	g.wl("%s := make(map[int64]int, len(%s))", rem, in)
	g.wl("for _, %s := range %s {", x, in)
	g.in()
	g.wl("%s[%s]++", rem, x)
	g.out()
	g.wl("}")
	g.wl("var %s []int64", v)
	g.wl("%s := false", found)
	y := g.fresh("x")
	g.wl("for _, %s := range %s {", y, in)
	g.in()
	g.wl("%s[%s]--", rem, y)
	g.wl("if %s[%d-%s] > 0 {", rem, target, y)
	g.in()
	g.wl("%s = []int64{%s, %d - %s}", v, y, target, y)
	g.wl("%s = true", found)
	g.wl("break")
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	g.wl("if !%s {", found)
	g.in()
	g.wl("dmFail(%s)", goStr("no combination satisfied the predicate"))
	g.out()
	g.wl("}")
	return v, nil
}

// emitHashSetDiffScan lowers the optimizer's O(n) rewrite of the
// difference-to-constant pair search (optimizer.fusePairDiff).
func (g *gen) emitHashSetDiffScan(n *ir.Node, in string) (string, error) {
	mode, _ := n.Meta["mode"].(string)
	target, ok := n.Meta["target"].(int64)
	if !ok {
		return "", unsupported(n, "missing target metadata")
	}
	flipped, _ := n.Meta["flipped"].(bool)
	v := g.fresh("v")
	if mode == "Count" {
		g.helper("dmDiffCount", declDiffCount)
		g.wl("%s := dmDiffCount(%s, %d, %v)", v, in, target, flipped)
		return v, nil
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmDiffFirst", declDiffFirst)
	okv := g.fresh("ok")
	g.wl("%s, %s := dmDiffFirst(%s, %d, %v)", v, okv, in, target, flipped)
	g.wl("if !%s {", okv)
	g.in()
	g.wl("dmFail(%s)", goStr("no combination satisfied the predicate"))
	g.out()
	g.wl("}")
	return v, nil
}

// emitHashSetTripleScan lowers the optimizer's O(n²) rewrite of the
// sum-to-constant triple search (optimizer.fuseTripleSum).
func (g *gen) emitHashSetTripleScan(n *ir.Node, in string) (string, error) {
	mode, _ := n.Meta["mode"].(string)
	target, ok := n.Meta["target"].(int64)
	if !ok {
		return "", unsupported(n, "missing target metadata")
	}
	v := g.fresh("v")
	if mode == "Count" {
		g.helper("dmTripleCount", declTripleCount)
		g.wl("%s := dmTripleCount(%s, %d)", v, in, target)
		return v, nil
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmTripleFirst", declTripleFirst)
	okv := g.fresh("ok")
	g.wl("%s, %s := dmTripleFirst(%s, %d)", v, okv, in, target)
	g.wl("if !%s {", okv)
	g.in()
	g.wl("dmFail(%s)", goStr("no combination satisfied the predicate"))
	g.out()
	g.wl("}")
	return v, nil
}

// emitDivisorPairScan lowers the optimizer's O(n) rewrite of the
// product-to-constant pair search (optimizer.fuseAllPairsProduct).
func (g *gen) emitDivisorPairScan(n *ir.Node, in string) (string, error) {
	mode, _ := n.Meta["mode"].(string)
	target, ok := n.Meta["target"].(int64)
	if !ok {
		return "", unsupported(n, "missing target metadata")
	}
	v := g.fresh("v")
	if mode == "Count" {
		g.helper("dmProductCount", declProductCount)
		g.wl("%s := dmProductCount(%s, %d)", v, in, target)
		return v, nil
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmProductFirst", declProductFirst)
	okv := g.fresh("ok")
	g.wl("%s, %s := dmProductFirst(%s, %d)", v, okv, in, target)
	g.wl("if !%s {", okv)
	g.in()
	g.wl("dmFail(%s)", goStr("no combination satisfied the predicate"))
	g.out()
	g.wl("}")
	return v, nil
}

// emitQuickselectItem lowers the optimizer's Sort + Take Item fusion: the
// kth order statistic via dmTopK, with Take Item's bounds behavior.
func (g *gen) emitQuickselectItem(n *ir.Node, in string) (string, error) {
	idx, _ := n.Meta["index"].(int)
	desc, _ := n.Meta["desc"].(bool)
	g.helper("dmFail", declFail, "fmt", "os")
	if idx < 0 {
		// A constant negative index always fails (mirrors emitTakeItem).
		g.wl(`dmFail("index %d out of range (length %%d)", len(%s))`, idx, in)
		v := g.fresh("v")
		g.wl("var %s int64", v)
		return v, nil
	}
	g.helper("dmSelectItem", declSelectItem)
	g.wl("if len(%s) <= %d {", in, idx)
	g.in()
	g.wl(`dmFail("index %d out of range (length %%d)", len(%s))`, idx, in)
	g.out()
	g.wl("}")
	v := g.fresh("v")
	// A single index reads one order statistic, so quickselect it directly
	// instead of sorting the whole k-front (dmTopK).
	g.wl("%s := dmSelectItem(%s, %d, %v)", v, in, idx, desc)
	return v, nil
}

// emitLinearMapExtremum lowers the optimizer's monotone map/extremum swap:
// reduce the input first, then inline the lambda body once over the result.
func (g *gen) emitLinearMapExtremum(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	pickMin, _ := n.Meta["pickMin"].(bool)
	reduce, _ := n.Meta["reduce"].(string)
	g.helper("dmFail", declFail, "fmt", "os")
	g.wl("if len(%s) == 0 {", in)
	g.in()
	g.wl("dmFail(%s)", goStr(fmt.Sprintf("%s of an empty list is undefined", reduce)))
	g.out()
	g.wl("}")
	ext, x := g.fresh("ext"), g.fresh("x")
	g.wl("%s := %s[0]", ext, in)
	g.wl("for _, %s := range %s[1:] {", x, in)
	g.in()
	op := ">"
	if pickMin {
		op = "<"
	}
	g.wl("if %s %s %s {", x, op, ext)
	g.in()
	g.wl("%s = %s", ext, x)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: ext, typ: ir.Int()}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	v := g.fresh("v")
	g.wl("%s := %s", v, body)
	return v, nil
}

// ---------------------------------------------------------------------------
// list transforms
// ---------------------------------------------------------------------------

func (g *gen) nodeLambda(n *ir.Node) (*ast.Lambda, error) {
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil {
		return nil, unsupported(n, "missing lambda metadata")
	}
	g.bindAmbientParams(lam)
	return lam, nil
}

// bindAmbientParams records how the lambda about to be compiled names the
// enclosing For loops' variables. Inside a For body the resolver gives every
// `Using:` lambda len(ambient) extra trailing parameters, bound positionally
// outermost-first — so the trailing slice is exactly the ambient one, and no
// primitive's own arity needs to be known here.
//
// Callers reach a lambda through nodeLambda and compile it immediately, and
// codegen is single-pass and single-threaded, so the mapping is only ever
// consulted for the lambda that just went through here. A leading parameter
// that happens to share a name still wins: compileExpr checks the caller's
// env first, and this map is only the fallback.
func (g *gen) bindAmbientParams(lam *ast.Lambda) {
	k := len(g.ambient)
	if k == 0 || len(lam.Params) < k {
		g.ambientNames = nil
		return
	}
	names := make(exprEnv, k)
	for i, p := range lam.Params[len(lam.Params)-k:] {
		names[p] = exprBinding{expr: g.ambient[i].v, typ: g.ambient[i].typ}
	}
	g.ambientNames = names
}

func (g *gen) emitMapEach(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	outElemGo, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, i, e := g.fresh("v"), g.fresh("i"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := make([]%s, len(%s))", v, outElemGo, in)
	g.wl("for %s, %s := range %s {", i, e, in)
	g.in()
	g.wl("%s[%s] = %s", v, i, body)
	g.out()
	g.wl("}")
	return v, nil
}

// emitSplitMapSum lowers Split(sep) + Map Each(scalar) + Sum: walk the un-split
// string line by line (exactly as strings.Split segments it) and accumulate the
// map lambda's result, so the []string of lines is never materialized.
func (g *gen) emitSplitMapSum(sep string, mapNode, sumNode *ir.Node, in string) (string, error) {
	g.imp("strings")
	lam, err := g.nodeLambda(mapNode)
	if err != nil {
		return "", err
	}
	acc, err := g.goType(sumNode.Out)
	if err != nil {
		return "", unsupported(sumNode, "%v", err)
	}
	v := g.fresh("v")
	str, idx, line := g.fresh("str"), g.fresh("idx"), g.fresh("line")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: line, typ: mapNode.In.Elem}})
	if err != nil {
		return "", unsupported(mapNode, "lambda: %v", err)
	}
	g.wl("var %s %s", v, acc)
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
	g.wl("%s += %s", v, body)
	g.wl("if %s < 0 { break }", idx)
	g.wl("%s = %s[%s+%d:]", str, str, idx, len(sep))
	g.out()
	g.wl("}")
	return v, nil
}

// emitMapSum lowers Map Each immediately followed by Sum: the lambda result is
// accumulated into a scalar, never materializing the mapped list.
func (g *gen) emitMapSum(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	acc, err := g.goType(n.Out.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, e := g.fresh("v"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("var %s %s", v, acc)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.wl("%s += %s", v, body)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitFilter(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, e := g.fresh("v"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("%s := []%s{}", v, elemGo)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.wl("if %s {", body)
	g.in()
	g.wl("%s = append(%s, %s)", v, v, e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitCountMatching(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	v, e := g.fresh("v"), g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: e, typ: n.In.Elem}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("var %s int64", v)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.wl("if %s {", body)
	g.in()
	g.wl("%s++", v)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitApply(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	body, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: in, typ: n.In}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	v := g.fresh("v")
	// Declared with its type rather than inferred with `:=`. A body that is a
	// bare integer literal (`Apply Using: (s) -> 3`, legal Domain) is an
	// untyped Go constant, and `:=` would make it `int` — which then fails to
	// assign anywhere an int64 is expected downstream.
	if goT, err := g.goType(n.Out); err == nil {
		g.wl("var %s %s = %s", v, goT, body)
		return v, nil
	}
	g.wl("%s := %s", v, body)
	return v, nil
}

func (g *gen) emitCount(n *ir.Node, in string) (string, error) {
	v := g.fresh("v")
	switch n.In.Kind {
	case ir.KList:
		g.wl("%s := int64(len(%s))", v, in)
	case ir.KSet:
		g.wl("%s := int64(len(%s.elems))", v, in)
	default:
		return "", unsupported(n, "Count over %s", n.In)
	}
	return v, nil
}

func (g *gen) emitFold(n *ir.Node, in string) (string, error) {
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	acc := g.fresh("acc")
	switch seed := n.Meta["seed"].(type) {
	case int64:
		g.wl("%s := int64(%d)", acc, seed)
	case string:
		g.wl("%s := %s", acc, goStr(seed))
	default:
		return "", unsupported(n, "seed of type %T", seed)
	}
	e := g.fresh("e")
	body, _, err := g.compileExpr(lam.Body, exprEnv{
		lam.Params[0]: {expr: acc, typ: n.Out},
		lam.Params[1]: {expr: e, typ: n.In.Elem},
	})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.wl("%s = %s", acc, body)
	g.out()
	g.wl("}")
	return acc, nil
}

func (g *gen) emitUnique(n *ir.Node, in string) (string, error) {
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, seen, e := g.fresh("v"), g.fresh("seen"), g.fresh("e")
	g.wl("%s := []%s{}", v, elemGo)
	g.wl("%s := make(map[%s]struct{}, len(%s))", seen, elemGo, in)
	g.wl("for _, %s := range %s {", e, in)
	g.in()
	g.wl("if _, ok := %s[%s]; !ok {", seen, e)
	g.in()
	g.wl("%s[%s] = struct{}{}", seen, e)
	g.wl("%s = append(%s, %s)", v, v, e)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitReverse(n *ir.Node, in string) (string, error) {
	if n.In != nil && n.In.Kind == ir.KText {
		g.helper("dmReverseText", declReverseText)
		v := g.fresh("v")
		g.wl("%s := dmReverseText(%s)", v, in)
		return v, nil
	}
	elemGo, err := g.goType(n.In.Elem)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v, i, e := g.fresh("v"), g.fresh("i"), g.fresh("e")
	g.wl("%s := make([]%s, len(%s))", v, elemGo, in)
	g.wl("for %s, %s := range %s {", i, e, in)
	g.in()
	g.wl("%s[len(%s)-1-%s] = %s", v, in, i, e)
	g.out()
	g.wl("}")
	return v, nil
}

func (g *gen) emitTakeItem(n *ir.Node, in string) (string, error) {
	idx, _ := n.Meta["index"].(int)
	g.helper("dmFail", declFail, "fmt", "os")
	if idx < 0 {
		// A constant negative index always fails; emit the failure plus a
		// zero-valued binding so downstream code still compiles.
		elemGo, err := g.goType(n.Out)
		if err != nil {
			return "", unsupported(n, "%v", err)
		}
		g.wl(`dmFail("index %d out of range (length %%d)", len(%s))`, idx, in)
		v := g.fresh("v")
		g.wl("var %s %s", v, elemGo)
		return v, nil
	}
	g.wl("if len(%s) <= %d {", in, idx)
	g.in()
	g.wl(`dmFail("index %d out of range (length %%d)", len(%s))`, idx, in)
	g.out()
	g.wl("}")
	v := g.fresh("v")
	g.wl("%s := %s[%d]", v, in, idx)
	return v, nil
}

// ---------------------------------------------------------------------------
// channels
// ---------------------------------------------------------------------------

func (g *gen) emitChannel(n *ir.Node, in string) (string, error) {
	name, _ := n.Meta["name"].(string)
	subNodes, _ := n.Meta["nodes"].([]*ir.Node)
	if subNodes == nil {
		return "", unsupported(n, "missing channel body metadata")
	}
	cur, err := g.emitSequence(subNodes, in)
	if err != nil {
		return "", err
	}
	// Fusions preserve the pipeline value type, so the channel's output type is
	// the last sub-node's declared output.
	curType := n.In
	if len(subNodes) > 0 {
		curType = subNodes[len(subNodes)-1].Out
	}
	g.chans[name] = chanVar{v: cur, typ: curType}
	return in, nil // a Channel is a passthrough for the main pipeline
}

func (g *gen) emitCombine(n *ir.Node, in string) (string, error) {
	froms, _ := n.Meta["from"].([]string)
	lam, err := g.nodeLambda(n)
	if err != nil {
		return "", err
	}
	env := exprEnv{}
	for i, name := range froms {
		cv, ok := g.chans[name]
		if !ok {
			return "", unsupported(n, "channel %q was not compiled", name)
		}
		env[lam.Params[i]] = exprBinding{expr: cv.v, typ: cv.typ}
	}
	body, _, err := g.compileExpr(lam.Body, env)
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	v := g.fresh("v")
	g.wl("%s := %s", v, body)
	return v, nil
}

// ---------------------------------------------------------------------------
// vows and the sink
// ---------------------------------------------------------------------------

func (g *gen) emitBindingVow(n *ir.Node, in string) (string, error) {
	if g.release {
		return in, nil // release mode: don't even emit the check
	}
	kind, _ := n.Meta["kind"].(string)
	raw, _ := n.Meta["raw"].(string)
	g.helper("dmFail", declFail, "fmt", "os")
	switch kind {
	case "count":
		if n.In.Kind != ir.KList {
			return "", unsupported(n, "Count vow over %s", n.In)
		}
		want, _ := n.Meta["want"].(int64)
		g.wl("if int64(len(%s)) != %d {", in, want)
		g.in()
		g.wl(`dmFail("vow violated [%%s]: expected count %%d, got %%d", %s, int64(%d), len(%s))`,
			goStr(raw), want, in)
		g.out()
		g.wl("}")
		return in, nil
	case "allvalues":
		if !n.In.Equal(ir.List(ir.Int())) {
			return "", unsupported(n, "All Values vow over %s", n.In)
		}
		sym, _ := n.Meta["sym"].(string)
		bound, _ := n.Meta["bound"].(int64)
		goOp := sym
		if sym == "=" {
			goOp = "=="
		}
		i, x := g.fresh("i"), g.fresh("x")
		g.wl("for %s, %s := range %s {", i, x, in)
		g.in()
		g.wl("if !(%s %s %d) {", x, goOp, bound)
		g.in()
		g.wl(`dmFail("vow violated [%%s]: element %%d (%%d) violates value %s %d", %s, %s, %s)`,
			sym, bound, goStr(raw), i, x)
		g.out()
		g.wl("}")
		g.out()
		g.wl("}")
		return in, nil
	case "holds":
		// The general form: any predicate over the current value, compiled
		// inline like every other lambda.
		lam, err := g.nodeLambda(n)
		if err != nil {
			return "", err
		}
		pred, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: in, typ: n.In}})
		if err != nil {
			return "", unsupported(n, "predicate: %v", err)
		}
		g.wl("if !(%s) {", pred)
		g.in()
		g.wl(`dmFail("vow violated [%%s]: predicate is false", %s)`, goStr(raw))
		g.out()
		g.wl("}")
		return in, nil
	default:
		return "", unsupported(n, "vow kind %q", kind)
	}
}

func (g *gen) emitEmit(n *ir.Node, in string) (string, error) {
	g.imp("fmt")
	if g.partLabel != "" {
		return g.emitLabelledEmit(n, in)
	}
	// `Reveal: stderr` picks the other stream. Like the Part label, the target
	// is a compile-time literal, so the binary has no runtime branch.
	println, printf := "fmt.Println(%s)", "fmt.Println(%s(%s))"
	if target, _ := n.Meta["target"].(string); target == "stderr" {
		g.imp("os")
		println, printf = "fmt.Fprintln(os.Stderr, %s)", "fmt.Fprintln(os.Stderr, %s(%s))"
	}
	switch n.In.Kind {
	case ir.KInt, ir.KText, ir.KBool:
		g.wl(println, in)
	case ir.KFloat:
		// fmt's %v for float64 is exactly strconv.FormatFloat('g', -1), the
		// same rendering ir.FormatValue uses — but keep it explicit so the
		// parity contract is visible in the generated source.
		g.helper("dmFmtFloat", declFmtFloat, "strconv")
		g.wl(printf, "dmFmtFloat", in)
	default:
		fn, err := g.fmtFunc(n.In)
		if err != nil {
			return "", unsupported(n, "cannot render %s: %v", n.In, err)
		}
		g.wl(printf, fn, in)
	}
	return in, nil
}

// emitLabelledEmit is Reveal inside a Part block. The label is a compile-time
// literal, so it is baked into the call; only the single-line/multi-line choice
// has to happen at runtime, and only because Text (and every composite) can
// contain a newline. dmLabel mirrors ir.LabelledOutput exactly — that pairing is
// what keeps the two backends byte-identical.
func (g *gen) emitLabelledEmit(n *ir.Node, in string) (string, error) {
	rendered, err := g.renderToString(n, in)
	if err != nil {
		return "", err
	}
	g.helper("dmLabel", declLabel, "strings")
	g.wl("fmt.Println(dmLabel(%s, %s))", goStr(g.partLabel), rendered)
	return in, nil
}

// renderToString returns a Go expression of type string rendering the node's
// input value exactly as ir.FormatValue would.
func (g *gen) renderToString(n *ir.Node, in string) (string, error) {
	switch n.In.Kind {
	case ir.KText:
		return in, nil
	case ir.KInt, ir.KBool:
		return fmt.Sprintf("fmt.Sprint(%s)", in), nil
	case ir.KFloat:
		g.helper("dmFmtFloat", declFmtFloat, "strconv")
		return fmt.Sprintf("dmFmtFloat(%s)", in), nil
	default:
		fn, err := g.fmtFunc(n.In)
		if err != nil {
			return "", unsupported(n, "cannot render %s: %v", n.In, err)
		}
		return fmt.Sprintf("%s(%s)", fn, in), nil
	}
}

// emitPart emits a Part block: its body inline, with the label in scope for any
// Reveal inside it, and the main pipeline value passed through untouched.
func (g *gen) emitPart(n *ir.Node, in string) (string, error) {
	label, _ := n.Meta["label"].(string)
	subNodes, _ := n.Meta["nodes"].([]*ir.Node)
	if subNodes == nil {
		return "", unsupported(n, "missing part body metadata")
	}
	prev := g.partLabel
	g.partLabel = label
	defer func() { g.partLabel = prev }()

	cur, err := g.emitSequence(subNodes, in)
	if err != nil {
		return "", err
	}
	// A Part's own result is discarded (only its Reveal is observable), so a
	// body that does not end in Emit leaves a value Go would reject as unused.
	if cur != "" && cur != in && subNodes[len(subNodes)-1].Prim != "Emit" {
		g.wl("_ = %s", cur)
	}
	return in, nil // a Part is a passthrough for the main pipeline
}
