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
atr browser start [flags]
```

Starts the browser as a daemon process. Output includes:
- Server endpoint URL
- Process ID (PID)
- State file location

By default the browser runs in visible (non-headless) mode for debugging.

**Flags:**
| Flag | Description |
|------|-------------|
| `--port <port>` | HTTP server port (default: 9333) |
| `--headless` | Run browser in headless mode (no visible window) |
| `--persist-session` | Keep cookies/sessions after browser closes |
| `--data-dir <path>` | Directory for browser data (default: `~/.atr/browser-data`) |
| `--sandbox` | Enable Chrome sandbox (disabled by default for Ubuntu 23.10+ compatibility) |
| `--system-chrome` | Use system-installed Google Chrome (falls back to bundled browser if not found) |

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
atr browser click <target> [--double]
```

Click an element. The target can be:
- Text content: `"Sign In"`
- UID from snapshot: `e5`
- CSS selector: `.submit-button`

Use `--double` for double-click.

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

### Wait for Element

```bash
atr browser wait <selector> [--timeout 5000] [--visible]
```

Wait for an element matching the CSS selector to appear in the DOM.

| Flag | Description |
|------|-------------|
| `--timeout <ms>` | Timeout in milliseconds (default: 5000) |
| `--visible` | Wait for the element to be visible (not `display:none` or `opacity:0`) |

### Scroll

```bash
atr browser scroll -s <selector> [--y 500] [--x 0] [--to-bottom] [--to-top]
```

Scroll within a specific element's scroll container (modals, sidebars, etc.).

| Flag | Description |
|------|-------------|
| `-s, --selector <selector>` | CSS selector of scrollable element (required) |
| `--x <pixels>` | Horizontal scroll position |
| `--y <pixels>` | Vertical scroll position |
| `--to-bottom` | Scroll to bottom of element |
| `--to-top` | Scroll to top of element |

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
atr browser screenshot [flags]
```

Capture a screenshot. Returns the path to the saved image or base64 data.

| Flag | Description |
|------|-------------|
| `--full` | Capture full scrollable page |
| `--file` | Save to file instead of base64 |
| `-s, --selector <selector>` | CSS selector of element to screenshot |
| `--selector-all <selector>` | Screenshot every element matching the selector |
| `--output-dir <path>` | Directory to save screenshots (with `--selector-all`) |
| `--timeout <ms>` | Per-element timeout in milliseconds (with `--selector-all`, default: 30000) |

Combine `--selector` with `--full` to capture the full scrollable height of an element (e.g., a modal with overflow scroll).

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

### Computed Styles

```bash
atr browser computed-styles <selector> [--properties "fontSize,color"]
```

Get computed CSS styles for an element. Without `--properties`, returns a default set of common layout and typography properties.

| Flag | Description |
|------|-------------|
| `--properties <csv>` | Comma-separated CSS properties to return |
| `--selector-all <selector>` | Get styles for every matching element |
| `--selector <selector>` | Repeatable flag for batch-querying multiple selectors |

```bash
atr browser computed-styles "h1" --properties "fontSize,fontWeight,color"
atr browser computed-styles --selector-all ".card"
atr browser computed-styles --selector ".btn" --selector ".link"
```

### Computed Styles Diff

```bash
atr browser computed-styles-diff <selector> --against <page-index> [flags]
```

Compare computed CSS styles of an element on the current page against the same (or different) element on another open page. Returns matches, mismatches, and a similarity score.

| Flag | Description |
|------|-------------|
| `--against <page-index>` | Page index to compare against (default: `0`) |
| `--properties <csv>` | Comma-separated CSS properties to compare |
| `--selector-target <selector>` | CSS selector on target page (defaults to source selector) |
| `--selector <selector>` | Repeatable flag for batch-diffing multiple selectors |

### Text Extraction

```bash
atr browser text <selector> [--flat] [--links] [--headings]
```

Extract text content from an element, structured by HTML tag hierarchy.

| Flag | Description |
|------|-------------|
| `--flat` | Return plain text only |
| `--links` | Return only link elements with href |
| `--headings` | Return only heading elements (h1-h6) |

### Clean Snapshot

```bash
atr browser clean-snapshot <selector> [flags]
```

Get a cleaned, indented DOM subtree. Removes data-\*/aria-\* attributes (except data-theme, data-variant, data-state), inline scripts/styles/hidden elements, flattens empty wrapper divs, collapses SVGs, and truncates text to 80 characters.

| Flag | Description |
|------|-------------|
| `--depth <n>` | Maximum tree depth (0 = unlimited) |
| `--max-length <chars>` | Maximum output characters (default: 5000) |
| `--svg-full` | Include full SVG path data |
| `--json` | Output as JSON tree instead of HTML |

### Font Check

```bash
atr browser font-check <font-family>
```

Check if a font family is actually loaded and rendering. Uses the CSS Font Loading API to verify real status rather than declared `@font-face`.

### Viewport

```bash
atr browser viewport [width height] [--preset mobile] [--dpr 2]
```

Get or set the browser viewport dimensions.

| Flag | Description |
|------|-------------|
| `--preset <name>` | Named preset: `mobile`, `tablet`, `desktop`, `wide` |
| `--dpr <number>` | Device pixel ratio (default: 1) |

Without arguments, returns the current viewport size.

### Download Images

```bash
atr browser download-images <selector> [--output-dir /tmp/] [--fallback-screenshot]
```

Download images found within elements matching a CSS selector.

| Flag | Description |
|------|-------------|
| `--output-dir <path>` | Directory to save images (default: `/tmp/`) |
| `--fallback-screenshot` | Screenshot elements when no `<img>` tags found |

### Ask

```bash
atr browser ask <question>
```

Ask a natural language question about the current page. An AI sub-agent inspects the page and returns a concise answer.

### Batch Execution

```bash
atr browser batch [--file commands.txt] [--on-error stop] [--timeout 60]
```

Execute multiple commands sequentially from stdin or a file. One command per line (without `atr browser` prefix). Lines starting with `#` are comments. Supports variable extraction with `let` and interpolation with `[[name]]`.

| Flag | Description |
|------|-------------|
| `--on-error <mode>` | Error handling: `stop` (default), `continue`, or `retry:N` |
| `--timeout <seconds>` | Total batch timeout (default: 60) |
| `--file <path>` | Read commands from file instead of stdin |

```bash
atr browser batch << 'EOF'
navigate https://example.com
wait .content --timeout 5000
eval "document.querySelectorAll('.card').length"
let count = $.result
screenshot --file
EOF
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
| `/wait` | POST | Wait for element |
| `/scroll` | POST | Scroll within element |
| `/snapshot` | GET | Get element tree |
| `/screenshot` | GET | Capture screenshot |
| `/html` | GET | Get page HTML |
| `/url` | GET | Get current URL |
| `/title` | GET | Get page title |
| `/eval` | POST | Execute JavaScript |
| `/computed-styles` | GET | Get computed CSS styles |
| `/computed-styles-diff` | GET | Compare styles across pages |
| `/text` | GET | Extract text content |
| `/clean-snapshot` | GET | Get cleaned DOM subtree |
| `/font-check` | GET | Check font load status |
| `/viewport` | GET/POST | Get or set viewport size |
| `/download-images` | POST | Download images from elements |
| `/ask` | POST | Ask AI about the page |
| `/record/start` | POST | Start recording interactions |
| `/record/stop` | POST | Stop recording, return events |
| `/record/status` | GET | Check recording status |
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

## Session Persistence

By default, browser sessions are ephemeral - cookies and login state are lost when the browser closes. Enable session persistence to maintain login state across browser restarts.

### Configuration

**Via CLI flags:**
```bash
atr browser start --persist-session
atr browser start --persist-session --data-dir ~/my-browser-data
```

**Via config file (`~/.atr/config.yaml`):**
```yaml
behavior:
  browser:
    persist_session: true
    data_dir: "~/.atr/browser-data"  # Optional, this is the default
```

**Via project config (`.atr/config.yaml`):**
```yaml
behavior:
  browser:
    persist_session: true
    data_dir: ".atr/browser-data"  # Project-local sessions
```

### How It Works

When `persist_session` is enabled:
1. Browser data (cookies, localStorage, etc.) is stored in `data_dir`
2. Data is preserved when the browser closes
3. On next launch, the browser loads the saved session data

### Use Cases

- **Development testing**: Login once, stay logged in across test runs
- **E2E testing**: Pre-authenticate test accounts
- **Manual testing**: Avoid repeated logins during iterative testing

### Example Workflow

```bash
# 1. Start browser with session persistence
atr browser start --persist-session

# 2. Login to a site
atr browser navigate https://github.com
# ... manually login ...

# 3. Stop browser
atr browser stop

# 4. Later: start again - still logged in!
atr browser start --persist-session
atr browser navigate https://github.com
# Session restored, still logged in!
```

### Security Considerations

- The data directory contains authentication cookies - treat as sensitive
- Use restrictive file permissions (the default location uses 0700)
- Don't commit browser data directories to version control
- Add `.atr/browser-data` to `.gitignore`
- Clear with `rm -rf ~/.atr/browser-data` to logout from all sites
