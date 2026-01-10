# ATR Browser Commands Reference

Complete reference for all ATR browser commands with full flag documentation.

## Lifecycle Commands

### start
```bash
atr browser start [--port PORT]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--port` | HTTP server port | 9333 |

Output includes server endpoint URL, PID, and state file location.

### stop
```bash
atr browser stop
```

Gracefully shuts down daemon and removes state file.

### status
```bash
atr browser status
```

Shows running state and endpoint URL.

## Navigation Commands

### navigate
```bash
atr browser navigate <url>
```

Returns page title and load time.

### back / forward / reload
```bash
atr browser back
atr browser forward
atr browser reload
```

Standard browser navigation actions.

## Page Management

### new-page
```bash
atr browser new-page [url]
```

Opens new tab, optionally with URL.

### list-pages
```bash
atr browser list-pages
```

Lists all tabs with URLs and titles. Use global `--json` flag for structured output.

### select-page
```bash
atr browser select-page <index>
```

Switch to tab at index (0-based).

### close-page
```bash
atr browser close-page <index>
```

Close tab at specified index.

## Interaction Commands

### click
```bash
atr browser click <target> [--double]
```

| Flag | Description |
|------|-------------|
| `--double` | Double click instead of single click |

Target can be UID (e.g., e0), text, aria-label, data-testid, or CSS selector.

### fill
```bash
atr browser fill <target> <value>
```

Examples:
```bash
atr browser fill "Email" "user@example.com"
atr browser fill "#password" "secret123"
atr browser fill e1 "form value"
```

### hover
```bash
atr browser hover <target>
```

For triggering dropdowns, tooltips, hover states.

### press-key
```bash
atr browser press-key <key>
```

Supported keys:
- Named: `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`, `Space`
- Modifiers: `Control+a`, `Shift+Tab`, `Alt+F4`
- Arrows: `ArrowUp`, `ArrowDown`, `ArrowLeft`, `ArrowRight`
- Function: `F1` through `F12`

### drag
```bash
atr browser drag <from> <to>
```

Both arguments are element targets.

## Inspection Commands

### snapshot
```bash
atr browser snapshot [--verbose]
```

| Flag | Description |
|------|-------------|
| `--verbose` | Include detailed attributes |

Returns the accessibility tree of visible page elements with unique identifiers (e0, e1, etc.) that can be used as click targets.

### screenshot
```bash
atr browser screenshot --file [--full]
```

| Flag | Description |
|------|-------------|
| `--file` | Save to file instead of base64 |
| `--full` | Capture full scrollable page |

With `--file`, screenshots are saved to `/tmp/` with a timestamped filename. No need to specify a file path.

Returns the path to the saved image (e.g., `/tmp/atr-screenshot-20240105-103045.png`).

### html
```bash
atr browser html
```

Returns full page HTML.

### url
```bash
atr browser url
```

Returns current page URL.

### title
```bash
atr browser title
```

Returns current page title.

### eval
```bash
atr browser eval <script>
```

Execute JavaScript and return result.

Examples:
```bash
atr browser eval "document.querySelectorAll('a').length"
atr browser eval "window.localStorage.getItem('token')"
atr browser eval "document.title"
```

## Debugging Commands

### console
```bash
atr browser console [--limit N]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--limit` | Maximum messages to return | 50 |

Get console messages (log, warn, error).

### network
```bash
atr browser network [--limit N]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--limit` | Maximum requests to return | 50 |

Get network requests with status, method, timing.

### errors
```bash
atr browser errors
```

Get failed network requests (4xx, 5xx, or network errors).

## Common Flags

Available on most commands:

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `--endpoint <url>` | Override server endpoint |

## Configuration

Server settings in `~/.atr/config.yaml`:

```yaml
server:
  port: 9333
  read_timeout: 30s
  write_timeout: 30s
```

## State File

Browser state stored at `~/.atr/browser.state`:

```json
{
  "pid": 12345,
  "endpoint": "http://localhost:9333",
  "started_at": "2024-01-05T10:30:00Z"
}
```
