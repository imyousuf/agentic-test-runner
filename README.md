# ATR - Agentic Test Runner

[![CI](https://github.com/imyousuf/agentic-test-runner/actions/workflows/ci.yml/badge.svg)](https://github.com/imyousuf/agentic-test-runner/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/imyousuf/agentic-test-runner)](https://goreportcard.com/report/github.com/imyousuf/agentic-test-runner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**ATR** is an AI-powered test runner that automatically analyzes failures and runs browser-based behavior tests using natural language.

## Features

- **AI Failure Analysis**: Run any command and get intelligent analysis when it fails
- **Browser Behavior Testing**: Write tests in natural language, let AI execute them
- **Multiple LLM Backends**: Supports Google Gemini API and Vertex AI
- **Cross-Platform**: Works on Linux, macOS, and Windows
- **Extensible**: Tool-based architecture for custom extensions

## Quick Start

### Install

```bash
go install github.com/imyousuf/agentic-test-runner/cmd/atr@latest
```

Or download from [Releases](https://github.com/imyousuf/agentic-test-runner/releases).

### Configure

```bash
# Using Gemini API (quickest)
export GEMINI_API_KEY="your-api-key"

# Or using Vertex AI
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
- **[Architecture](docs/architecture.md)** - How ATR works internally

## Configuration

ATR can be configured via `~/.atr/config.yaml`:

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

See [Configuration Guide](docs/configuration.md) for all options including Vertex AI authentication methods (ADC, service account, workload identity).

## Requirements

- Go 1.23+ (for building from source)
- Google Gemini API key or Google Cloud project with Vertex AI

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
