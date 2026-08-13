//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// A measured program may itself spawn a child — a foreign block shells out to
// python3 or `go run`, and `go run` in turn runs the binary it built. Killing
// only the process we started would leave those behind holding the CPU the
// next timed run is about to be measured on, so a run gets its own process
// group and the whole group is signalled.

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negating the pid addresses the group. If the group is gone already,
	// fall back to the process itself rather than reporting a failure the
	// caller can do nothing about.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

// peakRSSFrom converts what wait4 reported into bytes. Linux counts Maxrss in
// kilobytes; darwin counts it in bytes.
func peakRSSFrom(maxrss int64) int64 { return maxrss * rssUnit }
