---
name: atr-computer
description: Control the desktop (mouse, keyboard, screen, windows, apps) cross-platform via ATR. Take screenshots, click at coordinates, type text, drag, manage windows, launch and quit apps. Includes 'atr computer ask' — an in-process LLM agent that takes a natural-language instruction and drives the desktop end-to-end. Use for native UI testing, desktop automation, multi-monitor flows, or anything outside the browser. Pairs with atr-browser for combined web + desktop workflows.
allowed-tools: Bash(atr computer:*)
---

> **Companion skill — `atr-browser`** controls Chromium for any in-page operation (navigate, click on web elements, fill forms, scrape). Load both when:
>
> - You need to drag a file from the OS file manager into a browser upload zone.
> - Authentication or a native dialog (1Password, system file picker) is part of the flow.
> - You're delegating a high-level task that touches both — `atr computer ask "..."` can shell out to `atr browser ...` and vice versa.
>
> Computer daemon listens on port 9334; browser daemon on 9333. Both can run simultaneously.

# ATR Desktop Computer-Use Skill

This skill provides cross-platform desktop control through ATR's computer daemon. Mouse, keyboard, screen capture, window management, app launch, and an in-process LLM agent (`atr computer ask`) — all driven from the same `atr` CLI that powers `atr-browser`.

## Architecture

```
Claude Code --> atr CLI --> ATR Computer Daemon --> robotgo (X11 / macOS / Windows)
```

The daemon enforces a configurable safety countdown before every action so the user can intervene with Ctrl+C. Read-only operations (screenshot, position, list windows) skip the countdown.

When the daemon is started without `--no-gui` it also surfaces the countdown in a small native overlay so MCP/Claude-Code clients (which don't see the daemon's terminal) can still interrupt visually. On Linux the overlay uses `zenity` (full Cancel button — clicking it aborts the action with `ErrAborted`); when only `notify-send` is available it falls back to a one-shot desktop notification (visual only — abort still works via Ctrl+C in the daemon terminal). On macOS it uses `osascript display notification`; on Windows it uses a PowerShell toast. If no backend can initialize, the daemon logs a warning and continues with terminal-only countdown.

> **Linux:** X11 only in v1. Wayland is not supported yet.
> **GUI overlay:** install `zenity` for an abortable dialog (`apt install zenity`); fall back to `libnotify-bin` for visual-only notifications.

## Getting Started

### Step 1: Check status and start if needed

```bash
atr computer status
```

If not running:

```bash
atr computer start                           # default: per-request 3s countdown
atr computer start --countdown-mode per-app  # countdown only on first action per app
atr computer start --countdown-mode off      # no countdown (trusted batch runs)
atr computer start --countdown 1             # 1-second countdown
```

State and logs:
- State: `~/.atr/computer.state`
- Daemon log (where countdown messages appear): `~/.atr/computer.log`

### Step 2: Take a screenshot

```bash
atr computer screenshot --output /tmp/desk.png
atr computer screenshot --output /tmp/region.png --region 0,0,800,600
```

### Step 3: Drive mouse and keyboard

```bash
atr computer click 800 50
atr computer click 800 50 --double
atr computer click 800 50 --button right
atr computer move 100 100
atr computer drag --from 200,300 --to 1200,400
atr computer scroll --dy -3
atr computer hover 500 500

atr computer type "Hello, world"
atr computer key enter
atr computer chord ctrl+shift+t
```

### Step 4: Manage windows

```bash
atr computer --json window list                          # enumerate
atr computer window active                               # focused window
atr computer window focus --title "Firefox"              # by title or app name
atr computer window minimize --title "Slack"
atr computer window maximize --title "Code"
atr computer window restore --id 12345
atr computer window close --title "Calculator"
atr computer window move --id 12345 --to 100,100
atr computer window resize --id 12345 --size 800,600
```

### Step 5: Launch / quit apps

```bash
atr computer app launch firefox
atr computer app launch xclock
atr computer app quit firefox
```

### Step 6: Stop the daemon

```bash
atr computer stop
```

## Safety countdown modes

| Mode          | Behavior                                                                  |
|---------------|---------------------------------------------------------------------------|
| `per-request` | Default. Every gated action shows a countdown.                            |
| `per-app`     | First action against an app prompts; subsequent actions auto-approve.     |
| `off`         | No countdown. Explicit opt-in for trusted batches.                        |

Aborting: press Ctrl+C in the daemon's terminal during a countdown. The action returns `aborted by user`.

## Combined workflow with `atr-browser`

ATR can drive both desktop and browser in one flow. Example: end-to-end drag-and-drop between the OS file manager and a browser upload zone.

```bash
atr browser start
atr browser navigate https://example.com/upload

atr computer start --countdown-mode off
atr computer app launch nautilus
atr computer window focus --title "Files"
atr computer drag --from 200,300 --to 1200,400   # drag file into browser

atr browser screenshot --output /tmp/dnd.png
```

For browser-only workflows, see the `atr-browser` skill. For combined workflows, both daemons can run simultaneously on different ports (browser: 9333, computer: 9334).

## Asking the agent (`atr computer ask`)

Instead of issuing one command at a time, you can describe a desktop task in plain language and let an agent loop drive the screenshots, clicks, and keystrokes:

```bash
atr computer ask "open xclock and tell me what time it shows"
atr computer ask "list the open windows that contain 'chrome' in the title"
atr computer ask --max-steps 30 --timeout 10m "open the GNOME calculator and compute 17 * 23"
```

The agent uses the LLM backend configured for ATR (`backend: gemini-api | vertex-ai | claude-cli` in `~/.atr/config.yaml`). Default model aliases:

| Tier  | Model                          |
|-------|--------------------------------|
| flash | `gemini-3.7-flash`            |
| pro   | `gemini-3.1-pro-preview`      |

Override per-run with the global `--model` flag (e.g. `atr --model pro computer ask "..."`). With `backend: claude-cli`, the model flag is ignored and Claude CLI is invoked as a subprocess.

**Tools the agent can call** (curated subset):
- Perception: `computer_screenshot` (image fed back to the LLM), `computer_displays`, `computer_active_window`, `computer_list_windows`, `computer_position`
- Actuation: `computer_click`, `computer_type`, `computer_press_key`, `computer_key_chord`, `computer_focus_window`, `computer_window_state` (minimize/maximize/restore/close), `computer_launch_app`

**Limits and gotchas:**
- The agent **cannot type passwords** — if a sudo / polkit prompt appears, it stops and reports the blocker.
- Each screenshot sent to the LLM costs vision tokens; with `--model pro` a 10-step task is measurable spend.
- The countdown gate inherits whatever the daemon was started with. For ask runs you usually want `atr computer start --countdown-mode per-app` so the first action against an app prompts and subsequent ones auto-approve.

## Coordinates and multi-monitor

ATR uses **root coordinates** as the public API for clicks, moves, drags, and window positions. Root coords have a single bounding-box origin at (0, 0) covering all monitors — every visible coordinate is non-negative, matching `xrandr` and the X11 root window. `atr computer --json window list` and `atr computer displays` both return root coords.

For interactive use it's often easier to think in display-local pixels. Every mouse command accepts `--display N`, which interprets `x` and `y` as pixels relative to that display's top-left:

```bash
# Display 0 is primary (whose root origin may NOT be (0, 0) on multi-monitor setups)
atr computer click --display 0 100 200      # display-local
atr computer click 1540 200                  # root coords (equivalent on a setup where primary is at root (1440, 0))
```

`atr computer screenshot --region X,Y,W,H` is always display-local within the chosen `--display`.

A common workflow:

```bash
atr computer displays                                # see each display's root bounds
atr computer --json window list                      # find a window's root bounds
atr computer screenshot --display 0 --region X,Y,W,H # crop using display-local pixels
atr computer click --display 0 X Y                   # click the spot you cropped
```

## Tips

- Use `atr computer --json window list` to enumerate windows, then operate on a specific ID for stability.
- For Linux, the per-app cache key is `WM_CLASS` (window class), so all Chrome windows share one approval.
- Screenshots return PNG bytes; set `--output` to a local path the user expects.
- For multi-monitor setups: prefer `--display N` with display-local pixels for new code; reach for absolute root coords only when relaying values from `window list`.
