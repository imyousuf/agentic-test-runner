---
name: atr-record
description: Record a browser session to disk and read back what went wrong — console errors, failed requests, and the frames that were on screen when they happened. Use when a browser run failed and the transcript is not enough, when you need evidence of a flaky reproduction, or when somebody wants to watch what an agent did. Also covers `atr remote`, the live view that streams the running browser into a web page. Pairs with atr-browser, which drives the browser this records.
allowed-tools: Bash(atr record:*), Bash(atr remote:*)
---

# ATR Session Recording

`atr record` captures what the browser shows and writes it to disk. `atr remote`
serves a live view of the same browser in a web page.

Both attach to the browser `atr browser start` is already running, as a second
DevTools session. You can drive, watch and record at the same time.

## The point, for an agent

Not the video. **The manifest and the journal.** A recording lets you ask what
broke without watching anything:

```bash
# how many failures the page reported
jq '.devtools.errors' ~/.atr/recordings/<id>/manifest.json

# each one, with its time on the recording clock
jq '.events[] | select(.t=="error" or .t=="netfail")' ~/.atr/recordings/<id>/manifest.json

# the full journal is NDJSON, so grep works
grep '"t":"netfail"' ~/.atr/recordings/<id>/devtools.jsonl
```

Run a flow, then ask the recording what went wrong.

## The loop

```bash
# 1. Check it will work here. Exits 0 or 1.
atr record doctor || exit 1

# 2. Start. It returns immediately and prints the id.
ID=$(atr record start --quiet --title "checkout repro")

# 3. Drive the browser as usual: atr browser navigate/click/fill ...

# 4. Stop. Returns once the recording is playable.
atr record stop

# 5. Read it.
jq '.devtools.errors' ~/.atr/recordings/$ID/manifest.json
```

`atr record start` runs the recording in the background, so **never wrap it in
`nohup ... &`** and never hunt for its process id. `atr record stop` finds it,
interrupts it, and waits for `manifest.json` — so a script can stop and read
immediately, with no sleep.

Use `--quiet` when capturing the id. The friendly output contains a `PID:` line,
and `PID:` contains `ID:`, so a naive grep picks up both.

## Commands

| Command | Does |
|---|---|
| `atr record start [flags]` | Start in the background; prints the id |
| `atr record stop [id]` | Stop and wait until playable; no id means the newest |
| `atr record status` | What is running. Exits 1 when nothing is |
| `atr record list` | Every recording |
| `atr record encode <id>` | Export an MP4 (the only part needing ffmpeg) |
| `atr record repair <id>` | Rebuild the manifest of an interrupted recording |
| `atr record rm <id>...` | Delete |
| `atr record doctor` | Check dependencies. Exits 0 when recording will work |

`atr record` on its own records nothing. It is a parent command and it refuses
an argument it does not know, so a mistyped subcommand cannot start a recording
by accident.

### Flags worth knowing

| Flag | Default | Why |
|---|---|---|
| `--title` | empty | Always set one, or `atr record list` is unreadable |
| `--quiet`, `-q` | off | Print only the id, for `ID=$(...)` |
| `--foreground` | off | Hold the terminal and stop on Ctrl+C instead |
| `--max-duration` | `30m` | The safety net. A caller that forgets to stop still terminates |
| `--policy` | `follow` | `pin` records one tab and never pulls it to the front |
| `--redact-query` | off | Strip the query string from every URL in the log |
| `--encode` | `none` | `mp4` encodes when the recording stops |

## What a recording is

A directory, not a video. That *is* the format, so playback needs no codec.

```
~/.atr/recordings/20260901-064022-checkout-repro/
├── frames/000001.jpg …     the pixels
├── frames.jsonl            written as it goes, so a crash loses nothing
├── manifest.json           written only on a clean stop
└── devtools.jsonl          console, errors and network metadata
```

`manifest.json` carries `events` — what happened, on the recording clock:
`nav`, `tab`, `click`, `type`, `key`, and the two that matter most, `error` and
`netfail`. Repeats collapse into one event with a `count`.

`devtools.jsonl` is one JSON object per line: `console`, `error`, `issue`,
`req`, `res`, `netfail`, plus `tap` when the log follows a tab switch and `drop`
when a limit refused lines.

## What is never recorded

The log holds **no request body, no response body and no header**. A login POST
carries the password in one and the session in the other.

A `type` event records *that* somebody typed, never *what* — a password is typed
the same way as a search term.

URLs are stored whole, because a URL is metadata. If the session carries tokens
in query strings, pass `--redact-query`, which strips the query from the `url`
field and from URLs quoted inside a message. The manifest records whether it was
on.

## Traps

**Do not record the ATR live view.** Pointing the browser at `atr remote`'s own
page makes it stream itself, and the network log fills with the live view's
asset requests. Open the live view in a different browser from the one being
recorded.

**A recording follows the foreground tab.** Opening a new tab moves the
recording with it. Use `--policy pin` to hold one tab.

**Never `pkill -f atr`.** The pattern matches your own shell.

**Never `SIGKILL` a recorder.** A killed recorder leaves every frame on disk and
no manifest, and nothing will play it. `atr record stop` sends `SIGINT` for this
reason. If one was killed anyway, `atr record repair <id>` rebuilds the manifest
from `frames.jsonl`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Succeeded. For `status`, something is recording |
| 1 | Failed, or for `status`, nothing is recording |

`atr record doctor` exits 0 when recording will work here and 1 when it will
not, so a job can gate on it without parsing the output.

## The live view

```bash
atr remote --port 7788                  # prints a tokenised URL
atr remote --view-only                  # a link that cannot touch the browser
atr remote --redact-query               # same URL stripping as the recorder
```

The page shows the browser, a tab strip that can switch, close and open tabs, a
● Record button, and a DevTools dock with Console, Network and Issues. Past
recordings are under `#/recordings`.

The URL carries a token. Anyone with the link can drive the browser unless
`--view-only` is set.

A recording the live view started can be stopped from the page. One that
`atr record` started cannot — the page says who owns it and offers no button
that would lie.
