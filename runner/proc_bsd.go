//go:build unix && !linux && !darwin

package runner

// The BSDs report ru_maxrss in kilobytes, as Linux does.
const rssUnit = 1024
