//go:build darwin

package executor

import (
	"os"
	"os/exec"
)

// detectShell returns the shell to use on macOS.
func detectShell() string {
	// Use SHELL environment variable if set (user's preferred shell)
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}

	// Prefer zsh (default on modern macOS), fall back to bash
	if _, err := exec.LookPath("zsh"); err == nil {
		return "/bin/zsh"
	}
	return "/bin/bash"
}

// shellArgs returns the arguments to pass to the shell for executing a command.
func shellArgs(command string) []string {
	return []string{"-c", command}
}
