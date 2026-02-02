package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// AskConfig holds configuration for the ask sub-agent.
type AskConfig struct {
	// LLMClient is the LLM client to use.
	LLMClient llm.Client
	// Browser is the browser instance to inspect.
	Browser *browser.Browser
	// MaxIterations is the maximum number of agent loop iterations (default 5).
	MaxIterations int
	// Timeout is the maximum time for the ask operation (default 60s).
	Timeout time.Duration
	// Verbose enables debug logging.
	Verbose bool
}

// NewAskAgent creates an agent configured with only the 4 ask-specific tools.
func NewAskAgent(cfg AskConfig) *Agent {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 5
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	registry := NewRegistry()
	for _, tool := range NewAskTools(cfg.Browser) {
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

// Ask runs the ask sub-agent loop with the given question and returns a concise text answer.
func (a *Agent) Ask(ctx context.Context, question string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	systemPrompt := `You are a concise page inspector. You answer questions about the current browser page.

Rules:
- Use the provided tools to inspect the page as needed
- Return a plain text answer — no markdown, no formatting
- Be concise and direct
- If you cannot find the answer, say so clearly`

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: question},
	}

	tools := a.registry.Definitions()

	for iteration := 0; iteration < a.maxIterations; iteration++ {
		a.verboseLog("Ask iteration %d/%d", iteration+1, a.maxIterations)

		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("ask timeout after %d iterations: %w", iteration, err)
		}

		resp, err := a.llmClient.Chat(ctx, messages, tools)
		if err != nil {
			return "", fmt.Errorf("LLM call failed at iteration %d: %w", iteration, err)
		}

		if !resp.HasToolCalls() {
			a.verboseLog("Ask completed with answer: %s", truncate(resp.Content, 200))
			return resp.Content, nil
		}

		// Log tool calls
		for i, tc := range resp.ToolCalls {
			a.verboseLog("Ask tool call %d: %s", i+1, tc.Name)
		}

		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			toolResult, imgData, imgMIME, _, execErr := a.registry.ExecuteWithImage(ctx, tc.Name, tc.Arguments)
			if execErr != nil {
				toolResult = fmt.Sprintf("Error: %v", execErr)
			}

			a.verboseLog("Ask tool %s result length: %d", tc.Name, len(toolResult))

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
		}
	}

	return "", fmt.Errorf("ask agent reached maximum iterations (%d) without answering", a.maxIterations)
}

