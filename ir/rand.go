package ir

// Randomness, and why a language this careful about determinism has any.
//
// A game needs it — where the food lands, which piece is next, whether the
// river crossing goes badly — and this repository's whole test discipline is
// that two runs of the same program produce the same bytes. Both are true at
// once, because the sequence is a pure function of a seed:
//
//   - **Playing**, the seed comes from the clock, so a game is different every
//     time somebody plays it.
//   - **Replaying**, the seed is a line of the script, so a test is the same
//     every time it runs — and a run that went wrong can be reproduced exactly
//     by writing down the seed it used.
//
// The generator is splitmix64: sixty-four bits of state, one multiply-xor
// chain per value, no tables, and an equidistributed period of 2^64. It is
// chosen because it is short enough to be transcribed into the compiled
// backend correctly by eye — the two implementations must agree bit for bit,
// and every line of it is a line that could disagree.

// DefaultSeed is the stream a run gets when nobody chose one.
//
// It is a constant rather than the clock, because the two backends have to
// agree: an ordinary program that draws a random number must print the same
// thing interpreted and compiled, or the oracle every test in this repository
// rests on would fail for a program that is perfectly correct. A run that
// wants a different game says so — a replay script's `seed` line, or a host
// seeding from the clock when a game is actually being played.
//
// codegen mirrors this value; the two must not drift.
const DefaultSeed uint64 = 1

// Rand is a seeded stream. The zero value is unusable; a run makes one with
// NewRand, so a stream always has a seed somebody could write down.
type Rand struct {
	state uint64
	// draws counts values taken, purely so a run can report how far it got.
	draws int64
	seed  uint64
}

// NewRand starts a stream at a seed.
func NewRand(seed uint64) *Rand { return &Rand{state: seed, seed: seed} }

// Seed is the seed this stream started from, for a run that wants to say how
// it could be reproduced.
func (r *Rand) Seed() uint64 { return r.seed }

// Draws is how many values have been taken.
func (r *Rand) Draws() int64 { return r.draws }

// next advances the stream. This is splitmix64, and the compiled backend
// mirrors it exactly (codegen/randgen.go); the constants are the published
// ones and must not be adjusted on either side.
func (r *Rand) next() uint64 {
	r.draws++
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// Int returns a value in [0, n).
//
// The mapping is rejection sampling rather than a modulo. A modulo is one
// instruction and is biased when n does not divide 2^64 — slightly, but a
// board that never places food in its last column is a bug somebody would
// spend an evening on, and the rejection almost never fires.
func (r *Rand) Int(n int64) int64 {
	if n <= 0 {
		return 0
	}
	un := uint64(n)
	limit := ^uint64(0) - (^uint64(0) % un) - 1
	for {
		v := r.next()
		if v <= limit {
			return int64(v % un)
		}
	}
}

// Float returns a value in [0, 1).
//
// It uses the top 53 bits, which is exactly the mantissa of a float64: taking
// fewer would leave values it can never produce, and taking more would round.
func (r *Rand) Float() float64 {
	return float64(r.next()>>11) / float64(uint64(1)<<53)
}
