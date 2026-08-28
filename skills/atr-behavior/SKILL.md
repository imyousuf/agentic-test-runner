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
| `--headless` | Run browser headless | false |
| `--viewport <WxH>` | Viewport size | 1920x1080 |
| `--cdp-endpoint <url>` | Connect to existing browser | - |
| `--no-compile` | Replay only; never call the model. Fails loudly if the script is missing or stale | false |
| `--recompile` | Regenerate the script even if it matches the spec | false |
| `--no-repair` | Diagnose a drifted script but do not rewrite it | false |
| `--prune-values` | Remove inputs the script no longer reads | false |
| `--interpret` | Skip compilation and let the agent drive every step | false |

## Specs compile, and then replay without a model

A spec compiles once to a sibling `.js` file and afterwards replays with no
model in the loop — seconds, and no tokens. The agent returns only to diagnose
a failure.

```bash
atr run --behavior tests/login.test.txt              # compiles if needed, then replays
atr run --behavior tests/login.test.txt --no-compile # replay only; for CI
atr run --behavior tests/login.test.txt --recompile  # force a fresh compile
```

**Use `--no-compile` whenever you want certainty about cost.** It never calls
the model: it replays, or it fails and says why. Without it, a spec edit or an
unverified script triggers a compile, which drives the whole application and
takes minutes.

The compiled script is committed and carries a hash of the spec. Edit the spec
and the next run recompiles; reflow whitespace and it does not. A script that
has never completed a run is marked `// atr-unverified` and is recompiled
rather than trusted.

Test inputs live beside the spec in `login.test.properties` (committed), which
you may edit; `login.test.override.properties` (gitignored) wins over it, and
`ATR_VALUE_*` environment variables win over both.

A compile drives the spec **more than once** — once to learn the application,
then again to verify what it wrote. A destructive spec needs a fixture it can
rebuild, which is what `atr.setup("description", () => { ... })` is for: it runs
before the steps on every execution, is not counted as a step, and a failure in
it is reported as the fixture failing rather than the application misbehaving.

See `docs/behavior-compilation.md` for the full picture.

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
