package cli

import "syscall"

// detachAttr gives the recorder its own process group, which is the closest
// Windows equivalent of a session: a console Ctrl+C event aimed at this group
// no longer reaches it.
func detachAttr() *syscall.SysProcAttr {
	const createNewProcessGroup = 0x00000200
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}
