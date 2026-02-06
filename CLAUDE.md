# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ATR (Agentic Test Runner) is a Go CLI tool that uses AI agents to analyze command failures and execute browser-based behavior tests written in natural language. It supports multiple LLM backends (Gemini API, Vertex AI, Claude CLI, Gemini CLI) and uses Chrome DevTools Protocol for browser automation.

## Common Commands

```bash
make build          # Build binary to bin/atr
make test           # Run all tests (go test -v ./...)
make lint           # Run golangci-lint (auto-installs if missing)
make fmt            # Format code with gofmt
make tidy           # Tidy and verify go.mod
make test-coverage  # Generate HTML coverage report
make install        # Install to GOPATH/bin
```

Run a single test:
```bash
go test -v -run TestFunctionName ./internal/agent/
```

## Architecture

### Agent Loop (core pattern)

The system is built around an iterative agent loop in `internal/agent/agent.go`:
1. Build a prompt with failure context or behavior test content
2. Send conversation + tool definitions to an LLM
3. Parse tool calls from the LLM response
4. Execute each tool, add results back to conversation
5. Repeat until the LLM responds with no tool calls (final analysis)

Two entry points: `AnalyzeFailure()` for command failure analysis, `ExecuteBehaviorTest()` for browser tests.

### Tool System

All agent capabilities are pluggable tools implementing `internal/agent/tool.go`:
- `Tool` interface: `Name()`, `Description()`, `Parameters()` (JSON Schema), `Execute(ctx, args) (string, bool)`
- `ImageResultTool` interface: extends Tool with `ExecuteWithImage()` for screenshot-like tools
- `internal/agent/registry.go`: thread-safe tool registry that maintains registration order

Tool categories:
- **Shell**: `tools_shell.go` — `execute_command`
- **Code**: `tools_read.go`, `tools_grep.go` — `read_file`, `search_code`
- **Browser**: `tools_browser.go` — 21 tools (navigate, click, fill, snapshot, screenshot, etc.)
- **Ask**: `tools_ask.go` — `browser_ask` for AI-powered page inspection

### LLM Provider Abstraction

`pkg/llm/client.go` defines the `Client` interface (`Chat`, `ChatWithHistory`, `Model`, `Provider`, `Close`). Providers self-register via `RegisterProvider()` in init functions.

Implementations in `internal/llm/`:
- `gemini.go` — Gemini API and Vertex AI (Google genai SDK)
- `cli_claude.go` — Claude CLI (shell-based, no API key needed)
- `cli_gemini.go` — Gemini CLI (shell-based)

### Key Packages

| Package | Purpose |
|---------|---------|
| `cmd/atr/` | Entry point, calls `cli.Execute()` |
| `internal/agent/` | Agent loop, tool interfaces, all tool implementations |
| `internal/browser/` | Browser lifecycle via rod (CDP), element interactions, snapshots |
| `internal/cli/` | Cobra command definitions (run, browser, config, mcp, test, update) |
| `internal/config/` | Viper-based config: defaults → `~/.atr/config.yaml` → env vars (`ATR_*`) → CLI flags |
| `internal/executor/` | Cross-platform command execution, shell detection, venv/nvm activation |
| `internal/llm/` | LLM client implementations |
| `internal/api/` | HTTP server for browser control |
| `internal/mcp/` | MCP server implementation |
| `pkg/llm/` | Public LLM interfaces and types (Client, Message, Tool, Response, Provider) |
| `pkg/result/` | AnalysisResult, CommandResult, AgentMetrics types |
| `pkg/behavior/` | Behavior test result types |
| `skills/` | Claude Code skill definitions (atr-analyze, atr-behavior, atr-browser) |

### Result Parsing

The agent parses LLM final responses into structured results by extracting markdown sections (`parseSections()` in agent.go). Results include Status, Summary, Root Cause, and Recommendations.

### Platform-Specific Code

Shell detection and environment activation have platform-specific files:
- `shell_darwin.go`, `shell_linux.go`, `shell_windows.go`
- `activation_unix.go`, `activation_windows.go`

### Browser Automation

Uses the `go-rod/rod` library for Chrome DevTools Protocol. The `browser_snapshot` tool assigns unique element IDs so the LLM can reference specific elements. Browser instances can be launched fresh or connected to via an existing CDP endpoint.

## Module

```
github.com/imyousuf/agentic-test-runner (Go 1.25)
```
