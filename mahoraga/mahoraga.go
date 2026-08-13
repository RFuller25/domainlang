// Package mahoraga adapts one Domain program to one input.
//
// The optimizer asks what is true of all programs; this asks what is true of
// *this run*, and is allowed to exploit anything it can measure — cutting a
// stage this input never reaches, switching off an optimizer pass that
// pessimises this program, sizing an allocation from a line count read off the
// file. Those are not weaker optimizations. They are answers to a question the
// general optimizer is not permitted to ask.
//
// The design is in docs/superpowers/specs/2026-08-12-mahoraga-design.md. Three
// properties from it shape every type here:
//
//   - **The expected output never reaches anything that generates code.** It
//     lives behind Oracle, which takes bytes and returns a bool. The search
//     state holds no copy of it. Structurally, the answer cannot get into the
//     program.
//   - **The search space is a closed catalogue.** Nothing here mutates code
//     freely, and no transformation replaces a computation with its result —
//     which is why "print the answer" is unreachable rather than rejected.
//   - **Verification happens while adapting, not while running.** An adapted
//     binary carries no checks; every assumption is established here, against
//     the real input, before the adaptation resting on it is accepted.
package mahoraga

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"domain/codegen"
	"domain/optimizer"
	"domain/runner"
)

// TurnNames names the eight turns of the wheel, in order. The wheel draws its
// eight handles before the search has reached any of them, so it needs the
// roster up front rather than one name per TurnStart.
func TurnNames() []string {
	out := make([]string, len(turns))
	for i, t := range turns {
		out[i] = t.name
	}
	return out
}

// TurnBuilt reports whether turn n (1-based) is implemented. A handle for a
// turn the catalogue has not reached is drawn hollow rather than left off, so
// "found nothing" and "did not look" stay distinguishable on the wheel.
func TurnBuilt(n int) bool {
	if n < 1 || n > len(turns) {
		return false
	}
	return turns[n-1].built
}

// Oracle decides whether a candidate's output is correct.
//
// It is a type rather than a comparison written inline because it is the wall
// between the expected answer and the code generator: it is handed the bytes
// once, at construction, and everything downstream can ask only "is this
// right?". Nothing that builds or rewrites a program is given a way to read
// what the right answer is.
type Oracle struct {
	want []byte
}

// NewOracle reads the expected output. The bytes never leave this value.
func NewOracle(path string) (*Oracle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the expected output: %w", err)
	}
	return &Oracle{want: normalizeOutput(data)}, nil
}

// Correct reports whether output matches, ignoring a trailing newline.
func (o *Oracle) Correct(got []byte) bool {
	return bytes.Equal(o.want, normalizeOutput(got))
}

func normalizeOutput(b []byte) []byte {
	return bytes.TrimRight(b, "\r\n")
}

// Tier is how far an adaptation commits.
type Tier int

const (
	// General adaptations hold for any input: a different pass schedule, a
	// profile-guided rebuild. Nothing to verify.
	General Tier = iota
	// Guarded adaptations take a fast path for the observed shape and keep a
	// fallback, so they remain correct for any input.
	Guarded
	// Pinned adaptations are correct only for inputs meeting the recorded
	// contract. The binary carries no check — the contract was verified here,
	// while searching — so a pinned binary will not notice a different input.
	Pinned
)

func (t Tier) String() string {
	switch t {
	case General:
		return "general"
	case Guarded:
		return "guarded"
	case Pinned:
		return "pinned"
	}
	return "unknown"
}

// ParseTier reads a --tier value.
func ParseTier(s string) (Tier, bool) {
	switch s {
	case "general":
		return General, true
	case "guarded":
		return Guarded, true
	case "pinned", "":
		return Pinned, true
	}
	return 0, false
}

// Candidate is one configuration to build and measure.
//
// Everything in it is semantics-preserving today: a pass schedule cannot
// change what a program computes, and neither can a build flag. The catalogue
// entries that can (guarded and pinned specialisations) attach here in a later
// phase, each carrying the precondition that must hold for it.
type Candidate struct {
	Label    string
	Schedule optimizer.Schedule
	Build    codegen.BuildConfig
	// Tuning is what the search measured about the input, handed to the code
	// generator. Its zero value is exactly what `domain build` produces, so a
	// candidate that carries none is the compiler's own output.
	Tuning codegen.Tuning
	Tier   Tier

	// Pin records what this candidate assumes of a future input, for pinned
	// candidates. Nil when it assumes nothing.
	Pin func(f Facts, c *Contract)
}

// Measurement is what a candidate cost.
type Measurement struct {
	Mean   time.Duration
	Min    time.Duration
	StdDev time.Duration
	// StdErr is how precisely Mean is known — the statistic a comparison
	// between two configurations actually turns on. See stats.go.
	StdErr  time.Duration
	Runs    int
	Correct bool

	// Failure is why this candidate produced no usable measurement: it could
	// not be built, did not finish, or answered wrongly.
	Failure string
}

// OK reports whether this measurement can be compared with another.
func (m Measurement) OK() bool { return m.Failure == "" && m.Correct && m.Mean > 0 }

// Options controls a search.
type Options struct {
	Program  string // the .domain file
	Input    string // the input it is adapted to
	Expected string // the expected output, read only through an Oracle

	// BaselineRuns is how many times the baseline is measured. The mean of
	// these is what everything is compared against, and their spread sets the
	// noise floor.
	BaselineRuns int

	// ScreenRuns is how many runs a candidate gets before it is either
	// discarded or promoted to a full confirmation at BaselineRuns. Most
	// candidates lose; paying full price for each is what turns an hour into a
	// day.
	ScreenRuns int

	// MinEffect is the improvement, as a fraction, a candidate must show
	// beyond the noise floor before it is accepted. Zero means the default.
	MinEffect float64

	Turns   int // how many turns of the wheel; zero means all eight
	Tier    Tier
	Timeout time.Duration
	Seed    int64

	// Out is where the adapted binary is written, Recipe where the JSON goes.
	Out    string
	Recipe string
}

const (
	DefaultBaselineRuns = 10
	DefaultScreenRuns   = 3
	// DefaultMinEffect is the improvement a candidate must show *on top of*
	// the noise floor. Two percent is deliberately unambitious: the failure
	// this guards against is a tuner that reports a win it cannot reproduce,
	// and bench/README.md's own verdict on a change inside ±1% was that "the
	// elimination is real and the speedup is not".
	DefaultMinEffect = 0.02
)

func (o Options) baselineRuns() int {
	if o.BaselineRuns <= 0 {
		return DefaultBaselineRuns
	}
	return o.BaselineRuns
}

func (o Options) screenRuns() int {
	if o.ScreenRuns <= 0 {
		return DefaultScreenRuns
	}
	return o.ScreenRuns
}

func (o Options) minEffect() float64 {
	if o.MinEffect <= 0 {
		return DefaultMinEffect
	}
	return o.MinEffect
}

func (o Options) turns() int {
	if o.Turns <= 0 || o.Turns > 8 {
		return 8
	}
	return o.Turns
}

// DefaultOut is where the adapted binary goes when -o is not given.
func DefaultOut(program string) string {
	stem := strings.TrimSuffix(filepath.Base(program), ".domain")
	return filepath.Join(filepath.Dir(program), stem+"-adapted")
}

// DefaultRecipe is where the recipe goes when --recipe is not given.
func DefaultRecipe(program string) string {
	stem := strings.TrimSuffix(filepath.Base(program), ".domain")
	return filepath.Join(filepath.Dir(program), stem+".mahoraga.json")
}

// Event is what the search reports as it goes: consumed by the plain reporter
// today and by the wheel later. The search never writes to a terminal itself,
// so the same search drives both.
type Event struct {
	Kind      EventKind
	Turn      int
	TurnName  string
	Candidate string
	Total     int // candidates in this turn, when known
	Index     int // 1-based position within the turn

	Measurement Measurement
	// Champion is what the champion measured *in the same race*, alternating
	// run by run with the candidate.
	//
	// It rides along because the candidate's absolute figure means nothing on
	// its own. A recipe caught this: a candidate accepted at 842ms was drawn as
	// "best" beside a baseline of 713ms, reading 0.85× with a tick next to it,
	// because the two numbers came from different minutes of a machine that had
	// drifted twenty percent in between. The only drift-free quantity in a race
	// is the *ratio* of its two sides, and a display that has both can show one.
	Champion Measurement
	Effect   float64 // fraction improvement against the champion
	Reason   string  // why a candidate was rejected
	Tier     Tier

	// Schedule, Build and Tuning describe the champion *after* an Adapted
	// event: what a recipe written at that moment would carry. They ride along
	// on the event rather than being read off the search because the wheel
	// renders on a different goroutine, and a display that reached into the
	// search's state while it ran would be reading a value the search is
	// writing.
	Schedule optimizer.Schedule
	Build    codegen.BuildConfig
	Tuning   codegen.Tuning
}

type EventKind int

const (
	TurnStart EventKind = iota
	CandidateStart
	CandidateMeasured
	Adapted
	Rejected
	TurnEnd
	SearchDone
)

// Reporter consumes events. A nil Reporter is fine — the search runs headless.
type Reporter interface{ Report(Event) }

// ReporterFunc adapts a function to Reporter.
type ReporterFunc func(Event)

func (f ReporterFunc) Report(e Event) { f(e) }

// runnerOpts is the measurement configuration shared by every run in a search.
func (s *Search) runnerOpts(runs int) runner.Options {
	return runner.Options{
		Runs:       runs,
		Timeout:    s.opts.Timeout,
		KeepStdout: true,
		DomainBin:  s.domainBin,
	}
}
