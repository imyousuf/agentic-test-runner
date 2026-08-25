# Getting Started

This guide will help you get ATR up and running quickly.

## Prerequisites

- **Go 1.23+** (for building from source)
- **LLM Backend** - one of:
  - **Claude CLI** or **Gemini CLI** (recommended - no API key needed), OR
  - Google Gemini API key, OR
  - Google Cloud project with Vertex AI enabled

## Quick Install

### Option 1: Install Script (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/imyousuf/agentic-test-runner/main/install.sh | sh
```

### Option 2: Download Binary

Download the latest release from [GitHub Releases](https://github.com/imyousuf/agentic-test-runner/releases):

```bash
# Linux (amd64)
curl -L https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-linux-amd64.tar.gz | tar xz
sudo mv atr /usr/local/bin/

# macOS (arm64)
curl -L https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-darwin-arm64.tar.gz | tar xz
sudo mv atr /usr/local/bin/
```

### Verify Installation

```bash
atr version
```

## Configuration

### Using CLI Backends (Recommended - No API Key)

If you have Claude CLI or Gemini CLI installed, ATR can use them directly:

```bash
# Initialize config - auto-detects installed CLIs
atr config init

# Output: Detected CLI: claude-cli (2.1.3)
#         Using 'claude-cli' as default backend.
```

That's it! ATR will use the detected CLI for all LLM operations.

### Using Gemini API

1. Get an API key from [Google AI Studio](https://aistudio.google.com/apikey)

2. Set the environment variable:
```bash
export GEMINI_API_KEY="your-api-key"
```

### Using Vertex AI

1. Authenticate with Google Cloud:
```bash
gcloud auth application-default login
```

2. Set your project:
```bash
export GOOGLE_CLOUD_PROJECT="your-project-id"
```

3. Configure ATR to use Vertex AI:
```bash
atr config init
# Edit ~/.atr/config.yaml and set backend: vertex-ai
```

See [Configuration Guide](configuration.md) for detailed setup options.

## Your First Command Analysis

Run a failing command and let ATR analyze it:

```bash
# Example: Run tests that might fail
atr run --cmd "go test ./..."

# With additional context
atr run --cmd "npm test" --context "Testing the auth module"
```

When the command fails, ATR will:
1. Capture the output and exit code
2. Engage the AI agent to analyze the failure
3. Run diagnostic commands automatically
4. Provide a summary with recommendations

### Example Output

```
Executing: go test ./...
Directory: /path/to/project

--- FAIL: TestLogin (0.05s)
    login_test.go:42: expected status 200, got 401

✗ Command failed (exit code: 1, duration: 2.3s)

Analyzing failure with AI agent...
Using model: gemini-2.0-flash-exp (gemini-api)

======================================================================
ANALYSIS RESULTS
======================================================================

Status: FAILURE

Summary:
  The TestLogin test is failing because the authentication middleware
  expects a valid JWT token, but the test is not providing one.

Root Cause:
  The test setup in login_test.go:38 creates a request without the
  Authorization header that the auth middleware requires.

Recommendations:
  1. Add a mock JWT token to the test request headers
  2. Or configure the test to bypass auth middleware
  3. Check if the auth middleware was recently added

Files Examined:
  - login_test.go (test file)
  - middleware/auth.go (auth middleware)
  - handlers/login.go (login handler)
```

## Your First Behavior Test

ATR can also run browser-based behavior tests using AI-driven automation.

### Create a Test File

Create `tests/search.test.txt`:

```
Test: Search on Google

Steps:
1. Navigate to https://www.google.com
2. Type "agentic test runner" in the search box
3. Press Enter
4. Wait for results to load

Expected Results:
- Search results are displayed
- No console errors
```

### Run the Test

```bash
atr run --behavior tests/search.test.txt
```

The AI agent will:
1. Launch a browser (Chromium, auto-downloaded)
2. Interpret your natural language test steps
3. Execute each step using browser automation
4. Report success or failure with screenshots

### Example Output

```
Found 1 behavior test(s)

Launching browser...
Using model: gemini-2.0-flash-exp (gemini-api)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Test 1/1: search.test.txt
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

======================================================================
ANALYSIS RESULTS
======================================================================

Status: SUCCESS

Summary:
  The test successfully navigated to Google, performed a search for
  "agentic test runner", and verified that search results were displayed.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Summary: 1/1 tests passed
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

## Next Steps

- [Installation](installation.md) - More installation options
- [Configuration](configuration.md) - Detailed configuration reference
- [CLI Reference](cli-reference.md) - All commands and flags
- [Behavior Testing](behavior-testing.md) - Write browser tests
- [Architecture](architecture.md) - How ATR works
