# Browser Server Mode

ATR provides an HTTP server mode that enables programmatic browser control via CLI commands. This is designed for AI agents like Claude Code to interact with a browser through command-line calls instead of maintaining persistent MCP connections.

## Overview

The browser server architecture:

```
┌─────────────┐   CLI calls    ┌─────────────┐   HTTP    ┌─────────────┐
│   Claude    │ ─────────────▶ │   atr CLI   │ ────────▶ │ ATR Server  │
│   Code      │ ◀───────────── │  (client)   │ ◀──────── │ + Browser   │
└─────────────┘   stdout       └─────────────┘   JSON    └─────────────┘
```

Key features:
- **Daemon mode**: Browser runs as a background process
- **State persistence**: State file at `~/.atr/browser.state` tracks endpoint
- **Human-readable output**: Default output for humans, `--json` for structured data
- **Full browser control**: Navigate, click, type, screenshot, inspect elements

## Quick Start

```bash
# Start the browser daemon
atr browser start

# Navigate to a page
atr browser navigate https://example.com

# Get a snapshot of clickable elements
atr browser snapshot

# Click an element
atr browser click "More information..."

# Take a screenshot
atr browser screenshot

# Stop when done
atr browser stop
```

## Server Lifecycle

### Start Browser

```bash
atr browser start [--port PORT]
```

Starts the browser as a daemon process. Output includes:
- Server endpoint URL
- Process ID (PID)
- State file location

The browser always runs in visible (non-headless) mode for debugging.

**Flags:**
| Flag | Description |
|------|-------------|
| `--port <port>` | HTTP server port (default: 9333) |

### Stop Browser

```bash
atr browser stop
```

Gracefully shuts down the browser daemon and removes the state file.

### Check Status

```bash
atr browser status
```

Shows whether the browser is running and its endpoint.

## Navigation Commands

### Navigate to URL

```bash
atr browser navigate <url>
```

Navigate to the specified URL. Returns page title and load time.

### Back/Forward/Reload

```bash
atr browser back       # Go back in history
atr browser forward    # Go forward in history
atr browser reload     # Reload current page
```

## Page Management

### Open New Tab

```bash
atr browser new-page [url]
```

Opens a new tab, optionally navigating to a URL.

### List Tabs

```bash
atr browser list-pages
```

Lists all open tabs with their URLs and titles.

### Switch Tab

```bash
atr browser select-page <index>
```

Switches to the tab at the specified index (0-based).

### Close Tab

```bash
atr browser close-page <index>
```

Closes the tab at the specified index.

## Interaction Commands

### Click

```bash
atr browser click <target>
```

Click an element. The target can be:
- Text content: `"Sign In"`
- UID from snapshot: `e5`
- CSS selector: `.submit-button`

### Fill Input

```bash
atr browser fill <target> <value>
```

Type text into an input field.

```bash
atr browser fill "Email" "user@example.com"
atr browser fill "#password" "secret123"
```

### Hover

```bash
atr browser hover <target>
```

Hover over an element (for dropdowns, tooltips).

### Press Key

```bash
atr browser press-key <key>
```

Press a keyboard key. Supports:
- Named keys: `Enter`, `Tab`, `Escape`, `Backspace`
- Modifiers: `Control+a`, `Shift+Tab`
- Arrow keys: `ArrowUp`, `ArrowDown`, `ArrowLeft`, `ArrowRight`

### Drag and Drop

```bash
atr browser drag <from> <to>
```

Drag from one element to another.

## Inspection Commands

### Snapshot

```bash
atr browser snapshot [--verbose]
```

Get a structured view of interactive elements on the page. Returns elements with unique IDs (UIDs) that can be used as click targets.

```bash
# Compact view
atr browser snapshot

# Detailed view with coordinates
atr browser snapshot --verbose --json
```

### Screenshot

```bash
atr browser screenshot [--full] [--output PATH]
```

Capture a screenshot. Returns the path to the saved image.

| Flag | Description |
|------|-------------|
| `--full` | Capture full scrollable page |
| `--output <path>` | Custom output path |

### Get HTML

```bash
atr browser html
```

Get the current page HTML.

### Get URL

```bash
atr browser url
```

Get the current page URL.

### Get Title

```bash
atr browser title
```

Get the current page title.

### Evaluate JavaScript

```bash
atr browser eval <script>
```

Execute JavaScript in the page context and return the result.

```bash
atr browser eval "document.querySelectorAll('a').length"
atr browser eval "window.localStorage.getItem('token')"
```

## Debugging Commands

### Console Logs

```bash
atr browser console [--limit N]
```

Get console messages (log, warn, error).

### Network Requests

```bash
atr browser network [--limit N]
```

Get network requests with status, method, and timing.

### Failed Requests

```bash
atr browser errors
```

Get failed network requests (4xx, 5xx, or network errors).

## Common Flags

These flags work with most commands:

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `--endpoint <url>` | Override server endpoint |

## State File

The browser state is stored at `~/.atr/browser.state`:

```json
{
  "pid": 12345,
  "endpoint": "http://localhost:9333",
  "started_at": "2024-01-05T10:30:00Z"
}
```

This allows CLI commands to discover the running server automatically.

## Configuration

Server settings can be configured in `~/.atr/config.yaml`:

```yaml
server:
  port: 9333           # Default HTTP port
  read_timeout: 30s    # HTTP read timeout
  write_timeout: 30s   # HTTP write timeout
```

## HTTP API Reference

The server exposes a REST API at `http://localhost:<port>/api/v1`:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/navigate` | POST | Navigate to URL |
| `/back` | POST | Go back |
| `/forward` | POST | Go forward |
| `/reload` | POST | Reload page |
| `/pages` | GET | List pages |
| `/pages` | POST | Create new page |
| `/pages/{index}` | PUT | Select page |
| `/pages/{index}` | DELETE | Close page |
| `/click` | POST | Click element |
| `/fill` | POST | Fill input |
| `/hover` | POST | Hover element |
| `/press-key` | POST | Press key |
| `/drag` | POST | Drag element |
| `/snapshot` | GET | Get element tree |
| `/screenshot` | GET | Capture screenshot |
| `/html` | GET | Get page HTML |
| `/url` | GET | Get current URL |
| `/title` | GET | Get page title |
| `/eval` | POST | Execute JavaScript |
| `/console` | GET | Get console logs |
| `/network` | GET | Get network requests |
| `/errors` | GET | Get failed requests |
| `/shutdown` | POST | Graceful shutdown |

## Example: AI Agent Workflow

Here's how an AI agent like Claude Code might interact with a web page:

```bash
# 1. Start browser session
$ atr browser start
Browser started
  Endpoint: http://localhost:9333
  PID: 12345

# 2. Navigate to target page
$ atr browser navigate https://example.com/login
Navigated to https://example.com/login
  Title: Login - Example App
  Load time: 1.2s

# 3. Get interactive elements
$ atr browser snapshot --json
{
  "elements": [
    {"uid": "e0", "tag": "input", "name": "email", "placeholder": "Email"},
    {"uid": "e1", "tag": "input", "name": "password", "type": "password"},
    {"uid": "e2", "tag": "button", "text": "Sign In"}
  ]
}

# 4. Fill form
$ atr browser fill "Email" "user@example.com"
Filled: Email with value

$ atr browser fill "e1" "password123"
Filled: e1 with value

# 5. Submit
$ atr browser click "Sign In"
Clicked: Sign In
  New URL: https://example.com/dashboard

# 6. Verify state
$ atr browser url
https://example.com/dashboard

# 7. Take screenshot for verification
$ atr browser screenshot
Screenshot saved: /tmp/atr-screenshot-20240105-103045.png

# 8. Cleanup
$ atr browser stop
Browser stopped
```

## Troubleshooting

### Browser won't start

```bash
# Check if already running
atr browser status

# Force stop if state is stale
rm ~/.atr/browser.state
atr browser start
```

### Commands timeout

The server has 30-second timeouts. For slow pages:
1. Use `--endpoint` to connect to a custom server
2. Increase timeouts in config

### Can't find elements

Use `atr browser snapshot --verbose` to see all elements with their UIDs, then target by UID.

### Port already in use

```bash
# Specify a different port
atr browser start --port 9334
```
