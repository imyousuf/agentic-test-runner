# Changelog

## Unreleased

### Breaking

Browser and computer primitives now share canonical request structs in
`internal/ops`, and the field-name drift between the REST and MCP surfaces
has been normalized.

REST callers must update the request body field names below. MCP clients are
unaffected — schemas are introspected by the client.

| Endpoint                 | Old field       | New field       |
| ------------------------ | --------------- | --------------- |
| `POST /click`            | `target`        | `selector`      |
| `POST /click`            | (MCP) `double`  | `double_click`  |
| `POST /fill`             | `target`        | `selector`      |
| `POST /hover`            | `target`        | `selector`      |
| `POST /computer/click`   | `double`        | `double_click`  |

The bundled CLI (`atr browser ...`, `atr computer ...`) and batch dispatcher
(`atr browser batch`) are updated in the same release; users invoking the
CLI need no changes.

### Added

- `internal/ops` package: a single source of truth for browser and computer
  primitive request/result types and execution functions. REST and MCP
  handlers now decode their protocol into the same struct, call the same
  function, and format the same result. Adding a new primitive is one struct
  + one function instead of four-place edits.

- **Shared operations, hoisted automatically.** A run finds the sequences its
  specs keep repeating, names them in `_shared.js`, rewrites the scripts to
  call them, and proves the rewrites before keeping them. Nothing is kept
  unless the library declares operations only, still declares everything it
  declared before, and every rewritten script claims exactly what it claimed
  before and still passes against the live application — otherwise every file
  goes back as it was. `atr refactor-ops <dir>` runs it on demand,
  `--dry-run` reports without a browser or a model, `--no-extract` and
  `behavior.extract_operations: always|on-demand|off` turn it down. Under
  `--no-compile` it only ever reports, so a CI replay never leaves a modified
  working tree.

- A compile is shown the directory's already-compiled scripts, so two specs
  that reach the same page use the same selector and the same constant name
  instead of each inventing their own.

- **Execution history.** Every run is recorded to `~/.atr/history.db`;
  `atr history` reports pass rate, test-failure rate against infrastructure
  rate, repairs and median replay duration, per spec. `--json` for machines.
  `history.enabled`, `history.path`, `history.keep_days` (default 90).

- **OpenTelemetry export**, when `OTEL_EXPORTER_OTLP_ENDPOINT` or
  `--otel-endpoint` is set: metrics with bounded dimensions, a span tree of
  run → compile → attempt → step, and failure messages as logs correlated by
  span. Inert without an endpoint.

- **A lint over compiled scripts**, for the ways one can pass without testing
  anything: a step that cannot fail, a script that asserts nothing, an
  assertion swallowed by a catch, a short match against whole-page text, a
  fixed sleep, and a script that declares an operation of its own instead of
  sharing it. Blocking findings exit 2 — the application was never tested.
  `--lint error|warn|off`. The lint never calls the model.

- `atr.expectExists` and `atr.expectMissing`: assertions that wait, replacing
  `expect(atr.exists(x)).toBeTruthy()`, which gave the lookup a 500ms budget
  and then reported a slow render as a broken application — and, in the
  absence direction, passed when the element was merely late.

- `_shared.js` beside the specs, evaluated into the same VM before the script
  and shown verbatim to the compile and triage prompts. `expect` and
  `atr.fail` are refused from a library frame. Editing it does not force a
  recompile: scripts carry a second `atr-lib-sha256` header and replay to
  catch up.

- `skills/atr-author`: how to write a spec that cannot pass while the
  application is broken. `skills/atr-behavior` narrowed to operating.

### Fixed

- **One target grammar for every call.** `atr.text`, `atr.expectText` and
  `atr.scroll` — and the `text`, `scroll`, `screenshot`, `computed-styles`,
  `clean-snapshot` and `download-images` commands behind them — refused an
  XPath as an invalid selector, although `atr.click` and `atr.waitFor` took the
  same one and the compile prompt promises "CSS or XPath". A compiled script
  that read through the XPath it had just clicked with failed its first replay
  as a script fault and was sent for repair. Every target-taking call now
  resolves CSS, XPath and `:has-text()` through the same code. An XPath that
  does not parse is reported as an invalid selector — a script fault — instead
  of as an environment failure to be retried. (#29)
- **A `:has-text()` target is waited for.** It was looked up once, at the
  moment of the call, so `atr.click`, `atr.waitFor` and `atr.expectExists`
  failed within milliseconds on an element that was still rendering, whatever
  timeout they had been given — where the same element named by CSS or XPath
  was polled for. `expectExists` reported that as an assertion failure, which
  blames the application and is never retried, with a message claiming a wait
  that had not happened ("it was not there after 30s", from a 1.7s run). The
  lookup now polls until the caller's budget is spent, in one round trip per
  poll. `expectExists`, `expectMissing` and `waitFor` report the time they
  actually waited instead of the timeout they were handed. (#30)
- **A hand-written script is left alone.** Removing the `atr-spec-sha256`
  line is the documented way to keep a script as your own, but a plain run read
  "no hash" as "the spec changed": it spent a compile and overwrote the file,
  and `--no-compile` refused to run it at all, calling it stale. A script with
  no hash line is now replayed as it stands in both modes, whatever the spec
  says. Nothing writes to it: not a compile, not a repair (the diagnosis is
  still reported; the proposed rewrite is discarded), not a library stamp, and
  not the hoist, which no longer considers it. `--recompile` remains the
  explicit way to replace one, and says that it is doing so. `--recompile`
  together with `--no-compile` is refused as a conflict of flags rather than as
  a stale script. A compiled script whose hash line was only pushed out of its
  header is not hand-written, and is compiled again as before. (#31)
- **A wait ends when the run does.** `atr.waitFor`, `atr.waitForText`,
  `atr.expectExists` and `atr.expectText` took the run's context and dropped
  it, so a wait asked for longer than the run had left held the run for the
  whole of it — a script with two seconds remaining sat in a sixty-second wait
  for all sixty, and Ctrl-C had to wait with it. They stop at the caller's
  deadline or cancellation now, and report the run's own timeout rather than a
  missing element.
- **A lookup looks through its whole budget.** How often a waiting lookup
  looked again was left to a backoff that looked at about 0.2s, 0.6s and 1.4s
  and then not until 3s. The three seconds a read such as `atr.text` gets were
  therefore really 1.4: something that rendered in the second half was
  reported missing, and the last second of any wait was blind. A lookup now
  looks at least every half second and once more just before it gives up.
  `atr.waitForText` is paced the same way.
- **Plain targets work in every call, not only in a click.** A target that is
  not a selector — visible text, an aria-label, a data-testid, a name, a
  placeholder — is read each of those ways, and each way used to wait a slice
  of the budget before the next was tried. The slices added up to more than the
  budget, so the later ways never had a turn: `atr.exists("Sign in")` was
  false with the button on the page, `atr.expectExists("Welcome")` reported a
  heading that was there all along as an assertion failure, and
  `atr.expectMissing("Sign in")` passed. A click on visible text worked, after
  two seconds spent on the four attributes tried before it. Every way is now
  tried on every look. The ways that match text in part join half way through
  the budget (after a second at most), so that an exact match a render away is
  not beaten by a partial one already on the page.
- **Text in part is read from what the page shows, as written.** It used to
  match the source of inline scripts, and — being read as a regular expression
  — almost anything: a selector that had not rendered yet was a pattern that
  matched some word on most pages. It is now matched literally against what the
  body renders and the values its fields show, and resolves to the smallest
  element showing it, where it used to resolve to the root element, so a click
  on it landed in the middle of the viewport. A target written as a selector
  (`a[href="/logout"]`, `dialog`) is never matched as part of some text. Exact
  text no longer matches the `<title>`, which cannot be clicked: that click
  waited thirty seconds. A target that relied on being a pattern
  (`Sign (in|up)`) no longer matches.
- **`atr.waitForText` waits for the text to be shown.** It matched the source
  of inline scripts, so on a page whose script contains the words it will later
  display — most pages — the wait returned at once.
- A snapshot UID is `e` and a number and nothing else. Text beginning that
  way, such as `e2e suite`, was read as element 2.
- **Drag works.** `atr browser drag`, `POST /drag` and the `browser_drag` tool
  failed for every pair of elements with `from.getBoundingClientRect is not a
  function`: both elements were found and then handed to the page as JSON.
- **A batch of selectors may contain commas.** The repeatable `--selector`
  flag of `atr browser computed-styles` and `computed-styles-diff` joined its
  values with commas and the daemon split them on commas, so a selector
  containing one — `contains(., "x")` in an XPath, a CSS selector list — was
  cut in two and both halves reported as unmatched. The batch is sent as a
  JSON body now: `POST /computed-styles` and `POST /computed-styles-diff` take
  the request as JSON, with `selectors` and `properties` as arrays. The `GET`
  form with `selectors=a,b` still works. A newer CLI against an older daemon gets `method not allowed`
  rather than a wrong answer.
- **A hand-written script is triaged against itself.** When one failed, the
  triage prompt still asked whether the application did "what the
  specification requires" — but a hand-written script is replayed whatever the
  spec says, so the two may not agree. A drifted script beside a spec that
  asked for more could be given the verdict that the application was broken,
  which is terminal and exits `1`. The prompt now says the script is what the
  test checks and the spec is context. It also stops asking for a rewritten
  script that nothing would apply; the reason carries what moved.
- `atr.waitFor` on a selector the browser cannot parse is a script fault, as
  it already was for `atr.click` and `atr.exists`. It was reported as a
  timeout, which is retried — every retry failing the same way — before
  anything looked at the script; with `{visible: true}` it also spent the whole
  timeout first.
- Computer click/move/drag/hover responses no longer leak the internal
  `NoDisplay` sentinel (`-1`) through the `display` field. The field is now
  omitted entirely when the request didn't specify a display, and round-trips
  the explicit value otherwise. Result shape changed from `int` to optional
  `*int` (omitted via `omitempty`).
- MCP server now responds with an empty success result when a client sends
  `notifications/initialized` (or the legacy `initialized`) with an `id`,
  rather than silently dropping it. Notifications without an `id` continue
  to receive no response, per JSON-RPC 2.0.
