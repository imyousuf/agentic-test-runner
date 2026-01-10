package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GeminiCLIAdapter implements CLIAdapter for Gemini CLI.
type GeminiCLIAdapter struct{}

// geminiResponse represents the JSON output from Gemini CLI.
type geminiResponse struct {
	SessionID string `json:"session_id,omitempty"`
	Response  string `json:"response"`
	Stats     *struct {
		Models map[string]struct {
			Tokens struct {
				Input  int `json:"input"`
				Output int `json:"output"`
				Total  int `json:"total"`
			} `json:"tokens"`
		} `json:"models,omitempty"`
		Tools struct {
			TotalCalls int `json:"totalCalls"`
		} `json:"tools,omitempty"`
	} `json:"stats,omitempty"`
	Error string `json:"error,omitempty"`
}

// Executable returns the Gemini CLI executable name.
func (g *GeminiCLIAdapter) Executable() string {
	return "gemini"
}

// BuildArgs builds CLI arguments for Gemini CLI.
func (g *GeminiCLIAdapter) BuildArgs(prompt string, tools []string) []string {
	args := []string{
		prompt,
		"--output-format", "json",
		"-y", // Auto-approve tool execution
	}

	// Add allowed tools if specified
	if len(tools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(tools, ","))
	}

	return args
}

// ParseResponse parses Gemini CLI JSON output.
func (g *GeminiCLIAdapter) ParseResponse(output []byte) (*CLIResult, error) {
	var resp geminiResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Check for error response
	if resp.Error != "" {
		return nil, fmt.Errorf("CLI error: %s", resp.Error)
	}

	result := &CLIResult{
		Response:  resp.Response,
		SessionID: resp.SessionID,
	}

	// Extract token usage if available
	if resp.Stats != nil && resp.Stats.Models != nil {
		for _, model := range resp.Stats.Models {
			result.TokensUsed = &TokenUsage{
				InputTokens:  model.Tokens.Input,
				OutputTokens: model.Tokens.Output,
				TotalTokens:  model.Tokens.Total,
			}
			break // Take the first model's stats
		}
	}

	return result, nil
}
