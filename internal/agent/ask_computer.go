package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/computer"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// ComputerAskConfig holds configuration for the computer ask sub-agent.
type ComputerAskConfig struct {
	// LLMClient is the LLM client to use.
	LLMClient llm.Client
	// Computer is the desktop controller.
	Computer *computer.Computer
	// MaxIterations is the maximum number of agent loop iterations (default 20).
	MaxIterations int
	// Timeout is the maximum time for the ask operation (default 5 minutes).
	Timeout time.Duration
	// Verbose enables debug logging.
	Verbose bool
}

// NewComputerAskAgent builds an Agent wired with the curated computer-ask
// tool subset (screenshot + windows + click/type/key/chord/focus/launch).
func NewComputerAskAgent(cfg ComputerAskConfig) *Agent {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}

	registry := NewRegistry()
	for _, tool := range NewComputerAskTools(cfg.Computer) {
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

const computerAskSystemPrompt = `You drive a desktop on behalf of the user. Goal: accomplish the user's instruction by inspecting screenshots and calling tools.

Coordinate model:
- Mouse and window operations use ROOT coordinates by default — the bounding box of all monitors with origin (0, 0).
- Every mouse tool (click, type-via-focus, etc.) accepts an optional "display" parameter. When set, x/y are interpreted as pixels relative to that display's top-left. PREFER --display + display-local pixels because it is easier to read off a screenshot.
- Call computer_displays first to learn each monitor's bounds and pick the right display index.

Workflow:
1. Take a screenshot to see the current state.
2. Decide on a single next action.
3. Execute that one action.
4. Take another screenshot to verify the expected change happened.
5. Repeat until the goal is achieved or you hit a blocker.

Stopping rules:
- When the goal is achieved, return a 1–2 sentence summary as plain text (no tool call).
- If a system password / sudo / authentication prompt appears, STOP. You cannot type passwords. Return: "Blocked: <app> is requesting authentication; please complete it manually."
- If you cannot make progress after 3 consecutive screenshots that look identical, STOP and report what you tried.

Output:
- Final answer is plain text, 1–2 sentences. No markdown, no formatting.
- Be concrete: name the windows you interacted with, the buttons you clicked.`

// computerAskRecentImageWindow is the number of most-recent tool messages
// whose image bytes are kept in the LLM context. Older tool messages get
// their ImageData/ImageMIME cleared (text content is preserved). Without
// this, every screenshot stays in the history forever — at ~2 MB each over
// 20 iterations, we'd be re-sending tens of megabytes per LLM call.
const computerAskRecentImageWindow = 2

// ComputerAsk runs the computer ask sub-agent loop with the given instruction.
// It mirrors (*Agent).Ask but with the computer-specific system prompt and
// applies a sliding window over screenshot bytes to keep request payloads
// bounded.
func (a *Agent) ComputerAsk(ctx context.Context, instruction string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: computerAskSystemPrompt},
		{Role: llm.RoleUser, Content: instruction},
	}
	tools := a.registry.Definitions()

	for iteration := 0; iteration < a.maxIterations; iteration++ {
		a.verboseLog("ComputerAsk iteration %d/%d", iteration+1, a.maxIterations)

		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("computer ask timeout after %d iterations: %w", iteration, err)
		}

		resp, err := a.llmClient.Chat(ctx, messages, tools)
		if err != nil {
			return "", fmt.Errorf("LLM call failed at iteration %d: %w", iteration, err)
		}

		if !resp.HasToolCalls() {
			a.verboseLog("ComputerAsk completed with answer: %s", truncate(resp.Content, 200))
			return resp.Content, nil
		}

		for i, tc := range resp.ToolCalls {
			a.verboseLog("ComputerAsk tool call %d: %s", i+1, tc.Name)
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
			a.verboseLog("ComputerAsk tool %s result length: %d", tc.Name, len(toolResult))

			msg := llm.Message{
				Role:       llm.RoleTool,
				Content:    toolResult,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			}
			if len(imgData) > 0 {
				msg.ImageData = imgData
				msg.ImageMIME = imgMIME
			}
			messages = append(messages, msg)
		}

		messages = trimImageHistory(messages, computerAskRecentImageWindow)
	}

	return "", fmt.Errorf("computer ask agent reached maximum iterations (%d) without completing", a.maxIterations)
}

// trimImageHistory keeps only the keepLast most recent tool messages with
// image data; older tool messages have ImageData/ImageMIME cleared (text
// Content is preserved so the LLM still sees what the action returned).
func trimImageHistory(messages []llm.Message, keepLast int) []llm.Message {
	imageIdxs := []int{}
	for i, m := range messages {
		if m.Role == llm.RoleTool && len(m.ImageData) > 0 {
			imageIdxs = append(imageIdxs, i)
		}
	}
	if len(imageIdxs) <= keepLast {
		return messages
	}
	for _, idx := range imageIdxs[:len(imageIdxs)-keepLast] {
		messages[idx].ImageData = nil
		messages[idx].ImageMIME = ""
	}
	return messages
}
