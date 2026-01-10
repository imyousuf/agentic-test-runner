// Package llm provides LLM client implementations.
package llm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// CLIExecutorConfig holds configuration for CLI execution.
type CLIExecutorConfig struct {
	// Provider specifies which CLI to use.
	Provider llm.Provider
	// Timeout is the maximum time for CLI execution.
	Timeout time.Duration
	// WorkingDir is the working directory for CLI execution.
	WorkingDir string
}

// CLIExecutor executes prompts via CLI tools.
type CLIExecutor struct {
	config  CLIExecutorConfig
	adapter CLIAdapter
}

// CLIResult represents the result of CLI execution.
type CLIResult struct {
	// Response is the text response from the CLI.
	Response string
	// SessionID is the session identifier (if any).
	SessionID string
	// TokensUsed contains token usage information (if available).
	TokensUsed *TokenUsage
	// RawOutput is the raw JSON output from the CLI.
	RawOutput []byte
}

// TokenUsage contains token usage information from CLI execution.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// CLIAdapter defines the interface for CLI-specific implementations.
type CLIAdapter interface {
	// BuildArgs builds the CLI arguments for the given prompt.
	BuildArgs(prompt string, tools []string) []string
	// ParseResponse parses the CLI JSON output into a CLIResult.
	ParseResponse(output []byte) (*CLIResult, error)
	// Executable returns the CLI executable name.
	Executable() string
}

// NewCLIExecutor creates a new CLI executor.
func NewCLIExecutor(cfg CLIExecutorConfig) (*CLIExecutor, error) {
	var adapter CLIAdapter

	switch cfg.Provider {
	case llm.ProviderClaudeCLI:
		adapter = &ClaudeCLIAdapter{}
	case llm.ProviderGeminiCLI:
		adapter = &GeminiCLIAdapter{}
	default:
		return nil, fmt.Errorf("unsupported CLI provider: %s", cfg.Provider)
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute
	}

	return &CLIExecutor{
		config:  cfg,
		adapter: adapter,
	}, nil
}

// Execute runs a prompt through the CLI and returns the result.
func (e *CLIExecutor) Execute(ctx context.Context, prompt string, allowedTools []string) (*CLIResult, error) {
	// Build CLI arguments
	args := e.adapter.BuildArgs(prompt, allowedTools)

	// Create command
	cmd := exec.CommandContext(ctx, e.adapter.Executable(), args...)

	if e.config.WorkingDir != "" {
		cmd.Dir = e.config.WorkingDir
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Replace context in command
	cmd = exec.CommandContext(execCtx, e.adapter.Executable(), args...)
	if e.config.WorkingDir != "" {
		cmd.Dir = e.config.WorkingDir
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command
	if err := cmd.Run(); err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("CLI execution timed out after %v", e.config.Timeout)
		}
		// Include stderr in error message
		errMsg := err.Error()
		if stderr.Len() > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, stderr.String())
		}
		return nil, fmt.Errorf("CLI execution failed: %s", errMsg)
	}

	// Parse response
	result, err := e.adapter.ParseResponse(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to parse CLI response: %w (raw: %s)", err, stdout.String())
	}

	result.RawOutput = stdout.Bytes()
	return result, nil
}

// ExecuteForAnalysis executes a CLI command for failure analysis.
// This uses CLI's built-in tools (Bash, Read, Glob, Grep) to analyze failures.
func (e *CLIExecutor) ExecuteForAnalysis(ctx context.Context, prompt string) (*CLIResult, error) {
	var tools []string

	switch e.config.Provider {
	case llm.ProviderClaudeCLI:
		tools = []string{"Bash", "Read", "Glob", "Grep"}
	case llm.ProviderGeminiCLI:
		tools = []string{"run_shell_command", "read_file", "glob", "search_file_content"}
	}

	return e.Execute(ctx, prompt, tools)
}

// Provider returns the CLI provider.
func (e *CLIExecutor) Provider() llm.Provider {
	return e.config.Provider
}
