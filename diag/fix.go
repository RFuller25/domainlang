// The fix engine behind `domain expansion: fix`: every confident fix the
// analyzer produced is already folded into Report.FixedSrc; this file just
// separates the story into "what was repaired" and "what still needs a human".
package diag

// FixResult describes one automatic-repair run.
type FixResult struct {
	Fixed     string       // the repaired source (== input when nothing applied)
	Applied   []Diagnostic // errors that were repaired automatically
	Remaining []Diagnostic // errors that need a human (no confident fix)
}

// Fix analyzes src and applies every confident repair. It never writes files —
// the CLI owns the backup-and-write step.
func FixSrc(path, src string) *FixResult {
	rep := Analyze(path, src)
	res := &FixResult{Fixed: rep.FixedSrc}
	for _, d := range rep.Diags {
		if d.Severity != Error {
			continue
		}
		if d.HasConfidentFix() {
			res.Applied = append(res.Applied, d)
		} else {
			res.Remaining = append(res.Remaining, d)
		}
	}
	return res
}
