package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// Composite Map/Set keys: points and other tuples/records are
// keyable, so Unique, Convert To Set + contains, Group By, Count By, and get
// work over them — the sparse-grid-as-map shape.

const intsHeader = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\"\n" +
	"Channeled Energy: Convert List to Integers\n"

func TestUniqueOverPoints(t *testing.T) {
	src := intsHeader +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> point(x, x)\n" +
		"Cursed Technique: Unique\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "3\n3\n4\n3")
	if v.(int64) != 2 {
		t.Fatalf("unique points: got %v want 2 ((3,3) and (4,4))", v)
	}
}

func TestSetOfPointsAndContains(t *testing.T) {
	src := intsHeader +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> point(x, 0)\n" +
		"Channeled Energy: Convert To Set\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (s) -> contains(s, point(3, 0))\n"
	v, _ := runPipeline(t, src, "1\n3\n5")
	if v.(bool) != true {
		t.Fatalf("contains((3,0)): got %v want true", v)
	}
	v, _ = runPipeline(t, src, "1\n2\n5")
	if v.(bool) != false {
		t.Fatalf("contains((3,0)) over {1,2,5}: got %v want false", v)
	}
}

func TestCountByPointKey(t *testing.T) {
	// Group 1..5 by (x / 2, 0): (0,0)->1, (1,0)->{2,3}, (2,0)->{4,5}.
	src := intsHeader +
		"Maximum Technique: Count By\n" +
		"    Using: (x) -> point(x / 2, 0)\n"
	v, _ := runPipeline(t, src, "1\n2\n3\n4\n5")
	m := v.(*ir.MapValue)
	if m.Len() != 3 {
		t.Fatalf("count by point: got %d keys want 3 (%s)", m.Len(), ir.FormatValue(m))
	}
	if n, ok := m.Get([]ir.Value{int64(1), int64(0)}); !ok || n.(int64) != 2 {
		t.Fatalf("count for (1,0): got %v, %v want 2", n, ok)
	}
	// Insertion (first-seen) order is preserved in the rendering.
	if got := ir.FormatValue(m); got != "{[0, 0]: 1, [1, 0]: 2, [2, 0]: 2}" {
		t.Fatalf("render: got %q", got)
	}
}

func TestGroupByPointKeyAndGet(t *testing.T) {
	src := intsHeader +
		"Maximum Technique: Group By\n" +
		"    Using: (x) -> point(x / 10, 0)\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (m) -> length(get(m, point(1, 0)))\n"
	v, _ := runPipeline(t, src, "5\n12\n17\n23")
	if v.(int64) != 2 {
		t.Fatalf("group by point + get: got %v want 2 (12 and 17)", v)
	}
}

// A List of keyables is a key everywhere a Set or Map is built (ir.ListKeyable);
// Graph nodes stay plain keyables.
func TestListElementsAreKeys(t *testing.T) {
	const head = "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \",\"\n"
	v, _ := runPipeline(t, head+"Cursed Technique: Unique\nMaximum Technique: Count\n", "a,b\nc\na,b")
	if v.(int64) != 2 {
		t.Fatalf("unique lists: got %v want 2", v)
	}
	// Float lists are never keyable.
	if ir.ListKeyable(ir.List(ir.Float())) || !ir.ListKeyable(ir.List(ir.Tuple(ir.Int(), ir.List(ir.Text())))) {
		t.Fatal("ListKeyable: Float lists must be refused, nested lists of Int/Text accepted")
	}
}

// A List is a Set element: Convert To Set, toset, contains, insert and the set
// algebra key it through ir.KeyOf, in first-seen order.
func TestSetOfLists(t *testing.T) {
	const head = "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \",\"\n"
	v, _ := runPipeline(t, head+"Channeled Energy: Convert To Set\n", "a,b\nc\na,b\na\nb,a")
	if got := ir.FormatValue(v); got != "{[a, b], [c], [a], [b, a]}" {
		t.Fatalf("set of lists: got %s", got)
	}
	// Lists that print alike but differ stay apart.
	v, _ = runPipeline(t, head+"Channeled Energy: Convert To Set\nCursed Technique: Apply\n    Using: (s) -> size(s)\n", "a b\na,b")
	if v.(int64) != 2 {
		t.Fatalf("[\"a b\"] vs [\"a\", \"b\"]: got size %v want 2", v)
	}
	v, _ = runPipeline(t, head+"Channeled Energy: Convert To Set\nCursed Technique: Apply\n    Using: (s) -> contains(s, list(\"c\"))\n", "a,b\nc")
	if v.(bool) != true {
		t.Fatalf("contains: got %v", v)
	}
}

func TestMapKeyedByLists(t *testing.T) {
	const head = "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n"
	v, _ := runPipeline(t, head+"Maximum Technique: Group By\n    Using: (w) -> sort(split(w, \"\"))\n", "listen\nsilent\nabc\ncab")
	if got := ir.FormatValue(v); got != "{[e, i, l, n, s, t]: [listen, silent], [a, b, c]: [abc, cab]}" {
		t.Fatalf("anagram groups: got %s", got)
	}
	v, _ = runPipeline(t, head+"Maximum Technique: Count By\n    Using: (w) -> split(w, \"\")\n", "ab\nba\nab")
	m := v.(*ir.MapValue)
	if m.Len() != 2 {
		t.Fatalf("count by list: got %d keys want 2 (%s)", m.Len(), ir.FormatValue(m))
	}
	// A float list is still no key.
	for _, tail := range []string{
		"Maximum Technique: Count By\n    Using: (x) -> list(tofloat(1))\n",
		"Maximum Technique: Group By\n    Using: (x) -> list(tofloat(1))\n",
	} {
		_, err := resolveSrc(t, head+tail)
		if err == nil || !strings.Contains(err.Error(), "keyable") {
			t.Fatalf("%q: a float list must stay refused, got: %v", tail, err)
		}
	}
}

// Explore's state may be a List (or hold one): the visited set keys it through
// ir.KeyOf, so states that print alike but differ stay distinct.
func TestExploreOverListStates(t *testing.T) {
	const head = "Cursed Energy: stdin\n" +
		"Cursed Technique: Extract Integers\n" +
		"Domain Expansion: Explore\n"
	const rot = "    Using: (s) -> list(concat(drop(s, 1), take(s, 1)), reverse(s), s)\n"
	v, _ := runPipeline(t, head+rot, "1 2 3")
	if got := ir.FormatValue(v); got != "[[1, 2, 3], [2, 3, 1], [3, 2, 1], [3, 1, 2], [1, 3, 2], [2, 1, 3]]" {
		t.Fatalf("collect: got %s", got)
	}
	v, _ = runPipeline(t, head+"    Mode: Count\n"+rot, "")
	if v.(int64) != 1 {
		t.Fatalf("count over the empty list state: got %v want 1", v)
	}
	v, _ = runPipeline(t, head+"    Mode: Distances\n"+rot, "1 2 3")
	if got := ir.FormatValue(v); got != "{[1, 2, 3]: 0, [2, 3, 1]: 1, [3, 2, 1]: 1, [3, 1, 2]: 2, [1, 3, 2]: 2, [2, 1, 3]: 2}" {
		t.Fatalf("distances: got %s", got)
	}
	v, _ = runPipeline(t, head+"    Mode: Steps\n    Until: (s) -> sum(take(s, 1)) = 3\n"+rot, "1 2 3")
	if v.(int64) != 1 {
		t.Fatalf("steps: got %v want 1", v)
	}
	// [1, 23] and [12, 3] print alike once joined, and are different states.
	v, _ = runPipeline(t, head+"    Mode: Count\n    Using: (s) -> if length(s) < 2 then list(concat(s, list(1, 23)), concat(s, list(12, 3)), concat(s, list(123))) else take(list(s), 0)\n", "")
	if v.(int64) != 7 {
		t.Fatalf("states that print alike: got %v want 7", v)
	}
	v, _ = runPipeline(t, head+"    Mode: Cheapest\n    Cost: (t) -> sum(t)\n    Until: (s) -> sum(take(s, 1)) = 3\n"+rot, "1 2 3")
	if v.(int64) != 6 {
		t.Fatalf("cheapest: got %v want 6", v)
	}
	v, _ = runPipeline(t, head+"    Mode: Tally\n    Value: (s) -> 1\n    Combine: (a, b) -> a + b\n    Using: (s) -> if length(s) > 0 then list(drop(s, 1), take(s, length(s) - 1)) else take(list(s), 0)\n", "1 2 3")
	if v.(int64) != 8 {
		t.Fatalf("tally: got %v want 8", v)
	}
	// A cycle names the list state.
	_, err := runErr(t, head+"    Mode: Tally\n    Value: (s) -> 1\n    Combine: (a, b) -> a + b\n    Using: (s) -> list(reverse(s))\n"+"Reveal: stdout\n", "1 2 3")
	if err == nil || !strings.Contains(err.Error(), "[1, 2, 3] is reachable from itself") {
		t.Fatalf("tally cycle: got %v", err)
	}
	// A float list is still no state.
	_, err = resolveSrc(t, "Cursed Energy: stdin\nCursed Technique: Extract Integers\nCursed Technique: Map Each\n    Using: (x) -> tofloat(x)\nDomain Expansion: Explore\n    Using: (s) -> list(s)\n")
	if err == nil || !strings.Contains(err.Error(), "keyable") {
		t.Fatalf("float list state must stay refused, got: %v", err)
	}
}

// A Graph's nodes may be Lists: every reader and primitive keys them through
// ir.KeyOf, in insertion order, and nodes that print alike stay apart.
func TestGraphOfLists(t *testing.T) {
	const head = "Cursed Energy: stdin\n" +
		"Cursed Technique: Extract Integers\n" +
		"Cursed Technique: Chunk 2\n" +
		"Cursed Technique: Map Each\n" +
		"    Using: (p) -> tuple(list(item(p, 0) / 100, item(p, 0) % 100), list(item(p, 1) / 100, item(p, 1) % 100), 1)\n" +
		"Channeled Energy: Convert To Graph\n"
	v, _ := runPipeline(t, head, "123 1203 1203 123 7 123")
	if got := ir.FormatValue(v); got != "{[1, 23]: [([12, 3], 1)], [12, 3]: [([1, 23], 1)], [0, 7]: [([1, 23], 1)]}" {
		t.Fatalf("graph: got %s", got)
	}
	v, _ = runPipeline(t, head+"Domain Expansion: Strongly Connected Components\n", "123 1203 1203 123 7 123")
	if got := ir.FormatValue(v); got != "[[[0, 7]], [[1, 23], [12, 3]]]" {
		t.Fatalf("scc: got %s", got)
	}
	v, _ = runPipeline(t, head+"Domain Expansion: Topological Sort\n", "1 2 2 3")
	if got := ir.FormatValue(v); got != "[[0, 1], [0, 2], [0, 3]]" {
		t.Fatalf("topological sort: got %s", got)
	}
	v, _ = runPipeline(t, head+"Cursed Technique: Apply\n    Using: (g) -> tuple(contains(g, list(1, 23)), contains(g, list(12, 3)), contains(g, list(123)))\n", "123 1203")
	if got := ir.FormatValue(v); got != "[true, true, false]" {
		t.Fatalf("contains: got %s", got)
	}
	// A float list is still no node.
	_, err := resolveSrc(t, "Cursed Energy: stdin\nCursed Technique: Apply\n    Using: (s) -> emptygraph(list(tofloat(1)))\n")
	if err == nil || !strings.Contains(err.Error(), "keyable") {
		t.Fatalf("float list graph node must stay refused, got: %v", err)
	}
}
