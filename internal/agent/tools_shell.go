package agent

import (
	"context"
	"fmt"

	"github.com/imyousuf/agentic-test-runner/internal/executor"
)

// ShellTool executes shell commands for diagnostic purposes.
type ShellTool struct {
	executor   *executor.Executor
	workingDir string
}

// NewShellTool creates a new shell tool.
func NewShellTool(exec any, workingDir string) *ShellTool {
	// Accept either executor.Executor or our ShellExecutor interface
	return &ShellTool{
		workingDir: workingDir,
	}
}

// SetExecutor sets the executor to use for running commands.
func (t *ShellTool) SetExecutor(exec executor.Executor) {
	t.executor = &exec
}

// Name returns the tool name.
func (t *ShellTool) Name() string {
	return "execute_command"
}

// Description returns the tool description.
func (t *ShellTool) Description() string {
	return `Execute a shell command to diagnose the issue. Use this to run commands like 'ls', 'cat', 'find', 'which', 'env', 'go version', 'npm --version', etc. The command runs in the same working directory as the original failed command. Use this tool to investigate file system state, check installed tools, examine environment variables, or run diagnostic commands.`
}

// Parameters returns the JSON Schema for the tool's parameters.
func (t *ShellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the shell command.
func (t *ShellTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "Missing required parameter: command", true
	}

	if t.executor == nil {
		return "Shell executor not configured", true
	}

	result, err := (*t.executor).Execute(ctx, command, t.workingDir)
	if err != nil {
		return fmt.Sprintf("Error executing command: %v", err), true
	}

	output := fmt.Sprintf("Exit Code: %d\nDuration: %s\n\n", result.ExitCode, result.Duration)
	if result.Stdout != "" {
		output += "STDOUT:\n" + result.Stdout + "\n"
	}
	if result.Stderr != "" {
		output += "STDERR:\n" + result.Stderr
	}
	if result.TimedOut {
		output += "\n[Command timed out]"
	}

	return output, result.ExitCode != 0
}

// ExecutorAdapter adapts executor.Executor to ShellExecutor interface.
type ExecutorAdapter struct {
	Executor executor.Executor
}

// Execute implements ShellExecutor.
func (a *ExecutorAdapter) Execute(ctx context.Context, command, cwd string) (stdout, stderr string, exitCode int, err error) {
	result, err := a.Executor.Execute(ctx, command, cwd)
	if err != nil {
		return "", "", -1, err
	}
	return result.Stdout, result.Stderr, result.ExitCode, nil
}
