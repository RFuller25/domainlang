package codegen_test

import (
	"fmt"
	"strings"
	"testing"

	"domain/codegen"
	"domain/ir"
	"domain/prims"
)

// A `Consider` whose value the run never changes, read inside a loop the
// generator emits as its own function. Both halves matter: the binding is
// where a pin is written, and the block body is where a pin has to arrive for
// it to be worth anything.
const constBlockProgram = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider n Of (x) -> length(x)
    Using: (v) -> v % n
Maximum Technique: Sum
Reveal: stdout
`

// The same shape with the binding written to. A binding a stage assigns to is
// not a constant of the run whatever it held when the scope opened.
const constWrittenProgram = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider n As 0
    Using: (x) -> x + (n := n + 1)
Maximum Technique: Sum
Reveal: stdout
`

// considerKey finds the key a caller would have to write to pin a binding —
// the same one the probe reports — without hard-coding a source position that
// moves whenever the test program gains a line.
func considerKey(t *testing.T, src, name string) string {
	t.Helper()
	pipe := compilePipeline(t, src, true)
	var key string
	prims.WalkNodes(pipe, func(n *ir.Node) {
		if n.Prim != "Consider" || key != "" {
			return
		}
		key = codegen.ConsiderKey(n, name)
	})
	if key == "" {
		t.Fatalf("no Consider node in the program:\n%s", src)
	}
	return key
}

// A pinned binding is substituted at the reads, not merely initialized at the
// definition. The distinction is the entire value of the entry: `% n` against
// a local is a hardware division, and `% 16` is an AND — and the Go compiler
// can only make that trade when it can see the constant at the operator.
func TestPinnedConstantReachesTheReads(t *testing.T) {
	key := considerKey(t, constBlockProgram, "n")
	plain, tuned := emitTuned(t, constBlockProgram,
		codegen.Tuning{Constants: map[string]int64{key: 16}})

	if !strings.Contains(plain, "int64(len(") {
		t.Fatalf("the untuned build no longer computes the binding:\n%s", plain)
	}
	if strings.Contains(tuned, "int64(len(v2))") {
		t.Errorf("the pinned build still computes the binding it was given:\n%s", tuned)
	}
	if !strings.Contains(tuned, "int64(16)") {
		t.Errorf("the measured constant is not in the emitted program:\n%s", tuned)
	}
	// The read is what has to be a constant. Whatever the generator called the
	// local it no longer needs, no read of one may survive in main.
	body := tuned[strings.Index(tuned, "func main() {"):]
	if !strings.Contains(body, ", int64(16))") {
		t.Errorf("the read of the pinned binding is not the constant:\n%s", body)
	}
	if strings.Contains(body, "dmBind") {
		t.Errorf("the pinned binding is still a local:\n%s", body)
	}
}

// A binding that reaches a block body's function used to arrive as a
// parameter, which is a value the Go compiler must read. A pinned one is
// substituted into the body and the parameter is not declared at all — and the
// call site has to agree, or the program does not compile.
func TestPinnedConstantCrossesIntoABlockBody(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Simple Domain: Repeat 3
    Consider n Of (x) -> length(x)
    Cursed Technique: Map Each
        Using: (v) -> (v + 1) % n
Maximum Technique: Sum
Reveal: stdout
`
	key := considerKey(t, src, "n")
	plain, tuned := emitTuned(t, src, codegen.Tuning{Constants: map[string]int64{key: 8}})
	if !strings.Contains(plain, "func dmBlock") {
		t.Skipf("this program no longer emits a block function:\n%s", plain)
	}
	fn := tuned[strings.Index(tuned, "func dmBlock"):]
	fn = fn[:strings.Index(fn, "\n}\n")]
	if !strings.Contains(fn, "int64(8)") {
		t.Errorf("the pinned constant did not reach the block body:\n%s", fn)
	}
	if strings.Count(fn, "bb") > 0 && strings.Contains(fn, "% bb") {
		t.Errorf("the block still reads the binding through a parameter:\n%s", fn)
	}
}

// The refusal that keeps the entry honest. A stage that writes the binding
// makes it a variable, and a build that pinned it would ignore every write —
// so the pin is dropped and the ordinary local is emitted instead.
func TestAWrittenBindingIsNeverPinned(t *testing.T) {
	key := considerKey(t, constWrittenProgram, "n")
	_, tuned := emitTuned(t, constWrittenProgram,
		codegen.Tuning{Constants: map[string]int64{key: 99}})
	if strings.Contains(tuned, "int64(99)") {
		t.Errorf("a binding the program writes to was pinned anyway:\n%s", tuned)
	}
}

// A probe build reports what its bindings held; an ordinary build carries no
// trace of the machinery. The second half is the load-bearing one — every
// measured build in a search is an ordinary build.
func TestProbeBuildReportsBindings(t *testing.T) {
	plain, probed := emitTuned(t, constBlockProgram, codegen.Tuning{ProbeConstants: true})
	for _, want := range []string{"dmProbe(", "func dmProbeReport()", codegen.EnvConstProbe} {
		if !strings.Contains(probed, want) {
			t.Errorf("a probe build does not carry %q:\n%s", want, probed)
		}
	}
	for _, unwanted := range []string{"dmProbe", "dmProbeReport", codegen.EnvConstProbe} {
		if strings.Contains(plain, unwanted) {
			t.Errorf("an ordinary build carries the probe's %q:\n%s", unwanted, plain)
		}
	}
}

// The accumulator the generator cannot estimate: an Unfold's own predicate
// decides how many elements it produces, so the slice starts at nothing and
// grows by doubling. A measured length reserves it once.
const unfoldProgram = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (x) -> first(x)
Cursed Technique: Unfold
    While: (v) -> v < 100000
    Using: (v) -> v + 1
Maximum Technique: Sum
Reveal: stdout
`

func listSiteKey(t *testing.T, src, prim string) string {
	t.Helper()
	pipe := compilePipeline(t, src, true)
	var key string
	prims.WalkNodes(pipe, func(n *ir.Node) {
		if key != "" || n.Prim != prim {
			return
		}
		key = codegen.ListSiteKey(n)
	})
	if key == "" {
		t.Fatalf("no %s node in the program:\n%s", prim, src)
	}
	return key
}

func TestMeasuredCapacityReservesTheAccumulator(t *testing.T) {
	key := listSiteKey(t, unfoldProgram, "Unfold")
	plain, tuned := emitTuned(t, unfoldProgram,
		codegen.Tuning{ListCapacities: map[string]int{key: 100000}})
	if !strings.Contains(plain, "[]int64{}") {
		t.Fatalf("the untuned build no longer starts the accumulator empty:\n%s", plain)
	}
	if !strings.Contains(tuned, "make([]int64, 0, 100000)") {
		t.Errorf("the measured capacity is not in the emitted program:\n%s", tuned)
	}
	// A capacity for a site this program does not have must change nothing,
	// which is what makes a stale recipe slow rather than broken.
	_, stale := emitTuned(t, unfoldProgram,
		codegen.Tuning{ListCapacities: map[string]int{"list:Unfold@999:1": 100000}})
	if stale != plain {
		t.Error("a capacity keyed to a site that does not exist changed the program")
	}
}

// The probe reports how long the accumulator grew, at the point where the
// length is final.
func TestProbeReportsAccumulatorLength(t *testing.T) {
	key := listSiteKey(t, unfoldProgram, "Unfold")
	_, probed := emitTuned(t, unfoldProgram, codegen.Tuning{ProbeConstants: true})
	if !strings.Contains(probed, "dmProbe(\""+key+"\", int64(len(") {
		t.Errorf("the probe does not report the accumulator's length:\n%s", probed)
	}
}

// A reserved capacity is a hint and nothing else: the program has to answer
// the same whether it is right, far too small, or absurdly too large.
func TestMeasuredCapacityAgreesWithTheInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	key := listSiteKey(t, unfoldProgram, "Unfold")
	pipe, want := oracleFront(t, unfoldProgram, true, []byte("7\n"))
	for _, capacity := range []int{100000, 1, 10000000} {
		got := buildAndRun(t, pipe, []byte("7\n"), codegen.Options{
			Tuning: codegen.Tuning{ListCapacities: map[string]int{key: capacity}},
		})
		if got != want {
			t.Errorf("capacity %d changed the answer: got %q, want %q", capacity, got, want)
		}
	}
}

// The scheduler setting lands before anything allocates, like the collector
// settings beside it, and is absent from a build that did not ask for it.
func TestMaxProcsTuning(t *testing.T) {
	plain, tuned := emitTuned(t, constBlockProgram, codegen.Tuning{MaxProcs: 1})
	if !strings.Contains(tuned, "runtime.GOMAXPROCS(1)") {
		t.Errorf("the scheduler setting was not emitted:\n%s", tuned)
	}
	body := tuned[strings.Index(tuned, "func main() {"):]
	if procs, read := strings.Index(body, "GOMAXPROCS"), strings.Index(body, "dmReadSource"); read >= 0 && procs > read {
		t.Error("the scheduler was set after the input had already been read")
	}
	if strings.Contains(plain, "GOMAXPROCS") {
		t.Errorf("an untuned build sets GOMAXPROCS:\n%s", plain)
	}
}

// The oracle for the constants entry: a pinned build must answer exactly what
// the interpreter answers on the input the constant was measured from. A
// tuning that makes a program faster and wrong is the failure the whole design
// exists to make unreachable.
func TestPinnedConstantsAgreeWithTheInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	var input strings.Builder
	for i := range 64 {
		fmt.Fprintf(&input, "%d\n", i*37%251)
	}
	cases := []struct {
		name string
		src  string
		// value is what the binding actually holds on this input.
		value int64
	}{
		{"read in a lambda", constBlockProgram, 64},
		{"read in a block body", `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Simple Domain: Repeat 3
    Consider n Of (x) -> length(x)
    Cursed Technique: Map Each
        Using: (v) -> (v + 1) % n
Maximum Technique: Sum
Reveal: stdout
`, 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := considerKey(t, tc.src, "n")
			pipe, want := oracleFront(t, tc.src, true, []byte(input.String()))
			got := buildAndRun(t, pipe, []byte(input.String()), codegen.Options{
				Tuning: codegen.Tuning{Constants: map[string]int64{key: tc.value}},
			})
			if got != want {
				t.Errorf("pinning %s = %d changed the answer: got %q, want %q",
					key, tc.value, got, want)
			}
		})
	}
}
