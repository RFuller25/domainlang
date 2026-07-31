// Package bench holds the head-to-head benchmarks: each Domain program in
// testdata/ has a hand-written Go program beside it that answers the same
// question about the same input, and the harness builds both with the same
// toolchain flags and times them against each other.
//
// The goal the numbers exist to defend is that `domain build` output stays
// within 2× of what a competent Go programmer writes by hand. See
// README.md in this directory for how to run them and what the current
// numbers are.
package bench
