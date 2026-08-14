package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"domain/runner"
)

// battleArena writes a Domain program, a Python counterpart that computes the
// same thing, one that does not, and the input all three share.
func battleArena(t *testing.T) (dir, domainProg, pyProg, wrongProg string) {
	t.Helper()
	dir = t.TempDir()
	domainProg = filepath.Join(dir, "sum.domain")
	src := `Cursed Energy: sum.input
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("sum.domain", src)
	write("sum.input", "1\n2\n3\n4\n")
	pyProg = write("sum.py", "import sys\nprint(sum(int(x) for x in sys.stdin if x.strip()))\n")
	wrongProg = write("wrong.py", "import sys\nprint(sum(int(x) for x in sys.stdin if x.strip()) + 1)\n")
	return dir, domainProg, pyProg, wrongProg
}

func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("no python on PATH")
		}
	}
}

func TestParseBattleArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(*testing.T, string, string, battleOptions)
	}{
		{
			name: "two files, language inferred",
			args: []string{"a.domain", "b.py"},
			check: func(t *testing.T, a, b string, o battleOptions) {
				if a != "a.domain" || b != "b.py" {
					t.Errorf("files = %q, %q", a, b)
				}
				if o.Lang != "" {
					t.Errorf("lang should be empty when not given, got %q", o.Lang)
				}
			},
		},
		{
			name: "the spelling from the spec, with --lang between the files",
			args: []string{"a.domain", "--lang", "python", "b"},
			check: func(t *testing.T, a, b string, o battleOptions) {
				if a != "a.domain" || b != "b" {
					t.Errorf("files = %q, %q", a, b)
				}
				if o.Lang != "python" {
					t.Errorf("lang = %q", o.Lang)
				}
			},
		},
		{
			name: "every other flag",
			args: []string{"a.domain", "b.py", "--runs=7", "--timeout=3s", "--interpret",
				"--challenger-args", "-O -X utf8", "--input", "in.txt"},
			check: func(t *testing.T, _, _ string, o battleOptions) {
				if o.Runs != 7 || o.Timeout != 3*time.Second || !o.Interpret {
					t.Errorf("options = %+v", o)
				}
				if len(o.ChallengerArgs) != 3 {
					t.Errorf("challenger args = %v", o.ChallengerArgs)
				}
				if o.Input != "in.txt" {
					t.Errorf("input = %q", o.Input)
				}
			},
		},
		{name: "one file", args: []string{"a.domain"}, wantErr: true},
		{name: "three files", args: []string{"a.domain", "b.py", "c.go"}, wantErr: true},
		{name: "unknown flag", args: []string{"--nope", "a.domain", "b.py"}, wantErr: true},
		{name: "both input forms", args: []string{"a.domain", "b.py", "-i", "x", "--input-text", "y"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b, opts, err := parseBattleArgs(tc.args)
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
				tc.check(t, a, b, opts)
			}
		})
	}
}

func TestChallengerSpecResolution(t *testing.T) {
	cases := []struct {
		name, path, lang, want string
		wantErr                string
	}{
		{name: "inferred from .py", path: "x/rival.py", want: "Python"},
		{name: "inferred from .weave", path: "rival.weave", want: "Weave"},
		{name: "inferred from .go", path: "rival.go", want: "Go"},
		{name: "explicit wins", path: "rival.txt", lang: "weave", want: "Weave"},
		{name: "explicit is case-insensitive", path: "r.py", lang: "PYTHON", want: "Python"},
		{name: "unknown language", path: "r.py", lang: "fortran", wantErr: "unknown language"},
		{name: "cannot infer", path: "rival.txt", wantErr: "cannot tell what language"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := challengerSpec(tc.path, battleOptions{Lang: tc.lang})
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
				}
				// Whichever way it failed, the message must list what is
				// available — the user's next move depends on knowing.
				if !strings.Contains(err.Error(), "Weave") {
					t.Errorf("error does not list the known languages: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if spec.Name != tc.want {
				t.Errorf("language = %q want %q", spec.Name, tc.want)
			}
		})
	}
}

// The whole race against a Python counterpart that agrees.
func TestBattleAgreeingRace(t *testing.T) {
	requirePython(t)
	useRealDomainBinary(t)
	_, domainProg, pyProg, _ := battleArena(t)

	_, _, opts, err := parseBattleArgs([]string{domainProg, pyProg, "--runs=1", "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Battle(domainProg, pyProg, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errb.String())
	}
	got := out.String()
	for _, want := range []string{
		"output ✓ identical (10)",
		"WINS",
		"how this was measured",
		"both sides are subprocesses",
		"build time is reported separately",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("verdict is missing %q:\n%s", want, got)
		}
	}
}

// The gate: two programs that compute different things get no winner, and a
// nonzero exit. This is the property the command exists to protect.
func TestBattleRefusesToDeclareOnDisagreement(t *testing.T) {
	requirePython(t)
	useRealDomainBinary(t)
	_, domainProg, _, wrongProg := battleArena(t)

	_, _, opts, err := parseBattleArgs([]string{domainProg, wrongProg, "--runs=1", "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Battle(domainProg, wrongProg, opts, &out, &errb)
	got := out.String()
	if code == 0 {
		t.Errorf("a disagreement should exit nonzero:\n%s", got)
	}
	if !strings.Contains(got, "NO CONTEST") {
		t.Errorf("no refusal in the report:\n%s", got)
	}
	if strings.Contains(got, "WINS") {
		t.Errorf("a winner was declared despite the disagreement:\n%s", got)
	}
	// It shows where they differ, so the reader can act on it.
	if !strings.Contains(got, `"10"`) || !strings.Contains(got, `"11"`) {
		t.Errorf("the differing line is not shown:\n%s", got)
	}
	// And it reports no timings, because the race never ran — printing the
	// agreement check's single run under a "best of N" header would be
	// inventing a measurement.
	if strings.Contains(got, "best of") {
		t.Errorf("a no-contest reported a best-of-N that never happened:\n%s", got)
	}
}

// A missing runtime is a setup problem, not a failure of anyone's program, and
// the message has to say where to get it.
func TestBattleMissingRuntimeIsAUsageError(t *testing.T) {
	dir, domainProg, _, _ := battleArena(t)
	rival := filepath.Join(dir, "rival.weave")
	if err := os.WriteFile(rival, []byte("Source through sum\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Point the override at a name nothing will have, which is also the
	// documented way to redirect it.
	t.Setenv("DOMAIN_WEAVE", "")
	t.Setenv("PATH", dir)

	_, _, opts, err := parseBattleArgs([]string{domainProg, rival, "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Battle(domainProg, rival, opts, &out, &errb); code != 2 {
		t.Errorf("a missing runtime should be a usage error (2), got %d\n%s", code, errb.String())
	}
	msg := errb.String()
	for _, want := range []string{"Weave not found", "DOMAIN_WEAVE", "github.com/malleum/weavelang"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message does not mention %q: %s", want, msg)
		}
	}
}

func TestBattleJSONShape(t *testing.T) {
	requirePython(t)
	useRealDomainBinary(t)
	_, domainProg, pyProg, _ := battleArena(t)

	_, _, opts, err := parseBattleArgs([]string{domainProg, pyProg, "--runs=1", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Battle(domainProg, pyProg, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var doc battleJSON
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if !doc.Agreed {
		t.Error("agreeing programs reported as disagreeing")
	}
	if doc.Language != "Python" {
		t.Errorf("language = %q", doc.Language)
	}
	if doc.Winner == "" || doc.Speedup <= 0 {
		t.Errorf("no verdict in the JSON: %+v", doc)
	}
	if len(doc.Sides) != 2 {
		t.Fatalf("sides = %d want 2", len(doc.Sides))
	}
	// The compiled Domain side pays a build; the interpreted challenger
	// does not, and inventing one for it would misreport the race.
	if doc.Sides[0].BuildNanos <= 0 {
		t.Error("the compiled Domain side reported no build time")
	}
	if doc.Sides[1].BuildNanos != 0 {
		t.Error("the Python side reported a build time it never paid")
	}
}

// The verdict names a winner on both clocks, because they can disagree — a
// compiled side can win the run and lose to first answer.
func TestBattleVerdictReportsBothClocks(t *testing.T) {
	rep := &battleReport{
		Contestants: []runner.Contestant{{Label: "a (Domain, compiled)"}, {Label: "b (Python)"}},
		Runs:        5,
		Lang:        "Python",
		Answer:      "42",
		Results: []runner.Result{
			{Wall: 10 * time.Millisecond, Build: 500 * time.Millisecond},
			{Wall: 100 * time.Millisecond},
		},
	}
	var out bytes.Buffer
	rep.writePlain(&out)
	got := out.String()
	if !strings.Contains(got, "a (Domain, compiled)") || !strings.Contains(strings.ToUpper(got), "WINS") {
		t.Errorf("the faster side did not win the run:\n%s", got)
	}
	if !strings.Contains(got, "wins to first answer") {
		t.Errorf("the build-inclusive clock is not reported, though it flips the result:\n%s", got)
	}
}

// The rules are part of the output, not a footnote: a published head-to-head
// number that cannot be argued with is worse than none.
func TestBattleStatesItsMethodology(t *testing.T) {
	rep := &battleReport{
		Contestants: []runner.Contestant{{Label: "a"}, {Label: "b"}},
		Runs:        9, Lang: "Weave", Answer: "1",
		Results: []runner.Result{{Wall: time.Millisecond}, {Wall: 2 * time.Millisecond}},
	}
	var out bytes.Buffer
	rep.writePlain(&out)
	got := out.String()
	for _, want := range []string{
		"best of 9, alternating",
		"redirected file",
		"compiled and optimized",
		"the Weave side runs as its own runtime runs it",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the methodology does not state %q:\n%s", want, got)
		}
	}
	// --interpret must change the claim, not just the behaviour.
	rep.Interpreted = true
	out.Reset()
	rep.writePlain(&out)
	if !strings.Contains(out.String(), "--interpret was given") {
		t.Errorf("an interpreted race still claims to be compiled:\n%s", out.String())
	}
}
