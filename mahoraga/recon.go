package mahoraga

// Watching the program run, once, to see which stages did nothing.
//
// This is turn 2's reconnaissance, and it is the one place the search runs the
// *interpreter* rather than a compiled binary. The interpreter is the only
// backend with a trace hook, and what turn 2 needs is not a timing but an
// account of what each stage did to the value that passed through it.
//
// The finding it is after is narrow and worth stating exactly: **a stage whose
// output was the same length as its input, every single time, for a primitive
// where that means it did nothing at all.** A `Filter` over two million
// elements that discards none of them still evaluates its predicate two million
// times and still copies the list. The general optimizer cannot remove it,
// because whether a predicate ever fails is a property of the data — which is
// precisely the kind of thing this command is allowed to look at.
//
// It is *not* an account of what never ran. Nodes that never run do exist in a
// Domain pipeline, but they are the ones the optimizer already fused away, and
// they emit no code to cut; see the spec's status section for the survey.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
	"domain/prims"
	"domain/runner"
)

// idleStagePrims are the primitives for which "the output was as long as the
// input" means "this stage did nothing".
//
// The whitelist is the whole safety argument, and it is short on purpose. A
// `Sort` preserves length and reorders; a `Map Each` preserves length and
// replaces every element. Only for these does an unchanged length imply an
// unchanged value, so only these may be removed on the strength of one.
var idleStagePrims = map[string]string{
	"Filter":         "kept every element",
	"Filter Entries": "kept every entry",
	"Unique":         "found no duplicates",
	"Merge Ranges":   "found nothing to merge",
}

// IdleStage is a stage that was measured doing nothing.
type IdleStage struct {
	// Key identifies the node the way codegen.Tuning.ElideNodes does.
	Key  string
	Prim string
	Line int
	// Why is the observation in the language of the primitive: "kept every
	// element" rather than "size-preserving".
	Why string
	// Size is the length that went in and came out, for the report.
	Size int
	// Calls is how many times the stage ran. A stage inside a loop body that
	// did nothing on all four hundred laps is a stronger finding than one that
	// ran once, and the report should be able to say which.
	Calls int
}

// idleTimeout bounds the reconnaissance run. The interpreter is far slower than
// the compiled binary the rest of the search measures, and a program that takes
// a minute compiled is not one to wait out interpreted — turn 2 stands down
// instead, which costs the search that turn and nothing else.
const idleTimeout = 90 * time.Second

// findIdleStages interprets the program once and reports the stages that did
// nothing to their value.
//
// Failure is never fatal: a program the interpreter cannot run, or cannot run
// quickly enough, simply yields no findings. Turn 2 is the only caller and it
// treats an empty result as "nothing to cut", which is the truth either way.
func (s *Search) findIdleStages() ([]IdleStage, error) {
	data, err := os.ReadFile(s.opts.Input)
	if err != nil {
		return nil, err
	}
	pipe, err := runner.LoadPipelineSchedule(s.opts.Program, s.champion.Schedule)
	if err != nil {
		return nil, err
	}

	// The stages worth reporting are the ones that emit code. A node the
	// optimizer fused into its neighbour never runs and never will, and
	// "removing" it would be removing nothing — which on this corpus is every
	// node that has ever shown a zero call count.
	_, spans, err := codegen.EmitAnnotated(pipe, codegen.Options{})
	if err != nil {
		return nil, err
	}
	// Whether there is anything here that *could* be idle is a question about
	// the program, and it costs a walk of the node list. Asking it first is
	// worth a paragraph because of what it saves: the interpreted run is the
	// slowest thing in the search — bounded at ninety seconds, and a program
	// that takes half a second compiled can spend all ninety of them — and on
	// a program with no Filter, no Unique and no Merge Ranges it can only ever
	// report nothing. Two of the four programs in bench/mahoraga are that
	// shape.
	if !hasIdleCandidate(pipe, spans) {
		return nil, nil
	}

	counter := runner.NewNodeCounter()
	var out bytes.Buffer
	ctx := &ir.Context{
		Stdin:   bytes.NewReader(data),
		Stdout:  &out,
		BaseDir: filepath.Dir(s.opts.Program),
		Trace:   counter,
	}
	if err := runInterpreterBounded(pipe, ctx, idleTimeout); err != nil {
		return nil, err
	}
	// A reconnaissance run that produced the wrong answer is not evidence about
	// anything. It should be impossible — turn 1 has already established that
	// the compiled program is correct — but the two backends are separate
	// implementations, and this is the cheapest place to notice they disagree.
	if !s.oracle.Correct(out.Bytes()) {
		return nil, fmt.Errorf("the interpreter disagreed with the expected output")
	}

	var idle []IdleStage
	prims.WalkNodes(pipe, func(n *ir.Node) {
		why, eligible := idleStagePrims[n.Prim]
		if !eligible {
			return
		}
		if _, emits := spans[n]; !emits {
			return
		}
		st := counter.Stat(n)
		if st.Calls == 0 || st.Failed || !st.Sized || !st.SizePreserving {
			return
		}
		idle = append(idle, IdleStage{
			Key: codegen.NodeKey(n), Prim: n.Prim, Line: n.Pos.Line,
			Why: why, Size: st.MaxOutSize, Calls: st.Calls,
		})
	})
	return idle, nil
}

// hasIdleCandidate reports whether the program contains a stage that could be
// found idle at all: one of the whitelisted primitives, and one that emits
// code. A program with none cannot produce a finding, so the run that would
// look for one is a run whose answer is already known.
func hasIdleCandidate(pipe *ir.Pipeline, spans map[*ir.Node]codegen.Span) bool {
	found := false
	prims.WalkNodes(pipe, func(n *ir.Node) {
		if found {
			return
		}
		if _, eligible := idleStagePrims[n.Prim]; !eligible {
			return
		}
		if _, emits := spans[n]; !emits {
			return
		}
		found = true
	})
	return found
}

// runInterpreterBounded runs the interpreter with a deadline, and *stops* it
// when the deadline passes.
//
// The first version of this abandoned the goroutine instead, on the reasoning
// that leaking one once per search was cheap. It was not, and a recipe caught
// it: on a 712ms program the interpreted run could not finish in ninety
// seconds, was left running, and spent the next several minutes at a full core
// and a 389MB heap while the search measured against it. Turn 3 and the first
// half of turn 4 came in twenty percent slow, a candidate inside that window
// was accepted as a win, and only the final re-measurement — taken after the
// runaway had finished — caught it. The search was measuring its own
// reconnaissance.
//
// ir.Interrupter is the escape the REPL already uses for Ctrl+C: it rides the
// trace hook every node evaluation passes through and aborts at the next node
// boundary. It wraps the counter rather than replacing it, so the
// reconnaissance still records everything it saw up to the moment it stopped —
// findings from a partial run are not used, but the run does end.
//
// The wait after Stop is not ceremony. Interruption lands between node
// evaluations, so a single long-running primitive still has to finish, and
// returning while it does would put the search back where it started.
func runInterpreterBounded(pipe *ir.Pipeline, ctx *ir.Context, limit time.Duration) error {
	stop := ir.NewInterrupter(ctx.Trace)
	ctx.Trace = stop

	done := make(chan error, 1)
	go func() {
		_, err := interp.Run(pipe, ctx)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
	}

	stop.Stop()
	select {
	case <-done:
		return fmt.Errorf("the interpreted reconnaissance run did not finish in %s "+
			"and was stopped", limit)
	case <-time.After(interruptGrace):
		// Still inside one primitive after the grace period. Nothing more can
		// be done from here, and the honest thing is to say the search is
		// running alongside it rather than to pretend it stopped.
		return fmt.Errorf("the interpreted reconnaissance run did not finish in %s, "+
			"and did not stop within %s of being asked — measurements taken from "+
			"here may be contaminated by it", limit, interruptGrace)
	}
}

// interruptGrace is how long a stopped run is given to reach a node boundary.
const interruptGrace = 10 * time.Second
