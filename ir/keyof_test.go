package ir

import (
	"math/rand"
	"testing"
)

// randKeyable builds a random keyable value: scalars, tuples of keyable
// values, or records with keyable fields, up to a small depth.
func randKeyable(rng *rand.Rand, depth int) Value {
	kinds := 2
	if depth > 0 {
		kinds = 4
	}
	switch rng.Intn(kinds) {
	case 0:
		return int64(rng.Intn(5) - 2)
	case 1:
		texts := []string{"", "a", "ab", "i1;", "t1:a;", "l2:", ";"}
		return texts[rng.Intn(len(texts))]
	case 2:
		n := rng.Intn(3) + 1
		tup := make([]Value, n)
		for i := range tup {
			tup[i] = randKeyable(rng, depth-1)
		}
		return tup
	default:
		r := NewRecordValue()
		names := []string{"a", "b", "c"}
		for _, name := range names[:rng.Intn(3)+1] {
			r.Set(name, randKeyable(rng, depth-1))
		}
		return r
	}
}

// TestKeyOfAgreesWithDeepEqual is the soundness property behind composite
// Map/Set keys: KeyOf(a) == KeyOf(b) exactly when DeepEqual(a, b). The text
// pool above deliberately contains fragments of the encoding itself ("i1;",
// "l2:") to hunt for injection collisions.
func TestKeyOfAgreesWithDeepEqual(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	vals := make([]Value, 400)
	for i := range vals {
		vals[i] = randKeyable(rng, 2)
	}
	for i, a := range vals {
		for _, b := range vals[i:] {
			same := KeyOf(a) == KeyOf(b)
			if deep := DeepEqual(a, b); same != deep {
				t.Fatalf("KeyOf/DeepEqual disagree:\na = %s\nb = %s\nKeyOf equal: %v, DeepEqual: %v",
					FormatValue(a), FormatValue(b), same, deep)
			}
		}
	}
}

// TestKeyOfAdversarialCases pins the collisions a naive encoding would have.
func TestKeyOfAdversarialCases(t *testing.T) {
	tup := func(vs ...Value) Value { return vs }
	distinct := [][2]Value{
		{tup(int64(1), int64(2)), tup(int64(12))},                    // grouping
		{tup("ab", "c"), tup("a", "bc")},                             // string boundaries
		{tup("i1;"), tup(int64(1))},                                  // text mimicking an int encoding
		{tup(int64(1)), int64(1)},                                    // 1-tuple vs bare scalar... both keyable
		{tup(tup(int64(1)), int64(2)), tup(int64(1), tup(int64(2)))}, // nesting shape
		{"t1:a;", tup("a")},                                          // text mimicking a tuple encoding
	}
	for _, pair := range distinct {
		if KeyOf(pair[0]) == KeyOf(pair[1]) {
			t.Errorf("KeyOf collision between distinct values %s and %s",
				FormatValue(pair[0]), FormatValue(pair[1]))
		}
	}

	// Records compare by field name, not declaration order — keys must too.
	ab := NewRecordValue()
	ab.Set("a", int64(1))
	ab.Set("b", int64(2))
	ba := NewRecordValue()
	ba.Set("b", int64(2))
	ba.Set("a", int64(1))
	if KeyOf(ab) != KeyOf(ba) {
		t.Error("records with the same fields in different declaration order should share a key (DeepEqual says they are equal)")
	}

	// Sets of points: membership must be by value, not slice identity.
	s := NewSetValue()
	s.Add(tup(int64(1), int64(2)))
	if !s.Has(tup(int64(1), int64(2))) {
		t.Error("set membership for tuples must be structural")
	}
	if s.Add(tup(int64(1), int64(2))) {
		t.Error("adding a duplicate tuple should report false")
	}
	if s.Has(tup(int64(2), int64(1))) {
		t.Error("(2,1) must not match (1,2)")
	}

	// Maps keyed by points: lookup by value, insertion order preserved.
	m := NewMapValue()
	m.Put(tup(int64(0), int64(0)), int64(10))
	m.Put(tup(int64(0), int64(1)), int64(20))
	m.Put(tup(int64(0), int64(0)), int64(30)) // overwrite, not a new key
	if m.Len() != 2 {
		t.Fatalf("map with tuple keys: got %d keys, want 2", m.Len())
	}
	if v, ok := m.Get(tup(int64(0), int64(0))); !ok || v.(int64) != 30 {
		t.Fatalf("tuple-key lookup: got %v, %v", v, ok)
	}
}
