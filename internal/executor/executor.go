// Package executor provides shell command execution functionality.
package executor

import (
	"context"
	"time"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// Result represents the output of a command execution.
type Result struct {
	// Command is the command that was executed.
	Command string
	// ExitCode is the exit code of the command (0 = success).
	ExitCode int
	// Stdout is the captured standard output.
	Stdout string
	// Stderr is the captured standard error.
	Stderr string
	// Duration is how long the command took to execute.
	Duration time.Duration
	// TimedOut indicates if the command was killed due to timeout.
	TimedOut bool
	// Error is any Go-level error (not command failure).
	Error error
}

// Success returns true if the command executed successfully (exit code 0).
func (r *Result) Success() bool {
	return r.ExitCode == 0 && r.Error == nil && !r.TimedOut
}

// Failed returns true if the command failed.
func (r *Result) Failed() bool {
	return !r.Success()
}

// CombinedOutput returns stdout and stderr combined.
func (r *Result) CombinedOutput() string {
	output := r.Stdout
	if r.Stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += r.Stderr
	}
	return output
}

// Executor runs shell commands.
type Executor interface {
	// Execute runs a command in the specified working directory.
	Execute(ctx context.Context, command, cwd string) (*Result, error)

	// Shell returns the shell being used (e.g., "/bin/bash", "powershell.exe").
	Shell() string
}

// Config holds configuration for the executor.
type Config struct {
	// CommandTimeout is the maximum time for a single command.
	CommandTimeout time.Duration
	// MaxOutputSize is the maximum bytes to capture from command output.
	MaxOutputSize int
	// Environment contains environment detection configuration.
	Environment EnvironmentConfig
	// LLMClient is an optional LLM client for intelligent environment detection.
	LLMClient llm.Client
}

// DefaultConfig returns the default executor configuration.
func DefaultConfig() *Config {
	return &Config{
		CommandTimeout: 2 * time.Minute,
		MaxOutputSize:  100 * 1024, // 100KB
		Environment: EnvironmentConfig{
			AutoDetect: true,
		},
	}
}

// New creates a new Executor for the current platform.
func New(cfg *Config) Executor {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &shellExecutor{
		shell:          detectShell(),
		commandTimeout: cfg.CommandTimeout,
		maxOutputSize:  cfg.MaxOutputSize,
		envConfig:      cfg.Environment,
		llmClient:      cfg.LLMClient,
	}
}
