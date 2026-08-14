package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"domain/prims"
)

func TestParseStatsArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(*testing.T, string, statsOptions)
	}{
		{
			name: "defaults",
			args: []string{"aoc2024"},
			check: func(t *testing.T, path string, o statsOptions) {
				if path != "aoc2024" {
					t.Errorf("path = %q", path)
				}
				if o.Sort != "name" {
					t.Errorf("default sort = %q want name (day order for AoC naming)", o.Sort)
				}
				if o.Runs != 3 {
					t.Errorf("default runs = %d want 3", o.Runs)
				}
			},
		},
		{
			name: "every flag",
			args: []string{"--sort=time", "--top=5", "--runs=1", "--timeout=5s", "--interpret", "aoc"},
			check: func(t *testing.T, _ string, o statsOptions) {
				if o.Sort != "time" || o.Top != 5 || o.Runs != 1 {
					t.Errorf("options = %+v", o)
				}
				if o.Timeout != 5*time.Second {
					t.Errorf("timeout = %s", o.Timeout)
				}
				if !o.Interpret {
					t.Error("--interpret not recorded")
				}
			},
		},
		{name: "no folder", args: []string{"--sort=time"}, wantErr: true},
		{name: "bad sort", args: []string{"--sort=vibes", "aoc"}, wantErr: true},
		{name: "zero runs", args: []string{"--runs=0", "aoc"}, wantErr: true},
		{name: "unknown flag", args: []string{"--nope", "aoc"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, opts, err := parseStatsArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, path, opts)
			}
		})
	}
}

// The whole leaderboard over the challenges, interpreted so the test does not
// pay thirteen Go builds. Durations are never asserted — the row count, the
// columns, the totals line and the ✓ column are the contract.
func TestStatsOverChallenges(t *testing.T) {
	useRealDomainBinary(t)
	root := filepath.Join("..", "..", "challenges")
	_, opts, err := parseStatsArgs([]string{root, "--runs=1", "--plain", "--interpret"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Stats(root, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d — a challenge program failed or mismatched\n%s\n%s", code, out.String(), errb.String())
	}
	got := out.String()
	for _, want := range []string{
		"program", "LOC", "stages", "runtime", "passes fired",
		"13 programs", "13/13", "vocabulary", "slowest",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
	for _, name := range []string{"01_fizzbuzz", "13_minesweeper"} {
		if !strings.Contains(got, name) {
			t.Errorf("program %q missing from the table:\n%s", name, got)
		}
	}
}

func TestStatsJSONShape(t *testing.T) {
	useRealDomainBinary(t)
	root := filepath.Join("..", "..", "challenges")
	_, opts, err := parseStatsArgs([]string{root, "--runs=1", "--json", "--interpret"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Stats(root, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, errb.String())
	}
	var doc statsJSON
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if len(doc.Programs) != 13 {
		t.Fatalf("programs = %d want 13", len(doc.Programs))
	}
	if doc.Totals.OfN != 13 || doc.Totals.Matched != 13 {
		t.Errorf("expected totals = %d/%d, want 13/13", doc.Totals.Matched, doc.Totals.OfN)
	}
	if doc.Totals.LOC == 0 || doc.Totals.Stages == 0 {
		t.Errorf("totals look empty: %+v", doc.Totals)
	}
	// The vocabulary numerator must not exceed its denominator — the
	// structural statements are counted in Usage but are not catalog entries.
	if doc.Vocabulary.Primitives > doc.Vocabulary.OfPrims {
		t.Errorf("primitives used (%d) exceeds the catalog (%d)",
			doc.Vocabulary.Primitives, doc.Vocabulary.OfPrims)
	}
}

// A program that fails keeps its row and makes the command exit nonzero.
// Dropping it would quietly shrink the folder and make every total wrong.
func TestStatsKeepsFailingPrograms(t *testing.T) {
	useRealDomainBinary(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.domain"), []byte(benchProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Resolves, but dies at run time on an input it cannot parse.
	bad := `Cursed Energy: bad.input
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`
	if err := os.WriteFile(filepath.Join(dir, "bad.domain"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.input"), []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, opts, err := parseStatsArgs([]string{dir, "--runs=1", "--plain", "--interpret"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Stats(dir, opts, &out, &errb)
	got := out.String()
	if !strings.Contains(got, "bad") {
		t.Errorf("the failing program lost its row:\n%s", got)
	}
	if !strings.Contains(got, "2 programs") {
		t.Errorf("the totals dropped the failing program:\n%s", got)
	}
	if code == 0 {
		t.Error("a folder with a failing program should exit nonzero")
	}
}

// Sorting by time must put the rows without a time last: "did not finish" is
// not "instantaneous".
func TestStatsSortByTimePutsUntimedRowsLast(t *testing.T) {
	rows := []statRow{
		{Name: "fast", Wall: time.Millisecond},
		{Name: "broken", Status: "did not finish"},
		{Name: "slow", Wall: time.Second},
	}
	sortRows(rows, "time")
	if rows[0].Name != "slow" {
		t.Errorf("slowest should lead, got %q", rows[0].Name)
	}
	if rows[len(rows)-1].Name != "broken" {
		t.Errorf("the untimed row should sort last, got %q", rows[len(rows)-1].Name)
	}
}

func TestCountLOCIgnoresBlanksAndComments(t *testing.T) {
	src := "# a comment\n\nCursed Energy: stdin\n   \nReveal: stdout\n"
	loc, total := countLOC(src)
	if loc != 2 {
		t.Errorf("loc = %d want 2 (the two statements)", loc)
	}
	if total != 5 {
		t.Errorf("total = %d want 5", total)
	}
}

// A folder with no .expected files drops the column rather than filling it
// with dashes that read as failures.
func TestStatsOmitsExpectedColumnWhenAbsent(t *testing.T) {
	rep := &statsReport{
		Root: "x", Runs: 1, Vocab: prims.Used(nil),
		Rows: []statRow{{Name: "a", LOC: 3, Stages: 2, Wall: time.Millisecond}},
	}
	var out bytes.Buffer
	rep.writeTable(&out)
	if strings.Contains(out.String(), "✗") {
		t.Errorf("a folder with no .expected files shows failure marks:\n%s", out.String())
	}
	yes := true
	rep.Rows[0].Expected = &yes
	out.Reset()
	rep.writeTable(&out)
	if !strings.Contains(out.String(), "✓") {
		t.Errorf("a matching row should be ticked:\n%s", out.String())
	}
}
