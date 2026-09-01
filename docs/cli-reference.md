# CLI Reference

Complete reference for all ATR commands and flags.

## Global Flags

These flags are available for all commands:

| Flag | Description |
|------|-------------|
| `--config <path>` | Config file path (default: `~/.atr/config.yaml`) |
| `-v, --verbose` | Enable verbose output |
| `--backend <name>` | LLM backend: `claude-cli`, `gemini-cli`, `gemini-api`, `vertex-ai`, or `vertex-claude` |
| `--api-key <key>` | Gemini API key (for `gemini-api` backend) |
| `--project <id>` | GCP project for Vertex AI |
| `--location <region>` | GCP region for Vertex AI |
| `--model <tier>` | Model: `flash`/`pro` (Gemini API backends), `sonnet`/`opus` (`vertex-claude`), or `opus`/`sonnet`/`haiku` (claude-cli) |

---

## atr run

Execute commands with AI-powered failure analysis, or run browser behavior tests.

### Command Execution Mode

```bash
atr run --cmd "<command>" [flags]
```

Execute a shell command and analyze failures with AI.

#### Flags

| Flag | Description |
|------|-------------|
| `--cmd <command>` | Command to execute (required for this mode) |
| `--cwd <path>` | Working directory (default: current directory) |
| `--context <text>` | Additional context for the AI agent |
| `--python-venv <path>` | Path to Python virtual environment to activate |
| `--nvm-version <version>` | Node.js version to use via nvm (e.g., `18` or `18.17.0`) |
| `--no-auto-env` | Disable automatic environment detection |

#### Examples

```bash
# Run tests
atr run --cmd "go test ./..."

# Run in specific directory
atr run --cmd "npm test" --cwd "/path/to/project"

# With context
atr run --cmd "make build" --context "Building after refactoring the auth module"

# Use pro model for complex issues
atr run --cmd "pytest" --model pro

# With specific Python venv
atr run --cmd "pytest tests/" --python-venv /path/to/.venv

# With specific Node.js version
atr run --cmd "npm test" --nvm-version 18

# Disable auto environment detection
atr run --cmd "python script.py" --no-auto-env
```

### Behavior Testing Mode

```bash
atr run --behavior <path> [flags]
```

Run browser-based behavior tests using AI-driven automation.

#### Flags

| Flag | Description |
|------|-------------|
| `--behavior <path>` | Path to `.test.txt` file or directory |
| `--browser-url <url>` | Base URL for tests (overrides config) |
| `--headless` | Run browser headless (default: `false`) |
| `--no-compile` | Replay only; never call the model, and needs no backend configured. Fails if a script is missing or stale (use in CI) |
| `--recompile` | Regenerate the compiled script even if it matches the spec |
| `--no-repair` | Diagnose a drifted script but do not rewrite it |
| `--prune-values` | Remove inputs neither the compiled script nor `_shared.js` reads |
| `--lint <mode>` | What to do about a script that cannot fail: `error` (default), `warn`, `off` |
| `--no-triage` | Never ask the model why a failure happened, even to classify it |
| `--no-extract` | Never hoist repeated operations into `_shared.js` |
| `--otel-endpoint <url>` | OTLP collector for run telemetry, e.g. `http://localhost:4318` |
| `--interpret` | Skip compilation and let the agent drive every step (slower, costs tokens per run) |
| `--viewport <WxH>` | Viewport size, e.g., `1920x1080` |
| `--cdp-endpoint <url>` | Connect to existing browser via CDP |

#### Examples

```bash
# Run single test
atr run --behavior tests/login.test.txt

# Run all tests in directory
atr run --behavior tests/e2e/

# With base URL
atr run --behavior tests/e2e/ --browser-url http://localhost:3000

# Non-headless for debugging
atr run --behavior tests/login.test.txt --headless=false

# Custom viewport
atr run --behavior tests/mobile.test.txt --viewport 375x667

# Connect to existing browser
atr run --behavior tests/debug.test.txt --cdp-endpoint ws://localhost:9222
```

---

## atr refactor-ops

```bash
atr refactor-ops <directory> [flags]
```

Hoist the operations a directory's specs keep repeating into `_shared.js`.

Compiling a spec re-derives whatever the application makes it re-derive, so
several specs end up carrying their own copy of the same sign-in. This finds
those sequences, names them once, rewrites the scripts to call them, and proves
the rewrites before keeping them.

Nothing is kept unless all of it holds:

- the library declares operations only, and runs nothing at load time
- the library still declares every operation it declared before — other specs
  in the directory call them, and they are not replayed
- every rewritten script still lints
- every rewritten script claims exactly what it claimed before, character for
  character, and gained no branch, loop or early return around an assertion
- every rewritten script still passes against the live application

Fail any of those and every file goes back exactly as it was.

A run does this on its own, so this command is for when it has been turned off
— or for seeing what it would do first.

| Flag | Description |
|------|-------------|
| `--dry-run` | Report what could be hoisted; change nothing, open no browser, call no model |
| `--browser-url <url>` | Base URL for the verification replays |
| `--headless` | Run the browser headless |

```bash
# What is repeated here?
atr refactor-ops tests/ --dry-run

# Hoist it, and prove the rewrites before keeping them
atr refactor-ops tests/ --headless
```

**A run hoists on its own.** `behavior.extract_operations` decides when:

| Value | Meaning |
|-------|---------|
| `always` | Default. Hoist as soon as a repeated sequence appears |
| `on-demand` | Report what could be hoisted and change nothing |
| `off` | Do not look |

An unrecognised value is refused rather than defaulted, because the only reason
to set this key is to restrain something.

Under `--no-compile` a run only ever reports, whatever this is set to:
applying would call the model and rewrite the scripts, and that flag permits
neither — so a CI replay never leaves a modified working tree.

---

## atr browser

Control a browser via HTTP server mode. See [Browser Server Mode](browser-server.md) for detailed documentation.

### Lifecycle Commands

```bash
atr browser start [flags]         # Start browser daemon
atr browser stop                  # Stop browser daemon
atr browser status                # Check if running
```

| Flag | Description |
|------|-------------|
| `--port <port>` | Server port (default: 9333) |
| `--headless` | Run browser in headless mode (no visible window) |
| `--persist-session` | Keep cookies/sessions after browser closes |
| `--data-dir <path>` | Directory for browser data (default: `~/.atr/browser-data` when `--persist-session`) |
| `--sandbox` | Enable Chrome sandbox (disabled by default for Ubuntu 23.10+ compatibility) |
| `--system-chrome` | Use system-installed Google Chrome (falls back to bundled browser if not found) |

### Navigation

```bash
atr browser navigate <url>        # Navigate to URL
atr browser back                  # Go back
atr browser forward               # Go forward
atr browser reload                # Reload page
```

### Page Management

```bash
atr browser new-page [url]        # Open new tab
atr browser list-pages            # List all tabs
atr browser select-page <index>   # Switch to tab
atr browser close-page <index>    # Close tab
```

### Interaction

```bash
atr browser click <target> [--double]        # Click element (or double-click)
atr browser fill <target> <value>            # Type into input
atr browser hover <target>                   # Hover over element
atr browser press-key <key>                  # Press keyboard key
atr browser drag <from> <to>                 # Drag element
atr browser scroll -s <selector> [flags]     # Scroll within an element
atr browser wait <selector> [flags]          # Wait for element to appear
```

Target can be a UID (e.g., `e0`), text, aria-label, data-testid, or CSS selector.

#### scroll flags

| Flag | Description |
|------|-------------|
| `-s, --selector <selector>` | CSS selector of scrollable element (required) |
| `--x <pixels>` | Horizontal scroll position in pixels |
| `--y <pixels>` | Vertical scroll position in pixels |
| `--to-bottom` | Scroll to bottom of element |
| `--to-top` | Scroll to top of element |

#### wait flags

| Flag | Description |
|------|-------------|
| `--timeout <ms>` | Timeout in milliseconds (default: 5000) |
| `--visible` | Wait for element to be visible (not `display:none` or `opacity:0`) |

### Inspection

```bash
atr browser snapshot [--verbose]              # Get page elements with UIDs
atr browser screenshot [flags]                # Capture screenshot
atr browser html                              # Get page HTML
atr browser url                               # Get current URL
atr browser title                             # Get page title
atr browser eval <script>                     # Run JavaScript
atr browser computed-styles <selector> [flags] # Get computed CSS styles
atr browser computed-styles-diff <selector> [flags] # Compare styles across pages
atr browser text <selector> [flags]           # Extract text content
atr browser clean-snapshot <selector> [flags] # Get cleaned DOM subtree
atr browser font-check <font-family>          # Check if a font is loaded
atr browser viewport [width height] [flags]   # Get or set viewport size
atr browser ask <question>                    # Ask AI about the page
atr browser hud on|off|status                 # In-page agent panel
```

#### `atr browser hud`

Shows a floating agent panel inside the browser window, so you can hand work to
the agent without leaving the page. Headed browsers only.

```bash
atr browser start --hud          # start headed with the panel showing
atr browser hud on               # or turn it on later
atr browser hud on --working-dir ~/src/myapp
atr browser hud status
atr browser hud off
```

The panel's agent can drive the browser, run shell commands, read files and
search code. It fills passwords without ever seeing them — you give it the
command, ATR runs it and types the output into the field:

```
fill the password field by running: pass show github/password
```

Name entries instead of commands by adding `secrets.refs` to
`~/.atr/config.yaml`. See [In-Page Agent HUD](browser-hud.md).

| Flag | Description |
|------|-------------|
| `--working-dir <dir>` | Directory the agent's shell, read and search tools operate in (default: current directory) |

#### screenshot flags

| Flag | Description |
|------|-------------|
| `--full` | Capture full scrollable page |
| `--file` | Save to file instead of base64 |
| `-s, --selector <selector>` | CSS selector of element to screenshot |
| `--selector-all <selector>` | CSS selector matching multiple elements to screenshot |
| `--output-dir <path>` | Directory to save screenshots (used with `--selector-all`) |
| `--timeout <ms>` | Per-element timeout in milliseconds (used with `--selector-all`, default: 30000) |

Combine `--selector` with `--full` to capture the full scrollable height of an element (e.g., a modal with overflow scroll).

#### computed-styles flags

| Flag | Description |
|------|-------------|
| `--properties <csv>` | Comma-separated CSS properties to return (e.g., `fontSize,color,fontWeight`) |
| `--selector-all <selector>` | Get styles for every element matching the selector |
| `--selector <selector>` | Repeatable flag for batch-querying multiple selectors |

#### computed-styles-diff flags

| Flag | Description |
|------|-------------|
| `--against <page-index>` | Page index to compare against (default: `0`) |
| `--properties <csv>` | Comma-separated CSS properties to compare |
| `--selector-target <selector>` | CSS selector on target page (defaults to source selector) |
| `--selector <selector>` | Repeatable flag for batch-diffing multiple selectors |

#### text flags

| Flag | Description |
|------|-------------|
| `--flat` | Return plain text only |
| `--links` | Return only link elements with href |
| `--headings` | Return only heading elements (h1-h6) |

#### clean-snapshot flags

| Flag | Description |
|------|-------------|
| `--depth <n>` | Maximum tree depth (0 = unlimited) |
| `--max-length <chars>` | Maximum output characters (default: 5000) |
| `--svg-full` | Include full SVG path data (collapsed by default) |
| `--json` | Output as JSON tree instead of HTML |

#### viewport flags

| Flag | Description |
|------|-------------|
| `--preset <name>` | Named preset: `mobile`, `tablet`, `desktop`, `wide` |
| `--dpr <number>` | Device pixel ratio (default: 1) |

Without arguments, returns the current viewport size. With `width height` or `--preset`, sets the viewport.

### Image Downloading

```bash
atr browser download-images <selector> [flags]  # Download images from elements
```

| Flag | Description |
|------|-------------|
| `--output-dir <path>` | Directory to save images (default: `/tmp/`) |
| `--fallback-screenshot` | Screenshot elements when no `<img>` tags found |

### Batch Execution

```bash
atr browser batch [flags]         # Execute multiple commands sequentially
```

Reads commands from stdin or a file, one per line (without the `atr browser` prefix). Lines starting with `#` are comments. Supports variable extraction with `let` statements and interpolation with `[[name]]` syntax. Maximum 100 commands.

| Flag | Description |
|------|-------------|
| `--on-error <mode>` | Error handling: `stop` (default), `continue`, or `retry:N` |
| `--timeout <seconds>` | Total batch timeout in seconds (default: 60) |
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

### Recording

```bash
atr browser record [flags]        # Record browser interactions as a behavior test
```

Records user interactions in the browser and outputs a `.test.txt` behavior test file. Captures clicks, form fills, keyboard shortcuts, navigation, and scroll events. A floating overlay appears in the browser showing recorded steps in real time.

| Flag | Description |
|------|-------------|
| `--output`, `-o` | Output file path (default: `record-<timestamp>.test.txt`) |
| `--url` | Initial URL to navigate to before recording |

```bash
# Record interactions and save to file
atr browser record --url https://example.com -o repro.test.txt

# Record with auto-generated filename
atr browser record
```

Stop recording with Ctrl+C in the terminal or the "Stop" button in the browser overlay.

### Debugging

```bash
atr browser console [--limit N]   # Get console messages
atr browser network [--limit N]   # Get network requests
atr browser errors                # Get failed requests
```

### Common Flags

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `--endpoint <url>` | Override server endpoint |

---

## atr computer

Cross-platform desktop control: mouse, keyboard, screen capture, window/app management, and a built-in LLM agent (`atr computer ask`). Linux is X11-only in v1 (Wayland not supported).

The computer daemon is a separate process from the browser daemon — they listen on different ports (computer: 9334, browser: 9333) and can run simultaneously.

### Lifecycle Commands

```bash
atr computer start            # Start daemon (default: per-request 3s countdown)
atr computer start --countdown-mode per-app    # First action per app prompts; subsequent auto-approve
atr computer start --countdown-mode off        # No countdown (explicit opt-in for unattended)
atr computer start --countdown 1               # 1-second countdown
atr computer start --no-gui                    # Disable GUI overlay (terminal-only)
atr computer stop
atr computer status
atr computer reset-approvals  # Clear per-app approval cache
```

### Coordinates and `--display`

By default, mouse coordinates are **root coordinates** — the bounding box of all monitors with origin (0, 0). `atr computer displays` and `atr computer window list` both return root coords. Every mouse command also accepts `--display N` to switch to display-local pixels (relative to display N's top-left), which is usually easier to read off a screenshot.

```bash
atr computer displays                              # show each display's root bounds
atr computer click 1500 800                        # absolute root coords
atr computer click --display 0 60 800              # display-local on primary
```

### Screen and Mouse

| Command | Description |
|---------|-------------|
| `atr computer screenshot --output PATH` | Capture full primary display |
| `atr computer screenshot --display N --output PATH` | Capture display N |
| `atr computer screenshot --display N --region X,Y,W,H --output PATH` | Crop within display N (display-local pixels) |
| `atr computer click X Y` | Left click at root coords |
| `atr computer click --display N X Y --button right --double` | Display-local right double-click |
| `atr computer move X Y [--smooth] [--display N]` | Move mouse |
| `atr computer hover X Y [--display N]` | Move without clicking |
| `atr computer drag --from X,Y --to X,Y [--display N] [--button left/right/center]` | Drag with held button |
| `atr computer scroll --dy N [--dx N]` | Scroll wheel at current cursor position |
| `atr computer position` | Print current cursor coords |

### Keyboard

| Command | Description |
|---------|-------------|
| `atr computer type "TEXT" [--delay-ms N]` | Type text |
| `atr computer key KEY` | Press a single named key (e.g. `enter`, `esc`, `f5`, `tab`) |
| `atr computer chord "ctrl+shift+t"` | Press a key combination |

### Window Management

```bash
atr computer window list                            # JSON list of all windows
atr computer window active                          # Currently focused window
atr computer window focus --title "Firefox"         # Match by substring of title or app name
atr computer window minimize --id 12345
atr computer window maximize --title "Code"
atr computer window restore --id 12345
atr computer window close --title "Calculator"
atr computer window move --id 12345 --to 100,100
atr computer window resize --id 12345 --size 800,600
```

### Apps

```bash
atr computer app launch firefox       # On Linux: tries PATH, gtk-launch, xdg-open
atr computer app launch xclock
atr computer app quit firefox         # SIGTERM via pkill on Unix; taskkill on Windows
```

### Ask the agent (`atr computer ask`)

Run an in-process agent loop that screenshots the desktop, sends frames to the configured LLM, and calls the curated subset of computer tools (click, type, key, chord, focus_window, window_state, launch_app) until the goal is achieved or `--max-steps` is hit.

```bash
atr computer ask "open xclock and tell me what time it shows"
atr computer ask "list the open windows that contain 'chrome' in the title"
atr computer ask --max-steps 30 --timeout 10m "open the GNOME calculator and compute 17 * 23"
```

#### Flags

| Flag | Description |
|------|-------------|
| `--max-steps N` | Max agent iterations (default 20) |
| `--timeout DURATION` | Max wall-clock time (default 5m) |

The LLM backend and model are inherited from the global ATR config (`--backend`, `--model`, env vars, `~/.atr/config.yaml`). Default model aliases: `flash → gemini-3.7-flash`, `pro → gemini-3.1-pro-preview`. With `--backend claude-cli`, the model flag is ignored and Claude CLI drives the loop via MCP.

The agent **cannot type passwords**. If a sudo / polkit / authentication prompt appears, it stops and reports the blocker.

### Global flags (apply to all `atr computer ...` subcommands)

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `--endpoint <url>` | Override server endpoint |

---

## atr remote

Serve a live view of the browser that ATR drives, as a web page. Chrome streams the
page over the DevTools Protocol, so no X server, VNC, or desktop packages are needed —
it works against a headless browser on a server.

`atr view` is a registered alias.

```bash
# Watch the browser that "atr browser start" launched
atr remote

# Another port, attached to a browser by endpoint
atr remote --port 9000 --attach cdp://127.0.0.1:9222

# Watch without the ability to click
atr remote --view-only
```

The command prints a URL containing an access token:

```
ATR live view
  URL:     http://127.0.0.1:7788/?t=8f2c...
  Browser: Chrome/151.0.7922.170  (attached, not owned)
  Pages:   2
```

Closing the view leaves the browser running, and ATR keeps driving it while a viewer
watches.

| Flag | Default | Description |
|------|---------|-------------|
| `--port <n>` | `7788` | HTTP port |
| `--bind <addr>` | `127.0.0.1` | Listen address. A non-loopback bind requires an explicit `--token` |
| `--token <s>` | generated | Access token (or set `ATR_REMOTE_TOKEN`) |
| `--attach <url>` | discovered | CDP endpoint, such as `cdp://127.0.0.1:9222` |
| `--view-only` | `false` | Refuse input from viewers |
| `--quality <n>` | `60` | JPEG quality, 1 to 100 |
| `--max-width <n>` | `1600` | Largest frame width |
| `--fps <n>` | `20` | Target frame rate |

**Security:** a viewer gets full control of that browser and its cookies. The default
bind is loopback only. Reach a remote machine through an SSH tunnel rather than opening
the port:

```bash
ssh -L 7788:127.0.0.1:7788 myserver
```

### atr remote setup

Install a service that keeps the live view running: a systemd user unit on Linux, or a
launchd agent on macOS. It generates an access token and stores it in `~/.atr/remote.env`
with owner-only permissions, so the URL is stable across restarts.

It does not install or start a browser — the browser belongs to ATR, and the live view
attaches to whichever one ATR is driving.

```bash
atr remote setup                 # install, enable, and print the URL
atr remote setup --check         # report the state, change nothing
atr remote setup --port 9000     # use another port
atr remote setup --uninstall     # remove the service, keep the token
```

| Flag | Default | Description |
|------|---------|-------------|
| `--port <n>` | `7788` | HTTP port for the service |
| `--bind <addr>` | `127.0.0.1` | Listen address for the service |
| `--fps <n>` | `20` | Target frame rate |
| `--check` | `false` | Report the state and change nothing |
| `--uninstall` | `false` | Remove the service, keep the token |

See [Browser Live View](remote-live-view.md) for the protocol, the foreground rule, and
troubleshooting.

---

## atr mcp

Run ATR as an MCP (Model Context Protocol) server, exposing browser automation tools to MCP-compatible clients like Claude CLI or Gemini CLI.

### atr mcp serve

Start the MCP server for browser automation.

```bash
atr mcp serve [flags]
```

The server communicates via JSON-RPC 2.0 over stdio and exposes browser tools for navigation, interaction, and inspection.

#### Flags

| Flag | Description |
|------|-------------|
| `--headless` | Run browser in headless mode (default: `true`) |
| `--ignore-https-errors` | Ignore HTTPS certificate errors |

#### Available Tools (30)

| Tool | Description |
|------|-------------|
| `browser_navigate` | Navigate to a URL |
| `browser_go_back` | Navigate back |
| `browser_go_forward` | Navigate forward |
| `browser_reload` | Reload the page |
| `browser_new_page` | Open a new tab |
| `browser_list_pages` | List all open tabs |
| `browser_select_page` | Switch to tab by index |
| `browser_close_page` | Close tab by index |
| `browser_click` | Click on an element |
| `browser_fill` | Fill a form field |
| `browser_hover` | Hover over an element |
| `browser_press_key` | Press a key |
| `browser_drag` | Drag one element to another |
| `browser_wait` | Wait for element to appear |
| `browser_scroll` | Scroll within an element |
| `browser_get_url` | Get current page URL |
| `browser_get_title` | Get page title |
| `browser_get_html` | Get page HTML content |
| `browser_snapshot` | Get accessibility tree snapshot |
| `browser_screenshot` | Take a screenshot (page, element, or multi-element) |
| `browser_eval` | Execute JavaScript |
| `browser_computed_styles` | Get computed CSS styles |
| `browser_computed_styles_diff` | Compare styles across pages |
| `browser_text` | Extract text content |
| `browser_clean_snapshot` | Get cleaned DOM subtree |
| `browser_font_check` | Check font load status |
| `browser_viewport` | Get or set viewport size |
| `browser_download_images` | Download images from elements |
| `browser_ask` | Ask AI a question about the page |
| `browser_console` | Get console messages |
| `browser_network` | Get network requests |
| `browser_errors` | Get failed network requests |

#### Integration Examples

**With Claude CLI:**

```bash
# Inline configuration
claude -p "Navigate to example.com" \
  --mcp-config '{"mcpServers":{"atr-browser":{"command":"atr","args":["mcp","serve"]}}}' \
  --allowedTools "mcp__atr-browser__*"
```

**With Gemini CLI (project settings `.gemini/settings.json`):**

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

See [MCP Server Documentation](mcp-server.md) for detailed usage.

---

## atr config

Manage ATR configuration.

### atr config show

Display current configuration.

```bash
atr config show
```

Shows merged configuration from file, environment, and defaults.

### atr config init

Create a default configuration file.

```bash
atr config init
```

Creates `~/.atr/config.yaml` with default values and comments.

**Auto-Detection**: Automatically detects installed CLI tools (Claude CLI, Gemini CLI) and sets the first available as the default backend.

If the file already exists, you'll be prompted to confirm overwrite.

### atr config validate

Validate configuration.

```bash
atr config validate
```

Checks that:
- Required fields are present
- Backend is valid (`claude-cli`, `gemini-cli`, `gemini-api`, or `vertex-ai`)
- API credentials are configured (for API backends)
- CLI tools are available in PATH (for CLI backends)
- Model tier is valid (for API backends)

---

## atr test

Test LLM connectivity.

```bash
atr test
```

Sends a simple prompt to the configured LLM backend and verifies a response is received. Use this to validate your API key or Vertex AI setup before running actual commands.

Output:
```
Testing LLM connectivity...
  Backend: vertex-ai
  Model: gemini-3-flash-preview

LLM Response: Hello from ATR!
Response time: 1.2s

LLM connectivity test passed!
```

---

## atr test-cmd-env

Test environment detection for a command without executing it.

```bash
atr test-cmd-env "<command>" [flags]
```

Analyzes a command to show which Python/Node.js environments would be activated.

### Flags

| Flag | Description |
|------|-------------|
| `--cwd <path>` | Working directory for environment detection |
| `--python-venv <path>` | Override Python venv path |
| `--nvm-version <version>` | Override Node.js version |
| `--no-auto-env` | Disable automatic environment detection |

### Examples

```bash
# Test what environments a command would use
atr test-cmd-env "pytest tests/"
atr test-cmd-env "npm run build"
atr test-cmd-env "make test"

# Test with specific working directory
atr test-cmd-env "pytest" --cwd /path/to/project

# Test with manual venv override
atr test-cmd-env "python script.py" --python-venv /path/to/.venv
```

### Output

```
Command: pytest tests/
Working directory: /path/to/project

Detection method: LLM
Analysis:
  needs_python: true
  needs_node: false
  reasoning: pytest is a standard Python testing framework.

Python environment:
  Status: FOUND (will be activated)
  Type: python-venv
  Path: /path/to/project/.venv
  Activate: source /path/to/project/.venv/bin/activate
Node.js environment:
  Status: NOT NEEDED

Final command would be:
  source /path/to/project/.venv/bin/activate && pytest tests/
```

---

## atr version

Display version information.

```bash
atr version
```

Output:
```
atr version dev-abc1234
```

---

## atr update

Check for and install updates.

```bash
atr update [flags]
```

Downloads and installs the latest version of ATR from GitHub releases. Dev versions auto-update every 2 days on startup (configurable).

### Flags

| Flag | Description |
|------|-------------|
| `--check` | Only check for updates, don't install |
| `--force` | Force update even if already on latest version |

### Examples

```bash
# Check for updates without installing
atr update --check

# Update to latest version
atr update

# Force update even if already up to date
atr update --force
```

### Auto-Update Configuration

Dev versions automatically check for updates on startup. Configure in `~/.atr/config.yaml`:

```yaml
update:
  auto_update_dev: true   # Enable auto-update for dev versions (default: true)
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Everything passed |
| `1` | The thing under test is broken — a failed command, or a behaviour test whose assertion did not hold |
| `2` | The run could not decide — a missing input, a stale or absent compiled script under `--no-compile`, a compiled script that cannot fail, a browser that would not start, an unreachable model |

`1` means the application misbehaved and nothing else, so a red build has a
single meaning. Everything that says nothing about the application is `2`,
which a CI job can retry rather than escalate. When one spec fails an assertion
and another hits an infrastructure problem, the run exits `1`: a real
regression is never masked by a flaky neighbour.

---

## atr history

Report on behaviour test runs recorded locally in `~/.atr/history.db`.

```bash
atr history                                     # per spec, over the last 30 days
atr history --spec tests/login.test.txt --runs  # individual runs for one spec
atr history --since 7d --json
```

#### Flags

| Flag | Description |
|------|-------------|
| `--spec <path>` | Only this spec, named the way `atr history` lists it (repository-relative) |
| `--since <window>` | `90m`, `24h`, `30d` (default `30d`) |
| `--runs` | List individual runs rather than a per-spec summary |
| `--limit <n>` | How many runs `--runs` lists (default 20) |
| `--json` | Emit JSON |

The summary separates three things a general-purpose reporter folds together:

| column | meaning |
|--------|---------|
| `FAIL` | the application misbehaved — the only thing that exits `1` |
| `INFRA` | the run never reached the application, so it is excluded from the true failure rate |
| `FLAKE` | the run passed, but only after a retry — green in every other report |
| `REPAIRS` | how often the script was rewritten. A spec repaired repeatedly means the application's DOM is churning, not that the test is flaky |
| `REPLAY` | median duration of runs with no model in the loop. Those are deterministic, so a rising number means the *application* got slower |

The database is plain SQLite and the views (`runs`, `attempts`, `compiles`) are
a stable contract, so anything this command will not tell you is one query
away:

```bash
sqlite3 ~/.atr/history.db "SELECT spec, outcome, count(*) FROM runs GROUP BY 1,2"
```

Point ATR at an OTLP collector and the same runs export as OpenTelemetry
traces, metrics and logs — which is how a CI run's history survives the
container being torn down. In precedence order: `--otel-endpoint`, then
`OTEL_EXPORTER_OTLP_ENDPOINT`, then `telemetry.endpoint` in the config file.
Give the collector's base URL; the signal path is appended. With no endpoint
anywhere, nothing is emitted and no error is logged.

Configure in `~/.atr/config.yaml`; both sinks may be disabled, including at
once:

```yaml
history:
  enabled: true
  path: ""            # default ~/.atr/history.db
  keep_days: 90
telemetry:
  enabled: true                       # inert unless an endpoint is configured
  endpoint: "http://localhost:4318"   # or OTEL_EXPORTER_OTLP_ENDPOINT
  service_name: atr
  shutdown_timeout: 5s
```

ATR never records a resolved value in a field of its own. A value can appear
inside a failure message, because the message quotes the application and the
spec's own expectations back at you; whether messages leave the machine is
governed by whether you export the logs signal.

---

## Shell Completion

ATR supports shell completion for bash and zsh.

### atr install-completion

Automatically install shell completion for your current shell.

```bash
atr install-completion
```

This command:
- Auto-detects your shell (bash or zsh)
- Installs completion to the appropriate location based on permissions:
  - **With sudo**: System-wide (`/etc/bash_completion.d/` or `/usr/local/share/zsh/site-functions/`)
  - **Without sudo**: User directory (`~/.bash_completion.d/` or `~/.zsh/completions/`)

After installation, follow the printed instructions to enable completions in your shell.

### Manual Installation

If you prefer manual setup:

#### Bash

```bash
# Add to ~/.bashrc
source <(atr completion bash)
```

#### Zsh

```bash
# Add to ~/.zshrc
source <(atr completion zsh)
```

#### Fish

```bash
atr completion fish | source
```

#### PowerShell

```powershell
atr completion powershell | Out-String | Invoke-Expression
```

---

## Common Workflows

### Debug a Failing Test

```bash
# Run tests with analysis
atr run --cmd "go test ./... -v" --context "Tests started failing after recent changes"
```

### Run E2E Tests

```bash
# Run all behavior tests
atr run --behavior tests/e2e/ --browser-url http://localhost:3000

# Run specific test with visible browser
atr run --behavior tests/e2e/checkout.test.txt --headless=false
```

### CI/CD Integration

```bash
# In CI pipeline
export GEMINI_API_KEY="${{ secrets.GEMINI_API_KEY }}"
atr run --cmd "npm test" || exit 1
```

### Use Different Models

```bash
# Quick analysis with flash model (default)
atr run --cmd "make build" --model flash

# Deep analysis with pro model
atr run --cmd "make build" --model pro
```

---

## Environment Variables

See [Configuration](configuration.md#environment-variables) for the complete list.

Quick reference:
```bash
export GEMINI_API_KEY="your-key"           # Gemini API
export GOOGLE_CLOUD_PROJECT="project-id"   # Vertex AI
export ATR_MODEL="pro"                     # Use pro model
```

## Compiled behavior tests

`atr run --behavior` compiles a spec to JavaScript on first run and replays it
after, so a passing run costs no model calls. See
[Compiled Behavior Tests](behavior-compilation.md).

| Flag | Effect |
|------|--------|
| *(none)* | Compile if needed, replay, repair on drift |
| `--recompile` | Regenerate the script even if it matches the spec |
| `--no-compile` | Replay only, never call the model. Fails if a script is missing or stale — use in CI |
| `--no-repair` | Diagnose drift but leave the script alone |
| `--prune-values` | Remove inputs neither the script nor `_shared.js` reads |
| `--lint <mode>` | `error` (default), `warn`, `off` for the cannot-fail check |
| `--no-triage` | Never ask the model why a failure happened |
| `--interpret` | Skip compilation; the agent drives every step |
