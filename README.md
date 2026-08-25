# ATR - Agentic Test Runner

[![Go Report Card](https://goreportcard.com/badge/github.com/imyousuf/agentic-test-runner)](https://goreportcard.com/report/github.com/imyousuf/agentic-test-runner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**ATR** is an AI-powered test runner that automatically analyzes failures and runs browser-based behavior tests using natural language.

## Features

- **AI Failure Analysis**: Run any command and get intelligent analysis when it fails
- **Browser Behavior Testing**: Write tests in natural language, let AI execute them
- **Multiple LLM Backends**: Supports Google Gemini API, Vertex AI, Claude on Vertex AI (with prompt caching), and CLI tools (Claude, Gemini)
- **CLI Backend Support**: Use Claude CLI or Gemini CLI as backends - no API keys needed
- **Cross-platform Desktop Control**: Mouse, keyboard, screen capture, window/app management on Linux (X11), macOS, and Windows via `atr computer`, with multi-monitor support and an in-process LLM agent (`atr computer ask`)
- **MCP Server**: Expose browser AND desktop tools to any MCP-compatible client
- **Browser Live View**: Watch the browser in a web page and take over when a step needs a person, such as a login with MFA, via `atr rdp`. No X server or VNC required
- **Cross-Platform**: Works on Linux, macOS, and Windows
- **Extensible**: Tool-based architecture for custom extensions

## Quick Start

### Install

**macOS / Linux:**
```bash
curl -fsSL https://raw.githubusercontent.com/imyousuf/agentic-test-runner/main/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/imyousuf/agentic-test-runner/main/install.ps1 | iex
```

**From source** (needs Go 1.25+ and Node 22+):
```bash
git clone https://github.com/imyousuf/agentic-test-runner.git
cd agentic-test-runner
make web && make install
```

`make web` builds the `atr rdp` live view that is embedded in the binary;
`web/dist` is build output and is not committed. Without Node, build with
`go build -tags noweb ./cmd/atr` — every command still works except the live
view's web page.

Or download from [Releases](https://github.com/imyousuf/agentic-test-runner/releases).

### Configure

```bash
# Option 1: Using Claude or Gemini CLI (no API key needed)
# If you have claude or gemini CLI installed, ATR auto-detects them
atr config init  # Creates config with detected CLI as default backend

# Option 2: Using Gemini API
export GEMINI_API_KEY="your-api-key"

# Option 3: Using Vertex AI
gcloud auth application-default login
export GOOGLE_CLOUD_PROJECT="your-project"
```

### Run

**Analyze a failing command:**

```bash
atr run --cmd "go test ./..."
```

When the command fails, ATR's AI agent will:
- Analyze the failure output
- Run diagnostic commands
- Read relevant source files
- Provide actionable recommendations

**Run a behavior test:**

```bash
# Create a test file
cat > login.test.txt << 'EOF'
Test: User can log in

Steps:
1. Navigate to http://localhost:3000/login
2. Enter "user@example.com" in email field
3. Enter "password" in password field
4. Click "Sign In" button
5. Verify URL contains /dashboard
EOF

# Run the test
atr run --behavior login.test.txt
```

## Example Output

```
Executing: go test ./...
Directory: /path/to/project

--- FAIL: TestUserAuth (0.05s)
    auth_test.go:42: expected 200, got 401

✗ Command failed (exit code: 1, duration: 2.3s)

Analyzing failure with AI agent...
Using model: gemini-3.7-flash (gemini-api)

======================================================================
ANALYSIS RESULTS
======================================================================

Status: FAILURE

Summary:
  TestUserAuth fails because the auth middleware expects a JWT token,
  but the test doesn't provide one in the request headers.

Root Cause:
  Line 38 in auth_test.go creates a request without Authorization header.
  The auth middleware (middleware/auth.go:15) rejects it with 401.

Recommendations:
  1. Add mock JWT token to test request
  2. Or bypass auth middleware in test setup
  3. Check if middleware was recently added to the route

Files Examined:
  - auth_test.go
  - middleware/auth.go
  - routes/api.go
```

## Documentation

- **[Getting Started](docs/getting-started.md)** - First steps with ATR
- **[Installation](docs/installation.md)** - All installation methods
- **[Configuration](docs/configuration.md)** - Config file and authentication setup
- **[CLI Reference](docs/cli-reference.md)** - All commands and flags
- **[Behavior Testing](docs/behavior-testing.md)** - Write browser tests in natural language
- **[Compiled Behavior Tests](docs/behavior-compilation.md)** - Specs compile to JavaScript and replay with no model calls
- **[Browser Server](docs/browser-server.md)** - HTTP server for programmatic browser control
- **[In-Page Agent HUD](docs/browser-hud.md)** - Agent panel inside the browser window, with leak-free password filling
- **[Browser Live View](docs/rdp-live-view.md)** - Watch and control the browser from a web page (`atr rdp`)
- **[MCP Server](docs/mcp-server.md)** - MCP protocol server for CLI tool integration
- **[Architecture](docs/architecture.md)** - How ATR works internally
- **[llms.txt](docs/llms.txt)** - Quick reference for AI agents

## Claude Code Integration

ATR includes Claude Code skills for seamless AI-assisted browser automation. Install the skills to enable natural language control of ATR within Claude Code.

### Installation

1. Add the ATR marketplace:
   ```
   /plugin marketplace add imyousuf/agentic-test-runner
   ```

2. Install the skills plugin:
   ```
   /plugin install atr-skills@atr-marketplace
   ```

### Available Skills

| Skill | Description |
|-------|-------------|
| **atr-browser** | Control browser via ATR server (navigate, click, fill, screenshot, `browser ask`, `browser hud`) |
| **atr-computer** | Cross-platform desktop control (mouse, keyboard, screen, windows, apps, multi-monitor) plus `computer ask` — an in-process LLM agent that takes a natural-language instruction and drives the desktop end-to-end |
| **atr-analyze** | Run tests with AI analysis (default for test suites - keeps context clean) |
| **atr-behavior** | Run natural language browser tests |

The `atr-browser` and `atr-computer` skills cross-reference each other and can be loaded together when a workflow spans both (e.g. drag a file from the file manager into a browser drop zone).

### Usage Examples

Once installed, Claude Code automatically uses these skills when relevant:

- "Navigate to google.com and take a screenshot"
- "Run the pytest tests" (uses atr-analyze for clean output)
- "Run the behavior tests in tests/e2e/"
- "Open the GNOME calculator and tell me what 17×23 is" (computer ask)

## MCP Server

ATR can run as an MCP (Model Context Protocol) server, exposing browser automation tools to any MCP-compatible client like Claude CLI or Gemini CLI.

```bash
# Start the MCP server
atr mcp serve
```

### Integration with Claude CLI

```bash
# Inline configuration
claude -p "Navigate to example.com and take a screenshot" \
  --mcp-config '{"mcpServers":{"atr-browser":{"command":"atr","args":["mcp","serve"]}}}' \
  --allowedTools "mcp__atr-browser__*"

# Or add to ~/.claude.json
```

### Integration with Gemini CLI

Add to your project's `.gemini/settings.json`:

```json
{
  "mcpServers": {
    "atr-browser": {
      "command": "atr",
      "args": ["mcp", "serve"],
      "trust": true
    }
  }
}
```

See [MCP Server Documentation](docs/mcp-server.md) for the full list of browser and desktop tools exposed via MCP.

## Browser Live View (`atr rdp`)

Watch the browser that ATR drives, in a web page, and take control when a step needs a
person. Chrome streams the page over the DevTools Protocol, so no X server, no VNC, and no
desktop packages are needed. It works with a headless browser on a server.

```bash
# On the machine that runs the browser
atr browser start --headless
atr rdp
```

The command prints a URL with an access token:

```
ATR live view
  URL:     http://127.0.0.1:7788/?t=8f2c...
  Browser: Chrome/151.0.7922.170  (attached, not owned)
  Pages:   2
```

Open the URL. You can click, type, scroll, and paste. A tab bar and a URL bar let you switch
pages and navigate. Closing the view leaves the browser running, and ATR keeps driving it
while you watch.

### Agent on a server, viewer on your laptop

```bash
# On the server
atr rdp setup                      # install a service that keeps it running

# On your laptop
ssh -L 7788:127.0.0.1:7788 myserver
```

Then open `http://127.0.0.1:7788/?t=<token>`.

`atr rdp setup` writes a systemd user unit on Linux, or a launchd agent on macOS, and stores
a token in `~/.atr/rdp.env` so the URL is stable. Use `--check` to report the state, and
`--uninstall` to remove the service.

| Flag | Default | Purpose |
|---|---|---|
| `--port` | `7788` | HTTP port |
| `--bind` | `127.0.0.1` | Listen address |
| `--attach` | discovered | CDP endpoint, such as `cdp://127.0.0.1:9222` |
| `--view-only` | `false` | Refuse input from viewers |
| `--fps` | `20` | Target frame rate |

**Security**: a viewer gets full control of that browser and its cookies. The default bind is
loopback only. Reach a remote machine through an SSH tunnel, not by opening the port.

See [Browser Live View](docs/rdp-live-view.md) for the protocol, the foreground rule, and
troubleshooting.

## Desktop Control (`atr computer`)

ATR also drives the desktop directly — mouse, keyboard, screen capture, window/app management — for tests and automation that go beyond the browser.

```bash
# Start the daemon (default: 3-second countdown before each action; Ctrl+C to abort)
atr computer start

# Low-level primitives
atr computer screenshot --output /tmp/desktop.png
atr computer click --display 0 800 50
atr computer type "hello world"
atr computer window list

# High-level: ask an agent to do it for you
atr computer ask "open xclock and tell me what time it shows"
atr computer ask --max-steps 30 "open the GNOME calculator and compute 17 * 23"

atr computer stop
```

Default LLM models for `atr computer ask` (and `atr browser ask`):

| Backend                        | Tier     | Model                      |
|--------------------------------|----------|----------------------------|
| `gemini-api` / `vertex-ai`     | flash    | `gemini-3.7-flash`         |
| `gemini-api` / `vertex-ai`     | pro      | `gemini-3.1-pro-preview`   |
| `vertex-claude`                | sonnet   | `claude-sonnet-5`          |
| `vertex-claude`                | opus     | `claude-opus-5`            |

Switch with `atr --model pro computer ask "..."`. Backend `claude-cli` uses the Claude CLI subprocess via MCP and ignores the model flag.

Multi-monitor: bounds reported by `atr computer displays` and `window list` are in **root coordinates** (the bounding box of all monitors at origin). Use `--display N` to pass display-local pixels for clicks. Linux is X11-only in v1; Wayland is tracked as future work.

See [CLI Reference — atr computer](docs/cli-reference.md#atr-computer) and [Computer Server REST API](docs/computer-server.md) for full details.

## Configuration

ATR can be configured via `~/.atr/config.yaml`:

```yaml
# CLI backend (no API key needed - uses installed CLI tools)
backend: claude-cli  # or gemini-cli

# Or API backends:
backend: gemini-api  # or vertex-ai
model: flash         # or pro

gemini:
  api_key: "your-key"

# Or for Vertex AI:
vertex:
  project: your-project
  location: us-central1

# Or Claude models through Vertex AI, with prompt caching:
backend: vertex-claude
model: sonnet        # or opus
vertex:
  project: your-project
  location: global
```

### Claude on Vertex AI

`backend: vertex-claude` runs Sonnet and Opus over the Messages API through
Vertex, authenticated with Application Default Credentials:

```bash
gcloud auth application-default login
export GOOGLE_CLOUD_PROJECT=your-project
atr test
```

No API key is stored anywhere. Each request marks one prompt-cache checkpoint
at the end of its fixed prefix — the tool schemas and the system prompt — so
the agent loop pays for that prefix once instead of on every iteration. Caching
only applies above the API's minimum cacheable prompt size, so the agents with
large tool sets and long loops are the ones that benefit; short prompts are
sent uncached. A freshly written entry takes a few seconds to become readable,
so reads typically start landing from the third call of a run onwards.

Set `ATR_DEBUG_LLM=1` to log per-request token counts including cache reads and
writes.

See [Configuration Guide](docs/configuration.md) for all options including CLI backends, Vertex AI authentication methods (ADC, service account, workload identity), and MCP server configuration.

## Requirements

- Go 1.25+ and Node 22+ with npm (for building from source)
- One of the following LLM backends:
  - **Claude CLI** or **Gemini CLI** (recommended - no API key needed)
  - Google Gemini API key
  - Google Cloud project with Vertex AI

### Linux runtime dependencies (for `atr computer`)

The desktop computer-use feature uses [robotgo](https://github.com/go-vgo/robotgo) which requires X11 development headers and a few system libraries. On Debian/Ubuntu install them with:

```bash
make install-deps-linux
# or, manually:
sudo apt-get install -y \
  libxtst-dev libxss-dev libpng-dev \
  libxkbcommon-dev libx11-dev xclip xsel
```

Optional GUI overlay packages:

- **`zenity`** — abortable progress dialog for the per-action countdown (recommended). Install with `sudo apt-get install -y zenity`.
- **`libnotify-bin`** — fallback when `zenity` is not present; provides `notify-send` for visual-only notifications. Install with `sudo apt-get install -y libnotify-bin`.

If neither is available the daemon falls back to terminal-only countdown — no functionality is lost, but Claude-Code/MCP users won't see a visible interrupt.

> **Note:** Linux support for `atr computer` is X11 only in v1. Wayland is tracked as future work.
>
> **Cross-platform builds:** robotgo and webview-style libraries depend on platform-native CGo, so cross-compiling from a single host is no longer supported. Each release artifact is built on its own platform's CI runner (`ubuntu-latest`, `macos-latest`, `windows-latest`). For local development, `make build` produces a binary for your current platform.

## Simple Testing

To test the browser functionality just using this command should give you some idea -

```bash
atr browser stop; atr browser start && atr browser navigate "https://optimizely.com" && atr browser screenshot --full --file && atr browser ask "what is the title of the page?" && atr browser stop && atr run --behavior=<PATH_TO_GOPATH>/src/github.com/imyousuf/agentic-test-runner/examples/behavior/simple.test.txt && echo "test passed"
```

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
