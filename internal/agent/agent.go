package agent

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/executor"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
	"github.com/imyousuf/agentic-test-runner/pkg/result"
)

// Agent orchestrates the debugging process using an LLM and tools.
type Agent struct {
	llmClient     llm.Client
	executor      executor.Executor
	registry      Registry
	maxIterations int
	timeout       time.Duration
	verbose       bool
	// browser is set for agents that drive a page. RunBehavior needs it to
	// execute the compiled script itself, not only through tools.
	browser *browser.Browser
}

// Config holds configuration for creating an agent.
type Config struct {
	// LLMClient is the LLM client to use.
	LLMClient llm.Client
	// Executor is the shell executor for running commands.
	Executor executor.Executor
	// MaxIterations is the maximum number of agent loop iterations.
	MaxIterations int
	// Timeout is the maximum time for the entire analysis.
	Timeout time.Duration
	// WorkingDir is the working directory for tool execution.
	WorkingDir string
	// Verbose enables debug logging.
	Verbose bool
}

// AnalysisRequest contains the information about the failed command.
type AnalysisRequest struct {
	// Command is the command that was executed.
	Command string
	// WorkingDir is the directory where the command was executed.
	WorkingDir string
	// ExitCode is the exit code of the command.
	ExitCode int
	// Duration is how long the command took.
	Duration time.Duration
	// Stdout is the standard output.
	Stdout string
	// Stderr is the standard error.
	Stderr string
	// Context is user-provided context about the command.
	Context string
	// TimedOut indicates if the command timed out.
	TimedOut bool
}

// New creates a new Agent.
func New(cfg Config) *Agent {
	// Create tool registry with default tools
	registry := NewRegistry()

	// Create and configure shell tool
	shellTool := NewShellTool(nil, cfg.WorkingDir)
	shellTool.SetExecutor(cfg.Executor)
	registry.Register(shellTool)

	// Register other tools
	registry.Register(NewReadFileTool(cfg.WorkingDir))
	registry.Register(NewSearchCodeTool(cfg.WorkingDir))

	return &Agent{
		llmClient:     cfg.LLMClient,
		executor:      cfg.Executor,
		registry:      registry,
		maxIterations: cfg.MaxIterations,
		timeout:       cfg.Timeout,
		verbose:       cfg.Verbose,
	}
}

// BehaviorConfig holds configuration for behavior testing agents.
type BehaviorConfig struct {
	// LLMClient is the LLM client to use.
	LLMClient llm.Client
	// Browser is the browser instance for behavior testing.
	Browser *browser.Browser
	// MaxIterations is the maximum number of agent loop iterations.
	MaxIterations int
	// Timeout is the maximum time for the entire analysis.
	Timeout time.Duration
	// Verbose enables debug logging.
	Verbose bool
}

// BehaviorRequest contains information for behavior test execution.
type BehaviorRequest struct {
	// TestFile is the path to the .test.txt file.
	TestFile string
	// TestContent is the raw content of the test file.
	TestContent string
	// BaseURL is the base URL for the application under test.
	BaseURL string
}

// NewBehaviorAgent creates a new Agent with browser tools for behavior testing.
func NewBehaviorAgent(cfg BehaviorConfig) *Agent {
	registry := NewRegistry()

	// Register browser tools
	for _, tool := range NewBrowserTools(cfg.Browser) {
		registry.Register(tool)
	}

	return &Agent{
		llmClient:     cfg.LLMClient,
		registry:      registry,
		maxIterations: cfg.MaxIterations,
		timeout:       cfg.Timeout,
		verbose:       cfg.Verbose,
	}
}

// verboseLog prints a debug message if verbose mode is enabled.
func (a *Agent) verboseLog(format string, args ...interface{}) {
	if a.verbose {
		fmt.Fprintf(os.Stderr, "[ATR DEBUG] "+format+"\n", args...)
	}
}

// truncate shortens a string to maxLen characters, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// formatArgs converts tool arguments map to a string for logging.
func formatArgs(args map[string]any) string {
	if args == nil {
		return "{}"
	}
	var parts []string
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// ExecuteBehaviorTest runs a behavior test using the LLM to interpret and execute steps.
func (a *Agent) ExecuteBehaviorTest(ctx context.Context, req *BehaviorRequest) (*result.AnalysisResult, error) {
	startTime := time.Now()
	a.verboseLog("Starting behavior test: %s", req.TestFile)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build behavior test prompt
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: a.buildBehaviorPrompt(req)},
	}

	tools := a.registry.Definitions()
	a.verboseLog("Registered %d tools for behavior testing", len(tools))

	// Track metrics
	metrics := &result.AgentMetrics{}

	// Agent loop
	for iteration := 0; iteration < a.maxIterations; iteration++ {
		metrics.IterationCount = iteration + 1
		a.verboseLog("=== Iteration %d/%d ===", iteration+1, a.maxIterations)

		// Check context
		if err := ctx.Err(); err != nil {
			a.verboseLog("Context error: %v", err)
			return nil, fmt.Errorf("behavior test timeout after %d iterations: %w", iteration, err)
		}

		// Call LLM
		metrics.LLMCalls++
		a.verboseLog("Calling LLM with %d messages, %d tools", len(messages), len(tools))
		if a.verbose && len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			a.verboseLog("Last message role: %s, content length: %d", lastMsg.Role, len(lastMsg.Content))
		}

		llmStart := time.Now()
		resp, err := a.llmClient.Chat(ctx, messages, tools)
		llmDuration := time.Since(llmStart)

		if err != nil {
			a.verboseLog("LLM call failed after %v: %v", llmDuration, err)
			return nil, fmt.Errorf("LLM call failed at iteration %d: %w", iteration, err)
		}

		a.verboseLog("LLM response received in %v: tool_calls=%d, finish_reason=%s",
			llmDuration, len(resp.ToolCalls), resp.FinishReason)

		// Track token usage
		if resp.Usage != nil {
			metrics.TokensUsed += resp.Usage.TotalTokens
			a.verboseLog("Tokens: prompt=%d, completion=%d, total=%d",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
		}

		// If no tool calls, we have the final result
		if !resp.HasToolCalls() {
			a.verboseLog("No tool calls - parsing final result")
			a.verboseLog("Response content preview: %s", truncate(resp.Content, 200))
			metrics.TotalDuration = time.Since(startTime)

			analysisResult := a.parseBehaviorResult(resp.Content, req)
			analysisResult.AgentMetrics = metrics
			a.verboseLog("Behavior test completed with status: %s", analysisResult.Status)

			return analysisResult, nil
		}

		// Log tool calls
		for i, tc := range resp.ToolCalls {
			a.verboseLog("Tool call %d: %s(%s)", i+1, tc.Name, truncate(formatArgs(tc.Arguments), 100))
		}

		// Add assistant message with tool calls
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			ToolCalls: resp.ToolCalls,
		})

		// Execute tool calls
		for _, tc := range resp.ToolCalls {
			metrics.ToolCallsMade++
			a.verboseLog("Executing tool: %s", tc.Name)
			toolStart := time.Now()

			toolResult, imgData, imgMIME, isError, err := a.registry.ExecuteWithImage(ctx, tc.Name, tc.Arguments)
			toolDuration := time.Since(toolStart)

			if err != nil {
				a.verboseLog("Tool %s error after %v: %v", tc.Name, toolDuration, err)
				toolResult = fmt.Sprintf("Error: %v", err)
				isError = true
			} else {
				a.verboseLog("Tool %s completed in %v (error=%v)", tc.Name, toolDuration, isError)
				a.verboseLog("Tool result preview: %s", truncate(toolResult, 200))
			}

			// Add tool result message
			msg := llm.Message{
				Role:       llm.RoleTool,
				Content:    toolResult,
				ToolCallID: tc.Name,
			}
			if len(imgData) > 0 {
				msg.ImageData = imgData
				msg.ImageMIME = imgMIME
			}
			messages = append(messages, msg)

			_ = isError
		}
	}

	// Max iterations reached
	a.verboseLog("Max iterations (%d) reached without completion", a.maxIterations)
	metrics.TotalDuration = time.Since(startTime)

	return &result.AnalysisResult{
		Status:  result.StatusFailure,
		Summary: "Behavior test incomplete - maximum iterations reached",
		RootCause: fmt.Sprintf("The agent reached the maximum number of iterations (%d) without completing the test. "+
			"This may indicate the test is too complex or has blocking issues.", a.maxIterations),
		Recommendations: []string{
			"Review the test steps for clarity",
			"Check if all prerequisites are met",
			"Consider increasing max_iterations in configuration",
		},
		AgentMetrics: metrics,
	}, nil
}

// buildBehaviorPrompt constructs the prompt for behavior testing.
func (a *Agent) buildBehaviorPrompt(req *BehaviorRequest) string {
	var sb strings.Builder

	sb.WriteString(`You are an expert browser testing agent. Your task is to execute a behavior test specification using browser tools.

IMPORTANT WORKFLOW:
1. First, use browser_navigate to go to the test URL
2. Use browser_snapshot to get the current page elements with their UIDs
3. Execute each test step using the appropriate browser tool (click, fill, etc.)
4. After each action, use browser_snapshot to verify the result
5. Continue until all steps are complete or a step fails

Available tools:
- browser_navigate: Navigate to URLs
- browser_snapshot: Get page elements with UIDs (ALWAYS use this before interacting with elements)
- browser_click: Click on elements
- browser_fill: Type text into inputs
- browser_wait_for: Wait for text to appear
- browser_press_key: Press keyboard keys
- browser_screenshot: Capture screenshots
- browser_list_console: Check for console errors
- browser_list_network: Check network requests

When you complete the test (success or failure), respond with your final result in this format:

## Status
SUCCESS or FAILURE

## Summary
[Brief description of what happened]

## Steps Executed
1. [Step 1 description and result]
2. [Step 2 description and result]
...

## Root Cause (if FAILURE)
[Explanation of why the test failed]

## Recommendations (if FAILURE)
1. [How to fix the issue]
...

---

`)

	sb.WriteString(fmt.Sprintf("Test File: %s\n\n", req.TestFile))

	if req.BaseURL != "" {
		sb.WriteString(fmt.Sprintf("Base URL: %s\n\n", req.BaseURL))
	}

	sb.WriteString("--- Test Specification ---\n")
	sb.WriteString(req.TestContent)
	sb.WriteString("\n---\n\n")

	sb.WriteString("Begin executing the test. Use browser_snapshot first to understand the current page state.")

	return sb.String()
}

// parseBehaviorResult parses the LLM response for behavior tests.
func (a *Agent) parseBehaviorResult(content string, req *BehaviorRequest) *result.AnalysisResult {
	analysisResult := &result.AnalysisResult{
		Status: result.StatusFailure,
	}

	// Parse sections from the response
	sections := parseSections(content)

	if summary, ok := sections["summary"]; ok {
		analysisResult.Summary = strings.TrimSpace(summary)
	}

	if rootCause, ok := sections["root cause"]; ok {
		analysisResult.RootCause = strings.TrimSpace(rootCause)
	}

	if recommendations, ok := sections["recommendations"]; ok {
		analysisResult.Recommendations = parseRecommendations(recommendations)
	}

	// Check status
	if status, ok := sections["status"]; ok {
		status = strings.ToUpper(strings.TrimSpace(status))
		if status == "SUCCESS" {
			analysisResult.Status = result.StatusSuccess
		}
	}

	// Fallback
	if analysisResult.Summary == "" {
		if analysisResult.Status == result.StatusSuccess {
			analysisResult.Summary = "Behavior test completed successfully"
		} else {
			analysisResult.Summary = "Behavior test failed"
			analysisResult.RootCause = content
		}
	}

	return analysisResult
}

// AnalyzeFailure runs the agent loop to analyze a command failure.
func (a *Agent) AnalyzeFailure(ctx context.Context, req *AnalysisRequest) (*result.AnalysisResult, error) {
	startTime := time.Now()
	a.verboseLog("Starting failure analysis for command: %s", req.Command)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build conversation
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: a.buildPrompt(req)},
	}

	tools := a.registry.Definitions()
	a.verboseLog("Registered %d tools for analysis", len(tools))

	// Track metrics
	metrics := &result.AgentMetrics{}

	// Agent loop
	for iteration := 0; iteration < a.maxIterations; iteration++ {
		metrics.IterationCount = iteration + 1
		a.verboseLog("=== Iteration %d/%d ===", iteration+1, a.maxIterations)

		// Check context
		if err := ctx.Err(); err != nil {
			a.verboseLog("Context error: %v", err)
			return nil, fmt.Errorf("agent timeout after %d iterations: %w", iteration, err)
		}

		// Call LLM
		metrics.LLMCalls++
		a.verboseLog("Calling LLM with %d messages, %d tools", len(messages), len(tools))

		llmStart := time.Now()
		resp, err := a.llmClient.Chat(ctx, messages, tools)
		llmDuration := time.Since(llmStart)

		if err != nil {
			a.verboseLog("LLM call failed after %v: %v", llmDuration, err)
			return nil, fmt.Errorf("LLM call failed at iteration %d: %w", iteration, err)
		}

		a.verboseLog("LLM response received in %v: tool_calls=%d, finish_reason=%s",
			llmDuration, len(resp.ToolCalls), resp.FinishReason)

		// Track token usage
		if resp.Usage != nil {
			metrics.TokensUsed += resp.Usage.TotalTokens
			a.verboseLog("Tokens: prompt=%d, completion=%d, total=%d",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
		}

		// If no tool calls, we have the final answer
		if !resp.HasToolCalls() {
			a.verboseLog("No tool calls - parsing final result")
			a.verboseLog("Response content preview: %s", truncate(resp.Content, 200))
			metrics.TotalDuration = time.Since(startTime)

			analysisResult := a.parseAnalysisResult(resp.Content, req)
			analysisResult.AgentMetrics = metrics
			a.verboseLog("Analysis completed with status: %s", analysisResult.Status)

			return analysisResult, nil
		}

		// Log tool calls
		for i, tc := range resp.ToolCalls {
			a.verboseLog("Tool call %d: %s(%s)", i+1, tc.Name, truncate(formatArgs(tc.Arguments), 100))
		}

		// Add assistant message with tool calls
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			ToolCalls: resp.ToolCalls,
		})

		// Execute tool calls
		for _, tc := range resp.ToolCalls {
			metrics.ToolCallsMade++
			a.verboseLog("Executing tool: %s", tc.Name)
			toolStart := time.Now()

			toolResult, imgData, imgMIME, isError, err := a.registry.ExecuteWithImage(ctx, tc.Name, tc.Arguments)
			toolDuration := time.Since(toolStart)

			if err != nil {
				a.verboseLog("Tool %s error after %v: %v", tc.Name, toolDuration, err)
				toolResult = fmt.Sprintf("Error: %v", err)
				isError = true
			} else {
				a.verboseLog("Tool %s completed in %v (error=%v)", tc.Name, toolDuration, isError)
				a.verboseLog("Tool result preview: %s", truncate(toolResult, 200))
			}

			// Add tool result message
			msg := llm.Message{
				Role:       llm.RoleTool,
				Content:    toolResult,
				ToolCallID: tc.Name, // Use tool name as ID for Gemini
			}
			if len(imgData) > 0 {
				msg.ImageData = imgData
				msg.ImageMIME = imgMIME
			}
			messages = append(messages, msg)

			_ = isError // Track if needed
		}
	}

	// Max iterations reached
	a.verboseLog("Max iterations (%d) reached without completion", a.maxIterations)
	metrics.TotalDuration = time.Since(startTime)

	return &result.AnalysisResult{
		Status:  result.StatusFailure,
		Summary: "Analysis incomplete - maximum iterations reached",
		RootCause: fmt.Sprintf("The agent reached the maximum number of iterations (%d) without reaching a conclusion. "+
			"This may indicate a complex issue that requires manual investigation.", a.maxIterations),
		Recommendations: []string{
			"Review the command output manually",
			"Check if the issue is environment-specific",
			"Consider increasing max_iterations in configuration",
		},
		CommandResult: &result.CommandResult{
			Command:    req.Command,
			WorkingDir: req.WorkingDir,
			ExitCode:   req.ExitCode,
			Duration:   req.Duration,
			Stdout:     req.Stdout,
			Stderr:     req.Stderr,
			TimedOut:   req.TimedOut,
		},
		AgentMetrics: metrics,
	}, nil
}

// buildPrompt constructs the system prompt and initial message.
func (a *Agent) buildPrompt(req *AnalysisRequest) string {
	var sb strings.Builder

	sb.WriteString(`You are an expert debugging assistant analyzing a command failure.

Your goal is to understand WHY the command failed and provide actionable recommendations.

You have access to these tools:
- execute_command: Run additional shell commands to diagnose the issue
- read_file: Read file contents for context
- search_code: Search for patterns in the codebase (grep-like)

Process:
1. Analyze the initial failure output carefully
2. Use tools to gather more information if needed (check files, search code, run diagnostic commands)
3. Identify the root cause
4. Provide clear, actionable recommendations

When you have enough information to explain the failure, respond with your final analysis in this EXACT format:

## Status
FAILURE

## Summary
[One sentence summary of what happened]

## Root Cause
[Detailed explanation of why the failure occurred]

## Recommendations
For each recommendation, you MUST include:
1. The exact file path and line number (e.g., auth_test.py:38)
2. The current problematic code snippet
3. The suggested fix as actual code
4. Brief explanation of why this fixes the issue

Example format:
1. Fix missing auth header in auth_test.py:38
   Current:
     req = client.get("/api/user")
   Change to:
     req = client.get("/api/user", headers={"Authorization": f"Bearer {token}"})
   This adds the required JWT token that middleware/auth.py:15 expects.

2. [Next recommendation with same format...]

IMPORTANT:
- Do NOT use tool calls in your final response - only include the formatted analysis above
- Be specific in your recommendations - include exact file:line references and code snippets
- If the failure is due to missing dependencies, list them explicitly with install commands

---

`)

	sb.WriteString("Command Failed:\n")
	sb.WriteString(fmt.Sprintf("- Command: %s\n", req.Command))
	sb.WriteString(fmt.Sprintf("- Working Directory: %s\n", req.WorkingDir))
	sb.WriteString(fmt.Sprintf("- Exit Code: %d\n", req.ExitCode))
	sb.WriteString(fmt.Sprintf("- Duration: %s\n", req.Duration))

	if req.TimedOut {
		sb.WriteString("- Status: TIMED OUT\n")
	}

	if req.Context != "" {
		sb.WriteString(fmt.Sprintf("\nUser Context: %s\n", req.Context))
	}

	sb.WriteString("\n--- Standard Output ---\n")
	if req.Stdout != "" {
		sb.WriteString(req.Stdout)
	} else {
		sb.WriteString("(empty)")
	}

	sb.WriteString("\n\n--- Standard Error ---\n")
	if req.Stderr != "" {
		sb.WriteString(req.Stderr)
	} else {
		sb.WriteString("(empty)")
	}

	sb.WriteString("\n\nPlease analyze this failure and help me understand what went wrong.")

	return sb.String()
}

// parseAnalysisResult parses the LLM response into a structured result.
func (a *Agent) parseAnalysisResult(content string, req *AnalysisRequest) *result.AnalysisResult {
	analysisResult := &result.AnalysisResult{
		Status: result.StatusFailure,
		CommandResult: &result.CommandResult{
			Command:    req.Command,
			WorkingDir: req.WorkingDir,
			ExitCode:   req.ExitCode,
			Duration:   req.Duration,
			Stdout:     req.Stdout,
			Stderr:     req.Stderr,
			TimedOut:   req.TimedOut,
		},
	}

	// Parse sections from the response
	sections := parseSections(content)

	if summary, ok := sections["summary"]; ok {
		analysisResult.Summary = strings.TrimSpace(summary)
	} else {
		// Try to extract first meaningful line as summary
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				analysisResult.Summary = line
				break
			}
		}
	}

	if rootCause, ok := sections["root cause"]; ok {
		analysisResult.RootCause = strings.TrimSpace(rootCause)
	} else if rootCause, ok := sections["root_cause"]; ok {
		analysisResult.RootCause = strings.TrimSpace(rootCause)
	}

	if recommendations, ok := sections["recommendations"]; ok {
		analysisResult.Recommendations = parseRecommendations(recommendations)
	}

	// If status section says SUCCESS, update status
	if status, ok := sections["status"]; ok {
		status = strings.ToUpper(strings.TrimSpace(status))
		if status == "SUCCESS" {
			analysisResult.Status = result.StatusSuccess
		}
	}

	// Fallback if no structured content found
	if analysisResult.Summary == "" && analysisResult.RootCause == "" {
		analysisResult.Summary = "Analysis completed"
		analysisResult.RootCause = content
	}

	return analysisResult
}

// parseSections extracts markdown sections from content.
func parseSections(content string) map[string]string {
	sections := make(map[string]string)

	// Split by ## headers
	lines := strings.Split(content, "\n")
	var currentSection string
	var currentContent strings.Builder

	headerPattern := regexp.MustCompile(`^##\s*(.+?)\s*$`)

	for _, line := range lines {
		if match := headerPattern.FindStringSubmatch(line); match != nil {
			// Save previous section if exists
			if currentSection != "" {
				sections[currentSection] = strings.TrimSpace(currentContent.String())
			}
			// Start new section
			currentSection = strings.ToLower(strings.TrimSpace(match[1]))
			currentContent.Reset()
		} else if currentSection != "" {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}

	// Save last section
	if currentSection != "" {
		sections[currentSection] = strings.TrimSpace(currentContent.String())
	}

	return sections
}

// parseRecommendations extracts numbered or bulleted recommendations.
func parseRecommendations(content string) []string {
	var recommendations []string

	// Match numbered items (1. xxx) or bulleted items (- xxx, * xxx)
	itemPattern := regexp.MustCompile(`(?m)^(?:\d+\.|[-*])\s*(.+)$`)
	matches := itemPattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) >= 2 {
			rec := strings.TrimSpace(match[1])
			if rec != "" {
				recommendations = append(recommendations, rec)
			}
		}
	}

	// If no structured items found, split by newlines
	if len(recommendations) == 0 {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				recommendations = append(recommendations, line)
			}
		}
	}

	return recommendations
}
