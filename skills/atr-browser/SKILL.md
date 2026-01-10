---
name: atr-browser
description: Control browser, automate browser interactions, navigate to URLs, click on webpages, fill forms, take screenshots, inspect webpages, web scraping with browser, test websites manually, or interact with web pages programmatically using ATR browser server mode.
allowed-tools: Bash(atr browser:*)
---

# ATR Browser Automation Skill

This skill provides browser automation capabilities through ATR's browser server mode. The browser server runs as a daemon process and accepts CLI commands for browser control.

## Architecture

```
Claude Code --> atr CLI (client) --> ATR Server + Browser
```

The browser runs in visible (non-headless) mode by default for debugging and verification.

## Getting Started

### Step 1: Check Browser Status and Start if Needed

Before any browser operations, verify the browser server is running:

```bash
atr browser status
```

If the server is not running, start it:

```bash
atr browser start
```

The server stores state at `~/.atr/browser.state` which allows subsequent commands to discover the endpoint automatically.

### Step 2: Navigate and Interact

Once running, use navigation and interaction commands to control the browser.

## Command Categories

### Lifecycle Commands

| Command | Description |
|---------|-------------|
| `atr browser start [--port PORT]` | Start browser daemon (default port: 9333) |
| `atr browser stop` | Stop browser daemon |
| `atr browser status` | Check if browser is running |

### Navigation Commands

| Command | Description |
|---------|-------------|
| `atr browser navigate <url>` | Navigate to URL |
| `atr browser back` | Go back in history |
| `atr browser forward` | Go forward in history |
| `atr browser reload` | Reload current page |

### Page Management Commands

| Command | Description |
|---------|-------------|
| `atr browser new-page [url]` | Open new tab |
| `atr browser list-pages` | List all tabs |
| `atr browser select-page <index>` | Switch to tab (0-based) |
| `atr browser close-page <index>` | Close tab |

### Interaction Commands

| Command | Description |
|---------|-------------|
| `atr browser click <target> [--double]` | Click element (use --double for double-click) |
| `atr browser fill <target> <value>` | Type into input field |
| `atr browser hover <target>` | Hover over element |
| `atr browser press-key <key>` | Press keyboard key (e.g., Enter, Tab, Control+A) |
| `atr browser drag <from> <to>` | Drag element |

### Inspection Commands

| Command | Description |
|---------|-------------|
| `atr browser snapshot [--verbose]` | Get page elements with UIDs |
| `atr browser screenshot --file [--full]` | Capture screenshot (saves to /tmp/) |
| `atr browser html` | Get page HTML |
| `atr browser url` | Get current URL |
| `atr browser title` | Get page title |
| `atr browser eval <script>` | Execute JavaScript |

**Screenshot Note:** Use `--file` to save screenshots to `/tmp/` with a timestamped filename (e.g., `/tmp/atr-screenshot-20240105-103045.png`). No need to specify a file path. Add `--full` for full-page screenshots. Without `--file`, returns base64-encoded image data.

### Debugging Commands

| Command | Description |
|---------|-------------|
| `atr browser console [--limit N]` | Get console messages (default: 50) |
| `atr browser network [--limit N]` | Get network requests (default: 50) |
| `atr browser errors` | Get failed requests |

## Workflow Pattern

Follow this workflow for browser automation tasks:

1. **Ensure Server Running**
   ```bash
   atr browser status || atr browser start
   ```

2. **Navigate to Target**
   ```bash
   atr browser navigate https://example.com
   ```

3. **Inspect Page Elements**
   ```bash
   atr browser snapshot
   ```
   This returns elements with unique IDs (UIDs) like `e0`, `e1`, etc.

4. **Interact with Elements**
   Target elements by:
   - Text content: `"Sign In"`
   - UID from snapshot: `e5`
   - CSS selector: `.submit-button`

5. **Verify Results**
   ```bash
   atr browser url
   atr browser title
   atr browser screenshot --file
   ```

6. **Cleanup When Done**
   ```bash
   atr browser stop
   ```

## Using JSON Output

Add `--json` flag for structured output when parsing is needed:

```bash
atr browser snapshot --json
atr browser list-pages --json
atr browser network --json
```

## Element Targeting

The `<target>` parameter in click, fill, hover, and drag commands accepts:

1. **Element UID**: `e0`, `e5` (from snapshot output)
2. **Visible text**: `"Sign In"`, `"Submit Form"`
3. **aria-label**: Elements with matching aria-label attribute
4. **data-testid**: Elements with matching data-testid attribute
5. **CSS selector**: `#login-button`, `.nav-link`

Best practice: Use `atr browser snapshot` first to see available elements and their UIDs.

## Keyboard Keys

For `press-key` command:
- Named keys: `Enter`, `Tab`, `Escape`, `Backspace`
- Modifiers: `Control+a`, `Shift+Tab`, `Alt+Enter`
- Arrow keys: `ArrowUp`, `ArrowDown`, `ArrowLeft`, `ArrowRight`

## Troubleshooting

**Browser won't start:**
```bash
atr browser status
rm ~/.atr/browser.state  # If stale state
atr browser start
```

**Port already in use:**
```bash
atr browser start --port 9334
```

**Element not found:**
```bash
atr browser snapshot --verbose --json
```

## Additional Resources

For complete command reference with all flags, see `references/commands-reference.md`.
