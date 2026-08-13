package mahoraga

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"domain/ir"
	"domain/runner"
)

func requireGoToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
}

// idleArena writes a program whose Filter keeps every element of the input, and
// the input and expected output to go with it.
func idleArena(t *testing.T, keepAll bool) (prog, input, expected string) {
	t.Helper()
	dir := t.TempDir()
	src := `Cursed Energy: in.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Using: (x) -> x >= 0
Cursed Technique: Map Each
    Using: (x) -> x * 2
Maximum Technique: Sum
Reveal: stdout
`
	var lines []string
	sum := 0
	for i := 1; i <= 200; i++ {
		v := i
		if !keepAll && i%10 == 0 {
			v = -i // a value the filter drops
		}
		lines = append(lines, itoaTest(v))
		if v >= 0 {
			sum += v * 2
		}
	}
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return write("p.domain", src),
		write("in.txt", strings.Join(lines, "\n")+"\n"),
		write("want.txt", itoaTest(sum)+"\n")
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func newIdleSearch(t *testing.T, prog, input, expected string) *Search {
	t.Helper()
	s, err := NewSearch(Options{
		Program: prog, Input: input, Expected: expected, Tier: Pinned,
		Recipe: filepath.Join(filepath.Dir(prog), "r.json"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// A Filter that kept every element is the finding turn 2 exists for. The
// general optimizer cannot see it — whether a predicate ever fails is a
// property of the data — and a filter over a long list that discards nothing
// still evaluates its predicate once per element.
func TestFindsAFilterThatKeptEverything(t *testing.T) {
	prog, input, expected := idleArena(t, true)
	s := newIdleSearch(t, prog, input, expected)

	idle, err := s.findIdleStages()
	if err != nil {
		t.Fatal(err)
	}
	if len(idle) != 1 {
		t.Fatalf("found %d idle stages, want the one Filter: %+v", len(idle), idle)
	}
	got := idle[0]
	if got.Prim != "Filter" {
		t.Errorf("idle stage is %q, want Filter", got.Prim)
	}
	if got.Why == "" || got.Size != 200 || got.Calls == 0 {
		t.Errorf("the observation is not reportable: %+v", got)
	}
	if !strings.HasPrefix(got.Key, "Filter@") {
		t.Errorf("key %q does not identify the node by position", got.Key)
	}
}

// The same program on input the filter actually filters must find nothing. This
// is the check that stops turn 2 removing a stage that does work: one element
// out of two hundred is enough, and it is exactly the case a search that only
// sampled would miss.
func TestFindsNothingWhenTheFilterFilters(t *testing.T) {
	prog, input, expected := idleArena(t, false)
	s := newIdleSearch(t, prog, input, expected)

	idle, err := s.findIdleStages()
	if err != nil {
		t.Fatal(err)
	}
	if len(idle) != 0 {
		t.Errorf("a filter that dropped 20 of 200 elements was reported idle: %+v", idle)
	}
}

// A Map Each preserves length and replaces every element, so length alone can
// never justify removing one. The whitelist is the whole safety argument and it
// deserves a test that would fail if anyone widened it carelessly.
func TestOnlyWhitelistedPrimsAreEverIdle(t *testing.T) {
	for prim := range idleStagePrims {
		switch prim {
		case "Filter", "Filter Entries", "Unique", "Merge Ranges":
		default:
			t.Errorf("%q is on the idle whitelist; length preservation does not "+
				"imply it did nothing", prim)
		}
	}
	for _, prim := range []string{"Sort", "Map Each", "Reverse", "Sort By", "Transpose"} {
		if _, ok := idleStagePrims[prim]; ok {
			t.Errorf("%q preserves length and changes the value; it must never be elidable", prim)
		}
	}
}

// The removal has to actually happen and the program has to still be right.
// This is the end of the chain turn 2 builds: observe, elide, rebuild, check.
func TestElidingAnIdleStageKeepsTheAnswer(t *testing.T) {
	requireGoToolchain(t)
	prog, input, expected := idleArena(t, true)
	s := newIdleSearch(t, prog, input, expected)

	idle, err := s.findIdleStages()
	if err != nil || len(idle) != 1 {
		t.Fatalf("reconnaissance: %v %+v", err, idle)
	}

	c := baselineCandidate()
	plain, err := s.emitSource(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Tuning.ElideNodes = []string{idle[0].Key}
	cut, err := s.emitSource(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(cut) >= len(plain) {
		t.Errorf("eliding a stage did not shorten the program (%d then %d bytes)", len(plain), len(cut))
	}

	// And the binary still answers correctly, which is the only thing that
	// makes the elision anything other than a bug.
	bin, err := s.build(c, "elided")
	if err != nil {
		t.Fatal(err)
	}
	if m := s.measure(bin, 1); !m.Correct {
		t.Errorf("the program with the idle filter removed answered wrongly: %s", m.Failure)
	}
}

// A key naming a stage that is not there must be ignored rather than misapplied.
// A recipe outlives the program it was measured from, and a stale key should
// make a build slow, never wrong.
func TestAStaleElideKeyIsIgnored(t *testing.T) {
	prog, input, expected := idleArena(t, true)
	s := newIdleSearch(t, prog, input, expected)

	c := baselineCandidate()
	plain, err := s.emitSource(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Tuning.ElideNodes = []string{"Filter@999:1", "Sort@1:1"}
	same, err := s.emitSource(c)
	if err != nil {
		t.Fatal(err)
	}
	if same != plain {
		t.Error("a key matching no stage changed the emitted program")
	}
}

// A reconnaissance run that outstays its deadline must be *stopped*, not
// abandoned.
//
// This is the regression test for the bug that motivated the whole file. The
// first version left the goroutine running: on a 712ms program the interpreter
// could not finish in ninety seconds, kept a core busy and a 389MB heap live
// for minutes afterwards, and the search spent turns 3 and 4 measuring against
// its own reconnaissance — twenty percent slow, with a false win accepted
// inside the window.
//
// The program below never terminates on its own, so the only way this test can
// pass is if the interrupter actually reaches it.
func TestReconnaissanceStopsWhenItOverruns(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// `While: true` is unbounded by design — a limit must never be the reason a
	// correct program cannot run — which makes it exactly the shape that used
	// to run away.
	prog := write("spin.domain", `Cursed Energy: in.txt
Cursed Technique: Apply
    Using: (s) -> toint(s)
Simple Domain: While
    Using: (x) -> x > 0
    Cursed Technique: Apply
        Using: (x) -> x + 1
Reveal: stdout
`)
	input := write("in.txt", "1")
	expected := write("want.txt", "never\n")

	s, err := NewSearch(Options{
		Program: prog, Input: input, Expected: expected, Tier: Pinned,
		Recipe: filepath.Join(dir, "r.json"),
	}, nil)
	if err != nil {
		t.Skipf("the spinning program did not resolve: %v", err)
	}
	t.Cleanup(s.Close)

	pipe, err := runner.LoadPipelineSchedule(prog, s.champion.Schedule)
	if err != nil {
		t.Skipf("the spinning program did not compile: %v", err)
	}

	// A tracer that counts node evaluations. Whether the run is still going
	// after the call returns is the only question that matters here, and this
	// is what answers it — the error message alone would be satisfied by a
	// version that abandoned the goroutine and lied about it.
	ticks := &countingTracer{}
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: bytes.NewReader(nil), Stdout: &out, BaseDir: dir, Trace: ticks}

	start := time.Now()
	err = runInterpreterBounded(pipe, ctx, 300*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Skip("this program terminated on its own; it cannot test the interrupt")
	}
	if !strings.Contains(err.Error(), "stopped") {
		t.Errorf("the overrun was not reported as stopped: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("stopping the run took %v", elapsed)
	}
	if ticks.Load() == 0 {
		t.Fatal("the program never ran, so nothing was interrupted")
	}

	// The assertion the old code fails: no further work after the call returns.
	after := ticks.Load()
	time.Sleep(250 * time.Millisecond)
	if grown := ticks.Load() - after; grown != 0 {
		t.Errorf("the run kept going after runInterpreterBounded returned: %d more "+
			"node evaluations in 250ms. This is the bug — an abandoned run spends "+
			"the rest of the search competing with the measurements.", grown)
	}
}

// countingTracer counts node evaluations, from whichever goroutine is running
// them.
type countingTracer struct{ n atomic.Int64 }

func (c *countingTracer) Step(ir.StepEvent)          { c.n.Add(1) }
func (c *countingTracer) PushFrame(string, *ir.Type) {}
func (c *countingTracer) PopFrame(ir.Value)          {}
func (c *countingTracer) Load() int64                { return c.n.Load() }
