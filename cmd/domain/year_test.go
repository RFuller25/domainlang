package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"domain/aoc"
)

const fortuneSolved = `<html><body><main>
<article class="day-desc"><h2>--- Day 1: Inverse Captcha ---</h2>
<p>Sum the digits.</p>
<pre><code>1122
</code></pre>
<ul><li>one</li></ul>
</article>
<p>Your puzzle answer was <code>1034</code>.</p>
<article class="day-desc"><h2>--- Part Two ---</h2><p>Halfway round.</p></article>
<p>Your puzzle answer was <code>1356</code>.</p>
</main></body></html>`

const fortuneFresh = `<html><body><main>
<article class="day-desc"><h2>--- Day 2: Checksum ---</h2><p>Rows.</p></article>
</main></body></html>`

func fortuneSite(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2017/day/1":
			w.Write([]byte(fortuneSolved))
		case "/2017/day/2":
			w.Write([]byte(fortuneFresh))
		case "/2017/day/1/input":
			w.Write([]byte("1122"))
		case "/2017/day/2/input":
			w.Write([]byte("5 1 9\n7 5 3\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cache := t.TempDir()
	oldClient, oldGap := newYearClient, fortuneGap
	newYearClient = func() (*aoc.Client, error) {
		return &aoc.Client{
			Session:  "s",
			BaseURL:  srv.URL,
			CacheDir: cache,
			HTTP:     srv.Client(),
			Now:      func() time.Time { return time.Date(2017, 12, 2, 12, 0, 0, 0, time.UTC) },
		}, nil
	}
	fortuneGap = 0
	t.Cleanup(func() { newYearClient, fortuneGap = oldClient, oldGap })
}

func TestYearOfFortuneLaysOutTheYear(t *testing.T) {
	fortuneSite(t)
	dir := filepath.Join(t.TempDir(), "2017")
	var out, errb bytes.Buffer
	if code := YearOfFortune(2017, yearOptions{Dir: dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return string(b)
	}
	if got := read("i1"); got != "1122\n" {
		t.Errorf("i1 = %q", got)
	}
	if got := read("o1"); got != "Part 1: 1034\nPart 2: 1356\n" {
		t.Errorf("o1 = %q", got)
	}
	md := read("d1.md")
	for _, want := range []string{"# Day 1: Inverse Captcha", "## Part Two", "```\n1122\n```", "- one"} {
		if !strings.Contains(md, want) {
			t.Errorf("d1.md missing %q:\n%s", want, md)
		}
	}
	if st := read("d1.domain"); !strings.Contains(st, "Cursed Energy: i1") || !strings.Contains(st, `Part "2":`) {
		t.Errorf("d1.domain = %q", st)
	}
	// Day 2 is unsolved: no expected output rather than an empty one.
	if _, err := os.Stat(filepath.Join(dir, "o2")); err == nil {
		t.Error("o2 written for an unsolved day")
	}
	// Day 3 is not out; nothing for it.
	if _, err := os.Stat(filepath.Join(dir, "d3.md")); err == nil {
		t.Error("d3.md written for a locked day")
	}
	if !strings.Contains(out.String(), "not out yet") {
		t.Errorf("no mention of locked days:\n%s", out.String())
	}
}

func TestYearOfFortuneKeepsExistingFiles(t *testing.T) {
	fortuneSite(t)
	dir := t.TempDir()
	mine := filepath.Join(dir, "d1.domain")
	if err := os.WriteFile(mine, []byte("my work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := YearOfFortune(2017, yearOptions{Dir: dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if b, _ := os.ReadFile(mine); string(b) != "my work\n" {
		t.Errorf("starter overwritten: %q", b)
	}
}

func TestParseYearArgs(t *testing.T) {
	now := time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC)
	if y, o, err := parseYearArgs([]string{"2017", "--dir", "x"}, now); err != nil || y != 2017 || o.Dir != "x" {
		t.Errorf("got %d %+v %v", y, o, err)
	}
	for _, bad := range [][]string{nil, {"abc"}, {"2014"}, {"2026"}, {"2017", "2018"}, {"2017", "--nope"}, {"--dir"}} {
		if _, _, err := parseYearArgs(bad, now); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestYearOfFortuneIsAnExpansionCommand(t *testing.T) {
	cmd, rest, ok := expansionInvocation([]string{"expansion:", "year", "of", "fortune", "2017"})
	if !ok || strings.Join(cmd, " ") != "year of fortune" || len(rest) != 1 {
		t.Fatalf("cmd=%v rest=%v ok=%v", cmd, rest, ok)
	}
}
