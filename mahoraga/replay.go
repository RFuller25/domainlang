package mahoraga

// Replaying a recipe: rebuilding the adapted binary from source plus the
// record of what was done to it.
//
// This is what makes the recipe worth committing beside the program. A binary
// that is faster for unexplained reasons is a liability; a recipe is
// reviewable in a diff, survives a rebuild, and can be checked against an
// input without building anything.
//
// Replay does not re-search. It re-derives, and then re-verifies — because
// with no runtime guard in the adapted binary (see the spec: the check happens
// while adapting, not while running), the build-time check is the whole safety
// net.

import (
	"fmt"
	"os"
	"path/filepath"

	"domain/codegen"
	"domain/runner"
)

// VerifyResult is what a recipe's contract says about an input.
type VerifyResult struct {
	// Matches is whether the input is the one the recipe was adapted to.
	Matches bool
	// Safe is whether the recorded adaptations remain valid for this input.
	// A recipe of general-tier adaptations is safe for any input; one with a
	// pinned adaptation is not.
	Safe    bool
	Reasons []string
}

// Verify evaluates a recipe's contract against an input, without building.
//
// This is the honest way to ask "can I still use this binary?", and it is
// cheap: a fingerprint and the recorded tiers, no compilation and no run.
func Verify(r *Recipe, input string) VerifyResult {
	res := VerifyResult{Matches: true, Safe: true}
	fp := fingerprint(input)

	if fp.SHA256 != r.InputFingerprint.SHA256 {
		res.Matches = false
		switch {
		case fp.Bytes != r.InputFingerprint.Bytes:
			res.Reasons = append(res.Reasons, fmt.Sprintf(
				"the input is %d bytes; the recipe was adapted to %d",
				fp.Bytes, r.InputFingerprint.Bytes))
		case fp.Lines != r.InputFingerprint.Lines:
			res.Reasons = append(res.Reasons, fmt.Sprintf(
				"the input has %d lines; the recipe was adapted to %d",
				fp.Lines, r.InputFingerprint.Lines))
		default:
			res.Reasons = append(res.Reasons, "the input's contents differ from the one adapted to")
		}
	}
	if res.Matches {
		return res
	}

	// A different input is only a *problem* when something in the recipe is
	// bound to the old one, and that is a question about tiers rather than
	// about the mismatch.
	//
	//   - **General** adaptations — a pass schedule, a build flag — hold for
	//     any input. Nothing to say.
	//   - **Guarded** ones were measured against the original input but keep a
	//     fallback, so they stay *correct* on a different one and only stop
	//     being optimal. A capacity hint that is now wrong still appends; a
	//     disabled collector still has its memory limit. Worth mentioning, and
	//     nothing more — reporting them as unsafe, as this used to, would have
	//     made "guarded" and "pinned" the same tier in the only place the
	//     distinction is checked.
	//   - **Pinned** ones carry no check and no fallback. This is where the
	//     refusal lives.
	pinned := false
	for _, a := range r.Kept() {
		switch a.Tier {
		case Pinned.String():
			pinned = true
		case Guarded.String():
			res.Reasons = append(res.Reasons, fmt.Sprintf(
				"turn %d's %q was tuned for the original input; it stays correct here, "+
					"but may no longer be faster", a.Turn, a.ID))
		}
	}
	if !pinned {
		return res
	}

	// A pinned recipe is checked against its *contract*, not against the hash.
	//
	// This is the difference between "you may only ever run this on one file"
	// and "you may run this on any input satisfying what was assumed". The
	// assumption behind a removed UTF-8 decode is that every byte is one rune,
	// and any number of files satisfy it; binding to the hash was correct and
	// needlessly strict. The clauses that genuinely cannot be re-established —
	// a filter that would keep every element — are recorded as such and are
	// what still binds a recipe to one input.
	if r.Contract.Empty() {
		res.Safe = false
		res.Reasons = append(res.Reasons,
			"this recipe has a pinned adaptation and records no contract, so there is "+
				"nothing to check a different input against")
		return res
	}
	facts, err := readFacts(input)
	if err != nil {
		res.Safe = false
		res.Reasons = append(res.Reasons, fmt.Sprintf("%s could not be read: %v", input, err))
		return res
	}
	if bad := r.Contract.Check(facts); len(bad) > 0 {
		res.Safe = false
		res.Reasons = append(res.Reasons, bad...)
		return res
	}
	res.Reasons = append(res.Reasons,
		"the pinned adaptations' contract holds for this input, so the binary is still correct")
	return res
}

// Replay rebuilds the adapted binary from a recipe and writes it to out.
//
// It re-verifies rather than trusting: the contract is checked against the
// input, and the rebuilt binary is run and its output checked. A recipe whose
// contract no longer holds is refused at build time, which is far better
// feedback than a binary that answers wrongly at run time.
func Replay(r *Recipe, input, expected, out string) error {
	if input == "" {
		input = r.Input
	}
	if expected == "" {
		expected = r.Expected
	}
	if v := Verify(r, input); !v.Safe {
		return fmt.Errorf("this recipe cannot be replayed onto %s:\n  %s",
			input, joinLines(v.Reasons, "\n  "))
	}

	pipe, err := runner.LoadPipelineSchedule(r.Program, r.Candidate().Schedule)
	if err != nil {
		return err
	}
	// The tuning goes back in. Without it a replay would rebuild the pass
	// schedule and the build flags and quietly drop every catalogue edit — a
	// binary that is not the one the recipe measured, produced by the command
	// whose whole job is to reproduce it.
	cand := r.Candidate()
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{Tuning: cand.Tuning})
	if err != nil {
		return fmt.Errorf("generating Go: %w", err)
	}
	dir, err := os.MkdirTemp("", "mahoraga-replay-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	bin := filepath.Join(dir, "replayed")
	if err := codegen.BuildBinaryWith(goSrc, bin, cand.Build); err != nil {
		return err
	}

	// Re-verify: a replay that produced a wrong answer and said nothing would
	// be worse than no replay at all.
	oracle, err := NewOracle(expected)
	if err != nil {
		return err
	}
	results, err := runner.RaceContestants(
		[]runner.Contestant{{Label: "replayed", Argv: []string{bin}, Dir: filepath.Dir(input)}},
		runner.Input{Path: input},
		runner.Options{Runs: 1, KeepStdout: true},
	)
	if err != nil {
		return err
	}
	res := &results[0]
	if res.Err != nil {
		return res.Err
	}
	if res.Failed() {
		return fmt.Errorf("the replayed binary failed (exit %d)", res.ExitCode)
	}
	if !oracle.Correct(res.Stdout) {
		return fmt.Errorf("the replayed binary answered wrongly — nothing was written")
	}
	return copyFile(bin, out)
}

func joinLines(xs []string, sep string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}
