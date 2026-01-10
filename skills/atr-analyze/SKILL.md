---
name: atr-analyze
description: Analyze test failures, debug failing tests, run tests with AI analysis, understand why tests fail, get help with build failures, analyze command output, debug command errors, or get AI-powered analysis of shell command failures using ATR.
---

# ATR Command Analysis Skill

This skill provides AI-powered analysis of command failures using ATR (Agentic Test Runner). When a command fails, ATR's AI agent automatically investigates the failure by reading files, running diagnostic commands, and providing actionable recommendations.

## When to Use This Skill vs Direct Commands

**Use ATR analyze when:**
- A command is failing and the reason is unclear
- Test failures need deep investigation
- Build errors require understanding project context
- Multiple files or dependencies may be involved

**Use direct commands when:**
- The command is expected to succeed
- Simple one-off execution is needed
- No failure analysis is required

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

When a command fails, the ATR agent:

1. **Analyzes** the failure output for error patterns
2. **Reads** relevant source files mentioned in errors
3. **Searches** the codebase for related code
4. **Runs** diagnostic commands for more context
5. **Provides** root cause analysis and recommendations

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

1. **Provide context** when the failure might be related to recent changes
2. **Use --model pro** for complex, multi-file issues
3. **Specify --cwd** when running from a different directory
4. **Check environment** with `atr test-cmd-env "<command>"` to preview detection
