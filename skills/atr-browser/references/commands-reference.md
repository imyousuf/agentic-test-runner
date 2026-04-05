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
atr browser screenshot --file [--full] [--selector SELECTOR] [--selector-all SELECTOR] [--output-dir DIR] [--timeout MS]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--file` | | Save to file instead of base64 |
| `--full` | | Capture full scrollable page |
| `--selector` | `-s` | CSS selector of element to screenshot |
| `--selector-all` | | CSS selector matching multiple elements |
| `--output-dir` | | Directory to save screenshots (with --selector-all) |
| `--timeout` | | Per-element timeout in ms (with --selector-all, default: 30000) |

With `--file`, screenshots are saved to `/tmp/` with a timestamped filename.

Combine `--selector` with `--full` to capture an element's full scrollable height (useful for modals/dialogs with overflow).

Use `--selector-all` to screenshot every matching element as numbered PNGs (1.png, 2.png, etc.). Elements that fail or timeout are skipped and reported separately — successful screenshots are always saved.

Examples:
```bash
atr browser screenshot --file                                      # Viewport screenshot
atr browser screenshot --file --full                               # Full page screenshot
atr browser screenshot --file -s "header"                          # Screenshot header element
atr browser screenshot --file -s "#nav"                            # Screenshot by ID
atr browser screenshot --file -s "[role=dialog]" --full            # Full-height modal screenshot
atr browser screenshot --file --selector-all ".card"               # Screenshot all cards
atr browser screenshot --file --selector-all ".card" --output-dir ./cards/  # Save to dir
```

### computed-styles
```bash
atr browser computed-styles <selector> [--properties "prop1,prop2"] [--selector-all SELECTOR]
```

| Flag | Description |
|------|-------------|
| `--properties` | Comma-separated CSS properties to return (default: common layout/typography/font-rendering set) |
| `--selector-all` | CSS selector matching multiple elements — returns styles for each in an array |

Returns computed CSS styles for an element as JSON. The default property set includes font rendering properties (`fontFeatureSettings`, `textRendering`, `webkitFontSmoothing`, `fontKerning`) alongside layout and typography properties.

Examples:
```bash
atr browser computed-styles "h1"
atr browser computed-styles "h1" --properties "fontSize,fontWeight,color"
atr browser computed-styles ".hero-section"
atr browser computed-styles --selector-all "footer a" --json    # Styles for all footer links
atr browser computed-styles --selector-all ".card h3"           # Styles for all card headings
```

### computed-styles-diff
```bash
atr browser computed-styles-diff <selector> --against <page-index> [--properties "..."] [--selector-target "..."]
```

| Flag | Description |
|------|-------------|
| `--against` | Page index to compare against (0-based) |
| `--properties` | Comma-separated CSS properties to compare |
| `--selector-target` | Different CSS selector on the target page |

Compares computed styles between current page and another open page. Returns matches, mismatches, and similarity score.

Examples:
```bash
atr browser computed-styles-diff "h1" --against 0
atr browser computed-styles-diff "h1" --against 0 --properties "fontSize,color"
atr browser computed-styles-diff ".hero" --against 0 --selector-target ".banner"
```

### text
```bash
atr browser text <selector> [--flat] [--links] [--headings]
```

| Flag | Description |
|------|-------------|
| `--flat` | Return plain text only |
| `--links` | Return only link elements with href |
| `--headings` | Return only heading elements (h1-h6) |

Extracts text content from an element, structured by HTML tag hierarchy.

Examples:
```bash
atr browser text "footer"              # Structured text groups
atr browser text "footer" --flat       # Plain concatenated text
atr browser text "footer" --links      # Only <a> elements with hrefs
atr browser text "footer" --headings   # Only h1-h6 elements
```

### wait
```bash
atr browser wait <selector> [--timeout 5000] [--visible]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--timeout` | Timeout in milliseconds | 5000 |
| `--visible` | Wait for element to be visible (not just present) | false |

Waits until an element exists in the DOM. With `--visible`, also waits until the element is rendered and visible.

Examples:
```bash
atr browser wait "[role=dialog]"
atr browser wait "[role=dialog]" --timeout 10000
atr browser wait ".loading-spinner" --visible
```

### scroll
```bash
atr browser scroll --selector "<selector>" [--y N] [--x N] [--to-bottom] [--to-top]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--selector` | `-s` | CSS selector of scrollable element (required) |
| `--y` | | Vertical scroll position in pixels |
| `--x` | | Horizontal scroll position in pixels |
| `--to-bottom` | | Scroll to bottom of element |
| `--to-top` | | Scroll to top of element |

Scrolls within an element's scroll container. Returns scroll position and dimensions.

Examples:
```bash
atr browser scroll -s "[role=dialog]" --y 800
atr browser scroll -s "#modal" --to-bottom
atr browser scroll -s ".carousel" --x 400
```

### font-check
```bash
atr browser font-check <font-family>
```

Checks if a font family is actually loaded and rendering in the browser using the CSS Font Loading API. Unlike `computed-styles` which reports the declared `@font-face` family, `font-check` detects whether the font was actually downloaded and can render.

Returns:
- `family` — the queried font name
- `declared` — whether a `@font-face` declaration exists
- `loaded` — whether the font is actually loaded
- `status` — `loaded`, `loading`, `error`, `unloaded`, or `not_found`
- `reason` — explanation for non-loaded status
- `fallback` — the fallback fonts from the CSS stack

Examples:
```bash
atr browser font-check "sohne-var"
atr browser font-check "Inter"
atr browser font-check "Arial"
```

### download-images
```bash
atr browser download-images <selector> [--output-dir DIR] [--fallback-screenshot]
```

| Flag | Description |
|------|-------------|
| `--output-dir` | Directory to save images (default: /tmp/) |
| `--fallback-screenshot` | Screenshot elements when no `<img>` tags found |

Downloads images found within elements matching a CSS selector. Finds all `<img>` elements within scope and fetches their `src` URLs via the browser (bypassing CORS). With `--fallback-screenshot`, falls back to screenshotting each matching element when no `<img>` tags are found.

Files are saved as numbered images (1.png, 2.jpg, etc.).

Examples:
```bash
atr browser download-images "section:nth-of-type(2)" --output-dir ./images/
atr browser download-images ".bento-card" --fallback-screenshot --output-dir ./cards/
atr browser download-images "#gallery" --output-dir /tmp/gallery/
```

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
