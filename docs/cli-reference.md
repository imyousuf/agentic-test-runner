# CLI Reference

Complete reference for all ATR commands and flags.

## Global Flags

These flags are available for all commands:

| Flag | Description |
|------|-------------|
| `--config <path>` | Config file path (default: `~/.atr/config.yaml`) |
| `-v, --verbose` | Enable verbose output |
| `--backend <name>` | LLM backend: `gemini-api` or `vertex-ai` |
| `--api-key <key>` | Gemini API key |
| `--project <id>` | GCP project for Vertex AI |
| `--location <region>` | GCP region for Vertex AI |
| `--model <tier>` | Model tier: `flash` or `pro` |

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

If the file already exists, you'll be prompted to confirm overwrite.

### atr config validate

Validate configuration.

```bash
atr config validate
```

Checks that:
- Required fields are present
- Backend is valid
- API credentials are configured
- Model tier is valid

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

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success - command passed or no failures found |
| `1` | Failure - command failed or test failed |
| `2` | Configuration error |

---

## Shell Completion

ATR uses Cobra, which supports shell completion.

### Bash

```bash
# Add to ~/.bashrc
source <(atr completion bash)
```

### Zsh

```bash
# Add to ~/.zshrc
source <(atr completion zsh)
```

### Fish

```bash
atr completion fish | source
```

### PowerShell

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
