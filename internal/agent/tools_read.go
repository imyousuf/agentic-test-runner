package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadFileTool reads file contents for context.
type ReadFileTool struct {
	workingDir string
}

// NewReadFileTool creates a new file reading tool.
func NewReadFileTool(workingDir string) *ReadFileTool {
	return &ReadFileTool{
		workingDir: workingDir,
	}
}

// Name returns the tool name.
func (t *ReadFileTool) Name() string {
	return "read_file"
}

// Description returns the tool description.
func (t *ReadFileTool) Description() string {
	return `Read the contents of a file. Use this to examine source code, configuration files, log files, or any text file. Supports both absolute and relative paths (relative to working directory). You can optionally specify a line range to read only a portion of the file.`
}

// Parameters returns the JSON Schema for the tool's parameters.
func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file (absolute or relative to working directory)",
			},
			"start_line": map[string]any{
				"type":        "integer",
				"description": "Optional: Start reading from this line number (1-indexed)",
			},
			"end_line": map[string]any{
				"type":        "integer",
				"description": "Optional: Stop reading at this line number (inclusive)",
			},
		},
		"required": []string{"path"},
	}
}

// Execute reads the file contents.
func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "Missing required parameter: path", true
	}

	// Resolve path relative to working directory
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.workingDir, path)
	}

	// Clean the path
	path = filepath.Clean(path)

	// Security: Ensure path is within working directory or is absolute and exists
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Sprintf("Invalid path: %v", err), true
	}

	absWorkDir, err := filepath.Abs(t.workingDir)
	if err != nil {
		return fmt.Sprintf("Invalid working directory: %v", err), true
	}

	// Allow reading files within the working directory tree
	if !strings.HasPrefix(absPath, absWorkDir) {
		// Also allow reading common system files for diagnostics
		allowedPrefixes := []string{"/etc/", "/usr/", "/var/log/"}
		allowed := false
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(absPath, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Sprintf("Access denied: path '%s' is outside working directory and not in allowed system paths", path), true
		}
	}

	// Read the file
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("File not found: %s", path), true
		}
		return fmt.Sprintf("Error reading file: %v", err), true
	}

	// Check for binary content
	if isBinaryContent(content) {
		return fmt.Sprintf("File appears to be binary: %s", path), true
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	// Handle line range
	startLine := 1
	endLine := totalLines

	if start, ok := args["start_line"].(float64); ok && start > 0 {
		startLine = int(start)
	}
	if end, ok := args["end_line"].(float64); ok && end > 0 {
		endLine = int(end)
	}

	// Validate and adjust line numbers
	if startLine < 1 {
		startLine = 1
	}
	if startLine > totalLines {
		return fmt.Sprintf("Start line %d exceeds file length (%d lines)", startLine, totalLines), true
	}
	if endLine > totalLines {
		endLine = totalLines
	}
	if endLine < startLine {
		return fmt.Sprintf("End line %d is before start line %d", endLine, startLine), true
	}

	// Limit output size
	maxLines := 500
	if endLine-startLine+1 > maxLines {
		endLine = startLine + maxLines - 1
	}

	selectedLines := lines[startLine-1 : endLine]

	// Format output with line numbers
	var output strings.Builder
	output.WriteString(fmt.Sprintf("File: %s (lines %d-%d of %d)\n", path, startLine, endLine, totalLines))
	output.WriteString(strings.Repeat("-", 60) + "\n")

	for i, line := range selectedLines {
		lineNum := startLine + i
		output.WriteString(fmt.Sprintf("%5d | %s\n", lineNum, line))
	}

	if endLine < totalLines {
		output.WriteString(fmt.Sprintf("\n... [%d more lines]\n", totalLines-endLine))
	}

	return output.String(), false
}

// isBinaryContent checks if content appears to be binary.
func isBinaryContent(content []byte) bool {
	// Check first 512 bytes for null bytes or high proportion of non-printable chars
	checkLen := len(content)
	if checkLen > 512 {
		checkLen = 512
	}

	nullCount := 0
	nonPrintable := 0

	for i := 0; i < checkLen; i++ {
		b := content[i]
		if b == 0 {
			nullCount++
		}
		// Non-printable and non-whitespace
		if b < 32 && b != '\n' && b != '\r' && b != '\t' {
			nonPrintable++
		}
	}

	// If more than 10% null bytes or non-printable, consider it binary
	return nullCount > checkLen/10 || nonPrintable > checkLen/10
}
