package prims

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"domain/ir"
)

func TestReadSourceFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pipe, err := resolveSrc(t, "Cursed Energy: "+path+"\nReveal: stdout\n")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &ir.Context{}
	v, err := pipe.Nodes[0].Eval(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.(string) != "hello" {
		t.Fatalf("Read Source: got %q want %q (trailing newline should be trimmed)", v, "hello")
	}
}

func TestReadSourceFallsBackToStdinWhenFileMissing(t *testing.T) {
	pipe, err := resolveSrc(t, "Cursed Energy: does-not-exist.txt\nReveal: stdout\n")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &ir.Context{Stdin: strings.NewReader("from stdin")}
	v, err := pipe.Nodes[0].Eval(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.(string) != "from stdin" {
		t.Fatalf("expected stdin fallback for a missing file, got %q", v)
	}
}

// TestReadSourceDoesNotMaskRealErrors is a regression test. readSourceData
// used to fall back to stdin on *any* os.ReadFile error, not just "file
// does not exist" — so pointing Cursed Energy at a directory (a real,
// distinct failure) silently read unrelated stdin content instead of
// reporting the actual problem.
func TestReadSourceDoesNotMaskRealErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.ReadFile error semantics on directories differ on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "a-directory")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	pipe, err := resolveSrc(t, "Cursed Energy: "+target+"\nReveal: stdout\n")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &ir.Context{Stdin: strings.NewReader("WRONG DATA FROM STDIN")}
	_, err = pipe.Nodes[0].Eval(ctx, nil)
	if err == nil {
		t.Fatal("expected an error reading a directory as source, not a silent stdin fallback")
	}
	if strings.Contains(err.Error(), "WRONG DATA") {
		t.Fatalf("error should not contain stdin content: %v", err)
	}
}

func TestReadSourceNoStdinAndNoFile(t *testing.T) {
	pipe, err := resolveSrc(t, "Cursed Energy: does-not-exist.txt\nReveal: stdout\n")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &ir.Context{} // no Stdin at all
	if _, err := pipe.Nodes[0].Eval(ctx, nil); err == nil {
		t.Fatal("expected an error when neither the file nor stdin is available")
	}
}

func TestReadSourceRelativeToBaseDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rel.txt"), []byte("relative"), 0o644); err != nil {
		t.Fatal(err)
	}
	pipe, err := resolveSrc(t, "Cursed Energy: rel.txt\nReveal: stdout\n")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &ir.Context{BaseDir: dir}
	v, err := pipe.Nodes[0].Eval(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.(string) != "relative" {
		t.Fatalf("got %q want %q", v, "relative")
	}
}

func TestSplitAndSplitEach(t *testing.T) {
	pos := tokenPos()
	node, err := split.Build(opWithString("Split Text", ","), ArgSet{}, ir.Text(), pos)
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, node, "a,b,c").([]ir.Value)
	if len(out) != 3 || out[1] != "b" {
		t.Fatalf("split: %v", out)
	}

	seNode, err := splitEach.Build(opWithString("Split Each", "-"), ArgSet{}, ir.List(ir.Text()), pos)
	if err != nil {
		t.Fatal(err)
	}
	in := []ir.Value{"a-b", "c-d-e"}
	seOut := runNode(t, seNode, in).([]ir.Value)
	if len(seOut) != 2 {
		t.Fatalf("split each: %v", seOut)
	}
	first := seOut[0].([]ir.Value)
	if len(first) != 2 || first[0] != "a" || first[1] != "b" {
		t.Fatalf("split each group 0: %v", first)
	}
}

func TestSplitMissingSeparator(t *testing.T) {
	pos := tokenPos()
	if _, err := split.Build(opWords("Split", "Text"), ArgSet{}, ir.Text(), pos); err == nil {
		t.Fatal("expected an error for Split with no separator")
	}
}

func TestSplitWrongInputType(t *testing.T) {
	pos := tokenPos()
	if _, err := split.Build(opWithString("Split Text", ","), ArgSet{}, ir.Int(), pos); err == nil {
		t.Fatal("expected a type error for Split on Int")
	}
}

func TestConvertToIntegersFlatAndNested(t *testing.T) {
	pos := tokenPos()
	flatNode, err := convertToIntegers.Build(opWords("Convert", "List", "to", "Integers"), ArgSet{}, ir.List(ir.Text()), pos)
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, flatNode, []ir.Value{"1", "2", "3"}).([]ir.Value)
	if len(out) != 3 || out[0].(int64) != 1 || out[2].(int64) != 3 {
		t.Fatalf("flat convert: %v", out)
	}

	nestedNode, err := convertToIntegers.Build(opWords("Convert", "Each", "List", "to", "Integers"), ArgSet{}, ir.List(ir.List(ir.Text())), pos)
	if err != nil {
		t.Fatal(err)
	}
	nestedIn := []ir.Value{[]ir.Value{"1", "2"}, []ir.Value{"3"}}
	nestedOut := runNode(t, nestedNode, nestedIn).([]ir.Value)
	if len(nestedOut) != 2 {
		t.Fatalf("nested convert: %v", nestedOut)
	}
	g0 := nestedOut[0].([]ir.Value)
	if len(g0) != 2 || g0[0].(int64) != 1 || g0[1].(int64) != 2 {
		t.Fatalf("nested convert group 0: %v", g0)
	}
}

func TestConvertToIntegersWrongTypeMentionsBothShapes(t *testing.T) {
	// Regression test: the wrong-type error used to always report the
	// nested List<List<Text>> form via typeErr, even when the flat
	// List<Text> form is also accepted, which produced a misleading
	// message that never mentioned List<Text>.
	pos := tokenPos()
	_, err := convertToIntegers.Build(opWords("Convert", "List", "to", "Integers"), ArgSet{}, ir.Text(), pos)
	if err == nil {
		t.Fatal("expected a type error for Convert To Integers on Text")
	}
	if !strings.Contains(err.Error(), "List<Text>") || !strings.Contains(err.Error(), "List<List<Text>>") {
		t.Fatalf("expected error to mention both List<Text> and List<List<Text>>, got %q", err.Error())
	}
}

func TestConvertToIntegersRejectsNonNumeric(t *testing.T) {
	pos := tokenPos()
	node, err := convertToIntegers.Build(opWords("Convert", "List", "to", "Integers"), ArgSet{}, ir.List(ir.Text()), pos)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &ir.Context{}
	if _, err := node.Eval(ctx, []ir.Value{"1", "not-a-number"}); err == nil {
		t.Fatal("expected an error converting a non-numeric string")
	}
}

func TestConvertToIntegersTrimsWhitespace(t *testing.T) {
	n, err := parseIntValue("  42  ")
	if err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Fatalf("got %d want 42", n)
	}
}

func TestSumEmptyList(t *testing.T) {
	pos := tokenPos()
	node, err := sum.Build(opWords("Sum"), ArgSet{}, ir.List(ir.Int()), pos)
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, node, []ir.Value{})
	if out.(int64) != 0 {
		t.Fatalf("sum of empty list: got %v want 0", out)
	}
}

func TestSelectTopKMoreThanAvailable(t *testing.T) {
	pos := tokenPos()
	op := opWords("Select", "Top")
	op.Ints = []int64{10}
	node, err := selectTopK.Build(op, ArgSet{}, ir.List(ir.Int()), pos)
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, node, []ir.Value{int64(1), int64(2)}).([]ir.Value)
	if len(out) != 2 {
		t.Fatalf("Select Top 10 of a 2-element list: got %v", out)
	}
}

func TestSelectTopKNegativeCountErrors(t *testing.T) {
	pos := tokenPos()
	op := opWords("Select", "Top")
	op.Ints = []int64{-2}
	_, err := selectTopK.Build(op, ArgSet{}, ir.List(ir.Int()), pos)
	if err == nil || !strings.Contains(err.Error(), "non-negative count") {
		t.Fatalf("expected a non-negative-count resolve error, got %v", err)
	}
}

func TestSortAscendingAndEmpty(t *testing.T) {
	pos := tokenPos()
	op := opWords("Sort")
	node, err := sortPrim.Build(op, ArgSet{}, ir.List(ir.Int()), pos)
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, node, []ir.Value{int64(3), int64(1), int64(2)}).([]ir.Value)
	if out[0].(int64) != 1 || out[2].(int64) != 3 {
		t.Fatalf("ascending sort: %v", out)
	}
	empty := runNode(t, node, []ir.Value{}).([]ir.Value)
	if len(empty) != 0 {
		t.Fatalf("sort of empty list: %v", empty)
	}
}

func TestEmitRequiresUpstreamValue(t *testing.T) {
	pos := tokenPos()
	if _, err := emit.Build(opWords(), ArgSet{}, nil, pos); err == nil {
		t.Fatal("expected an error for Reveal with an empty pipeline")
	}
}
