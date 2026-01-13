// Package llm provides LLM client implementations.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

func init() {
	// Register CLI providers
	llm.RegisterProvider(llm.ProviderClaudeCLI, newClaudeCLIClient)
	llm.RegisterProvider(llm.ProviderGeminiCLI, newGeminiCLIClient)
}

// cliClient implements llm.Client for CLI-based backends.
// CLI backends work differently from API backends - they are autonomous agents
// that execute tools themselves via MCP rather than returning tool calls.
type cliClient struct {
	provider    llm.Provider
	executable  string
	timeout     time.Duration
	workingDir  string
	cdpEndpoint string // CDP endpoint for connecting to existing browser
}

// newClaudeCLIClient creates a new Claude CLI client.
func newClaudeCLIClient(_ context.Context, cfg llm.Config) (llm.Client, error) {
	// Find Claude CLI in common locations or PATH
	path := findClaudeCLI()
	if path == "" {
		return nil, fmt.Errorf("claude CLI not found in PATH or common locations\n" +
			"  Searched: ~/.claude/local/claude, ~/.local/bin/claude, /usr/local/bin/claude, PATH\n" +
			"  Install with: npm install -g @anthropic-ai/claude-code\n" +
			"  Or ensure claude is in your PATH")
	}

	return &cliClient{
		provider:    llm.ProviderClaudeCLI,
		executable:  path,
		timeout:     10 * time.Minute, // CLI needs longer timeout for browser automation
		cdpEndpoint: cfg.CDPEndpoint,
	}, nil
}

// newGeminiCLIClient creates a new Gemini CLI client.
func newGeminiCLIClient(_ context.Context, cfg llm.Config) (llm.Client, error) {
	// Find Gemini CLI in common locations or PATH
	path := findGeminiCLI()
	if path == "" {
		return nil, fmt.Errorf("gemini CLI not found in PATH or common locations\n" +
			"  Searched: ~/.local/bin/gemini, /usr/local/bin/gemini, PATH\n" +
			"  Install with: npm install -g @anthropic-ai/gemini-cli\n" +
			"  Or ensure gemini is in your PATH")
	}

	return &cliClient{
		provider:    llm.ProviderGeminiCLI,
		executable:  path,
		timeout:     10 * time.Minute, // CLI needs longer timeout for browser automation
		cdpEndpoint: cfg.CDPEndpoint,
	}, nil
}

// Chat sends messages to the CLI and returns a response.
// For CLI backends, the tools are provided via MCP configuration rather than
// being passed directly. The CLI executes tools autonomously.
func (c *cliClient) Chat(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	// Build prompt from messages
	prompt := c.buildPrompt(messages)

	// Build MCP config if tools are provided
	var mcpConfig string
	if len(tools) > 0 {
		var err error
		mcpConfig, err = c.buildMCPConfig(tools)
		if err != nil {
			return nil, fmt.Errorf("failed to build MCP config: %w", err)
		}
	}

	// Execute CLI
	result, err := c.execute(ctx, prompt, mcpConfig, c.getAllowedTools(tools))
	if err != nil {
		return nil, err
	}

	return &llm.Response{
		Content:      result,
		FinishReason: "stop",
	}, nil
}

// ChatWithHistory is like Chat but uses the provided history.
func (c *cliClient) ChatWithHistory(ctx context.Context, history []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	return c.Chat(ctx, history, tools)
}

// Model returns the model name (CLI uses its own default).
func (c *cliClient) Model() string {
	switch c.provider {
	case llm.ProviderClaudeCLI:
		return "claude-cli"
	case llm.ProviderGeminiCLI:
		return "gemini-cli"
	default:
		return "cli"
	}
}

// Provider returns the provider type.
func (c *cliClient) Provider() llm.Provider {
	return c.provider
}

// Close releases resources (no-op for CLI client).
func (c *cliClient) Close() error {
	return nil
}

// buildPrompt constructs a prompt string from messages.
func (c *cliClient) buildPrompt(messages []llm.Message) string {
	var parts []string

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			parts = append(parts, msg.Content)
		case llm.RoleAssistant:
			if msg.Content != "" {
				parts = append(parts, "Previous assistant response: "+msg.Content)
			}
		case llm.RoleTool:
			parts = append(parts, fmt.Sprintf("Tool result for %s: %s", msg.ToolCallID, msg.Content))
		case llm.RoleSystem:
			parts = append(parts, "System: "+msg.Content)
		}
	}

	return strings.Join(parts, "\n\n")
}

// buildMCPConfig builds MCP configuration JSON for the CLI.
func (c *cliClient) buildMCPConfig(_ []llm.Tool) (string, error) {
	// Get the ATR executable path
	atrPath, err := os.Executable()
	if err != nil {
		atrPath = "atr" // Fall back to PATH
	}

	// Build MCP server config
	config := map[string]any{
		"mcpServers": map[string]any{
			"atr-browser": map[string]any{
				"command": atrPath,
				"args":    []string{"mcp", "serve"},
				"env":     map[string]string{},
			},
		},
	}

	jsonBytes, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to serialize MCP config: %w", err)
	}
	return string(jsonBytes), nil
}

// getAllowedTools returns the list of allowed tool names for the CLI.
func (c *cliClient) getAllowedTools(tools []llm.Tool) []string {
	if len(tools) == 0 {
		return nil
	}

	// Map internal tool names to MCP tool names
	var allowed []string
	for _, tool := range tools {
		// Tools are exposed via MCP with mcp__atr-browser__ prefix
		allowed = append(allowed, fmt.Sprintf("mcp__atr-browser__%s", tool.Name))
	}
	return allowed
}

// execute runs the CLI with the given prompt and configuration.
func (c *cliClient) execute(ctx context.Context, prompt, mcpConfig string, allowedTools []string) (string, error) {
	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Build command arguments
	args := c.buildArgs(prompt, mcpConfig, allowedTools)

	// Create command
	cmd := exec.CommandContext(execCtx, c.executable, args...)
	if c.workingDir != "" {
		cmd.Dir = c.workingDir
	}

	// Set up environment with CDP endpoint if available
	cmd.Env = os.Environ()
	if c.cdpEndpoint != "" {
		cmd.Env = append(cmd.Env, "ATR_CDP_ENDPOINT="+c.cdpEndpoint)
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command
	if err := cmd.Run(); err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("CLI execution timed out after %v", c.timeout)
		}
		// Include stderr in error message
		errMsg := err.Error()
		if stderr.Len() > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, stderr.String())
		}
		return "", fmt.Errorf("CLI execution failed: %s", errMsg)
	}

	// Parse response based on provider
	return c.parseResponse(stdout.Bytes())
}

// buildArgs builds CLI arguments based on the provider.
func (c *cliClient) buildArgs(prompt, mcpConfig string, allowedTools []string) []string {
	switch c.provider {
	case llm.ProviderClaudeCLI:
		return c.buildClaudeArgs(prompt, mcpConfig, allowedTools)
	case llm.ProviderGeminiCLI:
		return c.buildGeminiArgs(prompt, mcpConfig, allowedTools)
	default:
		return nil
	}
}

// buildClaudeArgs builds arguments for Claude CLI.
func (c *cliClient) buildClaudeArgs(prompt, mcpConfig string, allowedTools []string) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "json",
	}

	// Add MCP config if provided
	if mcpConfig != "" {
		args = append(args, "--mcp-config", mcpConfig)
	}

	// Add allowed tools
	if len(allowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(allowedTools, ","))
	}

	return args
}

// buildGeminiArgs builds arguments for Gemini CLI.
func (c *cliClient) buildGeminiArgs(prompt, _ string, _ []string) []string {
	args := []string{
		"-p", prompt,
	}

	// Note: Gemini CLI uses project-level MCP config (.gemini/settings.json)
	// rather than command-line MCP config

	return args
}

// parseResponse parses the CLI output based on the provider.
func (c *cliClient) parseResponse(output []byte) (string, error) {
	switch c.provider {
	case llm.ProviderClaudeCLI:
		return c.parseClaudeResponse(output)
	case llm.ProviderGeminiCLI:
		return c.parseGeminiResponse(output)
	default:
		return string(output), nil
	}
}

// claudeJSONResponse represents Claude CLI JSON output.
type claudeJSONResponse struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	Result   string `json:"result"`
	Duration int64  `json:"duration_ms,omitempty"`
	Usage    *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
}

// parseClaudeResponse parses Claude CLI JSON output.
func (c *cliClient) parseClaudeResponse(output []byte) (string, error) {
	var resp claudeJSONResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		// Return error with raw output for debugging
		return "", fmt.Errorf("failed to parse CLI response as JSON: %w\nRaw output: %s", err, string(output))
	}

	// Check for error response
	if resp.Type == "result" && resp.Subtype == "error" {
		return "", fmt.Errorf("CLI error: %s", resp.Result)
	}

	return resp.Result, nil
}

// parseGeminiResponse parses Gemini CLI output.
func (c *cliClient) parseGeminiResponse(output []byte) (string, error) {
	// Gemini CLI outputs plain text by default
	return string(output), nil
}

// findClaudeCLI looks for the Claude CLI in common installation locations.
// It checks well-known paths first, then falls back to PATH search.
func findClaudeCLI() string {
	// Check common installation paths first
	home, _ := os.UserHomeDir()
	if home != "" {
		homePaths := []string{
			filepath.Join(home, ".claude", "local", "claude"), // Official Claude Code installer
			filepath.Join(home, ".local", "bin", "claude"),    // npm global with custom prefix
		}
		for _, p := range homePaths {
			if isExecutable(p) {
				return p
			}
		}
	}

	// System paths
	systemPaths := []string{
		"/usr/local/bin/claude",     // Manual installation / Homebrew (Intel)
		"/opt/homebrew/bin/claude",  // Homebrew (Apple Silicon)
	}
	for _, p := range systemPaths {
		if isExecutable(p) {
			return p
		}
	}

	// Fall back to PATH search
	if path, err := exec.LookPath("claude"); err == nil {
		return path
	}

	return ""
}

// findGeminiCLI looks for the Gemini CLI in common installation locations.
func findGeminiCLI() string {
	// Check common installation paths first
	home, _ := os.UserHomeDir()
	if home != "" {
		homePaths := []string{
			filepath.Join(home, ".local", "bin", "gemini"), // npm global with custom prefix
		}
		for _, p := range homePaths {
			if isExecutable(p) {
				return p
			}
		}
	}

	// System paths
	systemPaths := []string{
		"/usr/local/bin/gemini",    // Manual installation / Homebrew (Intel)
		"/opt/homebrew/bin/gemini", // Homebrew (Apple Silicon)
	}
	for _, p := range systemPaths {
		if isExecutable(p) {
			return p
		}
	}

	// Fall back to PATH search
	if path, err := exec.LookPath("gemini"); err == nil {
		return path
	}

	return ""
}

// isExecutable checks if a file exists and is executable.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	// Check if it's a regular file and has execute permission
	return info.Mode().IsRegular() && info.Mode()&0111 != 0
}
