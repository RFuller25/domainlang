package docs_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The playground is the language compiled to WebAssembly (cmd/domain-wasm),
// and the Run button on the docs site is the only place it is normally
// exercised. These tests drive the built module under node through
// testdata/wasm_harness.js — the same way runner.js drives it in a worker —
// so what a reader gets when they press Run is checked rather than assumed.
//
// The module is a build artifact and is not committed (see docs/wasm/
// README.md), so these skip when it is absent, the same way the renderer tests
// skip without node and the compiler tests skip without a Go toolchain.

// wasmCase is one program to run.
type wasmCase struct {
	Source   string            `json:"source"`
	Input    string            `json:"input"`
	Explain  bool              `json:"explain"`
	Optimize *bool             `json:"optimize,omitempty"`
	Libs     map[string]string `json:"libs,omitempty"`
}

// wasmResult mirrors what domainRun hands back to JavaScript.
type wasmResult struct {
	Output  string   `json:"output"`
	Explain []string `json:"explain"`
	Error   string   `json:"error"`
}

// runWasm executes the cases against the built module, or skips.
func runWasm(t *testing.T, cases ...wasmCase) []wasmResult {
	t.Helper()
	node := requireNode(t)
	if _, err := os.Stat(filepath.Join("wasm", "domain.wasm")); err != nil {
		t.Skip("docs/wasm/domain.wasm not built; run ./docs/wasm/build.sh")
	}
	casePath := filepath.Join(t.TempDir(), "cases.json")
	b, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(casePath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	harness, err := filepath.Abs(filepath.Join("testdata", "wasm_harness.js"))
	if err != nil {
		t.Fatal(err)
	}
	wasmDir, err := filepath.Abs("wasm")
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	cmd := exec.Command(node, harness, wasmDir, casePath)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the playground module: %v\n%s", err, errb.String())
	}
	var res struct {
		Announced []map[string]any `json:"announced"`
		Results   []wasmResult     `json:"results"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decoding harness output: %v\n%s", err, out.String())
	}
	// The module signals readiness before it will answer anything; a missing
	// signal means the worker would sit waiting forever.
	if len(res.Announced) == 0 {
		t.Error("the module never announced itself ready")
	}
	return res.Results
}

// The playground has to run the programs the site actually offers, and produce
// the answers the golden tests pin.
func TestPlaygroundRunsPrograms(t *testing.T) {
	results := runWasm(t,
		// The quick-orientation program from docs/README.md.
		wasmCase{
			Source: "Cursed Energy: input.txt\nCursed Technique: Split Text by \"\\n\"\n" +
				"Channeled Energy: Convert To Integers\nMaximum Technique: Sum\nReveal: stdout",
			Input: "1\n2\n3\n4\n5",
		},
		// A type error is reported as an ordinary result, not a crash.
		wasmCase{
			Source: "Cursed Energy: input.txt\nMaximum Technique: Sum\nReveal: stdout",
			Input:  "x",
		},
		// So is a parse error.
		wasmCase{Source: "Cursed Technique: Split Text by", Input: ""},
	)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if got := strings.TrimSpace(results[0].Output); got != "15" {
		t.Errorf("summing 1..5 gave %q (error %q), want 15", got, results[0].Error)
	}
	if results[1].Error == "" {
		t.Error("a type error produced no error message")
	} else if !strings.Contains(results[1].Error, "List<Int>") {
		t.Errorf("type error is unhelpful: %q", results[1].Error)
	}
	if results[2].Error == "" {
		t.Error("a malformed program produced no error message")
	}
}

// `Cursed Energy: input.txt` must read the input box. There is no filesystem
// under js/wasm, and it reports ENOSYS rather than "not found" — which the
// stdin fallback did not recognise, so every program naming an input file
// failed with "not implemented on js" instead of running. This is the test for
// that: the program names a file that could not exist either way.
func TestPlaygroundFallsBackToTheInputBox(t *testing.T) {
	results := runWasm(t, wasmCase{
		Source: "Cursed Energy: no-such-file-anywhere.txt\nCursed Technique: Split Text by \"\\n\"\n" +
			"Channeled Energy: Convert To Integers\nMaximum Technique: Sum\nReveal: stdout",
		Input: "10\n20\n12",
	})
	if results[0].Error != "" {
		t.Fatalf("naming a missing input file failed instead of reading the input box: %s", results[0].Error)
	}
	if got := strings.TrimSpace(results[0].Output); got != "42" {
		t.Errorf("output %q, want 42 — the input box was not used as stdin", got)
	}
}

// Explain is the reason the playground is worth having: it shows the optimizer
// answering a named-algorithm request with a different algorithm, which is the
// language's whole thesis and is otherwise only a claim in the prose.
func TestPlaygroundExplainsSubstitutions(t *testing.T) {
	src := "Cursed Energy: input.txt\nCursed Technique: Split Text by \"\\n\\n\"\n" +
		"Cursed Technique: Split Each by \"\\n\"\nChanneled Energy: Convert Each List to Integers\n" +
		"Maximum Technique: Sum Each Group\nDomain Expansion: Quicksort, Descending\n" +
		"Maximum Technique: Select Top 3, Sum\nReveal: stdout"
	input := "1000\n2000\n3000\n\n4000\n\n5000\n6000\n\n7000\n8000\n9000\n\n10000"
	off := false
	results := runWasm(t,
		wasmCase{Source: src, Input: input, Explain: true},
		wasmCase{Source: src, Input: input, Explain: true, Optimize: &off},
	)

	if got := strings.TrimSpace(results[0].Output); got != "45000" {
		t.Errorf("optimized run gave %q, want 45000", got)
	}
	if len(results[0].Explain) == 0 {
		t.Fatal("Explain reported no rewrites for a program written to trigger one")
	}
	joined := strings.Join(results[0].Explain, "\n")
	if !strings.Contains(strings.ToLower(joined), "quickselect") {
		t.Errorf("Explain did not mention the quickselect substitution:\n%s", joined)
	}

	// The unoptimized run must agree on the answer — that is the guarantee the
	// substitution rests on — while reporting no rewrites.
	if got := strings.TrimSpace(results[1].Output); got != "45000" {
		t.Errorf("unoptimized run gave %q, want the same 45000", got)
	}
	for _, line := range results[1].Explain {
		if !strings.Contains(line, "no optimizations applied") {
			t.Errorf("optimizer disabled but a rewrite was reported: %s", line)
		}
	}
}

// Every program in the gallery must run in the playground and produce exactly
// its .expected output — the same golden data the CLI tests use. The gallery
// offers a Run button on all of them, so anything that only works in the
// binary is a promise the site cannot keep.
func TestPlaygroundRunsEveryGalleryProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the full gallery run in -short")
	}
	var programs []galleryProgram
	b, err := os.ReadFile("gallery.json")
	if err != nil {
		t.Fatalf("reading gallery.json: %v", err)
	}
	if err := json.Unmarshal(b, &programs); err != nil {
		t.Fatal(err)
	}
	cases := make([]wasmCase, len(programs))
	for i, p := range programs {
		cases[i] = wasmCase{Source: p.Source, Input: p.Input, Libs: p.Libs}
	}
	results := runWasm(t, cases...)
	if len(results) != len(programs) {
		t.Fatalf("got %d results for %d programs", len(results), len(programs))
	}
	for i, p := range programs {
		got, want := results[i].Output, p.Expected
		if results[i].Error != "" {
			t.Errorf("%s/%s failed in the playground: %s", p.Group, p.ID, results[i].Error)
			continue
		}
		// The CLI compares trimmed output against the golden file; do the same.
		if strings.TrimRight(got, "\n") != strings.TrimRight(want, "\n") {
			t.Errorf("%s/%s output differs from its .expected file:\n got: %q\nwant: %q",
				p.Group, p.ID, got, want)
		}
	}
}
