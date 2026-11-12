package codegen

import "domain/ir"

// The list-keyed collections.
//
// dmSet and dmMap hash their keys with Go's own map, so their key type must be
// comparable, and a List compiles to a slice. A Set or Map whose key type holds
// a List (ir.ContainsList) is therefore a different Go type, dmLSet / dmLMap,
// with the same fields the rest of the generator reads (elems, keys, vs) and the
// same methods (add, contains, items, get, put): the position index is a
// map[string]… keyed by the injective encoding of codegen/keyfn.go, and the
// original keys are kept in insertion order beside it, so iteration, printing
// and conversion read exactly what the plain forms read.
//
// The encoder travels with the value (the kf field), set by the constructor, so
// no method needs a type-specific name. A zero value has no encoder: it can be
// read (it is empty) but not written, the same as a nil Go map.
//
// Programs whose collections have no List in the key never mention any of this,
// and their generated code is unchanged.

// lKeyed reports whether a collection keyed by t uses the list-keyed forms.
func lKeyed(t *ir.Type) bool { return ir.ContainsList(t) }

type lEntry struct{ name, decl string }

// lTable maps a plain helper's name to the list-keyed counterpart.
var lTable = map[string]lEntry{
	"dmSet":          {"dmLSet", declLSet},
	"dmMap":          {"dmLMap", declLMap},
	"dmSetHas":       {"dmLSetHas", declLSetHas},
	"dmMapGet":       {"dmLMapGet", declLMapGet},
	"dmBump":         {"dmLBump", declLBump},
	"dmAppend":       {"dmLAppend", declLAppend},
	"dmMapClone":     {"dmLMapClone", declLMapClone},
	"dmMapWith":      {"dmLMapWith", declLMapWith},
	"dmMapWithout":   {"dmLMapWithout", declLMapWithout},
	"dmSetClone":     {"dmLSetClone", declLSetClone},
	"dmSetWith":      {"dmLSetWith", declLSetWith},
	"dmSetWithout":   {"dmLSetWithout", declLSetWithout},
	"dmToSet":        {"dmLToSet", declLToSet},
	"dmSetUnion":     {"dmLSetUnion", declLSetUnion},
	"dmSetIntersect": {"dmLSetIntersect", declLSetIntersect},
	"dmSetDiff":      {"dmLSetDiff", declLSetDiff},
	"dmMapPutIn":     {"dmLMapPutIn", declLMapPutIn},
	"dmSetAddIn":     {"dmLSetAddIn", declLSetAddIn},
	"dmPluck":        {"dmLPluck", declLPluck},
	"dmMapValues":    {"dmLMapValues", declLMapValues},
}

// chelper declares the helper base (declared by decl) for a collection keyed by
// keyT, or its list-keyed counterpart, and returns the name to call it by.
func (g *gen) chelper(keyT *ir.Type, base, decl string, imports ...string) string {
	if !lKeyed(keyT) {
		g.helper(base, decl, imports...)
		return base
	}
	e, ok := lTable[base]
	if !ok {
		panic("codegen: no list-keyed form of " + base)
	}
	g.helper(e.name, e.decl, imports...)
	return e.name
}

// cname is chelper without declaring anything: the name only.
func cname(keyT *ir.Type, base string) string {
	if lKeyed(keyT) {
		return lTable[base].name
	}
	return base
}

// newSetExpr is the Go expression for an empty Set of element type elem.
func (g *gen) newSetExpr(elem *ir.Type, elemGo string) (string, error) {
	if !lKeyed(elem) {
		g.helper("dmSet", declSet)
		return "dmNewSet[" + elemGo + "]()", nil
	}
	kf, err := g.keyFn(elem)
	if err != nil {
		return "", err
	}
	g.helper("dmLSet", declLSet)
	return "dmNewLSet[" + elemGo + "](" + kf + ")", nil
}

// newMapExpr is the Go expression for an empty Map; capHint is "" or an Int
// expression for the expected size.
func (g *gen) newMapExpr(key *ir.Type, keyGo, valGo, capHint string) (string, error) {
	if !lKeyed(key) {
		g.helper("dmMap", declMap)
		if capHint != "" {
			return "dmNewMapCap[" + keyGo + ", " + valGo + "](" + capHint + ")", nil
		}
		return "dmNewMap[" + keyGo + ", " + valGo + "]()", nil
	}
	kf, err := g.keyFn(key)
	if err != nil {
		return "", err
	}
	g.helper("dmLMap", declLMap)
	if capHint != "" {
		return "dmNewLMapCap[" + keyGo + ", " + valGo + "](" + kf + ", " + capHint + ")", nil
	}
	return "dmNewLMap[" + keyGo + ", " + valGo + "](" + kf + ")", nil
}

const declLSet = `type dmLSet[T any] struct {
	elems []T
	has   map[string]struct{}
	kf    func(T) string
}

func dmNewLSet[T any](kf func(T) string) dmLSet[T] {
	return dmLSet[T]{has: map[string]struct{}{}, kf: kf}
}

func (s *dmLSet[T]) add(v T) {
	k := s.kf(v)
	if _, ok := s.has[k]; !ok {
		s.has[k] = struct{}{}
		s.elems = append(s.elems, v)
	}
}

func (s *dmLSet[T]) contains(v T) bool {
	if len(s.elems) == 0 {
		return false
	}
	_, ok := s.has[s.kf(v)]
	return ok
}

func (s dmLSet[T]) items() []T { return s.elems }`

const declLMap = `type dmLMap[K, V any] struct {
	keys []K
	vs   []V
	idx  map[string]int
	kf   func(K) string
}

func dmNewLMap[K, V any](kf func(K) string) dmLMap[K, V] {
	return dmLMap[K, V]{idx: map[string]int{}, kf: kf}
}

func dmNewLMapCap[K, V any](kf func(K) string, n int) dmLMap[K, V] {
	return dmLMap[K, V]{keys: make([]K, 0, n), vs: make([]V, 0, n), idx: make(map[string]int, n), kf: kf}
}

func (m *dmLMap[K, V]) get(k K) (V, bool) {
	if len(m.keys) > 0 {
		if i, ok := m.idx[m.kf(k)]; ok {
			return m.vs[i], true
		}
	}
	var zero V
	return zero, false
}

func (m *dmLMap[K, V]) put(k K, v V) {
	ks := m.kf(k)
	if i, ok := m.idx[ks]; ok {
		m.vs[i] = v
		return
	}
	m.idx[ks] = len(m.keys)
	m.keys = append(m.keys, k)
	m.vs = append(m.vs, v)
}

func (m *dmLMap[K, V]) has(k K) bool {
	_, ok := m.get(k)
	return ok
}`

const declLSetHas = `func dmLSetHas[T any](s dmLSet[T], v T) bool { return s.contains(v) }`

const declLMapGet = `func dmLMapGet[K, V any](m dmLMap[K, V], k K) V {
	v, ok := m.get(k)
	if !ok {
		dmFail("get: map has no key %v", k)
	}
	return v
}`

const declLBump = `func dmLBump[K any](m *dmLMap[K, int64], k K, d int64) {
	ks := m.kf(k)
	if i, ok := m.idx[ks]; ok {
		m.vs[i] += d
		return
	}
	m.idx[ks] = len(m.keys)
	m.keys = append(m.keys, k)
	m.vs = append(m.vs, d)
}`

const declLAppend = `func dmLAppend[K, V any](m *dmLMap[K, []V], k K, v V) {
	ks := m.kf(k)
	if i, ok := m.idx[ks]; ok {
		m.vs[i] = append(m.vs[i], v)
		return
	}
	m.idx[ks] = len(m.keys)
	m.keys = append(m.keys, k)
	m.vs = append(m.vs, []V{v})
}`

const declLMapClone = `func dmLMapClone[K, V any](m dmLMap[K, V]) dmLMap[K, V] {
	out := dmLMap[K, V]{keys: make([]K, len(m.keys)), vs: make([]V, len(m.vs)), idx: make(map[string]int, len(m.idx)), kf: m.kf}
	copy(out.keys, m.keys)
	copy(out.vs, m.vs)
	for k, i := range m.idx {
		out.idx[k] = i
	}
	return out
}`

const declLMapWith = `func dmLMapWith[K, V any](m dmLMap[K, V], k K, v V) dmLMap[K, V] {
	out := dmLMapClone(m)
	out.put(k, v)
	return out
}`

const declLMapWithout = `func dmLMapWithout[K, V any](m dmLMap[K, V], k K) dmLMap[K, V] {
	if !m.has(k) {
		return dmLMapClone(m)
	}
	ks := m.kf(k)
	out := dmNewLMapCap[K, V](m.kf, len(m.keys)-1)
	for i, key := range m.keys {
		if m.kf(key) != ks {
			out.idx[m.kf(key)] = len(out.keys)
			out.keys = append(out.keys, key)
			out.vs = append(out.vs, m.vs[i])
		}
	}
	return out
}`

const declLSetClone = `func dmLSetClone[T any](s dmLSet[T]) dmLSet[T] {
	out := dmLSet[T]{elems: make([]T, len(s.elems)), has: make(map[string]struct{}, len(s.has)), kf: s.kf}
	copy(out.elems, s.elems)
	for k := range s.has {
		out.has[k] = struct{}{}
	}
	return out
}`

const declLSetWith = `func dmLSetWith[T any](s dmLSet[T], v T) dmLSet[T] {
	out := dmLSetClone(s)
	out.add(v)
	return out
}`

const declLSetWithout = `func dmLSetWithout[T any](s dmLSet[T], v T) dmLSet[T] {
	if !s.contains(v) {
		return dmLSetClone(s)
	}
	vs := s.kf(v)
	out := dmLSet[T]{elems: make([]T, 0, len(s.elems)-1), has: make(map[string]struct{}, len(s.has)-1), kf: s.kf}
	for _, e := range s.elems {
		if k := s.kf(e); k != vs {
			out.elems = append(out.elems, e)
			out.has[k] = struct{}{}
		}
	}
	return out
}`

const declLToSet = `func dmLToSet[T any](xs []T, kf func(T) string) dmLSet[T] {
	out := dmLSet[T]{elems: make([]T, 0, len(xs)), has: make(map[string]struct{}, len(xs)), kf: kf}
	for _, x := range xs {
		out.add(x)
	}
	return out
}`

const declLSetUnion = `func dmLSetUnion[T any](a, b dmLSet[T]) dmLSet[T] {
	kf := a.kf
	if kf == nil {
		kf = b.kf
	}
	out := dmLSet[T]{elems: make([]T, len(a.elems), len(a.elems)+len(b.elems)),
		has: make(map[string]struct{}, len(a.elems)+len(b.elems)), kf: kf}
	copy(out.elems, a.elems)
	for k := range a.has {
		out.has[k] = struct{}{}
	}
	for _, e := range b.elems {
		out.add(e)
	}
	return out
}`

const declLSetIntersect = `func dmLSetIntersect[T any](a, b dmLSet[T]) dmLSet[T] {
	out := dmLSet[T]{has: map[string]struct{}{}, kf: a.kf}
	for _, e := range a.elems {
		if b.contains(e) {
			out.add(e)
		}
	}
	return out
}`

const declLSetDiff = `func dmLSetDiff[T any](a, b dmLSet[T]) dmLSet[T] {
	out := dmLSet[T]{has: map[string]struct{}{}, kf: a.kf}
	for _, e := range a.elems {
		if !b.contains(e) {
			out.add(e)
		}
	}
	return out
}`

const declLMapPutIn = `func dmLMapPutIn[K, V any](m dmLMap[K, V], k K, v V) dmLMap[K, V] {
	m.put(k, v)
	return m
}`

const declLSetAddIn = `func dmLSetAddIn[T any](s dmLSet[T], v T) dmLSet[T] {
	s.add(v)
	return s
}`

const declLPluck = `func dmLPluck[K, V any](m dmLMap[K, V], ks []K) []V {
	out := make([]V, len(ks))
	for i, k := range ks {
		v, ok := m.get(k)
		if !ok {
			dmFail("pluck: no entry for %v", k)
		}
		out[i] = v
	}
	return out
}`

const declLMapValues = `func dmLMapValues[K, V any](m dmLMap[K, V]) []V {
	return append([]V(nil), m.vs...)
}`

// wnewMap / wnewSet emit `v := <empty collection>`.
func (g *gen) wnewMap(v string, key *ir.Type, keyGo, valGo string) error {
	e, err := g.newMapExpr(key, keyGo, valGo, "")
	if err != nil {
		return err
	}
	g.wl("%s := %s", v, e)
	return nil
}

func (g *gen) wnewSet(v string, elem *ir.Type, elemGo string) error {
	e, err := g.newSetExpr(elem, elemGo)
	if err != nil {
		return err
	}
	g.wl("%s := %s", v, e)
	return nil
}

// contains / indexof over a List whose elements hold a List: equality is the
// byte encoding's, which is injective within one static type.
const declLContains = `func dmLContains[T any](xs []T, v T, kf func(T) string) bool {
	k := kf(v)
	for _, e := range xs {
		if kf(e) == k {
			return true
		}
	}
	return false
}`

const declLIndexOf = `func dmLIndexOf[T any](xs []T, v T, kf func(T) string) int64 {
	k := kf(v)
	for i, e := range xs {
		if kf(e) == k {
			return int64(i)
		}
	}
	return -1
}`
