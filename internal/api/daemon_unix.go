//go:build !windows

package api

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// setSysProcAttr sets platform-specific process attributes for daemon mode.
// On Unix systems, this sets Setsid to detach from the terminal.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

// registerShutdownSignals registers signals for graceful shutdown.
// On Unix, this includes SIGTERM in addition to os.Interrupt.
func registerShutdownSignals(sigCh chan os.Signal) {
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
}

// terminateProcess sends a termination signal to the process.
// On Unix, this sends SIGTERM.
func terminateProcess(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}
