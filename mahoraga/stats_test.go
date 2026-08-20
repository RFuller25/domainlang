package mahoraga

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

func ms(n float64) time.Duration { return time.Duration(n * float64(time.Millisecond)) }

// The first run is cold — page cache, a freshly written binary, an unscaled
// CPU — and including it in a spread measures the machine warming up rather
// than the program varying. This was worth 19.6% of noise floor on a 20ms
// program before it was fixed.
func TestSummarizeDiscardsTheWarmupRun(t *testing.T) {
	samples := []time.Duration{ms(40), ms(20), ms(20), ms(20), ms(20)}
	st := Summarize(samples)
	if st.Discarded != 1 {
		t.Errorf("discarded %d samples, want 1", st.Discarded)
	}
	// Four survive the warmup and the slowest is trimmed; with every remaining
	// sample identical the trim changes nothing it measures.
	if st.Runs != 3 || st.Trimmed != 1 {
		t.Errorf("kept %d samples and trimmed %d, want 3 and 1", st.Runs, st.Trimmed)
	}
	if st.Mean != ms(20) {
		t.Errorf("mean = %s, want 20ms — the cold first run is still in it", st.Mean)
	}
	if st.StdDev != 0 {
		t.Errorf("stddev = %s, want 0 once the warmup is out", st.StdDev)
	}
}

// With too few samples there is nothing to spare for a warmup, and nothing to
// say about a distribution.
func TestSummarizeSmallSamples(t *testing.T) {
	if st := Summarize(nil); st.Runs != 0 || st.Mean != 0 {
		t.Errorf("empty samples gave %+v", st)
	}
	st := Summarize([]time.Duration{ms(10)})
	if st.Runs != 1 || st.Discarded != 0 || st.Mean != ms(10) {
		t.Errorf("one sample gave %+v", st)
	}
	// Two samples: no warmup discarded (nothing would be left to measure), and
	// no spread reported.
	st = Summarize([]time.Duration{ms(10), ms(20)})
	if st.Discarded != 0 {
		t.Errorf("a two-sample set discarded a warmup, leaving almost nothing")
	}
	if st.StdErr != 0 {
		t.Errorf("a two-sample set reported a standard error of %s", st.StdErr)
	}
}

// The standard error is the deviation over root-N, and the difference between
// them is the difference between a search that can accept an improvement and
// one that cannot.
func TestStdErrIsDeviationOverRootN(t *testing.T) {
	// Nine kept samples after the warmup, deliberately spread.
	samples := []time.Duration{ms(99)}
	for i := range 9 {
		samples = append(samples, ms(float64(10+i)))
	}
	st := Summarize(samples)
	// Nine survive the warmup and the slowest two are trimmed.
	if st.Runs != 7 || st.Trimmed != 2 {
		t.Fatalf("kept %d samples and trimmed %d, want 7 and 2", st.Runs, st.Trimmed)
	}
	ratio := float64(st.StdDev) / float64(st.StdErr)
	if want := math.Sqrt(float64(st.Runs)); ratio < want-0.1 || ratio > want+0.1 {
		t.Errorf("stddev/stderr = %.2f, want ~%.2f (root of %d)", ratio, want, st.Runs)
	}
	if st.RelStdErr() >= float64(st.StdDev)/float64(st.Mean) {
		t.Error("the relative standard error is not smaller than the relative deviation")
	}
}

// Distinguishable is the accept decision, and the property that matters is
// that it refuses to call noise a win.
func TestDistinguishable(t *testing.T) {
	steady := func(mean float64, spread float64) Measurement {
		var samples []time.Duration
		rng := rand.New(rand.NewSource(7))
		samples = append(samples, ms(mean*2)) // warmup, discarded
		for range 20 {
			samples = append(samples, ms(mean+rng.NormFloat64()*spread))
		}
		return measurementFrom(samples, true)
	}

	champ := steady(100, 1)

	// A large, real improvement is distinguishable.
	better := steady(70, 1)
	if !Distinguishable(champ, better, 0.02) {
		t.Error("a 30% improvement was not distinguishable")
	}
	// A candidate with the same distribution is not, however the means fall.
	same := steady(100, 1)
	if Distinguishable(champ, same, 0.02) {
		t.Error("two samples from the same distribution were called different")
	}
	// Slower is never an improvement.
	worse := steady(130, 1)
	if Distinguishable(champ, worse, 0.02) {
		t.Error("a slower candidate was accepted")
	}
	// A real but tiny improvement is refused when it is below what is worth
	// recording, even though it is statistically visible.
	slight := steady(99, 0.1)
	if Distinguishable(champ, slight, 0.05) {
		t.Error("a 1% improvement was accepted against a 5% minimum effect")
	}
}

// The test the spec asks for: a search fed measurements from a distribution
// with *no real effect* must not manufacture a winner.
//
// This is the failure that would discredit the whole command — a tuner that
// always reports a win is not measuring — so it is checked directly rather
// than trusted to the end-to-end path.
func TestNoEffectIsNeverAWin(t *testing.T) {
	rng := rand.New(rand.NewSource(20260812))
	draw := func() Measurement {
		samples := []time.Duration{ms(60)} // warmup
		for range 10 {
			// One distribution, 20ms ± 1ms. Any "win" here is noise.
			samples = append(samples, ms(20+rng.NormFloat64()))
		}
		return measurementFrom(samples, true)
	}

	champion := draw()
	accepted := 0
	const trials = 300
	for range trials {
		c := draw()
		if Distinguishable(champion, c, DefaultMinEffect) {
			accepted++
			champion = c // the search would adopt it, so the test does too
		}
	}
	// Some false positives are inevitable in any threshold rule; what must not
	// happen is a steady drift of accepted "improvements" out of pure noise.
	if accepted > trials/20 {
		t.Errorf("%d of %d pure-noise candidates were accepted as improvements — "+
			"the search would manufacture a champion from nothing", accepted, trials)
	}
}

// A measurement built from samples carries the correctness verdict, and a
// wrong answer is never usable however fast it was.
func TestMeasurementFromMarksWrongAnswers(t *testing.T) {
	m := measurementFrom([]time.Duration{ms(10), ms(10), ms(10), ms(10)}, false)
	if m.OK() {
		t.Error("a wrong answer produced a usable measurement")
	}
	if m.Failure != "wrong answer" {
		t.Errorf("failure = %q", m.Failure)
	}
	ok := measurementFrom([]time.Duration{ms(10), ms(10), ms(10), ms(10)}, true)
	if !ok.OK() {
		t.Errorf("a correct measurement was not usable: %+v", ok)
	}
}

// msSamples builds a sample set in milliseconds, with a leading warmup that
// Summarize will discard.
func msSamples(values ...float64) []time.Duration {
	out := []time.Duration{ms(values[0] * 3)} // the warmup, thrown away
	for _, v := range values {
		out = append(out, ms(v))
	}
	return out
}

// Interference on a shared machine is one-sided: it can only make a run
// slower. Trimming the slow tail is what stops a handful of stolen timeslices
// inflating both sides' spread until a real difference cannot be seen.
func TestTrimmingRemovesTheSlowTail(t *testing.T) {
	// Eight clean runs at 50ms and two contaminated ones, in the order a real
	// race might produce them.
	clean := []time.Duration{ms(1), ms(50), ms(51), ms(120), ms(50), ms(49), ms(50), ms(51), ms(140), ms(50), ms(50)}
	st := Summarize(clean)
	if st.Trimmed == 0 {
		t.Fatal("nothing was trimmed")
	}
	if st.Mean > ms(55) {
		t.Errorf("mean = %s; the contaminated runs are still in it", st.Mean)
	}
	// The minimum is taken before the trim, so dropping the slowest cannot
	// move it.
	if st.Min != ms(49) {
		t.Errorf("min = %s, want 49ms", st.Min)
	}

	// And the property this exists for: two configurations a clean 16% apart,
	// each with a couple of contaminated runs, must still be distinguishable.
	champ := measurementFrom(clean, true)
	faster := []time.Duration{ms(1), ms(42), ms(43), ms(115), ms(42), ms(41), ms(42), ms(43), ms(130), ms(42), ms(42)}
	cand := measurementFrom(faster, true)
	if !Distinguishable(champ, cand, DefaultMinEffect) {
		t.Errorf("a 16%% win was lost in the slow tail: champion %s ±%s, candidate %s ±%s",
			champ.Mean, champ.StdErr, cand.Mean, cand.StdErr)
	}
}

// Trimming must not turn "the same twice" into a win: it removes the same
// share from both sides, so two identical configurations stay identical.
func TestTrimmingDoesNotManufactureAWin(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	sample := func() []time.Duration {
		out := []time.Duration{ms(80)} // warmup
		for range 20 {
			d := ms(50 + rng.NormFloat64())
			if rng.Intn(6) == 0 {
				d += ms(40) // interference
			}
			out = append(out, d)
		}
		return out
	}
	a, b := measurementFrom(sample(), true), measurementFrom(sample(), true)
	if Distinguishable(a, b, DefaultMinEffect) || Distinguishable(b, a, DefaultMinEffect) {
		t.Errorf("two draws from the same distribution were called different: %s ±%s vs %s ±%s",
			a.Mean, a.StdErr, b.Mean, b.StdErr)
	}
}

// The screen and the confirmation ask different questions, and the screen is
// the one that has to be generous: a candidate it discards is never looked at
// again, and at three runs it has two samples and no spread at all.
//
// The case is real. `i15_generators` reserving both of its five-million
// element accumulators is worth 23% hand-raced, and three screens in a row
// rejected it on two samples each while the machine drifted underneath them.
// The minimum is the sample that was least interfered with — the same
// reasoning TrimSlowest already rests on — so the screen takes whichever view
// is more favourable and lets the confirmation be strict.
func TestScreenEffectTakesTheMoreFavourableView(t *testing.T) {
	champion := Measurement{Mean: 100 * time.Millisecond, Min: 95 * time.Millisecond, Runs: 2}

	// A candidate whose mean was spoiled by one contaminated run, but whose
	// fastest run is plainly quicker than the champion's fastest.
	contaminated := Measurement{Mean: 101 * time.Millisecond, Min: 70 * time.Millisecond, Runs: 2}
	if got := ScreenEffect(champion, contaminated); got < 0.25 {
		t.Errorf("ScreenEffect = %.3f, want the minima's view (~0.26)", got)
	}
	if byMean := effectOf(champion, contaminated); byMean > 0.02 {
		t.Fatalf("the test case no longer has a mean that hides the win: %.3f", byMean)
	}

	// And the other way: a candidate that is genuinely faster on average keeps
	// its own view when the minima happen to agree less.
	steady := Measurement{Mean: 80 * time.Millisecond, Min: 94 * time.Millisecond, Runs: 2}
	if got := ScreenEffect(champion, steady); got < 0.19 {
		t.Errorf("ScreenEffect = %.3f, want the means' view (~0.20)", got)
	}

	// A candidate that is slower by both views stays slower by both. The screen
	// is generous, not blind.
	slower := Measurement{Mean: 130 * time.Millisecond, Min: 125 * time.Millisecond, Runs: 2}
	if got := ScreenEffect(champion, slower); got > 0 {
		t.Errorf("ScreenEffect = %.3f for a candidate slower on both views", got)
	}

	// A missing minimum — a measurement that failed before it recorded one —
	// falls back to the means rather than reading zero as infinitely fast.
	noMin := Measurement{Mean: 90 * time.Millisecond, Runs: 2}
	if got := ScreenEffect(champion, noMin); got < 0.09 || got > 0.11 {
		t.Errorf("ScreenEffect = %.3f with no minimum, want the means' view (~0.10)", got)
	}
}
