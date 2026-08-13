//go:build unix

package runner

import (
	"os"
	"syscall"
)

// rusage records the peak resident set the kernel observed for a finished
// process. It is the one allocation figure that needs no cooperation from the
// thing being measured, which is what makes it the figure battle can print
// for a Python program.
func (r *execResult) rusage(st *os.ProcessState) {
	if st == nil {
		return
	}
	ru, ok := st.SysUsage().(*syscall.Rusage)
	if !ok || ru == nil {
		return
	}
	r.peakRSS = peakRSSFrom(int64(ru.Maxrss))
}
