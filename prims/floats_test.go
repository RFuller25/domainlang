package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

const floatsPrelude = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \",\"\n" +
	"Channeled Energy: Convert To Floats\n"

func TestConvertToFloatsAndSum(t *testing.T) {
	src := floatsPrelude +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	v, out := runPipeline(t, src, "1.5,2.25,3")
	if v.(float64) != 6.75 {
		t.Fatalf("float sum: got %v want 6.75", v)
	}
	if strings.TrimSpace(out) != "6.75" {
		t.Fatalf("revealed %q, want 6.75", out)
	}
}

func TestConvertToFloatsFromInts(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Channeled Energy: Convert To Floats\n" +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> x / 2.0\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if v.(float64) != 3 {
		t.Fatalf("int-widened halves: got %v want 3", v)
	}
}

func TestFloatMinMaxProductSort(t *testing.T) {
	src := floatsPrelude +
		"Domain Expansion: Sort, Descending\n" +
		"Reveal: stdout\n"
	v, _ := runPipeline(t, src, "1.5,3.25,2")
	got := v.([]ir.Value)
	want := []float64{3.25, 2, 1.5}
	for i, w := range want {
		if got[i].(float64) != w {
			t.Fatalf("sorted[%d]: got %v want %v", i, got[i], w)
		}
	}

	for _, c := range []struct {
		op   string
		want float64
	}{
		{"Max", 3.25}, {"Min", 1.5}, {"Product", 9.75},
	} {
		v, _ := runPipeline(t, floatsPrelude+"Maximum Technique: "+c.op+"\nReveal: stdout\n", "1.5,3.25,2")
		if v.(float64) != c.want {
			t.Fatalf("%s: got %v want %v", c.op, v, c.want)
		}
	}
}

func TestFloatMixedArithmeticPromotes(t *testing.T) {
	src := floatsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> x * 2 + 1\n" + // Int literals promote against Float x
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	// 0.5*2+1 = 2, 1.5*2+1 = 4 → 6
	v, _ := runPipeline(t, src, "0.5,1.5")
	if v.(float64) != 6 {
		t.Fatalf("promoted arithmetic: got %v want 6", v)
	}
}

func TestFloatBuiltins(t *testing.T) {
	src := floatsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> tofloat(floor(x)) + tofloat(ceil(x)) + tofloat(round(x)) + sqrt(x * x) + abs(0.0 - x)\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	// x = 2.5: floor 2 + ceil 3 + round 3 + sqrt(6.25)=2.5 + abs(-2.5)=2.5 → 13
	v, _ := runPipeline(t, src, "2.5")
	if v.(float64) != 13 {
		t.Fatalf("float builtins: got %v want 13", v)
	}
}

func TestFloatsAreNotKeyable(t *testing.T) {
	_, err := resolveSrc(t, floatsPrelude+"Cursed Technique: Unique\nReveal: stdout\n")
	if err == nil || !strings.Contains(err.Error(), "keyable") {
		t.Fatalf("Unique over floats must be rejected as unkeyable, got %v", err)
	}
	_, err = resolveSrc(t, floatsPrelude+"Channeled Energy: Convert To Set\nReveal: stdout\n")
	if err == nil {
		t.Fatal("Convert To Set over floats must be rejected")
	}
}

func TestSelectTopKRejectsFloats(t *testing.T) {
	_, err := resolveSrc(t, floatsPrelude+"Maximum Technique: Select Top 3\nReveal: stdout\n")
	if err == nil || !strings.Contains(err.Error(), "List<Int>") {
		t.Fatalf("Select Top K over floats must be rejected, got %v", err)
	}
}

func TestConvertToFloatsBadInput(t *testing.T) {
	pipe, err := resolveSrc(t, floatsPrelude+"Reveal: stdout\n")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	ctx := &ir.Context{Stdin: strings.NewReader("1.5,zap"), Stdout: &strings.Builder{}}
	var cur ir.Value
	for _, n := range pipe.Nodes {
		cur, err = n.Eval(ctx, cur)
		if err != nil {
			break
		}
	}
	if err == nil || !strings.Contains(err.Error(), `"zap" is not a number`) {
		t.Fatalf("bad float input must error cleanly, got %v", err)
	}
}
