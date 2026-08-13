package codegen_test

import (
	"fmt"
	"strings"
	"testing"

	"domain/codegen"
)

const tuneSumProgram = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`

const tuneGridProgram = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Maximum Technique: Count Cells Where
    Using: (cell) -> cell = "#"
Reveal: stdout
`

// emitTuned compiles a program twice — untuned and tuned — so a test can assert
// on the difference rather than on the whole file.
func emitTuned(t *testing.T, src string, tuning codegen.Tuning) (plain, tuned string) {
	t.Helper()
	pipe := compilePipeline(t, src, true)
	var err error
	if plain, err = codegen.EmitProgram(pipe, codegen.Options{}); err != nil {
		t.Fatal(err)
	}
	if tuned, err = codegen.EmitProgram(pipe, codegen.Options{Tuning: tuning}); err != nil {
		t.Fatal(err)
	}
	return plain, tuned
}

// An empty tuning has to produce byte-for-byte what `domain build` produces.
// Everything else rests on this: a compiler whose output depended on who was
// asking, even a little, would not be one anybody could trust.
func TestEmptyTuningChangesNothing(t *testing.T) {
	for _, src := range []string{tuneSumProgram, tuneGridProgram} {
		plain, tuned := emitTuned(t, src, codegen.Tuning{})
		if plain != tuned {
			t.Error("an empty Tuning changed the emitted program")
		}
	}
	if !(codegen.Tuning{}).Empty() {
		t.Error("the zero Tuning does not report itself empty")
	}
}

// The fused split-and-parse loop reserves len(input)/2+1 — an over-reservation
// by the width of the numbers. A measured element count replaces it.
func TestListCapacityReplacesTheGuess(t *testing.T) {
	plain, tuned := emitTuned(t, tuneSumProgram, codegen.Tuning{ListCapacity: 1000})
	if !strings.Contains(plain, "/2+1") {
		t.Fatalf("the untuned build no longer carries the guess this entry replaces:\n%s", plain)
	}
	if strings.Contains(tuned, "/2+1") {
		t.Errorf("the guess survived a measured capacity:\n%s", tuned)
	}
	if !strings.Contains(tuned, "make([]int64, 0, 1000)") {
		t.Errorf("the measured capacity is not in the emitted program:\n%s", tuned)
	}
}

// With every byte verified to be one rune, the grid builder drops UTF-8
// decoding — one RuneLen per cell over a grid of thousands of them.
func TestASCIITextDropsTheDecode(t *testing.T) {
	plain, tuned := emitTuned(t, tuneGridProgram, codegen.Tuning{ASCIIText: true})
	if !strings.Contains(plain, "utf8.RuneLen") {
		t.Fatalf("the untuned grid builder no longer decodes runes:\n%s", plain)
	}
	if strings.Contains(tuned, "utf8.RuneLen") {
		t.Errorf("the decode survived a verified-ASCII input:\n%s", tuned)
	}
}

// Switching the collector off makes least sense in general and most sense for
// one short run. The memory limit is what keeps it safe on a larger input, so
// it has to be set before the collector is switched off and both have to land
// before the program allocates anything.
func TestGCTuning(t *testing.T) {
	_, tuned := emitTuned(t, tuneSumProgram,
		codegen.Tuning{GCPercent: -1, MemoryLimitBytes: 1 << 30})
	limit := strings.Index(tuned, "debug.SetMemoryLimit(1073741824)")
	off := strings.Index(tuned, "debug.SetGCPercent(-1)")
	switch {
	case limit < 0:
		t.Errorf("the memory limit was not emitted:\n%s", tuned)
	case off < 0:
		t.Errorf("the collector setting was not emitted:\n%s", tuned)
	case limit > off:
		t.Error("the memory limit is set after the collector is switched off, " +
			"leaving a window in which nothing bounds the heap")
	}
	if !strings.Contains(tuned, `"runtime/debug"`) {
		t.Errorf("runtime/debug was not imported:\n%s", tuned)
	}
	body := tuned[strings.Index(tuned, "func main() {"):]
	if first, read := strings.Index(body, "debug."), strings.Index(body, "dmReadSource"); read >= 0 && first > read {
		t.Error("the collector was tuned after the input had already been read")
	}
}

// A tuning nobody asked for must not drag runtime/debug into an ordinary
// build: an unused import does not compile.
func TestUntunedBuildHasNoDebugImport(t *testing.T) {
	plain, _ := emitTuned(t, tuneSumProgram, codegen.Tuning{})
	if strings.Contains(plain, "runtime/debug") {
		t.Errorf("an untuned build imports runtime/debug:\n%s", plain)
	}
}

// The check that matters: a tuned binary must answer exactly what the
// interpreter answers. A catalogue entry that makes a program faster and wrong
// is the failure this whole design exists to make unreachable, and the only
// way to keep it unreachable is to differentially test every entry — alone and
// combined, since they are applied greedily on top of one another.
func TestTunedBinariesAgreeWithTheInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	var numbers strings.Builder
	for i := range 500 {
		fmt.Fprintf(&numbers, "%d\n", i*7919%10007)
	}
	grid := strings.Repeat(".#..#.##..\n", 40)

	cases := []struct {
		name    string
		program string
		input   string
		tunings []codegen.Tuning
	}{
		{"sum", tuneSumProgram, numbers.String(), []codegen.Tuning{
			{ListCapacity: 500},
			// A capacity that is wrong in both directions still has to be
			// correct: it is a hint, and append is the fallback. Getting this
			// wrong would make the entry a correctness bug rather than a
			// performance one.
			{ListCapacity: 1},
			{ListCapacity: 100000},
			{GCPercent: -1, MemoryLimitBytes: 1 << 28},
			{ListCapacity: 500, GCPercent: -1, MemoryLimitBytes: 1 << 28},
		}},
		{"grid", tuneGridProgram, grid, []codegen.Tuning{
			{ASCIIText: true},
			{ASCIIText: true, GCPercent: -1, MemoryLimitBytes: 1 << 28},
			{ASCIIText: true, ListCapacity: 40, GCPercent: -1, MemoryLimitBytes: 1 << 28},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pipe := compilePipeline(t, tc.program, true)
			want := runInterpreter(t, pipe, []byte(tc.input))
			for _, tune := range tc.tunings {
				got := buildAndRun(t, pipe, []byte(tc.input), codegen.Options{Tuning: tune})
				if got != want {
					t.Errorf("tuning %+v changed the answer: got %q, want %q", tune, got, want)
				}
			}
		})
	}
}
