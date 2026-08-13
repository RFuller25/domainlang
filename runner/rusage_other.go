//go:build !unix

package runner

import "os"

func (r *execResult) rusage(*os.ProcessState) {}
