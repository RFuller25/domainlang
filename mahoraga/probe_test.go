package mahoraga

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProbe(t *testing.T, lines string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "constants.probe")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The finding and the non-finding. A binding that held one value for a whole
// run is a constant of that run; one that moved is the loop variable, and
// pinning it would produce a program that computes something else — so it is
// dropped where it is read rather than carried and filtered later.
func TestReadProbeKeepsOnlyTheStableBindings(t *testing.T) {
	path := writeProbe(t, strings.Join([]string{
		"Consider@6:5#l\t16\t16\t50000\tfalse",
		"Consider@11:9#i\t3\t99\t50000\ttrue",
		"Consider@2:1#limit\t40000000\t40000000\t1\tfalse",
		"list:Unfold@33:5\t5000000\t5000000\t1\tfalse",
		"",
	}, "\n"))

	got, sites, err := readProbe(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("readProbe kept %d bindings, want 2: %+v", len(got), got)
	}
	if got[0].Name != "l" || got[0].Value != 16 || got[0].Line != 6 || got[0].Calls != 50000 {
		t.Errorf("first binding read back as %+v", got[0])
	}
	if got[1].Name != "limit" || got[1].Value != 40000000 {
		t.Errorf("second binding read back as %+v", got[1])
	}
	for _, c := range got {
		if c.Name == "i" {
			t.Error("a binding that varied was kept")
		}
	}
	// The list site travels in the same report and is read differently: it is
	// not a constant and is never pinned, it is a capacity.
	if len(sites) != 1 || sites[0].Length != 5000000 || sites[0].Line != 33 {
		t.Errorf("list sites read back as %+v", sites)
	}
	for _, c := range got {
		if strings.HasPrefix(c.Key, listSitePrefix) {
			t.Errorf("a list site was read back as a constant: %+v", c)
		}
	}
}

// A report the search cannot read is an error rather than an empty result:
// silently finding no constants in a file full of them is the failure mode
// that would take a day to notice.
func TestReadProbeRejectsAMalformedReport(t *testing.T) {
	for _, bad := range []string{
		"Consider@6:5#l\t16\t16\t50000",
		"Consider@6:5#l\tsixteen\t16\t50000\tfalse",
		"Consider@6:5#l\t16\t16\tmany\tfalse",
	} {
		if _, _, err := readProbe(writeProbe(t, bad+"\n")); err == nil {
			t.Errorf("readProbe accepted %q", bad)
		}
	}
}

// The key is what the compiler is handed back; the name and the line are what
// the report says. A key that does not parse must cost the report its label
// and nothing else.
func TestSplitConsiderKey(t *testing.T) {
	cases := []struct {
		key       string
		name      string
		line, col int
	}{
		{"Consider@6:5#l", "l", 6, 5},
		{"Consider@120:9#legLen", "legLen", 120, 9},
		{"nonsense", "nonsense", 0, 0},
	}
	for _, tc := range cases {
		name, line, col := splitConsiderKey(tc.key)
		if name != tc.name || line != tc.line || col != tc.col {
			t.Errorf("splitConsiderKey(%q) = %q, %d, %d; want %q, %d, %d",
				tc.key, name, line, col, tc.name, tc.line, tc.col)
		}
	}
}

// Turn 8 tries the most-evaluated bindings first and stops: each pin is a
// build and a race, and a program with forty bindings is not one where the
// fortieth is costing the time.
func TestPinnableConstantsAreOrderedAndBounded(t *testing.T) {
	s := &Search{}
	for i := range maxPinnedConstants + 4 {
		s.facts.Constants = append(s.facts.Constants, Constant{
			Key: string(rune('a'+i)) + "@1:1#x", Value: int64(i), Calls: int64(i),
		})
	}
	got := s.pinnableConstants()
	if len(got) != maxPinnedConstants {
		t.Fatalf("pinnableConstants returned %d, want %d", len(got), maxPinnedConstants)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Calls < got[i].Calls {
			t.Errorf("constants are not ordered by evaluation count: %+v", got)
		}
	}
}

// Every candidate is built from the champion, so a candidate that wrote into
// the champion's map would be editing configurations already measured — and
// the recipe would record a build nobody ran.
func TestWithConstantCopies(t *testing.T) {
	base := map[string]int64{"a@1:1#x": 1}
	next := withConstant(base, "b@2:2#y", 2)
	if len(base) != 1 {
		t.Errorf("withConstant wrote into the map it was given: %+v", base)
	}
	if next["a@1:1#x"] != 1 || next["b@2:2#y"] != 2 {
		t.Errorf("withConstant lost an entry: %+v", next)
	}
}
