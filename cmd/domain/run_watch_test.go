package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a buffer a watched run can write to while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestParseRunArgsWatch(t *testing.T) {
	_, opts, err := parseRunArgs([]string{"p.domain", "--watch", "--release"})
	if err != nil || !opts.Watch || !opts.Release {
		t.Errorf("opts %+v, err %v", opts, err)
	}
	if _, opts, _ := parseRunArgs([]string{"-w", "p.domain"}); !opts.Watch {
		t.Error("-w should set Watch")
	}
	if _, _, err := parseRunArgs([]string{"p.domain", "--watch", "--record", "x"}); err == nil {
		t.Error("--watch with --record should be refused")
	}
	got := withoutWatchFlag([]string{"--release", "p.domain", "--watch", "--quiet", "-w"}, "p.domain")
	if strings.Join(got, " ") != "--release --quiet" {
		t.Errorf("withoutWatchFlag = %v", got)
	}
}

// The watch set is the program and every file it reads, resolved the way a
// run resolves them; a program that does not resolve watches only itself.
func TestWatchedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.domain")
	writeFile(t, path, "Cursed Energy: data/in.txt\nMaximum Technique: Count Characters\nReveal: stdout\n")
	if got := watchedFiles(path); len(got) != 1 || got[0] != path {
		t.Errorf("an unresolvable program: %v", got)
	}
	writeFile(t, path, "Cursed Energy: data/in.txt\nShikigami: Lines\nMaximum Technique: Count\nReveal: stdout\n")
	want := []string{path, filepath.Join(dir, "data", "in.txt")}
	if got := watchedFiles(path); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("watchedFiles = %v, want %v", got, want)
	}
	writeFile(t, path, "Cursed Energy: stdin\nShikigami: Lines\nMaximum Technique: Count\nReveal: stdout\n")
	if got := watchedFiles(path); len(got) != 1 {
		t.Errorf("a program reading stdin: %v", got)
	}
}

// The loop itself: a change to the input runs the program again, a change to
// the program runs the new one, and a run that never ends is stopped by the
// next save rather than waited for.
func TestRunWatchLoop(t *testing.T) {
	useRealDomainBinary(t)
	old := runWatchInterval
	runWatchInterval = 20 * time.Millisecond
	defer func() { runWatchInterval = old }()

	dir := t.TempDir()
	path := filepath.Join(dir, "p.domain")
	input := filepath.Join(dir, "in.txt")
	const sum = "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout\n"
	writeFile(t, path, sum)
	writeFile(t, input, "1\n2\n")

	var out, errOut syncBuffer
	w := &runWatcher{bin: measureDomainBin, path: path, flags: []string{"--quiet"}, stdout: &out, stderr: &errOut}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() { w.loop(ctx); close(finished) }()

	waitFor(t, "the first run", func() bool { return out.String() == "3\n" })
	waitFor(t, "the first report", func() bool { return strings.Contains(errOut.String(), "watching p.domain, in.txt") })

	// The file's size changes too, so the edit is seen whatever the
	// filesystem's timestamp resolution.
	writeFile(t, input, "1\n2\n10\n")
	waitFor(t, "a re-run on the new input", func() bool { return out.String() == "3\n13\n" })

	// A program that never finishes…
	writeFile(t, path, "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Sum\n"+
		"Simple Domain: While\n    Using: (v) -> v > 0\n    Cursed Technique: Apply\n        Using: (v) -> v + 1\nReveal: stdout\n")
	time.Sleep(200 * time.Millisecond)
	// …is abandoned for the next version.
	writeFile(t, path, "Cursed Energy: in.txt\nShikigami: Ints\nMaximum Technique: Max\nReveal: stdout\n")
	waitFor(t, "the run after the endless one", func() bool { return out.String() == "3\n13\n10\n" })
	if !strings.Contains(errOut.String(), "stopped the run that was still going") {
		t.Errorf("stderr never says the endless run was stopped:\n%s", errOut.String())
	}

	// A failing program is reported and watched on, not fatal.
	writeFile(t, path, "Cursed Energy: in.txt\nMaximum Technique: Nonsense\n")
	waitFor(t, "the failure report", func() bool { return strings.Contains(errOut.String(), "[watch] exit 1") })

	cancel()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the loop did not stop when its context ended")
	}
}
