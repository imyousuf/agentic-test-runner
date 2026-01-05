//go:build linux

package executor

import "os/exec"

// detectShell returns the shell to use on Linux.
func detectShell() string {
	// Prefer bash, fall back to sh
	if _, err := exec.LookPath("bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

// shellArgs returns the arguments to pass to the shell for executing a command.
func shellArgs(command string) []string {
	return []string{"-c", command}
}
