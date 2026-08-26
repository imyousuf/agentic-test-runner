package browser

import "syscall"

// stillActive is the exit code Windows reports for a process that has not
// exited yet.
const stillActive = 259

// processAlive reports whether a pid refers to a running process.
//
// Windows has no signals, and the Unix trick of sending signal 0 fails there
// for every pid — including live ones. Using it would make a running instance
// look dead, and the caller deletes the profile lock of anything it believes
// has gone: the one instance genuinely holding the profile would have its lock
// removed out from under it.
//
// Opening a handle and reading the exit code is the equivalent question. A pid
// that cannot be opened is either gone or belongs to another user, and neither
// is ours to clean up.
func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(handle) }()

	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
