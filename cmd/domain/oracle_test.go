package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"domain/runner"
)

// oracleGrid builds the four cells with the given outputs, in runner.Four's
// order: interpret/naive, interpret/optimized, compile/naive, compile/optimized.
func oracleGrid(outs ...string) oracleCase {
	c := oracleCase{Input: "test"}
	for i, cfg := range runner.Four {
		c.Results = append(c.Results, runner.Result{Config: cfg, Stdout: []byte(outs[i])})
	}
	c.judge()
	return c
}

// The grid is two axes, and a disagreement's direction names the component.
func TestOracleBlamesTheRightHalf(t *testing.T) {
	cases := []struct {
		name    string
		outs    [4]string
		verdict string
		blame   string
	}{
		{"all agree", [4]string{"1\n", "1\n", "1\n", "1\n"}, "agree", ""},
		// Naive and optimized differ on both backends; the backends agree.
		{"optimizer", [4]string{"1\n", "2\n", "1\n", "2\n"}, "disagree", "optimizer"},
		// The backends differ on both pipelines; the optimizer changes nothing.
		{"backend", [4]string{"1\n", "1\n", "2\n", "2\n"}, "disagree", "backend"},
		// One cell alone is wrong: it differs from a neighbour on each axis.
		{"one cell", [4]string{"1\n", "1\n", "1\n", "2\n"}, "disagree", "both"},
		// A trailing newline is a difference here, as it is for bench.
		{"newline", [4]string{"1\n", "1\n", "1", "1"}, "disagree", "backend"},
	}
	for _, tc := range cases {
		c := oracleGrid(tc.outs[:]...)
		if c.Verdict != tc.verdict || c.Blame != tc.blame {
			t.Errorf("%s: verdict %q blame %q, want %q %q", tc.name, c.Verdict, c.Blame, tc.verdict, tc.blame)
		}
	}
}

// A program that fails is compared on how it failed, not on what it managed to
// print first or how the backend worded the error.
func TestOracleComparesFailuresByStatus(t *testing.T) {
	c := oracleCase{}
	for i, cfg := range runner.Four {
		c.Results = append(c.Results, runner.Result{
			Config: cfg, ExitCode: 1,
			Stdout: []byte(strings.Repeat("x", i)),
			Stderr: []byte("worded differently " + cfg.Label()),
		})
	}
	c.judge()
	if c.Verdict != "agree" {
		t.Errorf("four cells failing with the same status: verdict %q, want agree", c.Verdict)
	}

	// …but failing where the others succeed is a disagreement.
	c.Results[3].ExitCode = 0
	c.judge()
	if c.Verdict != "disagree" {
		t.Errorf("one cell succeeding where three fail: verdict %q, want disagree", c.Verdict)
	}
}

// A cell the harness could not run is not evidence either way, and a grid with
// fewer than two cells left has checked nothing — which must not read as a pass.
func TestOracleUncheckedIsNotAgreement(t *testing.T) {
	c := oracleGrid("1\n", "1\n", "1\n", "1\n")
	for i := 1; i < 4; i++ {
		c.Results[i].Err = errBuildFailed
	}
	c.judge()
	if c.Verdict != "unchecked" {
		t.Errorf("verdict %q, want unchecked", c.Verdict)
	}
	rep := &oracleReport{Cases: []oracleCase{c}}
	if rep.agreed() {
		t.Error("a report that cross-checked nothing must not count as agreement")
	}
}

var errBuildFailed = &exitError{code: 1}

func TestOracleReportShowsTheFirstDifference(t *testing.T) {
	c := oracleGrid("a\nb\nc\n", "a\nB\nc\n", "a\nb\nc\n", "a\nB\nc\n")
	var out bytes.Buffer
	(&oracleReport{Program: "p.domain", Cases: []oracleCase{c}}).writeTable(&out)
	got := out.String()
	for _, want := range []string{
		"COMPILER BUG",
		"the optimizer changed this program's answer",
		"line 2",
		`interpret / naive: "b"`,
		`interpret / optimized: "B"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
}

func TestParseOracleArgs(t *testing.T) {
	path, opts, err := parseOracleArgs([]string{"p.domain", "--input", "a", "-i", "b", "--input=c", "--timeout", "5s", "--release"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "p.domain" || strings.Join(opts.Inputs, ",") != "a,b,c" || opts.Timeout.Seconds() != 5 {
		t.Errorf("got %q %+v", path, opts)
	}
	for _, c := range opts.Cells {
		if !c.Release {
			t.Error("--release should mark every cell")
		}
	}
	if runner.Four[0].Release {
		t.Error("--release leaked into runner.Four")
	}
	for _, bad := range [][]string{
		{},
		{"p.domain", "--bogus"},
		{"p.domain", "--input", "a", "--input-text", "b"},
		{"p.domain", "--cells", "compile/optimized"}, // one cell agrees with nothing
		{"p.domain", "--input"},
	} {
		if _, _, err := parseOracleArgs(bad); err == nil {
			t.Errorf("parseOracleArgs(%v) should fail", bad)
		}
	}
}

// End to end: a real program through all four cells, over two inputs.
func TestOracleCommand(t *testing.T) {
	useRealDomainBinary(t)
	path := writeBenchProgram(t)
	other := strings.TrimSuffix(path, "prog.domain") + "other.txt"
	writeFile(t, other, "10\n20\n")

	var out, errOut bytes.Buffer
	code := Expansion([]string{"oracle"}, []string{path, "--input", other, "--input", strings.TrimSuffix(path, "prog.domain") + "input.txt", "--json"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
	}
	var rep struct {
		Agreed bool `json:"agreed"`
		Cases  []struct {
			Verdict string `json:"verdict"`
			Cells   []struct {
				Stdout string `json:"stdout"`
			} `json:"cells"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !rep.Agreed || len(rep.Cases) != 2 {
		t.Fatalf("agreed=%v cases=%d\n%s", rep.Agreed, len(rep.Cases), out.String())
	}
	for i, want := range []string{"30\n", "6\n"} {
		if len(rep.Cases[i].Cells) != 4 {
			t.Fatalf("case %d has %d cells", i, len(rep.Cases[i].Cells))
		}
		for _, cell := range rep.Cases[i].Cells {
			if cell.Stdout != want {
				t.Errorf("case %d: a cell printed %q, want %q", i, cell.Stdout, want)
			}
		}
	}

	// The table form, on the sibling input.
	out.Reset()
	if code := Expansion([]string{"oracle"}, []string{path}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "✓ all 4 cells agree on the output") {
		t.Errorf("table:\n%s", out.String())
	}
}
