package lsp

import (
	"strings"
	"testing"
)

// inlayRequest builds a textDocument/inlayHint request for the whole document.
func inlayRequest(id int) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "textDocument/inlayHint",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"range": map[string]any{
				"start": map[string]any{"line": 0, "character": 0},
				"end":   map[string]any{"line": 500, "character": 0},
			},
		},
	}
}

// hintsFor drives a session and returns the hints as line → label.
func hintsFor(t *testing.T, src string) map[int]string {
	t.Helper()
	results := drive(t, initialize(), didOpen(src), inlayRequest(2))
	res := resultOf(t, results, 2)
	if res == nil {
		return nil
	}
	list, ok := res.([]any)
	if !ok {
		t.Fatalf("inlayHint result is %T, want a list", res)
	}
	out := map[int]string{}
	for _, h := range list {
		m := h.(map[string]any)
		pos := m["position"].(map[string]any)
		line := int(pos["line"].(float64))
		out[line] = m["label"].(string)
	}
	return out
}

func TestInlayHintsShowStageTypes(t *testing.T) {
	src := "Cursed Energy: in.txt\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	hints := hintsFor(t, src)
	want := map[int]string{
		0: ": Text",
		1: ": List<Text>",
		2: ": List<Int>",
		3: ": Int",
	}
	for line, label := range want {
		if hints[line] != label {
			t.Errorf("line %d hint = %q, want %q (all: %v)", line, hints[line], label, hints)
		}
	}
}

// A hint is anchored at end of line, so the editor renders it after the code.
func TestInlayHintAnchoredAtEndOfLine(t *testing.T) {
	src := "Cursed Energy: in.txt\nReveal: stdout\n"
	results := drive(t, initialize(), didOpen(src), inlayRequest(2))
	res := resultOf(t, results, 2)
	list := res.([]any)
	if len(list) == 0 {
		t.Fatal("no hints")
	}
	first := list[0].(map[string]any)
	pos := first["position"].(map[string]any)
	if got := int(pos["character"].(float64)); got != len("Cursed Energy: in.txt") {
		t.Errorf("hint character = %d, want %d (end of line)", got, len("Cursed Energy: in.txt"))
	}
	if first["paddingLeft"] != true {
		t.Error("hint should ask for left padding so it does not touch the code")
	}
}

// A Shikigami call inlines several nodes at one position; the hint must be the
// call's result type, not its first internal step.
func TestInlayHintForShikigamiCallShowsCallResult(t *testing.T) {
	src := "Cursed Energy: in.txt\n" +
		"Shikigami: Ints\n" +
		"Maximum Technique: Sum\n"
	hints := hintsFor(t, src)
	// Ints is Text -> List<Int>; its body's first step yields List<Text>.
	if hints[1] != ": List<Int>" {
		t.Errorf("Shikigami call hint = %q, want \": List<Int>\" (the call's result)", hints[1])
	}
}

// A Channel is a passthrough, so its own type says nothing; the useful thing is
// what a From: consumer will see.
func TestInlayHintForChannelShowsItsResult(t *testing.T) {
	src := "Cursed Energy: in.txt\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channel \"count\":\n" +
		"    Maximum Technique: Count\n" +
		"Maximum Technique: Combine\n" +
		"    From: count\n" +
		"    Using: (c) -> c\n" +
		"Reveal: stdout\n"
	hints := hintsFor(t, src)
	if hints[2] != ": Int" {
		t.Errorf("Channel hint = %q, want \": Int\" (its body's result)", hints[2])
	}
	if hints[3] != ": Int" {
		t.Errorf("the Count inside the channel should get its own hint, got %q", hints[3])
	}
}

// A Part is a passthrough whose body does the work, and a vow never changes the
// value: neither gets a hint, but the statements inside a Part do.
func TestInlayHintsSkipPartsAndVows(t *testing.T) {
	src := "Cursed Energy: in.txt\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Binding Vow: All Values > 0\n" +
		"Part \"1\":\n" +
		"    Maximum Technique: Sum\n" +
		"    Reveal: stdout\n"
	hints := hintsFor(t, src)
	if _, ok := hints[3]; ok {
		t.Errorf("a Binding Vow should get no hint, got %q", hints[3])
	}
	if _, ok := hints[4]; ok {
		t.Errorf("a Part should get no hint, got %q", hints[4])
	}
	if hints[5] != ": Int" {
		t.Errorf("the Sum inside the Part should get a hint, got %q", hints[5])
	}
}

// Resolution stops at the first error but hands back what it built, so the lines
// above a mistake keep their hints — the REPL's incremental feel in a file that
// does not yet resolve.
func TestInlayHintsSurviveALaterError(t *testing.T) {
	src := "Cursed Energy: in.txt\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Maximum Technique: Sum\n" + // Sum wants List<Int>, gets List<Text>
		"Reveal: stdout\n"
	hints := hintsFor(t, src)
	if hints[0] != ": Text" || hints[1] != ": List<Text>" {
		t.Errorf("hints before the error should survive, got %v", hints)
	}
	if _, ok := hints[2]; ok {
		t.Errorf("the failing line should have no hint, got %q", hints[2])
	}
}

// A first line that cannot resolve at all yields no hints and no crash.
func TestInlayHintsOnUnresolvableProgram(t *testing.T) {
	hints := hintsFor(t, "Maximum Technique: Sum\n")
	if len(hints) != 0 {
		t.Errorf("expected no hints, got %v", hints)
	}
}

func TestInlayHintsOnUnknownDocument(t *testing.T) {
	req := inlayRequest(2)
	results := drive(t, initialize(), req)
	if res := resultOf(t, results, 2); res != nil {
		t.Errorf("hints for an unopened document should be null, got %v", res)
	}
}

func TestInlayHintCapabilityAdvertised(t *testing.T) {
	results := drive(t, initialize())
	res := resultOf(t, results, 1)
	caps := res.(map[string]any)["capabilities"].(map[string]any)
	if caps["inlayHintProvider"] != true {
		t.Errorf("initialize should advertise inlayHintProvider, got %v", caps)
	}
}

// Requesting a narrow range must not return hints outside it.
func TestInlayHintsRespectTheRequestedRange(t *testing.T) {
	src := "Cursed Energy: in.txt\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Maximum Technique: Sum\n"
	req := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/inlayHint",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"range": map[string]any{
				"start": map[string]any{"line": 2, "character": 0},
				"end":   map[string]any{"line": 3, "character": 0},
			},
		},
	}
	results := drive(t, initialize(), didOpen(src), req)
	list := resultOf(t, results, 2).([]any)
	for _, h := range list {
		line := int(h.(map[string]any)["position"].(map[string]any)["line"].(float64))
		if line < 2 || line > 3 {
			t.Errorf("hint on line %d is outside the requested range", line)
		}
	}
	if len(list) != 2 {
		t.Errorf("expected 2 hints in range, got %d", len(list))
	}
}

// Hover shows a Shikigami's declared signature now that definitions can carry
// one — the payoff of annotating the prelude.
func TestHoverShowsDeclaredSignature(t *testing.T) {
	src := "Shikigami \"Doubled\" (k: Int) : List<Int> -> List<Int>\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x * k\n" +
		"Cursed Energy: in.txt\n" +
		"Shikigami: Ints\n"
	hover := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 0, "character": 3},
		},
	}
	results := drive(t, initialize(), didOpen(src), hover)
	res := resultOf(t, results, 2)
	if res == nil {
		t.Fatal("hover on a Shikigami definition returned null")
	}
	val := res.(map[string]any)["contents"].(map[string]any)["value"].(string)
	for _, want := range []string{"k: Int", "List<Int> -> List<Int>"} {
		if !strings.Contains(val, want) {
			t.Errorf("hover should show %q, got %q", want, val)
		}
	}
}
