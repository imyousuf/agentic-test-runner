# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ATR (Agentic Test Runner) is a Go CLI tool that uses AI agents to analyze command failures and run browser-based behavior tests. It supports multiple LLM backends: Claude CLI, Gemini CLI, Gemini API, and Vertex AI.

Module: `github.com/imyousuf/agentic-test-runner`
Go version: 1.25+

## Build & Development Commands

```bash
make web                # Build the live view web app into web/dist (needs Node 22+)
make build              # Build binary to bin/atr (run `make web` first)
make install            # Install to GOPATH/bin
make test               # Run tests: go test -v ./...
make test-coverage      # Tests with coverage report (coverage.html)
make lint               # Run golangci-lint (auto-installs if missing)
make fmt                # Format code with gofmt -s -w
make tidy               # go mod tidy && go mod verify
```

Run a single test:
```bash
go test -v -run TestFunctionName ./internal/agent/
```

CI runs `go test -v -race ./...`, `go vet ./...`, and format checks on PRs to main.

## Architecture

### Core Agent Loop (`internal/agent/agent.go`)

The central pattern: prepare a prompt → loop calling the LLM with tools → LLM returns tool calls → execute tools → feed results back → repeat until LLM produces a final answer (or max iterations). This drives both command analysis and behavior testing.

### LLM Provider Abstraction

- **`pkg/llm/`** — Public interface layer. `Client` interface (`Chat`, `ChatWithHistory`, `Model`, `Provider`, `Close`), `Message`/`ToolCall`/`Response` types, and a provider registry with `RegisterProvider`/`NewClient` factory pattern.
- **`internal/llm/`** — Provider implementations. `geminiClient` for Gemini API & Vertex AI (via `google.golang.org/genai`). `cliClient` for Claude CLI & Gemini CLI (spawns subprocess, communicates via JSON-RPC/MCP protocol).

### Tool System (`internal/agent/`)

Tools implement the `Tool` interface: `Name()`, `Description()`, `Parameters()` (JSON Schema), `Execute(ctx, args) (string, bool)`. Tools returning images implement `ImageResultTool` with `ExecuteWithImage`. Tools are registered in a `ToolRegistry` and converted to `llm.Tool` for LLM calls.

Tool implementations:
- `tools_shell.go` — Shell command execution
- `tools_read.go` — File reading
- `tools_grep.go` — Code search
- `tools_browser.go` — Browser automation (navigate, click, fill, screenshot, etc.)
- `tools_ask.go` — AI question tool

### Integration Surfaces (CLI / REST / MCP) and the `internal/ops` Layer

ATR exposes its browser and computer primitives through three peer surfaces, all converging on a shared execution layer.

- **`internal/ops/`** — Canonical Request/Result structs and execution functions for every primitive (e.g., `ops.ClickRequest`, `ops.Click(ctx, *browser.Browser, ClickRequest) (ClickResult, error)`). Validation and error wrapping live here. JSON tags + `jsonschema:"required"` / `jsonschema_description:"..."` struct tags drive both REST decoding and MCP `inputSchema` reflection.
- **REST daemon** (`internal/api/`) — The execution engine for `atr browser start` / `atr computer start`. Handlers decode the HTTP body into the ops Request struct, call `ops.X(...)`, and `writeSuccess(result)`. `internal/api/computer_handlers.go` keeps `abortStatus(err)` mapping `computer.ErrAborted` → HTTP 499.
- **CLI** (`internal/cli/`) — Cobra subcommands. With the exception of `atr run`, browser/computer subcommands are **thin HTTP clients** that POST to the running daemon at `http://localhost:<port>/api/v1/...` (see `internal/cli/browser.go`, `internal/cli/computer.go`). They require a running daemon.
- **MCP** (`internal/mcp/`) — JSON-RPC server (`atr mcp serve`). Tool dispatchers decode the `args map[string]any` into the same ops Request struct via `ops.MapToStruct(args, &req)`, call the same `ops.X(...)`, and format the result for MCP. `inputSchema` for each tool is generated from the ops Request struct via `schemaFor(&ops.XRequest{})` in `internal/mcp/schema.go` — no hand-written schemas. Embeds its own `*browser.Browser` / `*computer.Computer` instances; independent of any running REST daemon.

When adding a new browser/computer primitive:
1. Implement the underlying capability in `internal/browser/` or `internal/computer/`.
2. Add an `XRequest`/`XResult` struct and `func X(...)` in `internal/ops/browser_ops.go` or `internal/ops/computer_ops.go` (with `jsonschema:"required"` tags on required fields).
3. Add a REST handler in `internal/api/handlers.go` that decodes into the Request and calls `ops.X`.
4. Add a CLI subcommand in `internal/cli/` that HTTPs to that handler (if user-facing).
5. Add an MCP tool entry in `internal/mcp/tools.go` or `internal/mcp/computer_tools.go` (`InputSchema: schemaFor(&ops.XRequest{})`) and a dispatch case in `internal/mcp/server.go` or `internal/mcp/computer_dispatch.go` (if agent-callable).

For LLM-driven use, any LLM with shell access can drive ATR via plain `atr <subcommand>` invocations; MCP is for agents that consume tool schemas directly.

### Two Main Execution Modes

1. **Command Analysis** (`atr run --cmd "go test ./..."`) — Executor runs the command; on failure, the agent loop starts with shell/read/grep tools to diagnose the issue and produce a summary, root cause, and recommendations.

2. **Behavior Testing** (`atr run --behavior tests/login.test.txt`) — Parses `.test.txt` files with natural language test steps, launches a browser, and the agent drives browser tools to execute the steps and report pass/fail.

### Other Key Packages

- **`internal/cli/`** — Cobra command definitions (run, config, browser, computer, mcp, rdp, test, version, update). Browser subcommands include: navigate, click, fill, hover, drag, wait, scroll, screenshot (with --selector, --selector-all, --full, --timeout), snapshot, clean-snapshot, computed-styles (with --selector, --selector-all), computed-styles-diff (with --selector), text, font-check, download-images, viewport, batch, eval, ask, record (with --output, --url), console, network, errors. Computer subcommands include: start, stop, status, screenshot, click, move, drag, scroll, hover, type, key, chord, position, displays, window (list/active/focus/minimize/maximize/restore/close/move/resize), app (launch/quit), reset-approvals, ask (LLM agent loop for natural-language tasks).
- **`internal/config/`** — Configuration loading from `~/.atr/config.yaml`, env vars, and CLI flags via Viper
- **`internal/executor/`** — Cross-platform shell execution with environment detection (Python venv, nvm)
- **`internal/browser/`** — Browser lifecycle management using `go-rod/rod` (Chromium via CDP)
- **`internal/computer/`** — Cross-platform desktop control via `go-vgo/robotgo` (mouse/keyboard/screen) and `xgbutil/ewmh` for X11 window management. Linux-X11 only in v1; macOS/Windows window management is stubbed. Includes a configurable countdown safety gate (`per-request` / `per-app` / `off`) before each gated action, abortable via SIGINT.
- **`internal/api/`** — REST daemon (the execution engine). Holds session state and runs primitives. Started by `atr browser start` / `atr computer start`; CLI subcommands HTTP into it.
- **`internal/mcp/`** — MCP JSON-RPC server for Claude Code integration (`atr mcp serve`). Peer surface to CLI/REST: embeds its own `Browser`/`Computer` and calls the same package methods.
- **`internal/rdp/`** — Browser live view (`atr rdp`). Attaches to a running browser as a second CDP session and streams the active page over a WebSocket to an embedded web app, relaying mouse/keyboard input back. Read-only mode is enforced in the `Streamer`, so every surface inherits it. Never owns or closes the browser.
- **`web/`** — The live view's React/Vite application. `web/dist` is build output and is **not** committed: run `make web` (needs Node 22+) before `make build`. The `noweb` build tag serves a placeholder for contributors without Node.
- **`internal/capture/`** — Test failure context capture
- **`internal/output/`** — Output formatting (text, file, summarization)
- **`pkg/behavior/`** and **`pkg/result/`** — Public result types

### Claude Code Integration

- `.mcp.json` — Registers ATR as an MCP server
- `.claude-plugin/plugin.json` — Plugin metadata
- `skills/` — Claude Code skills (atr-browser, atr-computer, atr-behavior, atr-analyze)

## Code Conventions

- Error wrapping: `fmt.Errorf("doing X: %w", err)`
- Tool results return `(string, bool)` where bool indicates error
- Platform-specific code uses `_darwin.go`, `_linux.go`, `_windows.go` suffixes
- Configuration uses Viper with YAML config at `~/.atr/config.yaml`
