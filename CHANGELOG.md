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

- Computer click/move/drag/hover responses no longer leak the internal
  `NoDisplay` sentinel (`-1`) through the `display` field. The field is now
  omitted entirely when the request didn't specify a display, and round-trips
  the explicit value otherwise. Result shape changed from `int` to optional
  `*int` (omitted via `omitempty`).
- MCP server now responds with an empty success result when a client sends
  `notifications/initialized` (or the legacy `initialized`) with an `id`,
  rather than silently dropping it. Notifications without an `id` continue
  to receive no response, per JSON-RPC 2.0.
