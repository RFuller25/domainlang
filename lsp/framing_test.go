package lsp

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestOversizedFrameIsRefusedNotReserved covers the one input that reaches the
// server before any of its own code does: the byte count in the header. It was
// handed straight to make(), so a desynchronized stream — or a client that got
// its arithmetic wrong — asked for a hundred gigabytes, and Go answers that by
// killing the process. No recover sees it and no diagnostic survives it.
func TestOversizedFrameIsRefusedNotReserved(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"absurd", "Content-Length: 99999999999\r\n\r\n{}", "larger than"},
		{"maxint", "Content-Length: 9223372036854775807\r\n\r\n{}", "larger than"},
		{"unparseable", "Content-Length: abc\r\n\r\n{}", "bad Content-Length"},
		{"missing", "\r\n\r\n{}", "missing Content-Length"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Serve(strings.NewReader(c.in), io.Discard, io.Discard)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want %q", err, c.want)
			}
		})
	}
}

// TestLargeButLegalDocumentIsServed puts the limit where a real file cannot
// reach it: a megabyte of program still opens and still gets diagnostics.
func TestLargeButLegalDocumentIsServed(t *testing.T) {
	var b strings.Builder
	b.WriteString("Cursed Energy: in.txt\n")
	for range 20000 {
		b.WriteString("# a comment line, repeated until the document is large\n")
	}
	b.WriteString("Maximum Technique: Sum\nReveal: stdout\n")
	if b.Len() < 1<<20 {
		t.Fatalf("test document is only %d bytes", b.Len())
	}
	results := drive(t, initialize(), didOpen(b.String()))
	findDiagnostics(t, results) // fails the test if none was published
}

// TestServerAnswersAfterAnUnknownMethod keeps the loop honest about requests it
// has no handler for: a null result is a reply, and silence is a hang.
func TestServerAnswersAfterAnUnknownMethod(t *testing.T) {
	var in bytes.Buffer
	for _, m := range []any{
		initialize(),
		req(7, "textDocument/documentSymbol", map[string]any{}),
		req(8, "$/madeUpMethod", map[string]any{"anything": []any{1, 2, 3}}),
	} {
		in.Write(frame(t, m))
	}
	in.Write(frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"}))
	var out bytes.Buffer
	if err := Serve(&in, &out, io.Discard); err != nil {
		t.Fatalf("serve: %v", err)
	}
	for _, id := range []int{7, 8} {
		if !strings.Contains(out.String(), fmt.Sprintf(`"id":%d`, id)) {
			t.Errorf("request %d went unanswered", id)
		}
	}
}
