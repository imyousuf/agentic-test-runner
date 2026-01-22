---
name: atr-analyze
description: Run tests with AI-powered analysis and clean summarized output. Use this as the DEFAULT way to run test suites (pytest, jest, go test, make test, npm test, etc.) to keep conversation context clean. ATR analyzes the full output and returns a concise summary of results, highlighting any failures with actionable insights. Also useful for analyzing build failures and debugging command errors.
---

# ATR Command Analysis Skill

This skill runs commands through ATR (Agentic Test Runner) which provides AI-powered analysis and **clean summarized output**. Use this as your **default way to run test suites** - the AI analyzes the full output and returns a concise summary, keeping your conversation context clean and focused.

## When to Use This Skill vs Direct Commands

**Use ATR analyze (RECOMMENDED DEFAULT) when:**
- Running test suites (pytest, jest, go test, npm test, make test)
- Running builds that produce verbose output
- You want clean, summarized results instead of raw output
- You want automatic failure analysis if something goes wrong

**Use direct Bash commands only when:**
- Running quick one-off commands with minimal output
- You specifically need the raw, unprocessed output
- Interactive commands that require user input

## Basic Usage

```bash
atr run --cmd "<command>"
```

Examples:
```bash
atr run --cmd "go test ./..."
atr run --cmd "npm test"
atr run --cmd "pytest tests/"
atr run --cmd "make build"
```

## Adding Context

Provide context to help the AI agent focus its analysis:

```bash
atr run --cmd "<command>" --context "<context>"
```

Examples:
```bash
atr run --cmd "go test ./..." --context "Tests started failing after refactoring the auth module"
atr run --cmd "npm run build" --context "Added new dependency yesterday"
atr run --cmd "pytest" --context "Testing the new payment integration"
```

## Command Options

| Flag | Description |
|------|-------------|
| `--cmd <command>` | Command to execute (required) |
| `--cwd <path>` | Working directory |
| `--context <text>` | Additional context for AI agent |
| `--model flash\|pro` | Model tier (flash=fast, pro=deep analysis) |
| `--python-venv <path>` | Python virtual environment path |
| `--nvm-version <version>` | Node.js version via nvm |
| `--no-auto-env` | Disable automatic environment detection |

## Working Directory

Specify where to run the command:

```bash
atr run --cmd "npm test" --cwd "/path/to/project"
```

## Environment Detection

ATR automatically detects and activates appropriate environments:

**Python projects:**
- Detects `.venv`, `venv`, or Poetry environments
- Auto-activates virtual environment

**Node.js projects:**
- Detects `.nvmrc` or `package.json` engine requirements
- Auto-activates correct Node.js version via nvm

Override automatic detection:
```bash
atr run --cmd "pytest" --python-venv /custom/path/.venv
atr run --cmd "npm test" --nvm-version 18
atr run --cmd "make" --no-auto-env
```

## Model Selection

Use different models for different needs:

```bash
# Quick analysis (default)
atr run --cmd "make build" --model flash

# Deep analysis for complex issues
atr run --cmd "go test ./..." --model pro
```

## What the AI Agent Does

The ATR agent processes command output and:

1. **Summarizes** results into a clean, concise report
2. **Identifies** test pass/fail status and key metrics
3. **Analyzes** any failures for error patterns and root causes
4. **Reads** relevant source files when failures occur
5. **Provides** actionable recommendations for fixing issues

This keeps your conversation context clean by replacing verbose test output with a focused summary.

## Example Output

```
Executing: go test ./...
Directory: /path/to/project

--- FAIL: TestUserAuth (0.05s)
    auth_test.go:42: expected 200, got 401

Command failed (exit code: 1)

Analyzing failure with AI agent...

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

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Command passed |
| 1 | Command failed (analysis provided) |
| 2 | Configuration error |

## Configuration

Configure ATR in `~/.atr/config.yaml`:

```yaml
backend: gemini-api  # or vertex-ai
model: flash         # or pro

gemini:
  api_key: "your-key"

# Or for Vertex AI:
vertex:
  project: your-project
  location: us-central1
```

Environment variables:
```bash
export GEMINI_API_KEY="your-key"
# Or
export GOOGLE_CLOUD_PROJECT="project-id"
```

## Best Practices

1. **Use as default** for running test suites to keep conversation context clean
2. **Provide context** when the failure might be related to recent changes
3. **Use --model pro** for complex, multi-file issues
4. **Specify --cwd** when running from a different directory
5. **Check environment** with `atr test-cmd-env "<command>"` to preview detection
