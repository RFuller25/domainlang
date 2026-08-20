package mahoraga

// Watching the program's own bindings, once, to see which of them never moved.
//
// This is the reconnaissance behind turn 8's constant pinning, and it is the
// second of the two runs turn 1 takes that are deliberately not timed (the
// other is the CPU profile). Neither can be folded into the measured runs:
// a probe build carries a map write per binding evaluation, and a build that
// reports on itself is not the build anyone wants measured.
//
// What it is after is narrow. A `Consider l Of length` inside a loop body is
// re-evaluated every lap, and the generator has to emit it as a Go local
// because it cannot know that the list never changes length. Watching it hold
// 16 on all fifty thousand laps is what licenses emitting `16` — and what the
// Go compiler does with that is the actual prize: `% l` stops being a division
// and becomes a mask.
//
// Two findings are kept apart on purpose:
//
//   - **held the same value every time** — a constant of this run, worth a
//     build.
//   - **held a different value at some point** — the loop variable, and
//     pinning it would produce a program that computes something else. The
//     probe reports `varies` and the constant is dropped, not measured.

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"domain/codegen"
)

// Constant is one `Consider` binding measured over an entire run.
type Constant struct {
	// Key identifies the binding the way codegen.Tuning.Constants does.
	Key  string
	Name string
	Line int
	Col  int

	// Value is what it held, and Calls how many times it was evaluated. The
	// count is what separates a binding worth a build from one that is read
	// once at startup and never again.
	Value int64
	Calls int64
}

// String renders a constant the way the report names it.
func (c Constant) String() string {
	return fmt.Sprintf("%s = %d at line %d", c.Name, c.Value, c.Line)
}

// probeFileName is where a probe build writes its report, inside the search's
// work directory.
const probeFileName = "constants.probe"

// collectProbeFacts builds a probe binary, runs it once untimed, and folds
// what it reports — the bindings and the accumulator lengths — into the facts.
//
// Failure is never fatal and never even reported as a skipped turn: a program
// with neither bindings nor unestimated accumulators, a probe build that will
// not compile, a run that does not finish — all of them mean the same thing to
// the search, which is that there is nothing here to pin or to reserve. The
// one thing it must not do is contaminate a measurement, which is why it runs
// here, between the baseline and the first candidate, rather than beside
// anything being timed.
func (s *Search) collectProbeFacts() {
	// Whether there is anything to probe is a question the probe source
	// answers exactly: it carries one dmProbe call per site, so a source with
	// none is a program with no bindings and no unestimated accumulator. That
	// is cheaper than guessing from the baseline's Go and, unlike a guess,
	// cannot be wrong — and it saves the build on every program that has
	// neither, which is most of them.
	c := baselineCandidate()
	c.Tuning.ProbeConstants = true
	goSrc, err := s.emitSource(c)
	if err != nil || !strings.Contains(goSrc, "dmProbe(") {
		return
	}
	bin := filepath.Join(s.workDir, "probe")
	if err := codegen.BuildBinaryWith(goSrc, bin, codegen.BuildConfig{}); err != nil {
		return
	}
	out := filepath.Join(s.workDir, probeFileName)
	if err := s.runProbe(bin, out); err != nil {
		return
	}
	consts, sites, err := readProbe(out)
	if err != nil {
		return
	}
	s.facts.Constants, s.facts.ListSites = consts, sites
}

// runProbe runs the probe binary on the real input with the report hook on.
//
// Bounded, because a probe build is slower than the program it reports on and
// by an amount nobody can predict from the source: a binding inside a hot loop
// is one map write per lap, and a loop with forty million laps is forty
// million of them. A probe that would take longer than the search can afford
// is killed and turn 8 simply has no constants to try — the same outcome as a
// program with no bindings, which is a normal outcome.
func (s *Search) runProbe(bin, out string) error {
	f, err := os.Open(s.opts.Input)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), s.probeTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), codegen.EnvConstProbe+"="+out)
	cmd.Stdin = f
	cmd.Dir = filepath.Dir(s.opts.Input)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// probeTimeout is how long the probe run is given: a generous multiple of what
// the baseline costs, with a floor for programs too quick for the multiple to
// mean anything.
func (s *Search) probeTimeout() time.Duration {
	limit := 30 * s.baseline.Mean
	if limit < minProbeTimeout {
		limit = minProbeTimeout
	}
	return limit
}

const minProbeTimeout = 30 * time.Second

// ListSite is a list accumulator the generator had no estimate for, with the
// longest it was measured growing.
//
// It is the other half of what one probe run reports, and it is read
// differently from a Constant in both directions: a site whose length *varied*
// is still worth reserving for (a capacity is a hint, and the largest run is
// the one to size for), and a site's value is its maximum rather than its
// first.
type ListSite struct {
	Key    string
	Line   int
	Length int
	// Fills is how many times the loop that builds it ran.
	Fills int64
}

// readProbe parses a probe report: one `key first max calls varies` line per
// site, tab separated. Bindings and list sites share the file and are told
// apart by the key's prefix.
//
// Bindings that varied are dropped here rather than carried and filtered
// later. Nothing downstream has any use for them, and a Constant that reached
// a candidate would be a candidate that computes the wrong thing — the failure
// this whole file exists to make impossible.
func readProbe(path string) ([]Constant, []ListSite, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	var consts []Constant
	var sites []ListSite
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			return nil, nil, fmt.Errorf("malformed probe line %q", line)
		}
		first, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("malformed probe value in %q", line)
		}
		maximum, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("malformed probe maximum in %q", line)
		}
		calls, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("malformed probe count in %q", line)
		}
		if key, ok := strings.CutPrefix(fields[0], listSitePrefix); ok {
			// A site that was never filled reports nothing worth reserving.
			if maximum <= 0 {
				continue
			}
			_, line, _ := splitNodeKey(key)
			sites = append(sites, ListSite{
				Key: fields[0], Line: line, Length: int(maximum), Fills: calls,
			})
			continue
		}
		if fields[4] != "false" {
			continue
		}
		c := Constant{Key: fields[0], Value: first, Calls: calls}
		c.Name, c.Line, c.Col = splitConsiderKey(fields[0])
		consts = append(consts, c)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	return consts, sites, nil
}

// listSitePrefix is how codegen.ListSiteKey marks a list accumulator, so one
// report can carry both kinds of finding.
const listSitePrefix = "list:"

// splitNodeKey reads a `Prim@line:col` key back for the report.
func splitNodeKey(key string) (prim string, line, col int) {
	at := strings.LastIndex(key, "@")
	if at < 0 {
		return key, 0, 0
	}
	prim = key[:at]
	l, c, ok := strings.Cut(key[at+1:], ":")
	if !ok {
		return prim, 0, 0
	}
	line, _ = strconv.Atoi(l)
	col, _ = strconv.Atoi(c)
	return prim, line, col
}

// splitConsiderKey reads a key back: "Consider@12:5#l" is `l`, line 12.
//
// A key that does not parse is not an error — the name and the line are for
// the report, not for the build — so it degrades to an unnamed binding rather
// than losing a pin that would have worked.
func splitConsiderKey(key string) (name string, line, col int) {
	at := strings.LastIndex(key, "@")
	hash := strings.LastIndex(key, "#")
	if at < 0 || hash < at {
		return key, 0, 0
	}
	name = key[hash+1:]
	pos := key[at+1 : hash]
	l, c, ok := strings.Cut(pos, ":")
	if !ok {
		return name, 0, 0
	}
	line, _ = strconv.Atoi(l)
	col, _ = strconv.Atoi(c)
	return name, line, col
}
