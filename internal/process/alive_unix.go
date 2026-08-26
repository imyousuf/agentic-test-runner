//go:build !windows

package process

import (
	"errors"
	"os"
	"syscall"
)

// Alive reports whether a pid refers to a running process.
//
// On Unix FindProcess always succeeds, so the signal is what actually answers
// the question. Signal 0 performs the permission and existence checks without
// delivering anything.
//
// EPERM is the interesting case: it means the process exists but belongs to
// another user, so the only honest answer is that it is alive. Reading it as
// dead is worse than a wrong status line — the profile-lock cleanup deletes
// the SingletonLock of anything it believes has gone, which would corrupt the
// profile of a Chrome that is very much running.
func Alive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
