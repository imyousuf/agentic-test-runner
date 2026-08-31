# `atr remote` — Browser Live View

Status: draft for review
Command: `atr remote --port 7788`

## 1. Summary

Add a command that serves a live view of the browser that ATR drives.

```
atr remote --port 7788
```

The command starts a small web server. The server attaches to ATR's browser as a second CDP
session. Chrome streams the page as JPEG frames. A React application draws the frames and
sends mouse and keyboard events back. ATR serves the web page from its own binary.

You then watch the agent work, and you take over for a step that needs a person, such as a
login with multi-factor authentication.

## 2. Why

The agent often runs on a server with no display. You cannot see its browser, and you cannot
finish a manual step. Today the only options are VNC or a desktop, and both need packages on
the server.

This command needs no X server, no VNC, and no extra packages. Chrome performs the capture
and the JPEG encoding.

## 3. Flow diagrams

### 3.1 Where the command sits in ATR

```
                       ┌──────────────────────────────────────────┐
  atr run --cmd  ──────│ execute, then an LLM agent on failure    │
                       └──────────────────────────────────────────┘
                       ┌──────────────────────────────────────────┐
  atr run --behavior ──│ parse the test, then an LLM agent loop   │──┐
                       └──────────────────────────────────────────┘  │
                       ┌──────────────────────────────────────────┐  │
  atr browser <cmd> ───│ HTTP client ──▶ :9333 API server         │──┤
                       └──────────────────────────────────────────┘  │
                       ┌──────────────────────────────────────────┐  │
  atr mcp serve ───────│ MCP over stdio ──▶ in-process rod        │──┤
                       └──────────────────────────────────────────┘  │
                                                                     ▼
                                                            ┌────────────────┐
  atr remote  (new) ──────┐                                    │    Chrome      │
                       │  attach as a second CDP session ──▶│                │
                       │  screencast out, input in          └────────────────┘
                       └──────────────────────────────────────────
```

`atr remote` adds no code to the existing paths. It observes and drives the same browser.

### 3.2 Startup and discovery

```
  atr remote --port 7788
        │
        ├─▶ --attach flag set?            ── yes ─▶ use it
        │        no
        ├─▶ ATR_CDP_ENDPOINT set?         ── yes ─▶ use it
        │        no
        ├─▶ ~/.atr/browser.state          ── has cdp_endpoint ─▶ use it
        │        no
        ├─▶ --start given?                ── yes ─▶ launch a browser, owned = true
        │        no
        └─▶ exit with advice: "run atr browser start first"
                 │
                 ▼
        rod.New().ControlURL(cdp) ─▶ Connect
                 │
                 ▼
        list page targets ─▶ choose the active one
                 │
                 ▼
        Page.bringToFront          (a background tab sends no frames)
                 │
                 ▼
        Page.startScreencast       (jpeg, quality, maxWidth, everyNthFrame)
                 │
                 ▼
        serve HTTP on 127.0.0.1:7788, print the URL with the token
```

### 3.3 The frame path

```
  Chrome            atr remote                         browser tab (React)
    │                  │                                  │
    │ screencastFrame  │                                  │
    ├─────────────────▶│                                  │
    │                  │ ack at once                      │
    │◀─────────────────┤                                  │
    │                  │                                  │
    │                  │ store in the per-viewer slot     │
    │                  │ (replace, never queue)           │
    │                  │                                  │
    │                  │ binary WebSocket message         │
    │                  ├─────────────────────────────────▶│
    │                  │                                  │ createImageBitmap
    │                  │                                  │ drawImage on canvas
```

### 3.4 The input path

```
  React                       atr remote                     Chrome
    │ pointerdown                 │                          │
    ├────────────────────────────▶│                          │
    │  {x,y in canvas pixels}     │ convert to page pixels   │
    │                             │ x = cx/cw * deviceWidth  │
    │                             ├─────────────────────────▶│
    │                             │ Input.dispatchMouseEvent │
    │                             │                          │
    │ keydown                     │                          │
    ├────────────────────────────▶│ Input.dispatchKeyEvent   │
    │                             ├─────────────────────────▶│
    │ paste                       │                          │
    ├────────────────────────────▶│ Input.insertText         │
    │                             ├─────────────────────────▶│
```

### 3.5 The foreground conflict

```
   agent switches tab                    your view goes silent
          │                                       │
          ▼                                       ▼
   Page.bringToFront(other)  ──▶  Chrome stops compositing your tab
                                              │
                                   no frames, and no error
                                              │
                              watchdog: 2 s without a frame
                                              │
                    ┌─────────────────────────┴─────────────────────┐
                    ▼                                               ▼
         policy "follow the agent"                       policy "hold my tab"
         stream the new front tab                    bringToFront(your tab) again
```

## 4. Spike results

A spike ran before this document was finished, against a real Chrome. The code is in
`cmd/spike`, `cmd/spike2`, and `cmd/spike3` on this branch's working tree.

Verified:

| Check | Result |
|---|---|
| ATR builds from a clean clone on macOS | Yes, 37 s |
| Resolve `http://host/json/version` to a `ws://` URL | Works |
| Two CDP sessions on one browser | Works |
| Attach to the browser that **ATR launched**, using `cdp_endpoint` from the state file | Works |
| A second session lists ATR's pages | Works |
| Screencast of an ATR-owned page | Works, 30 frames in 3 s |
| ATR keeps driving its browser while a viewer is attached | Works |
| Click at a computed coordinate | Works, the page navigated |
| `Input.insertText` | Works |

Measured, five second samples:

| Scenario | Frame rate | Bandwidth | Average frame |
|---|---|---|---|
| Animated page, foreground tab | 58 fps | 4.2 to 5.2 Mbit/s | 9 to 11 kB |
| Animated page, background tab | **0 fps** | 0 | none |
| After `Page.bringToFront` | 58 fps | 4.2 Mbit/s | 9 kB |
| Static page, foreground tab | 0.2 fps | 0.01 Mbit/s | 8.5 kB |

Two conclusions:

**A background tab sends nothing.** Chrome stops compositing a tab that is not in front. The
stream then stops with no error. Section 8 defines the response.

**An idle page is almost free.** A static page sent one frame in five seconds. A form that
waits for a person costs no bandwidth.

**No state file change is needed.** `BrowserState` already carries `cdp_endpoint`
(`internal/api/state.go:24`), and both `Launch()` and `Connect()` record the control URL.

## 5. Command

```
atr remote [flags]
```

| Flag | Default | Purpose |
|---|---|---|
| `--port` | `7788` | The HTTP port. |
| `--bind` | `127.0.0.1` | The listen address. |
| `--token` | from `ATR_REMOTE_TOKEN` | The access token. One is generated when empty. |
| `--attach` | discovered | A CDP endpoint. |
| `--start` | `false` | Launch a browser when none runs. |
| `--view-only` | `false` | Drop all input on the server. |
| `--quality` | `auto` | JPEG quality, 1 to 100, or `auto`. |
| `--max-width` | `1600` | The largest frame width. |
| `--fps` | `20` | The target frame rate. It sets `everyNthFrame`. |
| `--open` | `false` | Open the page in the local browser. |
| `--output` | `~/.atr/recordings` | Where the page reads and writes recordings. |

Output:

```
ATR live view
  URL:     http://127.0.0.1:7788/?t=8f2c...
  Browser: Chrome/151.0.7922.170  (attached, not owned)
  Pages:   2
  Record:  off, press ● in the page (~/.atr/recordings)
```

The command never records on its own. It only gives the page the ability to
start a recording, and to browse the recordings that already exist. See
[`docs/session-recording.md`](./session-recording.md).

## 6. Protocol

One WebSocket at `/ws`. Binary messages carry frames. Text messages carry control.

### Server to client, binary

```
byte 0      : 0x01 = frame
bytes 1-4   : header length, uint32 big endian
bytes 5..n  : JSON header
bytes n+1.. : JPEG bytes
```

Header fields: `seq`, `width`, `height`, `deviceWidth`, `deviceHeight`, `scrollX`,
`scrollY`, `pageScale`, `timestamp`.

### Server to client, text

```json
{"t":"pages","pages":[{"id":"A1","title":"Login","url":"https://…","active":true}]}
{"t":"status","viewers":1,"streaming":true,"fps":18,"canRecord":true}
{"t":"error","message":"the page closed"}
{"t":"record","recording":true,"id":"20260831-142530-login","elapsedMs":8100,"frames":42,"bytes":918273,"dropped":0}
```

`canRecord` is false when the command runs with `--view-only`, or when the
recordings directory cannot be opened. The `record` message repeats once a
second while a recording runs, and once when it starts or stops.

### Client to server, text

```json
{"t":"mouse","kind":"pressed","x":412,"y":233,"button":"left","clicks":1,"mod":0}
{"t":"wheel","x":412,"y":233,"dx":0,"dy":-120}
{"t":"key","kind":"down","key":"Enter","code":"Enter","vk":13,"text":"","mod":0}
{"t":"text","value":"user@example.com"}
{"t":"selectPage","id":"A1"}
{"t":"navigate","url":"https://example.com"}
{"t":"policy","foreground":"hold"}
```

### Coordinates

The client converts canvas pixels to page pixels:

```
pageX = canvasX / canvasWidth * frame.deviceWidth
```

The server does not scale. The spike proved this conversion by clicking a link.

## 7. Frame handling

- Acknowledge every frame at once. Chrome stops the stream without the acknowledgement.
- Do not wait for a viewer before the acknowledgement.
- Keep one frame for each viewer. Replace it when a newer frame arrives. A slow viewer sees
  the newest frame and never a backlog.
- Set `everyNthFrame` from `--fps`. The spike measured 58 fps, which is more than a person
  needs. A target of 15 to 20 fps cuts about 5 Mbit/s to about 1.5 Mbit/s.

## 8. Tabs and the foreground rule

The web application draws the tab bar. Chrome's tab strip is not in the stream.

The spike proved that a background tab sends no frames. Three consequences follow, and
Chrome cannot be persuaded otherwise.

1. Only one tab can stream at a time.
2. The streamed tab must be the foreground tab.
3. The agent and the viewer compete for the foreground.

Response:

- A watchdog reports silence after two seconds as `{"t":"status","streaming":false}`.
- The application shows a banner: `Another tab moved to the front.`
- The viewer picks a policy:
  - **Follow the agent.** Stream whichever tab is in front. Good for watching. The default.
  - **Hold my tab.** Call `bringToFront` on your tab again. Good for typing.

## 9. Input mapping

CDP accepts the values that a browser key event already provides.

| Browser event | CDP field |
|---|---|
| `event.key` | `key` |
| `event.code` | `code` |
| `event.keyCode` | `windowsVirtualKeyCode` |
| printable text | `text`, plus a `char` event |

Rules:

- Send `keyDown`, then `char` for a printable key, then `keyUp`.
- Build `modifiers`: 1 Alt, 2 Control, 4 Meta, 8 Shift.
- Use `Input.insertText` for a paste. Do not send one event for each character.
- The client calls `preventDefault` for keys that its own browser would take.

No server-side keymap edit is needed. This is far simpler than X11.

## 10. Security

A viewer gets full control of a browser and its cookies.

- Bind `127.0.0.1` by default. Reach it through an SSH tunnel.
- Require a token. Generate one at start when none is given, and print it in the URL.
- Accept the token as a `Bearer` header, or once as a WebSocket query parameter.
- Refuse a non-loopback bind when the token is empty.
- Check the `Origin` header on the upgrade.
- `--view-only` drops input on the server, not in the client.

## 11. Package layout

```
internal/cli/remote.go            the cobra command
internal/remote/server.go         HTTP, the WebSocket, and the static files
internal/remote/screencast.go     the CDP screencast and the acknowledgement loop
internal/remote/input.go          the event mapping
internal/remote/hub.go            viewers and the frame fan-out
internal/remote/sink.go           the interface every frame consumer implements
internal/remote/discover.go       the endpoint discovery order
internal/remote/recording.go      the recorder sink and the record session
internal/remote/recordings_api.go the recording and library routes
web/                              the React source and the build output
```

The streamer fans a frame out to every sink. `Hub` is the sink that feeds the
viewers. `RecorderSink` is the sink that feeds the disk. Neither knows about
the other.

No change is needed in `internal/mcp`, `internal/agent`, `internal/api`, or
`internal/browser`.

## 12. Web application

Stack: Vite, React 19, and TypeScript. No user interface framework.

| Component | Purpose |
|---|---|
| `App` | The socket, the state, the route, and the reconnection. |
| `Viewport` | The canvas. It draws frames and captures input. |
| `FrameCanvas` | The draw loop. Both the viewport and the player use it. |
| `TabBar` | The page list. |
| `UrlBar` | The current URL and navigation. |
| `StatusBar` | Frame rate, latency, viewers, and the streaming state. |
| `RecordButton` | Start and stop a recording. It is hidden when `canRecord` is false. |
| `Library` | The list of recordings. |
| `Player` | Playback of one recording. |

Rules:

- One `<canvas>`. Decode with `createImageBitmap`, which runs off the main thread.
- Draw inside `requestAnimationFrame`. Draw the newest frame only.
- Do not build a data URL for each frame.
- Throttle `pointermove` to one message for each animation frame.

Build:

- `make web` runs `npm ci && npm run build` into `web/dist`.
- `//go:embed all:web/dist` puts the result in the binary.
- A `noweb` build tag serves a short message, so a contributor without Node can build the Go
  code.

## 13. Test plan

- Unit: the coordinate conversion at several scale factors.
- Unit: the modifier bit field for every combination.
- Unit: the hub replaces a queued frame and never grows.
- Integration: attach, stream, and receive ten frames.
- Integration: a canvas click changes the page URL.
- Integration: the watchdog reports silence when another tab moves to the front.
- Integration: two viewers both receive frames.
- Manual: a real login through the view.

## 14. Phases

| Phase | Content | Result |
|---|---|---|
| 1 | Discovery, attach, screencast, WebSocket, canvas | You can watch the page. |
| 2 | Mouse, keyboard, and paste | You can finish a login. |
| 3 | Tab bar, URL bar, watchdog, and the foreground policy | You can manage pages. |
| 4 | Token, origin check, and `--view-only` | Safe to leave running. |
| 5 | Adaptive quality and the status bar | Comfortable on a slow link. |

Phases 1 and 2 are about 1000 lines of Go and 800 lines of React.

## 15. Confidence

The spike replaced estimates with measurements.

| Part | Before | After |
|---|---|---|
| Builds and integrates with ATR | 90 | 98 |
| Discovery from the state file | 95 | 99 |
| Screencast loop | 80 | 95 |
| Two CDP sessions, agent unaffected | 85 | 97 |
| Coordinate maths | 70 | 92 |
| Input mapping, mouse and text | 85 | 90 |
| Latency over a tunnel | 65 | 70 |
| Keyboard corner cases | 60 | 60 |
| Tab and foreground handling | 80 | 75 |

**Overall for phases 1 and 2: 90 out of 100.**

## 16. Open questions

1. Should `atr remote` start a browser when none runs? The spec keeps `--start` off by default.
2. Should phase 1 include "Pause the agent"? The foreground conflict makes it more useful
   than it first appeared.
3. ~~Do you want `atr view` as an alias?~~ Answered. The command is `atr remote`, and it
   keeps `view` and `rdp` as aliases. RDP is the name of a Microsoft protocol, and this
   command uses CDP and shows a page, not a desktop.
4. Should input force the foreground? A click reaches a background tab, but you cannot see
   the result.
