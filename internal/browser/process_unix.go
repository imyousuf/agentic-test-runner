//go:build !windows

package browser

import (
	"os"
	"syscall"
)

// processAlive reports whether a pid refers to a running process.
//
// On Unix FindProcess always succeeds, so the signal is what actually answers
// the question. Signal 0 performs the permission and existence checks without
// delivering anything.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
