package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFmtArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantPaths []string
		wantOpts  fmtOptions
		wantErr   bool
	}{
		{"one file", []string{"a.domain"}, []string{"a.domain"}, fmtOptions{}, false},
		{"several files", []string{"a.domain", "b.domain"}, []string{"a.domain", "b.domain"}, fmtOptions{}, false},
		{"write", []string{"-w", "a.domain"}, []string{"a.domain"}, fmtOptions{Write: true}, false},
		{"long write", []string{"--write", "a.domain"}, []string{"a.domain"}, fmtOptions{Write: true}, false},
		{"list", []string{"-l", "a.domain"}, []string{"a.domain"}, fmtOptions{List: true}, false},
		{"check", []string{"--check", "a.domain"}, []string{"a.domain"}, fmtOptions{Check: true}, false},
		{"stdin", []string{"-"}, []string{"-"}, fmtOptions{}, false},
		{"no files", nil, nil, fmtOptions{}, true},
		{"only flags", []string{"-w"}, nil, fmtOptions{}, true},
		{"unknown flag", []string{"--wat", "a.domain"}, nil, fmtOptions{}, true},
		{"write with stdin", []string{"-w", "-"}, nil, fmtOptions{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			paths, opts, err := parseFmtArgs(c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if strings.Join(paths, ",") != strings.Join(c.wantPaths, ",") {
				t.Errorf("paths = %v, want %v", paths, c.wantPaths)
			}
			if opts != c.wantOpts {
				t.Errorf("opts = %+v, want %+v", opts, c.wantOpts)
			}
		})
	}
}

const unformatted = "Cursed Energy: x\nCursed Technique: Map Each\n  Using:(a)->a+1\n"
const formatted = "Cursed Energy: x\nCursed Technique: Map Each\n    Using: (a) -> a + 1\n"

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFmtToStdout(t *testing.T) {
	path := writeTemp(t, "p.domain", unformatted)
	var out, errBuf bytes.Buffer
	if code := Fmt([]string{path}, fmtOptions{}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if out.String() != formatted {
		t.Errorf("stdout = %q, want %q", out.String(), formatted)
	}
	// The file itself is untouched without -w.
	b, _ := os.ReadFile(path)
	if string(b) != unformatted {
		t.Errorf("file was modified without -w")
	}
}

func TestFmtWriteInPlace(t *testing.T) {
	path := writeTemp(t, "p.domain", unformatted)
	var out, errBuf bytes.Buffer
	if code := Fmt([]string{path}, fmtOptions{Write: true}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	b, _ := os.ReadFile(path)
	if string(b) != formatted {
		t.Errorf("file = %q, want %q", string(b), formatted)
	}
	if out.Len() != 0 {
		t.Errorf("-w should print nothing, got %q", out.String())
	}
}

func TestFmtCheckAndList(t *testing.T) {
	dirty := writeTemp(t, "dirty.domain", unformatted)
	clean := writeTemp(t, "clean.domain", formatted)

	var out, errBuf bytes.Buffer
	if code := Fmt([]string{dirty}, fmtOptions{Check: true}, nil, &out, &errBuf); code != 1 {
		t.Errorf("--check on an unformatted file: exit = %d, want 1", code)
	}
	if !strings.Contains(out.String(), dirty) {
		t.Errorf("--check should name the file, got %q", out.String())
	}

	out.Reset()
	if code := Fmt([]string{clean}, fmtOptions{Check: true}, nil, &out, &errBuf); code != 0 {
		t.Errorf("--check on a formatted file: exit = %d, want 0", code)
	}
	if out.Len() != 0 {
		t.Errorf("--check should print nothing for a clean file, got %q", out.String())
	}

	// -l lists like --check but exits 0, so it can be used informationally.
	out.Reset()
	if code := Fmt([]string{dirty}, fmtOptions{List: true}, nil, &out, &errBuf); code != 0 {
		t.Errorf("-l exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), dirty) {
		t.Errorf("-l should name the file, got %q", out.String())
	}
}

func TestFmtStdin(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := Fmt([]string{"-"}, fmtOptions{}, strings.NewReader(unformatted), &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if out.String() != formatted {
		t.Errorf("stdout = %q, want %q", out.String(), formatted)
	}
}

// A file that does not parse is reported and left exactly as it was, even
// under -w: fmt must never make a broken program worse.
func TestFmtBrokenFileIsLeftAlone(t *testing.T) {
	broken := "Cursed Energy: x\n\tReveal: stdout\n"
	path := writeTemp(t, "broken.domain", broken)
	var out, errBuf bytes.Buffer
	if code := Fmt([]string{path}, fmtOptions{Write: true}, nil, &out, &errBuf); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errBuf.String(), "tab") {
		t.Errorf("stderr should explain the failure, got %q", errBuf.String())
	}
	b, _ := os.ReadFile(path)
	if string(b) != broken {
		t.Errorf("broken file was rewritten: %q", string(b))
	}
}

func TestFmtMissingFile(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := Fmt([]string{"no-such-file.domain"}, fmtOptions{}, nil, &out, &errBuf); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if errBuf.Len() == 0 {
		t.Error("expected an error on stderr")
	}
}

// Several files in one invocation: a failure in one must not stop the others.
func TestFmtContinuesAfterFailure(t *testing.T) {
	broken := writeTemp(t, "broken.domain", "Cursed Energy: x\n\tReveal: stdout\n")
	good := writeTemp(t, "good.domain", unformatted)
	var out, errBuf bytes.Buffer
	if code := Fmt([]string{broken, good}, fmtOptions{Write: true}, nil, &out, &errBuf); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	b, _ := os.ReadFile(good)
	if string(b) != formatted {
		t.Errorf("the good file was not formatted: %q", string(b))
	}
}

func TestUsageMentionsFmt(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)
	if !strings.Contains(buf.String(), "domain fmt") {
		t.Errorf("usage should document fmt, got %q", buf.String())
	}
}

// `expansion: fix` rewrites source too (tabs, smart quotes, missing colons).
// Its output has to be fmt-clean, or the two tools would fight over a file.
func TestFixOutputIsFmtClean(t *testing.T) {
	// Tab indentation is a lex error, so fmt cannot repair it — fix can. After
	// fix, the result must need no further formatting.
	src := "Cursed Energy: x\nCursed Technique: Split Text by \"\\n\"\nCursed Technique: Map Each\n\tUsing: (a) -> a\nReveal: stdout\n"
	path := writeTemp(t, "fixme.domain", src)
	var out, errBuf bytes.Buffer
	if code := Expansion([]string{"fix"}, []string{path}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("fix exit = %d, stdout = %q, stderr = %q", code, out.String(), errBuf.String())
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Fmt([]string{path}, fmtOptions{Check: true}, nil, &out, &errBuf); code != 0 {
		t.Errorf("fix output is not fmt-clean:\n%s\nfmt would change: %s", fixed, out.String())
	}
}
