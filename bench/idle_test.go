package bench

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The idle check every measurement here runs first.
//
// `min` of several runs defends against a neighbour that shows up for one run.
// It does nothing at all about one that never leaves: a leftover process pegging
// a core for hours inflates every figure in the table by the same 5–10% and
// looks exactly like a real result. Worse, in a search it changes *which*
// adaptations get kept — small ones become noise and vanish, large ones lose a
// quarter of their measured value — so the artifact of a contended run is a
// recipe that is wrong rather than a number that is high.
//
// The guard is a hard stop rather than a warning. A benchmark that prints a
// caveat and produces a table anyway produces a table someone will quote.

// maxBusyCores is how much of the machine may be busy before a measurement is
// refused: a quarter of one core, which is enough headroom for the sampler
// itself and for ordinary background daemons, and far below the one full core a
// runaway process holds.
const maxBusyCores = 0.25

// cpuSample is the cumulative jiffies in /proc/stat's aggregate line: total
// across all fields, and the idle portion (idle + iowait).
type cpuSample struct{ total, idle float64 }

func readCPU() (cpuSample, bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSample{}, false
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	if len(f) < 6 || f[0] != "cpu" {
		return cpuSample{}, false
	}
	var s cpuSample
	for i, v := range f[1:] {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return cpuSample{}, false
		}
		s.total += n
		// Fields are user, nice, system, idle, iowait, … — 3 and 4 are the two
		// kinds of not-working.
		if i == 3 || i == 4 {
			s.idle += n
		}
	}
	return s, true
}

// busyCores samples /proc/stat over a short window and returns how many cores'
// worth of work the machine is doing. The second result is false where
// /proc/stat is not available, which is not a reason to refuse to measure —
// only a reason not to claim the machine was checked.
func busyCores(window time.Duration) (float64, bool) {
	a, ok := readCPU()
	if !ok {
		return 0, false
	}
	time.Sleep(window)
	b, ok := readCPU()
	if !ok {
		return 0, false
	}
	dt := b.total - a.total
	if dt <= 0 {
		return 0, false
	}
	busyFraction := 1 - (b.idle-a.idle)/dt
	return busyFraction * float64(runtime.NumCPU()), true
}

// requireIdle fails the test unless the machine is quiet enough to measure on.
//
// Set DOMAIN_BENCH_ANY_LOAD=1 to take the measurement anyway — for a machine
// whose baseline is genuinely not zero, where the alternative is not measuring
// at all. The numbers from such a run are not comparable with numbers from a
// quiet one, and that is the whole reason the variable has to be typed out.
func requireIdle(t *testing.T) {
	t.Helper()
	if os.Getenv("DOMAIN_BENCH_ANY_LOAD") != "" {
		t.Logf("DOMAIN_BENCH_ANY_LOAD is set: measuring without checking the machine is idle")
		return
	}
	busy, ok := busyCores(400 * time.Millisecond)
	if !ok {
		t.Logf("could not read /proc/stat; measuring without an idle check")
		return
	}
	if busy > maxBusyCores {
		t.Fatalf("machine is not idle: %.2f cores busy, limit %.2f\n"+
			"something else is using this box, and every number below would be "+
			"inflated by it — find the process and wait, or set "+
			"DOMAIN_BENCH_ANY_LOAD=1 to measure anyway", busy, maxBusyCores)
	}
	t.Logf("machine is idle (%.2f cores busy of %d)", busy, runtime.NumCPU())
}
