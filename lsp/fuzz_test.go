package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// FuzzServe feeds arbitrary bytes to the session loop as a client would: the
// framing, the JSON, the method names and the parameters are all the fuzzer's
// to choose. Serve may return an error — a desynchronized stream is not
// recoverable and saying so is the honest answer — but it must return.
func FuzzServe(f *testing.F) {
	var seed bytes.Buffer
	for _, m := range []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen",
			"params": map[string]any{"textDocument": map[string]any{"uri": uri, "text": halfWritten}}},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover",
			"params": map[string]any{"textDocument": map[string]any{"uri": uri},
				"position": map[string]any{"line": 1, "character": 0}}},
	} {
		body, _ := json.Marshal(m)
		fmt.Fprintf(&seed, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	f.Add(seed.Bytes())
	f.Add([]byte("Content-Length: 2\r\n\r\n{}"))
	f.Add([]byte("Content-Length: 99999999999\r\n\r\n{}"))
	f.Add([]byte("Content-Length: -1\r\n\r\n"))
	f.Add([]byte("Content-Length: abc\r\n\r\n"))
	f.Add([]byte("\r\n\r\n"))

	f.Fuzz(func(t *testing.T, in []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Serve panicked on %q: %v", in, r)
			}
		}()
		_ = Serve(bytes.NewReader(in), io.Discard, io.Discard)
	})
}

// FuzzRequests holds the framing fixed and fuzzes what an editor actually
// varies: the buffer's text and where the cursor is in it. Every request that
// takes a position gets asked at that position, including positions the
// document does not have.
func FuzzRequests(f *testing.F) {
	f.Add(halfWritten, 4, 20)
	f.Add(halfWritten, -1, -1)
	f.Add(halfWritten, 1<<30, 1<<30)
	f.Add("", 0, 0)
	f.Add("Cursed Energy: in.txt\nReveal: stdout\n", 1, 3)
	f.Add("Consider n As range(5)\n", 0, 12)
	f.Add("Cursed Energy: 日本\nReveal: stdout\n", 0, 16)

	f.Fuzz(func(t *testing.T, text string, line, char int) {
		at := map[string]any{"line": line, "character": char}
		msgs := []any{
			initialize(),
			didOpen(text),
			req(2, "textDocument/hover", map[string]any{
				"textDocument": map[string]any{"uri": uri}, "position": at}),
			req(3, "textDocument/completion", map[string]any{
				"textDocument": map[string]any{"uri": uri}, "position": at}),
			req(4, "textDocument/definition", map[string]any{
				"textDocument": map[string]any{"uri": uri}, "position": at}),
			req(5, "textDocument/codeAction", map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"range":        map[string]any{"start": at, "end": at}}),
			req(6, "textDocument/inlayHint", map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"range":        map[string]any{"start": at, "end": at}}),
		}
		var in bytes.Buffer
		for _, m := range msgs {
			body, err := json.Marshal(m)
			if err != nil {
				t.Skip() // a text the protocol cannot carry is not a server bug
			}
			fmt.Fprintf(&in, "Content-Length: %d\r\n\r\n%s", len(body), body)
		}
		var out bytes.Buffer
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panicked on %q at %d:%d: %v", text, line, char, r)
			}
		}()
		if err := Serve(&in, &out, io.Discard); err != nil {
			t.Fatalf("serve returned %v on well-framed messages", err)
		}
		// Every request must be answered, however odd the position: a client
		// that never gets a reply is a client that hangs.
		for _, id := range []string{`"id":2`, `"id":3`, `"id":4`, `"id":5`, `"id":6`} {
			if !strings.Contains(out.String(), id) {
				t.Fatalf("no reply with %s for %q at %d:%d", id, text, line, char)
			}
		}
	})
}

// req builds one JSON-RPC request message.
func req(id int, method string, params map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}
