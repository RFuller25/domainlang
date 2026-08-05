package ir

import "time"

// ForeignRun is one execution of a foreign-language block: the command that
// ran, the bytes that crossed in each direction, and how it went.
//
// It lives here, in the shared value model, for the reason ir.StepEvent does:
// the side that produces it (prims, where the subprocess runs) and the side
// that consumes it (a recorder, a profiler, a debugger) must not have to import
// each other to agree on its shape.
//
// The strings are what actually crossed the wire, which is the whole point of
// capturing them. A foreign stage is the one place in a Domain program where
// the value stops being a value and becomes bytes, and the mistakes live in
// that translation: a trailing newline that was or was not there, a grid whose
// rows were not what the block expected, an empty list that arrived as no
// input at all. A reader shown only the Domain value on each side can see that
// the answer is wrong and never see why.
//
// They may be truncated by whoever captured them — a whole AoC input is a
// megabyte and a display has no use for the tail of it — so each carries the
// full length beside it.
type ForeignRun struct {
	Lang    string // canonical language name
	Command string // the resolved command line, as run
	Stdin   Capture
	Stdout  Capture
	Stderr  Capture
	Err     error         // nil when the block succeeded
	Dur     time.Duration // wall time of the subprocess
}

// Capture is a possibly-truncated copy of one stream, with the size it was
// truncated from.
type Capture struct {
	Text  string
	Bytes int // the stream's full length, before any truncation
}

// Truncated reports whether Text is short of the whole stream.
func (c Capture) Truncated() bool { return len(c.Text) < c.Bytes }

// CaptureText builds a Capture, keeping at most max bytes and cutting on a rune
// boundary so a truncated stream is still printable.
func CaptureText(s string, max int) Capture {
	c := Capture{Text: s, Bytes: len(s)}
	if max <= 0 || len(s) <= max {
		return c
	}
	cut := max
	// Back off to the start of the rune the cut landed inside, if any.
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	c.Text = s[:cut]
	return c
}
