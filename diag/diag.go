// Package diag is Domain's diagnostics engine: it runs the static front end
// (lex → parse → resolve), converts every error into a rich, positioned
// Diagnostic with a plain-language explanation, a "did you mean" suggestion
// where one can be computed, and — when the repair is unambiguous — a
// machine-applicable Fix. It also houses the linter (style/hygiene warnings
// and performance hints) and the source-level optimizer rewrites behind the
// `domain expansion:` CLI commands.
//
// The engine finds more than the first error per stage: whenever a diagnostic
// carries a confident fix, the analyzer applies it to a private working copy
// and re-runs the front end, surfacing the next error that was hiding behind
// it. The user's file is never touched by analysis — only `expansion: fix`
// and `expansion: optimize` write, and they back the original up first.
package diag

import (
	"fmt"
	"sort"
	"strings"

	"domain/token"
)

// Severity classifies a diagnostic.
type Severity int

const (
	Error   Severity = iota // the program cannot run
	Warning                 // the program runs but something looks wrong
	Hint                    // the program is fine; here is how to make it better
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "hint"
	}
}

// Fix is a machine-applicable repair: replace src[Start:End] with Replacement.
// Offsets index the source version the diagnostic was produced against.
// Confident fixes are safe to apply automatically (`expansion: fix`);
// non-confident ones are only ever shown as suggestions.
type Fix struct {
	Start, End  int
	Replacement string
	Confident   bool
}

// Diagnostic is one finding, positioned in the source.
type Diagnostic struct {
	Severity Severity
	Code     string // short category: "syntax", "name", "type", "style", "perf", ...
	Pos      token.Position
	EndCol   int    // 1-based column just past the underlined range; 0 = underline one word
	Msg      string // what is wrong
	Help     string // how to make it right ("did you mean ...?")
	Notes    []string
	Fix      *Fix
	LineText string // the offending source line, captured at analysis time
}

// HasConfidentFix reports whether the diagnostic can be repaired automatically.
func (d *Diagnostic) HasConfidentFix() bool { return d.Fix != nil && d.Fix.Confident }

// Width is the number of columns the diagnostic underlines — the same span
// the renderer marks with carets, exported for LSP range conversion.
func (d *Diagnostic) Width() int { return underlineWidth(d) }

// Render formats one diagnostic in a compiler-style block:
//
//	error[name]: unknown keyword "Cursed Tecnique"
//	  --> day1.domain:3:1
//	   3 | Cursed Tecnique: Split Text by "\n\n"
//	     | ^^^^^^^^^^^^^^^
//	  help: did you mean "Cursed Technique"?
func Render(d *Diagnostic, path string, color bool) string {
	var b strings.Builder
	head := d.Severity.String()
	if d.Code != "" {
		head += "[" + d.Code + "]"
	}
	b.WriteString(paint(color, sevColor(d.Severity), head+": "+d.Msg))
	b.WriteByte('\n')
	fmt.Fprintf(&b, "  --> %s:%d:%d\n", path, d.Pos.Line, d.Pos.Col)

	if d.LineText != "" {
		lineNo := fmt.Sprintf("%4d", d.Pos.Line)
		fmt.Fprintf(&b, "%s | %s\n", lineNo, d.LineText)
		width := underlineWidth(d)
		pad := strings.Repeat(" ", len(lineNo)) + " | " + strings.Repeat(" ", d.Pos.Col-1)
		b.WriteString(pad + paint(color, sevColor(d.Severity), strings.Repeat("^", width)) + "\n")
	}
	if d.Help != "" {
		b.WriteString("  " + paint(color, cCyan, "help") + ": " + d.Help + "\n")
	}
	for _, n := range d.Notes {
		b.WriteString("  " + paint(color, cBlue, "note") + ": " + n + "\n")
	}
	if d.HasConfidentFix() {
		b.WriteString("  " + paint(color, cGreen, "fix") + ": auto-fixable — run `domain expansion: fix`\n")
	}
	return b.String()
}

// underlineWidth picks how many carets to draw: the explicit range when the
// diagnostic set one, else the length of the word starting at the position,
// clamped to the line.
func underlineWidth(d *Diagnostic) int {
	max := len(d.LineText) - (d.Pos.Col - 1)
	if max < 1 {
		return 1
	}
	w := 1
	if d.EndCol > d.Pos.Col {
		w = d.EndCol - d.Pos.Col
	} else {
		rest := d.LineText[d.Pos.Col-1:]
		w = len(rest) - len(strings.TrimLeft(rest, wordChars(rest)))
		if w < 1 {
			w = 1
		}
	}
	if w > max {
		w = max
	}
	return w
}

// wordChars returns the cutset that extends the underlined word: identifier
// characters, or the leading rune itself for punctuation.
func wordChars(rest string) string {
	c := rest[0]
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
		return "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"
	}
	return string(c)
}

// ANSI colors, used only when the caller asked for color.
const (
	cRed    = "\x1b[1;31m"
	cYellow = "\x1b[1;33m"
	cCyan   = "\x1b[1;36m"
	cBlue   = "\x1b[1;34m"
	cGreen  = "\x1b[1;32m"
	cReset  = "\x1b[0m"
)

func sevColor(s Severity) string {
	switch s {
	case Error:
		return cRed
	case Warning:
		return cYellow
	default:
		return cCyan
	}
}

func paint(color bool, code, s string) string {
	if !color {
		return s
	}
	return code + s + cReset
}

// sortDiags orders diagnostics by source position, errors first within a line.
func sortDiags(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].Pos.Line != ds[j].Pos.Line {
			return ds[i].Pos.Line < ds[j].Pos.Line
		}
		if ds[i].Pos.Col != ds[j].Pos.Col {
			return ds[i].Pos.Col < ds[j].Pos.Col
		}
		return ds[i].Severity < ds[j].Severity
	})
}

// lineAt extracts the 1-based line from src, without its trailing newline.
func lineAt(src string, line int) string {
	cur := 1
	start := 0
	for i := 0; i <= len(src); i++ {
		if i == len(src) || src[i] == '\n' {
			if cur == line {
				return strings.TrimRight(src[start:i], "\r")
			}
			cur++
			start = i + 1
		}
	}
	return ""
}
