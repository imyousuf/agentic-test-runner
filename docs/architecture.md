# Architecture

This document describes the internal architecture of ATR.

## Overview

ATR has two layered concerns:

1. An **AI agent loop** that uses tools to investigate command failures and run behavior tests (`atr run`).
2. A set of **browser and desktop primitives** (~54 of them) exposed through three peer integration surfaces — CLI, REST, MCP — all converging on a single canonical execution layer (`internal/ops`).

The agent loop and the primitives meet whenever an agent (`atr run`, `atr browser ask`, `atr computer ask`) calls a browser/computer tool: the tool implementations route through the same ops layer that REST and MCP use.

```
┌─────────────────────────────────────────────────────────────────┐
│                          ATR surfaces                           │
│                                                                 │
│   CLI (cobra)          REST (HTTP)            MCP (JSON-RPC)    │
│   internal/cli/        internal/api/          internal/mcp/     │
│        │                    │                       │           │
│        │  HTTP to daemon    │  decode JSON          │  decode   │
│        ▼                    ▼  into Request         ▼   args    │
│   (runs the daemon)         │                       │           │
│                             ▼                       ▼           │
│                    ┌────────────────────────────────────┐       │
│                    │     internal/ops (canonical)       │       │
│                    │   Request structs + Execute funcs  │       │
│                    │   one source of truth per primitive│       │
│                    └────────────────────────────────────┘       │
│                             │                       │           │
│                             ▼                       ▼           │
│                    internal/browser/         internal/computer/ │
│                    (rod / CDP)               (robotgo / X11)    │
│                                                                 │
│   Agent loop (internal/agent/) calls ops.* via tool wrappers.   │
└─────────────────────────────────────────────────────────────────┘
```

## Package Structure

```
github.com/imyousuf/agentic-test-runner/
├── cmd/atr/              # CLI entry point
├── internal/             # Private packages
│   ├── agent/            # AI agent loop + tool wrappers (shell, read, grep, browser, ask)
│   ├── api/              # REST daemon: HTTP handlers that decode into ops.* requests
│   ├── browser/          # Browser primitives via go-rod (CDP)
│   ├── capture/          # Test-failure context capture
│   ├── cli/              # Cobra commands (run, browser, computer, mcp, config, ...)
│   ├── computer/         # Desktop primitives via robotgo + X11/EWMH
│   ├── config/           # Viper-based config loading
│   ├── executor/         # Cross-platform shell execution
│   ├── llm/              # LLM provider implementations (Gemini API, Vertex, CLI)
│   ├── mcp/              # MCP JSON-RPC server; reflects schemas from ops Request structs
│   ├── ops/              # Canonical Request/Result + Execute funcs for every primitive
│   └── output/           # Output formatting (text, file, summary)
├── pkg/
│   ├── behavior/         # Public behavior-test result types
│   ├── llm/              # Public LLM client interface + provider registry
│   └── result/           # Public analysis-result types
└── examples/behavior/    # Example .test.txt files
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

### Integration Surfaces and the `internal/ops` Layer

ATR exposes its browser and computer primitives through three peer surfaces, all converging on a shared execution layer.

- **`internal/ops/`** — Canonical `Request`/`Result` structs and execution functions, one per primitive (e.g. `ops.ClickRequest`, `ops.Click(ctx, *browser.Browser, ClickRequest) (ClickResult, error)`). Validation and error wrapping live here. JSON tags + `jsonschema:"required"` / `jsonschema_description:"..."` struct tags drive both REST decoding and MCP `inputSchema` reflection.

- **REST daemon (`internal/api/`)** — The execution engine for `atr browser start` / `atr computer start`. Handlers decode the HTTP body into the ops Request, call `ops.X(...)`, and `writeSuccess(result)`. Computer handlers map `computer.ErrAborted` to HTTP 499 via `abortStatus(err)`.

- **CLI (`internal/cli/`)** — Cobra subcommands. Except for `atr run`, the browser/computer subcommands are **thin HTTP clients** that POST to the running daemon at `http://localhost:<port>/api/v1/...`. They require a running daemon.

- **MCP (`internal/mcp/`)** — JSON-RPC server (`atr mcp serve`). Each tool dispatcher decodes the `args map[string]any` into the same ops Request via `ops.MapToStruct(args, &req)`, calls `ops.X(...)`, and formats the result for MCP. `inputSchema` is generated from the ops Request struct via `schemaFor(&ops.XRequest{})` — there are no hand-written schemas. The MCP server embeds its own `*browser.Browser` / `*computer.Computer`; it does not require a running REST daemon.

```go
// internal/ops/browser_ops.go — canonical primitive
type ClickRequest struct {
    Selector    string `json:"selector"     jsonschema:"required" jsonschema_description:"CSS selector to click"`
    DoubleClick bool   `json:"double_click"                       jsonschema_description:"Issue a double-click instead"`
}

func Click(ctx context.Context, b *browser.Browser, req ClickRequest) (ClickResult, error) { ... }
```

```go
// internal/api/handlers.go — REST adapter
var req ops.ClickRequest
_ = json.NewDecoder(r.Body).Decode(&req)
res, err := ops.Click(r.Context(), s.browser, req)
writeSuccess(w, res)
```

```go
// internal/mcp/server.go — MCP adapter
var req ops.ClickRequest
_ = ops.MapToStruct(args, &req)
res, err := ops.Click(ctx, s.browser, req)
return fmt.Sprintf("Clicked on %s", res.Selector), nil
```

When adding a new primitive, the work is:

1. Implement the underlying capability in `internal/browser/` or `internal/computer/`.
2. Add the `XRequest`/`XResult` and `func X(...)` in `internal/ops/`.
3. Add a REST handler that decodes into `XRequest` and calls `ops.X`.
4. Add a CLI subcommand that HTTPs to that handler (if user-facing).
5. Add an MCP tool entry (`InputSchema: schemaFor(&ops.XRequest{})`) and dispatch case (if agent-callable).

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

~32 browser tools for navigation, interaction, snapshots, screenshots, computed-style inspection, network, console, and more. The agent's tool wrappers delegate to `internal/ops` so they share validation and behavior with the REST/MCP surfaces. See [Behavior Testing](behavior-testing.md) for the full list.

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
| `github.com/go-vgo/robotgo` | Cross-platform desktop control (mouse, keyboard) |
| `github.com/jezek/xgbutil` | X11 EWMH window management (Linux) |
| `github.com/vcaesar/screenshot` | Screen capture |
| `github.com/invopop/jsonschema` | Reflect Go structs into MCP `inputSchema` |
| `google.golang.org/genai` | Gemini API + Vertex AI client |

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
