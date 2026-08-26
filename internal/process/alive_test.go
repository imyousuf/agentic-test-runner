package process

import (
	"os"
	"runtime"
	"testing"
)

func TestAliveReportsThisProcess(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Error("the running test process is not alive")
	}
}

func TestAliveReportsAPidThatCannotExist(t *testing.T) {
	// Far above any pid a system hands out, on every platform this builds for.
	if Alive(1 << 30) {
		t.Error("a pid that cannot exist was reported alive")
	}
}

// The case the Unix implementation got wrong. Signalling a process owned by
// another user fails with EPERM, which says the process is there -- and the
// caller deletes the Chrome profile lock of anything it believes has gone.
func TestAliveTreatsAnotherUsersProcessAsAlive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pid 1 is not the init process on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, so nothing is another user's process")
	}
	if !Alive(1) {
		t.Error("pid 1 is running and owned by root; reporting it dead deletes live profile locks")
	}
}
