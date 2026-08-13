package mahoraga

// The statistics that decide whether a number means anything.
//
// This is the part that separates a tuner from a random number generator with
// good typography, and the first version of it was wrong in a way worth
// recording: it set the noise floor from the raw standard deviation, which on
// a 20ms program came out at nearly 20% and made every real improvement
// unacceptable.
//
// Three corrections, all standard and all necessary:
//
//   - **The first run is a warmup and does not count.** The page cache is cold,
//     the CPU has not scaled up, and the binary has just been written to disk.
//     That run is real — it is what a user pays once — but including it in a
//     *spread* measures the machine warming up rather than the program varying.
//   - **The floor is the standard error of the mean, not the standard
//     deviation.** The question is not "how much does one run vary" but "how
//     precisely do I know this mean", and those differ by a factor of √N. Using
//     the deviation demands a candidate beat the spread of individual runs,
//     which is a far higher bar than distinguishing two averages and rejects
//     improvements that are plainly real.
//   - **The slow tail is contamination, not spread.** Interference on a shared
//     machine only ever makes a run slower, so the top quarter of a wall-clock
//     sample is other people's work landing in the measurement. See TrimSlowest
//     for the search this cost a real 16% win.

import (
	"math"
	"slices"
	"time"
)

// WarmupRuns is how many leading samples are discarded before the statistics
// are computed. One is enough: the effects it excludes (cold page cache, a
// freshly written binary, an unscaled CPU) are first-run effects.
const WarmupRuns = 1

// MinSamplesForSpread is how many samples must survive the warmup before a
// spread is computed at all. Below it the standard error is reported as zero,
// which makes the threshold fall back to the minimum effect alone — honest,
// because two samples say nothing about a distribution.
const MinSamplesForSpread = 3

// TrimSlowest is the fraction of the slowest surviving samples discarded before
// the mean and spread are computed.
//
// This is the third correction, and it came from a search that measured a real
// 16% win and then refused to ship it. The final race put the champion at 44.2ms
// minimum and the baseline at 52.3ms, a clean and repeatable gap — and the
// *means* came out 51.4 and 62.4 with a 24% spread, wide enough that the two
// could not be told apart. The samples were not noisy in the sense the standard
// error assumes. They were contaminated.
//
// Interference on a shared machine is **one-sided**: a neighbour process, a
// page fault, a scheduler decision can only ever make a run slower, never
// faster. So the upper tail of a wall-clock sample is not the distribution's
// own shape, it is other people's work landing in the measurement, and
// including it inflates the spread of both sides until nothing is
// distinguishable from anything. bench/README.md reaches the same conclusion
// and goes further, reporting the minimum outright.
//
// Trimming a quarter is the conservative version of that: enough to drop the
// occasional stolen timeslice, not so much that a genuinely bimodal program
// gets reported as its fast half. It applies only where there are samples to
// spare, and Min is always taken before the trim — dropping the slowest cannot
// change the fastest, but a reader should not have to work that out.
const TrimSlowest = 0.25

// MinSamplesForTrim is how many surviving samples are needed before trimming is
// worth doing. Below it, throwing one away costs more precision than the
// contamination it removes.
const MinSamplesForTrim = 4

// Stats summarises a set of timing samples.
type Stats struct {
	Mean   time.Duration
	Min    time.Duration
	StdDev time.Duration
	// StdErr is the standard error of the mean: how precisely the mean is
	// known, which is what a comparison between two configurations turns on.
	StdErr time.Duration
	// Runs is the number of samples the statistics were computed from, after
	// the warmup and the slow tail were discarded.
	Runs int
	// Discarded is how many leading samples were dropped as warmup.
	Discarded int
	// Trimmed is how many slowest samples were dropped as interference.
	Trimmed int
}

// RelStdErr is the standard error as a fraction of the mean — the noise floor.
func (s Stats) RelStdErr() float64 {
	if s.Mean <= 0 {
		return 0
	}
	return float64(s.StdErr) / float64(s.Mean)
}

// Summarize computes the statistics for a set of samples: the warmup run is
// dropped, then the slowest quarter of what is left, then the mean and spread
// are taken from the rest. See the constants above for why each.
func Summarize(samples []time.Duration) Stats {
	kept := samples
	discarded := 0
	if len(samples) > WarmupRuns+1 {
		kept = samples[WarmupRuns:]
		discarded = WarmupRuns
	}
	if len(kept) == 0 {
		return Stats{}
	}

	// The minimum comes from before the trim. Dropping the slowest cannot
	// change the fastest, but taking it here says so without the reader having
	// to reason about it.
	minimum := kept[0]
	for _, s := range kept {
		if s < minimum {
			minimum = s
		}
	}

	trimmed := 0
	if len(kept) >= MinSamplesForTrim {
		sorted := append([]time.Duration(nil), kept...)
		slices.Sort(sorted)
		trimmed = max(int(float64(len(sorted))*TrimSlowest), 1)
		kept = sorted[:len(sorted)-trimmed]
	}

	var total time.Duration
	for _, s := range kept {
		total += s
	}
	st := Stats{
		Mean:      total / time.Duration(len(kept)),
		Min:       minimum,
		Runs:      len(kept),
		Discarded: discarded,
		Trimmed:   trimmed,
	}
	if len(kept) < MinSamplesForSpread {
		return st
	}

	mean := float64(st.Mean)
	var sum float64
	for _, s := range kept {
		d := float64(s) - mean
		sum += d * d
	}
	// The sample standard deviation (n−1), since these are a sample of the
	// runs this program could have, not the population of them.
	variance := sum / float64(len(kept)-1)
	st.StdDev = time.Duration(math.Sqrt(variance))
	st.StdErr = time.Duration(math.Sqrt(variance / float64(len(kept))))
	return st
}

// measurementFrom builds a Measurement from samples and a correctness verdict.
func measurementFrom(samples []time.Duration, correct bool) Measurement {
	st := Summarize(samples)
	m := Measurement{
		Mean: st.Mean, Min: st.Min, StdDev: st.StdDev, StdErr: st.StdErr,
		Runs: st.Runs, Correct: correct,
	}
	if !correct {
		m.Failure = "wrong answer"
	}
	return m
}

// Distinguishable reports whether two measurements differ by more than their
// combined uncertainty — the question "is this candidate actually different
// from the champion, or did I just measure twice?"
//
// The combined standard error of a difference is the root of the sum of
// squares, which is the usual propagation of independent errors.
func Distinguishable(champion, candidate Measurement, minEffect float64) bool {
	if champion.Mean <= 0 || candidate.Mean <= 0 {
		return false
	}
	// Without enough samples on both sides there is no spread to reason from,
	// and a comparison of two means with unknown uncertainty is not a
	// comparison at all. Refusing here is what stops a two-sample measurement
	// degenerating into "whichever mean is lower wins", which would accept
	// noise as an improvement every time.
	if champion.Runs < MinSamplesForSpread || candidate.Runs < MinSamplesForSpread {
		return false
	}
	diff := float64(champion.Mean) - float64(candidate.Mean)
	combined := math.Sqrt(
		float64(champion.StdErr)*float64(champion.StdErr) +
			float64(candidate.StdErr)*float64(candidate.StdErr))
	// Two standard errors is the conventional "probably real" line, and it is
	// deliberately not a p-value: this is a stopping rule for a search, not a
	// claim about a hypothesis.
	if diff < 2*combined {
		return false
	}
	return diff/float64(champion.Mean) >= minEffect
}
