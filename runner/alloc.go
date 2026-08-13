package runner

// Measuring what a run allocated.
//
// Two numbers, with two different standards of evidence, and the report keeps
// them apart because one is always available and the other has to be offered.
//
// **Peak RSS** comes from the kernel, via wait4's rusage. It needs no
// cooperation from the child, so it works for a compiled Domain binary, for
// `domain run`, and for the Python or Go program `battle` races against. It
// is the headline figure.
//
// **Bytes allocated, allocation count and GC cycles** come from the Go
// runtime's own accounting, which means the measured process has to report
// them. Both of ours can: the domain binary and a compiled Domain program each
// check DOMAIN_ALLOC_REPORT at exit and, when it names a file, write one line
// of runtime.MemStats into it. A process that does not report leaves those
// fields zero and Reported false — the report prints a dash rather than
// guessing.
//
// One rule keeps this from corrupting what it measures: **an allocation run is
// never a timing run**. reflect on the numbers if you like, but reading
// MemStats stops the world, so the allocation figures come from a separate
// execution that no stopwatch is pointed at.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// EnvAllocReport names the file a measured process writes its allocation
// figures to. Empty or unset means "do not report", which is every ordinary
// run of a Domain program.
const EnvAllocReport = "DOMAIN_ALLOC_REPORT"

// AllocStats is what one run allocated.
type AllocStats struct {
	PeakRSS int64 // bytes, from the kernel; 0 when unavailable

	// Reported is whether the fields below carry a number. They come from the
	// measured process's own runtime and are absent for anything that does not
	// speak the protocol — a foreign program, or a binary built before it.
	Reported   bool
	TotalAlloc uint64 // cumulative bytes allocated over the whole run
	Mallocs    uint64 // cumulative allocation count
	HeapSys    uint64 // bytes of heap obtained from the OS
	NumGC      uint32
}

// WriteReport writes the current process's allocation figures to the file
// named by DOMAIN_ALLOC_REPORT, if it is set. It is called at exit by the
// domain binary and by a compiled Domain program; when the variable is unset
// it costs one environment lookup and returns.
func WriteReport() {
	path := os.Getenv(EnvAllocReport)
	if path == "" {
		return
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	line := fmt.Sprintf("%d %d %d %d\n", m.TotalAlloc, m.Mallocs, m.HeapSys, m.NumGC)
	_ = os.WriteFile(path, []byte(line), 0o644)
}

// parseReport reads a report file back.
func parseReport(path string) (AllocStats, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AllocStats{}, false
	}
	fields := strings.Fields(string(data))
	if len(fields) != 4 {
		return AllocStats{}, false
	}
	var s AllocStats
	vals := make([]uint64, 4)
	for i, f := range fields {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return AllocStats{}, false
		}
		vals[i] = v
	}
	s.TotalAlloc, s.Mallocs, s.HeapSys = vals[0], vals[1], vals[2]
	s.NumGC = uint32(vals[3])
	s.Reported = true
	return s, true
}

// measureAlloc runs the command once more, untimed, with the reporting hook
// switched on, and collects whatever the run is willing to say.
func (c *command) measureAlloc(prep *prepared, timeout time.Duration) AllocStats {
	dir, err := os.MkdirTemp("", "domain-alloc-*")
	if err != nil {
		return AllocStats{}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	report := filepath.Join(dir, "alloc")

	res, err := c.exec(prep, timeout, false, []string{EnvAllocReport + "=" + report})
	if err != nil || res == nil {
		return AllocStats{}
	}
	stats, _ := parseReport(report)
	stats.PeakRSS = res.peakRSS
	return stats
}
