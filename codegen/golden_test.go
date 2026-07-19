package codegen_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"domain/codegen"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden generated-source snapshots")

// TestEmittedSourceGolden snapshots the generated Go for the Day 1 anchor
// (optimized mode, so the quickselect rewrite is in the emitted code). It
// catches accidental codegen churn in review: an intentional change re-blesses
// the snapshot with `go test ./codegen -run TestEmittedSourceGolden -update`.
func TestEmittedSourceGolden(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "testdata", "day1.domain"))
	if err != nil {
		t.Fatal(err)
	}
	pipe := compilePipeline(t, string(src), true)
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}

	goldenPath := filepath.Join("testdata", "day1_optimized_go.golden")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(goSrc), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden updated: %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("missing golden snapshot (run with -update to create it): %v", err)
	}
	if goSrc != string(want) {
		t.Errorf("generated source changed; if intentional, re-bless with -update\n--- got ---\n%s", goSrc)
	}
}
