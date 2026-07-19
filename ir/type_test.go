package ir

import "testing"

func TestTupleStringAndEqual(t *testing.T) {
	a := Tuple(Text(), Int())
	if a.String() != "(Text, Int)" {
		t.Fatalf("tuple string: got %q", a.String())
	}
	if !a.Equal(Tuple(Text(), Int())) {
		t.Fatal("identical tuples should be equal")
	}
	if a.Equal(Tuple(Int(), Text())) {
		t.Fatal("tuples differing in element order must not be equal")
	}
	if a.Equal(Tuple(Text())) {
		t.Fatal("tuples of different arity must not be equal")
	}
}

func TestRecordStringAndEqual(t *testing.T) {
	r := Record(Field{"a", Int()}, Field{"b", Int()})
	if r.String() != "{a:Int, b:Int}" {
		t.Fatalf("record string: got %q", r.String())
	}
	// Record equality is insensitive to field declaration order.
	if !r.Equal(Record(Field{"b", Int()}, Field{"a", Int()})) {
		t.Fatal("records with same fields in different order should be equal")
	}
	// Different field type breaks equality.
	if r.Equal(Record(Field{"a", Int()}, Field{"b", Text()})) {
		t.Fatal("records differing in a field type must not be equal")
	}
	// Different field name breaks equality.
	if r.Equal(Record(Field{"a", Int()}, Field{"c", Int()})) {
		t.Fatal("records differing in a field name must not be equal")
	}
	// Different arity breaks equality.
	if r.Equal(Record(Field{"a", Int()})) {
		t.Fatal("records of different arity must not be equal")
	}
}

func TestKindsDoNotCrossEqual(t *testing.T) {
	if Tuple(Int()).Equal(Record(Field{"a", Int()})) {
		t.Fatal("tuple and record must not be equal")
	}
	if List(Int()).Equal(Tuple(Int())) {
		t.Fatal("list and tuple must not be equal")
	}
}

func TestMapSetGridBoolStringAndEqual(t *testing.T) {
	if Bool().String() != "Bool" {
		t.Fatalf("bool string: %q", Bool().String())
	}
	m := Map(Text(), Int())
	if m.String() != "Map<Text, Int>" {
		t.Fatalf("map string: %q", m.String())
	}
	if !m.Equal(Map(Text(), Int())) {
		t.Fatal("identical maps should be equal")
	}
	if m.Equal(Map(Int(), Int())) {
		t.Fatal("maps with different key types must not be equal")
	}
	if m.Equal(Map(Text(), Text())) {
		t.Fatal("maps with different value types must not be equal")
	}

	s := Set(Int())
	if s.String() != "Set<Int>" {
		t.Fatalf("set string: %q", s.String())
	}
	if !s.Equal(Set(Int())) || s.Equal(Set(Text())) {
		t.Fatal("set equality wrong")
	}

	g := Grid(Text())
	if g.String() != "Grid<Text>" {
		t.Fatalf("grid string: %q", g.String())
	}
	if !g.Equal(Grid(Text())) || g.Equal(Grid(Int())) {
		t.Fatal("grid equality wrong")
	}

	// List<List<Int>> still works (regression).
	nested := List(List(Int()))
	if nested.String() != "List<List<Int>>" {
		t.Fatalf("nested list string: %q", nested.String())
	}
}
