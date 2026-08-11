package lsp

import (
	"strings"
	"testing"
)

// hostile is every buffer that has been found to end the server rather than be
// diagnosed. Each one is a thing a person could type or paste, and each one
// used to take the process with it — three of them fatally, past any recover:
// a stack Go grows until it gives up, and two allocations Go cannot serve.
//
// They are asserted as a session rather than a unit: opening the document is
// what an editor does, and answering afterwards is what the server has to keep
// doing.
var hostile = []struct{ name, text string }{
	{"miscounted call", halfWritten},
	{"keyword with a block but no operation", "Cursed Energy:\n    Sum\n"},
	{"a fold that would build terabytes", consider("fill(1099511627776, 0)")},
	{"a fold that would count to a hundred million", consider("length(range(0, 100000000))")},
	{"a fold that would pad to the end of Int", consider("padleft(\"1\", 9223372036854775807, \"0\")")},
	{"a fold that would factor a 19-digit number", consider("divisors(9223372036854775783)")},
	{"a fold that divides by zero", consider("1 / 0")},
	{"a fold off the end of a list", consider("item(list(), 0)")},
	{"nesting deeper than the stack", consider(strings.Repeat("abs(", 100000) + "1" + strings.Repeat(")", 100000))},
	{"an operator chain longer than the stack", consider("1" + strings.Repeat(" + 1", 500000))},
	{"an empty document", ""},
	{"one space", " "},
	{"a lone colon", ":\n"},
	{"nothing but indentation", "\n\n    \n\t\n"},
}

func consider(expr string) string {
	return `Cursed Energy: in.txt
Shikigami: Lines
Channeled Energy: Convert List to Integers
Maximum Technique: Count Matching
    Consider n As ` + expr + `
    Using: (x) -> x > n
Reveal: stdout
`
}

// TestHostileDocumentsAreServed opens each one and then asks the server for
// everything an editor asks for. The test passing at all is most of the point —
// a server that died takes the test binary with it — but a reply to every
// request is what the editor needs, so that is what is checked.
func TestHostileDocumentsAreServed(t *testing.T) {
	for _, c := range hostile {
		t.Run(c.name, func(t *testing.T) {
			at := map[string]any{"line": 4, "character": 20}
			results := drive(t,
				initialize(),
				didOpen(c.text),
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
			)
			for _, id := range []float64{2, 3, 4, 5, 6} {
				resultOf(t, results, id)
			}
		})
	}
}

// TestHostileDocumentsDiagnoseRatherThanReportABug checks the quality of the
// answer, not just its existence: these are errors in a program, and the
// message has to say so. "internal error" would mean the guard caught a crash
// — which keeps the session alive, but is a bug report, not a diagnostic.
func TestHostileDocumentsDiagnoseRatherThanReportABug(t *testing.T) {
	for _, c := range hostile {
		t.Run(c.name, func(t *testing.T) {
			results := drive(t, initialize(), didOpen(c.text))
			for _, d := range findDiagnostics(t, results) {
				msg := d.(map[string]any)["message"].(string)
				if strings.Contains(msg, "internal error") {
					t.Errorf("crash reported as a diagnostic: %s", msg)
				}
			}
		})
	}
}
