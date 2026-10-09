# DevTools capture — console, errors and network

Status: draft for review
Depends on: [`docs/session-recording.md`](./session-recording.md),
[`docs/web-ui-redesign.md`](./web-ui-redesign.md)

## 1. Summary

Capture what the DevTools console and the DevTools network panel would show, keep it beside
the frames, and open it in a dock next to the picture.

```
atr remote            the dock streams while you watch
atr record            every recording gets the log; there is no flag to switch it off
▸ Recordings ▸ play   the dock follows the playhead, and a click on a row seeks
```

A recording answers *what did the agent see*. It does not answer *what did the page do*. A
button that does nothing looks the same as a button that fired a request and got a 500. The
frames cannot tell those apart. The console and the network log can.

## 2. Scope

**In scope.**

| Source | CDP event | What only it gives |
|---|---|---|
| Console calls | `Runtime.consoleAPICalled` | `console.log`, `warn`, `error` and their arguments |
| Uncaught errors | `Runtime.exceptionThrown` | the message and the stack trace |
| Browser log | `Log.entryAdded` | a CORS block, a CSP violation, a 404 on a subresource, a deprecation. The console shows these, and `consoleAPICalled` does **not** carry them. |
| Network | `Network.requestWillBeSent`, `responseReceived`, `loadingFinished`, `loadingFailed` | method, URL, status, resource type, transferred size and duration |

**Out of scope for now.** Request and response bodies, request and response headers, and the
`Tracing` domain. Section 9 says why, and section 12 says when they arrive.

## 3. Four decisions

**Metadata, not payloads.** A body needs one `Network.getResponseBody` call for each
request, timed before Chrome evicts its buffer, and it stores whatever the person typed into
the form. The status, the URL and the duration already answer most questions, and they cost
about 100 kB a minute.

**The tap follows the streamed tab.** It is enabled on the page the screencast is on, and it
moves when the stream moves. It needs no target discovery and no auto-attach, because
`Streamer.stream` already knows which page it is on and already tears down cleanly. The cost
is real and it is stated in section 8: a popup that never comes to the front is never
logged.

**Always on, and part of every recording.** The domains are enabled when the streamer selects
a page, not when the panel opens and not when a recording starts. A panel that fills only
after you open it is useless, because you open it *after* something went wrong.

`atr record` therefore always writes `devtools.jsonl`. **There is no `--no-devtools`.** The
log is part of a recording in the way the frames are, and nobody asks for `--no-frames`. A
recording that sometimes has a log and sometimes does not is a recording nobody can trust to
answer a question, and the whole value here is being able to open a session from last week
and find the failure in it.

The two things a flag would have been for have better answers. Size is handled by the limits
in section 10. Privacy is handled by section 9: no bodies, no headers, and `--redact-query`
when a URL carries a token.

`atr remote` enables the tap for the same reason, and `atr run --record` inherits it.

**The wire is the format.** One event is encoded once, as one JSON object. The live view
reads it off the WebSocket, and the recorder appends it to `devtools.jsonl`. The file is
the message log. Nothing has to agree on two shapes.

## 4. Shape

```
                      ┌──────────────┐
   Chrome ──frames───▶│              │──Frame───▶ Hub ──────▶ the live view
          ──console──▶│   Streamer   │──Text────▶ RecorderSink
          ──errors───▶│  + the tap   │──Action──▶      │
          ──network──▶│              │──LogEvent▶      ▼
                      └──────────────┘         devtools.jsonl
```

`internal/remote/devtools.go` holds the tap. It turns a CDP event into a plain struct and
hands it to the sinks. `internal/record` keeps storing plain structs, and it stays free of
every CDP type, which is the rule `recording.go` already states.

The fan-out copies the pattern that `Actor` set:

```go
// Logger is a Sink that also wants what the page reported. Hub implements it,
// because a viewer cannot see a console message in a JPEG.
type Logger interface {
	Log(LogEvent)
}
```

The event is marshalled twice, once for the wire and once for the journal. That is one small
allocation for each event, and it buys a fan-out with no shared buffer and no ordering rule
between two consumers.

### The event

```go
type LogEvent struct {
	TS       int64  // unix milliseconds; the recorder converts it to atMs
	T        string // "console", "error", "issue", "req", "res", "netfail", "tap"
	Level    string // "debug", "info", "log", "warn", "error"
	Text     string
	Stack    string // only on "error"
	ReqID    string // ties a "res" or a "netfail" to its "req"
	Method   string
	URL      string
	Status   int
	Kind     string // the resource type: document, xhr, fetch, script, image
	Bytes    int64
	DurMs    int64
	TargetID string
}
```

A request writes two lines: `req` when it is sent, and `res` or `netfail` when it settles.
The join happens in the panel, keyed on `ReqID`. Writing one joined line instead would need
a pending map in Go, a flush of everything still in flight at stop, and a second shape for
the live view, which wants to show the request the moment it fires. One dumb append-only
writer is worth two lines.

A request that never settles simply has no second line. The panel draws it as pending.

**`TS` is a wall clock, not a recording clock.** A recording can start at any point in a
session, so the tap cannot stamp `atMs`. The recorder subtracts its own start time when it
writes the line, and the journal carries both.

## 5. Storage

```
~/.atr/recordings/20260831-142530/
  frames/  frames.jsonl  manifest.json
  devtools.jsonl        ← one JSON object per line, append-only
```

```json
{"atMs":12480,"ts":1756645530112,"t":"req","reqId":"1000.42",
 "method":"POST","url":"https://api.example.com/v1/chat","kind":"fetch"}
{"atMs":12960,"ts":1756645530592,"t":"res","reqId":"1000.42",
 "status":500,"bytes":840,"durMs":480}
{"atMs":12970,"ts":1756645530602,"t":"error",
 "text":"TypeError: Cannot read properties of undefined (reading 'id')",
 "stack":"at renderThread (app.js:1284)"}
```

Same shape as `frames.jsonl`, and for the same reason: an interrupted recording keeps its
log, and `atr record repair` can fold the counts into the rebuilt manifest.

The manifest gains one optional block:

```json
"devtools": { "lines": 1840, "bytes": 412000, "dropped": 0,
              "bodies": false, "headers": false }
```

**The manifest version stays at 2.** The block is additive, no existing field changes
meaning, and a reader that has never heard of it keeps working.

Every recording made after this feature lands carries the block, **including one with an
empty log**, where `lines` is 0. A quiet page and a recording from before the feature are
different answers, and only a present block with a zero in it can say the first. An absent
block means the recording is older, and the player then shows no dock rather than an empty
one.

`bodies` and `headers` are written even though both are false today. A reader has to be able
to tell "this recording kept no headers" from "this recording is from before headers
existed", and only a recorded flag can say that.

## 6. HAR is an export

`atr record har <id>` writes a HAR file. Chrome DevTools, Charles and Proxyman all import
one. Nothing at record time depends on it, exactly as nothing at record time depends on
`ffmpeg`.

A HAR built from metadata alone carries no bodies and no headers. That is a valid HAR, and
the export says so in its `comment` field rather than pretending otherwise.

## 7. The dock

One component, two views, 380 px wide by default, dragged to resize, remembered in
`localStorage`.

```
┌────────────────────────────────┬─────────────────────────┐
│                                │ Console  Network  Issues│
│                                │          ▲12      ▲3    │
│           the frame            ├─────────────────────────┤
│                                │ 0:12.4 ⬤ POST /v1/chat  │
│                                │        500 · 480 ms     │
│                                │ 0:12.9 ✕ TypeError:     │
│                                │        Cannot read pro… │
├────────────────────────────────┤ 0:13.1 ⚠ CORS blocked   │
│ ▌·  ▬   ✕        ▌             │                         │
│ ━━━━━━━━●───────────────────── │ [filter…]  [errors ▾]   │
└────────────────────────────────┴─────────────────────────┘
```

- **Console** shows `console`, `error` and `issue` rows. **Network** shows the joined
  requests. **Issues** shows only what failed: an uncaught error, a `netfail`, and any status
  of 400 or more. The tab labels carry a count, and the count turns red when it holds an
  error.
- A filter field matches the text and the URL. A level menu narrows by severity.
- Each row shows its time on the recording clock, so a row and the playhead speak the same
  language.

**In the live view** the rows arrive over the WebSocket. Clear empties the view and not the
recording, and the button says so.

**In the player** the dock reads `devtools.jsonl` once and then follows the playhead. Rows
after the playhead are hidden, so the dock always shows what was known at that moment.
A "show all" toggle reveals the rest. Clicking a row seeks the video to it. Scrubbing
scrolls the dock to the last row at or before the head.

That pair of behaviours is the whole point. The frames say when it looked wrong, and the
dock says what happened at that second.

## 8. Timeline marks

**Every error goes on the timeline.** This is the point of the whole feature: you scrub to
the red mark, and the dock beside it already says what broke.

Four things make an `error` mark:

| Source | Example |
|---|---|
| An uncaught exception | `TypeError: Cannot read properties of undefined` |
| A `console.error` call | what the application itself reported |
| A browser log entry at error level | a CORS block, a CSP violation, a failed subresource |
| A failed or refused request | a `netfail`, or any status of 400 or more |

The recorder writes each one as a manifest event, so the mark row built last week draws them
with no new drawing code.

- Two new kinds: `error` and `netfail`. Both paint in `--danger`, which is red in the light
  theme and a lighter red in the dark one.
- They rank **first** in the cluster order, ahead of `nav`. When an error lands in the same
  moment as a navigation, the cluster shows red. A person scanning the bar is looking for
  the failure, not for the page load that came with it.
- `Reason` carries the message, cut to 200 characters. The tooltip shows it, so you can read
  the error without opening the dock at all.
- Clicking the mark seeks to it **and** scrolls the dock to that row.

**Warnings are off the row by default, and one chip turns them on.** A `console.warn` and a
warning-level log entry become amber marks when the chip is on. Most applications warn
constantly, so an always-on warning would bury the errors it sits beside. The chip is in the
dock, next to the level filter, and its state is remembered.

A warning mark is **derived in the player** from `devtools.jsonl`, not written to the
manifest. A recorded event is fixed at record time, and a chip that can only change what the
next recording shows is not a chip. Errors go in the manifest as well, because a mark that
matters must survive a reader who never loads the journal, and because `atr record list` can
then say that a session has failures in it.

**Repeats collapse.** Identical messages inside one second make one mark that carries a
count, shown as `TypeError: … (×50)`. A React error loop fires fifty times in a moment, and
fifty marks in one place hide every other mark on the bar. The journal still keeps all fifty
lines, so nothing is lost, and the dock still lists them.

A `console.log` never becomes a mark. This is the same rule that made a typing burst one
mark: the row is a map, and a map that shows everything shows nothing.

### The gap this design accepts

The tap covers the streamed tab. Two cases lose rows, and both are worth naming:

1. An OAuth popup or a background worker tab is never logged while it is not the streamed
   tab.
2. When the foreground policy is `follow` and the stream moves, the tap moves with it. The
   old tab stops being logged from that moment.

The tap writes a `tap` line whenever it moves, carrying the target it left and the target it
joined. The dock draws it as a divider that says *the log follows the streamed tab*. A
visible gap is honest. A silent one is a bug report.

## 9. Privacy

The recorder already refuses to write what somebody typed. This must not undo that.

- **No bodies.** A login post carries the password in its body.
- **No headers.** They carry `Cookie` and `Authorization`.
- **URLs are stored whole.** A URL can carry a token in its query string. The manifest
  already stores whole URLs for `nav` events, so this is not a new exposure, but it is a
  real one. The dock truncates the query when it draws a row, and `--redact-query` strips
  it from the disk as well.
- **`--redact-query` also cleans the prose.** Chrome names a blocked request inside the
  message it writes about it, so a flag that only cleaned the `url` field would leave the
  token in the text beside it. The tap takes the query off every URL it finds in the text
  and in the stack, not only off the field.
- The manifest records which of these were on, so a person who reads a recording later knows
  what is in it.

## 10. Volume

A busy single page application makes thousands of requests, and a broken one makes thousands
of errors in a minute. Three limits, all of the same kind the frame ring already uses:

| Limit | Default | What happens at it |
|---|---|---|
| Live ring | 2000 rows | The oldest row leaves. A viewer who joins late gets the ring. |
| `--max-log-size` | 20 MB | The journal stops growing, `dropped` counts what was refused. |
| Rate cap | 200 lines a second | Lines above it are dropped and counted. An error loop cannot fill a disk. |

The dropped count is in the manifest and on the dock, because a truncated log that does not
say it is truncated is worse than no log.

## 11. Files

**New**

```
internal/remote/devtools.go        the tap: CDP events to LogEvent
internal/remote/devtools_test.go
internal/record/devtools.go        the journal writer and its limits
internal/record/devtools_test.go
web/src/DevTools.tsx               the dock
web/src/devtools.ts                the join on reqId, and the filters
```

**Changed**

```
internal/remote/sink.go            the Logger interface
internal/remote/screencast.go      enable in stream(), disable in stop(), fan out
internal/remote/recording.go       RecorderSink.Log
internal/remote/hub.go             Hub.Log and the live ring
internal/remote/recordings_api.go  GET /api/recordings/{id}/devtools.jsonl
internal/record/recorder.go        Log(LogEvent), and the error marks
internal/record/types.go           the manifest devtools block, two event kinds
internal/record/store.go           repair folds the journal counts in
internal/cli/record.go             --max-log-size, --redact-query
web/src/protocol.ts                the devtools message and the manifest block
web/src/App.tsx  Player.tsx        the dock, and the playhead link
web/src/activity.ts                rank and label the two new kinds
web/src/styles.css                 the dock, and the danger marks
```

## 12. Phases

| Phase | Work | Result |
|---|---|---|
| 1 ✅ | The tap, `LogEvent`, the fan-out, `devtools.jsonl`, the manifest block, the limits | `atr record` produces a full log. No UI yet, and every part is testable in Go. |
| 2 ✅ | The `error` and `netfail` manifest events, the dedupe, the red marks | **The failures are on the timeline.** The mark row and its jump buttons already exist, so a recording made today becomes searchable for what went wrong. |
| 3 ✅ | The player dock, hide past the playhead, seek on click, scroll on scrub, the warning chip | The mark says *something broke here*, and the dock says what. |
| 4 ✅ | The WebSocket message, `Hub` ring, the live dock | You can watch an agent and see the request it just fired. |
| 5 | `atr record har`, then `--capture-bodies` and `--capture-headers` as opt-ins | The data leaves ATR, and the heavy capture becomes possible for the people who need it. |

Each phase ships on its own. Phases 1 and 2 together already answer the question that started
this, and they need no new UI component at all.

Phases 1 to 4 are built. Phase 5 is not started.

One thing the build changed. `--redact-query` now takes the query string out of the message
text and the stack as well as out of the `url` field. A live page proved the field alone was
not enough: Chrome reports a blocked request as prose, and the prose names the URL in full.

## 13. Test plan

- A fake tap drives a fixed list of `LogEvent`s into a `RecorderSink`. The journal must hold
  them in order, with `atMs` on the recording clock, and the manifest counts must match.
- A recording started 10 seconds into a session must stamp its first line at about 10 000 ms
  less than the wall clock says. This is the bug the two clocks exist to prevent.
- The rate cap: 1000 events in one second must produce 200 lines and a dropped count of 800.
- `--max-log-size` reached mid-recording must stop the journal and leave the frames alone.
- `atr record repair` on a killed recording must rebuild a manifest whose devtools counts
  match the journal on disk.
- A request with no response must appear as one `req` line, and the panel must draw it as
  pending.
- Every recording must carry a `devtools` block, and a recording of a page that did nothing
  must carry one with `"lines": 0`. There is no flag that can produce a recording without
  one.
- A `console.error`, an uncaught exception, an error-level log entry and a 500 must each
  produce one manifest event. A `console.log` and a 200 must produce none.
- The mark dedupe: 50 identical errors in 200 ms must produce one event with a count of 50.
- A warning must be in the journal and never in the manifest events. The player must draw
  it on the row only while the chip is on.
- `tsc -b`, and a manual pass on a page that throws, a page with a CORS block, a 404 on an
  image, and a slow XHR.

## 14. Risks

| Risk | Answer |
|---|---|
| `Network.enable` on a browser ATR does not own changes its behaviour, and there is now no flag to turn it off. | It is what DevTools itself does, and Chrome allows several clients. Body buffering is set to zero, so Chrome keeps less than an open DevTools window does. If a real page is ever found that this disturbs, the answer is to fix the tap, not to make the log optional. |
| The log and the video disagree about which tab they show. | The tap is bound to the streamed page, so they cannot disagree. When the stream moves, a `tap` line marks the seam. |
| A chatty page floods the WebSocket. | The rate cap runs before the fan-out, so it protects the socket and the disk together. |
| A URL in the log leaks a token. | Named in section 9, with `--redact-query` as the answer when somebody needs it. |
| The dock steals width from the frame. | The stage already scales the canvas to its container, so the picture shrinks and never crops. The dock is closed by default. |
| The join on `reqId` grows without bound in a long live session. | The live ring caps it at 2000 rows, and an unmatched `req` older than the ring is dropped with it. |

## 15. Confidence

**82 out of 100.**

Phase 1 is routine Go against events this repo already subscribes to in
`internal/browser/browser.go`, and I rate it 92. Phase 2 is 90: the mark row, the clustering
and the jump buttons are built and proved, so the work is two event kinds and a colour.
Phase 3 is the playhead link, which is the same clock arithmetic the mark row already does,
and I rate it 88. Phase 4 is 80: the live ring and a late joiner are easy to get subtly
wrong. Phase 5 is 70, because a body capture has to fetch each body before Chrome drops it,
and that timing is the part of CDP that disappoints most often.
