// Package ops contains the canonical execution layer for ATR's browser and
// computer primitives. Each primitive is one Request struct + one Result
// struct + one function that calls into internal/browser or internal/computer.
//
// REST handlers and MCP dispatchers are thin adapters that decode their
// protocol into the canonical Request, call the function, and format the
// Result for their wire protocol. The same Request structs also drive MCP
// inputSchema generation via reflection (see internal/mcp/tools.go).
package ops

import (
	"encoding/json"
	"fmt"
)

// MapToStruct decodes an MCP-style map[string]any argument bag into a typed
// request struct, using the struct's json tags. Round-trips through JSON so
// the same struct tags REST uses also drive MCP decoding — and there's a
// single source of truth for field naming.
//
// Unknown keys in the map are silently ignored, matching the permissive
// behavior of json.Decoder used by REST handlers.
func MapToStruct(m map[string]any, v any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("ops: encode map: %w", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("ops: decode into struct: %w", err)
	}
	return nil
}
