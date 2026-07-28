// The REPL's error reporting: the diagnostics engine, not the raw error.
//
// `domain check`, `domain expansion: lint` and the language server all report
// a broken program the same way — the offending line, carets under the exact
// span, a "did you mean" when the analyzer can guess, and the repaired line
// when the repair is unambiguous. The REPL used to print `error: <err>` and
// drop the statement, which is the one place a typo is *most* likely and the
// one place the extra context is cheapest to act on.
//
// Two adjustments for the interactive setting. The path is a label ("repl"),
// not a file, because a session is not one — but the analysis still resolves
// against the session's base directory, so imports behave the way the running
// program's do. And where a file's diagnostic ends with "run `domain
// expansion: fix`", a session shows the repaired line itself: there is no file
// to fix, and the statement is one keystroke from being retyped.
package main

import (
	"path/filepath"
	"strings"

	"domain/diag"
)

// diagLabel is the path a REPL diagnostic points at, in place of a file name.
const diagLabel = "repl"

// renderDiagnostics analyzes src and renders its errors. It returns "" when
// the analyzer finds no error to report — a runtime failure, say, or a front
// end that disagrees with this one — so the caller can fall back to the raw
// error rather than print nothing at all.
func (r *repl) renderDiagnostics(src string) string {
	baseDir, color := r.baseDir, r.color
	if baseDir == "" {
		baseDir = "."
	}
	// The analyzer takes its resolution root from the path it is given, so it
	// gets a real one inside the session's directory; only the rendering uses
	// the label.
	report := diag.Analyze(filepath.Join(baseDir, "repl.domain"), src)

	var b strings.Builder
	for _, d := range report.Diags {
		if d.Severity != diag.Error {
			continue // lint and perf findings are not why this statement failed
		}
		shown := d
		fix := d.Fix
		shown.Fix = nil // the file-oriented "run expansion: fix" line
		b.WriteString(linkCode(diag.Render(&shown, diagLabel, color), d.Code, color))
		if fix != nil && fix.Confident {
			if repaired := repairedLine(src, *fix, d.Pos.Line); repaired != "" {
				b.WriteString("  " + paintFix(color) + ": " + repaired + "\n")
			}
		}
	}
	return b.String()
}

// linkCode makes the `error[code]` tag a hyperlink to the page that explains
// that class of mistake, when `:docs` is serving one. The tag is found by its
// text rather than rebuilt, so the renderer stays the diagnostics engine's.
func linkCode(block, code string, color bool) string {
	if code == "" || docsBaseURL() == "" {
		return block
	}
	tag := "[" + code + "]"
	i := strings.Index(block, tag)
	if i < 0 {
		return block
	}
	return block[:i] + docsLink(tag, docsPageFor(code), color) + block[i+len(tag):]
}

// repairedLine applies one fix to src and returns the line it repaired, so the
// suggestion is the statement the user meant to type rather than a description
// of it. It returns "" if the fix does not land inside the source (a stale
// offset from an earlier repair round).
func repairedLine(src string, fix diag.Fix, line int) string {
	if fix.Start < 0 || fix.End > len(src) || fix.End < fix.Start {
		return ""
	}
	fixed := src[:fix.Start] + fix.Replacement + src[fix.End:]
	lines := strings.Split(fixed, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// paintFix labels the repaired line, in the same green diag uses for fixes.
func paintFix(color bool) string {
	if !color {
		return "fix"
	}
	return styFix.Render("fix")
}
