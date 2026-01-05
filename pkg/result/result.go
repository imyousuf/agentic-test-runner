// Package result defines the output types for ATR analysis results.
// This package is designed to be reusable for both CLI and MCP server outputs.
package result

import "time"

// Status represents the outcome of a command execution.
type Status string

const (
	// StatusSuccess indicates the command completed successfully.
	StatusSuccess Status = "SUCCESS"
	// StatusFailure indicates the command failed.
	StatusFailure Status = "FAILURE"
)

// AnalysisResult is the final output of an ATR analysis.
type AnalysisResult struct {
	// Status indicates whether the command succeeded or failed.
	Status Status `json:"status"`

	// Summary is a brief one-sentence description of what happened.
	Summary string `json:"summary"`

	// RootCause provides a detailed explanation of why the failure occurred.
	// Empty if Status is SUCCESS.
	RootCause string `json:"root_cause,omitempty"`

	// Recommendations is a list of actionable steps to fix the issue.
	// Empty if Status is SUCCESS.
	Recommendations []string `json:"recommendations,omitempty"`

	// CommandResult contains details about the original command execution.
	CommandResult *CommandResult `json:"command_result"`

	// AgentMetrics contains statistics about the agent's analysis process.
	AgentMetrics *AgentMetrics `json:"agent_metrics,omitempty"`
}

// CommandResult contains information about the executed command.
type CommandResult struct {
	// Command is the command that was executed.
	Command string `json:"command"`

	// WorkingDir is the directory where the command was executed.
	WorkingDir string `json:"working_dir"`

	// ExitCode is the exit code of the command.
	ExitCode int `json:"exit_code"`

	// Duration is how long the command took to execute.
	Duration time.Duration `json:"duration"`

	// Stdout is the standard output of the command.
	Stdout string `json:"stdout,omitempty"`

	// Stderr is the standard error output of the command.
	Stderr string `json:"stderr,omitempty"`

	// TimedOut indicates if the command was killed due to timeout.
	TimedOut bool `json:"timed_out"`
}

// AgentMetrics contains statistics about the agent's analysis process.
type AgentMetrics struct {
	// ToolCallsMade is the total number of tool calls made during analysis.
	ToolCallsMade int `json:"tool_calls_made"`

	// IterationCount is the number of agent loop iterations.
	IterationCount int `json:"iteration_count"`

	// TotalDuration is the total time spent in analysis.
	TotalDuration time.Duration `json:"total_duration"`

	// LLMCalls is the number of LLM API calls made.
	LLMCalls int `json:"llm_calls"`

	// TokensUsed is the total number of tokens used (if available).
	TokensUsed int `json:"tokens_used,omitempty"`
}

// IsSuccess returns true if the analysis indicates success.
func (r *AnalysisResult) IsSuccess() bool {
	return r.Status == StatusSuccess
}

// IsFailure returns true if the analysis indicates failure.
func (r *AnalysisResult) IsFailure() bool {
	return r.Status == StatusFailure
}
