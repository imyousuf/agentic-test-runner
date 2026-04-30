# Computer Server Mode

ATR provides cross-platform desktop control through a daemon — the computer server — that exposes mouse, keyboard, screen capture, window/app management, and an in-process LLM agent over an HTTP REST API.

The computer daemon is a **separate process** from the browser daemon. The two listen on different ports (computer: 9334, browser: 9333) and can run simultaneously. Both are driven from the same `atr` CLI; both expose tools to the same MCP server (`atr mcp serve`).

## Overview

```
┌─────────────┐   CLI calls    ┌─────────────┐   HTTP    ┌──────────────────────┐
│  AI agent   │ ─────────────▶ │   atr CLI   │ ────────▶ │ ATR Computer Daemon  │
│  / human    │ ◀───────────── │  (client)   │ ◀──────── │ + robotgo + xgbutil  │
└─────────────┘   stdout       └─────────────┘   JSON    └──────────────────────┘
```

Key features:
- **Daemon mode**: Runs as a background process; state at `~/.atr/computer.state`, log at `~/.atr/computer.log`.
- **Configurable safety countdown** before each action (`per-request` / `per-app` / `off`). Abort with Ctrl+C.
- **Optional GUI overlay** via native dialogs (`zenity` on Linux, `osascript` notification on macOS, PowerShell toast on Windows). Falls back to terminal-only if no backend is available.
- **Multi-monitor aware**: API uses **root coordinates** (origin at the bounding box of all monitors). Mouse tools accept an optional `display` field for display-local pixels.
- **In-process LLM agent**: `POST /api/v1/computer/ask` runs a screenshot↔LLM↔tool loop until a natural-language goal is achieved.

> **Linux note:** v1 is X11 only. Wayland is tracked as future work.

## Quick Start

```bash
atr computer start
atr computer status
atr computer screenshot --output /tmp/desktop.png
atr computer click --display 0 100 200
atr computer ask "open xclock and tell me the time"
atr computer stop
```

## Configuration

The daemon reads `~/.atr/config.yaml` (see [Configuration](configuration.md)):

```yaml
computer:
  enabled: true
  port: 9334
  countdown:
    mode: per-request    # per-request | per-app | off
    seconds: 3
  gui:
    enabled: true
  display: 0
```

All keys are also reachable via env vars (`ATR_COMPUTER_*`) and as CLI flags on `atr computer start`.

## REST API

All endpoints live under `/api/v1/computer/`. Responses follow the standard ATR shape:

```json
{ "success": true,  "data": { ... } }
{ "success": false, "error": "..." }
```

### Lifecycle

| Method | Path | Body | Notes |
|--------|------|------|-------|
| `GET` | `/health` | — | Returns running, endpoint, mode, approved-app count |
| `POST` | `/shutdown` | — | Graceful shutdown |
| `POST` | `/approvals/clear` | — | Clear per-app approval cache |

### Perception (passive — no countdown)

| Method | Path | Body | Returns |
|--------|------|------|---------|
| `POST` | `/screenshot` | `{ display, region, x, y, width, height }` | `{ image_base64, size_bytes, format: "png" }` |
| `GET` | `/displays` | — | `{ primary: {width, height}, displays: [{index, bounds: {Min, Max}, primary}] }` |
| `GET` | `/position` | — | `{ x, y }` (root coords) |
| `GET` | `/windows` | — | `{ windows: [...], count }` (root coords for bounds) |
| `GET` | `/window/active` | — | Single window object |

`/screenshot` body fields:
- `display` (int, optional): which display to capture; defaults to configured default.
- `region` (bool): if true, treat `x, y, width, height` as a region within the display.
- `x, y` (int): top-left of region (display-local pixels).
- `width, height` (int): region size; both must be > 0 when `region: true`.

### Actuation (gated — countdown runs)

| Method | Path | Body |
|--------|------|------|
| `POST` | `/click` | `{ x, y, button: "left"\|"right"\|"center", double, display? }` |
| `POST` | `/move` | `{ x, y, smooth, display? }` |
| `POST` | `/drag` | `{ from_x, from_y, to_x, to_y, button, display? }` |
| `POST` | `/scroll` | `{ dx, dy }` (cursor position; no display) |
| `POST` | `/hover` | `{ x, y, display? }` |
| `POST` | `/type` | `{ text, delay_ms }` |
| `POST` | `/key` | `{ key }` |
| `POST` | `/chord` | `{ chord }` |
| `POST` | `/window/focus` | `{ id }` |
| `POST` | `/window/state` | `{ id, state: "minimize"\|"maximize"\|"restore"\|"close" }` |
| `POST` | `/window/move` | `{ id, x, y }` |
| `POST` | `/window/resize` | `{ id, width, height }` |
| `POST` | `/app/launch` | `{ name }` |
| `POST` | `/app/quit` | `{ name }` |

When a request is aborted via Ctrl+C / SIGINT, the response is HTTP `499` with `error: "aborted by user"`.

### Agent

| Method | Path | Body | Returns |
|--------|------|------|---------|
| `POST` | `/ask` | `{ instruction, max_steps?, timeout_seconds? }` | `{ answer, duration_ms, backend, model }` |

Runs an in-process LLM loop using the daemon's configured backend (gemini-api / vertex-ai / claude-cli). The agent screenshots the desktop, calls `computer_*` tools, and iterates until the goal is achieved or `max_steps` (default 20) is hit.

The agent **cannot type passwords**. If a sudo / polkit / authentication prompt appears it stops and reports the blocker.

Default model aliases used by `/ask`:

| Tier  | Model                          |
|-------|--------------------------------|
| flash | `gemini-3.1-flash-preview`     |
| pro   | `gemini-3.2-pro-preview`       |

Backend `claude-cli` ignores the model alias and uses the Claude CLI subprocess via MCP.

## Coordinate Model

The API uses **root coordinates** for all mouse and window operations. Root coords are the X11 root window coordinate system: origin (0, 0) is at the top-left of the bounding box of all monitors, and every visible coordinate is non-negative.

`GET /displays` returns each display's bounds in root coords. For example, on a 2-monitor layout with a vertical secondary at root (0, 0)–(1440, 2560) and a landscape primary at root (1440, 0)–(4000, 1440):

```json
{
  "primary": { "width": 4000, "height": 2560 },
  "displays": [
    { "index": 0, "bounds": {"Min": {"X": 1440, "Y": 0}, "Max": {"X": 4000, "Y": 1440}}, "primary": true },
    { "index": 1, "bounds": {"Min": {"X": 0,    "Y": 0}, "Max": {"X": 1440, "Y": 2560}}, "primary": false }
  ]
}
```

Mouse endpoints accept a `display` field. When set, `x` and `y` are interpreted as display-local pixels relative to that display's top-left and translated to root before being sent to robotgo.

`/screenshot` always uses display-local pixels for the optional `region` (it implicitly knows which display's origin to add).

## Companion Daemon — `atr browser`

For browser automation, see [Browser Server Mode](browser-server.md). The two daemons are independent but designed to be used together — for example, a single `atr computer ask` run can shell out to `atr browser navigate` and back.
