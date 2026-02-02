// Package llm provides a unified interface for interacting with Large Language Models.
// This package is designed to be reusable by other Go projects.
package llm

import "encoding/json"

// Role represents the role of a message sender in a conversation.
type Role string

const (
	// RoleUser represents a message from the user.
	RoleUser Role = "user"
	// RoleAssistant represents a message from the assistant/model.
	RoleAssistant Role = "assistant"
	// RoleTool represents a tool/function response.
	RoleTool Role = "tool"
	// RoleSystem represents a system message.
	RoleSystem Role = "system"
)

// Message represents a single message in a conversation.
type Message struct {
	// Role indicates who sent this message.
	Role Role `json:"role"`
	// Content is the text content of the message.
	Content string `json:"content"`
	// ToolCallID is set when this message is a tool response.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolCalls contains any tool calls made by the assistant.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ImageData holds optional inline image bytes for multimodal messages (e.g., screenshots).
	ImageData []byte `json:"image_data,omitempty"`
	// ImageMIME is the MIME type for ImageData (e.g., "image/png").
	ImageMIME string `json:"image_mime,omitempty"`
}

// ToolCall represents a function/tool call made by the LLM.
type ToolCall struct {
	// ID is a unique identifier for this tool call.
	ID string `json:"id"`
	// Name is the name of the tool/function to call.
	Name string `json:"name"`
	// Arguments contains the arguments to pass to the tool as a map.
	Arguments map[string]any `json:"arguments"`
	// ThoughtSignature is an encrypted representation of the model's reasoning (Gemini 3+).
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	// CallID is the ID of the tool call this result corresponds to.
	CallID string `json:"call_id"`
	// Name is the name of the tool that was called.
	Name string `json:"name"`
	// Content is the result content (usually a string).
	Content string `json:"content"`
	// IsError indicates if the tool execution resulted in an error.
	IsError bool `json:"is_error"`
}

// Tool defines a tool/function that can be called by the LLM.
type Tool struct {
	// Name is the unique identifier for this tool.
	Name string `json:"name"`
	// Description explains what this tool does.
	Description string `json:"description"`
	// Parameters is a JSON Schema defining the tool's parameters.
	Parameters map[string]any `json:"parameters"`
}

// Response represents a response from the LLM.
type Response struct {
	// Content is the text content of the response (if any).
	Content string `json:"content,omitempty"`
	// ToolCalls contains any tool calls the LLM wants to make.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// FinishReason indicates why the response ended.
	FinishReason string `json:"finish_reason,omitempty"`
	// Usage contains token usage information.
	Usage *Usage `json:"usage,omitempty"`
}

// Usage contains token usage information for a request.
type Usage struct {
	// PromptTokens is the number of tokens in the prompt.
	PromptTokens int `json:"prompt_tokens"`
	// CompletionTokens is the number of tokens in the completion.
	CompletionTokens int `json:"completion_tokens"`
	// TotalTokens is the total number of tokens used.
	TotalTokens int `json:"total_tokens"`
}

// HasToolCalls returns true if the response contains tool calls.
func (r *Response) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// MarshalArguments marshals tool call arguments to JSON.
func (tc *ToolCall) MarshalArguments() ([]byte, error) {
	return json.Marshal(tc.Arguments)
}

// UnmarshalArguments unmarshals JSON arguments into the tool call.
func (tc *ToolCall) UnmarshalArguments(data []byte) error {
	return json.Unmarshal(data, &tc.Arguments)
}
