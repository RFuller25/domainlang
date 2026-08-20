package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// frame encodes one JSON-RPC message with Content-Length framing.
func frame(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))), body...)
}

// drive runs one server session over the given messages and decodes every
// message the server wrote back.
func drive(t *testing.T, msgs ...any) []map[string]any {
	t.Helper()
	var in bytes.Buffer
	for _, m := range msgs {
		in.Write(frame(t, m))
	}
	in.Write(frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"}))
	var out bytes.Buffer
	if err := Serve(&in, &out, io.Discard); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var results []map[string]any
	rest := out.Bytes()
	for len(rest) > 0 {
		sep := bytes.Index(rest, []byte("\r\n\r\n"))
		if sep < 0 {
			t.Fatalf("unframed trailing output: %q", rest)
		}
		var n int
		if _, err := fmt.Sscanf(string(rest[:sep]), "Content-Length: %d", &n); err != nil {
			t.Fatalf("bad header %q", rest[:sep])
		}
		var msg map[string]any
		if err := json.Unmarshal(rest[sep+4:sep+4+n], &msg); err != nil {
			t.Fatal(err)
		}
		results = append(results, msg)
		rest = rest[sep+4+n:]
	}
	return results
}

const uri = "file:///t/prog.domain"

func didOpen(text string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri, "text": text}},
	}
}

func initialize() map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}}
}

// findDiagnostics returns the diagnostics of the last publishDiagnostics.
func findDiagnostics(t *testing.T, results []map[string]any) []any {
	t.Helper()
	var diags []any
	found := false
	for _, r := range results {
		if r["method"] == "textDocument/publishDiagnostics" {
			params := r["params"].(map[string]any)
			diags = params["diagnostics"].([]any)
			found = true
		}
	}
	if !found {
		t.Fatal("no publishDiagnostics received")
	}
	return diags
}

func resultOf(t *testing.T, results []map[string]any, id float64) any {
	t.Helper()
	for _, r := range results {
		if got, ok := r["id"].(float64); ok && got == id {
			return r["result"]
		}
	}
	t.Fatalf("no response with id %v", id)
	return nil
}

func TestInitializeAdvertisesCapabilities(t *testing.T) {
	results := drive(t, initialize())
	caps := resultOf(t, results, 1).(map[string]any)["capabilities"].(map[string]any)
	for _, want := range []string{"hoverProvider", "definitionProvider", "codeActionProvider"} {
		if caps[want] != true {
			t.Errorf("capability %s missing: %v", want, caps)
		}
	}
	if caps["textDocumentSync"].(float64) != 1 {
		t.Errorf("want full sync, got %v", caps["textDocumentSync"])
	}
}

func TestDidOpenPublishesRichDiagnostics(t *testing.T) {
	src := "Cursed Energy: in.txt\nCursed Tecnique: Split Text by \"\\n\"\nReveal stdout\n"
	results := drive(t, initialize(), didOpen(src))
	diags := findDiagnostics(t, results)
	if len(diags) < 2 {
		t.Fatalf("want the typo and the missing colon, got %v", diags)
	}
	first := diags[0].(map[string]any)
	if first["severity"].(float64) != 1 || first["source"] != "domain" {
		t.Errorf("bad severity/source: %v", first)
	}
	msg := first["message"].(string)
	if !strings.Contains(msg, "Cursed Tecnique") || !strings.Contains(msg, `did you mean "Cursed Technique"`) {
		t.Errorf("message lost the suggestion: %q", msg)
	}
	rng := first["range"].(map[string]any)
	if rng["start"].(map[string]any)["line"].(float64) != 1 {
		t.Errorf("typo should be on 0-based line 1: %v", rng)
	}
}

func TestHoverShowsPipelineTypes(t *testing.T) {
	src := "Cursed Energy: in.txt\nCursed Technique: Split Text by \"\\n\"\nChanneled Energy: Convert To Integers\nMaximum Technique: Sum\nReveal: stdout\n"
	hover := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 3, "character": 0},
		},
	}
	results := drive(t, initialize(), didOpen(src), hover)
	res := resultOf(t, results, 2)
	if res == nil {
		t.Fatal("hover returned null on a clean program")
	}
	val := res.(map[string]any)["contents"].(map[string]any)["value"].(string)
	if !strings.Contains(val, "List<Int> → Int") {
		t.Errorf("hover should show the Sum signature, got %q", val)
	}
}

// Hovering a *name* answers about the name rather than about the line it sits
// on, and the column it arrives as is a UTF-16 offset — which is the same
// number as the byte offset until something outside the BMP is on the line, so
// one of these puts an emoji in front of the word.
func TestHoverOverAVariableSaysWhereItComesFrom(t *testing.T) {
	src := "Cursed Object: target As 2020\n" +
		"Cursed Energy: in.txt\n" +
		"Cursed Technique: Map Each\n" +
		"    Using: (n) -> n + target    # 🎯 target again\n" +
		"Cursed Tool: target As target + 1\n" +
		"Reveal: stdout\n"
	hover := func(id, line, character int) map[string]any {
		return map[string]any{
			"jsonrpc": "2.0", "id": id, "method": "textDocument/hover",
			"params": map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"position":     map[string]any{"line": line, "character": character},
			},
		}
	}
	// 0-based line 3, on the `target` of `n + target`.
	results := drive(t, initialize(), didOpen(src), hover(2, 3, 22))
	res := resultOf(t, results, 2)
	if res == nil {
		t.Fatal("hover returned null over a global")
	}
	val := res.(map[string]any)["contents"].(map[string]any)["value"].(string)
	for _, want := range []string{"**target**", "`Int`", "declared on line 1", "written on line 5"} {
		if !strings.Contains(val, want) {
			t.Errorf("hover over a global is missing %q, got %q", want, val)
		}
	}

	// The second `target` on that line is inside a comment, and past an emoji:
	// the character offset counts two UTF-16 units for it, so answering by byte
	// offset alone would land in the wrong place.
	// The comment's `target` starts at byte 39 and at UTF-16 offset 37.
	results = drive(t, initialize(), didOpen(src), hover(2, 3, 37))
	if res := resultOf(t, results, 2); res != nil {
		val := res.(map[string]any)["contents"].(map[string]any)["value"].(string)
		if strings.Contains(val, "**target**") {
			t.Errorf("hover answered about a name inside a comment: %q", val)
		}
	}
}

func TestDefinitionJumpsToShikigami(t *testing.T) {
	src := "Shikigami \"Halve\"\n    Cursed Technique: Map Each\n        Using: (x) -> x / 2\nCursed Energy: in.txt\nShikigami: Ints\nShikigami: Halve\nReveal: stdout\n"
	def := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/definition",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 5, "character": 3},
		},
	}
	results := drive(t, initialize(), didOpen(src), def)
	res := resultOf(t, results, 2)
	if res == nil {
		t.Fatal("definition returned null")
	}
	loc := res.(map[string]any)
	line := loc["range"].(map[string]any)["start"].(map[string]any)["line"].(float64)
	if loc["uri"] != uri || line != 0 {
		t.Errorf("definition should point at line 0, got %v", loc)
	}
	// A prelude Shikigami has no file definition: expect null, not a bogus jump.
	def4 := map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "textDocument/definition",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 4, "character": 3},
		},
	}
	results = drive(t, initialize(), didOpen(src), def4)
	if res := resultOf(t, results, 3); res != nil {
		t.Errorf("prelude definition should be null, got %v", res)
	}
}

func TestCodeActionAppliesEveryConfidentFix(t *testing.T) {
	src := "Cursed Energy: in.txt\nCursed Tecnique: Split Text by \"\\n\"\nReveal stdout\n"
	action := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/codeAction",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"range":        map[string]any{},
		},
	}
	results := drive(t, initialize(), didOpen(src), action)
	actions := resultOf(t, results, 2).([]any)
	if len(actions) != 1 {
		t.Fatalf("want one fix-all action, got %v", actions)
	}
	a := actions[0].(map[string]any)
	if a["kind"] != "quickfix" {
		t.Errorf("kind = %v", a["kind"])
	}
	edits := a["edit"].(map[string]any)["changes"].(map[string]any)[uri].([]any)
	newText := edits[0].(map[string]any)["newText"].(string)
	if !strings.Contains(newText, "Cursed Technique: Split") || !strings.Contains(newText, "Reveal: stdout") {
		t.Errorf("fixed text incomplete:\n%s", newText)
	}
}

func TestInitializeAdvertisesCompletion(t *testing.T) {
	results := drive(t, initialize())
	caps := resultOf(t, results, 1).(map[string]any)["capabilities"].(map[string]any)
	comp, ok := caps["completionProvider"].(map[string]any)
	if !ok {
		t.Fatalf("completionProvider missing: %v", caps)
	}
	trigs := comp["triggerCharacters"].([]any)
	if len(trigs) == 0 || trigs[0] != ":" {
		t.Errorf("expected ':' trigger, got %v", trigs)
	}
}

func TestHoverShowsPrimitiveDocumentation(t *testing.T) {
	src := "Cursed Energy: in.txt\nCursed Technique: Split Text by \"\\n\"\nChanneled Energy: Convert To Integers\nMaximum Technique: Sum\nReveal: stdout\n"
	hover := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 3, "character": 4},
		},
	}
	results := drive(t, initialize(), didOpen(src), hover)
	val := resultOf(t, results, 2).(map[string]any)["contents"].(map[string]any)["value"].(string)
	for _, want := range []string{"Maximum Technique: Sum", "List<Int> → Int", "Sum of all elements", "primitives.md#sum"} {
		if !strings.Contains(val, want) {
			t.Errorf("hover missing %q:\n%s", want, val)
		}
	}
}

func TestHoverDocumentsPrimitiveEvenWhenProgramDoesNotTypeCheck(t *testing.T) {
	// A type error upstream (Sum wants List<Int>, gets List<Text>) means the
	// pipeline never resolves — but the primitive on the line is still known.
	src := "Cursed Energy: in.txt\nCursed Technique: Split Text by \"\\n\"\nMaximum Technique: Sum\nReveal: stdout\n"
	hover := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 2, "character": 4},
		},
	}
	results := drive(t, initialize(), didOpen(src), hover)
	res := resultOf(t, results, 2)
	if res == nil {
		t.Fatal("hover should still document the primitive on a non-resolving program")
	}
	val := res.(map[string]any)["contents"].(map[string]any)["value"].(string)
	if !strings.Contains(val, "Maximum Technique: Sum") {
		t.Errorf("hover lost the primitive doc:\n%s", val)
	}
}

func TestCompletionOffersKeywordsPrimitivesAndArgs(t *testing.T) {
	// At the head of a fresh line: statement keywords.
	kw := labelsOf(CompletionItems(""))
	if !kw["Cursed Technique"] || !kw["Domain Expansion"] || !kw["Reveal"] {
		t.Errorf("head-of-line completion missing statement keywords: %v", keys(kw))
	}

	// After a keyword and colon: that keyword's primitives, and nothing from
	// another class.
	ops := labelsOf(CompletionItems("Domain Expansion: "))
	if !ops["BFS"] || !ops["Dijkstra"] || !ops["Sort"] {
		t.Errorf("operation completion missing Domain Expansion primitives: %v", keys(ops))
	}
	if ops["Filter"] || ops["Sum"] {
		t.Errorf("operation completion leaked another keyword's primitives: %v", keys(ops))
	}

	// Indented continuation line: argument labels.
	args := labelsOf(CompletionItems("    "))
	if !args["Using:"] || !args["Mode:"] {
		t.Errorf("indented completion missing argument labels: %v", keys(args))
	}

	// After Mode:, its enum values.
	modes := labelsOf(CompletionItems("    Mode: "))
	if !modes["Each"] || !modes["First"] {
		t.Errorf("Mode value completion missing: %v", keys(modes))
	}
}

func TestCompletionEndToEnd(t *testing.T) {
	src := "Cursed Energy: in.txt\nMaximum Technique: \n"
	comp := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 1, "character": 18},
		},
	}
	results := drive(t, initialize(), didOpen(src), comp)
	res := resultOf(t, results, 2).(map[string]any)
	items := res["items"].([]any)
	found := false
	for _, it := range items {
		m := it.(map[string]any)
		if m["label"] == "Group By" {
			found = true
			if !strings.Contains(m["detail"].(string), "→") {
				t.Errorf("primitive item missing signature detail: %v", m)
			}
		}
	}
	if !found {
		t.Errorf("completion did not offer Maximum Technique primitives: %v", items)
	}
}

func labelsOf(items []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		out[it["label"].(string)] = true
	}
	return out
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestLinePrefixTreatsCharacterAsUTF16Offset(t *testing.T) {
	// "café " is 5 UTF-16 units but 6 bytes; a cursor after the space (unit 5)
	// must take the whole prefix, not stop one byte short inside it.
	text := "café Mode\n"
	if got := linePrefix(text, 0, 5); got != "café " {
		t.Errorf("BMP non-ASCII: got %q, want %q", got, "café ")
	}
	// "😀" is one rune, 2 UTF-16 units, 4 bytes. Unit offset 2 is after the
	// emoji (byte 4); the old byte-based slice would cut it mid-rune.
	text = "😀Mode\n"
	if got := linePrefix(text, 0, 2); got != "😀" {
		t.Errorf("astral rune: got %q, want %q", got, "😀")
	}
	// An offset landing between surrogate halves must stay on a rune boundary.
	if got := linePrefix(text, 0, 1); got != "😀" {
		t.Errorf("mid-surrogate: got %q, want %q", got, "😀")
	}
	// Clamping past the end of the line and negative offsets still work.
	if got := linePrefix(text, 0, 99); got != "😀Mode" {
		t.Errorf("clamp: got %q, want %q", got, "😀Mode")
	}
	if got := linePrefix(text, 0, -1); got != "" {
		t.Errorf("negative: got %q, want %q", got, "")
	}
}

func TestDidChangeReplacesAndDidCloseClears(t *testing.T) {
	change := map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didChange",
		"params": map[string]any{
			"textDocument":   map[string]any{"uri": uri},
			"contentChanges": []any{map[string]any{"text": "Cursed Energy: in.txt\nReveal: stdout\n"}},
		},
	}
	closeMsg := map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didClose",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri}},
	}
	results := drive(t, initialize(), didOpen("Reveal stdout\n"), change, closeMsg)
	// The last publish (didClose) must be empty.
	if diags := findDiagnostics(t, results); len(diags) != 0 {
		t.Errorf("didClose should clear diagnostics, got %v", diags)
	}
}
