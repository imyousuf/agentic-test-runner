# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ATR (Agentic Test Runner) is a Go CLI tool that uses AI agents to analyze command failures and run browser-based behavior tests. It supports multiple LLM backends: Claude CLI, Gemini CLI, Gemini API, and Vertex AI.

Module: `github.com/imyousuf/agentic-test-runner`
Go version: 1.25+

## Build & Development Commands

```bash
make build              # Build binary to bin/atr
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

### Two Main Execution Modes

1. **Command Analysis** (`atr run --cmd "go test ./..."`) — Executor runs the command; on failure, the agent loop starts with shell/read/grep tools to diagnose the issue and produce a summary, root cause, and recommendations.

2. **Behavior Testing** (`atr run --behavior tests/login.test.txt`) — Parses `.test.txt` files with natural language test steps, launches a browser, and the agent drives browser tools to execute the steps and report pass/fail.

### Other Key Packages

- **`internal/cli/`** — Cobra command definitions (run, config, browser, mcp, test, version, update)
- **`internal/config/`** — Configuration loading from `~/.atr/config.yaml`, env vars, and CLI flags via Viper
- **`internal/executor/`** — Cross-platform shell execution with environment detection (Python venv, nvm)
- **`internal/browser/`** — Browser lifecycle management using `go-rod/rod` (Chromium via CDP)
- **`internal/api/`** — HTTP server for long-running browser control (`atr browser start`)
- **`internal/mcp/`** — MCP JSON-RPC server for Claude Code integration (`atr mcp serve`)
- **`internal/capture/`** — Test failure context capture
- **`internal/output/`** — Output formatting (text, file, summarization)
- **`pkg/behavior/`** and **`pkg/result/`** — Public result types

### Claude Code Integration

- `.mcp.json` — Registers ATR as an MCP server
- `.claude-plugin/plugin.json` — Plugin metadata
- `skills/` — Claude Code skills (atr-browser, atr-behavior, atr-analyze)

## Code Conventions

- Error wrapping: `fmt.Errorf("doing X: %w", err)`
- Tool results return `(string, bool)` where bool indicates error
- Platform-specific code uses `_darwin.go`, `_linux.go`, `_windows.go` suffixes
- Configuration uses Viper with YAML config at `~/.atr/config.yaml`
