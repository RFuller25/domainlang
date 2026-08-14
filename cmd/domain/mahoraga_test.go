package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"domain/mahoraga"
)

func mahoragaArena(t *testing.T) (prog, input, expected string) {
	t.Helper()
	dir := t.TempDir()
	src := `Cursed Energy: in.txt
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
	return write("prog.domain", src), write("in.txt", "1\n2\n3\n4\n"), write("want.txt", "10\n")
}

func TestParseMahoragaArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(*testing.T, string, string, string, mahoragaOptions)
	}{
		{
			name: "three files, pinned by default",
			args: []string{"p.domain", "in.txt", "want.txt"},
			check: func(t *testing.T, p, i, e string, o mahoragaOptions) {
				if p != "p.domain" || i != "in.txt" || e != "want.txt" {
					t.Errorf("files = %q %q %q", p, i, e)
				}
				if o.Tier != mahoraga.Pinned {
					t.Errorf("default tier = %v, want pinned — the command is named for adapting to one opponent", o.Tier)
				}
			},
		},
		{
			name: "every flag",
			args: []string{"p.domain", "in", "want", "--turns=4", "--runs=6", "--screen-runs=2",
				"--min-effect=0.05", "--tier=guarded", "--timeout=30s", "--seed=7", "-o", "bin", "--recipe", "r.json"},
			check: func(t *testing.T, _, _, _ string, o mahoragaOptions) {
				if o.Turns != 4 || o.BaselineRuns != 6 || o.ScreenRuns != 2 {
					t.Errorf("run counts = %+v", o)
				}
				if o.MinEffect != 0.05 || o.Tier != mahoraga.Guarded {
					t.Errorf("effect/tier = %v %v", o.MinEffect, o.Tier)
				}
				if o.Timeout != 30*time.Second || o.Seed != 7 {
					t.Errorf("timeout/seed = %v %v", o.Timeout, o.Seed)
				}
				if o.Out != "bin" || o.Recipe != "r.json" {
					t.Errorf("paths = %q %q", o.Out, o.Recipe)
				}
			},
		},
		{
			// The recipe records the program, input and expected output, so a
			// replay needs none of them.
			name: "replay needs no files",
			args: []string{"--replay", "r.json"},
			check: func(t *testing.T, p, _, _ string, o mahoragaOptions) {
				if o.Replay != "r.json" {
					t.Errorf("replay = %q", o.Replay)
				}
				if p != "" {
					t.Errorf("replay should not require a program, got %q", p)
				}
			},
		},
		{
			name: "verify takes an optional input to check",
			args: []string{"--verify", "r.json", "other.txt"},
			check: func(t *testing.T, _, i, _ string, o mahoragaOptions) {
				if o.Verify != "r.json" || i != "other.txt" {
					t.Errorf("verify = %q, input = %q", o.Verify, i)
				}
			},
		},
		{name: "too few files", args: []string{"p.domain", "in.txt"}, wantErr: true},
		{name: "too many files", args: []string{"a", "b", "c", "d"}, wantErr: true},
		{name: "unknown flag", args: []string{"--nope", "a", "b", "c"}, wantErr: true},
		{name: "bad tier", args: []string{"--tier=reckless", "a", "b", "c"}, wantErr: true},
		{name: "negative runs", args: []string{"--runs=-1", "a", "b", "c"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, i, e, o, err := parseMahoragaArgs(tc.args)
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
				tc.check(t, p, i, e, o)
			}
		})
	}
}

// The whole command over a tiny program, one turn: it writes a binary and a
// recipe, and reports without claiming a win it did not find.
func TestMahoragaEndToEnd(t *testing.T) {
	requireGoToolchain(t)
	prog, input, expected := mahoragaArena(t)
	dir := filepath.Dir(prog)
	out := filepath.Join(dir, "adapted")
	recipe := filepath.Join(dir, "r.json")

	_, _, _, opts, err := parseMahoragaArgs([]string{prog, input, expected,
		"--turns=1", "--runs=3", "--screen-runs=2", "--plain", "-o", out, "--recipe", recipe})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Mahoraga(prog, input, expected, opts, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	got := stdout.String()

	for _, want := range []string{"turn 1", "baseline", "binary", "recipe"} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
	// Only the baseline turn ran, so there is nothing to have won with — and
	// the command must say so rather than manufacture a result.
	if !strings.Contains(got, "BASELINE UNBEATEN") {
		t.Errorf("a search that adapted nothing did not say so:\n%s", got)
	}
	for _, p := range []string{out, recipe} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was not written: %v", p, err)
		}
	}
	// A pinned binary's caveat must not be printed when nothing was adapted:
	// the baseline binary is a perfectly general program.
	if strings.Contains(got, "may answer wrongly") {
		t.Errorf("the pinned-binary warning was printed for an unadapted baseline:\n%s", got)
	}
}

// Replay rebuilds from the recipe and re-checks, and verify answers "can I
// still use this?" without building.
func TestMahoragaReplayAndVerify(t *testing.T) {
	requireGoToolchain(t)
	prog, input, expected := mahoragaArena(t)
	dir := filepath.Dir(prog)
	recipe := filepath.Join(dir, "r.json")

	_, _, _, opts, err := parseMahoragaArgs([]string{prog, input, expected,
		"--turns=1", "--runs=3", "--screen-runs=2", "--plain",
		"-o", filepath.Join(dir, "adapted"), "--recipe", recipe})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Mahoraga(prog, input, expected, opts, nil, &out, &errb); code != 0 {
		t.Fatalf("search failed: %s", errb.String())
	}

	// Verify against the input it was adapted to.
	_, vin, _, vopts, err := parseMahoragaArgs([]string{"--verify", recipe, "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Mahoraga("", vin, "", vopts, nil, &out, &errb); code != 0 {
		t.Errorf("verifying against the original input failed: %s", out.String())
	}
	if !strings.Contains(out.String(), "the input this recipe was adapted to") {
		t.Errorf("verify did not confirm the input:\n%s", out.String())
	}

	// Replay rebuilds a working binary.
	replayed := filepath.Join(dir, "replayed")
	_, rin, rexp, ropts, err := parseMahoragaArgs([]string{"--replay", recipe, "-o", replayed, "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Mahoraga("", rin, rexp, ropts, nil, &out, &errb); code != 0 {
		t.Fatalf("replay failed: %s\n%s", out.String(), errb.String())
	}
	if _, err := os.Stat(replayed); err != nil {
		t.Errorf("replay wrote no binary: %v", err)
	}
	if !strings.Contains(out.String(), "re-checked") {
		t.Errorf("replay did not report re-verifying:\n%s", out.String())
	}
}

// A recipe of general-tier adaptations replays onto any input; the mismatch is
// worth mentioning and nothing more. (A pinned one will not be, once turn 8
// exists — which is why Verify separates "different" from "unsafe".)
func TestMahoragaVerifyDistinguishesDifferentFromUnsafe(t *testing.T) {
	r := &mahoraga.Recipe{
		Version:          mahoraga.RecipeVersion,
		InputFingerprint: mahoraga.Fingerprint{Bytes: 100, Lines: 10, SHA256: "abc"},
	}
	dir := t.TempDir()
	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(other, []byte("1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := mahoraga.Verify(r, other)
	if v.Matches {
		t.Error("a different input was reported as matching")
	}
	if !v.Safe {
		t.Error("a recipe with no pinned adaptations was reported unsafe on a different input")
	}
	if len(v.Reasons) == 0 {
		t.Error("no reason was given for the mismatch")
	}

	// Add a pinned adaptation and it becomes unsafe.
	r.Adaptations = []mahoraga.Adaptation{{Turn: 8, ID: "flat grid", Tier: "pinned", Kept: true}}
	if mahoraga.Verify(r, other).Safe {
		t.Error("a pinned adaptation was reported safe on an input it was not verified against")
	}
}

// `domain build --recipe` applies a recorded pass schedule and build flags,
// which is what makes a tuning durable across rebuilds.
func TestBuildWithRecipe(t *testing.T) {
	requireGoToolchain(t)
	prog, input, expected := mahoragaArena(t)
	dir := filepath.Dir(prog)
	recipe := filepath.Join(dir, "r.json")

	_, _, _, opts, err := parseMahoragaArgs([]string{prog, input, expected,
		"--turns=1", "--runs=3", "--screen-runs=2", "--plain",
		"-o", filepath.Join(dir, "adapted"), "--recipe", recipe})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Mahoraga(prog, input, expected, opts, nil, &out, &errb); code != 0 {
		t.Fatalf("search failed: %s", errb.String())
	}

	rebuilt := filepath.Join(dir, "rebuilt")
	path, bopts, err := parseBuildArgs([]string{prog, "--recipe", recipe, "-o", rebuilt})
	if err != nil {
		t.Fatal(err)
	}
	if bopts.Recipe != recipe {
		t.Fatalf("--recipe not parsed: %q", bopts.Recipe)
	}
	out.Reset()
	errb.Reset()
	if err := Build(path, bopts, nil, &out, &errb); err != nil {
		t.Fatalf("build --recipe: %v\n%s", err, errb.String())
	}
	if _, err := os.Stat(rebuilt); err != nil {
		t.Fatalf("no binary from --recipe: %v", err)
	}
}

// A recipe carrying a guarded or pinned adaptation was verified against a
// particular input. `domain build` has no input, so it must refuse rather
// than produce a binary bound to a contract nobody checked.
func TestBuildRefusesANonGeneralRecipe(t *testing.T) {
	prog, _, _ := mahoragaArena(t)
	dir := filepath.Dir(prog)

	r := &mahoraga.Recipe{
		Version: mahoraga.RecipeVersion,
		Program: prog,
		Adaptations: []mahoraga.Adaptation{
			{Turn: 8, TurnName: "pinned specialisation", ID: "flat grid", Tier: "pinned", Kept: true},
		},
	}
	recipe := filepath.Join(dir, "pinned.json")
	if err := r.Write(recipe); err != nil {
		t.Fatal(err)
	}

	path, bopts, err := parseBuildArgs([]string{prog, "--recipe", recipe, "-o", filepath.Join(dir, "x")})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	err = Build(path, bopts, nil, &out, &errb)
	if err == nil {
		t.Fatal("a pinned recipe was applied by `domain build`, which has no input to verify it against")
	}
	msg := err.Error()
	// The refusal has to point at the command that *can* check it.
	if !strings.Contains(msg, "pinned-tier") || !strings.Contains(msg, "--replay") {
		t.Errorf("the refusal does not explain the alternative: %v", err)
	}
}

// --quiet drops the running commentary and keeps the verdict. It is a third
// point on the same axis as the wheel and --plain, not a variant of either:
// a script wants the result without fifty rejections scrolling past it.
func TestMahoragaQuiet(t *testing.T) {
	requireGoToolchain(t)
	prog, input, expected := mahoragaArena(t)
	dir := filepath.Dir(prog)

	run := func(extra ...string) string {
		t.Helper()
		args := append([]string{prog, input, expected, "--turns=1", "--runs=3", "--screen-runs=2",
			"-o", filepath.Join(dir, "adapted"), "--recipe", filepath.Join(dir, "r.json")}, extra...)
		_, _, _, opts, err := parseMahoragaArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := Mahoraga(prog, input, expected, opts, nil, &stdout, &stderr); code != 0 {
			t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	loud := run("--plain")
	quiet := run("--quiet")

	// The commentary goes. The markers are ones only the per-candidate report
	// produces — "baseline" alone appears in the verdict too, which is exactly
	// the sort of thing that makes a test pass while proving nothing.
	for _, gone := range []string{"turn 1", "mean ·"} {
		if !strings.Contains(loud, gone) {
			t.Fatalf("--plain is missing %q, so this test proves nothing:\n%s", gone, loud)
		}
		if strings.Contains(quiet, gone) {
			t.Errorf("--quiet still prints the running commentary %q:\n%s", gone, quiet)
		}
	}
	// The verdict stays. A flag that silenced it would leave nothing but an
	// exit code to interpret.
	for _, kept := range []string{"BASELINE UNBEATEN", "binary", "recipe"} {
		if !strings.Contains(quiet, kept) {
			t.Errorf("--quiet dropped %q from the verdict:\n%s", kept, quiet)
		}
	}
	if len(quiet) >= len(loud) {
		t.Errorf("--quiet (%d bytes) is not shorter than --plain (%d)", len(quiet), len(loud))
	}
}

// Both spellings, and the short form.
func TestMahoragaQuietParsing(t *testing.T) {
	for _, flag := range []string{"--quiet", "-q"} {
		_, _, _, o, err := parseMahoragaArgs([]string{"p", "i", "e", flag})
		if err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if !o.Quiet {
			t.Errorf("%s did not set Quiet", flag)
		}
		// Quiet is not the wheel, whatever the terminal looks like.
		if wantsWheel(o, nil, nil) {
			t.Errorf("%s still chose the wheel", flag)
		}
	}
}

// --runs has to reach the measurement, not merely the options struct. Both
// spellings, and the count the search actually uses.
func TestMahoragaRunsReachesTheSearch(t *testing.T) {
	for _, args := range [][]string{
		{"p", "i", "e", "--runs", "17"},
		{"p", "i", "e", "--runs=17"},
	} {
		_, _, _, o, err := parseMahoragaArgs(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if o.BaselineRuns != 17 {
			t.Fatalf("%v: BaselineRuns = %d, want 17", args, o.BaselineRuns)
		}
	}

	// Unset means the package default rather than zero runs.
	_, _, _, o, err := parseMahoragaArgs([]string{"p", "i", "e"})
	if err != nil {
		t.Fatal(err)
	}
	if o.BaselineRuns != 0 {
		t.Errorf("BaselineRuns = %d with no flag; zero is what lets Options pick the default", o.BaselineRuns)
	}
	if got := (mahoraga.Options{}).BaselineRuns; got != 0 {
		t.Errorf("the zero Options carries %d baseline runs", got)
	}
}

// Every flag the help text advertises for mahoraga must actually parse. A help
// message that lists a flag the parser rejects is worse than one that omits it.
func TestHelpListsOnlyFlagsMahoragaAccepts(t *testing.T) {
	var help bytes.Buffer
	usage(&help)
	section := help.String()
	i := strings.Index(section, "Mahoraga flags")
	if i < 0 {
		t.Fatal("the help text has no mahoraga section")
	}
	section = section[i:]
	if j := strings.Index(section, "\nExamples:"); j >= 0 {
		section = section[:j]
	}

	needsValue := map[string]string{
		"--runs": "3", "--screen-runs": "2", "--min-effect": "0.05", "--turns": "4",
		"--tier": "guarded", "--seed": "7", "-o": "bin", "--recipe": "r.json",
		"--replay": "r.json", "--verify": "r.json",
	}
	seen := 0
	for _, line := range strings.Split(section, "\n") {
		for _, word := range strings.Fields(line) {
			word = strings.TrimSuffix(strings.TrimSuffix(word, ","), ":")
			if !strings.HasPrefix(word, "-") || word == "-" {
				continue
			}
			// The tier line spells its values as `general|guarded|pinned`.
			if strings.Contains(word, "|") {
				continue
			}
			args := []string{word}
			if v, ok := needsValue[word]; ok {
				args = append(args, v)
			}
			// --replay and --verify work from the recipe, so they take at most
			// an input and an expected output rather than all three files.
			if word != "--replay" && word != "--verify" {
				args = append(args, "p", "i", "e")
			}
			if _, _, _, _, err := parseMahoragaArgs(args); err != nil {
				t.Errorf("the help text advertises %q, which mahoraga rejects: %v", word, err)
			}
			seen++
		}
	}
	if seen < 10 {
		t.Errorf("only %d flags were checked; the section parsing is wrong", seen)
	}
}

// With nothing adapted the champion *is* the baseline binary, so the two final
// figures are one program measured twice. Printing them as "baseline" against
// "best" with a ratio invents a comparison — it read "best 1.95ms (0.95×)" for
// a binary identical to the one on the line above, which is the same mistake
// the wheel used to make with a drifted champion.
func TestVerdictDoesNotCompareABinaryWithItself(t *testing.T) {
	r := &mahoraga.Recipe{
		Version:       mahoraga.RecipeVersion,
		Baseline:      mahoraga.MeasurementJSON{MeanNanos: int64(1850 * time.Microsecond), Runs: 9},
		FinalBaseline: mahoraga.MeasurementJSON{MeanNanos: int64(1850 * time.Microsecond), Runs: 9},
		Champion:      mahoraga.MeasurementJSON{MeanNanos: int64(1950 * time.Microsecond), Runs: 9},
		Speedup:       0.949,
		NoiseFloorPct: 0.7,
	}
	var out bytes.Buffer
	writeMahoragaVerdict(&out, r, mahoraga.Options{Out: "bin", Recipe: "r.json", Input: "in"})
	got := out.String()

	if !strings.Contains(got, "BASELINE UNBEATEN") {
		t.Errorf("a search that kept nothing did not say so:\n%s", got)
	}
	if strings.Contains(got, "0.95×") || strings.Contains(got, "best      ") {
		t.Errorf("the verdict compares the baseline binary with itself:\n%s", got)
	}
	// What the pair does show is how repeatable the measurement was, and it is
	// labelled as that.
	if !strings.Contains(got, "measured twice") {
		t.Errorf("the two figures are not explained as one build measured twice:\n%s", got)
	}
}
