package typecheck

import (
	"strings"
	"testing"

	"domain/ir"
)

// typeOf types a lambda body against the given parameter types.
func typeOf(t *testing.T, src string, params ...*ir.Type) (*ir.Type, error) {
	t.Helper()
	return LambdaType(parseLambda(t, src), params...)
}

// wantType requires a lambda to type to want.
func wantType(t *testing.T, src string, want *ir.Type, params ...*ir.Type) {
	t.Helper()
	got, err := typeOf(t, src, params...)
	if err != nil {
		t.Errorf("%s: %v", src, err)
		return
	}
	if !got.Equal(want) {
		t.Errorf("%s : %s, want %s", src, got, want)
	}
}

// wantTypeErr requires a lambda to fail with a message containing sub.
func wantTypeErr(t *testing.T, src, sub string, params ...*ir.Type) {
	t.Helper()
	_, err := typeOf(t, src, params...)
	if err == nil {
		t.Errorf("%s: expected an error containing %q, got none", src, sub)
		return
	}
	if !strings.Contains(err.Error(), sub) {
		t.Errorf("%s: error %q does not contain %q", src, err, sub)
	}
}

func TestCollectionBuiltinTypes(t *testing.T) {
	listInt, listText := ir.List(ir.Int()), ir.List(ir.Text())
	setInt := ir.Set(ir.Int())
	mapTI := ir.Map(ir.Text(), ir.Int())
	pairs := ir.List(ir.Tuple(ir.Text(), ir.Int()))

	wantType(t, "(xs) -> toset(xs)", setInt, listInt)
	wantType(t, "(xs) -> emptyset(first(xs))", setInt, listInt)
	wantType(t, "(xs) -> emptymap(first(xs), 0)", ir.Map(ir.Text(), ir.Int()), listText)
	wantType(t, "(ps) -> tomap(ps)", mapTI, pairs)
	wantType(t, "(m) -> entries(m)", pairs, mapTI)
	wantType(t, "(s) -> insert(s, 1)", setInt, setInt)
	wantType(t, `(m) -> insert(m, "k", 1)`, mapTI, mapTI)
	wantType(t, "(s) -> del(s, 1)", setInt, setInt)
	wantType(t, `(m) -> del(m, "k")`, mapTI, mapTI)
	wantType(t, "(a, b) -> union(a, b)", setInt, setInt, setInt)
	wantType(t, "(a, b) -> intersect(a, b)", setInt, setInt, setInt)
	wantType(t, "(g) -> setat(g, 0, 0, \"x\")", ir.Grid(ir.Text()), ir.Grid(ir.Text()))
	wantType(t, "(g) -> cellpoints(g)", ir.List(PointType()), ir.Sparse(ir.Int()))
}

// insert has two shapes told apart by the collection, so a Set given three
// arguments (or a Map given two) has to say which one it wanted.
func TestInsertArityDependsOnTheCollection(t *testing.T) {
	setInt := ir.Set(ir.Int())
	mapTI := ir.Map(ir.Text(), ir.Int())
	wantTypeErr(t, "(s) -> insert(s, 1, 2)", "insert into a Set takes 2 arguments", setInt)
	wantTypeErr(t, `(m) -> insert(m, "k")`, "insert into a Map takes 3 arguments", mapTI)
	wantTypeErr(t, "(xs) -> insert(xs, 1)", "insert needs a Set or Map", ir.List(ir.Int()))
}

func TestCollectionBuiltinTypeErrors(t *testing.T) {
	cases := []struct {
		src    string
		params []*ir.Type
		sub    string
	}{
		{"(xs) -> toset(xs)", []*ir.Type{ir.List(ir.List(ir.Int()))}, "keyable"},
		{"(xs) -> tomap(xs)", []*ir.Type{ir.List(ir.Int())}, "(key, value) pairs"},
		{"(s) -> insert(s, \"x\")", []*ir.Type{ir.Set(ir.Int())}, "insert value must be Int"},
		{"(a, b) -> union(a, b)", []*ir.Type{ir.Set(ir.Int()), ir.Set(ir.Text())}, "same type"},
		{"(a, b) -> union(a, b)", []*ir.Type{ir.Set(ir.Int()), ir.List(ir.Int())}, "needs Set arguments"},
		// A Sparse grid uses put; setat is the dense one, and the message says so.
		{"(g) -> setat(g, 0, 0, 1)", []*ir.Type{ir.Sparse(ir.Int())}, "a Sparse grid uses put"},
		{"(g) -> cellpoints(g)", []*ir.Type{ir.Grid(ir.Int())}, "needs a Sparse argument"},
	}
	for _, c := range cases {
		wantTypeErr(t, c.src, c.sub, c.params...)
	}
}

// The field names decide the result type, so they must be literals — the same
// rule item-over-a-Tuple's index already follows.
func TestRecordNeedsLiteralFieldNames(t *testing.T) {
	rec := ir.Record(ir.Field{Name: "a", Type: ir.Int()}, ir.Field{Name: "b", Type: ir.Text()})
	wantType(t, `(n) -> record("a", n, "b", "x")`, rec, ir.Int())
	wantType(t, `(r) -> with(r, "a", 5)`, rec, rec)
	wantType(t, `(r) -> r.b`, ir.Text(), rec)

	wantTypeErr(t, `(s) -> record(s, 1)`, "must be a literal", ir.Text())
	wantTypeErr(t, `(n) -> record("a")`, "record takes at least 2 argument", ir.Int())
	wantTypeErr(t, `(n) -> record("a", 1, "b")`, "even number of arguments", ir.Int())
	wantTypeErr(t, `(n) -> record("a", 1, "a", 2)`, "duplicate field", ir.Int())
	wantTypeErr(t, `(n) -> record("", 1)`, "cannot be empty", ir.Int())
	wantTypeErr(t, `(r) -> with(r, "nope", 1)`, `has no field "nope"`, rec)
	wantTypeErr(t, `(r) -> with(r, "a", "text")`, `field "a" must be Int`, rec)
	wantTypeErr(t, `(r, s) -> with(r, s, 1)`, "literal field name", rec, ir.Text())
}

// textjoin takes a List of any element type, not just List<Text>: every
// element renders exactly as Reveal would (Int, Float, Bool, Record, ...)
// before being joined.
func TestTextJoinAcceptsAnyElementType(t *testing.T) {
	rec := ir.Record(ir.Field{Name: "a", Type: ir.Int()}, ir.Field{Name: "b", Type: ir.Text()})
	wantType(t, `(xs) -> textjoin(xs, ",")`, ir.Text(), ir.List(ir.Text()))
	wantType(t, `(xs) -> textjoin(xs, ",")`, ir.Text(), ir.List(ir.Int()))
	wantType(t, `(xs) -> textjoin(xs, ",")`, ir.Text(), ir.List(ir.Float()))
	wantType(t, `(xs) -> textjoin(xs, ",")`, ir.Text(), ir.List(ir.Bool()))
	wantType(t, `(xs) -> textjoin(xs, ",")`, ir.Text(), ir.List(rec))
	wantType(t, `(xs) -> textjoin(xs, ",")`, ir.Text(), ir.List(ir.List(ir.Int())))

	wantTypeErr(t, `(n) -> textjoin(n, ",")`, "textjoin needs a List", ir.Int())
	wantTypeErr(t, `(xs) -> textjoin(xs, 5)`, "separator must be Text", ir.List(ir.Int()))
}

// pow is the one builtin that follows the operators' promotion rule rather than
// staying integral, so both directions are worth pinning.
func TestPowPromotesLikeTheOperators(t *testing.T) {
	wantType(t, "(n) -> pow(n, 2)", ir.Int(), ir.Int())
	wantType(t, "(f) -> pow(f, 2)", ir.Float(), ir.Float())
	wantType(t, "(n) -> pow(n, 0.5)", ir.Float(), ir.Int())
	wantTypeErr(t, `(s) -> pow(s, 2)`, "pow needs Int or Float", ir.Text())
}

func TestFloatBuiltinTypes(t *testing.T) {
	for _, name := range []string{"log", "log2", "log10", "exp", "sin", "cos", "tan"} {
		wantType(t, "(n) -> "+name+"(n)", ir.Float(), ir.Int())
		wantType(t, "(f) -> "+name+"(f)", ir.Float(), ir.Float())
		wantTypeErr(t, "(s) -> "+name+"(s)", "needs Int or Float", ir.Text())
	}
	wantType(t, "(n) -> atan2(n, n)", ir.Float(), ir.Int())
	wantType(t, "(n) -> hypot(n, n)", ir.Float(), ir.Int())
	wantType(t, "(f) -> trunc(f)", ir.Int(), ir.Float())
	wantType(t, "(n) -> trunc(n)", ir.Int(), ir.Int())
}

func TestTextAndNumberTheoryTypes(t *testing.T) {
	wantType(t, `(s) -> split(s, ",")`, ir.List(ir.Text()), ir.Text())
	wantType(t, `(s) -> words(s)`, ir.List(ir.Text()), ir.Text())
	wantType(t, `(s) -> ord(s)`, ir.Int(), ir.Text())
	wantType(t, `(n) -> chr(n)`, ir.Text(), ir.Int())
	wantType(t, `(s) -> contains(s, "a")`, ir.Bool(), ir.Text())
	wantType(t, `(s) -> isdigit(s)`, ir.Bool(), ir.Text())
	wantType(t, `(s) -> padleft(s, 3, "0")`, ir.Text(), ir.Text())
	wantType(t, `(n) -> range(0, n)`, ir.List(ir.Int()), ir.Int())
	wantType(t, `(s) -> fill(3, s)`, ir.List(ir.Text()), ir.Text())
	wantType(t, `(n) -> digits(n)`, ir.List(ir.Int()), ir.Int())
	wantType(t, `(xs) -> fromdigits(xs)`, ir.Int(), ir.List(ir.Int()))
	wantType(t, `(n) -> isprime(n)`, ir.Bool(), ir.Int())
	wantType(t, `(n) -> divisors(n)`, ir.List(ir.Int()), ir.Int())
	wantType(t, `(xs) -> crt(xs, xs)`, ir.Int(), ir.List(ir.Int()))
	wantType(t, `(n) -> tohex(n)`, ir.Text(), ir.Int())
	wantType(t, `(s) -> fromhex(s)`, ir.Int(), ir.Text())
	wantType(t, `(n) -> testbit(n, 0)`, ir.Bool(), ir.Int())
	wantType(t, `(n) -> popcount(n)`, ir.Int(), ir.Int())

	wantTypeErr(t, `(n) -> split(n, ",")`, "must be Text", ir.Int())
	wantTypeErr(t, `(xs) -> fromdigits(xs)`, "needs List<Int>", ir.List(ir.Text()))
	wantTypeErr(t, `(xs) -> crt(xs, xs)`, "needs List<Int>", ir.List(ir.Text()))
}
