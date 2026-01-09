package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

const envDetectionPrompt = `Analyze this shell command and determine which development environments it needs.

Command: %s

Respond with JSON only (no markdown, no code blocks):
{
  "needs_python": true or false,
  "needs_node": true or false,
  "reasoning": "brief explanation"
}

Consider:
- Python: python, pip, pytest, poetry, pipenv, django, flask, uvicorn, gunicorn, mypy, black, ruff, pylint, etc.
- Node.js: node, npm, npx, yarn, pnpm, webpack, vite, tsc, tsx, eslint, prettier, jest, mocha, etc.
- Build tools like 'make' may run either - check if the command suggests Python (e.g., pytest, python) or Node (e.g., npm, node)
- Shell scripts (*.sh) or unknown commands should have both set to false unless context is clear`

// pythonPatterns matches common Python-related commands.
var pythonPatterns = regexp.MustCompile(`^(python[23]?|pip[23]?|pytest|py\.test|poetry|pipenv|django-admin|flask|uvicorn|gunicorn|mypy|black|ruff|pylint|isort|autopep8|bandit|tox|nox|hatch|pdm|uv)$`)

// nodePatterns matches common Node.js-related commands.
var nodePatterns = regexp.MustCompile(`^(node|npm|npx|yarn|pnpm|webpack|vite|tsc|tsx|eslint|prettier|jest|mocha|vitest|esbuild|rollup|parcel|turbo|nx|lerna|bun)$`)

// DetectEnvironmentNeeds uses an LLM to analyze a command and determine environment needs.
func DetectEnvironmentNeeds(ctx context.Context, client llm.Client, command string) (*EnvironmentNeeds, error) {
	if client == nil {
		return nil, fmt.Errorf("LLM client is nil")
	}

	prompt := fmt.Sprintf(envDetectionPrompt, command)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	resp, err := client.Chat(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	// Clean up the response - remove markdown code blocks if present
	text := strings.TrimSpace(resp.Content)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var needs EnvironmentNeeds
	if err := json.Unmarshal([]byte(text), &needs); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response as JSON: %w", err)
	}

	return &needs, nil
}

// DetectEnvironmentNeedsFromPattern uses regex pattern matching to detect environment needs.
// Returns nil if no patterns match (unknown command).
func DetectEnvironmentNeedsFromPattern(command string) *EnvironmentNeeds {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}

	// Get the base command name (handle paths like ./venv/bin/python)
	firstWord := filepath.Base(fields[0])

	needsPython := pythonPatterns.MatchString(firstWord)
	needsNode := nodePatterns.MatchString(firstWord)

	// If no pattern matches, return nil to indicate "unknown"
	if !needsPython && !needsNode {
		return nil
	}

	return &EnvironmentNeeds{
		NeedsPython: needsPython,
		NeedsNode:   needsNode,
		Reasoning:   "pattern match",
	}
}

// filterEnvironments filters detected environments based on what the command needs.
func filterEnvironments(envs []*DetectedEnvironment, needs *EnvironmentNeeds) []*DetectedEnvironment {
	if needs == nil {
		// Unknown needs - return all environments
		return envs
	}

	var filtered []*DetectedEnvironment
	for _, env := range envs {
		switch env.Type {
		case EnvTypePythonVenv, EnvTypeConda:
			if needs.NeedsPython {
				filtered = append(filtered, env)
			}
		case EnvTypeNVM, EnvTypeFNM, EnvTypeNodeModules:
			if needs.NeedsNode {
				filtered = append(filtered, env)
			}
		}
	}
	return filtered
}
