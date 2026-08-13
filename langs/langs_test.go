package langs

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every entry has to be startable. An incomplete spec is a language that
// lexes — the set is what the lexer reads — and then fails at the point of
// running, which is the latest possible moment to find out.
func TestEverySpecIsComplete(t *testing.T) {
	if len(specs) == 0 {
		t.Fatal("the table is empty")
	}
	for _, s := range specs {
		t.Run(s.Name, func(t *testing.T) {
			if s.Name == "" || s.File == "" || s.Env == "" {
				t.Errorf("incomplete: %+v", s)
			}
			if len(s.Candidates) == 0 {
				t.Error("no PATH candidates, so the runtime can only ever be found through the env override")
			}
			if !strings.HasPrefix(s.Env, "DOMAIN_") {
				t.Errorf("env override %q should be namespaced DOMAIN_*", s.Env)
			}
			// Without one of these the runtime is started and never told
			// which program to run.
			if !s.AppendProg && len(s.Args) == 0 {
				t.Error("neither AppendProg nor Args: the runner would get no program")
			}
			if len(s.Exts) == 0 {
				t.Error("no extensions, so battle could never infer --lang from a filename")
			}
			for _, e := range s.Exts {
				if !strings.HasPrefix(e, ".") {
					t.Errorf("extension %q should start with a dot", e)
				}
			}
			if filepath.Ext(s.File) == "" {
				t.Errorf("program file %q has no extension; a Go file must end in .go to compile, "+
					"and the others are named for whoever reads a stack trace", s.File)
			}
		})
	}
}

// Names must be unique case-insensitively, since Lookup matches that way, and
// so must extensions, since ByExt returns the first match.
func TestNamesAndExtensionsAreUnique(t *testing.T) {
	seenName := map[string]string{}
	seenExt := map[string]string{}
	for _, s := range specs {
		key := strings.ToLower(s.Name)
		if prev, ok := seenName[key]; ok {
			t.Errorf("%s and %s collide case-insensitively, but Lookup matches that way", prev, s.Name)
		}
		seenName[key] = s.Name
		for _, e := range s.Exts {
			if prev, ok := seenExt[e]; ok {
				t.Errorf("%s is claimed by both %s and %s", e, prev, s.Name)
			}
			seenExt[e] = s.Name
		}
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"Python", "python", "PYTHON", "pYtHoN"} {
		s, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) found nothing", name)
		}
		if s.Name != "Python" {
			t.Errorf("Lookup(%q).Name = %q, want the canonical Python", name, s.Name)
		}
	}
	if _, ok := Lookup("Fortran"); ok {
		t.Error("Lookup accepted a language that is not in the table")
	}
	if _, ok := Lookup(""); ok {
		t.Error("Lookup accepted the empty name")
	}
}

func TestByExt(t *testing.T) {
	cases := map[string]string{
		"prog.py":         "Python",
		"main.go":         "Go",
		"rival.weave":     "Weave",
		"rival.wv":        "Weave",
		"a/b/thing.crust": "cRust",
		"UPPER.PY":        "Python",
	}
	for path, want := range cases {
		s, ok := ByExt(path)
		if !ok {
			t.Errorf("ByExt(%q) found nothing, want %s", path, want)
			continue
		}
		if s.Name != want {
			t.Errorf("ByExt(%q) = %s want %s", path, s.Name, want)
		}
	}
	for _, path := range []string{"prog", "prog.domain", "prog.txt", ""} {
		if s, ok := ByExt(path); ok {
			t.Errorf("ByExt(%q) = %s, want no match", path, s.Name)
		}
	}
}

// The env override is what makes the feature usable where a runtime is not on
// PATH under its usual name, and it must accept a command with arguments.
func TestBinaryHonorsTheEnvOverride(t *testing.T) {
	s, _ := Lookup("Python")
	t.Setenv(s.Env, "uv run python")
	got, err := s.Binary()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"uv", "run", "python"}
	if len(got) != len(want) {
		t.Fatalf("Binary() = %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Binary() = %v want %v", got, want)
		}
	}
}

// A missing runtime is a distinct error type, because it is not a failure of
// the user's program and must not be reported as one.
func TestMissingRuntimeSaysWhereToGetIt(t *testing.T) {
	s, _ := Lookup("Weave")
	t.Setenv(s.Env, "")
	// Point the lookup at a name nothing will have.
	s.Candidates = []string{"weave-that-is-not-installed-anywhere"}
	_, err := s.Binary()
	if err == nil {
		t.Fatal("expected an error for a missing runtime")
	}
	var notInstalled *NotInstalledError
	if !asNotInstalled(err, &notInstalled) {
		t.Fatalf("error is not a *NotInstalledError: %T", err)
	}
	msg := err.Error()
	for _, want := range []string{"weave-that-is-not-installed", "DOMAIN_WEAVE", "github.com/malleum/weavelang"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message does not mention %q: %s", want, msg)
		}
	}
}

func asNotInstalled(err error, target **NotInstalledError) bool {
	if e, ok := err.(*NotInstalledError); ok {
		*target = e
		return true
	}
	return false
}

// The invocation each language actually gets. These are the contracts the
// runtimes document, and getting one wrong is a language that silently never
// runs — so they are pinned rather than left to the table's shape.
func TestCommandShapes(t *testing.T) {
	// A fixed binary, so the assertion is about the arguments rather than
	// about what happens to be installed here.
	cases := []struct {
		lang string
		want []string // after the binary
	}{
		{"Python", []string{"/dir/program.py"}},
		{"Go", []string{"run", "."}},
		{"cRust", []string{"/dir/program.crust"}},
		{"rask", []string{"/dir/program.rask"}},
		// `weave run file.weave` is what weave's own CLI documents as
		// "compile and run, feeding stdin to Source".
		{"Weave", []string{"run", "/dir/program.weave"}},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			s, ok := Lookup(tc.lang)
			if !ok {
				t.Fatalf("%s is not in the table", tc.lang)
			}
			t.Setenv(s.Env, "/fake/bin")
			argv, extra, err := s.Command("/dir")
			if err != nil {
				t.Fatal(err)
			}
			if argv[0] != "/fake/bin" {
				t.Errorf("binary = %q", argv[0])
			}
			got := argv[1:]
			if len(got) != len(tc.want) {
				t.Fatalf("args = %v want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("args = %v want %v", got, tc.want)
				}
			}
			if tc.lang == "Go" && extra["go.mod"] == "" {
				t.Error("Go needs a go.mod beside the program")
			}
		})
	}
}

// Command writes into a throwaway directory; CommandFor runs a file where it
// already lies, which is what battle needs.
func TestCommandForRunsTheFileInPlace(t *testing.T) {
	s, _ := Lookup("Weave")
	t.Setenv(s.Env, "/fake/weave")
	argv, err := s.CommandFor("/home/me/rival.weave")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/fake/weave", "run", "/home/me/rival.weave"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Errorf("CommandFor = %v want %v", argv, want)
	}
}

// Command must not hand back the table's own maps: a caller adding a file to
// the extras would otherwise change every later invocation of that language.
func TestCommandCopiesTheExtras(t *testing.T) {
	s, _ := Lookup("Go")
	t.Setenv(s.Env, "/fake/go")
	_, extra, err := s.Command("/dir")
	if err != nil {
		t.Fatal(err)
	}
	extra["sneaky.txt"] = "hello"
	_, again, err := s.Command("/dir")
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := again["sneaky.txt"]; leaked {
		t.Error("Command exposed the table's own extras map")
	}
}

// All must likewise not expose the table.
func TestAllCopies(t *testing.T) {
	a := All()
	if len(a) == 0 {
		t.Fatal("All returned nothing")
	}
	a[0].Name = "clobbered"
	if All()[0].Name == "clobbered" {
		t.Error("All exposes the underlying table")
	}
}
