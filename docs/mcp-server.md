# MCP Server

ATR can run as an MCP (Model Context Protocol) server, exposing **browser** AND **desktop** automation tools to any MCP-compatible client like Claude CLI or Gemini CLI.

## Overview

The MCP server allows you to use ATR's browser and desktop automation capabilities from within AI CLI tools. Instead of running ATR commands directly, the CLI tool can invoke ATR's tools through the standardized MCP protocol.

The server exposes two tool families:
- `browser_*` (30 tools) — Chromium control via the browser daemon
- `computer_*` (22 tools) — cross-platform desktop control via the computer daemon, including `computer_ask` (an in-process LLM agent for natural-language tasks)

```
CLI Tool (Claude/Gemini) --> MCP Protocol --> ATR Server --> Browser
```

## Starting the Server

```bash
atr mcp serve [flags]
```

### Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--headless` | Run browser in headless mode | `true` |
| `--ignore-https-errors` | Ignore HTTPS certificate errors | `false` |

## Available Tools

52 tools total: 30 browser (`browser_*`) and 22 desktop (`computer_*`).

## Available Browser Tools (30)

### Navigation

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_navigate` | Navigate to a URL | `url` (required) |
| `browser_go_back` | Navigate back in history | - |
| `browser_go_forward` | Navigate forward in history | - |
| `browser_reload` | Reload the current page | - |

### Page Management

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_new_page` | Open a new tab | `url` (optional) |
| `browser_list_pages` | List all open tabs | - |
| `browser_select_page` | Switch to tab by index | `index` (required) |
| `browser_close_page` | Close tab by index | `index` (required) |

### Interaction

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_click` | Click on an element | `selector` (required), `double` (optional) |
| `browser_fill` | Fill a form field | `selector` (required), `value` (required) |
| `browser_hover` | Hover over an element | `selector` (required) |
| `browser_press_key` | Press a key or combination | `key` (required) |
| `browser_drag` | Drag one element to another | `from` (required), `to` (required) |
| `browser_wait` | Wait for element to appear | `selector` (required), `timeout` (optional, ms), `visible` (optional) |
| `browser_scroll` | Scroll within an element | `selector` (required), `x`/`y` (optional), `to_bottom`/`to_top` (optional) |

### Inspection

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_get_url` | Get the current page URL | - |
| `browser_get_title` | Get the current page title | - |
| `browser_get_html` | Get the page HTML content | - |
| `browser_snapshot` | Get accessibility tree | `verbose` (optional) |
| `browser_screenshot` | Take a screenshot | `file`, `full_page`, `selector`, `selector_all`, `output_dir` (all optional) |
| `browser_eval` | Execute JavaScript | `script` (required) |
| `browser_computed_styles` | Get computed CSS styles | `selector` (required), `properties` (optional, CSV) |
| `browser_computed_styles_diff` | Compare styles across pages | `selector` (required), `against` (required), `properties`, `selector_target` (optional) |
| `browser_text` | Extract text content | `selector` (required), `mode` (optional: structured/flat/links/headings) |
| `browser_clean_snapshot` | Get cleaned DOM subtree | `selector` (required), `depth`, `max_length`, `svg_full`, `json` (optional) |
| `browser_font_check` | Check font load status | `family` (required) |
| `browser_viewport` | Get or set viewport size | `width`/`height`, `preset`, `dpr` (all optional — omit all to get current) |
| `browser_download_images` | Download images from elements | `selector` (required), `output_dir`, `fallback_screenshot` (optional) |

### AI

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_ask` | Ask AI a question about the page | `question` (required) |

### Debugging

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_console` | Get console messages | `limit` (optional, default: 50) |
| `browser_network` | Get network requests | `limit` (optional, default: 50) |
| `browser_errors` | Get failed network requests | - |

## Available Computer Tools (22)

Cross-platform desktop control. Coordinates default to **root coords** (the bounding box of all monitors with origin (0, 0)). Mouse tools accept an optional `display` integer to switch to display-local pixels relative to that display's top-left.

### Perception

| Tool | Description | Parameters |
|------|-------------|------------|
| `computer_screenshot` | Capture the desktop and write to a file. Returns the path. | `output` (optional path), `display` (optional) |
| `computer_displays` | List all displays and primary screen size | - |
| `computer_position` | Current mouse position (root coords) | - |
| `computer_active_window` | Currently focused window with bounds + app name | - |
| `computer_list_windows` | All top-level windows with bounds | - |

### Mouse

| Tool | Description | Parameters |
|------|-------------|------------|
| `computer_click` | Click at (x, y) | `x`, `y` (required), `button` (left/right/center), `double`, `display` |
| `computer_double_click` | Double-click | `x`, `y` (required), `display` |
| `computer_right_click` | Right-click | `x`, `y` (required), `display` |
| `computer_move` | Move cursor | `x`, `y` (required), `smooth`, `display` |
| `computer_drag` | Drag from (from_x, from_y) to (to_x, to_y) | `from_x`, `from_y`, `to_x`, `to_y` (required), `button`, `display` |
| `computer_scroll` | Scroll wheel at cursor position | `dx`, `dy` (positive = up) |
| `computer_hover` | Move without clicking | `x`, `y` (required), `display` |

### Keyboard

| Tool | Description | Parameters |
|------|-------------|------------|
| `computer_type` | Type text | `text` (required), `delay_ms` |
| `computer_press_key` | Press a single named key | `key` (required, e.g. `enter`, `esc`, `f5`) |
| `computer_key_chord` | Press a key combination | `chord` (required, e.g. `ctrl+shift+t`) |

### Window Management

| Tool | Description | Parameters |
|------|-------------|------------|
| `computer_focus_window` | Bring window to front | `id` (required) |
| `computer_window_state` | Set state | `id`, `state` (required, one of: minimize/maximize/restore/close) |
| `computer_move_window` | Move window | `id`, `x`, `y` (required) |
| `computer_resize_window` | Resize window | `id`, `width`, `height` (required) |

### App Lifecycle

| Tool | Description | Parameters |
|------|-------------|------------|
| `computer_launch_app` | Launch an application by name | `name` (required) |
| `computer_quit_app` | Quit an application by name | `name` (required) |

### Agent

| Tool | Description | Parameters |
|------|-------------|------------|
| `computer_ask` | Run an in-process LLM agent loop to accomplish a desktop task. The daemon uses its configured LLM (gemini-3.7-flash, gemini-3.1-pro-preview, claude-sonnet-5, claude-opus-5, or claude-cli) to screenshot, decide, and call lower-level tools until done. | `instruction` (required), `max_steps` (optional, default 20) |

## Integration with Claude CLI

### Method 1: Inline Configuration

```bash
claude -p "Navigate to example.com and take a screenshot" \
  --mcp-config '{"mcpServers":{"atr-browser":{"command":"atr","args":["mcp","serve"]}}}' \
  --allowedTools "mcp__atr-browser__*"
```

### Method 2: Add to User Settings

Add to `~/.claude.json`:

```json
{
  "mcpServers": {
    "atr-browser": {
      "command": "atr",
      "args": ["mcp", "serve"]
    }
  }
}
```

### Method 3: Project-Level Configuration

Create `.mcp.json` in your project root:

```json
{
  "mcpServers": {
    "atr-browser": {
      "command": "atr",
      "args": ["mcp", "serve"]
    }
  }
}
```

## Integration with Gemini CLI

### Method 1: Project Settings

Create `.gemini/settings.json` in your project:

```json
{
  "mcpServers": {
    "atr-browser": {
      "command": "atr",
      "args": ["mcp", "serve"],
      "trust": true
    }
  }
}
```

### Method 2: User Settings

Add to `~/.gemini/settings.json`:

```json
{
  "mcpServers": {
    "atr-browser": {
      "command": "atr",
      "args": ["mcp", "serve"],
      "trust": true
    }
  }
}
```

## Element Selectors

Browser interaction tools accept flexible selectors:

1. **CSS Selectors**: `#login-button`, `.nav-link`, `input[name="email"]`
2. **XPath**: `//button[@type="submit"]`
3. **Text Content**: `"Sign In"`, `"Submit Form"`
4. **Aria Labels**: Elements with matching `aria-label` attribute
5. **Test IDs**: Elements with matching `data-testid` attribute

## Keyboard Keys

For `browser_press_key`, use:

- **Named keys**: `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`
- **Modifiers**: `Control+a`, `Shift+Tab`, `Alt+Enter`, `Meta+c`
- **Arrow keys**: `ArrowUp`, `ArrowDown`, `ArrowLeft`, `ArrowRight`
- **Function keys**: `F1`, `F2`, etc.

## Protocol Details

The MCP server implements the [Model Context Protocol](https://modelcontextprotocol.io/) specification:

- **Transport**: JSON-RPC 2.0 over stdio
- **Protocol Version**: 2024-11-05
- **Server Name**: atr-browser
- **Server Version**: 1.0.0

### Supported Methods

| Method | Description |
|--------|-------------|
| `initialize` | Initialize the server connection |
| `initialized` | Notification after initialization |
| `tools/list` | List available browser tools |
| `tools/call` | Execute a browser tool |

## Examples

### Navigate and Screenshot

```
claude -p "Navigate to https://news.ycombinator.com and take a screenshot of the page"
```

### Fill a Form

```
claude -p "Go to https://example.com/login, fill 'user@example.com' in the email field, fill 'password123' in the password field, and click the submit button"
```

### Inspect Page

```
claude -p "Navigate to https://github.com and get a snapshot of all interactive elements"
```

## Troubleshooting

### Server Won't Start

Ensure ATR is installed and in your PATH:

```bash
which atr
atr version
```

### Browser Won't Launch

Check browser configuration:

```bash
atr config show
```

Verify browser can start manually:

```bash
atr browser start
atr browser status
atr browser stop
```

### Connection Issues

Test the MCP server directly:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | atr mcp serve
```

Expected response:
```json
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"atr-browser","version":"1.0.0"},"capabilities":{"tools":{}}}}
```

## See Also

- [CLI Reference](cli-reference.md#atr-mcp) - Command reference for `atr mcp serve`
- [Configuration](configuration.md) - Browser and server configuration options
- [Browser Server](browser-server.md) - HTTP server mode for programmatic control
