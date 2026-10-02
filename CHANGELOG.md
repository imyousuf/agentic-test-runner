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
