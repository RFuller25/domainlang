package ir

import (
	"testing"

	"domain/token"
)

// A foreign block reports its runtime's whole output — a traceback, a compile
// error — so a RuntimeError message can now run to several lines. The stage tag
// belongs on the first line, not dangling after the last line of somebody
// else's output, and every single-line message must render exactly as before.
func TestRuntimeErrorMultiline(t *testing.T) {
	single := &RuntimeError{Prim: "Sum", Pos: token.Position{Line: 3, Col: 1},
		Msg: "Sum of an empty list is undefined"}
	if got, want := single.Error(), "3:1: Sum of an empty list is undefined (in Sum)"; got != want {
		t.Errorf("single-line error changed:\ngot  %q\nwant %q", got, want)
	}

	multi := &RuntimeError{Prim: "Foreign Block", Pos: token.Position{Line: 4, Col: 1},
		Msg: "the Python block failed with status 1\nTraceback:\n  boom"}
	want := "4:1: the Python block failed with status 1 (in Foreign Block)\nTraceback:\n  boom"
	if got := multi.Error(); got != want {
		t.Errorf("multi-line error:\ngot  %q\nwant %q", got, want)
	}
}
