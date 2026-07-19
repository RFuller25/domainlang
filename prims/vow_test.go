package prims

import (
	"strings"
	"testing"
)

func TestVowAllValuesComparisons(t *testing.T) {
	cases := []struct {
		vow     string
		stdin   string
		wantErr bool
	}{
		{"Binding Vow: All Values > 0", "1,2,3", false},
		{"Binding Vow: All Values > 5", "1,2,3", true},
		{"Binding Vow: All Values >= 1", "1,2,3", false},
		{"Binding Vow: All Values >= 2", "1,2,3", true},
		{"Binding Vow: All Values < 10", "1,2,3", false},
		{"Binding Vow: All Values < 2", "1,2,3", true},
		{"Binding Vow: All Values <= 3", "1,2,3", false},
		{"Binding Vow: All Values <= 2", "1,2,3", true},
		{"Binding Vow: All Values = 2", "2,2,2", false},
		{"Binding Vow: All Values = 2", "2,2,3", true},
	}
	for _, c := range cases {
		src := "Cursed Energy: stdin\n" +
			"Cursed Technique: Split Text by \",\"\n" +
			"Channeled Energy: Convert List to Integers\n" +
			c.vow + "\n"
		_, err := runErr(t, src, c.stdin)
		if c.wantErr && err == nil {
			t.Fatalf("%s: expected a vow violation", c.vow)
		}
		if !c.wantErr && err != nil {
			t.Fatalf("%s: unexpected error: %v", c.vow, err)
		}
	}
}

func TestVowCountEquals(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Binding Vow: Count Equals 3\n"
	if _, err := runErr(t, src, "a,b,c"); err != nil {
		t.Fatalf("expected count vow to pass: %v", err)
	}
	if _, err := runErr(t, src, "a,b"); err == nil {
		t.Fatal("expected count vow to fail")
	}
}

// TestVowCountEqualsSymbolForm covers the `Count = N` spelling, which routes
// through the OpSyms match (hasSym) rather than the word `Equals`.
func TestVowCountEqualsSymbolForm(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Binding Vow: Count = 3\n"
	if _, err := runErr(t, src, "a,b,c"); err != nil {
		t.Fatalf("expected 'Count = 3' vow to pass: %v", err)
	}
	if _, err := runErr(t, src, "a,b"); err == nil {
		t.Fatal("expected 'Count = 3' vow to fail on two items")
	}
}

func TestVowResolveErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			"count vow missing number",
			"Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nBinding Vow: Count Equals\n",
			"Count vow requires a number",
		},
		{
			"all values vow missing comparison",
			"Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nBinding Vow: All Values\n",
			"All Values vow requires a comparison",
		},
		{
			"unsupported vow phrase",
			"Cursed Energy: stdin\nBinding Vow: Something Weird\n",
			"unsupported Binding Vow",
		},
		{
			"vow with no upstream value",
			"Binding Vow: All Values > 0\n",
			"has no value to assert over",
		},
	}
	for _, c := range cases {
		_, err := resolveSrc(t, c.src)
		if err == nil {
			t.Fatalf("%s: expected resolve error", c.name)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error %q does not contain %q", c.name, err.Error(), c.want)
		}
	}
}

func TestVowViolationMessageIncludesActualValue(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Binding Vow: Count Equals 99\n"
	_, err := runErr(t, src, "1,2,3")
	if err == nil {
		t.Fatal("expected a vow violation")
	}
	if !strings.Contains(err.Error(), "vow violated") || !strings.Contains(err.Error(), "actual value") {
		t.Fatalf("expected a descriptive vow-violation message, got: %v", err)
	}
}

func TestVowIsPassthrough(t *testing.T) {
	// A vow must not change the value it passes through.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Binding Vow: All Values > 0\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if v.(int64) != 6 {
		t.Fatalf("vow should be transparent to downstream stages: got %v want 6", v)
	}
}

func TestVowOnNonListValueIsARuntimeError(t *testing.T) {
	// Binding Vow defers type applicability to Eval; a vow placed after a
	// non-list value must fail cleanly at runtime, not panic.
	src := "Cursed Energy: stdin\nBinding Vow: All Values > 0\n"
	_, err := runErr(t, src, "hello")
	if err == nil {
		t.Fatal("expected a runtime error for a vow over a non-list value")
	}
}
