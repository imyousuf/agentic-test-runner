package llm

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// blockTypes reports the block kinds in a message, so a test can assert the
// shape without reaching into the SDK's union internals everywhere.
func blockTypes(m anthropic.MessageParam) []string {
	var out []string
	for _, b := range m.Content {
		switch {
		case b.OfText != nil:
			out = append(out, "text")
		case b.OfToolUse != nil:
			out = append(out, "tool_use")
		case b.OfToolResult != nil:
			out = append(out, "tool_result")
		case b.OfImage != nil:
			out = append(out, "image")
		default:
			out = append(out, "other")
		}
	}
	return out
}

// ATR records a tool result against the tool's NAME, because that is what
// Gemini matches on. Anthropic needs the id of the tool_use block that asked
// for it, so the conversion has to recover it — getting this wrong makes every
// tool-using request fail at the API.
func TestToolResultCarriesTheToolUseID(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "do it"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "toolu_abc123", Name: "browser_snapshot", Arguments: map[string]any{}},
		}},
		{Role: llm.RoleTool, ToolCallID: "browser_snapshot", Content: "the page"},
	}

	out, err := toAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d messages, want 3: %v", len(out), out)
	}

	result := out[2].Content[0].OfToolResult
	if result == nil {
		t.Fatal("the tool result did not become a tool_result block")
	}
	if result.ToolUseID != "toolu_abc123" {
		t.Errorf("ToolUseID = %q, want the id from the tool_use block, not the tool name", result.ToolUseID)
	}
}

// The same tool called twice in one turn is what name matching cannot resolve.
// Position can, because the agent loop appends results in call order.
func TestParallelCallsToTheSameToolPairByPosition(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "check both"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "toolu_first", Name: "read_file", Arguments: map[string]any{"path": "a"}},
			{ID: "toolu_second", Name: "read_file", Arguments: map[string]any{"path": "b"}},
		}},
		{Role: llm.RoleTool, ToolCallID: "read_file", Content: "contents of a"},
		{Role: llm.RoleTool, ToolCallID: "read_file", Content: "contents of b"},
	}

	out, err := toAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	// Anthropic requires both results in ONE user message.
	if len(out) != 3 {
		t.Fatalf("got %d messages, want 3 (results must be grouped): %v", len(out), blockTypesAll(out))
	}
	results := out[2].Content
	if len(results) != 2 {
		t.Fatalf("got %d result blocks in one message, want 2", len(results))
	}
	if got := results[0].OfToolResult.ToolUseID; got != "toolu_first" {
		t.Errorf("first result -> %q, want toolu_first", got)
	}
	if got := results[1].OfToolResult.ToolUseID; got != "toolu_second" {
		t.Errorf("second result -> %q, want toolu_second", got)
	}
}

func blockTypesAll(msgs []anthropic.MessageParam) [][]string {
	out := make([][]string, len(msgs))
	for i, m := range msgs {
		out[i] = blockTypes(m)
	}
	return out
}

// A trimmed history can leave a result with no surviving call. Anthropic
// rejects an unmatched tool_result outright, so it must not be emitted as one.
func TestOrphanedToolResultDoesNotBecomeAToolResultBlock(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleTool, ToolCallID: "browser_snapshot", Content: "orphan"},
		{Role: llm.RoleUser, Content: "carry on"},
	}

	out, err := toAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	for _, m := range out {
		for _, b := range m.Content {
			if b.OfToolResult != nil {
				t.Fatal("an orphaned result became a tool_result block; the API would reject the request")
			}
		}
	}
}

// Screenshots are how the computer-use and HUD agents see anything.
func TestImagesSurviveConversion(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "what is on screen?", ImageData: []byte{0x89, 0x50}, ImageMIME: "image/png"},
	}

	out, err := toAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	types := blockTypes(out[0])
	if len(types) != 2 || types[0] != "image" {
		t.Errorf("blocks = %v, want the image first then the text", types)
	}
}

// The system prompt travels outside the message list.
func TestSystemMessageIsLiftedOutOfTheConversation(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "you are a test agent"},
		{Role: llm.RoleUser, Content: "hello"},
	}

	if got := systemPrompt(messages, ""); got != "you are a test agent" {
		t.Errorf("systemPrompt = %q", got)
	}

	out, err := toAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d messages, want 1 — the system message must not be sent inline", len(out))
	}
}

// An explicitly configured prompt wins over one found in the history.
func TestConfiguredSystemPromptTakesPrecedence(t *testing.T) {
	messages := []llm.Message{{Role: llm.RoleSystem, Content: "from history"}}
	if got := systemPrompt(messages, "from config"); got != "from config" {
		t.Errorf("systemPrompt = %q, want the configured one", got)
	}
}

func TestToolSchemaConversion(t *testing.T) {
	tools := []llm.Tool{{
		Name:        "browser_click",
		Description: "Click an element",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"target": map[string]any{"type": "string"}},
			"required":   []any{"target"},
		},
	}}

	out := toAnthropicTools(tools)
	if len(out) != 1 || out[0].OfTool == nil {
		t.Fatal("tool did not convert")
	}
	if out[0].OfTool.Name != "browser_click" {
		t.Errorf("Name = %q", out[0].OfTool.Name)
	}
	if len(out[0].OfTool.InputSchema.Required) != 1 || out[0].OfTool.InputSchema.Required[0] != "target" {
		t.Errorf("Required = %v, want [target]", out[0].OfTool.InputSchema.Required)
	}
}

// Assistant text and its tool calls belong to one message.
func TestAssistantTextAndToolCallsShareAMessage(t *testing.T) {
	args := map[string]any{"target": "#go"}
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "click it"},
		{Role: llm.RoleAssistant, Content: "Clicking now.", ToolCalls: []llm.ToolCall{
			{ID: "toolu_1", Name: "browser_click", Arguments: args},
		}},
	}

	out, err := toAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got := blockTypes(out[1]); len(got) != 2 || got[0] != "text" || got[1] != "tool_use" {
		t.Fatalf("blocks = %v, want [text tool_use]", got)
	}

	// The arguments must survive as an object, not as a string.
	raw, err := json.Marshal(out[1].Content[1].OfToolUse.Input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("tool input is not a JSON object: %s", raw)
	}
	if back["target"] != "#go" {
		t.Errorf("input = %s, want target=#go", raw)
	}
}

func TestEmptyHistoryIsAnError(t *testing.T) {
	if _, err := toAnthropicMessages(nil); err == nil {
		t.Error("expected an error for an empty conversation")
	}
}

// With real ids the pairing no longer depends on results arriving in call
// order — which is the whole point of carrying them.
func TestResultsPairByIDEvenOutOfOrder(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "check both"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "toolu_first", Name: "read_file", Arguments: map[string]any{"path": "a"}},
			{ID: "toolu_second", Name: "read_file", Arguments: map[string]any{"path": "b"}},
		}},
		// The slower tool answered first.
		{Role: llm.RoleTool, ToolCallID: "toolu_second", ToolName: "read_file", Content: "contents of b"},
		{Role: llm.RoleTool, ToolCallID: "toolu_first", ToolName: "read_file", Content: "contents of a"},
	}

	out, err := toAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	results := out[2].Content
	if len(results) != 2 {
		t.Fatalf("got %d result blocks, want 2", len(results))
	}
	if got := results[0].OfToolResult.ToolUseID; got != "toolu_second" {
		t.Errorf("first result -> %q, want toolu_second: the id must win over position", got)
	}
	if got := results[1].OfToolResult.ToolUseID; got != "toolu_first" {
		t.Errorf("second result -> %q, want toolu_first", got)
	}
}

// A result naming an id that no call in this turn issued still has to land
// somewhere valid — Anthropic rejects an unmatched tool_result outright.
func TestResultWithAnUnknownIDFallsBackToPosition(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "check it"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "toolu_real", Name: "read_file", Arguments: map[string]any{}},
		}},
		{Role: llm.RoleTool, ToolCallID: "toolu_from_a_trimmed_turn", ToolName: "read_file", Content: "contents"},
	}

	out, err := toAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	result := out[2].Content[0].OfToolResult
	if result == nil {
		t.Fatal("the result did not become a tool_result block")
	}
	if result.ToolUseID != "toolu_real" {
		t.Errorf("ToolUseID = %q, want the call actually made this turn", result.ToolUseID)
	}
}
