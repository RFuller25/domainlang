package codegen_test

import (
	"fmt"
	"strings"
	"testing"

	"domain/codegen"
)

// Interpreter/binary byte parity for the linear-accumulator pass.
//
// Every program here is compiled and run in **both** optimizer modes, and
// `--no-optimize` is the copying semantics — so each case is simultaneously a
// four-way agreement: interpreter and binary, copying and in-place. That is
// the whole verification story for a pass whose entire job is to make a copy
// disappear without anyone noticing.
func TestCompiledLinearAccumulatorsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			// The frequency map: reads of the accumulator are arguments, so
			// they run before the write.
			name: "frequency map fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Fold
    Seed: (xs) -> emptymap("", 0)
    Using: (acc, w) -> insert(acc, w, getor(acc, w, 0) + 1)
Channeled Energy: Convert To Entries
Cursed Technique: Map Each
    Using: (e) -> item(e, 0) + "=" + totext(item(e, 1))
Maximum Technique: Join
    Using: ","
Reveal: stdout
`,
			input: "a\nb\na\nc\na\nb",
		},
		{
			// The conditional record, and a chained insert into a Set.
			name: "conditional and chained set inserts",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> emptyset(0)
    Using: (acc, x) -> if x % 2 = 0 then insert(insert(acc, x), 0 - x) else acc
Cursed Technique: Apply
    Using: (s) -> totext(size(s)) + "/" + totext(sum(tolist(s)))
Reveal: stdout
`,
			input: "1\n2\n3\n4\n2\n5\n6",
		},
		{
			// A sparse plane grown one cell at a time, then densified.
			name: "sparse plotted in a fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> sparse("-")
    Using: (acc, x) -> put(acc, x % 3, x % 4, "#")
Channeled Energy: Convert To Grid
Reveal: stdout
`,
			input: "0\n5\n7\n11\n2",
		},
		{
			// FoldOver: the accumulator is the current pipeline value, and a
			// sibling Part reads the same grid. If the compiled fold skipped
			// its one-time clone, the second Part would show the writes.
			name: "grid mutated by a FoldOver a sibling Part also reads",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid

Channel "steps":
    Cursed Technique: Apply
        Using: (g) -> range(0, 3)

Part "written":
    Maximum Technique: Fold
        From: steps
        Using: (g, i) -> setat(g, i, i, "Z")
    Reveal: stdout

Part "original":
    Reveal: stdout
`,
			input: "abcd\nefgh\nijkl",
		},
		{
			// The shape the pass must refuse: the pre-update value is still
			// read, so the copy has to survive into the binary too.
			name: "a fold that reads the accumulator afterwards",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> emptymap(0, 0)
    Using: (acc, x) -> tomap(list(tuple(size(insert(acc, x, 1)), 0), tuple(size(acc), 1)))
Cursed Technique: Apply
    Using: (m) -> totext(size(m)) + "/" + totext(sum(keys(m)))
Reveal: stdout
`,
			input: "1\n2\n3\n4",
		},
		{
			// Reduce is seedless, so its accumulator is an element of the
			// input list — which the pipeline still holds.
			name: "reduce over sets the pipeline still holds",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> toset(list(x % 4))

Part "reduced":
    Maximum Technique: Reduce
        Using: (a, b) -> insert(a, first(tolist(b)))
    Cursed Technique: Apply
        Using: (s) -> size(s)
    Reveal: stdout

Part "first":
    Cursed Technique: Take Item 0
    Cursed Technique: Apply
        Using: (s) -> size(s)
    Reveal: stdout
`,
			input: "1\n2\n3\n5\n6",
		},
		{
			// A Map and a Set carried in a loop's *state tuple*, which is where
			// a loop has to put them: it threads one value, so anything beyond
			// a single collection goes in a tuple. Both are written every lap
			// and neither is read after its write.
			//
			// Under the copying semantics this clones the map on every lap, so
			// the four-way agreement here is the whole claim: the in-place
			// build must answer identically to the one that copies.
			name: "map and set in loop state",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(xs, emptymap(0, 0), emptyset(0), 0)
Simple Domain: While
    Using: (s) -> item(s, 3) < 40
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider n as item(s, 3) in
                      consider m as insert(item(s, 1), n % 7, n) in
                      consider st as insert(item(s, 2), n % 5) in
                      tuple(xs, m, st, n + 1)
Cursed Technique: Apply
    Using: (s) -> textjoin(list(
        totext(size(item(s, 1))),
        totext(getor(item(s, 1), 3, 0 - 1)),
        totext(size(item(s, 2))),
        totext(sum(item(s, 0))),
        totext(item(s, 3))
    ), "|")
Reveal: stdout
`,
			input: "4\n5\n6",
		},
		{
			// The same shape with the state read after the write, which the
			// pass refuses — so this pair pins that the refused program still
			// agrees with itself in both modes.
			name: "map in loop state read after the write",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(xs, emptymap(0, 0), 0)
Simple Domain: While
    Using: (s) -> item(s, 2) < 40
    Cursed Technique: Apply
        Using: (s) -> consider m as insert(item(s, 1), item(s, 2) % 7, item(s, 2)) in
                      tuple(item(s, 0), m, item(s, 2) + size(item(s, 1)))
Cursed Technique: Apply
    Using: (s) -> textjoin(list(totext(size(item(s, 1))), totext(item(s, 2))), "|")
Reveal: stdout
`,
			input: "4\n5\n6",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}

// The parity test above proves the in-place build answers correctly; it cannot
// tell whether the rewrite fired at all, because a build that quietly kept
// cloning would pass it. This pins the emitted code instead: the update becomes
// the mutating helper, and the state gets storage of its own on entry.
//
// The clone-on-entry half is the part worth a test rather than a comment. The
// analysis lets a mark be rooted at a Map inside the state tuple, and it is
// ownValueExpr that has to copy that map — a field taken by reference would
// have the loop writing through to whatever the caller still holds.
func TestLoopStateCollectionsWriteInPlaceAndAreOwned(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(xs, emptymap(0, 0), emptyset(0), 0)
Simple Domain: While
    Using: (s) -> item(s, 3) < 40
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider n as item(s, 3) in
                      consider m as insert(item(s, 1), n % 7, n) in
                      consider st as insert(item(s, 2), n % 5) in
                      tuple(xs, m, st, n + 1)
Cursed Technique: Apply
    Using: (s) -> size(item(s, 1)) + size(item(s, 2))
Reveal: stdout
`
	pipe := compilePipeline(t, src, true)
	got, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}

	// One of the two updates is marked: the set's write reads the state, and it
	// follows the map's write, so the read-after-write rule takes the later one
	// and refuses the earlier. Whichever it is, a mutating helper must appear.
	if !strings.Contains(got, "dmSetAddIn(") && !strings.Contains(got, "dmMapPutIn(") {
		t.Errorf("no in-place update in a loop state carrying a Map and a Set:\n%s", got)
	}
	// The clone on entry, which is what makes the write above safe — and only
	// for the field actually written. One of the two updates is marked, so
	// exactly one of the two collections is copied; owning both would be the
	// over-broad clone that made day 6 of the AoC suite 1.8x slower, by copying
	// a twelve-thousand-entry map on every lap of the loop outside the one that
	// writes a sixteen-element list.
	clonesMap := strings.Contains(got, "dmMapClone((")
	clonesSet := strings.Contains(got, "dmSetClone((")
	if !clonesMap && !clonesSet {
		t.Errorf("the written field is not owned on entry:\n%s", got)
	}
	if clonesMap && clonesSet {
		t.Errorf("both collections are copied, but only one is written:\n%s", got)
	}

	// And the refused shape keeps the copying helper, so the assertion above is
	// not just matching a helper that is always emitted.
	const readAfter = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(xs, emptymap(0, 0), 0)
Simple Domain: While
    Using: (s) -> item(s, 2) < 40
    Cursed Technique: Apply
        Using: (s) -> consider m as insert(item(s, 1), item(s, 2) % 7, item(s, 2)) in
                      tuple(item(s, 0), m, item(s, 2) + size(item(s, 1)))
Cursed Technique: Apply
    Using: (s) -> size(item(s, 1))
Reveal: stdout
`
	pipe2 := compilePipeline(t, readAfter, true)
	got2, err := codegen.EmitProgram(pipe2, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	if strings.Contains(got2, "dmMapPutIn(") {
		t.Errorf("the state is read after the write, so the copy must stay:\n%s", got2)
	}
	if !strings.Contains(got2, "dmMapWith(") {
		t.Errorf("want the copying helper for a refused update:\n%s", got2)
	}
}

// The nested-loop shape, and the reason the copy on entry is narrowed to the
// fields a marked update actually writes.
//
// Day 6 of the AoC suite is an outer search whose state carries a growing map,
// and an inner redistribution loop that writes only the short list beside it.
// Owning the whole state on entry to the *inner* loop copies that map once per
// lap of the outer one — which is the quadratic this pass exists to remove,
// reintroduced one level up, and measured 1.8x slower than not running the pass
// at all. The inner loop must copy the list and leave the map alone.
func TestAnInnerLoopOwnsOnlyWhatItWrites(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(xs, emptymap(0, 0), 0, 0)
Simple Domain: While
    Using: (s) -> item(s, 2) < 20
    Cursed Technique: Apply
        Using: (s) -> consider seen as item(s, 1) in
                      consider n as item(s, 2) in
                      consider xs as item(s, 0) in
                      tuple(xs, insert(seen, n, n), n + 1, 3)
    Simple Domain: While
        Using: (s) -> item(s, 3) > 0
        Cursed Technique: Apply
            Using: (s) -> consider seen as item(s, 1) in
                          consider n as item(s, 2) in
                          consider left as item(s, 3) in
                          consider banks as item(s, 0) in
                          tuple(set(banks, 0, left), seen, n, left - 1)
Cursed Technique: Apply
    Using: (s) -> size(item(s, 1)) + item(s, 2)
Reveal: stdout
`
	pipe := compilePipeline(t, src, true)
	got, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	// The inner loop writes the list, so it copies the list.
	if !strings.Contains(got, "append([]int64(nil)") {
		t.Errorf("the list the inner loop writes is not owned on entry:\n%s", got)
	}
	// Exactly one map copy: the outer loop's, for the insert it marks. A second
	// would be the inner loop copying the map it never touches.
	if n := strings.Count(got, "dmMapClone(("); n > 1 {
		t.Errorf("the map is copied %d times; the inner loop does not write it:\n%s", n, got)
	}
}

// A long `consider` chain has to compile.
//
// Each binding used to become its own nested closure, so a chain of forty — the
// size of an instruction decoder, and a shape the interpreter runs without
// complaint — took the Go compiler about a hundred seconds and then the OOM
// killer. Flattening the chain into one closure with sequential declarations
// makes the same program compile in a fraction of a second.
//
// Two things this has to get right that nesting got for free: a binding is in
// scope for the bindings after it but not for its own value, and a binding that
// shadows an earlier name needs a distinct Go local, since one block cannot
// declare the same name twice.
func TestALongConsiderChainCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a binary; skipped in -short mode")
	}
	requireGo(t)

	var b strings.Builder
	b.WriteString(`Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) ->
`)
	const depth = 60
	for i := range depth {
		// Each value reads the binding before it, so the chain is a real
		// dependency chain rather than a list of independent constants.
		prev := "first(xs)"
		if i > 0 {
			fmt.Fprintf(&b, "        consider v%d as v%d + %d in\n", i, i-1, i)
			continue
		}
		fmt.Fprintf(&b, "        consider v%d as %s + %d in\n", i, prev, i)
	}
	fmt.Fprintf(&b, "        v%d\n", depth-1)
	b.WriteString(`Cursed Technique: Apply
    Using: (n) -> totext(n)
Reveal: stdout
`)

	pipe := compilePipeline(t, b.String(), true)
	want := runInterpreter(t, pipe, []byte("7\n8\n9"))
	got := buildAndRun(t, pipe, []byte("7\n8\n9"), codegen.Options{})
	if got != want {
		t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
	}
}

// A rebound name inside a chain. Nested closures gave each binding its own
// scope; flattened into one block, two `var dmLetx` declarations would be a Go
// redeclaration error, so the second gets a distinct local.
func TestAConsiderChainMayRebindAName(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a binary; skipped in -short mode")
	}
	requireGo(t)
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) ->
        consider x as first(xs) in
        consider y as x * 2 in
        consider x as y + 100 in
        consider z as x + 1 in
        totext(x) + "|" + totext(y) + "|" + totext(z)
Reveal: stdout
`
	pipe := compilePipeline(t, src, true)
	want := runInterpreter(t, pipe, []byte("5\n6"))
	got := buildAndRun(t, pipe, []byte("5\n6"), codegen.Options{})
	if got != want {
		t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
	}
	if want != "110|10|111\n" {
		t.Errorf("the rebinding is not doing what the test assumes: %q", want)
	}
}
