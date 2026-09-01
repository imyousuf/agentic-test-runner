//go:build !windows

package cli

import "syscall"

// detachAttr puts the recorder in a session of its own.
//
// Without it the recorder stays in the caller's process group, so a Ctrl+C
// aimed at some later command in the same terminal would stop a recording
// nobody was thinking about.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
