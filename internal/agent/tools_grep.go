package agent

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SearchCodeTool searches for patterns in the codebase.
type SearchCodeTool struct {
	workingDir string
}

// NewSearchCodeTool creates a new code search tool.
func NewSearchCodeTool(workingDir string) *SearchCodeTool {
	return &SearchCodeTool{
		workingDir: workingDir,
	}
}

// Name returns the tool name.
func (t *SearchCodeTool) Name() string {
	return "search_code"
}

// Description returns the tool description.
func (t *SearchCodeTool) Description() string {
	return `Search for a pattern in the codebase using regex. Returns matching lines with file paths and line numbers. Similar to grep -rn. Use this to find function definitions, error messages, configuration values, or any text pattern in the source code.`
}

// Parameters returns the JSON Schema for the tool's parameters.
func (t *SearchCodeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regex pattern to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Optional: Directory or file to search in (defaults to working directory)",
			},
			"file_pattern": map[string]any{
				"type":        "string",
				"description": "Optional: Glob pattern to filter files (e.g., '*.go', '*.py', '*.js')",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Optional: Maximum number of results to return (default: 50)",
			},
			"case_insensitive": map[string]any{
				"type":        "boolean",
				"description": "Optional: Perform case-insensitive search (default: false)",
			},
		},
		"required": []string{"pattern"},
	}
}

// Execute searches for the pattern.
func (t *SearchCodeTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return "Missing required parameter: pattern", true
	}

	// Determine search path
	searchPath := t.workingDir
	if p, ok := args["path"].(string); ok && p != "" {
		if filepath.IsAbs(p) {
			searchPath = p
		} else {
			searchPath = filepath.Join(t.workingDir, p)
		}
	}

	// Security check
	absPath, err := filepath.Abs(searchPath)
	if err != nil {
		return fmt.Sprintf("Invalid path: %v", err), true
	}
	absWorkDir, _ := filepath.Abs(t.workingDir)
	if !strings.HasPrefix(absPath, absWorkDir) {
		return "Access denied: path is outside working directory", true
	}

	// Parse options
	maxResults := 50
	if m, ok := args["max_results"].(float64); ok && m > 0 {
		maxResults = int(m)
		if maxResults > 200 {
			maxResults = 200 // Cap at 200
		}
	}

	filePattern := ""
	if fp, ok := args["file_pattern"].(string); ok {
		filePattern = fp
	}

	caseInsensitive := false
	if ci, ok := args["case_insensitive"].(bool); ok {
		caseInsensitive = ci
	}

	// Compile regex
	if caseInsensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Sprintf("Invalid regex pattern: %v", err), true
	}

	// Search
	var results []string
	resultCount := 0

	err = filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Skip errors
		}

		// Skip directories
		if d.IsDir() {
			// Skip hidden directories and common ignore patterns
			name := d.Name()
			if strings.HasPrefix(name, ".") ||
				name == "node_modules" ||
				name == "vendor" ||
				name == "__pycache__" ||
				name == "dist" ||
				name == "build" ||
				name == "target" {
				return filepath.SkipDir
			}
			return nil
		}

		// Check file pattern
		if filePattern != "" {
			matched, _ := filepath.Match(filePattern, d.Name())
			if !matched {
				return nil
			}
		}

		// Skip binary and large files
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > 1024*1024 { // Skip files > 1MB
			return nil
		}

		// Read file
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Skip binary files
		if isBinaryContent(content) {
			return nil
		}

		// Search for pattern
		lines := strings.Split(string(content), "\n")
		relPath, _ := filepath.Rel(t.workingDir, path)

		for lineNum, line := range lines {
			if resultCount >= maxResults {
				return filepath.SkipAll
			}

			if re.MatchString(line) {
				// Truncate long lines
				displayLine := line
				if len(displayLine) > 200 {
					displayLine = displayLine[:200] + "..."
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", relPath, lineNum+1, strings.TrimSpace(displayLine)))
				resultCount++
			}
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll && err != context.Canceled {
		return fmt.Sprintf("Error searching: %v", err), true
	}

	if len(results) == 0 {
		return fmt.Sprintf("No matches found for pattern: %s", pattern), false
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d matches for pattern: %s\n", len(results), pattern))
	output.WriteString(strings.Repeat("-", 60) + "\n")
	output.WriteString(strings.Join(results, "\n"))

	if resultCount >= maxResults {
		output.WriteString(fmt.Sprintf("\n\n... [results limited to %d matches]", maxResults))
	}

	return output.String(), false
}
