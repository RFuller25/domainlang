package ir

import "testing"

func TestAsListAndAsInt(t *testing.T) {
	l, err := AsList([]Value{int64(1), int64(2)})
	if err != nil || len(l) != 2 {
		t.Fatalf("AsList: %v %v", l, err)
	}
	if _, err := AsList(int64(3)); err == nil {
		t.Fatal("AsList on a non-list should error")
	}

	n, err := AsInt(int64(5))
	if err != nil || n != 5 {
		t.Fatalf("AsInt: %v %v", n, err)
	}
	if _, err := AsInt("not an int"); err == nil {
		t.Fatal("AsInt on Text should error")
	}
}

func TestAsIntSliceAndIntsToValue(t *testing.T) {
	xs, err := AsIntSlice([]Value{int64(1), int64(2), int64(3)})
	if err != nil {
		t.Fatal(err)
	}
	if len(xs) != 3 || xs[1] != 2 {
		t.Fatalf("AsIntSlice: %v", xs)
	}

	if _, err := AsIntSlice([]Value{int64(1), "oops"}); err == nil {
		t.Fatal("AsIntSlice should error on a non-int element")
	}
	if _, err := AsIntSlice("not a list"); err == nil {
		t.Fatal("AsIntSlice should error on a non-list")
	}

	back := IntsToValue(xs)
	if len(back) != 3 || back[2].(int64) != 3 {
		t.Fatalf("IntsToValue round-trip: %v", back)
	}
	if len(IntsToValue(nil)) != 0 {
		t.Fatal("IntsToValue(nil) should be an empty (non-nil-length) slice")
	}
}

func TestDeepEqualScalars(t *testing.T) {
	cases := []struct {
		a, b Value
		want bool
	}{
		{int64(1), int64(1), true},
		{int64(1), int64(2), false},
		{int64(1), "1", false}, // cross-type
		{"a", "a", true},
		{"a", "b", false},
		{true, true, true},
		{true, false, false},
		{nil, nil, true},
		{nil, int64(0), false},
		{int64(0), nil, false},
	}
	for _, c := range cases {
		if got := DeepEqual(c.a, c.b); got != c.want {
			t.Fatalf("DeepEqual(%v, %v): got %v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestDeepEqualLists(t *testing.T) {
	a := []Value{int64(1), []Value{int64(2), int64(3)}}
	b := []Value{int64(1), []Value{int64(2), int64(3)}}
	if !DeepEqual(a, b) {
		t.Fatal("structurally identical nested lists should be DeepEqual")
	}
	c := []Value{int64(1), []Value{int64(2), int64(4)}}
	if DeepEqual(a, c) {
		t.Fatal("lists differing in a nested element must not be DeepEqual")
	}
	if DeepEqual([]Value{int64(1)}, []Value{int64(1), int64(2)}) {
		t.Fatal("lists of different length must not be DeepEqual")
	}
	if !DeepEqual([]Value{}, []Value{}) {
		t.Fatal("two empty lists should be DeepEqual")
	}
}

func TestDeepEqualRecordsMapsSetsGrids(t *testing.T) {
	r1 := NewRecordValue()
	r1.Set("x", int64(1))
	r1.Set("y", int64(2))
	r2 := NewRecordValue()
	r2.Set("y", int64(2)) // different insertion order, same fields/values
	r2.Set("x", int64(1))
	if !DeepEqual(r1, r2) {
		t.Fatal("records with the same fields (any insertion order) should be DeepEqual")
	}
	r3 := NewRecordValue()
	r3.Set("x", int64(9))
	if DeepEqual(r1, r3) {
		t.Fatal("records differing in a field value must not be DeepEqual")
	}

	m1 := NewMapValue()
	m1.Put(int64(1), "a")
	m2 := NewMapValue()
	m2.Put(int64(1), "a")
	if !DeepEqual(m1, m2) {
		t.Fatal("maps with the same entries should be DeepEqual")
	}
	m3 := NewMapValue()
	m3.Put(int64(1), "b")
	if DeepEqual(m1, m3) {
		t.Fatal("maps differing in a value must not be DeepEqual")
	}

	s1 := SetFromList([]Value{int64(1), int64(2)})
	s2 := SetFromList([]Value{int64(2), int64(1)}) // different order
	if !DeepEqual(s1, s2) {
		t.Fatal("sets with the same elements (any order) should be DeepEqual")
	}

	g1 := NewGridValue(1, 2)
	g1.SetAt(0, 0, int64(1))
	g1.SetAt(0, 1, int64(2))
	g2 := NewGridValue(1, 2)
	g2.SetAt(0, 0, int64(1))
	g2.SetAt(0, 1, int64(2))
	if !DeepEqual(g1, g2) {
		t.Fatal("grids with the same dims/cells should be DeepEqual")
	}
	g3 := NewGridValue(2, 1)
	if DeepEqual(g1, g3) {
		t.Fatal("grids with different dims must not be DeepEqual")
	}
}

func TestDeepEqualCrossKind(t *testing.T) {
	if DeepEqual([]Value{int64(1)}, int64(1)) {
		t.Fatal("a list and a scalar must not be DeepEqual")
	}
	if DeepEqual(NewRecordValue(), NewMapValue()) {
		t.Fatal("a record and a map must not be DeepEqual")
	}
}

func TestDescribeValue(t *testing.T) {
	cases := []struct {
		v    Value
		want string
	}{
		{int64(1), "Int"},
		{"a", "Text"},
		{true, "Bool"},
		{[]Value{}, "List"},
		{NewRecordValue(), "Record"},
		{NewMapValue(), "Map"},
		{NewSetValue(), "Set"},
		{NewGridValue(1, 1), "Grid"},
		{nil, "<none>"},
	}
	for _, c := range cases {
		if got := DescribeValue(c.v); got != c.want {
			t.Fatalf("DescribeValue(%v): got %q want %q", c.v, got, c.want)
		}
	}
}

func TestFormatShortTruncatesLongText(t *testing.T) {
	long := ""
	for i := 0; i < 60; i++ {
		long += "x"
	}
	got := FormatShort(long)
	if len(got) >= len(long) {
		t.Fatalf("expected truncation, got %q", got)
	}
}

func TestFormatShortScalars(t *testing.T) {
	if FormatShort(int64(42)) != "42" {
		t.Fatalf("got %q", FormatShort(int64(42)))
	}
	if FormatShort(true) != "true" {
		t.Fatalf("got %q", FormatShort(true))
	}
	if FormatShort("hi") != `"hi"` {
		t.Fatalf("got %q", FormatShort("hi"))
	}
}

func TestFormatShortGrid(t *testing.T) {
	g := NewGridValue(2, 3)
	if FormatShort(g) != "Grid 2x3" {
		t.Fatalf("got %q", FormatShort(g))
	}
}

func TestContextChannels(t *testing.T) {
	var ctx Context // zero value: Channels is nil
	if _, ok := ctx.Channel("missing"); ok {
		t.Fatal("expected no channel for an unset context")
	}
	ctx.SetChannel("a", int64(1))
	v, ok := ctx.Channel("a")
	if !ok || v.(int64) != 1 {
		t.Fatalf("SetChannel/Channel round-trip: got %v %v", v, ok)
	}
	// SetChannel must lazily create the map without the caller pre-populating it.
	if ctx.Channels == nil {
		t.Fatal("SetChannel should lazily initialize Channels")
	}
}

func TestRuntimeErrorMessage(t *testing.T) {
	err := &RuntimeError{Prim: "Sort", Msg: "boom"}
	if got := err.Error(); got == "" {
		t.Fatal("RuntimeError.Error() should not be empty")
	}
}

// TestFormatShort covers every rendering branch of the error-message
// formatter: scalars, truncated long text, each collection kind, the
// 8-entry elision, and the default case.
func TestFormatShort(t *testing.T) {
	long := ""
	for i := 0; i < 50; i++ {
		long += "x"
	}
	rec := NewRecordValue()
	rec.Set("a", int64(1))
	rec.Set("b", "hi")
	m := NewMapValue()
	m.Put("k", int64(7))
	s := NewSetValue()
	s.Add(int64(3))
	g := NewGridValue(2, 3)
	big := make([]Value, 12)
	for i := range big {
		big[i] = int64(i)
	}

	cases := []struct {
		name string
		in   Value
		want string
	}{
		{"int", int64(42), "42"},
		{"bool", true, "true"},
		{"short string", "hi", `"hi"`},
		{"long string truncated", long, `"` + long[:40] + `"…`},
		{"list", []Value{int64(1), int64(2)}, "[1, 2]"},
		{"long list elided", big, "[0, 1, 2, 3, 4, 5, 6, 7, …(4 more)]"},
		{"record", rec, `{a: 1, b: "hi"}`},
		{"map", m, `{"k": 7}`},
		{"set", s, "{3}"},
		{"grid", g, "Grid 2x3"},
		{"default", 3.5, "3.5"},
	}
	for _, c := range cases {
		if got := FormatShort(c.in); got != c.want {
			t.Errorf("%s: FormatShort = %q, want %q", c.name, got, c.want)
		}
	}
}
