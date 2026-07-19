package prims

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"domain/ir"
)

// runErr resolves and runs src, returning the first error (resolve or runtime)
// or the final value.
func runErr(t *testing.T, src, stdin string) (ir.Value, error) {
	t.Helper()
	pipe, err := resolveSrc(t, src)
	if err != nil {
		return nil, err
	}
	ctx := &ir.Context{Stdin: strings.NewReader(stdin), Stdout: &strings.Builder{}}
	var cur ir.Value
	for _, n := range pipe.Nodes {
		cur, err = n.Eval(ctx, cur)
		if err != nil {
			return nil, err
		}
	}
	return cur, nil
}

func TestMatchPatternNamedRecordOneMode(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{a:int}-{b:int}\"\n"
	v, _ := runPipeline(t, src, "2-4")
	rec, ok := v.(*ir.RecordValue)
	if !ok {
		t.Fatalf("expected RecordValue, got %T", v)
	}
	if a, _ := rec.Get("a"); a.(int64) != 2 {
		t.Fatalf("field a: %v", a)
	}
	if b, _ := rec.Get("b"); b.(int64) != 4 {
		t.Fatalf("field b: %v", b)
	}
}

func TestMatchPatternPositionalList(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{int}x{int}x{int}\"\n"
	v, _ := runPipeline(t, src, "2x3x4")
	xs, ok := v.([]ir.Value)
	if !ok || len(xs) != 3 {
		t.Fatalf("expected 3-tuple, got %T %v", v, v)
	}
	if xs[0].(int64) != 2 || xs[2].(int64) != 4 {
		t.Fatalf("captures: %v", xs)
	}
}

func TestMatchPatternWordAndText(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{cmd:word}: {rest:text}\"\n"
	v, _ := runPipeline(t, src, "note: hello world")
	rec := v.(*ir.RecordValue)
	if c, _ := rec.Get("cmd"); c.(string) != "note" {
		t.Fatalf("cmd: %v", c)
	}
	if r, _ := rec.Get("rest"); r.(string) != "hello world" {
		t.Fatalf("rest: %v", r)
	}
}

func TestMatchPatternEachProducesRecordList(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{dir:word} {n:int}\"\n"
	v, _ := runPipeline(t, src, "forward 5\nup 3")
	list, ok := v.([]ir.Value)
	if !ok || len(list) != 2 {
		t.Fatalf("expected 2 records, got %T %v", v, v)
	}
	r0 := list[0].(*ir.RecordValue)
	if d, _ := r0.Get("dir"); d.(string) != "forward" {
		t.Fatalf("dir: %v", d)
	}
	if n, _ := r0.Get("n"); n.(int64) != 5 {
		t.Fatalf("n: %v", n)
	}
}

func TestMatchPatternNoMatchIsRuntimeError(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{a:int}-{b:int}\"\n"
	_, err := runErr(t, src, "not a range")
	if err == nil {
		t.Fatal("expected a no-match runtime error")
	}
	if !strings.Contains(err.Error(), "does not match template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestMatchPatternRoundTrip is a property test: format a string from random
// values using the exact shape of a named template, parse it back, and
// confirm the captured fields equal the original values (Match Pattern
// parse -> render round-trips).
func TestMatchPatternRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{a:int}-{b:int},{c:word}\"\n"
	for iter := 0; iter < 200; iter++ {
		a := int64(rng.Intn(2001) - 1000)
		b := int64(rng.Intn(2001) - 1000)
		c := fmt.Sprintf("w%d", rng.Intn(1000))
		input := fmt.Sprintf("%d-%d,%s", a, b, c)

		v, _ := runPipeline(t, src, input)
		rec, ok := v.(*ir.RecordValue)
		if !ok {
			t.Fatalf("iter %d: expected RecordValue, got %T", iter, v)
		}
		if got, _ := rec.Get("a"); got.(int64) != a {
			t.Fatalf("iter %d: field a: got %v want %d", iter, got, a)
		}
		if got, _ := rec.Get("b"); got.(int64) != b {
			t.Fatalf("iter %d: field b: got %v want %d", iter, got, b)
		}
		if got, _ := rec.Get("c"); got.(string) != c {
			t.Fatalf("iter %d: field c: got %v want %q", iter, got, c)
		}
	}
}

func TestMatchPatternResolveErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"bad hole type",
			"Cursed Energy: stdin\nCursed Technique: Match Pattern\n    Using: \"{x:float}\"\n",
			"unknown hole type",
		},
		{
			"mode each on text",
			"Cursed Energy: stdin\nCursed Technique: Match Pattern\n    Mode: Each\n    Using: \"{a:int}\"\n",
			"Mode: Each expects List<Text>",
		},
		{
			"missing template",
			"Cursed Energy: stdin\nCursed Technique: Match Pattern\n",
			"requires a template",
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
