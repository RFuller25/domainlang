package main

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// withTheme installs a palette for one test and puts the old one back.
func withTheme(t *testing.T, light bool) {
	t.Helper()
	was := lightTheme
	useTheme(light)
	t.Cleanup(func() { useTheme(was) })
}

func TestIsLightColor(t *testing.T) {
	cases := []struct {
		name string
		c    color.Color
		want bool
	}{
		{"white", color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, true},
		{"solarized light", color.RGBA{R: 0xfd, G: 0xf6, B: 0xe3, A: 0xff}, true},
		{"black", color.RGBA{}, false},
		{"solarized dark", color.RGBA{R: 0x00, G: 0x2b, B: 0x36, A: 0xff}, false},
		{"nothing reported", nil, false},
	}
	for _, tc := range cases {
		if got := isLightColor(tc.c); got != tc.want {
			t.Errorf("%s: isLightColor = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The two palettes differ, and both render something: a theme that quietly
// produced the same escapes would be worse than none.
func TestThemesDiffer(t *testing.T) {
	withTheme(t, false)
	dark := highlightSource(`Cursed Technique: Split Text by "\n"`, true)
	darkHeat := heat(90, true).Render("hot")

	useTheme(true)
	light := highlightSource(`Cursed Technique: Split Text by "\n"`, true)
	lightHeat := heat(90, true).Render("hot")

	if dark == light {
		t.Error("the light palette highlights identically to the dark one")
	}
	if darkHeat == lightHeat {
		t.Error("the heat ramp does not change with the palette")
	}
	if ansi.Strip(light) != ansi.Strip(dark) {
		t.Error("the palettes disagree about the text itself, not just its color")
	}
}

// A background report installs the matching palette; a report that matches the
// palette already installed changes nothing.
func TestReplTTYAdoptsTheTerminalBackground(t *testing.T) {
	withTheme(t, false)
	m := newTestModel(t)

	next, _ := m.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}})
	m = next.(replModel)
	if !lightTheme {
		t.Error("a light background did not install the light palette")
	}

	m.Update(tea.BackgroundColorMsg{Color: color.RGBA{A: 0xff}})
	if lightTheme {
		t.Error("a dark background did not install the dark palette")
	}
}

// Swapping the palette under a running program would race the goroutine that
// paints its results, so it waits.
func TestReplTTYThemeSwapWaitsForARunningProgram(t *testing.T) {
	withTheme(t, false)
	m := newTestModel(t)
	m.evaluating = true

	next, _ := m.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}})
	m = next.(replModel)
	if lightTheme {
		t.Fatal("the palette was swapped while a program was running")
	}
	if m.pendingTheme == nil {
		t.Fatal("the swap was dropped instead of deferred")
	}

	next, _ = m.Update(evalDoneMsg{})
	m = next.(replModel)
	if !lightTheme {
		t.Error("the deferred swap was never applied")
	}
	if m.pendingTheme != nil {
		t.Error("the pending swap was not cleared")
	}
}

// Diagnostics link their error code into the docs site once :docs serves one,
// and print plainly before that.
func TestDiagnosticsLinkToTheDocsSite(t *testing.T) {
	t.Chdir(t.TempDir())
	src := "Cursed Tecnique: Split\n"

	plain := renderDiagnosticsFor(t, src)
	if strings.Contains(plain, "\x1b]8;") {
		t.Errorf("a link was emitted with no server to link to:\n%q", plain)
	}

	url, err := startDocsSite(0)
	if err != nil {
		t.Fatalf("starting the docs site: %v", err)
	}
	t.Cleanup(func() {
		docsSite.Lock()
		docsSite.url = ""
		docsSite.Unlock()
	})

	linked := renderDiagnosticsFor(t, src)
	if !strings.Contains(linked, ansi.SetHyperlink(url+"#/language")) {
		t.Errorf("the error code was not linked to the language page:\n%q", linked)
	}
	if ansi.Strip(linked) == "" {
		t.Error("linking swallowed the diagnostic")
	}
}

func renderDiagnosticsFor(t *testing.T, src string) string {
	t.Helper()
	r := &repl{baseDir: ".", color: true}
	return r.renderDiagnostics(src)
}

func TestDocsPageForCode(t *testing.T) {
	for code, want := range map[string]string{
		"name": "language", "syntax": "language", "type": "data-model",
		"resolve": "primitives", "perf": "diagnostics", "": "README",
	} {
		if got := docsPageFor(code); got != want {
			t.Errorf("docsPageFor(%q) = %q, want %q", code, got, want)
		}
	}
}
