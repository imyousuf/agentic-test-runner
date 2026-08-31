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

### Compiled Behavior Tests (`internal/testscript/`, `internal/agent/behavior_*.go`)

A `.test.txt` spec compiles once to a sibling `.test.js` and replays with no
model in the loop; the agent returns only to triage a failure.

- `internal/testscript/` is an embedded **goja** JS runtime plus a host `atr`
  library. No Node, no DOM, no network — scripts reach the browser only
  through the library, which keeps ATR a single binary.
- The **failure taxonomy** in `errors.go` is the load-bearing part.
  `assertion` means the app is wrong and is *never* repaired — repairing an
  assertion is indistinguishable from deleting the test. `not_found` and
  `script` are repair candidates; `timeout` and `environment` are retried.
  `RunBehavior` never calls the model for an assertion failure, which is both
  the cost saving and the safety property.
- `findElement`'s CSS/XPath branches return early and so never reach the
  `ErrElementNotFound` at the end of the fallback chain. `asNotFound` maps
  their deadline to it — without that, the most common drift case (a CSS
  selector that no longer matches) is misclassified as environmental and
  retried forever instead of repaired.
- Test inputs live in a sibling `*.test.properties` (committed) layered under
  `*.test.override.properties` (gitignored) and `ATR_VALUE_*` env vars. Values
  support `$(command)` / `${VAR}`, expanded lazily at read time and cached per
  run — which also means **a properties file is executable**, so a committed
  one runs on every machine including CI.
- A missing or unresolvable input is `KindConfig`: not repairable, not
  retryable, and never sent to the model. The obvious "repair" is to inline the
  literal back into the script, which would undo the reason inputs live
  outside it.
- Compiled scripts are **committed**, and carry an `atr-spec-sha256` header.
  A spec edit invalidates them; a whitespace-only edit does not, because a
  reformat should not cost tokens.

2. **Behavior Testing** (`atr run --behavior tests/login.test.txt`) — Parses `.test.txt` files with natural language test steps, launches a browser, and the agent drives browser tools to execute the steps and report pass/fail.

### Other Key Packages

- **`internal/cli/`** — Cobra command definitions (run, config, browser, computer, remote, record, mcp, test, version, update). `atr remote` serves the live-view web UI over CDP screencast; `atr record` captures a session as JPEG frames plus a manifest, with subcommands list, encode, repair, rm, doctor. Browser subcommands include: navigate, click, fill, hover, drag, wait, scroll, screenshot (with --selector, --selector-all, --full, --timeout), snapshot, clean-snapshot, computed-styles (with --selector, --selector-all), computed-styles-diff (with --selector), text, font-check, download-images, viewport, batch, eval, ask, record (with --output, --url), console, network, errors, hud (on/off/status). Computer subcommands include: start, stop, status, screenshot, click, move, drag, scroll, hover, type, key, chord, position, displays, window (list/active/focus/minimize/maximize/restore/close/move/resize), app (launch/quit), reset-approvals, ask (LLM agent loop for natural-language tasks).
- **`internal/config/`** — Configuration loading from `~/.atr/config.yaml`, env vars, and CLI flags via Viper
- **`internal/executor/`** — Cross-platform shell execution with environment detection (Python venv, nvm)
- **`internal/browser/`** — Browser lifecycle management using `go-rod/rod` (Chromium via CDP)
- **`internal/computer/`** — Cross-platform desktop control via `go-vgo/robotgo` (mouse/keyboard/screen) and `xgbutil/ewmh` for X11 window management. Linux-X11 only in v1; macOS/Windows window management is stubbed. Includes a configurable countdown safety gate (`per-request` / `per-app` / `off`) before each gated action, abortable via SIGINT.
- **`internal/api/`** — REST daemon (the execution engine). Holds session state and runs primitives. Started by `atr browser start` / `atr computer start`; CLI subcommands HTTP into it.
- **`internal/mcp/`** — MCP JSON-RPC server for Claude Code integration (`atr mcp serve`). Peer surface to CLI/REST: embeds its own `Browser`/`Computer` and calls the same package methods.
- **`internal/secret/`** — Fetches secrets by running the user's password-manager command. Used by `browser_fill_secret` so a credential is fetched and consumed inside one tool call and never becomes a tool result (which would put it in the LLM message history, re-sent on every later turn).
- **`internal/capture/`** — Test failure context capture
- **`internal/output/`** — Output formatting (text, file, summarization)
- **`pkg/behavior/`** and **`pkg/result/`** — Public result types

### Browser Concurrency Invariants (`internal/browser/browser.go`)

Three rules, each learned from a deadlock. Break one and the symptom is a
hang or a spurious `context deadline exceeded`, usually only under load:

1. **Never block rod's event-dispatch goroutine.** `page.EachEvent` /
   `browser.EachEvent` callbacks run on it; blocking there stops rod reading
   further CDP messages, including the responses a blocked call is waiting
   for. Target callbacks therefore hand work to `queueTargetEvent`, which is
   drained by one worker (one, not one-per-event, so created/destroyed pairs
   stay ordered).
2. **Never hold `b.mu` across a CDP call.** `Runtime.enable` can stall
   indefinitely when a renderer is wedged, and holding the lock through it
   freezes every other caller. `NewPage` and `handleTargetCreated` both read
   what they need under `RLock`, do the CDP setup unlocked, then take the
   write lock only for the bookkeeping — and re-check the target map, since
   the other path may have registered it meanwhile.
3. **`go page.EachEvent(cb)()` spawns only `wait()`.** The `EachEvent(cb)`
   call itself — including the `EnableDomain` it performs — runs on the
   *calling* goroutine. Calling it inline (`wait := page.EachEvent(cb)`)
   deadlocks: it subscribes and then blocks on `Runtime.enable` before
   returning the `wait()` that drains the subscription.

4. **One CDP session per target.** `PageFromTarget` hands out a *fresh*
   session every time it is called, so a target reached by two paths ends up
   with two — each enabling the Runtime and Network domains and overriding the
   viewport. `NewPage` and the target-created worker both reach every new
   target, so both go through `adoptTarget`, which is serialised on `adoptMu`
   and returns the page already in use if there is one. Noticing the duplicate
   afterwards is not enough: the old code re-checked the target map at the end
   and discarded the second page, by which time the second session was
   attached and its domains enabled.

Chrome will still, under tab churn, occasionally bring up a target whose
renderer answers nothing: browser-level calls such as `Target.getTargetInfo`
succeed and report the URL, while everything needing the renderer — evaluating
script, reading the DOM, waiting for load — goes unanswered and never
recovers. It cannot be retried around, because a fresh same-origin tab lands in
the same wedged renderer. `waitLoad` therefore takes its wait in slices and
probes with a trivial `Runtime.evaluate` between them, so this is reported as
`ErrRendererUnresponsive` in seconds rather than as "did not finish loading"
after the whole page budget. Tests that are not about renderer health skip on
that one typed error.

Related: `tryFind` rebinds a found element to a fresh action-sized deadline.
The search timeout it was found under (500ms on the UID path) must not follow
the element into the caller's next operations — `Fill` makes three more CDP
calls after finding, which fit in the leftover budget on an idle connection
and do not when anything else is talking to the same target.

### In-Page Agent HUD (`internal/browser/hud.go`, `internal/agent/hud.go`)

`atr browser hud on` injects a floating agent panel into every page of a headed
browser. Notes for anyone touching it:

- The panel is injected into a **named isolated world**, not the page's main
  world, and talks to Go over a **CDP binding** rather than the network. A
  `fetch`/WebSocket transport would be blocked by `connect-src` on any
  strict-CSP site; a CDP binding is not a network request. Isolated-world
  globals are also invisible to page script.
- The UI lives in a **closed shadow root**. That is what keeps it out of
  `Snapshot()`, which uses `querySelectorAll` and does not pierce shadow
  boundaries. Screenshot paths call `hideHud(page)` to keep it out of captures.
- **Nothing on the event-dispatch path may take `b.mu` or make a blocking CDP
  call.** `page.EachEvent` callbacks run on rod's dispatch goroutine; blocking
  there stalls every other listener. `handleHudMessage` takes the `*hudSession`
  as a parameter for exactly this reason, and pushes replies from a goroutine.
- `page.EachEvent` must be spawned (`go page.EachEvent(...)()`), never called
  inline: it subscribes and *then* makes a blocking `Runtime.enable` call
  before returning the `wait()` that drains the subscription. The resulting
  window where the injected script has run but the subscription is not yet live
  is covered by the panel retrying its `hello` until answered.
- Panel pushes go through an **outbox queue**, never inline. Chrome serialises
  commands per target, so a `Runtime.evaluate` painting the transcript in
  between the agent's own commands eats into the budget of the very next
  click or fill. The outbox is never closed — senders would race and panic;
  `deliverLoop` exits on `done`.
- `hud.attached` is keyed by **target ID, not `*rod.Page`**. `PageFromTarget`
  returns a fresh Page value for a target it has already returned, so a
  pointer key lets one tab be attached twice — and every panel message then
  gets dispatched twice.
- The HUD is deliberately **not exposed over MCP** — an agent enabling an
  in-page panel for itself is not a useful operation.

### Claude Code Integration

- `.mcp.json` — Registers ATR as an MCP server
- `.claude-plugin/plugin.json` — Plugin metadata
- `skills/` — Claude Code skills (atr-browser, atr-computer, atr-behavior, atr-analyze)

## Code Conventions

- Error wrapping: `fmt.Errorf("doing X: %w", err)`
- Tool results return `(string, bool)` where bool indicates error
- Platform-specific code uses `_darwin.go`, `_linux.go`, `_windows.go` suffixes
- Configuration uses Viper with YAML config at `~/.atr/config.yaml`
