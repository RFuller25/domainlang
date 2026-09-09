// Randomness and the clock, compiled.
//
// This mirrors ir/rand.go and ir.Clock into the emitted program, as
// codegen/viewgen.go mirrors ir/view.go and for the same reason: codegen
// generates its own runtime rather than importing this repository's.
//
// The generator's constants are the published splitmix64 ones and must match
// ir/rand.go exactly. That is the whole reason splitmix64 was chosen over
// something with tables — every line of it is a line the two sides could
// disagree on, and there are few enough lines to check by eye.
package codegen

// declRand is the stream and the clock. They are one declaration because a
// program that has either almost always has both, and because both are the
// same kind of thing: state a run carries that no expression is a function of.
// The initial state is ir.DefaultSeed. An unseeded program must draw the same
// sequence interpreted and compiled, or the differential oracle would fail for
// a program that is entirely correct.
const declRand = `var dmRandState uint64 = 1
var dmFrames int64
var dmElapsedMS int64

// dmSeed starts the stream. A compiled game seeds from the clock when it is
// played and from its replay script when it is not, exactly as the
// interpreter does.
func dmSeed(seed uint64) { dmRandState = seed }

func dmRandNext() uint64 {
	dmRandState += 0x9e3779b97f4a7c15
	z := dmRandState
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// dmRandInt mirrors ir.Rand.Int: rejection sampling, not a modulo, so that a
// board never quietly refuses to use its last column.
func dmRandInt(n int64) int64 {
	if n <= 0 {
		dmFail("random() needs a count above zero, got %d", n)
	}
	un := uint64(n)
	limit := ^uint64(0) - (^uint64(0) % un) - 1
	for {
		v := dmRandNext()
		if v <= limit {
			return int64(v % un)
		}
	}
}

// dmRandFloat mirrors ir.Rand.Float: the top 53 bits, which is exactly a
// float64's mantissa.
func dmRandFloat() float64 {
	return float64(dmRandNext()>>11) / float64(uint64(1)<<53)
}`
