# `atr record` — Session Recording

Status: draft for review
Command: `atr record --output run/`
Depends on: `internal/remote` (the `atr remote` live view). The rename from `rdp` to
`remote` is a prerequisite. This document uses the new names.

## 1. Summary

Record the browser that ATR drives, then watch the result in the `atr remote` web page.

```
atr record                       record from the command line
atr remote                       press ● Record in the page
atr remote  ▸ Recordings         browse and play what you recorded
```

The recorder attaches to the running browser as a DevTools session. Chrome streams the page
as JPEG frames. The recorder writes every frame to disk with its timestamp. The web page
plays those frames back on a canvas, with the real timing.

**Recording is never on by default.** A person starts it in three ways, and each one is
explicit: the `atr record` command, the ● Record button, or `--record` on a behavior run.

## 2. Why

ATR captures one screenshot on failure (`internal/capture/capture.go:49`). It shows the end
state, not the three steps that produced it.

ATR also records interactions (`internal/browser/recorder.go`), which produces a replayable
`.test.txt`. It does not show what the page looked like.

A recording answers what neither answers: what did the agent see, and when?

`atr remote` already solved the hard part. It runs a CDP screencast and fans frames out to
viewers. A recorder is one more consumer of the same frames.

## 3. The key decision: frames are the format

A recording is a directory of JPEG frames and a manifest. That is the artefact. MP4 is an
export.

This matters because the web page can play the frames directly. `Viewport.tsx` already
decodes a JPEG with `createImageBitmap` and draws it on a canvas. A player does the same
thing, and it reads the timing from the manifest instead of from a socket.

Three consequences follow:

1. **`ffmpeg` leaves the hot path.** Nothing needs it to record, and nothing needs it to
   watch. It runs only when somebody asks for a file to send to another person.
2. **A killed process still leaves a playable recording.** The frames are already on disk.
3. **The player can do things a video cannot.** It can skip a 30 second page load, jump to
   a tab switch, and show the URL at the playhead.

## 4. Shape

```
                      ┌──────────────┐
                      │  Streamer    │  one CDP screencast
    Chrome ──frames──▶│              │
                      └──────┬───────┘
                             │  fan out to sinks
                 ┌───────────┴────────────┐
                 ▼                        ▼
         ┌───────────────┐        ┌───────────────┐
         │  Hub          │        │  Recorder     │
         │  WebSocket    │        │  disk         │
         └───────┬───────┘        └───────┬───────┘
                 │                        │
                 │                  ~/.atr/recordings/<id>/
                 │                        │
                 ▼                        ▼
         ┌────────────────────────────────────────┐
         │  the web page                          │
         │    Live      canvas ◀── WebSocket      │
         │    Recordings  canvas ◀── HTTP + manifest │
         └────────────────────────────────────────┘
```

Capture never depends on the web server. `atr record` runs headless with no HTTP listener.
The web page adds control and playback on top.

## 5. Storage layout

```
~/.atr/recordings/
  20260831-142530/
    manifest.json
    frames.jsonl          written as it goes; manifest.json is built on stop
    frames/
      000001.jpg
      000002.jpg
    recording.live.json   present only while a recorder is writing this directory
    recording.mp4         only after somebody exports
```

`manifest.json`:

```json
{
  "version": 2,
  "id": "20260831-142530",
  "title": "",
  "startedAt": "2026-08-31T14:25:30.112Z",
  "stoppedAt": "2026-08-31T14:27:02.880Z",
  "durationMs": 92768,
  "browser": "Chrome/151.0.7922.170",
  "options": { "quality": 60, "maxWidth": 1600, "fps": 10, "policy": "follow",
               "refLagMs": 1000, "changeThreshold": 0.002,
               "dedupeEpsilon": 0.0005, "keepEveryMs": 2000 },
  "droppedFrames": 0,
  "sharedFrames": 324,
  "frames": [
    { "seq": 1, "file": "000001.jpg", "atMs": 0, "w": 1280, "h": 720,
      "targetId": "A1", "score": 0.0027 },
    { "seq": 2, "file": "000001.jpg", "atMs": 100, "w": 1280, "h": 720,
      "targetId": "A1" }
  ],
  "events": [
    { "atMs": 12480, "t": "tab",   "targetId": "B2", "url": "https://example.com/pay" },
    { "atMs": 14100, "t": "click", "targetId": "B2", "reason": "left" },
    { "atMs": 15600, "t": "type",  "targetId": "B2" },
    { "atMs": 18200, "t": "key",   "targetId": "B2", "reason": "Enter" },
    { "atMs": 19050, "t": "nav",   "targetId": "B2", "url": "https://example.com/paid" },
    { "atMs": 31000, "t": "stall", "reason": "another tab took the foreground" },
    { "atMs": 31200, "t": "resume" }
  ]
}
```

`atMs` is milliseconds from `startedAt`. The manifest is the only timing source.

### The event kinds

| `t`      | What it means                          | `reason` holds        |
| -------- | -------------------------------------- | --------------------- |
| `tab`    | the recording moved to another tab     | —                     |
| `nav`    | the tab it is on went somewhere else   | —                     |
| `click`  | a viewer pressed a mouse button        | the button name       |
| `type`   | a viewer typed                         | nothing, ever         |
| `key`    | a viewer pressed a named key           | `Enter`, `Tab`, `Escape` |
| `stall`  | the page stopped producing frames      | why                   |
| `resume` | it started again                       | —                     |
| `note`   | anything a caller adds by hand         | the note              |

**A `type` event records that somebody typed, never what they typed.** A password is typed
the same way as a search term, and a recording that keeps one keeps the other. One event
covers a whole burst of keys: a mark for each letter would be a wall of marks that says
nothing, and it would also spell the word out on the timeline.

`nav` and `tab` ride on the tab list the streamer polls once a second, so they work for a
browser that nobody is driving through ATR, and they catch a single page application that
only rewrites its URL. `click`, `type` and `key` come from the live view's own input path,
so a session that nobody watched has none of them.

`file` is not unique. Frame 2 above shows the same picture as frame 1, so it points at the
same JPEG. `score` is how much the frame differs from the frame one reference lag earlier,
and it is left out when it is zero. Version 1 has neither; see §7a.

The recorder appends one JSON line for each frame to `frames.jsonl` as it writes. It
assembles `manifest.json` on stop. A killed process therefore leaves a readable record, and
`atr record repair <id>` rebuilds the manifest from the sidecar.

### Knowing that a recording is running

A directory with no manifest looks the same whether a recorder is filling it or a recorder
died in it. The two need different answers — one needs the library to wait, the other needs
a repair — so a running recorder writes `recording.live.json` and refreshes it every two
seconds:

```json
{ "id": "20260831-142530", "title": "Login flow", "pid": 4113,
  "source": "cli", "startedAt": "…", "seenAt": "…" }
```

It is removed on a clean stop. A marker whose `seenAt` is more than 15 seconds old belongs
to a process that is gone, so the directory reads as interrupted again. Nothing coordinates
this: `atr record` and `atr remote` are separate processes that share only the recordings
root, and a file with a refreshed timestamp tells one about the other with a single `stat`.

`source` is what started it: `cli` for `atr record`, `live-view` for the button in the page.

This is why the library can say **● recording** on a row, why the live view can say that
somebody else is recording the browser you are watching, and why `DELETE` on a live
recording is refused rather than pulling the directory out from under a running recorder.

## 6. The record button

`atr remote` gains a recorder that is **not started**. A button in the toolbar starts it.

```
┌──────────────────────────────────────────────────────────────────────┐
│ [Login] [Checkout]                                    Live │ Recordings │
├──────────────────────────────────────────────────────────────────────┤
│ https://example.com/login          [Go] [Following the agent] [● Record] │
└──────────────────────────────────────────────────────────────────────┘

while recording:                                        [■ Stop 01:32 · 418f]
```

The dot is red while recording. Every viewer sees the same state, because the server
broadcasts it. Two people watching the same session cannot disagree about whether a
recording runs.

Rules:

- Off by default, always. The server holds no recorder until a start arrives.
- The button is the only way to start a recording from the page. `atr remote` gains no
  recording flag. A browser session is long, and you can press the button at any point in
  it. `atr record` covers the case with no page.
- `--view-only` blocks the start. A view-only viewer must not write to the operator's disk.
- One recording at a time for each server. A second start returns 409.
- Stopping the server stops the recording and writes the manifest.
- **The button never disappears.** A server with no recordings directory, and a view-only
  link, both show it disabled with the reason on it. A control that vanishes reads as a
  missing feature; a disabled one that says why reads as an answer.
- When another process is recording this browser, the bar says so beside the button:
  `● atr record is recording · 0:45`, and the status bar shows `● recording elsewhere`.
  This server did not start that recording and cannot stop it, so it offers no Stop for
  it — but somebody watching the screen has to know that what they are doing is being kept.

### Server API

```
POST   /api/record/start     {"title":"Login flow"}  →  {"id":"20260831-142530"}
POST   /api/record/stop                              →  {"id":…,"frames":418,"durationMs":…}
GET    /api/record/status                            →  {"recording":true,"id":…,…}
```

### WebSocket

The server pushes the state once a second while recording, and once on every change:

```json
{"t":"record","recording":true,"id":"20260831-142530",
 "elapsedMs":92000,"frames":418,"bytes":4300000,"dropped":0,
 "elsewhere":[{"id":"20260831-142012","title":"Login flow",
               "source":"cli","elapsedMs":45000}]}
```

`elsewhere` is every live marker under the recordings root that this server did not write.
It is on every `record` message, including the one sent when a viewer connects, so a viewer
who joins in the middle sees the same thing as one who was there at the start.

`ServerMsg` in `web/src/protocol.ts` gains `RecordMsg`.

## 7. Browsing recordings

A second view in the same page lists what is on disk and plays it.

```
┌──────────────────────────────────────────────────────────────────────┐
│                                                  Live │ Recordings ◀ │
├──────────────────────────────────────────────────────────────────────┤
│  ┌────────┐  20260831-142530          1:32   418 frames   4.1 MB     │
│  │ thumb  │  Login flow                                              │
│  │        │  example.com/login → example.com/pay      [Play] [⋯]     │
│  └────────┘                                                          │
│  ┌────────┐  20260831-140218          0:14    61 frames   0.6 MB     │
│  │ thumb  │  example.com                              [Play] [⋯]     │
│  └────────┘                                                          │
└──────────────────────────────────────────────────────────────────────┘
```

The `⋯` menu holds Rename, Export MP4, Download, and Delete.

A row whose directory has a fresh `recording.live.json` is drawn differently, and it is
inert:

```
│  ┌────────┐  Login flow  ● recording                                 │
│  │ thumb  │  running · 418 frames · 4.1 MB                           │
│  │        │  atr record is writing this now. It can be played        │
│  └────────┘  once it stops.                                          │
```

It has no actions and it does not open. Rename and Export need the manifest that the stop
will write, playing it would run off the end of a file list that is still growing, and
Delete would pull the directory out from under a running recorder. Saying so on the row is
better than offering four buttons that all fail.

The player:

```
┌──────────────────────────────────────────────────────────────────────┐
│  ‹ Recordings      20260831-142530 · Login flow                      │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│                        the canvas                                    │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│ ▌· ▬  ▌      ·· ▌         ▬ ·         ▌                    the marks │
│ ▁▃█▅▂▁ ░░0:14░░ ▂█▇█▅▃▁▂ ░░0:12░░ ▁▄█▆▂ ░░0:11░░                      │
│                                 │playhead                            │
│ [◀|] [▶] [|▶] 1× ▾  ☑ Skip inactivity (−0:36)   0:14 / 0:27 (0:49)   │
└──────────────────────────────────────────────────────────────────────┘
```

The bar is the activity timeline. A plain progress bar says nothing about a session
recording, because most of a session is a still page and the interesting seconds look
exactly like the boring ones.

- The height of each column is the change score for that moment, on a log scale. The
  scores span three decades — a blinking caret is 0.0002 and a page navigation is near 1 —
  so a linear bar would be a flat line with a few spikes.
- A hatched block is a stretch where nothing changed. It is labelled with how long it
  really lasted. **Click it to play it in full**; nothing was thrown away.
- **Skip inactivity** squeezes every quiet stretch to 0.5 s. The label says how much real
  time that removes, and the clock shows the played length with the real length after it.
  This is the feature a video cannot give you.
- **A row of marks sits along the top of the bar**, one for each manifest event. The bars
  and the marks answer different questions: the bars find the busy part of a session, the
  marks find the click that started it.
- A mark takes a shape from its kind, so the row reads without hovering every mark: a tall
  teal bar for `nav` and `tab`, a black dot for `click`, a black dash for `type`, an amber
  dot for `key` and `stall`, a green dot for `resume`.
- Marks closer than 1/140 of the bar are drawn as one. A cluster takes the shape of its
  most notable member, in the order nav, tab, key, click, type, stall, resume, note,
  because a navigation next to a click is a navigation to somebody looking for the moment
  the page changed.
- The tooltip says the time, what happened, and the URL or the reason:
  `0:14 · went to https://example.com/pay`. A cluster adds `(+3 more)`.
- Click a mark to seek to it. **`[◀|]` and `[|▶]` jump to the previous and the next mark**,
  and the up and down arrow keys do the same from the keyboard.
- Marks are placed on the playback clock, not on the recording clock. Skipping inactivity
  moves everything after the first cut, so a mark travels with the frame it belongs to.
- The URL under the playhead comes from the last `tab` event at or before it.
- Speed is 0.5×, 1×, 2×, or 4×.
- At the end the Play button becomes **Replay** and starts again from the top. A Play
  button that is already at the end has nothing to play, and pressing it and getting
  nothing reads as a broken player. The space bar follows the same rule.

A version 1 recording has no scores. The player falls back to the old rule there: a pause
longer than 2 s between frames is the only evidence of inactivity such a manifest carries.

### Recordings API

```
GET    /api/recordings                       the list, newest first
GET    /api/recordings/{id}/manifest.json
GET    /api/recordings/{id}/frames/{file}    one JPEG
GET    /api/recordings/{id}/recording.mp4    when it exists
POST   /api/recordings/{id}/encode           run ffmpeg, return the path
PATCH  /api/recordings/{id}                  {"title":"…"}
DELETE /api/recordings/{id}
```

The list carries enough for a card without reading every manifest:

```json
{ "recordings": [
  { "id": "20260831-142530", "title": "Login flow",
    "startedAt": "2026-08-31T14:25:30.112Z", "durationMs": 92768,
    "frames": 418, "bytes": 4300000, "mp4": false,
    "thumb": "/api/recordings/20260831-142530/frames/000001.jpg",
    "urls": ["https://example.com/login", "https://example.com/pay"],
    "live": false, "source": "" }
]}
```

`live` is true while a fresh marker sits in the directory, and `source` says which process
put it there. A live recording has no manifest yet, so its frame count and its length come
from the frames journal, and its title and its start time come from the marker.

**Path traversal is the main risk here**, because two path segments come from the client.

- `id` must match `^\d{8}-\d{6}(-[a-z0-9-]{1,40})?$`.
- The frame name must match `^\d{6}\.jpg$`.
- Serve through `http.ServeFileFS` over an `os.DirFS(recordingsDir)`, never through a
  `filepath.Join` on the raw input.
- The existing token check covers every one of these routes.
- `DELETE` removes one directory under the recordings root, and it refuses a symlink. It
  also refuses a directory with a fresh live marker, because the recorder writing it would
  keep writing frames into a path that no longer exists.

`--recordings-dir` sets the root. The default is `~/.atr/recordings`, mode 0700.

## 8. Player mechanics

The draw loop is the one `Viewport.tsx` already runs. The difference is where frames come
from and when they are shown.

- Binary search the manifest for the last frame whose `atMs` is at or before the playhead.
- Prefetch a window ahead of the playhead. Keep about 60 decoded `ImageBitmap` objects.
- **Close every bitmap that leaves the cache.** An `ImageBitmap` holds memory that the
  garbage collector does not reclaim on its own. `Viewport.tsx:31` already closes each
  frame after it draws. The player holds many, so the rule matters more.
- Seeking clears the cache and refetches from the new position.
- Skip inactivity builds a second timeline that maps real time to played time. The scrub
  bar uses the played timeline; the manifest keeps the real one. Opening one quiet stretch
  rebuilds it, so the playhead is moved to the start of that stretch rather than left
  where it was, which would land on an unrelated moment.

### Finding the quiet stretches

`web/src/activity.ts` turns the per frame scores into spans.

- A frame counts as movement when its score is at or above the threshold. The recorder
  writes the threshold it used into the manifest, but the player is free to use another
  one: the score carries no threshold, so a finished recording can be re-judged.
- Movement is widened by 1.2 s after and 0.5 s before. Cutting on the exact frame reads as
  a stutter. The eye needs a moment of stillness to take a change in, and a moment before
  the next one to find the place.
- A quiet run shorter than 1.5 s is not cut. It costs more attention to notice the cut
  than to watch the second.

## 7a. Telling movement from a still page

The recorder scores every frame as it captures it, and writes the score into the manifest.
Doing it at record time costs 13 ms per frame on a goroutine that already sits behind a
queue, and it saves the player from decoding a thousand JPEGs before it can draw a bar.

**A signature is a 32×20 greyscale thumbnail.** Comparing whole JPEGs is useless: the
encoder makes every frame differ, so byte equality found exactly one static pair in a 1421
frame recording. Downsampling throws that noise away and keeps the layout, which is what
actually changes when something happens.

**The difference is the worst tile of a 4×4 grid, not the frame mean.** One changed word in
a large page moves the frame mean by almost nothing, so a frame mean calls typing idle.

**The reference frame sits 1 s back, not one frame back.** This is the part that makes it
work. A caret blinks: it differs from the frame before it and matches the frame a second
earlier. Measuring against a lagged reference cancels anything that reverts and keeps
anything cumulative, such as typing. Measured against the previous frame, typing is only
3–5× louder than a caret blink; measured against a 1 s reference, the two separate
cleanly.

Verified against a recording with an exact typing schedule: every typing second scored
0.0021 to 0.0030 and every idle second scored 0.0000 to 0.0004. At the 0.002 threshold
that is 13 of 13 hits and no false positive.

| Setting | Default | Why |
| --- | --- | --- |
| Reference lag | 1000 ms | Cancels the caret blink, which reverts. |
| Activity threshold | 0.002 | Sits in the empty band between typing and a caret. |
| Dedupe epsilon | 0.0005 | One typed character always writes a file; a blink shares one. |
| Keep every | 2000 ms | Bounds a drift that stays under the epsilon. |

### Nothing is dropped; a file is shared

A frame whose picture matches the last one written keeps its own record, with its own
`atMs` and its own score, and points `file` at the JPEG that is already on disk. The
timeline stays complete and the disk holds one copy.

- `Manifest.sharedFrames` counts the frames that point at a file another frame owns. It is
  how much the sharing saved, not how much was lost.
- The ring buffer counts references per file name. A file is deleted only when the last
  frame pointing at it is cut, or a still page would lose its only picture on the first
  trim.
- The MP4 export needs no change. The concat demuxer already emits one entry per frame
  with an explicit duration, so a repeated name simply holds for as long as the run did.

Measured on a 49 s session: 369 frame entries, 45 files, 3.5 MB. All 369 moments are on
the timeline.

`--keep-all` turns the sharing off and writes every frame to its own file.

Refactor: pull the canvas and the draw loop out of `Viewport.tsx` into `FrameCanvas.tsx`.
`Viewport` becomes `FrameCanvas` plus the input handlers. `Player` becomes `FrameCanvas`
plus the transport. About 40 lines move.

Routing: the app uses `location.hash`, with `#/` for live and `#/recordings/:id` for a
recording. No router library. The page is too small to earn one.

## 9. The CLI

```
atr record start [flags]        start recording and return; prints the id
atr record start --foreground   record until Ctrl+C instead
atr record stop [id]            stop, and wait until the manifest is on disk
atr record status               what is recording now; exits 1 when nothing is
atr record list                 the same list the API returns
atr record encode <id>          write recording.mp4
atr record repair <id>          rebuild manifest.json from frames.jsonl
atr record rm <id>
```

`atr record` on its own records nothing. It is the parent, and it refuses an
argument it does not know, so a mistyped subcommand cannot start a recording by
accident.

`start` re-runs this binary detached and waits for the live marker before it
prints the id, so a caller never has to write `nohup ... &` or guess a PID.
`stop` reads the pid out of `recording.live.json`, sends `SIGINT`, and waits for
`manifest.json`. It interrupts and never kills: a killed recorder leaves frames
with no manifest, which is what `repair` exists to clean up.

| Flag | Default | Purpose |
|---|---|---|
| `--output`, `-o` | `~/.atr/recordings/<id>/` | The output directory. |
| `--title` | **required** | What the recording is for. Refused when blank: an untitled recording is a bare timestamp in the library. |
| `--attach` | discovered | A CDP endpoint. The same order as `atr remote`. |
| `--quality` | `60` | JPEG quality, 1 to 100. |
| `--max-width` | `1600` | The largest frame width. |
| `--fps` | `10` | The target frame rate. It sets `everyNthFrame`. |
| `--max-duration` | `30m` | Stop after this time. |
| `--max-size` | `1GB` | Stop when the frames reach this size. |
| `--keep-last` | none | Keep only the last window, for example `60s`. |
| `--heartbeat` | `5s` | Force one frame after this much silence. `0` turns it off. |
| `--policy` | `follow` | `follow` the front tab, `pin` one tab, or `hold` it in front. |
| `--encode` | `none` | `none`, `mp4`, or `webm`. Encode when the recording stops. |
| `--change-threshold` | `0.002` | The activity score the player treats as movement. |
| `--keep-every` | `2s` | Write a frame of its own at least this often. |
| `--keep-all` | off | Write every frame to its own file. See §7a. |

`atr remote` takes the same three activity flags, because it records too.

`--encode` defaults to `none`, because the web page plays frames and most recordings never
need a file.

### Recording a behavior test

```
atr run --behavior tests/login.test.txt --record
```

The flag is off by default. When it is set, `atr run` starts a recorder before the first
step and stops it after the last one. `--record-title` defaults to the test file name, so
the list reads `login.test.txt` instead of a timestamp.

The recording id reaches the result, and `FailureContext` carries the path
(`internal/capture/types.go`). A failed test then points at the recording that shows it
fail.

The size limits apply here too. In CI, `--record --keep-last 60s` keeps only the seconds
before the failure.

## 10. Dependencies and errors

A recording needs things that may not be there. Every one of them is checked **before the
first frame**, and every failure names the dependency, says why it is needed, and gives the
command that fixes it.

### The rule

**Preflight runs before capture, never after.** A missing `ffmpeg` must not surface thirty
minutes later, when the recording stops. If `--encode=mp4` is set and `ffmpeg` is absent,
`atr record` refuses to start.

The one exception runs the other way. If a dependency disappears **during** a recording,
the stop still writes the frames and the manifest, and it reports the encode failure
separately. A dependency problem never destroys an artefact that is already on disk.

### What is checked

| Dependency | When | On failure |
|---|---|---|
| A reachable CDP endpoint | always | Start a browser, or pass `--attach` |
| At least one open page | always | Open one with `atr browser navigate <url>` |
| The recordings directory is writable | always | The `mkdir` or `chmod` to run |
| Free disk over `--max-size` | always | Warn. Fail only with `--strict` |
| `ffmpeg` on `PATH` | only with `--encode` | The install command for this OS |
| The chosen encoder inside `ffmpeg` | only with `--encode` | Use `--encode=webm`, or install a full build |

`ffmpeg` is checked only when the run will actually use it. A plain `atr record` never
mentions `ffmpeg`, because it never needs it.

### The error

```
Error: atr record needs ffmpeg, and it is not on PATH.

  Why   --encode=mp4 is set, so the recording is encoded when it stops.
  Fix   sudo apt install ffmpeg            (Debian, Ubuntu)
        brew install ffmpeg                (macOS)
        winget install Gyan.FFmpeg         (Windows)
  Or    drop --encode. The recording still plays in "atr remote", which
        needs no ffmpeg. Encode it later with "atr record encode <id>".
  Docs  docs/session-recording.md#11-mp4-export
```

The `Or` line is not decoration. Frames are the format, so for `ffmpeg` there is a real
alternative, and the error should say so rather than send somebody to install a package
they do not need.

The command for the host operating system prints first. The others follow, because a person
reading a CI log is often not on the machine that failed.

### The type

```go
// MissingError reports an absent dependency and how to supply it. Every
// surface renders the same struct: the CLI as the block above, the REST API
// as JSON, and the web page as a panel with a copy button.
type MissingError struct {
	Dependency  string   // "ffmpeg"
	Why         string   // why this run needs it
	Fix         []Fix    // the host's own platform first
	Alternative string   // empty when there is no way around it
	Docs        string
}

type Fix struct {
	Platform string // "debian", "macos", "windows"
	Command  string
}

func (e *MissingError) Error() string
```

REST returns the same fields, so the web page can render the fix instead of a bare
"failed":

```json
{ "success": false,
  "error": "atr record needs ffmpeg, and it is not on PATH",
  "dependency": "ffmpeg",
  "why": "--encode=mp4 is set, so the recording is encoded when it stops",
  "fix": [ { "platform": "debian", "command": "sudo apt install ffmpeg" } ],
  "alternative": "Play the recording in atr remote. It needs no ffmpeg.",
  "docs": "docs/session-recording.md#11-mp4-export" }
```

The Export item in the `⋯` menu shows that panel. It does not show a toast that says the
export failed.

### `atr record doctor`

```
$ atr record doctor

  ok       browser          Chrome/151.0.7922.71 at ws://127.0.0.1:46839
  ok       pages            2 open
  ok       recordings dir   ~/.atr/recordings  (0700, 41 GB free)
  ok       ffmpeg           8.0.1
  ok       encoder libx264  present
  warn     encoder libvpx   absent — "--encode=webm" will not work
                            fix: sudo apt install ffmpeg   (Debian, Ubuntu)
```

It exits 0 when every required check passes, and 1 when one fails. A warning alone does not
fail it. Put it in CI before a recorded test run, and a missing package shows up as one
clear line instead of a broken artefact.

## 11. MP4 export

Chrome emits a frame only when the page changes, so the frame rate is variable. The encoder
uses the `concat` demuxer with a duration for each frame:

```
file 'frames/000001.jpg'
duration 0.350
file 'frames/000002.jpg'
duration 12.480
file 'frames/000003.jpg'
duration 0.100
file 'frames/000003.jpg'
```

The last entry repeats the final file with no duration. `ffmpeg` ignores the duration of
the last entry, and without the repeat the final frame never appears.

```
ffmpeg -hide_banner -loglevel error \
  -f concat -safe 0 -i concat.txt \
  -vf "scale=W:H:force_original_aspect_ratio=decrease,\
pad=W:H:(ow-iw)/2:(oh-ih)/2,format=yuv420p" \
  -fps_mode vfr -c:v libx264 -preset veryfast -crf 28 \
  recording.mp4
```

`W` and `H` come from the largest frame in the manifest, rounded up to an even number. A
tab switch changes the frame size in the middle of a stream, and `libx264` needs one size
for the whole video. The filters letterbox the smaller frames instead of stretching them.

A missing `ffmpeg` never fails a recording. The encode endpoint returns 503 with a clear
message, and the UI hides the Export item.

## 12. Frame handling and backpressure

The screencast acknowledgement must never wait for the disk. Chrome stops the stream when
an acknowledgement is late.

- The `Streamer` acknowledges the frame first. This code exists
  (`internal/remote/screencast.go:194`).
- The recorder's sink pushes the frame into a buffered channel of 512 entries and returns.
- One writer goroutine drains the channel and writes the bytes.

The `Hub` drops a stale frame, because a live viewer only wants the newest image. A
recorder must not use that rule, because a dropped frame is lost history. The recorder uses
a deep buffer and drops only when the buffer is full. Every drop increments a counter that
reaches the manifest, the status line, and the UI.

512 frames at 10 kB is about 5 MB of memory, and 51 seconds of slack at 10 fps.

## 13. Silence, tabs, and the foreground rule

A background tab emits no frames, and no error. Section 16 measured it again in headless
mode: one frame when the screencast starts, then nothing. The recorder inherits the
problem. It does not inherit the old answer.

The old answer was to fight the agent for the foreground. The spike found a better one.
`Page.captureScreenshot` works on a tab that is **not** in front, it returns in 65 ms, and
it returns a current picture, not a stale surface. The recorder can therefore keep
recording one tab while the agent works in another.

Three policies:

| `--policy` | Behaviour | Cost |
|---|---|---|
| `follow` | Record whichever tab is in front. Full frame rate. The default. | The recording jumps between tabs. |
| `pin` | Record one tab only, whatever the agent does. The heartbeat supplies the frames while the tab is hidden. | 0.2 fps while hidden. It never interferes. |
| `hold` | Bring the recorded tab to the front and keep it there. | It fights the agent. Rarely needed now. |

- Each tab switch writes a `tab` event. The player draws it as a tick, so a reviewer sees
  why the picture changed.
- `--heartbeat 5s` forces one capture after 5 s of silence, whether the tab is in front or
  not. At 12 kB a frame that is about 150 kB a minute.
- `Streamer.Watch` already restreams the front tab, which is `follow`
  (`internal/remote/screencast.go:315`). `Streamer.Snapshot` already does the capture
  (`internal/remote/screencast.go:400`). `pin` is the new part: it skips the restream and
  lets the heartbeat carry the recording.
- Starting a screencast always yields one frame, even on a hidden tab. The recorder
  therefore has a first image immediately, with no special case.

## 14. Size control

Section 16 measured the two ends. At fps 10, a page that never stops moving costs 6.8 MB a
minute. A page that waits for a person costs 150 kB a minute, which is the heartbeat alone.
A real test sits between the two, and much nearer the bottom.

The worst case is therefore about 200 MB for the default 30 minute limit.

- `--max-duration` and `--max-size` stop the recording.
- `--keep-last 60s` runs a ring: the writer deletes frames older than the window and
  rewrites the head of `frames.jsonl`. Use it in CI, where only the seconds before a
  failure matter.
- `--keep-last` and `--max-size` are exclusive. The ring already bounds the size.

The recordings list shows the total on disk, and the UI warns above 5 GB.

## 15. Package layout

```
internal/remote/sink.go        the Sink interface; Hub implements it
internal/remote/screencast.go  Streamer holds []Sink instead of one *Hub    (edit)
internal/remote/server.go      the record and recordings routes             (edit)
internal/record/recorder.go    the Sink implementation and the lifecycle
internal/record/writer.go      frames, frames.jsonl, manifest.json
internal/record/store.go       list, read, rename, delete, and the id rules
internal/record/ring.go        the --keep-last window
internal/record/encode.go      concat.txt and the ffmpeg call
internal/cli/record.go         the cobra command and its subcommands
web/src/FrameCanvas.tsx        the canvas and the draw loop                 (extract)
web/src/Viewport.tsx           FrameCanvas plus the input handlers          (edit)
web/src/RecordButton.tsx       the toolbar control
web/src/Library.tsx            the list of recordings
web/src/Player.tsx             the playback view
web/src/useRecordings.ts       the HTTP client
web/src/protocol.ts            RecordMsg and the manifest types             (edit)
```

The `Sink` interface:

```go
// Sink receives frames from a Streamer. A Sink must not block; the screencast
// acknowledgement runs on the same goroutine.
type Sink interface {
	Frame(*Frame)
	Text([]byte)
}
```

`Hub.Broadcast` and `Hub.BroadcastText` become `Hub.Frame` and `Hub.Text`. `NewStreamer`
drops its `*Hub` argument and gains `AddSink(Sink)`. That is the whole change to existing
Go behaviour.

Nothing changes in `internal/browser`, `internal/mcp`, `internal/agent`, or `internal/api`.

## 16. Spike results

A spike ran against Chrome 151.0.7922.71, headless, on Linux. The code is in
`cmd/spike-record`. Repeat it with `go run ./cmd/spike-record`, and add `-headful` on a
machine that has a display.

| Check | Result |
|---|---|
| Two CDP sessions each stream the same page | **Yes.** Both received 179 frames in 3 s, byte for byte the same. |
| A background tab keeps streaming | **No.** One frame at the start, then nothing for 3 s. |
| `Page.captureScreenshot` on a background tab | **Works.** 65 ms, with no `bringToFront`. |
| That background capture is current, not stale | **Fresh.** A DOM change made while the tab was hidden appeared in the next capture. |
| A still foreground page, 5 s at fps 10 | **0 frames.** |
| An animated page, 5 s at fps 10 | 49 frames, 12.2 kB each, 6.8 MB a minute. |
| `ffmpeg` concat with two frame sizes | **Works.** 800×600 frames padded into a 1280×720 h264 file, no error. |
| The repeated last entry in `concat.txt` | **Required.** Without it: 5 frames and 14.96 s. With it: 6 frames and 15.96 s. |

Five conclusions:

**`atr record` and `atr remote` can run at the same time.** This was the one design-level
risk. It is gone, and section 4 stands as drawn.

**The heartbeat is not a safety net. It is the frame source for a quiet page.** A still page
produced zero frames in five seconds. Without a heartbeat, a recording of a login form that
waits for a person would contain nothing at all. `--heartbeat 5s` stays on by default, and
section 13 now leads with it.

**The heartbeat works on a background tab, and it tells the truth.** This is the surprise,
and it changed the design. A recording no longer goes dark when the agent switches tabs.
The recorder keeps producing frames of the tab it was told to record, without ever fighting
for the foreground. That is the new `--policy=pin`.

**The size budget holds.** 6.8 MB a minute is the worst case, and a real test spends most of
its time far below it.

**The export chain is verified, including the trap.** `ffmpeg` 8.0.1 encoded a five frame
sequence whose size changed halfway through, and the output was one 1280×720 h264 file of
15.96 s against 15.93 s of requested duration. Dropping the repeated last entry lost the
final frame, exactly as section 11 warns. That rule now has a measurement behind it, not
just a claim.

One caveat: every measurement is headless, because this machine has no display. Background
tab compositing is exactly the behaviour that differs between the two modes. The result
agrees with the headful `atr remote` spike, so the risk is small, but `-headful` should be
run once on a desktop before phase 6 tunes the heartbeat.

## 17. Test plan

Go:

- Unit: frame timestamps convert to `atMs` correctly.
- Unit: `concat.txt` repeats the last file and omits its duration.
- Unit: `W` and `H` round up to an even number.
- Unit: the ring deletes by age and rewrites the head of `frames.jsonl`.
- Unit: the sink returns immediately when the channel is full, and counts the drop.
- Unit: the id and frame-name patterns reject `..`, an absolute path, and a symlink.
- Unit: `repair` rebuilds a manifest from a truncated `frames.jsonl`.
- Unit: preflight with an empty `PATH` returns a `MissingError` that names `ffmpeg`, and
  carries the fix for the host platform first.
- Unit: preflight says nothing about `ffmpeg` when `--encode=none`.
- Unit: a stub `ffmpeg` that lists no `libx264` produces the "use `--encode=webm`" advice.
- Integration: `--encode=mp4` with no `ffmpeg` refuses to start, and writes no frames.
- Integration: `ffmpeg` removed mid-recording — the stop still writes the frames and the
  manifest, and it reports the encode failure separately.
- Integration: record an animated page for 3 s, assert 10 or more frames and a monotonic
  sequence.
- Integration: `GET /api/recordings` lists a recording that was just written.
- Integration: a second `POST /api/record/start` returns 409.
- Integration: `--view-only` makes a start return 403.
- Integration: `atr record` and `atr remote` run together, and both receive frames.

Web:

- Unit: the binary search finds the frame for a playhead time, including both edges.
- Unit: the skip-inactivity timeline maps real time to played time and back, and stays
  monotonic when one quiet stretch is opened.
- Unit: a version 1 manifest still plays, on the pause rule.
- Unit: the bitmap cache closes every bitmap it evicts.
- Manual: record a login, then play it, and confirm the timing looks right.

Activity detection:

- Unit: the difference of a signature with itself is zero, and a change in one tile of
  sixteen is above the threshold.
- Unit: a signal that reverts within the reference lag, such as a caret, scores below the
  threshold.
- Unit: a run of identical frames writes one file and keeps every frame entry.
- Unit: the ring trim keeps a file while any frame still points at it.
- Unit: `ConcatList` gives a repeated file its real time.
- Golden, opt in through `ATR_GOLDEN_RECORDING`: replay a real recording and assert the
  active fraction is that of a session with a person in it.

## 18. Phases

| Phase | Content | Result |
|---|---|---|
| 0 | The spike checks — **done**, section 16 | The design is confirmed. |
| 1 | `Sink`, recorder, writer, store, heartbeat, preflight, `atr record` | Frames and a manifest from the CLI. |
| 2 | `/api/record/*`, the `record` WebSocket message, `RecordButton` | You can record from the page. |
| 3 | `/api/recordings/*`, `FrameCanvas` extract, `Library` | You can see what you recorded. |
| 4 | `Player`, the timeline, event ticks, skip gaps | You can watch what you recorded. |
| 4a | Manifest version 2: change scores, shared files, `Scrubber` | You can see where the session had a person in it, and skip the rest. |
| 5 | `concat.txt`, ffmpeg, Export, `atr record encode`, `atr record doctor` | You can send someone an MP4. |
| 6 | `--keep-last`, `--max-size`, `--heartbeat`, `repair` | Safe to leave running. |
| 7 | `atr run --behavior --record`, the path in `FailureContext` | A failed test carries its recording. |

Phases 1 to 4 are the ask: about 900 lines of Go and 700 lines of React.

## 19. Confidence

| Part | Score | Note |
|---|---|---|
| Two screencasts on one page | 98 | Measured, section 16 |
| The heartbeat on a background tab | 95 | Measured, section 16. Headless only |
| The sink refactor in `internal/remote` | 96 | A small, mechanical change |
| The store, the id rules, and path safety | 92 | Well understood, and directly testable |
| Writing frames without stalling the ack | 90 | 12.2 kB a frame is known; the buffer depth is still a guess |
| Playing frames on a canvas from a manifest | 90 | The draw path already works in `Viewport` |
| The bitmap cache and the prefetch window | 75 | Memory behaviour needs a real recording to tune |
| Skip gaps and the two timelines | 70 | The mapping is fiddly in both directions |
| The ffmpeg export | 92 | The exact command ran here, mixed sizes and all |
| Preflight and the error messages | 94 | Plain Go, and every check is testable with a fake PATH |

**Overall for phases 1 to 4: 88 out of 100.** The spike removed the design risk. What
remains is ordinary implementation risk, and it sits almost entirely in the player: the
bitmap cache and the skip-gaps timeline.

**Phase 5, the export: 92.** It was 70 before the spike ran the real command.

## 20. Open questions

Settled: recording is never on by default; `atr run --behavior` opts in with `--record`;
`atr remote` gains no recording flag and no recordings-only mode; two screencasts on one
page work, so `atr record` and `atr remote` may run together.

Still open:

1. Should the player show the interaction events from `internal/browser/recorder.go` on the
   same timeline when both recorders ran? The two artefacts would then line up.
2. Does a recording need redaction? It captures a password typed into a field whose type is
   text. The 0700 directory is a warning, not a control.
3. Do cloud sessions push recordings to object storage, or is the local artefact plus an
   existing upload path enough?
4. Should `atr record doctor` become `atr doctor`, and cover the browser and computer
   surfaces as well? The pattern is worth more than one command.
