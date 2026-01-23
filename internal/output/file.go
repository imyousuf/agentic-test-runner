package output

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// GetOutputDir returns ~/.atr/outputs/, creating it if needed.
func GetOutputDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	outputDir := filepath.Join(homeDir, ".atr", "outputs")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	return outputDir, nil
}

// SaveOutput writes command output to a timestamped file.
// Returns the file path.
func SaveOutput(stdout, stderr, command, cwd string) (string, error) {
	outputDir, err := GetOutputDir()
	if err != nil {
		return "", err
	}

	// Generate filename: run-YYYYMMDD-HHMMSS-<short-hash>.log
	now := time.Now()
	timestamp := now.Format("20060102-150405")

	// Create a short hash from command and timestamp for uniqueness
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%d", command, cwd, now.UnixNano())))
	shortHash := fmt.Sprintf("%x", hash[:4])

	filename := fmt.Sprintf("run-%s-%s.log", timestamp, shortHash)
	filepath := filepath.Join(outputDir, filename)

	// Build file content
	content := buildOutputContent(stdout, stderr, command, cwd, now)

	// Write to file
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write output file: %w", err)
	}

	return filepath, nil
}

// buildOutputContent creates the formatted output file content.
func buildOutputContent(stdout, stderr, command, cwd string, timestamp time.Time) string {
	var content string

	content += "# ATR Command Output\n"
	content += fmt.Sprintf("# Command: %s\n", command)
	content += fmt.Sprintf("# Directory: %s\n", cwd)
	content += fmt.Sprintf("# Timestamp: %s\n", timestamp.Format(time.RFC3339))
	content += "\n"

	content += "=== STDOUT ===\n"
	if stdout != "" {
		content += stdout
		if len(stdout) > 0 && stdout[len(stdout)-1] != '\n' {
			content += "\n"
		}
	} else {
		content += "(empty)\n"
	}
	content += "\n"

	content += "=== STDERR ===\n"
	if stderr != "" {
		content += stderr
		if len(stderr) > 0 && stderr[len(stderr)-1] != '\n' {
			content += "\n"
		}
	} else {
		content += "(empty)\n"
	}

	return content
}
