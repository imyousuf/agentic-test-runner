//go:build windows

package api

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// setSysProcAttr sets platform-specific process attributes for daemon mode.
// On Windows, we use CREATE_NEW_PROCESS_GROUP to detach from the console.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// registerShutdownSignals registers signals for graceful shutdown.
// On Windows, only os.Interrupt is available.
func registerShutdownSignals(sigCh chan os.Signal) {
	signal.Notify(sigCh, os.Interrupt)
}

// terminateProcess sends a termination signal to the process.
// On Windows, we use os.Kill as SIGTERM is not available.
func terminateProcess(process *os.Process) error {
	return process.Kill()
}
