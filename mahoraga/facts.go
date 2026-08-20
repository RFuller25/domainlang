package mahoraga

// What the search knows about the input, and about the program built for it.
//
// This is the reconnaissance the catalogue is built on. Every entry in
// catalogue.go is a precondition over these facts plus the Go the compiler
// actually emitted, and both halves matter: a fact says the *input* permits an
// adaptation, and the emitted source says the *program* has anywhere to apply
// it. An entry that skipped the second check would spend a compile and a
// measurement proving that changing nothing changes nothing.
//
// Reading the emitted Go to decide what is worth trying is not a shortcut
// around the IR — it is the only place some of these questions have an answer.
// Whether the fused split-and-parse loop reserved a guessed capacity, or
// whether a grid builder decodes UTF-8, are facts about the *backend's*
// choices, and the backend is what the search is tuning.

import (
	"os"
	"strings"
)

// Facts are the measured shape of the input and of the baseline run.
//
// Nothing here is or could be the answer: they are counts, ranges and yes/no
// questions about the input's encoding, never its contents. That is what keeps
// "print the expected output" unreachable rather than merely rejected.
type Facts struct {
	// Bytes and Segments describe the input file: its size, and how many
	// newline-separated pieces `Split Text by "\n"` would produce from it after
	// the trailing newline is trimmed, which is the number a capacity hint
	// wants.
	Bytes    int64
	Segments int

	// ASCII is true when every byte of the input is below 0x80, so a rune index
	// is a byte index everywhere in the program.
	ASCII bool

	// LongestLine is the longest segment, in bytes.
	LongestLine int

	// HeapReported says the baseline binary wrote its allocation figures, which
	// every Domain binary can (see runner/alloc.go). Without it the collector
	// entries have nothing to reason from and stand down.
	HeapReported bool
	// HeapSys is bytes of heap the baseline obtained from the OS, and NumGC how
	// many collections it ran. A program that never collected has nothing to
	// win by collecting less.
	HeapSys    uint64
	TotalAlloc uint64
	NumGC      uint32

	// Constants are the `Consider` bindings a probe build watched hold one
	// value for an entire run. See probe.go: they are the only facts here
	// measured from inside the program rather than from the input file, and
	// the only ones a *different* input could change without changing the
	// file's shape at all.
	Constants []Constant

	// ListSites are the list accumulators the same probe watched grow — the
	// ones the generator reserves nothing for because nothing in scope
	// predicts their length.
	ListSites []ListSite
}

// readFacts measures an input file.
func readFacts(path string) (Facts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Facts{}, err
	}
	f := Facts{Bytes: int64(len(data)), ASCII: true}

	// The segment count is what the program will actually see: dmReadSource
	// trims trailing newlines, then the split produces one more piece than
	// there are separators in what is left. An empty input is one empty piece,
	// which is what strings.Split does and what the interpreter does.
	trimmed := strings.TrimRight(string(data), "\r\n")
	f.Segments = 1
	line := 0
	for i := range len(trimmed) {
		c := trimmed[i]
		if c >= 0x80 {
			f.ASCII = false
		}
		if c == '\n' {
			f.Segments++
			if line > f.LongestLine {
				f.LongestLine = line
			}
			line = 0
			continue
		}
		line++
	}
	if line > f.LongestLine {
		f.LongestLine = line
	}
	// Scanning the trimmed text is enough for the ASCII question: the only
	// bytes the trim removed were newlines and carriage returns, both ASCII.
	return f, nil
}
