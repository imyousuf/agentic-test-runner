package process

import (
	"errors"
	"syscall"
)

// stillActive is the exit code Windows reports for a process that has not
// exited yet.
const stillActive = 259

// Alive reports whether a pid refers to a running process.
//
// Windows has no signals, and the Unix trick of sending signal 0 fails there
// for every pid — including live ones. Using it would make a running instance
// look dead: "atr browser status" reports no daemon while one is serving, and
// the profile-lock cleanup deletes the SingletonLock of the one instance
// genuinely holding the profile.
//
// Opening a handle and reading the exit code asks the same question in terms
// Windows answers. A handle refused for access belongs to another user, which
// means the process is there — not ours to touch, but alive; any other
// failure to open means there is no such process.
func Alive(pid int) bool {
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = syscall.CloseHandle(handle) }()

	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
