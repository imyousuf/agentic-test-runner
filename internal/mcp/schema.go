package mcp

// Schema reflection helpers. Tool input schemas are derived from the
// canonical Request structs in internal/ops, so that field names, JSON tags,
// `jsonschema:"required"` markers, and `jsonschema_description:"..."` tags
// stay the single source of truth across REST, MCP, and CLI surfaces.

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
)

// schemaFor reflects the given Go value's type into a JSON Schema and returns
// it as the map[string]any shape that Tool.InputSchema expects.
//
// Pass a pointer to a zero-valued instance of the Request struct (e.g.
// `schemaFor(&ops.ClickRequest{})`). For tools without arguments pass
// `&struct{}{}` to produce an empty object schema.
func schemaFor(v any) map[string]any {
	r := &jsonschema.Reflector{
		// Inline definitions; MCP schemas must be self-contained.
		DoNotReference: true,
		// Allow extra properties so we don't break older clients that send
		// fields the server hasn't seen yet.
		AllowAdditionalProperties: true,
		// Emit a top-level `{type:"object", properties:{...}}` rather than a
		// `$ref` into a definitions block.
		ExpandedStruct: true,
		// Honor `jsonschema:"required"` field tags.
		RequiredFromJSONSchemaTags: true,
	}
	s := r.Reflect(v)
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Errorf("mcp: marshal reflected schema: %w", err))
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		panic(fmt.Errorf("mcp: unmarshal reflected schema: %w", err))
	}
	// Drop the JSON Schema dialect URL — MCP clients don't need it and the
	// hand-written schemas didn't carry it either.
	delete(m, "$schema")
	delete(m, "$id")
	return m
}
