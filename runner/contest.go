package runner

// Racing a Domain program against a program in another language.
//
// `domain expansion: battle` publishes a head-to-head number, which is the
// most contestable thing this repo produces — so the two sides are measured by
// exactly the same code as everything else here: both subprocesses, both
// reading the input as a redirected regular file, best of N interleaved so
// drift lands on both. The only difference between a contestant that is a
// Domain program and one that is not is how its argv gets built.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Contestant is one side of a race. Exactly one of Program and Argv is set.
type Contestant struct {
	// Label names the side in a report.
	Label string

	// Program is a Domain program, run under Config.
	Program string
	Config  Config

	// Argv is an external command — a Python or Weave program with its
	// runtime in front of it. Dir is the working directory it runs in;
	// empty means the directory holding the program.
	Argv []string
	Dir  string
}

// External reports whether this side is not a Domain program.
func (c Contestant) External() bool { return len(c.Argv) > 0 }

// RaceContestants measures every contestant against the same input.
//
// Runs are interleaved round-robin rather than run N-at-a-time per side, for
// the reason bench/README.md gives: a load spike or a thermal step that lands
// entirely on one side would be indistinguishable from that side being slower.
func RaceContestants(cs []Contestant, in Input, opts Options) ([]Result, error) {
	preps := make([]*prepared, len(cs))
	cmds := make([]*command, len(cs))
	results := make([]Result, len(cs))
	defer func() {
		for _, p := range preps {
			if p != nil {
				p.cleanup()
			}
		}
	}()

	for i, c := range cs {
		results[i].Config = c.Config
		prep, cmd, err := setupContestant(c, in, opts)
		if err != nil {
			results[i].Err = err
			continue
		}
		preps[i], cmds[i] = prep, cmd
		results[i].Build = cmd.buildTime
	}

	for rep := range opts.runs() {
		for i, cmd := range cmds {
			if cmd == nil {
				continue
			}
			r := &results[i]
			if r.Err != nil || r.Timeout {
				continue
			}
			out, err := cmd.run(preps[i], opts.timeout(), rep == 0 || opts.KeepStdout)
			if err != nil {
				r.Err = err
				continue
			}
			if rep == 0 || opts.KeepStdout {
				r.Stdout = out.stdout
			}
			r.Stderr = out.stderr
			r.ExitCode = out.exitCode
			r.Timeout = out.timedOut
			r.Alloc.PeakRSS = max(r.Alloc.PeakRSS, out.peakRSS)
			r.Samples = append(r.Samples, out.wall)
			if rep == 0 || out.wall < r.Wall {
				r.Wall = out.wall
			}
		}
	}

	// The allocation figures come from their own untimed run, for the reason
	// alloc.go gives: reading MemStats stops the world, so a stopwatch must not
	// be pointed at the execution that reports them.
	if opts.Alloc {
		for i, cmd := range cmds {
			if cmd == nil || results[i].Err != nil || results[i].Timeout {
				continue
			}
			results[i].Alloc = cmd.measureAlloc(preps[i], opts.timeout())
		}
	}
	return results, nil
}

// setupContestant arranges the input for one side and builds its command.
func setupContestant(c Contestant, in Input, opts Options) (*prepared, *command, error) {
	if c.External() {
		prep, err := prepareExternal(c, in)
		if err != nil {
			return nil, nil, err
		}
		return prep, &command{path: c.Argv[0], args: c.Argv[1:]}, nil
	}
	prep, err := prepare(c.Program, in)
	if err != nil {
		return nil, nil, err
	}
	cmd, err := buildCommand(c.Program, c.Config, opts)
	if err != nil {
		prep.cleanup()
		return nil, nil, err
	}
	return prep, cmd, nil
}

// prepareExternal materializes the input for a non-Domain program.
//
// There is no source-target mirroring to do here: a program in another
// language reads its input from stdin, which is the whole contract the foreign
// wire format is built on — sys.stdin in Python, Source in Weave, os.Stdin in
// Go. So the input is a file and the file is redirected, which is the same
// thing the Domain side gets.
func prepareExternal(c Contestant, in Input) (*prepared, error) {
	p := &prepared{workDir: c.Dir}
	if p.workDir == "" && len(c.Argv) > 0 {
		p.workDir = filepath.Dir(c.Argv[len(c.Argv)-1])
	}
	if p.workDir == "" {
		p.workDir = "."
	}

	inputPath := in.Path
	if in.Bytes != nil {
		dir, err := os.MkdirTemp("", "domain-contest-in-*")
		if err != nil {
			return nil, err
		}
		p.tmpDir = dir
		inputPath = filepath.Join(dir, "input")
		if err := os.WriteFile(inputPath, in.Bytes, 0o644); err != nil {
			p.cleanup()
			return nil, err
		}
	}
	if inputPath != "" {
		abs, err := filepath.Abs(inputPath)
		if err != nil {
			p.cleanup()
			return nil, err
		}
		p.inputPath = abs
	}
	return p, nil
}

// CheckAgreement runs every contestant exactly once and reports whether they
// produced identical output.
//
// This is the precondition for a race being worth reporting at all, and it is
// deliberately separate from the timing runs: a faster program that prints the
// wrong answer has not come out on top, and publishing a time for it would be
// the single most misleading thing a head-to-head could do.
func CheckAgreement(cs []Contestant, in Input, opts Options) ([]Result, error) {
	one := opts
	one.Runs = 1
	one.KeepStdout = true
	one.Alloc = false
	return RaceContestants(cs, in, one)
}

// Disagreement describes two sides that did not produce the same output.
type Disagreement struct {
	A, B       int    // indices into the contestant list
	Line       int    // 1-based line where they first differ; 0 when one failed
	AText      string // that line on each side
	BText      string
	AFailed    bool
	BFailed    bool
	FailReason string
}

// FindDisagreement compares results pairwise against the first side that
// produced output, returning nil when everything that ran agreed.
func FindDisagreement(results []Result) *Disagreement {
	ref := -1
	for i := range results {
		if results[i].Err == nil && !results[i].Failed() {
			ref = i
			break
		}
	}
	if ref < 0 {
		// Nothing succeeded. Report the first failure so the caller has
		// something to show rather than an empty verdict.
		for i := range results {
			if results[i].Err != nil || results[i].Failed() {
				return &Disagreement{
					A: i, B: i, AFailed: true, BFailed: true,
					FailReason: failReason(&results[i]),
				}
			}
		}
		return nil
	}
	for i := range results {
		if i == ref {
			continue
		}
		r := &results[i]
		if r.Err != nil || r.Failed() {
			return &Disagreement{A: ref, B: i, BFailed: true, FailReason: failReason(r)}
		}
		if string(r.Stdout) == string(results[ref].Stdout) {
			continue
		}
		line, a, b := firstDifference(results[ref].Stdout, r.Stdout)
		return &Disagreement{A: ref, B: i, Line: line, AText: a, BText: b}
	}
	return nil
}

func failReason(r *Result) string {
	switch {
	case r.Err != nil:
		return r.Err.Error()
	case r.Timeout:
		return "did not finish"
	default:
		return fmt.Sprintf("exit %d", r.ExitCode)
	}
}

// firstDifference is the 1-based line number where two outputs diverge, and
// that line from each. A trailing newline is not a difference: the two sides
// are different languages' print statements, and holding one to the other's
// final byte would fail every race for a reason nobody cares about.
func firstDifference(a, b []byte) (int, string, string) {
	as, bs := splitLines(a), splitLines(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			return i + 1, x, y
		}
	}
	return 0, "", ""
}

func splitLines(b []byte) []string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// SameOutput reports whether two results printed the same thing, ignoring a
// trailing newline.
func SameOutput(a, b *Result) bool {
	n, _, _ := firstDifference(a.Stdout, b.Stdout)
	return n == 0
}

// Speedup is a ÷ b as a ratio, or 0 when either side has no time.
func Speedup(a, b time.Duration) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return float64(a) / float64(b)
}
