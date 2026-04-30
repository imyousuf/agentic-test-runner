package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// ComputerStateFileName is the name of the computer daemon state file.
	ComputerStateFileName = "computer.state"
	// ComputerLogFileName is the name of the computer daemon log file.
	ComputerLogFileName = "computer.log"
)

// ComputerState represents the persisted state of a running computer-control daemon.
type ComputerState struct {
	PID       int       `json:"pid"`
	Endpoint  string    `json:"endpoint"`
	Mode      string    `json:"mode"`
	StartedAt time.Time `json:"started_at"`
}

// ComputerStateFilePath returns the path to the computer state file.
func ComputerStateFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".atr", ComputerStateFileName), nil
}

// ComputerLogFilePath returns the path to the computer daemon log file.
func ComputerLogFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".atr", ComputerLogFileName), nil
}

// LoadComputerState loads the computer state from disk.
func LoadComputerState() (*ComputerState, error) {
	path, err := ComputerStateFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read computer state file: %w", err)
	}
	var state ComputerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse computer state file: %w", err)
	}
	return &state, nil
}

// SaveComputerState saves the computer state to disk.
func SaveComputerState(state *ComputerState) error {
	path, err := ComputerStateFilePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal computer state: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write computer state file: %w", err)
	}
	return nil
}

// RemoveComputerState removes the computer state file.
func RemoveComputerState() error {
	path, err := ComputerStateFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove computer state file: %w", err)
	}
	return nil
}

// GetRunningComputerState returns the state if a computer daemon is running, nil otherwise.
// It cleans up stale state files for processes that no longer exist.
func GetRunningComputerState() (*ComputerState, error) {
	state, err := LoadComputerState()
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	if !IsProcessRunning(state.PID) {
		if err := RemoveComputerState(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove stale computer state file: %v\n", err)
		}
		return nil, nil
	}
	return state, nil
}
