package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTidyArgs(t *testing.T) {
	paths, opts, err := parseTidyArgs([]string{"-w", "a.domain", "b.domain"})
	if err != nil || !opts.Write || len(paths) != 2 {
		t.Errorf("paths=%v opts=%+v err=%v", paths, opts, err)
	}
	if _, _, err := parseTidyArgs(nil); err == nil {
		t.Error("no files should be an error")
	}
	if _, _, err := parseTidyArgs([]string{"-w", "-"}); err == nil {
		t.Error("-w with stdin should be an error")
	}
	if _, _, err := parseTidyArgs([]string{"--frobnicate", "a.domain"}); err == nil {
		t.Error("an unknown flag should be an error")
	}
}

func TestTidyCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.domain")
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(untidy + "\n")

	// By default the program goes to stdout, the report to stderr, and the
	// file is not touched.
	var out, errb bytes.Buffer
	if code := Tidy([]string{path}, tidyOptions{}, nil, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if out.String() != tidied+"\n" {
		t.Errorf("stdout:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "destructured 1 tuple parameter") {
		t.Errorf("stderr: %s", errb.String())
	}
	if b, _ := os.ReadFile(path); string(b) != untidy+"\n" {
		t.Error("the file was rewritten without -w")
	}

	// --check names the file and fails; after -w it passes.
	out.Reset()
	if code := Tidy([]string{path}, tidyOptions{Check: true, Quiet: true}, nil, &out, &errb); code != 1 || strings.TrimSpace(out.String()) != path {
		t.Errorf("--check: exit %d, stdout %q", code, out.String())
	}
	if code := Tidy([]string{path}, tidyOptions{Write: true, Quiet: true}, nil, &out, &errb); code != 0 {
		t.Errorf("-w: exit %d", code)
	}
	if b, _ := os.ReadFile(path); string(b) != tidied+"\n" {
		t.Errorf("after -w:\n%s", b)
	}
	out.Reset()
	if code := Tidy([]string{path}, tidyOptions{Check: true, Quiet: true}, nil, &out, &errb); code != 0 || out.Len() != 0 {
		t.Errorf("--check on a tidy file: exit %d, stdout %q", code, out.String())
	}

	// A program that does not check is an error and is left alone.
	const broken = "Cursed Energy: input.txt\nCursed Tecnique: Sum\n"
	write(broken)
	errb.Reset()
	if code := Tidy([]string{path}, tidyOptions{Write: true}, nil, &out, &errb); code != 1 || errb.Len() == 0 {
		t.Errorf("broken program: exit %d, stderr %q", code, errb.String())
	}
	if b, _ := os.ReadFile(path); string(b) != broken {
		t.Error("a program that does not check was rewritten")
	}

	// stdin to stdout.
	out.Reset()
	if code := Tidy([]string{"-"}, tidyOptions{Quiet: true}, strings.NewReader(untidy+"\n"), &out, &errb); code != 0 || out.String() != tidied+"\n" {
		t.Errorf("stdin: exit %d\n%s", code, out.String())
	}
}
