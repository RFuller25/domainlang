package codegen

import (
	"fmt"
	"strings"

	"domain/ir"
)

// The first-order list builtins: sort, unique, flatten, product, zip,
// enumerate, chunk, windows, transpose. None takes a function argument, so
// each is a plain generic helper — except the three whose result type is
// element-dependent (sort needs the element's ordering, zip and enumerate
// build a tuple struct), which are interned per type the way eq.go and cmp.go
// intern theirs.
//
// Every one mirrors the primitive of the same job exactly, because the two
// spellings have to answer the same: sort is Sort's ordering, unique keeps
// first-seen order like Unique, chunk keeps a short final block like Chunk,
// windows drops a trailing partial one like Window, and transpose refuses a
// ragged input with the wording Transpose uses.

// listFn interns a generated helper under key, calling build to produce its
// source the first time.
func (g *gen) listFn(key, prefix string, build func(name string) (string, error)) (string, error) {
	if name, ok := g.listFns[key]; ok {
		return name, nil
	}
	name := fmt.Sprintf("dm%s%d", prefix, len(g.listFns)+1)
	g.listFns[key] = name
	decl, err := build(name)
	if err != nil {
		return "", err
	}
	g.decls = append(g.decls, decl)
	return name, nil
}

// sortFn interns a stable sort for one element type, over the same ordering
// ir.Compare defines — so `sort(xs)` in a lambda and a `Sort` stage agree.
func (g *gen) sortFn(elem *ir.Type) (string, error) {
	elemGo, err := g.goType(elem)
	if err != nil {
		return "", err
	}
	return g.listFn("sort:"+canonicalKey(elem), "Sort", func(name string) (string, error) {
		cmp, err := g.cmpExpr("a", "b", elem)
		if err != nil {
			return "", err
		}
		g.imp("slices")
		return fmt.Sprintf(`func %s(xs []%s) []%s {
	out := append([]%s(nil), xs...)
	slices.SortStableFunc(out, func(a, b %s) int { return %s })
	return out
}`, name, elemGo, elemGo, elemGo, elemGo, cmp), nil
	})
}

// pairFn interns zip or enumerate for one output tuple type. Both build a
// tuple struct, which is why neither can be a single generic helper.
func (g *gen) pairFn(kind string, a, b *ir.Type) (string, error) {
	pairT := ir.Tuple(a, b)
	pairGo, err := g.goType(pairT)
	if err != nil {
		return "", err
	}
	aGo, err := g.goType(a)
	if err != nil {
		return "", err
	}
	key := kind + ":" + canonicalKey(pairT)
	return g.listFn(key, strings.ToUpper(kind[:1])+kind[1:], func(name string) (string, error) {
		if kind == "enumerate" {
			bGo, err := g.goType(b)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(`func %s(xs []%s) []%s {
	out := make([]%s, len(xs))
	for i, v := range xs {
		out[i] = %s{int64(i), v}
	}
	return out
}`, name, bGo, pairGo, pairGo, pairGo), nil
		}
		bGo, err := g.goType(b)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`func %s(a []%s, b []%s) []%s {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]%s, n)
	for i := 0; i < n; i++ {
		out[i] = %s{a[i], b[i]}
	}
	return out
}`, name, aGo, bGo, pairGo, pairGo, pairGo), nil
	})
}

// The type-independent halves, as ordinary generic helpers.

const declUniqueList = `func dmUniqueList[T comparable](xs []T) []T {
	seen := make(map[T]struct{}, len(xs))
	out := make([]T, 0, len(xs))
	for _, x := range xs {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}`

const declFlatten = `func dmFlatten[T any](xss [][]T) []T {
	out := []T{}
	for _, xs := range xss {
		out = append(out, xs...)
	}
	return out
}`

const declProduct = `func dmProduct[T int64 | float64](xs []T) T {
	p := T(1)
	for _, x := range xs {
		p *= x
	}
	return p
}`

// declBitReduce is the bitwise counterpart of dmProduct, one per operator. The
// seed is the operator's identity, so the empty list leaves a later fold
// unchanged — the rule sum(0)/product(1) follow, and for `and` that is all bits
// set rather than zero.
func declBitReduce(name string) string {
	seed, op := "0", "|"
	switch name {
	case "bandall":
		seed, op = "-1", "&"
	case "bxorall":
		op = "^"
	}
	return "func dm" + strings.Title(name) + "(xs []int64) int64 {\n" +
		"\tacc := int64(" + seed + ")\n" +
		"\tfor _, x := range xs {\n" +
		"\t\tacc " + op + "= x\n" +
		"\t}\n" +
		"\treturn acc\n}"
}

// dmAnd and dmOr are the *function* spellings of the connectives. They exist
// as functions rather than as `&&`/`||` precisely so both operands are
// evaluated: that is what makes the compiled program agree with the
// interpreter, which evaluates every argument before dispatching.
const declAnd = `func dmAnd(a, b bool) bool {
	return a && b
}`

const declOr = `func dmOr(a, b bool) bool {
	return a || b
}`

const declChunk = `func dmChunk[T any](xs []T, n int64) [][]T {
	if n < 1 {
		dmFail("chunk: size must be >= 1, got %d", n)
	}
	out := [][]T{}
	for i := int64(0); i < int64(len(xs)); i += n {
		hi := i + n
		if hi > int64(len(xs)) {
			hi = int64(len(xs))
		}
		out = append(out, append([]T(nil), xs[i:hi]...))
	}
	return out
}`

const declWindows = `func dmWindows[T any](xs []T, n int64) [][]T {
	if n < 1 {
		dmFail("windows: size must be >= 1, got %d", n)
	}
	out := [][]T{}
	for i := int64(0); i+n <= int64(len(xs)); i++ {
		out = append(out, append([]T(nil), xs[i:i+n]...))
	}
	return out
}`

const declTransposeList = `func dmTransposeList[T any](rows [][]T) [][]T {
	cols := 0
	if len(rows) > 0 {
		cols = len(rows[0])
	}
	for r := range rows {
		if len(rows[r]) != cols {
			dmFail("transpose: grid is not rectangular: row %d has %d cells, expected %d",
				r, len(rows[r]), cols)
		}
	}
	out := make([][]T, cols)
	for c := 0; c < cols; c++ {
		col := make([]T, len(rows))
		for r := range rows {
			col[r] = rows[r][c]
		}
		out[c] = col
	}
	return out
}`
