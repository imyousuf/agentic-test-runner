// Package agent provides the AI agent implementation for analyzing command failures.
package agent

import (
	"context"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// Tool defines an interface for tools that can be called by the LLM.
type Tool interface {
	// Name returns the unique identifier for this tool.
	Name() string

	// Description returns a description of what this tool does.
	Description() string

	// Parameters returns a JSON Schema defining the tool's parameters.
	Parameters() map[string]any

	// Execute runs the tool with the given arguments.
	// Returns the result as a string and whether an error occurred.
	Execute(ctx context.Context, args map[string]any) (string, bool)
}

// ToLLMTool converts a Tool to an llm.Tool for use with LLM clients.
func ToLLMTool(t Tool) llm.Tool {
	return llm.Tool{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Parameters(),
	}
}

// ToLLMTools converts a slice of Tools to llm.Tools.
func ToLLMTools(tools []Tool) []llm.Tool {
	result := make([]llm.Tool, len(tools))
	for i, t := range tools {
		result[i] = ToLLMTool(t)
	}
	return result
}
