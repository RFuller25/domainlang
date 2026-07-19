package codegen

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domain/ir"
	"domain/pattern"
)

// Unit tests for the pieces the e2e oracle suites only exercise incidentally:
// fastEligible's edge templates, fieldName sanitization, record/tuple struct
// interning, and BuildBinary's error paths.

func TestFastEligible(t *testing.T) {
	cases := []struct {
		tmpl string
		want bool
	}{
		// The canonical fast path: int holes separated by non-digit literals.
		{"{a:int}-{b:int},{c:int}-{d:int}", true},
		{"{a:int}", true},           // hole at end of template
		{"x={a:int}", true},         // leading literal is irrelevant
		{"{a:int} {b:int}", true},   // space separator cannot be stolen
		{"{a:int}{b:int}", false},   // adjacent holes need backtracking
		{"{w:word} grade {n:int}", true}, // word hole delimited by whitespace literal
		{"{w:word} {n:int}", true},  // word hole stops at the space separator
		{"{w:word}x{n:int}", false}, // word followed by non-whitespace needs backtracking
		{"{s:text}:{n:int}", false}, // text (.*) needs the regex engine
		{"{a:int}5x{b:int}", false}, // literal starts with a digit a greedy scan would steal
		{"{a:int}9", false},         // trailing digit literal, same theft
	}
	for _, c := range cases {
		tmpl, err := pattern.ParseTemplate(c.tmpl)
		if err != nil {
			t.Fatalf("ParseTemplate(%q): %v", c.tmpl, err)
		}
		if got := fastEligible(tmpl, tmpl.OutputType()); got != c.want {
			t.Errorf("fastEligible(%q) = %v, want %v", c.tmpl, got, c.want)
		}
	}
}

// TestHasLongLiteral pins hasLongLiteral to genFastParser's actual emission
// rule (strings.HasPrefix only for literal segments over 4 bytes; shorter
// ones compile to direct byte comparisons). matchParseFunc uses this to
// decide whether the "strings" import is genuinely needed for a fast-path
// template, instead of the coarser "template has any literal" test — the
// coarser test used to register an unconditional "strings" import even when
// every literal segment was short, which would fail `go build` with
// "imported and not used" if nothing else in the generated file needed
// strings (masked today because Read Source always uses it).
func TestHasLongLiteral(t *testing.T) {
	cases := []struct {
		tmpl string
		want bool
	}{
		{"{a:int}-{b:int}", false},       // 1-byte separator: direct byte compare
		{"{a:int},{b:int}", false},       // 1-byte separator
		{"{a:int}====={b:int}", true},    // 5-byte separator: over the threshold
		{"{a:int}: {b:int}", false},      // 2-byte separator, exactly at the threshold minus 2
		{"{a:int}", false},               // no literal segments at all
		{"{w:word} grade {n:int}", true}, // " grade " separator is 7 bytes: over the threshold
	}
	for _, c := range cases {
		tmpl, err := pattern.ParseTemplate(c.tmpl)
		if err != nil {
			t.Fatalf("ParseTemplate(%q): %v", c.tmpl, err)
		}
		if got := hasLongLiteral(tmpl); got != c.want {
			t.Errorf("hasLongLiteral(%q) = %v, want %v", c.tmpl, got, c.want)
		}
	}
}

func TestFieldName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc", "abc"},
		{"a_b", "a_b"},
		{"_x", "_x"},
		{"A9", "A9"},
		{"9lives", "f9lives"}, // digit cannot start a Go identifier
		{"type", "f_type"},    // Go keywords are prefixed
		{"func", "f_func"},
		{"range", "f_range"},
		{"a-b", "a_b"},   // non-identifier runes are sanitized
		{"café", "caf_"}, // non-ASCII sanitized too
		{"", "f_"},       // degenerate empty name stays a legal identifier
	}
	for _, c := range cases {
		if got := fieldName(c.in); got != c.want {
			t.Errorf("fieldName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRecordStructInterning pins the intern rules across nodes: the same
// structural type shares one generated struct, and two record types with the
// same fields in a different order intern to distinct structs (which is why
// `=` between them is rejected rather than miscompiled).
func TestRecordStructInterning(t *testing.T) {
	g := &gen{}
	ab := ir.Record(ir.Field{Name: "a", Type: ir.Int()}, ir.Field{Name: "b", Type: ir.Text()})
	abAgain := ir.Record(ir.Field{Name: "a", Type: ir.Int()}, ir.Field{Name: "b", Type: ir.Text()})
	ba := ir.Record(ir.Field{Name: "b", Type: ir.Text()}, ir.Field{Name: "a", Type: ir.Int()})

	n1, err := g.recordType(ab)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := g.recordType(abAgain)
	if err != nil {
		t.Fatal(err)
	}
	n3, err := g.recordType(ba)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != n2 {
		t.Errorf("structurally equal records interned to different structs: %q vs %q", n1, n2)
	}
	if n1 == n3 {
		t.Errorf("field order must distinguish records, both interned to %q", n1)
	}
	if len(g.decls) != 2 {
		t.Errorf("expected exactly 2 struct declarations (one per distinct type), got %d: %v", len(g.decls), g.decls)
	}

	// Tuples intern on the same rule.
	t1, err := g.tupleType(ir.Tuple(ir.Int(), ir.Int()))
	if err != nil {
		t.Fatal(err)
	}
	t2, err := g.tupleType(ir.Tuple(ir.Int(), ir.Int()))
	if err != nil {
		t.Fatal(err)
	}
	t3, err := g.tupleType(ir.Tuple(ir.Int(), ir.Text()))
	if err != nil {
		t.Fatal(err)
	}
	if t1 != t2 || t1 == t3 {
		t.Errorf("tuple interning: got %q %q %q, want first two equal and third distinct", t1, t2, t3)
	}
}

// TestBuildBinaryMissingToolchain covers the LookPath error path: with an
// empty PATH the build must fail with the message pointing at the toolchain,
// not a panic or an opaque exec error.
func TestBuildBinaryMissingToolchain(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := BuildBinary("package main\nfunc main() {}\n", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected an error with no toolchain on PATH")
	}
	if !strings.Contains(err.Error(), "Go toolchain is required") {
		t.Fatalf("error should name the missing toolchain, got: %v", err)
	}
}

// TestBuildBinaryBadSource covers the `go build` failure path: invalid
// generated source surfaces the compiler's output, prefixed recognizably.
func TestBuildBinaryBadSource(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go toolchain; skipped in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	err := BuildBinary("package main\nfunc main() { this is not go }\n", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected an error for invalid Go source")
	}
	if !strings.Contains(err.Error(), "go build failed") {
		t.Fatalf("error should be prefixed 'go build failed', got: %v", err)
	}
}
