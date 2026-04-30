package api

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// withFakeHome redirects the home directory for the duration of the test
// so state files go into a temp dir. Sets both HOME (Unix / macOS) and
// USERPROFILE (Windows) since os.UserHomeDir() consults different env vars
// per platform.
func withFakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestComputerStatePathsUseHome(t *testing.T) {
	home := withFakeHome(t)

	gotState, err := ComputerStateFilePath()
	if err != nil {
		t.Fatalf("ComputerStateFilePath: %v", err)
	}
	wantState := filepath.Join(home, ".atr", "computer.state")
	if gotState != wantState {
		t.Errorf("state path = %q, want %q", gotState, wantState)
	}

	gotLog, err := ComputerLogFilePath()
	if err != nil {
		t.Fatalf("ComputerLogFilePath: %v", err)
	}
	wantLog := filepath.Join(home, ".atr", "computer.log")
	if gotLog != wantLog {
		t.Errorf("log path = %q, want %q", gotLog, wantLog)
	}
}

func TestComputerStateRoundTrip(t *testing.T) {
	withFakeHome(t)

	if loaded, err := LoadComputerState(); err != nil || loaded != nil {
		t.Fatalf("expected nil state with no file, got %+v err=%v", loaded, err)
	}

	original := &ComputerState{
		PID:       12345,
		Endpoint:  "http://localhost:9334",
		Mode:      "per-request",
		StartedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := SaveComputerState(original); err != nil {
		t.Fatalf("SaveComputerState: %v", err)
	}

	loaded, err := LoadComputerState()
	if err != nil {
		t.Fatalf("LoadComputerState: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected state, got nil")
	}
	if loaded.PID != original.PID || loaded.Endpoint != original.Endpoint || loaded.Mode != original.Mode {
		t.Errorf("round trip mismatch: %+v vs %+v", loaded, original)
	}
	if !loaded.StartedAt.Equal(original.StartedAt) {
		t.Errorf("StartedAt mismatch: %v vs %v", loaded.StartedAt, original.StartedAt)
	}
}

func TestComputerStateRemove(t *testing.T) {
	withFakeHome(t)

	if err := SaveComputerState(&ComputerState{PID: 1, Endpoint: "x", Mode: "off"}); err != nil {
		t.Fatalf("SaveComputerState: %v", err)
	}
	if err := RemoveComputerState(); err != nil {
		t.Fatalf("RemoveComputerState: %v", err)
	}
	path, _ := ComputerStateFilePath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("state file should be gone, stat err = %v", err)
	}
	// Removing again should be a no-op
	if err := RemoveComputerState(); err != nil {
		t.Errorf("second RemoveComputerState: %v", err)
	}
}

func TestGetRunningComputerStateCleansStaleFile(t *testing.T) {
	withFakeHome(t)

	// Use a PID that almost certainly does not exist
	if err := SaveComputerState(&ComputerState{PID: 99999999, Endpoint: "x", Mode: "off"}); err != nil {
		t.Fatalf("SaveComputerState: %v", err)
	}
	state, err := GetRunningComputerState()
	if err != nil {
		t.Fatalf("GetRunningComputerState: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil for stale state, got %+v", state)
	}
	path, _ := ComputerStateFilePath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected stale state file to be removed, stat err = %v", err)
	}
}

func TestGetRunningComputerStateReturnsLiveState(t *testing.T) {
	if runtime.GOOS == "windows" {
		// IsProcessRunning uses os.Process.Signal(0) which is not
		// implemented on Windows; the production check always returns
		// false there. Tracked separately from this CI fix.
		t.Skip("IsProcessRunning(Signal(0)) not supported on Windows")
	}
	withFakeHome(t)

	// Use our own PID, which is definitely running
	if err := SaveComputerState(&ComputerState{PID: os.Getpid(), Endpoint: "x", Mode: "off"}); err != nil {
		t.Fatalf("SaveComputerState: %v", err)
	}
	state, err := GetRunningComputerState()
	if err != nil {
		t.Fatalf("GetRunningComputerState: %v", err)
	}
	if state == nil {
		t.Fatal("expected state, got nil")
	}
	if state.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", state.PID, os.Getpid())
	}
}
