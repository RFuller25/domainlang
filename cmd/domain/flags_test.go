package main

import (
	"reflect"
	"strconv"
	"testing"
)

func TestFlagSet(t *testing.T) {
	type got struct {
		On, Off bool
		Name    string
		N       int
		Words   []string
	}
	run := func(args []string, unknown func(string) bool) (got, error) {
		g := got{Off: true}
		f := newFlagSet("bad flag %q", func(a string) error {
			g.Words = append(g.Words, a)
			return nil
		})
		if unknown != nil {
			f.isFlag = unknown
		}
		f.bool("-v --verbose", &g.On, true)
		f.bool("--quiet", &g.Off, false)
		f.str("-n --name --name=", "%s needs a name", &g.Name)
		f.val("--count --count=", "%s needs a count", parseInto(strconv.Atoi, &g.N))
		err := f.parse(args)
		return g, err
	}
	cases := []struct {
		name    string
		args    []string
		want    got
		wantErr string
	}{
		{"empty", nil, got{Off: true}, ""},
		{"bool aliases", []string{"-v"}, got{On: true, Off: true}, ""},
		{"bool false", []string{"--quiet", "--verbose"}, got{On: true}, ""},
		{"separate value", []string{"--name", "x"}, got{Off: true, Name: "x"}, ""},
		{"short alias", []string{"-n", "x"}, got{Off: true, Name: "x"}, ""},
		{"equals value", []string{"--name=x=y"}, got{Off: true, Name: "x=y"}, ""},
		{"empty equals value", []string{"--name="}, got{Off: true}, ""},
		{"short has no equals form", []string{"-n=x"}, got{Off: true}, `bad flag "-n=x"`},
		{"value may look like a flag", []string{"--name", "--verbose"}, got{Off: true, Name: "--verbose"}, ""},
		{"repeated overwrites", []string{"--name", "a", "--name=b"}, got{Off: true, Name: "b"}, ""},
		{"missing value names the alias", []string{"-n"}, got{Off: true}, "-n needs a name"},
		{"missing value after others", []string{"-v", "--count"}, got{On: true, Off: true}, "--count needs a count"},
		{"callback error", []string{"--count=x"}, got{Off: true}, `strconv.Atoi: parsing "x": invalid syntax`},
		{"first error wins", []string{"--count=x", "--bogus"}, got{Off: true}, `strconv.Atoi: parsing "x": invalid syntax`},
		{"unknown flag", []string{"--bogus"}, got{Off: true}, `bad flag "--bogus"`},
		{"bool takes no equals", []string{"--verbose=1"}, got{Off: true}, `bad flag "--verbose=1"`},
		{"dash alone is a flag by default", []string{"-"}, got{Off: true}, `bad flag "-"`},
		{"positionals in order", []string{"a", "-v", "b"}, got{On: true, Off: true, Words: []string{"a", "b"}}, ""},
		{"empty positional", []string{""}, got{Off: true, Words: []string{""}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := run(c.args, nil)
			if c.wantErr == "" && err != nil || c.wantErr != "" && (err == nil || err.Error() != c.wantErr) {
				t.Fatalf("error = %v, want %q", err, c.wantErr)
			}
			if !reflect.DeepEqual(g, c.want) {
				t.Errorf("got %+v, want %+v", g, c.want)
			}
		})
	}

	t.Run("custom flag test", func(t *testing.T) {
		g, err := run([]string{"-", "-x"}, func(a string) bool { return a == "-x" })
		if err == nil || err.Error() != `bad flag "-x"` || !reflect.DeepEqual(g.Words, []string{"-"}) {
			t.Errorf("got %+v, %v", g, err)
		}
	})
}

func TestEqualsFormsAreSeparateRegistrations(t *testing.T) {
	var sep, eq string
	f := newFlagSet("", nil)
	f.str("--d", "%s needs a value", &sep)
	f.str("--d=", "", &eq)
	if err := f.parse([]string{"--d", "a", "--d=b"}); err != nil || sep != "a" || eq != "b" {
		t.Errorf("sep=%q eq=%q err=%v", sep, eq, err)
	}
}

func TestOnePath(t *testing.T) {
	var p string
	pos := onePath(&p, "extra %q")
	if err := pos("a"); err != nil || p != "a" {
		t.Fatalf("first: %q %v", p, err)
	}
	if err := pos("b"); err == nil || err.Error() != `extra "b"` || p != "a" {
		t.Fatalf("second: %q %v", p, err)
	}
}

// The parsers that had no tests of their own, pinned to their wording.
func TestSmallParsers(t *testing.T) {
	if _, _, err := parseExplainArgs([]string{"a", "b"}); err == nil || err.Error() != `unexpected extra argument "b"` {
		t.Errorf("explain: %v", err)
	}
	if _, _, err := parseExplainArgs([]string{"--x"}); err == nil || err.Error() != `unknown flag "--x"` {
		t.Errorf("explain: %v", err)
	}
	if _, err := parseCheckArgs([]string{"-x"}); err == nil || err.Error() != `check takes no flags (got "-x")` {
		t.Errorf("check: %v", err)
	}
	if _, _, err := parseBuildArgs([]string{"p", "-o"}); err == nil || err.Error() != "-o requires a path" {
		t.Errorf("build: %v", err)
	}
	if _, _, err := parseRunArgs([]string{"p", "--record"}); err == nil || err.Error() != "--record requires a path" {
		t.Errorf("run: %v", err)
	}
	if _, _, err := parseFmtArgs([]string{"-x"}); err == nil || err.Error() != `unknown flag "-x" for fmt` {
		t.Errorf("fmt: %v", err)
	}
	if paths, _, err := parseFmtArgs([]string{"-", "a"}); err != nil || !reflect.DeepEqual(paths, []string{"-", "a"}) {
		t.Errorf("fmt: %v %v", paths, err)
	}
	if _, _, err := parseVisualizeArgs([]string{"p", "--depth=0"}); err == nil || err.Error() != "--depth needs a positive number" {
		t.Errorf("visualize: %v", err)
	}
	if _, _, err := parseVisualizeArgs([]string{"p", "--depth", "0"}); err == nil || err.Error() != `--depth needs a positive number, got "0"` {
		t.Errorf("visualize: %v", err)
	}
	if _, err := parseDocumentationArgs([]string{"-p=70000"}); err == nil || err.Error() != `invalid port "70000" (want 1-65535)` {
		t.Errorf("documentation: %v", err)
	}
	if port, err := parseDocumentationArgs([]string{"--port", "8080"}); err != nil || port != 8080 {
		t.Errorf("documentation: %v %v", port, err)
	}
	if _, err := parseVSCodeArgs([]string{"x"}); err == nil || err.Error() != `vscode takes no file argument (got "x")` {
		t.Errorf("vscode: %v", err)
	}
}
