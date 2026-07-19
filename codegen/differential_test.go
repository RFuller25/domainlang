package codegen_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"domain/codegen"
)

// B.f2 — differential property testing. A seeded generator builds small
// random pipelines over the compiled surface plus random inputs, and every
// generated program must produce byte-identical stdout from the interpreter
// and the compiled binary, in both optimizer modes. Seeds are fixed so CI is
// deterministic and a failure reproduces; the failing program source and
// input are logged.
//
// The generator only composes *total* stages (no partial ops like Max on a
// possibly-empty list), so success-path equality is always the property
// under test.

// intLambda produces a random total Int -> Int lambda body.
func intLambda(r *rand.Rand) string {
	bodies := []string{
		fmt.Sprintf("(x) -> x * %d + %d", r.Intn(5)+1, r.Intn(20)-10),
		fmt.Sprintf("(x) -> abs(x) - %d", r.Intn(10)),
		fmt.Sprintf("(x) -> gcd(x, %d) + sign(x)", r.Intn(30)+1),
		fmt.Sprintf("(x) -> lcm(x, %d)", r.Intn(6)+1),
		fmt.Sprintf("(x) -> if x > %d then x else %d - x", r.Intn(40), r.Intn(10)),
		fmt.Sprintf("(x) -> modpow(abs(x), %d, %d)", r.Intn(5), r.Intn(90)+7),
	}
	return bodies[r.Intn(len(bodies))]
}

// intPredicate produces a random total Int -> Bool lambda body.
func intPredicate(r *rand.Rand) string {
	preds := []string{
		fmt.Sprintf("(x) -> x > %d", r.Intn(60)-20),
		fmt.Sprintf("(x) -> x < %d or x > %d", r.Intn(20)-10, r.Intn(50)+20),
		fmt.Sprintf("(x) -> gcd(x, %d) = 1", r.Intn(12)+2),
		"(x) -> sign(x) >= 0",
	}
	return preds[r.Intn(len(preds))]
}

// genIntPipeline builds a random program over List<Int>.
func genIntPipeline(r *rand.Rand) string {
	var sb strings.Builder
	sb.WriteString("Cursed Energy: stdin\n")
	sb.WriteString("Cursed Technique: Split Text by \"\\n\"\n")
	sb.WriteString("Channeled Energy: Convert List to Integers\n")
	for i, n := 0, r.Intn(4)+2; i < n; i++ {
		switch r.Intn(6) {
		case 0:
			fmt.Fprintf(&sb, "Cursed Technique: Map Each\n    Using: %s\n", intLambda(r))
		case 1:
			fmt.Fprintf(&sb, "Cursed Technique: Filter\n    Using: %s\n", intPredicate(r))
		case 2:
			if r.Intn(2) == 0 {
				sb.WriteString("Domain Expansion: Quicksort\n")
			} else {
				sb.WriteString("Domain Expansion: Quicksort, Descending\n")
			}
			// Sometimes chase the sort with Select Top K so the quickselect
			// rewrite fires in optimized mode and is property-tested too.
			if r.Intn(2) == 0 {
				fmt.Fprintf(&sb, "Maximum Technique: Select Top %d\n", r.Intn(6))
			}
		case 3:
			sb.WriteString("Reverse Cursed Technique: Reverse\n")
		case 4:
			sb.WriteString("Cursed Technique: Unique\n")
		case 5:
			fmt.Fprintf(&sb, "Maximum Technique: Select Top %d\n", r.Intn(8))
		}
	}
	switch r.Intn(3) {
	case 0:
		sb.WriteString("Maximum Technique: Sum\n")
	case 1:
		sb.WriteString("Maximum Technique: Count\n")
	}
	sb.WriteString("Reveal: stdout\n")
	return sb.String()
}

// genIntInput builds 1-25 random integer lines.
func genIntInput(r *rand.Rand) string {
	n := r.Intn(25) + 1
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%d", r.Intn(150)-50)
	}
	return strings.Join(lines, "\n")
}

// genGridPipeline builds a random program over a digit Grid<Int>.
func genGridPipeline(r *rand.Rand) string {
	var sb strings.Builder
	sb.WriteString("Cursed Energy: stdin\n")
	sb.WriteString("Cursed Technique: Split Text by \"\\n\"\n")
	sb.WriteString("Cursed Technique: Split Each by \"\"\n")
	sb.WriteString("Channeled Energy: Convert Each List to Integers\n")
	sb.WriteString("Channeled Energy: Convert To Grid\n")
	for i, n := 0, r.Intn(3); i < n; i++ {
		switch r.Intn(2) {
		case 0:
			sb.WriteString("Cursed Technique: Transpose\n")
		case 1:
			// Keep cells non-negative so a Dijkstra ender stays legal.
			fmt.Fprintf(&sb, "Cursed Technique: Map Cells\n    Using: (h) -> h * %d + %d\n",
				r.Intn(3)+1, r.Intn(4))
		}
	}
	switch r.Intn(3) {
	case 0:
		sb.WriteString("Domain Expansion: Dijkstra from 0 0\n")
	case 1:
		fmt.Fprintf(&sb, "Maximum Technique: Count Cells\n    Using: (h) -> h > %d\n", r.Intn(9))
	case 2:
		fmt.Fprintf(&sb, "Cursed Technique: Map Cells\n    Using: (g, r, c) -> at(g, r, c) + r + c\n")
	}
	sb.WriteString("Reveal: stdout\n")
	return sb.String()
}

// genGridInput builds a random rectangular digit grid.
func genGridInput(r *rand.Rand) string {
	rows, cols := r.Intn(6)+1, r.Intn(8)+1
	lines := make([]string, rows)
	for i := range lines {
		var row strings.Builder
		for c := 0; c < cols; c++ {
			fmt.Fprintf(&row, "%d", r.Intn(10))
		}
		lines[i] = row.String()
	}
	return strings.Join(lines, "\n")
}

func TestDifferentialRandomPipelines(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	type family struct {
		name string
		prog func(*rand.Rand) string
		in   func(*rand.Rand) string
		n    int
	}
	families := []family{
		{"ints", genIntPipeline, genIntInput, 10},
		{"grid", genGridPipeline, genGridInput, 6},
	}
	for _, f := range families {
		for seed := int64(1); seed <= int64(f.n); seed++ {
			r := rand.New(rand.NewSource(seed))
			src, input := f.prog(r), f.in(r)
			for _, optimize := range []bool{true, false} {
				mode := "naive"
				if optimize {
					mode = "optimized"
				}
				name := fmt.Sprintf("%s/seed%d/%s", f.name, seed, mode)
				src, input, optimize := src, input, optimize
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					pipe := compilePipeline(t, src, optimize)
					want := runInterpreter(t, pipe, []byte(input))
					got := buildAndRun(t, pipe, []byte(input), codegen.Options{})
					if got != want {
						t.Errorf("stdout mismatch\n--- program ---\n%s--- input ---\n%s\ninterpreter: %q\nbinary:      %q",
							src, input, want, got)
					}
				})
			}
		}
	}
}
