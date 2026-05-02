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
