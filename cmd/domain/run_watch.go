// `domain run <file> --watch` — run again on every save.
//
// The stepper has had `--watch` and the REPL `:watch` for as long as either has
// existed; the plain run, which is what an Advent of Code afternoon mostly is,
// had neither. This is the loop itself: edit the program or its input in one
// window, and the answer in the other is always the current one.
//
// Each run is a child process rather than a call into the interpreter. A
// program that does not terminate is the ordinary state of a program being
// written, and the only reliable way to abandon one the moment a newer version
// is saved is to kill it. It also means a watched run is exactly `domain run`:
// same flags, same output, same exit status, nothing reimplemented.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"domain/ir"
	"domain/prims"
	"domain/runner"
)

// runWatchInterval is how often the watched files are checked — the same
// poll-and-compare `:watch` uses, for the reasons repl_watch.go gives. A
// variable so tests do not have to wait for it.
var runWatchInterval = 300 * time.Millisecond

// runWatcher is one watching session.
type runWatcher struct {
	bin    string   // the domain binary each run executes
	path   string   // the program
	flags  []string // the run's own flags, --watch removed
	stdin  []byte   // what was piped in, replayed to every run
	stdout io.Writer
	stderr io.Writer
	clear  bool // wipe the screen before each run (a terminal is watching)
}

// RunWatch is the CLI entry: it watches until interrupted.
func RunWatch(path string, flags []string, stdin *os.File, stdout, stderr io.Writer) int {
	bin := measureDomainBin
	if bin == "" {
		var err error
		if bin, err = os.Executable(); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
	}
	w := &runWatcher{bin: bin, path: path, flags: flags, stdout: stdout, stderr: stderr, clear: isColorTerminal(stdout)}
	// Piped input is read once and replayed: a pipe can only be drained by the
	// first run, and every run after it would otherwise see an empty one. A
	// terminal is not read at all — there is nobody on the other end of it
	// that a re-run could ask twice.
	if fi, err := stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		b, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "domain: reading stdin: %v\n", err)
			return 1
		}
		w.stdin = b
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	w.loop(ctx)
	return 0
}

// watchedFiles is the program plus every file it reads: each `Cursed Energy:`
// target, resolved against the program's directory the way a run resolves it.
// A program that does not resolve watches only itself — which is the file that
// has to change for it to resolve again.
func watchedFiles(path string) []string {
	files := []string{path}
	pipe, err := runner.LoadPipeline(path, false)
	if err != nil {
		return files
	}
	seen := map[string]bool{path: true}
	prims.WalkNodes(pipe, func(n *ir.Node) {
		if n.Prim != "Read Source" {
			return
		}
		target, _ := n.Meta["target"].(string)
		if target == "" || strings.EqualFold(target, "stdin") {
			return
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		if !seen[target] {
			seen[target] = true
			files = append(files, target)
		}
	})
	return files
}

// loop runs the program, waits for a watched file to change, and runs it
// again, until ctx ends. A change that lands while a run is still going kills
// that run: its answer is already about a program that no longer exists.
func (w *runWatcher) loop(ctx context.Context) {
	for ctx.Err() == nil {
		// The set is rebuilt each time, before the run starts: an edit may
		// have pointed the program at a different input, and a save made
		// while the run is going must still be noticed.
		files := watchedFiles(w.path)
		watch := newVisWatch(files...)

		if w.clear {
			fmt.Fprint(w.stdout, "\x1b[2J\x1b[3J\x1b[H")
		}
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		start := time.Now()
		cmd := exec.CommandContext(runCtx, w.bin, append([]string{"run", w.path}, w.flags...)...)
		cmd.Stdin = bytes.NewReader(w.stdin)
		cmd.Stdout, cmd.Stderr = w.stdout, w.stderr
		go func() { done <- cmd.Run() }()

		finished, changed := false, ""
		tick := time.NewTicker(runWatchInterval)
		for changed == "" && ctx.Err() == nil {
			select {
			case err := <-done:
				done = nil // a nil channel never fires again
				finished = true
				w.report(err, time.Since(start), files)
			case <-tick.C:
				changed, _ = watch.changed()
			case <-ctx.Done():
			}
		}
		tick.Stop()
		cancel()
		if !finished {
			<-done
			if changed != "" {
				fmt.Fprintln(w.stderr, "[watch] stopped the run that was still going")
			}
		}
		if changed != "" && !w.clear {
			fmt.Fprintf(w.stderr, "[watch] %s changed\n", filepath.Base(changed))
		}
	}
}

// report is the line under a finished run: how it ended, how long it took, and
// what a change to would run it again.
func (w *runWatcher) report(err error, took time.Duration, files []string) {
	status := "ok"
	if err != nil {
		status = "failed"
		if ee, ok := err.(*exec.ExitError); ok {
			status = fmt.Sprintf("exit %d", ee.ExitCode())
		}
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	sort.Strings(names[1:]) // the program first, its inputs in a stable order
	fmt.Fprintf(w.stderr, "[watch] %s in %s · watching %s — ctrl+c stops\n",
		status, took.Round(time.Millisecond), strings.Join(names, ", "))
}
