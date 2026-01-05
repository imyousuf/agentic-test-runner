# Architecture

This document describes the internal architecture of ATR.

## Overview

ATR (Agentic Test Runner) is built around an **AI agent loop** that uses tools to investigate and analyze failures. The same agent architecture powers both command failure analysis and browser behavior testing.

```
┌─────────────────────────────────────────────────────────────────┐
│                           ATR CLI                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │ run --cmd   │  │ run         │  │ config                  │  │
│  │             │  │ --behavior  │  │                         │  │
│  └──────┬──────┘  └──────┬──────┘  └─────────────────────────┘  │
│         │                │                                      │
│         ▼                ▼                                      │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                    Agent Loop                            │   │
│  │  ┌─────────┐    ┌─────────┐    ┌─────────┐              │   │
│  │  │ Prompt  │───▶│   LLM   │───▶│  Tool   │──┐           │   │
│  │  └─────────┘    └─────────┘    │  Calls  │  │           │   │
│  │       ▲                        └─────────┘  │           │   │
│  │       │                                     │           │   │
│  │       └─────────────────────────────────────┘           │   │
│  └─────────────────────────────────────────────────────────┘   │
│                            │                                    │
│         ┌──────────────────┼──────────────────┐                │
│         ▼                  ▼                  ▼                │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐        │
│  │  Shell      │    │  Code       │    │  Browser    │        │
│  │  Tools      │    │  Tools      │    │  Tools      │        │
│  └─────────────┘    └─────────────┘    └─────────────┘        │
└─────────────────────────────────────────────────────────────────┘
```

## Package Structure

```
github.com/imyousuf/agentic-test-runner/
├── cmd/atr/              # CLI entry point
│   └── main.go
├── internal/             # Private packages
│   ├── agent/            # AI agent implementation
│   │   ├── agent.go      # Agent loop
│   │   ├── tool.go       # Tool interface
│   │   ├── registry.go   # Tool registry
│   │   ├── tools_shell.go     # Shell tools
│   │   ├── tools_read.go      # File reading tools
│   │   ├── tools_grep.go      # Code search tools
│   │   └── tools_browser.go   # Browser automation tools
│   ├── browser/          # Browser automation
│   │   ├── browser.go    # Lifecycle management
│   │   └── element.go    # Element interactions
│   ├── capture/          # Failure context capture
│   │   ├── types.go      # Data structures
│   │   └── capture.go    # Capture logic
│   ├── cli/              # CLI commands
│   │   ├── root.go       # Root command
│   │   ├── run.go        # Run command
│   │   ├── config.go     # Config commands
│   │   └── version.go    # Version command
│   ├── config/           # Configuration
│   │   └── config.go     # Config loading
│   ├── executor/         # Command execution
│   │   ├── executor.go   # Command runner
│   │   └── shell_*.go    # Platform-specific shells
│   ├── llm/              # LLM provider registration
│   │   └── gemini.go     # Gemini provider
│   └── output/           # Output formatting
│       ├── formatter.go  # Formatter interface
│       └── text.go       # Text formatter
├── pkg/                  # Public packages
│   ├── behavior/         # Behavior test results
│   │   └── result.go
│   ├── llm/              # LLM client interface
│   │   ├── client.go     # Client interface
│   │   ├── provider.go   # Provider registry
│   │   └── types.go      # Message types
│   └── result/           # Analysis results
│       └── result.go
└── examples/             # Example files
    └── behavior/         # Example tests
```

## Core Components

### Agent Loop

The agent (`internal/agent/agent.go`) implements an iterative loop:

```go
for iteration := 0; iteration < maxIterations; iteration++ {
    // 1. Send conversation to LLM
    response := llm.Chat(messages)

    // 2. Check for tool calls
    if response.HasToolCalls() {
        for _, call := range response.ToolCalls {
            // 3. Execute tool
            result := tools.Execute(call)

            // 4. Add result to conversation
            messages.Add(ToolResult{call.ID, result})
        }
    } else {
        // 5. No more tool calls - agent is done
        return ParseResult(response)
    }
}
```

### Tool Interface

Tools follow a simple interface (`internal/agent/tool.go`):

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any  // JSON Schema
    Execute(ctx context.Context, args map[string]any) (string, error)
}
```

Tools are registered in a registry and provided to the LLM as function definitions.

### LLM Provider Abstraction

The LLM client (`pkg/llm/client.go`) abstracts the provider:

```go
type Client interface {
    Chat(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error)
    Model() string
    Provider() string
    Close() error
}
```

Providers register themselves at init time:

```go
// internal/llm/gemini.go
func init() {
    llm.RegisterProvider("gemini-api", NewGeminiClient)
    llm.RegisterProvider("vertex-ai", NewVertexAIClient)
}
```

### Browser Automation

Browser automation uses [rod](https://go-rod.github.io/) for Chrome DevTools Protocol:

```go
// internal/browser/browser.go
type Browser struct {
    browser *rod.Browser
    pages   []*rod.Page
    current int
}

func (b *Browser) Launch(ctx context.Context) error
func (b *Browser) NewPage(ctx context.Context, url string) error
func (b *Browser) Click(ctx context.Context, target string) error
func (b *Browser) Fill(ctx context.Context, target, value string) error
func (b *Browser) Snapshot(verbose bool) ([]ElementInfo, error)
```

### Configuration

Configuration uses [Viper](https://github.com/spf13/viper) for flexible loading:

1. Default values (hardcoded)
2. Config file (`~/.atr/config.yaml`)
3. Environment variables (`ATR_*`, `GEMINI_API_KEY`, etc.)
4. Command-line flags

## Data Flow

### Command Failure Analysis

```
1. User runs: atr run --cmd "go test"
                 │
                 ▼
2. Executor runs command, captures output
                 │
                 ▼
3. If command fails (exit code != 0):
                 │
                 ▼
4. Agent created with shell/code tools
                 │
                 ▼
5. Agent loop:
   - LLM analyzes failure output
   - LLM calls tools (run commands, read files, search code)
   - Results added to conversation
   - Repeat until LLM has enough info
                 │
                 ▼
6. LLM outputs final analysis
                 │
                 ▼
7. Result formatted and displayed
```

### Behavior Test Execution

```
1. User runs: atr run --behavior test.txt
                 │
                 ▼
2. Browser launched, test file read
                 │
                 ▼
3. Agent created with browser tools
                 │
                 ▼
4. Prompt: "Execute this test using browser tools"
                 │
                 ▼
5. Agent loop:
   - LLM reads test step
   - LLM calls browser_snapshot to see elements
   - LLM calls browser_click, browser_fill, etc.
   - Results added to conversation
   - Repeat for each step
                 │
                 ▼
6. LLM outputs test result
                 │
                 ▼
7. Result formatted and displayed
```

## Tool Categories

### Shell Tools (`tools_shell.go`)

| Tool | Description |
|------|-------------|
| `execute_command` | Run shell commands for diagnostics |

### Code Tools (`tools_read.go`, `tools_grep.go`)

| Tool | Description |
|------|-------------|
| `read_file` | Read file contents |
| `search_code` | Regex search in codebase |

### Browser Tools (`tools_browser.go`)

21 tools for browser automation. See [Behavior Testing](behavior-testing.md) for the full list.

## Extension Points

### Adding a New Tool

1. Create tool struct implementing `Tool` interface:

```go
type MyTool struct{}

func (t *MyTool) Name() string { return "my_tool" }
func (t *MyTool) Description() string { return "Does something useful" }
func (t *MyTool) Parameters() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "param1": map[string]any{"type": "string"},
        },
        "required": []string{"param1"},
    }
}
func (t *MyTool) Execute(ctx context.Context, args map[string]any) (string, error) {
    // Implementation
    return "result", nil
}
```

2. Register in the appropriate tool factory function.

### Adding a New LLM Provider

1. Implement the `Client` interface in a new file
2. Register at init time:

```go
func init() {
    llm.RegisterProvider("my-provider", func(ctx context.Context, cfg Config) (Client, error) {
        return NewMyProviderClient(cfg)
    })
}
```

3. Import the package in `internal/llm/` to trigger registration.

## Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | Configuration management |
| `github.com/go-rod/rod` | Browser automation (CDP) |
| `github.com/google/generative-ai-go` | Gemini API client |
| `cloud.google.com/go/vertexai` | Vertex AI client |

## Security Considerations

- **API Keys**: Stored in config file or environment variables, never logged
- **Command Execution**: Commands run with user's permissions
- **Browser Isolation**: Each test gets a fresh browser context
- **Network**: Only connects to configured LLM endpoints

## Performance

- **Agent Loop**: Bounded by `max_iterations` (default: 100)
- **Timeouts**: Configurable at agent, executor, and browser levels
- **Token Usage**: Displayed after each analysis
- **Browser**: Chromium cached for fast subsequent launches
