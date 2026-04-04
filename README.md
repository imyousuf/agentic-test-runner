# ATR - Agentic Test Runner

[![Go Report Card](https://goreportcard.com/badge/github.com/imyousuf/agentic-test-runner)](https://goreportcard.com/report/github.com/imyousuf/agentic-test-runner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**ATR** is an AI-powered test runner that automatically analyzes failures and runs browser-based behavior tests using natural language.

## Features

- **AI Failure Analysis**: Run any command and get intelligent analysis when it fails
- **Browser Behavior Testing**: Write tests in natural language, let AI execute them
- **Multiple LLM Backends**: Supports Google Gemini API, Vertex AI, and CLI tools (Claude, Gemini)
- **CLI Backend Support**: Use Claude CLI or Gemini CLI as backends - no API keys needed
- **MCP Server**: Expose browser tools to any MCP-compatible client
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

**From source:**
```bash
go install github.com/imyousuf/agentic-test-runner/cmd/atr@latest
```

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
Using model: gemini-2.0-flash-exp (gemini-api)

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
- **[Browser Server](docs/browser-server.md)** - HTTP server for programmatic browser control
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
| **atr-browser** | Control browser via ATR server (navigate, click, fill, screenshot) |
| **atr-analyze** | Run tests with AI analysis (default for test suites - keeps context clean) |
| **atr-behavior** | Run natural language browser tests |

### Usage Examples

Once installed, Claude Code automatically uses these skills when relevant:

- "Navigate to google.com and take a screenshot"
- "Run the pytest tests" (uses atr-analyze for clean output)
- "Run the behavior tests in tests/e2e/"

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

See [MCP Server Documentation](docs/mcp-server.md) for the full list of available browser tools.

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
```

See [Configuration Guide](docs/configuration.md) for all options including CLI backends, Vertex AI authentication methods (ADC, service account, workload identity), and MCP server configuration.

## Requirements

- Go 1.23+ (for building from source)
- One of the following LLM backends:
  - **Claude CLI** or **Gemini CLI** (recommended - no API key needed)
  - Google Gemini API key
  - Google Cloud project with Vertex AI

## Simple Testing

To test the browser functionality just using this command should give you some idea -

```bash
atr browser stop; atr browser start && atr browser navigate "https://optimizely.com" && atr browser screenshot --full --file && atr browser ask "what is the title of the page?" && atr browser stop && atr run --behavior=<PATH_TO_GOPATH>/src/github.com/imyousuf/agentic-test-runner/examples/behavior/simple.test.txt && echo "test passed"
```

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
