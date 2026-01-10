package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ClaudeCLIAdapter implements CLIAdapter for Claude CLI.
type ClaudeCLIAdapter struct{}

// claudeResponse represents the JSON output from Claude CLI.
type claudeResponse struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	Result   string `json:"result"`
	Duration string `json:"duration_ms,omitempty"`
	Usage    *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
}

// Executable returns the Claude CLI executable name.
func (c *ClaudeCLIAdapter) Executable() string {
	return "claude"
}

// BuildArgs builds CLI arguments for Claude CLI.
func (c *ClaudeCLIAdapter) BuildArgs(prompt string, tools []string) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "json",
	}

	// Add allowed tools if specified
	if len(tools) > 0 {
		args = append(args, "--allowedTools", strings.Join(tools, ","))
	}

	return args
}

// ParseResponse parses Claude CLI JSON output.
func (c *ClaudeCLIAdapter) ParseResponse(output []byte) (*CLIResult, error) {
	var resp claudeResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Check for error response
	if resp.Type == "result" && resp.Subtype == "error" {
		return nil, fmt.Errorf("CLI error: %s", resp.Result)
	}

	result := &CLIResult{
		Response:  resp.Result,
		SessionID: resp.SessionID,
	}

	// Extract token usage if available
	if resp.Usage != nil {
		result.TokensUsed = &TokenUsage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}

	return result, nil
}
