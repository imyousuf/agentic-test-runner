package llm

import (
	"encoding/base64"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// Converting ATR's conversation shape to Anthropic's is not a field rename.
// Two differences matter:
//
//  1. Anthropic requires each result to name the id of the tool_use block that
//     asked for it. ATR carries that in ToolCallID, so the pairing is a
//     lookup — but a history can be trimmed, or come from a provider that
//     issued no ids at all, so position is kept as the fallback: results
//     arrive in the order the calls were made.
//
//  2. ATR appends one RoleTool message per result. Anthropic requires every
//     result for an assistant turn to arrive in a single user message, in the
//     order the calls were made.
//
// Both are handled by walking the history a turn at a time: each assistant
// message carries the tool calls, and the RoleTool messages that follow are
// its results.

// systemPrompt collects the leading system text. Anthropic carries the system
// prompt outside the message list, so it is extracted rather than converted.
func systemPrompt(messages []llm.Message, configured string) string {
	if configured != "" {
		return configured
	}
	for _, msg := range messages {
		if msg.Role == llm.RoleSystem {
			return msg.Content
		}
	}
	return ""
}

// toAnthropicMessages converts ATR history into Anthropic message params.
func toAnthropicMessages(messages []llm.Message) ([]anthropic.MessageParam, error) {
	var out []anthropic.MessageParam

	// pending maps a tool name to the ids of calls from the assistant turn
	// currently being answered, in the order they were made.
	var pending []llm.ToolCall
	var results []anthropic.ContentBlockParamUnion

	flushResults := func() {
		if len(results) == 0 {
			return
		}
		out = append(out, anthropic.MessageParam{
			Role:    anthropic.MessageParamRoleUser,
			Content: results,
		})
		results = nil
		pending = nil
	}

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleSystem:
			// Carried outside the message list; see systemPrompt.
			continue

		case llm.RoleTool:
			// A result for the assistant turn immediately above. Prefer the
			// id it names; fall back to the next unconsumed call, which is
			// the one it answers, when the id is missing or unknown.
			id, ok := takeCall(&pending, msg.ToolCallID)
			if !ok {
				// No call to pair with: the history was trimmed between the
				// call and its result. Anthropic rejects an unmatched
				// tool_result, so the content is kept as plain user text
				// rather than dropped.
				results = append(results, anthropic.NewTextBlock(
					fmt.Sprintf("Result of %s: %s", msg.ToolCallID, msg.Content)))
				continue
			}
			results = append(results, anthropic.NewToolResultBlock(id, msg.Content, false))

		case llm.RoleAssistant:
			flushResults()

			blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(msg.ToolCalls))
			if msg.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}
			for _, call := range msg.ToolCalls {
				id := call.ID
				if id == "" {
					id = call.Name
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(id, call.Arguments, call.Name))
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleAssistant,
				Content: blocks,
			})
			pending = msg.ToolCalls

		default: // user
			flushResults()

			blocks := make([]anthropic.ContentBlockParamUnion, 0, 2)
			// Screenshots ride along as image blocks; the computer-use and HUD
			// agents depend on them.
			if len(msg.ImageData) > 0 {
				mime := msg.ImageMIME
				if mime == "" {
					mime = "image/png"
				}
				blocks = append(blocks, anthropic.NewImageBlockBase64(mime, base64.StdEncoding.EncodeToString(msg.ImageData)))
			}
			if msg.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: blocks,
			})
		}
	}

	flushResults()

	if len(out) == 0 {
		return nil, fmt.Errorf("no messages to send")
	}
	return out, nil
}

// toAnthropicTools converts ATR tool definitions to Anthropic tool params.
func toAnthropicTools(tools []llm.Tool) []anthropic.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		schema := anthropic.ToolInputSchemaParam{}
		if props, ok := tool.Parameters["properties"].(map[string]any); ok {
			schema.Properties = props
		}
		if req, ok := tool.Parameters["required"].([]string); ok {
			schema.Required = req
		} else if raw, ok := tool.Parameters["required"].([]any); ok {
			required := make([]string, 0, len(raw))
			for _, r := range raw {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
			schema.Required = required
		}

		param := anthropic.ToolUnionParamOfTool(schema, tool.Name)
		if param.OfTool != nil && tool.Description != "" {
			param.OfTool.Description = anthropic.String(tool.Description)
		}
		out = append(out, param)
	}
	return out
}

// takeCall consumes the call a result answers and returns the id to send back.
//
// The id the result names wins, because that is the only thing that survives
// reordering. When it names nothing recognisable — an older history recorded
// against the tool name, a provider that issued no ids, a call already
// consumed — the next unconsumed call is used instead, which is correct
// whenever results arrive in the order the calls were made.
func takeCall(pending *[]llm.ToolCall, id string) (string, bool) {
	if id != "" {
		for i, call := range *pending {
			if call.ID == id {
				*pending = append((*pending)[:i:i], (*pending)[i+1:]...)
				return call.ID, true
			}
		}
	}
	if len(*pending) == 0 {
		return "", false
	}
	next := (*pending)[0]
	*pending = (*pending)[1:]
	if next.ID != "" {
		return next.ID, true
	}
	return next.Name, true
}
