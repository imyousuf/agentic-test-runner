---
name: atr-behavior
description: Run behavior tests, run browser tests, execute .test.txt files, run e2e tests with ATR, natural language browser testing, AI browser tests, behavior-driven browser testing, or run browser-based behavior tests using ATR with natural language test specifications.
---

# ATR Behavior Testing Skill

This skill enables running browser-based behavior tests using ATR's AI-driven automation. Write tests in natural language in `.test.txt` files and let the AI execute them.

## Overview

Unlike traditional browser testing that requires precise selectors and step-by-step code, ATR uses an AI agent to:
1. Read natural language test specifications
2. Interpret what actions to perform
3. Execute using browser automation
4. Analyze failures and provide recommendations

## Basic Usage

### Run Single Test
```bash
atr run --behavior tests/login.test.txt
```

### Run Directory of Tests
```bash
atr run --behavior tests/e2e/
```

All `.test.txt` files in the directory are executed.

### With Base URL
```bash
atr run --behavior tests/e2e/ --browser-url http://localhost:3000
```

The base URL is used for relative navigation paths.

## Command Options

| Flag | Description | Default |
|------|-------------|---------|
| `--behavior <path>` | Test file or directory | (required) |
| `--browser-url <url>` | Base URL for tests | from config |
| `--headless` | Run browser headless | true |
| `--viewport <WxH>` | Viewport size | 1920x1080 |
| `--cdp-endpoint <url>` | Connect to existing browser | - |

## Test File Format

Test files use `.test.txt` extension with natural language:

```
Test: <test name>

Prerequisites:
- <prerequisite 1>
- <prerequisite 2>

Steps:
1. <step 1>
2. <step 2>
3. <step 3>

Expected Results:
- <expected result 1>
- <expected result 2>
```

### Example: Login Test

```
Test: User can log in with valid credentials

Prerequisites:
- Application running at http://localhost:3000
- Test user exists: test@example.com / password123

Steps:
1. Navigate to /login
2. Enter "test@example.com" in the email field
3. Enter "password123" in the password field
4. Click the "Sign In" button
5. Wait for the dashboard to load

Expected Results:
- URL contains /dashboard
- Welcome message is visible
- No console errors
```

## Running Tests

### Non-Headless Mode (for debugging)
```bash
atr run --behavior tests/login.test.txt --headless=false
```

### Mobile Viewport
```bash
atr run --behavior tests/mobile.test.txt --viewport 375x667
```

### Connect to Existing Browser

1. Launch Chrome with remote debugging:
   ```bash
   google-chrome --remote-debugging-port=9222
   ```

2. Connect ATR:
   ```bash
   atr run --behavior tests/debug.test.txt --cdp-endpoint ws://localhost:9222
   ```

## How It Works

The AI agent has access to these browser tools:

**Navigation:** navigate, back, forward, reload, new-page, select-page, close-page, wait-for

**Input:** click, fill, hover, press-key, drag, upload-file, handle-dialog

**Inspection:** snapshot, screenshot, evaluate JavaScript, console logs, network requests

## Element Resolution

The AI finds elements using multiple strategies:
1. Accessible name: `[aria-label="Sign In"]`
2. Test ID: `[data-testid="submit-btn"]`
3. Name attribute: `[name="email"]`
4. Placeholder: `[placeholder="Enter email"]`
5. Text content: Element containing "Sign In"
6. CSS selector: `#submit`

**Best Practice:** Use `aria-label` or `data-testid` for reliable element targeting.

## Failure Analysis

When tests fail, ATR captures:
- Screenshot of current state
- Console logs
- Network requests
- DOM snapshot

The AI provides root cause analysis and recommendations.

## Integration with atr-browser

For manual browser exploration before writing tests:

1. Start browser server: `atr browser start`
2. Navigate and explore: `atr browser navigate <url>`
3. Inspect elements: `atr browser snapshot`
4. Write test file based on exploration
5. Run test: `atr run --behavior test.test.txt`
6. Stop browser: `atr browser stop`

## Best Practices

1. **Clear descriptions:** "Click the 'Add to Cart' button in the product details section"
2. **Add waits:** "Wait for 'Loading...' to disappear"
3. **Use test IDs:** Reference `data-testid` attributes when available
4. **One flow per test:** Keep tests focused on single user journeys
5. **Document prerequisites:** Clearly state required application state

## Configuration

Configure in `~/.atr/config.yaml`:

```yaml
behavior:
  base_url: "http://localhost:3000"

  browser:
    executable: "auto"
    headless: true
    viewport:
      width: 1920
      height: 1080
    page_timeout: "30s"
    action_timeout: "10s"
```

## Additional Resources

For detailed test file format and examples, see `references/test-file-format.md`.
