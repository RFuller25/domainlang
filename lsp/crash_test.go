package lsp

import (
	"strings"
	"testing"
)

// halfWritten is a `range(5, 10)` caught halfway through being typed, inside a
// binding the resolver constant-folds. Every keystroke over this line used to
// panic the server; five restarts later the client gives up and the editor has
// no language support left, which is what made a one-line typo look like a
// dead extension.
const halfWritten = `Cursed Energy: in.txt
Shikigami: Lines
Channeled Energy: Convert List to Integers
Maximum Technique: Count Matching
    Consider n As range(5)
    Using: (x) -> x > n
Reveal: stdout
`

func TestHalfWrittenCallDiagnosesInsteadOfCrashing(t *testing.T) {
	results := drive(t, initialize(), didOpen(halfWritten))
	diags := findDiagnostics(t, results)
	if len(diags) == 0 {
		t.Fatal("no diagnostics for a miscounted call")
	}
	first := diags[0].(map[string]any)
	msg := first["message"].(string)
	if !strings.Contains(msg, "range takes 2 argument(s), got 1") {
		t.Errorf("message = %q, want the arity error", msg)
	}
	if rng := first["range"].(map[string]any); rng["start"].(map[string]any)["line"].(float64) != 4 {
		t.Errorf("arity error should be on 0-based line 4: %v", rng)
	}
}

// TestSessionSurvivesEveryPrefixOfALine types the same line one character at a
// time, the way an editor actually sends it, and requires the session to still
// be answering requests at the end of it.
func TestSessionSurvivesEveryPrefixOfALine(t *testing.T) {
	const line = "    Consider n As range(5, 10)"
	head := "Cursed Energy: in.txt\nShikigami: Lines\nChanneled Energy: Convert List to Integers\nMaximum Technique: Count Matching\n"
	tail := "\n    Using: (x) -> x > n\nReveal: stdout\n"

	msgs := []any{initialize(), didOpen(head + tail)}
	for i := range len(line) + 1 {
		msgs = append(msgs, map[string]any{
			"jsonrpc": "2.0", "method": "textDocument/didChange",
			"params": map[string]any{
				"textDocument":   map[string]any{"uri": uri},
				"contentChanges": []any{map[string]any{"text": head + line[:i] + tail}},
			},
		})
	}
	// A request at the end: a server that died mid-typing never answers it.
	msgs = append(msgs, map[string]any{
		"jsonrpc": "2.0", "id": 99, "method": "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 1, "character": 0},
		},
	})
	results := drive(t, msgs...)
	var answered bool
	for _, r := range results {
		if id, ok := r["id"].(float64); ok && id == 99 {
			answered = true
		}
	}
	if !answered {
		t.Fatal("server stopped answering after typing the line out")
	}
}
