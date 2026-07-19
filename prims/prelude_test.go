package prims

import "testing"

// TestPreludeSmoke resolves and runs every prelude Shikigami on a
// representative input. A prelude bug would otherwise surface as a user
// error, so this test catches a broken prelude definition directly, at the
// source, instead of via whatever program happens to first exercise it.
func TestPreludeSmoke(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		stdin string
		check func(t *testing.T, v any)
	}{
		{
			"Lines",
			"Cursed Energy: stdin\nShikigami: Lines\nMaximum Technique: Count\n",
			"a\nb\nc",
			func(t *testing.T, v any) {
				if v.(int64) != 3 {
					t.Fatalf("Lines then Count: got %v want 3", v)
				}
			},
		},
		{
			"Blocks",
			"Cursed Energy: stdin\nShikigami: Blocks\nChanneled Energy: Convert Each List to Integers\nMaximum Technique: Sum Each Group\n",
			"1\n2\n\n3\n4",
			func(t *testing.T, v any) {
				got, ok := v.([]any)
				if !ok || len(got) != 2 || got[0].(int64) != 3 || got[1].(int64) != 7 {
					t.Fatalf("Blocks group sums: got %v want [3 7]", v)
				}
			},
		},
		{
			"Ints",
			"Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n",
			"1\n2\n3",
			func(t *testing.T, v any) {
				if v.(int64) != 6 {
					t.Fatalf("Ints then Sum: got %v want 6", v)
				}
			},
		},
		{
			"Digit Grid",
			"Cursed Energy: stdin\nShikigami: Digit Grid\nMaximum Technique: Count Cells\n    Using: (h) -> h >= 5\n",
			"39\n15",
			func(t *testing.T, v any) {
				if v.(int64) != 2 { // 9 and 5
					t.Fatalf("Digit Grid then Count Cells: got %v want 2", v)
				}
			},
		},
		{
			"Top K Sum",
			"Cursed Energy: stdin\nShikigami: Ints\nShikigami: Top K Sum\n    k: 2\n",
			"1\n5\n3\n9\n2",
			func(t *testing.T, v any) {
				if v.(int64) != 14 { // top 2: 9 + 5
					t.Fatalf("Top K Sum: got %v want 14", v)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, _ := runPipeline(t, c.src, c.stdin)
			c.check(t, v)
		})
	}
}

// TestPreludeDefsParseCleanly is a narrower, faster check that just proves
// the embedded prelude source itself lexes, parses, and resolves without
// error — independent of any particular program using it.
func TestPreludeDefsParseCleanly(t *testing.T) {
	defs, err := preludeDefs()
	if err != nil {
		t.Fatalf("prelude failed to parse: %v", err)
	}
	want := map[string]bool{"Lines": false, "Blocks": false, "Ints": false, "Digit Grid": false, "Top K Sum": false}
	for _, d := range defs {
		if _, ok := want[d.Name]; ok {
			want[d.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("expected prelude Shikigami %q not found", name)
		}
	}
}
