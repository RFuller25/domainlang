// Package runner executes a Domain program under a chosen backend and
// optimizer setting and measures what it cost.
//
// It is the single place a program gets run for measurement, shared by every
// `domain expansion:` command that answers its question by running something
// more than once — bench, battle, stats, shrink, fuzz, golf. Seven commands
// with seven private timing loops would be seven subtly different answers to
// "how long does this program take", which is the one number they all have to
// agree on.
//
// # Why every timed run is a subprocess
//
// bench/README.md establishes the methodology these numbers live or die by,
// and this package inherits it rather than reinventing it:
//
//   - **Both sides are subprocesses**, so both pay process startup. That
//     includes the *interpreted* configurations: timing an in-process
//     interp.Run against an out-of-process binary would hand the interpreter a
//     free win on startup and tilt the bench table in the direction that
//     flatters the language. Interpreted runs therefore re-execute the domain
//     binary as `domain run <file>`.
//   - **The input is a redirected regular file**, never a pipe. Over a pipe
//     nothing can size its read up front, which costs a whole-input reader
//     roughly 3× on the read alone.
//   - **Best of N, alternating.** The minimum is closest to the cost of the
//     work, since noise only ever adds; alternating between configurations
//     spreads thermal and cache drift across all of them instead of loading it
//     onto whichever ran last.
//
// In-process interpretation is still available, as Interpret, for the callers
// that want the trace hook rather than a timing (coverage, fuzz). It is not
// timed, and says so.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
	"domain/prims"
)

// Config is one of the ways a Domain program can be run.
type Config struct {
	Compiled bool // false: interpret via `domain run`; true: codegen, build, exec
	Optimize bool // false: the naive pipeline, which is the correctness oracle
	Release  bool // shed Binding Vows
}

// Label names a configuration the way a report column does.
func (c Config) Label() string {
	backend, opt := "interpret", "optimized"
	if c.Compiled {
		backend = "compile"
	}
	if !c.Optimize {
		opt = "naive"
	}
	s := backend + " / " + opt
	if c.Release {
		s += " / release"
	}
	return s
}

// Four is the grid bench reports and fuzz differentials against: both
// backends, each with the optimizer on and off.
var Four = []Config{
	{Compiled: false, Optimize: false},
	{Compiled: false, Optimize: true},
	{Compiled: true, Optimize: false},
	{Compiled: true, Optimize: true},
}

// Input is what the program reads. At most one field is set; the zero Input
// means the program gets no input at all.
type Input struct {
	Path  string // an existing file
	Bytes []byte // materialized to a temp file before the run
}

// Result is one measured execution.
type Result struct {
	Config   Config
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Timeout  bool // the run hit the deadline and was killed

	// Err is a *harness* failure — the program could not be built, or the
	// process could not be started. A program that ran and failed on its own
	// terms is not an Err: it is a nonzero ExitCode with its Stderr intact,
	// which is the outcome shrink and fuzz are looking for.
	Err error

	Wall  time.Duration // best of N, the run only
	Build time.Duration // compile time; zero when interpreted
	Alloc AllocStats

	// Samples is every run's wall time, in the order they were taken.
	//
	// Wall is the minimum of these, which is the right figure for "how fast
	// is this program" — noise only ever adds. But a caller deciding whether
	// two configurations actually differ needs the spread as well as the
	// floor, and cannot recover it from a minimum. `domain expansion:
	// mahoraga` sets its noise floor from these.
	Samples []time.Duration
}

// Mean is the average of the samples, and StdDev their population standard
// deviation. Both are zero when nothing was measured.
func (r *Result) Mean() time.Duration {
	if len(r.Samples) == 0 {
		return 0
	}
	var total time.Duration
	for _, s := range r.Samples {
		total += s
	}
	return total / time.Duration(len(r.Samples))
}

func (r *Result) StdDev() time.Duration {
	if len(r.Samples) < 2 {
		return 0
	}
	mean := float64(r.Mean())
	var sum float64
	for _, s := range r.Samples {
		d := float64(s) - mean
		sum += d * d
	}
	return time.Duration(math.Sqrt(sum / float64(len(r.Samples))))
}

// Failed reports whether the program itself failed, as distinct from the
// harness failing to run it.
func (r *Result) Failed() bool { return r.Err == nil && (r.ExitCode != 0 || r.Timeout) }

// Options controls how a measurement is taken.
type Options struct {
	// Runs is how many times each configuration is executed; the reported
	// Wall is the fastest. Zero means DefaultRuns.
	Runs int

	// Timeout bounds a single run. Zero means DefaultTimeout. A run that hits
	// it is killed and reported with Timeout set — non-termination is a
	// finding for fuzz and shrink, not a hang of the tool.
	Timeout time.Duration

	// Alloc adds a separate, untimed run per configuration to collect
	// allocation figures. It is never the same execution as a timing run:
	// reading the runtime's memory stats stops the world.
	Alloc bool

	// DomainBin is the domain binary used for interpreted runs. Empty means
	// the currently running executable, which is what the CLI wants. Tests set
	// it, because there the running executable is the test binary.
	DomainBin string

	// KeepStdout retains each run's output. Off by default: a program whose
	// answer is megabytes of text should not be held in memory five times over
	// just to be timed. The output of the *first* run of each configuration is
	// always captured regardless, since every caller compares outputs.
	KeepStdout bool
}

const (
	DefaultRuns    = 5
	DefaultTimeout = 60 * time.Second
)

func (o Options) runs() int {
	if o.Runs <= 0 {
		return DefaultRuns
	}
	return o.Runs
}

func (o Options) timeout() time.Duration {
	if o.Timeout <= 0 {
		return DefaultTimeout
	}
	return o.Timeout
}

func (o Options) domainBin() (string, error) {
	if o.DomainBin != "" {
		return o.DomainBin, nil
	}
	return os.Executable()
}

// Race measures every configuration against the same input and returns the
// results in the order the configurations were given.
//
// Runs are interleaved round-robin rather than run N-at-a-time per
// configuration, so a load spike or a thermal step lands on every side rather
// than on whichever happened to be running.
func Race(program string, configs []Config, in Input, opts Options) ([]Result, error) {
	prep, err := prepare(program, in)
	if err != nil {
		return nil, err
	}
	defer prep.cleanup()

	results := make([]Result, len(configs))
	cmds := make([]*command, len(configs))
	for i, c := range configs {
		results[i].Config = c
		cmd, err := buildCommand(program, c, opts)
		if err != nil {
			results[i].Err = err
			continue
		}
		cmds[i] = cmd
		results[i].Build = cmd.buildTime
	}

	for rep := range opts.runs() {
		for i, cmd := range cmds {
			if cmd == nil {
				continue
			}
			r := &results[i]
			if r.Err != nil || r.Timeout {
				continue // a configuration that cannot run is not retried
			}
			out, err := cmd.run(prep, opts.timeout(), rep == 0 || opts.KeepStdout)
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
			r.Samples = append(r.Samples, out.wall)
			if rep == 0 || out.wall < r.Wall {
				r.Wall = out.wall
			}
		}
	}

	if opts.Alloc {
		for i, cmd := range cmds {
			if cmd == nil || results[i].Err != nil || results[i].Timeout {
				continue
			}
			results[i].Alloc = cmd.measureAlloc(prep, opts.timeout())
		}
	}
	return results, nil
}

// Run measures a single configuration.
func Run(program string, c Config, in Input, opts Options) (Result, error) {
	rs, err := Race(program, []Config{c}, in, opts)
	if err != nil {
		return Result{}, err
	}
	return rs[0], nil
}

// Once executes a configuration exactly one time, keeping the output and
// skipping the best-of-N loop. This is what shrink and fuzz drive: they run
// thousands of different inputs once each, and the answer they want is what
// the program did, not how fast it did it.
func Once(program string, c Config, in Input, opts Options) (Result, error) {
	opts.Runs = 1
	opts.KeepStdout = true
	opts.Alloc = false
	return Run(program, c, in, opts)
}

// ---------------------------------------------------------------------------
// Building one configuration's command
// ---------------------------------------------------------------------------

// command is a prepared, repeatable execution of one configuration.
//
// A compiled command is cached for the life of the process, keyed by program
// and configuration. Without that, a fuzz campaign or a shrink search — both
// of which run one configuration over thousands of different inputs — would
// pay a full codegen and Go build per candidate, and the search would be
// measuring the compiler rather than the program.
type command struct {
	path      string   // binary to exec
	args      []string // arguments after path
	interpret bool     // the program path is an argument, not baked into the binary
	buildTime time.Duration
	tmpDir    string // build output, removed by Cleanup
}

var (
	buildMu    sync.Mutex
	buildCache = map[string]*command{}
)

// Cleanup removes every binary this process built. The CLI defers it; a
// caller that skips it leaks a temp directory until the OS reclaims it.
func Cleanup() {
	buildMu.Lock()
	defer buildMu.Unlock()
	for k, c := range buildCache {
		if c.tmpDir != "" {
			_ = os.RemoveAll(c.tmpDir)
		}
		delete(buildCache, k)
	}
}

func cacheKey(program string, c Config) string {
	abs, err := filepath.Abs(program)
	if err != nil {
		abs = program
	}
	return fmt.Sprintf("%s|%v|%v|%v", abs, c.Compiled, c.Optimize, c.Release)
}

func buildCommand(program string, c Config, opts Options) (*command, error) {
	if !c.Compiled {
		bin, err := opts.domainBin()
		if err != nil {
			return nil, fmt.Errorf("locating the domain binary: %w", err)
		}
		args := []string{"run"}
		if !c.Optimize {
			args = append(args, "--no-optimize")
		}
		if c.Release {
			args = append(args, "--release")
		}
		// The program path is appended at run time, because prepare may have
		// mirrored the program into a temp directory for this input.
		return &command{path: bin, args: args, interpret: true}, nil
	}

	key := cacheKey(program, c)
	buildMu.Lock()
	if cached, ok := buildCache[key]; ok {
		buildMu.Unlock()
		return cached, nil
	}
	buildMu.Unlock()

	start := time.Now()
	pipe, err := LoadPipeline(program, c.Optimize)
	if err != nil {
		return nil, err
	}
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{Release: c.Release})
	if err != nil {
		return nil, fmt.Errorf("generating Go: %w", err)
	}
	dir, err := os.MkdirTemp("", "domain-runner-bin-*")
	if err != nil {
		return nil, err
	}
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("building: %w", err)
	}
	cmd := &command{path: bin, buildTime: time.Since(start), tmpDir: dir}
	buildMu.Lock()
	// A concurrent builder may have won the race; keep whichever is already
	// published so every caller shares one binary, and drop this one.
	if existing, ok := buildCache[key]; ok {
		buildMu.Unlock()
		_ = os.RemoveAll(dir)
		return existing, nil
	}
	buildCache[key] = cmd
	buildMu.Unlock()
	return cmd, nil
}

// LoadPipeline runs the front end over a program file and optionally
// optimizes it. Callers that need the *unoptimized* pipeline for measurement
// (coverage, which must not let fuseMapMap hide a Map Each) pass false.
func LoadPipeline(program string, optimize bool) (*ir.Pipeline, error) {
	src, err := os.ReadFile(program)
	if err != nil {
		return nil, err
	}
	toks, err := lexer.Lex(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", program, err)
	}
	prog, err := parser.Parse(string(src), toks)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", program, err)
	}
	pipe, err := prims.ResolveWith(prog, prims.FileOptions(program))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", program, err)
	}
	optimizer.Optimize(pipe, optimize)
	return pipe, nil
}

// LoadRewrites runs the front end and reports which optimizer passes fired,
// for the callers that want the rewrite list rather than the pipeline.
func LoadRewrites(program string) (*ir.Pipeline, []optimizer.Rewrite, error) {
	src, err := os.ReadFile(program)
	if err != nil {
		return nil, nil, err
	}
	toks, err := lexer.Lex(string(src))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", program, err)
	}
	prog, err := parser.Parse(string(src), toks)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", program, err)
	}
	pipe, err := prims.ResolveWith(prog, prims.FileOptions(program))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", program, err)
	}
	return pipe, optimizer.Optimize(pipe, true), nil
}

// ---------------------------------------------------------------------------
// Running it
// ---------------------------------------------------------------------------

type execResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
	wall     time.Duration
	timedOut bool
	peakRSS  int64
}

// run executes the command once with the prepared input redirected onto its
// stdin, and returns what happened.
func (c *command) run(prep *prepared, timeout time.Duration, keepStdout bool) (*execResult, error) {
	return c.exec(prep, timeout, keepStdout, nil)
}

func (c *command) exec(prep *prepared, timeout time.Duration, keepStdout bool, env []string) (*execResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := c.args
	if c.interpret {
		args = append(append([]string(nil), args...), prep.programPath)
	}
	cmd := exec.CommandContext(ctx, c.path, args...)
	cmd.Dir = prep.workDir
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	if prep.inputPath != "" {
		f, err := os.Open(prep.inputPath)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		cmd.Stdin = f
	}

	var outBuf, errBuf bytes.Buffer
	if keepStdout {
		cmd.Stdout = &outBuf
	} else {
		cmd.Stdout = nil // discarded by os/exec
	}
	cmd.Stderr = &errBuf

	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start)

	res := &execResult{stderr: errBuf.Bytes(), wall: wall}
	if keepStdout {
		res.stdout = outBuf.Bytes()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.timedOut = true
		res.exitCode = -1
		return res, nil
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.exitCode = ee.ExitCode()
			res.rusage(ee.ProcessState)
			return res, nil
		}
		return nil, err // could not start: a harness failure
	}
	res.rusage(cmd.ProcessState)
	return res, nil
}

// ---------------------------------------------------------------------------
// Input preparation
// ---------------------------------------------------------------------------

// prepared is a program plus an input, arranged so the program reads the
// input whichever way it asks for it.
type prepared struct {
	programPath string // possibly inside a mirrored directory
	workDir     string
	inputPath   string
	tmpDir      string
}

func (p *prepared) cleanup() {
	if p.tmpDir != "" {
		_ = os.RemoveAll(p.tmpDir)
	}
}

// prepare arranges for the program to read the given input.
//
// A Domain program names its own source (`Cursed Energy: input.txt`) and falls
// back to stdin when that file is absent, so "give this program this input"
// has two different meanings depending on the program. Redirecting stdin alone
// is not enough: run in the program's own directory, a real input.txt sitting
// beside it would shadow the input the caller asked for — which would silently
// make every shrink candidate and every fuzz input a no-op.
//
// So when the program names a file, its directory is *mirrored* into a temp
// directory with symlinks — preserving sibling libraries an import needs and
// any other data file it reads — and the caller's input is written in place of
// the named target. Nothing in the user's directory is touched. When the
// program reads stdin, no mirror is needed and the redirect is the whole job.
func prepare(program string, in Input) (*prepared, error) {
	abs, err := filepath.Abs(program)
	if err != nil {
		return nil, err
	}
	p := &prepared{programPath: abs, workDir: filepath.Dir(abs)}

	inputPath := in.Path
	if in.Bytes != nil {
		dir, err := os.MkdirTemp("", "domain-runner-in-*")
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
	if inputPath == "" {
		return p, nil
	}
	if inputPath, err = filepath.Abs(inputPath); err != nil {
		p.cleanup()
		return nil, err
	}
	p.inputPath = inputPath

	target, err := sourceTarget(abs)
	if err != nil {
		p.cleanup()
		return nil, err
	}
	if target == "" {
		return p, nil // reads stdin; the redirect is enough
	}

	mirror, err := mirrorDir(filepath.Dir(abs), target, inputPath, p.tmpDir)
	if err != nil {
		p.cleanup()
		return nil, err
	}
	if p.tmpDir == "" {
		p.tmpDir = mirror
	}
	p.workDir = mirror
	p.programPath = filepath.Join(mirror, filepath.Base(abs))
	return p, nil
}

// sourceTarget is the file a program's first stage names, or "" when it reads
// stdin. The pipeline is resolved unoptimized, since nothing about the source
// stage depends on optimization and this is the cheaper front end.
func sourceTarget(program string) (string, error) {
	pipe, err := LoadPipeline(program, false)
	if err != nil {
		return "", err
	}
	if len(pipe.Nodes) == 0 {
		return "", nil
	}
	n := pipe.Nodes[0]
	if n.Prim != "Read Source" {
		return "", nil
	}
	t, _ := n.Meta["target"].(string)
	if t == "" || equalFold(t, "stdin") {
		return "", nil
	}
	return t, nil
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// mirrorDir builds a temp directory that looks like src to the program:
// every entry symlinked across, except target, which is a copy of input.
func mirrorDir(src, target, input, reuse string) (string, error) {
	dir := reuse
	if dir == "" {
		d, err := os.MkdirTemp("", "domain-runner-dir-*")
		if err != nil {
			return "", err
		}
		dir = d
	} else {
		dir = filepath.Join(dir, "work")
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", err
		}
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Name() == target {
			continue // replaced by the caller's input, below
		}
		from, err := filepath.Abs(filepath.Join(src, e.Name()))
		if err != nil {
			return "", err
		}
		// A symlink that cannot be made (an exotic filesystem, Windows
		// without the privilege) is not fatal: the entry is simply absent,
		// and a program that needed it fails with its own error rather than
		// the harness failing with one nobody asked about.
		_ = os.Symlink(from, filepath.Join(dir, e.Name()))
	}

	// The target may name a path with directories in it ("data/day1.txt").
	dst := filepath.Join(dir, filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	// Remove any symlink the mirror loop made for an intermediate directory
	// so the copy lands in the temp tree rather than through the link.
	_ = os.Remove(dst)
	data, err := os.ReadFile(input)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// ---------------------------------------------------------------------------
// Interpretation in process, for the callers that want the trace hook
// ---------------------------------------------------------------------------

// Interpret runs a program in this process with the given context, returning
// the pipeline it ran. It takes no timing: an in-process run is not comparable
// with a subprocess one, and reporting a duration from here would invite
// exactly that comparison.
//
// It is what coverage --dynamic and fuzz drive, because both need a hook
// installed on the Context (a Tracer, a Probe) that a subprocess cannot carry.
//
// # One at a time, process-wide
//
// The interpreter keeps process-global mutable state — eval.bindings, reset
// per run by eval.ResetBindings, and ir.currentCtx, saved and restored around
// every node evaluation in ir.EvalNode. Two interpretations running at once
// used to corrupt each other, and the symptom was not a clean failure: it
// surfaced as impossible values deep inside an unrelated primitive, a negative
// slice bound most often.
//
// interp.Run now holds a process-wide mutex for the duration of a run, so
// concurrent callers serialise rather than race. Correctness, not throughput:
// a caller that interprets many inputs still gets no parallelism from doing it
// on several goroutines. Every measured run goes through a subprocess, which is
// immune by construction, and that remains one of the several reasons the timed
// path is built that way.
func Interpret(program string, optimize bool, ctx *ir.Context) (*ir.Pipeline, ir.Value, error) {
	pipe, err := LoadPipeline(program, optimize)
	if err != nil {
		return nil, nil, err
	}
	if ctx.BaseDir == "" {
		ctx.BaseDir = filepath.Dir(program)
	}
	v, err := interp.Run(pipe, ctx)
	return pipe, v, err
}

// Prebuild compiles a configuration up front, so a campaign that is about to
// run thousands of inputs through it pays the build once and reports a build
// failure before the search starts rather than on the first candidate.
func Prebuild(program string, c Config, opts Options) error {
	_, err := buildCommand(program, c, opts)
	return err
}

// LoadPipelineSchedule runs the front end and optimizes with a chosen pass
// schedule, for the callers searching that space (`domain expansion:
// mahoraga`). The zero Schedule is the default pipeline, so this with no
// argument is LoadPipeline(program, true).
func LoadPipelineSchedule(program string, s optimizer.Schedule) (*ir.Pipeline, error) {
	src, err := os.ReadFile(program)
	if err != nil {
		return nil, err
	}
	toks, err := lexer.Lex(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", program, err)
	}
	prog, err := parser.Parse(string(src), toks)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", program, err)
	}
	pipe, err := prims.ResolveWith(prog, prims.FileOptions(program))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", program, err)
	}
	optimizer.OptimizeWith(pipe, s)
	return pipe, nil
}
