package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// Registry manages a collection of tools.
type Registry interface {
	// Register adds a tool to the registry.
	Register(tool Tool)

	// Get retrieves a tool by name.
	Get(name string) (Tool, bool)

	// All returns all registered tools.
	All() []Tool

	// Definitions returns LLM tool definitions for all registered tools.
	Definitions() []llm.Tool

	// Execute executes a tool by name with the given arguments.
	Execute(ctx context.Context, name string, args map[string]any) (string, bool, error)

	// ExecuteWithImage executes a tool and returns image data if the tool supports it.
	ExecuteWithImage(ctx context.Context, name string, args map[string]any) (string, []byte, string, bool, error)
}

// toolRegistry is the default implementation of Registry.
type toolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	order []string // Maintains registration order
}

// NewRegistry creates a new tool registry.
func NewRegistry() Registry {
	return &toolRegistry{
		tools: make(map[string]Tool),
		order: make([]string, 0),
	}
}

// Register adds a tool to the registry.
func (r *toolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = tool
}

// Get retrieves a tool by name.
func (r *toolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[name]
	return tool, ok
}

// All returns all registered tools in registration order.
func (r *toolRegistry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]Tool, len(r.order))
	for i, name := range r.order {
		tools[i] = r.tools[name]
	}
	return tools
}

// Definitions returns LLM tool definitions for all registered tools.
func (r *toolRegistry) Definitions() []llm.Tool {
	return ToLLMTools(r.All())
}

// Execute executes a tool by name with the given arguments.
func (r *toolRegistry) Execute(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	tool, ok := r.Get(name)
	if !ok {
		return "", true, fmt.Errorf("unknown tool: %s", name)
	}

	result, isError := tool.Execute(ctx, args)
	return result, isError, nil
}

// ExecuteWithImage executes a tool and returns image data if the tool implements ImageResultTool.
func (r *toolRegistry) ExecuteWithImage(ctx context.Context, name string, args map[string]any) (string, []byte, string, bool, error) {
	tool, ok := r.Get(name)
	if !ok {
		return "", nil, "", true, fmt.Errorf("unknown tool: %s", name)
	}

	if imgTool, ok := tool.(ImageResultTool); ok {
		text, imgData, mime, isError := imgTool.ExecuteWithImage(ctx, args)
		return text, imgData, mime, isError, nil
	}

	result, isError := tool.Execute(ctx, args)
	return result, nil, "", isError, nil
}

// DefaultRegistry returns a registry with all default tools registered.
func DefaultRegistry(workingDir string, executor ShellExecutor) Registry {
	registry := NewRegistry()

	// Register default tools
	registry.Register(NewShellTool(executor, workingDir))
	registry.Register(NewReadFileTool(workingDir))
	registry.Register(NewSearchCodeTool(workingDir))

	return registry
}

// ShellExecutor is the interface required by the shell tool.
type ShellExecutor interface {
	Execute(ctx context.Context, command, cwd string) (stdout, stderr string, exitCode int, err error)
}
