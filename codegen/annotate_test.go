package codegen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/codegen"
	"domain/ir"
)

// EmitAnnotated exists so the visualizer can point at the Go a stage became.
// Everything it promises rests on one property: the source it returns is the
// source a build would produce. Markers go in and come back out.
func TestEmitAnnotatedMatchesEmitProgram(t *testing.T) {
	for _, name := range []string{
		"day1.domain", "day1_shikigami.domain", "day4.domain",
		"day5_full.domain", "day8_full.domain", "aoc2020_day1_part2.domain",
	} {
		src, err := os.ReadFile(filepath.Join("../testdata", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		for _, optimize := range []bool{true, false} {
			pipe := compilePipeline(t, string(src), optimize)
			want, err := codegen.EmitProgram(pipe, codegen.Options{})
			if err != nil {
				t.Fatalf("%s: EmitProgram: %v", name, err)
			}
			got, spans, err := codegen.EmitAnnotated(pipe, codegen.Options{})
			if err != nil {
				t.Fatalf("%s: EmitAnnotated: %v", name, err)
			}
			if got != want {
				t.Errorf("%s (optimize=%v): annotated source differs from the compiled one", name, optimize)
			}
			if strings.Contains(got, "//domain:") {
				t.Errorf("%s: a marker comment survived into the source", name)
			}
			if len(spans) == 0 {
				t.Errorf("%s: no node was attributed any code", name)
			}
			lines := strings.Split(got, "\n")
			for node, span := range spans {
				if span.Start < 1 || span.End > len(lines)+1 || span.Start >= span.End {
					t.Errorf("%s: %s has span %+v, outside the %d-line source",
						name, node.Prim, span, len(lines))
				}
			}
		}
	}
}

// The spans have to point at the right code, not merely at code: a node's lines
// are the ones its own lowering wrote.
func TestEmitAnnotatedSpansPointAtTheNodesCode(t *testing.T) {
	pipe := compilePipeline(t, `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Simple Domain: Repeat 3
    Cursed Technique: Apply
        Using: (v) -> v
Maximum Technique: Sum
Reveal: stdout
`, true)
	src, spans, err := codegen.EmitAnnotated(pipe, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(src, "\n")
	text := func(n *ir.Node) string {
		span, ok := spans[n]
		if !ok {
			t.Fatalf("%s was attributed no code", n.Prim)
		}
		return strings.Join(lines[span.Start-1:span.End-1], "\n")
	}

	var loop, reveal *ir.Node
	for _, n := range pipe.Nodes {
		switch {
		case strings.HasPrefix(n.Prim, "Simple Domain"):
			loop = n
		case n.Prim == "Emit":
			reveal = n
		}
	}
	if loop == nil || reveal == nil {
		t.Fatal("expected a loop and a Reveal in this program")
	}
	if got := text(loop); !strings.Contains(got, "for ") {
		t.Errorf("the loop should have compiled to a for statement, got:\n%s", got)
	}
	if got := text(reveal); !strings.Contains(got, "Print") {
		t.Errorf("Reveal should have compiled to a print, got:\n%s", got)
	}
	// A node's span holds its own code and stops there: the loop's lines do not
	// swallow the Reveal that follows it.
	if strings.Contains(text(loop), "Print") {
		t.Errorf("the loop's span ran past the loop:\n%s", text(loop))
	}
	// The body's own node is attributed inside the loop's span.
	body, _ := loop.Meta["nodes"].([]*ir.Node)
	if len(body) == 0 {
		t.Fatal("the loop should carry its body")
	}
	inner, ok := spans[body[0]]
	if !ok {
		t.Fatal("the loop body's node was attributed no code")
	}
	if outer := spans[loop]; inner.Start <= outer.Start || inner.End > outer.End {
		t.Errorf("the body's span %+v should sit inside the loop's %+v", inner, outer)
	}
}

// A program the backend cannot compile reports why, rather than handing back
// half a file — the visualizer shows that message in place of the code.
func TestEmitAnnotatedReportsUnsupported(t *testing.T) {
	pipe := compilePipeline(t, "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nMaximum Technique: Count\n", true)
	pipe.Nodes[len(pipe.Nodes)-1].Prim = "Nonexistent Primitive"
	if _, _, err := codegen.EmitAnnotated(pipe, codegen.Options{}); err == nil {
		t.Error("expected an unsupported-primitive error")
	}
}
