package bench

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The case table, testdata/ and README.md each restate the same list of
// programs. These keep them from drifting apart without anyone noticing: a
// program added to testdata/ but not to the table is never timed, and a README
// that counts the wins by hand is wrong the first time a number moves.

// Every testdata program is a case, and every case has both halves of its pair.
func TestCasesMatchTestdata(t *testing.T) {
	inTable := map[string]bool{}
	for _, c := range cases {
		if inTable[c.name] {
			t.Errorf("case %q listed twice", c.name)
		}
		inTable[c.name] = true
		for _, ext := range []string{".domain", ".go"} {
			if _, err := os.Stat(filepath.Join("testdata", c.name+ext)); err != nil {
				t.Errorf("case %q: %v", c.name, err)
			}
		}
	}
	for _, ext := range []string{".domain", ".go"} {
		files, err := filepath.Glob(filepath.Join("testdata", "*"+ext))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if name := strings.TrimSuffix(filepath.Base(f), ext); !inTable[name] {
				t.Errorf("%s has no entry in the case table, so it is never timed", f)
			}
		}
	}
}

var resultRow = regexp.MustCompile("(?m)^\\| `([a-z_0-9]+)` \\| [0-9.]+ ms \\| [0-9.]+ ms \\| \\**([0-9.]+)×")

// README.md's results table lists every case once, and the prose above and
// below it states the counts that table implies.
func TestREADMEMatchesCases(t *testing.T) {
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	rows := resultRow.FindAllStringSubmatch(readme, -1)
	listed := map[string]bool{}
	faster, over := 0, 0
	for _, r := range rows {
		listed[r[1]] = true
		ratio, _ := strconv.ParseFloat(r[2], 64)
		if ratio < 1 {
			faster++
		}
		if ratio > 2 {
			over++
		}
	}
	for _, c := range cases {
		if !listed[c.name] {
			t.Errorf("README results table has no row for %q", c.name)
		}
		if !strings.Contains(readme, "| `"+c.name+"` |") {
			t.Errorf("README has no row for %q", c.name)
		}
	}
	if len(rows) != len(cases) {
		t.Errorf("README results table has %d rows, the case table %d", len(rows), len(cases))
	}

	budgeted := 0
	for _, c := range cases {
		if c.budget != 0 {
			budgeted++
		}
	}
	if over != budgeted {
		t.Errorf("README shows %d cases over 2×, the case table budgets %d", over, budgeted)
	}

	flat := strings.ToLower(strings.Join(strings.Fields(readme), " "))
	for _, want := range []string{
		spell(len(cases)-over) + " of the " + spell(len(cases)),
		spell(faster) + " of them faster",
		spell(faster) + " are faster",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("README prose should say %q (it counts by hand and has drifted)", want)
		}
	}
}

// spell writes a count the way the README does: lowercase words, 0–40.
func spell(n int) string {
	ones := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
		"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen",
		"eighteen", "nineteen"}
	if n < 20 {
		return ones[n]
	}
	tens := map[int]string{20: "twenty", 30: "thirty", 40: "forty"}
	if t, ok := tens[n/10*10]; ok {
		if n%10 == 0 {
			return t
		}
		return t + "-" + ones[n%10]
	}
	return strconv.Itoa(n)
}
