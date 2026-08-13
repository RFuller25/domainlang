//go:build !unix

package runner

import "os/exec"

// Platforms without process groups get the plain single-process kill that
// exec.CommandContext would have done anyway, and no RSS figure: a report
// that prints a dash is better than one that invents a number.

func setProcessGroup(*exec.Cmd) {}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func peakRSSFrom(int64) int64 { return 0 }
