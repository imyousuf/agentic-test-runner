//go:build windows

package executor

// detectShell returns the shell to use on Windows.
func detectShell() string {
	return "powershell.exe"
}

// shellArgs returns the arguments to pass to PowerShell for executing a command.
func shellArgs(command string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", command}
}
