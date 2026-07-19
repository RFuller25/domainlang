package ir

// Memo caches the results of a keyed computation: Get returns the cached value
// for a key, computing and storing it on first sight. It is the runtime's
// memoization facility for primitive implementers (and is used by codegen to
// intern generated struct declarations). Not safe for concurrent use — like
// the other runtime collections it assumes the single-threaded interpreter.
type Memo[K comparable, V any] struct {
	vals map[K]V
}

// Get returns the memoized value for key, calling compute (once) to produce it
// if the key has not been seen before.
func (m *Memo[K, V]) Get(key K, compute func() V) V {
	if v, ok := m.vals[key]; ok {
		return v
	}
	if m.vals == nil {
		m.vals = map[K]V{}
	}
	v := compute()
	m.vals[key] = v
	return v
}

// Has reports whether key has already been computed.
func (m *Memo[K, V]) Has(key K) bool {
	_, ok := m.vals[key]
	return ok
}

// Len reports the number of memoized keys.
func (m *Memo[K, V]) Len() int { return len(m.vals) }
