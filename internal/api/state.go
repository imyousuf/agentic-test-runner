// Package api provides the HTTP server for browser control.
package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// StateFileName is the name of the state file.
	StateFileName = "browser.state"
)

// BrowserState represents the persisted state of a running browser server.
type BrowserState struct {
	PID            int       `json:"pid"`
	Endpoint       string    `json:"endpoint"`
	StartedAt      time.Time `json:"started_at"`
	BrowserVersion string    `json:"browser_version,omitempty"`
}

// StateFilePath returns the path to the state file.
func StateFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".atr", StateFileName), nil
}

// LoadState loads the browser state from disk.
func LoadState() (*BrowserState, error) {
	path, err := StateFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No state file exists
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state BrowserState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// SaveState saves the browser state to disk.
func SaveState(state *BrowserState) error {
	path, err := StateFilePath()
	if err != nil {
		return err
	}

	// Ensure directory exists with restrictive permissions (user-only)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Use restrictive permissions - state file contains server endpoint
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// RemoveState removes the state file.
func RemoveState() error {
	path, err := StateFilePath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}

// IsProcessRunning checks if a process with the given PID is running.
func IsProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0 to check
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// GetRunningState returns the state if a browser server is running, nil otherwise.
func GetRunningState() (*BrowserState, error) {
	state, err := LoadState()
	if err != nil {
		return nil, err
	}

	if state == nil {
		return nil, nil
	}

	// Verify the process is actually running
	if !IsProcessRunning(state.PID) {
		// Process died, clean up stale state
		_ = RemoveState()
		return nil, nil
	}

	return state, nil
}
