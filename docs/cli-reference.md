# CLI Reference

Complete reference for all ATR commands and flags.

## Global Flags

These flags are available for all commands:

| Flag | Description |
|------|-------------|
| `--config <path>` | Config file path (default: `~/.atr/config.yaml`) |
| `-v, --verbose` | Enable verbose output |
| `--backend <name>` | LLM backend: `claude-cli`, `gemini-cli`, `gemini-api`, or `vertex-ai` |
| `--api-key <key>` | Gemini API key (for `gemini-api` backend) |
| `--project <id>` | GCP project for Vertex AI |
| `--location <region>` | GCP region for Vertex AI |
| `--model <tier>` | Model: `flash`/`pro` (API backends) or `opus`/`sonnet`/`haiku` (claude-cli) |

---

## atr run

Execute commands with AI-powered failure analysis, or run browser behavior tests.

### Command Execution Mode

```bash
atr run --cmd "<command>" [flags]
```

Execute a shell command and analyze failures with AI.

#### Flags

| Flag | Description |
|------|-------------|
| `--cmd <command>` | Command to execute (required for this mode) |
| `--cwd <path>` | Working directory (default: current directory) |
| `--context <text>` | Additional context for the AI agent |
| `--python-venv <path>` | Path to Python virtual environment to activate |
| `--nvm-version <version>` | Node.js version to use via nvm (e.g., `18` or `18.17.0`) |
| `--no-auto-env` | Disable automatic environment detection |

#### Examples

```bash
# Run tests
atr run --cmd "go test ./..."

# Run in specific directory
atr run --cmd "npm test" --cwd "/path/to/project"

# With context
atr run --cmd "make build" --context "Building after refactoring the auth module"

# Use pro model for complex issues
atr run --cmd "pytest" --model pro

# With specific Python venv
atr run --cmd "pytest tests/" --python-venv /path/to/.venv

# With specific Node.js version
atr run --cmd "npm test" --nvm-version 18

# Disable auto environment detection
atr run --cmd "python script.py" --no-auto-env
```

### Behavior Testing Mode

```bash
atr run --behavior <path> [flags]
```

Run browser-based behavior tests using AI-driven automation.

#### Flags

| Flag | Description |
|------|-------------|
| `--behavior <path>` | Path to `.test.txt` file or directory |
| `--browser-url <url>` | Base URL for tests (overrides config) |
| `--headless` | Run browser headless (default: `true`) |
| `--viewport <WxH>` | Viewport size, e.g., `1920x1080` |
| `--cdp-endpoint <url>` | Connect to existing browser via CDP |

#### Examples

```bash
# Run single test
atr run --behavior tests/login.test.txt

# Run all tests in directory
atr run --behavior tests/e2e/

# With base URL
atr run --behavior tests/e2e/ --browser-url http://localhost:3000

# Non-headless for debugging
atr run --behavior tests/login.test.txt --headless=false

# Custom viewport
atr run --behavior tests/mobile.test.txt --viewport 375x667

# Connect to existing browser
atr run --behavior tests/debug.test.txt --cdp-endpoint ws://localhost:9222
```

---

## atr browser

Control a browser via HTTP server mode. See [Browser Server Mode](browser-server.md) for detailed documentation.

### Lifecycle Commands

```bash
atr browser start [--port PORT]   # Start browser daemon
atr browser stop                  # Stop browser daemon
atr browser status                # Check if running
```

### Navigation

```bash
atr browser navigate <url>        # Navigate to URL
atr browser back                  # Go back
atr browser forward               # Go forward
atr browser reload                # Reload page
```

### Page Management

```bash
atr browser new-page [url]        # Open new tab
atr browser list-pages            # List all tabs
atr browser select-page <index>   # Switch to tab
atr browser close-page <index>    # Close tab
```

### Interaction

```bash
atr browser click <target>        # Click element
atr browser fill <target> <value> # Type into input
atr browser hover <target>        # Hover over element
atr browser press-key <key>       # Press keyboard key
atr browser drag <from> <to>      # Drag element
```

### Inspection

```bash
atr browser snapshot [--verbose]  # Get page elements with UIDs
atr browser screenshot [--full]   # Capture screenshot
atr browser html                  # Get page HTML
atr browser url                   # Get current URL
atr browser title                 # Get page title
atr browser eval <script>         # Run JavaScript
```

### Debugging

```bash
atr browser console [--limit N]   # Get console messages
atr browser network [--limit N]   # Get network requests
atr browser errors                # Get failed requests
```

### Common Flags

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `--endpoint <url>` | Override server endpoint |

---

## atr mcp

Run ATR as an MCP (Model Context Protocol) server, exposing browser automation tools to MCP-compatible clients like Claude CLI or Gemini CLI.

### atr mcp serve

Start the MCP server for browser automation.

```bash
atr mcp serve [flags]
```

The server communicates via JSON-RPC 2.0 over stdio and exposes browser tools for navigation, interaction, and inspection.

#### Flags

| Flag | Description |
|------|-------------|
| `--headless` | Run browser in headless mode (default: `true`) |
| `--ignore-https-errors` | Ignore HTTPS certificate errors |

#### Available Tools

| Tool | Description |
|------|-------------|
| `browser_navigate` | Navigate to a URL |
| `browser_click` | Click on an element |
| `browser_fill` | Fill a form field |
| `browser_screenshot` | Take a screenshot |
| `browser_get_url` | Get current page URL |
| `browser_get_title` | Get page title |
| `browser_get_html` | Get page HTML content |
| `browser_snapshot` | Get accessibility tree snapshot |
| `browser_console` | Get console messages |
| `browser_network` | Get network requests |
| `browser_press_key` | Press a key |
| `browser_hover` | Hover over an element |
| `browser_go_back` | Navigate back |
| `browser_go_forward` | Navigate forward |
| `browser_reload` | Reload the page |

#### Integration Examples

**With Claude CLI:**

```bash
# Inline configuration
claude -p "Navigate to example.com" \
  --mcp-config '{"mcpServers":{"atr-browser":{"command":"atr","args":["mcp","serve"]}}}' \
  --allowedTools "mcp__atr-browser__*"
```

**With Gemini CLI (project settings `.gemini/settings.json`):**

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

See [MCP Server Documentation](mcp-server.md) for detailed usage.

---

## atr config

Manage ATR configuration.

### atr config show

Display current configuration.

```bash
atr config show
```

Shows merged configuration from file, environment, and defaults.

### atr config init

Create a default configuration file.

```bash
atr config init
```

Creates `~/.atr/config.yaml` with default values and comments.

**Auto-Detection**: Automatically detects installed CLI tools (Claude CLI, Gemini CLI) and sets the first available as the default backend.

If the file already exists, you'll be prompted to confirm overwrite.

### atr config validate

Validate configuration.

```bash
atr config validate
```

Checks that:
- Required fields are present
- Backend is valid (`claude-cli`, `gemini-cli`, `gemini-api`, or `vertex-ai`)
- API credentials are configured (for API backends)
- CLI tools are available in PATH (for CLI backends)
- Model tier is valid (for API backends)

---

## atr test

Test LLM connectivity.

```bash
atr test
```

Sends a simple prompt to the configured LLM backend and verifies a response is received. Use this to validate your API key or Vertex AI setup before running actual commands.

Output:
```
Testing LLM connectivity...
  Backend: vertex-ai
  Model: gemini-3-flash-preview

LLM Response: Hello from ATR!
Response time: 1.2s

LLM connectivity test passed!
```

---

## atr test-cmd-env

Test environment detection for a command without executing it.

```bash
atr test-cmd-env "<command>" [flags]
```

Analyzes a command to show which Python/Node.js environments would be activated.

### Flags

| Flag | Description |
|------|-------------|
| `--cwd <path>` | Working directory for environment detection |
| `--python-venv <path>` | Override Python venv path |
| `--nvm-version <version>` | Override Node.js version |
| `--no-auto-env` | Disable automatic environment detection |

### Examples

```bash
# Test what environments a command would use
atr test-cmd-env "pytest tests/"
atr test-cmd-env "npm run build"
atr test-cmd-env "make test"

# Test with specific working directory
atr test-cmd-env "pytest" --cwd /path/to/project

# Test with manual venv override
atr test-cmd-env "python script.py" --python-venv /path/to/.venv
```

### Output

```
Command: pytest tests/
Working directory: /path/to/project

Detection method: LLM
Analysis:
  needs_python: true
  needs_node: false
  reasoning: pytest is a standard Python testing framework.

Python environment:
  Status: FOUND (will be activated)
  Type: python-venv
  Path: /path/to/project/.venv
  Activate: source /path/to/project/.venv/bin/activate
Node.js environment:
  Status: NOT NEEDED

Final command would be:
  source /path/to/project/.venv/bin/activate && pytest tests/
```

---

## atr version

Display version information.

```bash
atr version
```

Output:
```
atr version dev-abc1234
```

---

## atr update

Check for and install updates.

```bash
atr update [flags]
```

Downloads and installs the latest version of ATR from GitHub releases. Dev versions auto-update every 2 days on startup (configurable).

### Flags

| Flag | Description |
|------|-------------|
| `--check` | Only check for updates, don't install |
| `--force` | Force update even if already on latest version |

### Examples

```bash
# Check for updates without installing
atr update --check

# Update to latest version
atr update

# Force update even if already up to date
atr update --force
```

### Auto-Update Configuration

Dev versions automatically check for updates on startup. Configure in `~/.atr/config.yaml`:

```yaml
update:
  auto_update_dev: true   # Enable auto-update for dev versions (default: true)
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success - command passed or no failures found |
| `1` | Failure - command failed or test failed |
| `2` | Configuration error |

---

## Shell Completion

ATR supports shell completion for bash and zsh.

### atr install-completion

Automatically install shell completion for your current shell.

```bash
atr install-completion
```

This command:
- Auto-detects your shell (bash or zsh)
- Installs completion to the appropriate location based on permissions:
  - **With sudo**: System-wide (`/etc/bash_completion.d/` or `/usr/local/share/zsh/site-functions/`)
  - **Without sudo**: User directory (`~/.bash_completion.d/` or `~/.zsh/completions/`)

After installation, follow the printed instructions to enable completions in your shell.

### Manual Installation

If you prefer manual setup:

#### Bash

```bash
# Add to ~/.bashrc
source <(atr completion bash)
```

#### Zsh

```bash
# Add to ~/.zshrc
source <(atr completion zsh)
```

#### Fish

```bash
atr completion fish | source
```

#### PowerShell

```powershell
atr completion powershell | Out-String | Invoke-Expression
```

---

## Common Workflows

### Debug a Failing Test

```bash
# Run tests with analysis
atr run --cmd "go test ./... -v" --context "Tests started failing after recent changes"
```

### Run E2E Tests

```bash
# Run all behavior tests
atr run --behavior tests/e2e/ --browser-url http://localhost:3000

# Run specific test with visible browser
atr run --behavior tests/e2e/checkout.test.txt --headless=false
```

### CI/CD Integration

```bash
# In CI pipeline
export GEMINI_API_KEY="${{ secrets.GEMINI_API_KEY }}"
atr run --cmd "npm test" || exit 1
```

### Use Different Models

```bash
# Quick analysis with flash model (default)
atr run --cmd "make build" --model flash

# Deep analysis with pro model
atr run --cmd "make build" --model pro
```

---

## Environment Variables

See [Configuration](configuration.md#environment-variables) for the complete list.

Quick reference:
```bash
export GEMINI_API_KEY="your-key"           # Gemini API
export GOOGLE_CLOUD_PROJECT="project-id"   # Vertex AI
export ATR_MODEL="pro"                     # Use pro model
```
