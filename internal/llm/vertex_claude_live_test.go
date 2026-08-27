package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// These tests talk to Vertex AI and cost money, so they only run when asked:
//
//	ATR_LIVE_VERTEX=1 GOOGLE_CLOUD_PROJECT=<project> go test ./internal/llm/ -run Live -v
//
// They need Application Default Credentials (gcloud auth application-default
// login) and a project with the Anthropic publisher models enabled.
func liveConfig(t *testing.T) llm.Config {
	t.Helper()
	if os.Getenv("ATR_LIVE_VERTEX") == "" {
		t.Skip("set ATR_LIVE_VERTEX=1 to run tests that call Vertex AI")
	}
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		t.Skip("set GOOGLE_CLOUD_PROJECT to run tests that call Vertex AI")
	}
	model := os.Getenv("ATR_LIVE_MODEL")
	if model == "" {
		model = "claude-sonnet-5"
	}
	location := os.Getenv("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = "global"
	}
	return llm.Config{
		Provider:    llm.ProviderVertexClaude,
		Model:       model,
		Project:     project,
		Location:    location,
		Temperature: 0,
		MaxTokens:   256,
	}
}

// bulkyTools stands in for ATR's real tool set, which is what the cache
// checkpoint is there to cover. Caching has a minimum prompt size, so a
// two-tool request would never be cached at all and would prove nothing.
func bulkyTools(n int) []llm.Tool {
	filler := strings.Repeat(
		"The selector identifies the element to act on. It may be a CSS selector, "+
			"an XPath expression, or the visible text of the element. ", 12)
	tools := make([]llm.Tool, 0, n)
	for i := 0; i < n; i++ {
		tools = append(tools, llm.Tool{
			Name:        "atr_probe_" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Description: filler,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{"type": "string", "description": filler},
					"timeout":  map[string]any{"type": "integer", "description": "Milliseconds to wait."},
				},
				"required": []any{"selector"},
			},
		})
	}
	return tools
}

// The point of the provider: the fixed prefix is paid for once, then read from
// cache on every later iteration of the agent loop.
func TestLiveCacheIsReadOnTheSecondCall(t *testing.T) {
	cfg := liveConfig(t)
	cfg.SystemPrompt = strings.Repeat(
		"You are ATR, a browser automation agent. Answer with a single word. ", 40)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := llm.NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	vc, ok := client.(*vertexClaudeClient)
	if !ok {
		t.Fatalf("registry returned %T, want *vertexClaudeClient", client)
	}
	tools := bulkyTools(24)

	call := func(question string) *anthropic.Message {
		t.Helper()
		msg, err := vc.sendRaw(ctx, []llm.Message{{Role: llm.RoleUser, Content: question}}, tools)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		t.Logf("%-14s input=%d cache_write=%d cache_read=%d output=%d", question,
			msg.Usage.InputTokens, msg.Usage.CacheCreationInputTokens,
			msg.Usage.CacheReadInputTokens, msg.Usage.OutputTokens)
		return msg
	}

	// Prime the cache. On a cold entry this is the call that pays for the
	// prefix; if an earlier run left one warm, it reads instead. Either way
	// the checkpoint has to be doing something.
	first := call("Say ready.")
	if first.Usage.CacheCreationInputTokens == 0 && first.Usage.CacheReadInputTokens == 0 {
		t.Fatalf("nothing was cached; the checkpoint is not being applied")
	}

	// A freshly written entry is not readable the instant the response
	// returns — a request sent a second or two later can still miss and
	// rewrite it. Retry a few times before calling it a failure, so a slow
	// commit does not read as a broken checkpoint.
	var second *anthropic.Message
	for attempt := 0; attempt < 4; attempt++ {
		time.Sleep(5 * time.Second)
		// A different question every time: the part after the checkpoint is
		// supposed to change, which is what the agent loop does each turn.
		second = call(fmt.Sprintf("Say go %d.", attempt))
		if second.Usage.CacheReadInputTokens > 0 {
			break
		}
	}

	if second.Usage.CacheReadInputTokens == 0 {
		t.Fatalf("the prefix was never read back from cache; it is being re-sent every iteration")
	}
	// The uncached remainder should be the question alone, not the tools.
	if second.Usage.InputTokens > 100 {
		t.Errorf("billed %d uncached input tokens; the tool schemas are not inside the checkpoint",
			second.Usage.InputTokens)
	}
}

// A full round trip through the agent-loop shape: ask for a tool call, hand
// back a result, get a final answer.
func TestLiveToolCallRoundTrip(t *testing.T) {
	cfg := liveConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := llm.NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	tools := []llm.Tool{{
		Name:        "get_page_title",
		Description: "Returns the title of the page currently open in the browser.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}

	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "You are a browser agent. Use the tools available to you."},
		{Role: llm.RoleUser, Content: "What is the title of the page that is open? Use the tool."},
	}

	resp, err := client.Chat(ctx, history, tools)
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if len(resp.ToolCalls) == 0 {
		t.Fatalf("expected a tool call, got content %q", resp.Content)
	}
	call := resp.ToolCalls[0]
	if call.Name != "get_page_title" {
		t.Fatalf("called %q", call.Name)
	}
	if call.ID == "" {
		t.Error("tool call has no id; the tool result cannot be correlated")
	}

	// Feed the result back the way the agent loop does.
	history = append(history,
		llm.Message{Role: llm.RoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls},
		llm.Message{Role: llm.RoleTool, ToolCallID: call.Name, Content: "Example Domain"},
	)

	final, err := client.ChatWithHistory(ctx, history, tools)
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if !strings.Contains(strings.ToLower(final.Content), "example domain") {
		t.Errorf("final answer did not use the tool result: %q", final.Content)
	}
	t.Logf("final: %s", final.Content)
}
