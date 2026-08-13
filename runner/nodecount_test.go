package runner

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/ir"
	"domain/prims"
)

// traceProgram interprets a program in this process with a NodeCounter
// installed, and hands back the pipeline it ran so nodes can be identified.
func traceProgram(t *testing.T, src, input string) (*ir.Pipeline, *NodeCounter) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.domain")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	counter := NewNodeCounter()
	ctx := &ir.Context{
		Stdin:  strings.NewReader(input),
		Stdout: io.Discard,
		Trace:  counter,
	}
	pipe, _, err := Interpret(path, false, ctx)
	if err != nil {
		t.Fatalf("running the program: %v", err)
	}
	return pipe, counter
}

// find locates the first node with the given Prim, so a test can talk about a
// specific stage without depending on its index.
func find(t *testing.T, p *ir.Pipeline, prim string) *ir.Node {
	t.Helper()
	var found *ir.Node
	prims.WalkNodes(p, func(n *ir.Node) {
		if found == nil && n.Prim == prim {
			found = n
		}
	})
	if found == nil {
		t.Fatalf("no %q node in the pipeline", prim)
	}
	return found
}

func TestNodeCounterCountsPerNode(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`
	pipe, c := traceProgram(t, src, "1\n2\n3\n")

	// Every top-level stage of a straight pipeline runs exactly once.
	for _, n := range pipe.Nodes {
		if got := c.Calls(n); got != 1 {
			t.Errorf("%s ran %d times, want 1", n.Prim, got)
		}
	}
	if c.Total() != len(pipe.Nodes) {
		t.Errorf("total evaluations = %d, want %d", c.Total(), len(pipe.Nodes))
	}
	if never := c.NeverRan(pipe); len(never) != 0 {
		t.Errorf("a straight pipeline had unreached nodes: %v", never)
	}
}

// The counter has to distinguish two occurrences of the same primitive, since
// "which Map Each is hot" is the question it exists to answer.
func TestNodeCounterDistinguishesOccurrences(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Simple Domain: Repeat 5
    Cursed Technique: Map Each
        Using: (x) -> x + 1
Cursed Technique: Map Each
    Using: (x) -> x * 2
Maximum Technique: Sum
Reveal: stdout
`
	pipe, c := traceProgram(t, src, "1\n2\n3\n")

	var maps []*ir.Node
	prims.WalkNodes(pipe, func(n *ir.Node) {
		if n.Prim == "Map Each" {
			maps = append(maps, n)
		}
	})
	if len(maps) != 2 {
		t.Fatalf("expected two Map Each nodes, found %d", len(maps))
	}
	// One ran once per lap of Repeat 5, the other once. Which is which is not
	// asserted by position: WalkNodes is breadth-first, so a nested node
	// arrives after every top-level one, and a test that depended on that
	// order would break the day the walk changed for an unrelated reason.
	a, b := c.Calls(maps[0]), c.Calls(maps[1])
	if a == b {
		t.Fatalf("both Map Each nodes counted %d — they are not being told apart", a)
	}
	if min(a, b) != 1 || max(a, b) != 5 {
		t.Errorf("Map Each call counts are %d and %d, want 1 and 5", a, b)
	}
}

// Turn 2 of mahoraga rests entirely on this: a stage this input never reaches
// must be reported as never reached.
func TestNodeCounterFindsUnreachedNodes(t *testing.T) {
	// The loop's predicate is false on the first test, so its body never runs.
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Simple Domain: While
    Using: (n) -> n > 1000000
    Cursed Technique: Apply
        Using: (n) -> n - 1
Reveal: stdout
`
	pipe, c := traceProgram(t, src, "1\n2\n3\n")

	never := c.NeverRan(pipe)
	if len(never) == 0 {
		t.Fatal("the unreached loop body was not reported")
	}
	var names []string
	for _, n := range never {
		names = append(names, n.Prim)
	}
	if !contains(names, "Apply") {
		t.Errorf("the Apply inside the never-entered loop body is not in %v", names)
	}
	// And the stages that did run are not in the list.
	for _, n := range never {
		if n.Prim == "Sum" || n.Prim == "Read Source" {
			t.Errorf("%s ran but was reported as never reached", n.Prim)
		}
	}
}

// Sizes are what turn an appending loop into one sized allocation, so the
// counter has to observe them.
func TestNodeCounterRecordsOutputSizes(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`
	pipe, c := traceProgram(t, src, "1\n2\n3\n4\n5\n")

	split := find(t, pipe, "Split")
	st := c.Stat(split)
	if !st.Known {
		t.Fatal("the Split stage reported no output size")
	}
	if st.MaxOutSize != 5 {
		t.Errorf("Split produced %d elements, want 5", st.MaxOutSize)
	}
	// A scalar-producing stage has no size, which is not the same as zero.
	sum := find(t, pipe, "Sum")
	if c.Stat(sum).Known {
		t.Errorf("Sum produced a scalar but reported a size of %d", c.Stat(sum).MaxOutSize)
	}
}

func TestNodeCounterHotOrdersByCalls(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Simple Domain: Repeat 20
    Cursed Technique: Apply
        Using: (xs) -> xs
Maximum Technique: Sum
Reveal: stdout
`
	pipe, c := traceProgram(t, src, "1\n2\n")
	hot := c.Hot(pipe)
	if len(hot) == 0 {
		t.Fatal("nothing reported as hot")
	}
	if hot[0].Prim != "Apply" {
		t.Errorf("hottest node is %s, want the Apply that ran 20 times", hot[0].Prim)
	}
	for i := 1; i < len(hot); i++ {
		if c.Calls(hot[i]) > c.Calls(hot[i-1]) {
			t.Errorf("Hot is not ordered by call count at %d", i)
		}
	}
}

// A node that never ran has a zero stat rather than a missing one, so callers
// need no nil check.
func TestNodeCounterZeroStatForUnknownNode(t *testing.T) {
	c := NewNodeCounter()
	st := c.Stat(&ir.Node{Prim: "Sort"})
	if st.Calls != 0 || st.Known || st.Failed {
		t.Errorf("an unseen node reported %+v", st)
	}
	if c.Ran(nil) {
		t.Error("a nil node reported as having run")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
