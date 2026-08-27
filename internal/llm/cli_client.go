// Package llm provides LLM client implementations.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	provider   llm.Provider
	executable string
	model      string // Model alias: opus, sonnet, haiku
	timeout    time.Duration
	workingDir string
	verbose    bool // Enable debug logging
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

	// Normalize model name to Claude CLI alias
	model := normalizeClaudeModel(cfg.Model)

	return &cliClient{
		provider:   llm.ProviderClaudeCLI,
		executable: path,
		model:      model,
		timeout:    cliTimeout(cfg),
		verbose:    cfg.Verbose,
	}, nil
}

// defaultCLITimeout bounds one CLI invocation when the config does not say.
//
// Long, because a CLI backend does not make an API round trip: one invocation
// drives an entire browser run and emits a script at the end of it.
const defaultCLITimeout = 10 * time.Minute

// cliTimeout honours the configured cli.timeout.
//
// It used to be hardcoded, so `cli.timeout: 30m` was reported by
// `atr config show` and then ignored: compiles died at exactly ten minutes
// however high the setting was.
func cliTimeout(cfg llm.Config) time.Duration {
	if cfg.Timeout > 0 {
		return cfg.Timeout
	}
	return defaultCLITimeout
}

// normalizeClaudeModel converts model names to Claude CLI model IDs.
func normalizeClaudeModel(model string) string {
	switch strings.ToLower(model) {
	case "opus", "claude-opus", "claude-opus-4":
		return "claude-opus-4-6"
	case "sonnet", "claude-sonnet", "claude-sonnet-4":
		return "claude-sonnet-4-5"
	case "haiku", "claude-haiku", "claude-haiku-4":
		return "claude-haiku-4-5"
	default:
		// If it looks like a full model name, pass it through
		if strings.HasPrefix(model, "claude-") {
			return model
		}
		// Default: let CLI pick its default
		return ""
	}
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
		provider:   llm.ProviderGeminiCLI,
		executable: path,
		timeout:    cliTimeout(cfg),
		verbose:    cfg.Verbose,
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
		Content:      result.Content,
		FinishReason: "stop",
		Usage:        result.Usage,
	}, nil
}

// ChatWithHistory is like Chat but uses the provided history.
func (c *cliClient) ChatWithHistory(ctx context.Context, history []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	return c.Chat(ctx, history, tools)
}

// Model returns the model name.
func (c *cliClient) Model() string {
	if c.model != "" {
		return c.model
	}
	switch c.provider {
	case llm.ProviderClaudeCLI:
		return "claude-cli (default)"
	case llm.ProviderGeminiCLI:
		return "gemini-cli (default)"
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
			// The tool's name, not the call id: this is prose the model
			// reads, and "Tool result for toolu_01Abc" tells it nothing.
			label := msg.ToolName
			if label == "" {
				label = msg.ToolCallID
			}
			parts = append(parts, fmt.Sprintf("Tool result for %s: %s", label, msg.Content))
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
	// Note: MCP server discovers browser via ~/.atr/browser.state file
	// which is written by `atr run --behavior` before invoking the CLI
	config := map[string]any{
		"mcpServers": map[string]any{
			"atr-browser": map[string]any{
				"command": atrPath,
				"args":    []string{"mcp", "serve"},
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

	// ATR's tools come from two places and only one of them is MCP.
	//
	// The browser and computer tools are served by `atr mcp serve`, so they
	// carry that server's prefix. The command-analysis tools are ATR's own —
	// they are not in the MCP server at all, and naming them as though they
	// were produced an allowlist made entirely of tools that do not exist. The
	// CLI has its own equivalents, so those are what it is allowed to use.
	seen := make(map[string]bool, len(tools))
	var allowed []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		allowed = append(allowed, name)
	}

	for _, tool := range tools {
		if native, ok := cliNativeTools[tool.Name]; ok {
			add(native)
			continue
		}
		add("mcp__atr-browser__" + tool.Name)
	}
	return allowed
}

// cliNativeTools maps ATR's own tool names onto the CLI's equivalents.
var cliNativeTools = map[string]string{
	"execute_command": "Bash",
	"read_file":       "Read",
	"search_code":     "Grep",
}

// verboseLog prints a debug message if verbose mode is enabled.
func (c *cliClient) verboseLog(format string, args ...interface{}) {
	if c.verbose {
		fmt.Fprintf(os.Stderr, "[ATR CLI DEBUG] "+format+"\n", args...)
	}
}

// sanitizeArgsForLog returns args with the prompt truncated for cleaner logging.
func (c *cliClient) sanitizeArgsForLog(args []string) []string {
	result := make([]string, len(args))
	for i, arg := range args {
		// Truncate long prompts
		if i > 0 && args[i-1] == "-p" && len(arg) > 100 {
			result[i] = arg[:100] + "...[truncated]"
		} else {
			result[i] = arg
		}
	}
	return result
}

// execute runs the CLI with the given prompt and configuration.
func (c *cliClient) execute(ctx context.Context, prompt, mcpConfig string, allowedTools []string) (cliResult, error) {
	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Build command arguments
	args := c.buildArgs(prompt, mcpConfig, allowedTools)

	c.verboseLog("Executing CLI: %s", c.executable)
	c.verboseLog("Arguments: %v", c.sanitizeArgsForLog(args))
	if mcpConfig != "" {
		c.verboseLog("MCP config: %s", mcpConfig)
	}
	// Create command
	cmd := exec.CommandContext(execCtx, c.executable, args...)
	if c.workingDir != "" {
		cmd.Dir = c.workingDir
	}

	// MCP server discovers browser via ~/.atr/browser.state file
	cmd.Env = os.Environ()

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout

	// In verbose mode, stream stderr to console in real-time for debugging
	if c.verbose {
		cmd.Stderr = io.MultiWriter(&stderr, os.Stderr)
	} else {
		cmd.Stderr = &stderr
	}

	c.verboseLog("Starting CLI process...")
	startTime := time.Now()

	// Run command
	if err := cmd.Run(); err != nil {
		duration := time.Since(startTime)
		c.verboseLog("CLI process failed after %v: %v", duration, err)

		// Always print captured output in verbose mode on failure/interrupt
		if c.verbose {
			if stdout.Len() > 0 {
				fmt.Fprintf(os.Stderr, "[ATR CLI DEBUG] Captured stdout (%d bytes):\n%s\n", stdout.Len(), stdout.String())
			} else {
				fmt.Fprintf(os.Stderr, "[ATR CLI DEBUG] No stdout captured\n")
			}
			if stderr.Len() > 0 {
				fmt.Fprintf(os.Stderr, "[ATR CLI DEBUG] Captured stderr (%d bytes):\n%s\n", stderr.Len(), stderr.String())
			} else {
				fmt.Fprintf(os.Stderr, "[ATR CLI DEBUG] No stderr captured\n")
			}
		}

		if execCtx.Err() == context.DeadlineExceeded {
			return cliResult{}, fmt.Errorf("CLI execution timed out after %v", c.timeout)
		}
		if ctx.Err() == context.Canceled {
			return cliResult{}, fmt.Errorf("CLI execution interrupted after %v", duration)
		}
		// Include both stdout and stderr in error message for debugging
		errMsg := err.Error()
		if stderr.Len() > 0 {
			errMsg = fmt.Sprintf("%s\nstderr: %s", errMsg, stderr.String())
		}
		if stdout.Len() > 0 {
			errMsg = fmt.Sprintf("%s\nstdout: %s", errMsg, stdout.String())
		}
		return cliResult{}, fmt.Errorf("CLI execution failed: %s", errMsg)
	}

	c.verboseLog("CLI process completed in %v", time.Since(startTime))
	c.verboseLog("stdout length: %d bytes, stderr length: %d bytes", stdout.Len(), stderr.Len())

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
		"--dangerously-skip-permissions", // Required for automation - skip interactive permission prompts
	}

	// Add model if specified (opus, sonnet, haiku, or full model name)
	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	// Add verbose flag if enabled (passes through to Claude CLI)
	if c.verbose {
		args = append(args, "--verbose")
	}

	// Add MCP config if provided
	if mcpConfig != "" {
		args = append(args, "--mcp-config", mcpConfig)
	}

	// Add allowed tools to restrict what Claude can use
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

// cliResult is what one CLI invocation produced.
type cliResult struct {
	Content string
	Usage   *llm.Usage
}

// parseResponse parses the CLI output based on the provider.
func (c *cliClient) parseResponse(output []byte) (cliResult, error) {
	switch c.provider {
	case llm.ProviderClaudeCLI:
		return c.parseClaudeResponse(output)
	case llm.ProviderGeminiCLI:
		return c.parseGeminiResponse(output)
	default:
		return cliResult{Content: string(output)}, nil
	}
}

// claudeJSONResponse represents Claude CLI JSON output.
type claudeJSONResponse struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Result  string `json:"result"`
	// IsError is how the CLI reports a failed run. Matching only on
	// subtype=="error" missed it, and an error message was handed back to the
	// caller as though it were the model's answer.
	IsError      bool         `json:"is_error"`
	Duration     int64        `json:"duration_ms,omitempty"`
	Usage        *claudeUsage `json:"usage,omitempty"`
	TotalCostUSD float64      `json:"total_cost_usd,omitempty"`
	SessionID    string       `json:"session_id,omitempty"`
}

// claudeUsage mirrors the CLI's usage block. The cache figures are reported
// separately from input_tokens, so a caller that ignores them undercounts the
// prompt badly on any run that reuses a cached prefix.
type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// toLLMUsage folds the cache figures into the prompt count, so the number is
// comparable with what the other providers report.
func (u *claudeUsage) toLLMUsage() *llm.Usage {
	if u == nil {
		return nil
	}
	prompt := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	return &llm.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
	}
}

// parseClaudeResponse parses Claude CLI JSON output.
func (c *cliClient) parseClaudeResponse(output []byte) (cliResult, error) {
	resp, err := decodeClaudeOutput(output)
	if err != nil {
		// Return error with raw output for debugging
		return cliResult{}, fmt.Errorf("failed to parse CLI response as JSON: %w\nRaw output: %s", err, string(output))
	}

	if resp.IsError || resp.Subtype == "error" {
		return cliResult{}, fmt.Errorf("CLI error: %s", resp.Result)
	}

	if c.verbose && resp.Usage != nil {
		c.verboseLog("tokens in=%d out=%d cache_read=%d cache_write=%d cost=$%.4f",
			resp.Usage.InputTokens, resp.Usage.OutputTokens,
			resp.Usage.CacheReadInputTokens, resp.Usage.CacheCreationInputTokens,
			resp.TotalCostUSD)
	}

	return cliResult{Content: resp.Result, Usage: resp.Usage.toLLMUsage()}, nil
}

// decodeClaudeOutput reads the CLI's answer in either shape it produces.
//
// With --output-format json the CLI answers with one result object. Adding
// --verbose turns the same flag into stream-json: an array of events for the
// whole run — the session init, each assistant turn, each tool call — with the
// answer in a final event of type "result". ATR passes --verbose through from
// its own flag, so the shape changes underneath it depending on how the user
// invoked atr, and only one of the two used to parse.
//
// The last result event wins: earlier ones belong to earlier turns.
func decodeClaudeOutput(output []byte) (claudeJSONResponse, error) {
	trimmed := bytes.TrimSpace(output)
	if !bytes.HasPrefix(trimmed, []byte("[")) {
		var resp claudeJSONResponse
		err := json.Unmarshal(trimmed, &resp)
		return resp, err
	}

	var events []claudeJSONResponse
	if err := json.Unmarshal(trimmed, &events); err != nil {
		return claudeJSONResponse{}, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "result" {
			return events[i], nil
		}
	}
	return claudeJSONResponse{}, fmt.Errorf("no result event in %d streamed events", len(events))
}

// parseGeminiResponse parses Gemini CLI output.
func (c *cliClient) parseGeminiResponse(output []byte) (cliResult, error) {
	// Gemini CLI outputs plain text by default, and reports no usage.
	return cliResult{Content: string(output)}, nil
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
		"/usr/local/bin/claude",    // Manual installation / Homebrew (Intel)
		"/opt/homebrew/bin/claude", // Homebrew (Apple Silicon)
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
