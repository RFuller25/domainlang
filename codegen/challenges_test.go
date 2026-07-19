package codegen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/codegen"
)

// TestCompiledChallengesMatchInterpreter compiles every challenges/ program
// and requires byte-identical stdout against the interpreter oracle, in both
// optimizer modes — the compiled-backend half of the challenge suite's
// end-to-end regression net (interpreter goldens live in cmd/domain).
func TestCompiledChallengesMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	dir := "../challenges"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".domain") {
			continue
		}
		ran++
		base := strings.TrimSuffix(e.Name(), ".domain")
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		input, err := os.ReadFile(filepath.Join(dir, base+".input"))
		if err != nil {
			t.Fatalf("every challenge needs a sibling .input file: %v", err)
		}
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			optimize := optimize
			t.Run(base+"/"+mode, func(t *testing.T) {
				t.Parallel()
				// The binary runs from an empty temp dir, so the named input
				// file is absent and Read Source falls back to the piped
				// stdin — the same bytes the interpreter oracle reads.
				pipe := compilePipeline(t, string(src), optimize)
				want := runInterpreter(t, pipe, input)
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
	if ran < 13 {
		t.Fatalf("expected at least 13 challenges, found %d", ran)
	}
}
