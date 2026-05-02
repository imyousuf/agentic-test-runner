package ops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"
)

// These tests pin the validation contract of the ops layer. They pass nil for
// *browser.Browser / *computer.Computer because every covered case must reject
// the request *before* touching the backend. If a future edit forgets a guard
// and lets a nil pointer through, the test will panic — which is louder than a
// silent regression.

// --- Shared MapToStruct round-trip ------------------------------------------

func TestMapToStruct_RoundTripsJSONTags(t *testing.T) {
	in := map[string]any{
		"selector":     "#submit",
		"double_click": true,
		"ignored":      "extra keys are silently dropped",
	}
	var got ClickRequest
	if err := MapToStruct(in, &got); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	if got.Selector != "#submit" || got.DoubleClick != true {
		t.Fatalf("decoded = %+v, want Selector=#submit DoubleClick=true", got)
	}
}

// --- Browser ops validation -------------------------------------------------

func TestBrowserOps_RequiredFieldsRejected(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		run     func() (any, error)
		wantSub string
	}{
		{"navigate empty URL", func() (any, error) {
			return Navigate(ctx, nil, NavigateRequest{})
		}, "url is required"},

		{"click empty selector", func() (any, error) {
			return Click(ctx, nil, ClickRequest{})
		}, "selector is required"},

		{"fill empty selector", func() (any, error) {
			return Fill(ctx, nil, FillRequest{Value: "x"})
		}, "selector is required"},

		{"hover empty selector", func() (any, error) {
			return Hover(ctx, nil, HoverRequest{})
		}, "selector is required"},

		{"press_key empty key", func() (any, error) {
			return PressKey(ctx, nil, PressKeyRequest{})
		}, "key is required"},

		{"drag missing endpoints", func() (any, error) {
			return Drag(ctx, nil, DragRequest{From: "a"})
		}, "to are required"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.run()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %q, want substring %q", err.Error(), c.wantSub)
			}
		})
	}
}

// --- Computer ops validation ------------------------------------------------

func TestComputerOps_RegionScreenshotRejectsZeroSize(t *testing.T) {
	_, err := ComputerScreenshot(context.Background(), nil, ComputerScreenshotRequest{
		Region: true,
		Width:  0,
		Height: 100,
	})
	if err == nil {
		t.Fatal("expected error for region without width")
	}
	if !strings.Contains(err.Error(), "width") || !strings.Contains(err.Error(), "height") {
		t.Errorf("err = %q, want mention of width and height", err.Error())
	}
}

// --- MapToStruct with pointer / optional fields -----------------------------

// ComputerClickRequest.Display is *int specifically so callers can distinguish
// "no display field" (root coords) from "display 0" (display-local coords on
// the primary monitor). Round-trip both shapes through MapToStruct to make
// sure that distinction survives — a regression here breaks multi-monitor
// click semantics.
func TestMapToStruct_DistinguishesAbsentFromZeroPointer(t *testing.T) {
	var absent ComputerClickRequest
	if err := MapToStruct(map[string]any{"x": 10, "y": 20}, &absent); err != nil {
		t.Fatalf("MapToStruct (absent display): %v", err)
	}
	if absent.Display != nil {
		t.Errorf("absent display should decode to nil pointer, got *int = %d", *absent.Display)
	}

	var explicit ComputerClickRequest
	if err := MapToStruct(map[string]any{"x": 10, "y": 20, "display": 0}, &explicit); err != nil {
		t.Fatalf("MapToStruct (explicit 0): %v", err)
	}
	if explicit.Display == nil {
		t.Fatal("explicit display=0 should decode to non-nil pointer")
	}
	if *explicit.Display != 0 {
		t.Errorf("explicit display = %d, want 0", *explicit.Display)
	}
}

// --- Result struct field shapes --------------------------------------------

// computer.NoDisplay (-1) is an internal sentinel used by the underlying
// primitive to mean "the request didn't specify a display". It must never
// appear in JSON responses — that would surface a meaningless "-1" to MCP /
// REST clients. The result structs use *int with omitempty so an absent
// display is rendered as no field at all, while an explicit display (e.g. 0,
// 1, 2) round-trips intact.
func TestComputerClickResult_OmitsDisplayWhenAbsent(t *testing.T) {
	t.Run("absent → field omitted", func(t *testing.T) {
		raw, err := json.Marshal(ComputerClickResult{X: 10, Y: 20, Display: nil})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), "display") {
			t.Errorf("display field leaked into JSON for nil display: %s", raw)
		}
		if strings.Contains(string(raw), "-1") {
			t.Errorf("NoDisplay sentinel leaked into JSON: %s", raw)
		}
	})

	t.Run("explicit 0 → field present", func(t *testing.T) {
		zero := 0
		raw, err := json.Marshal(ComputerClickResult{X: 10, Y: 20, Display: &zero})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), `"display":0`) {
			t.Errorf("explicit display=0 should appear in JSON, got: %s", raw)
		}
	})
}

// --- Field-name regression: legacy keys must NOT decode as the canonical -----

// A common refactor accident is to silently re-accept the old field name. The
// json decoder ignores unknown keys, so feeding `{"target":"x"}` into a
// ClickRequest leaves Selector empty — and the validation guard then catches
// it. Pin both halves of that contract: the legacy key is dropped, AND the
// ops function rejects the empty-Selector request.
func TestClickRequest_LegacyTargetKeyIsDropped(t *testing.T) {
	var req ClickRequest
	if err := MapToStruct(map[string]any{"target": "#submit"}, &req); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	if req.Selector != "" {
		t.Fatalf("legacy target key should not populate Selector, got %q", req.Selector)
	}
	if _, err := Click(context.Background(), nil, req); err == nil ||
		!strings.Contains(err.Error(), "selector is required") {
		t.Fatalf("Click should reject empty Selector, got err=%v", err)
	}
}

// --- Schema reflection contract --------------------------------------------

// MCP tool inputSchemas are reflected from these Request structs. The MCP
// server's Reflector config (DoNotReference, ExpandedStruct,
// RequiredFromJSONSchemaTags, AllowAdditionalProperties) decides whether
// `required` arrays and field descriptions actually populate. If a future
// upgrade of invopop/jsonschema changes those defaults, every MCP client
// would silently see broken schemas. Assert the contract here so the
// regression fails the build instead.
func TestSchemaReflection_ClickRequestContract(t *testing.T) {
	r := &jsonschema.Reflector{
		DoNotReference:             true,
		AllowAdditionalProperties:  true,
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: true,
	}
	raw, err := json.Marshal(r.Reflect(&ClickRequest{}))
	if err != nil {
		t.Fatalf("marshal reflected schema: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := s["type"]; got != "object" {
		t.Errorf("type = %v, want object", got)
	}

	required, _ := s["required"].([]any)
	if len(required) != 1 || required[0] != "selector" {
		t.Errorf("required = %v, want [selector]", required)
	}

	props, _ := s["properties"].(map[string]any)
	selProp, _ := props["selector"].(map[string]any)
	if selProp["description"] == "" || selProp["description"] == nil {
		t.Errorf("selector property missing description; got %+v", selProp)
	}
	dcProp, _ := props["double_click"].(map[string]any)
	if dcProp["type"] != "boolean" {
		t.Errorf("double_click type = %v, want boolean", dcProp["type"])
	}

	// Field-name regression: legacy "target" must NOT appear as a property.
	if _, leaked := props["target"]; leaked {
		t.Error("schema leaks legacy 'target' property — field-name normalization regressed")
	}
}

// Several computer ops use *int for display so callers can distinguish
// "no display" from "display 0". The struct tags carry json:"display,omitempty"
// and a jsonschema_description. Some Reflector configurations drop pointer
// fields with `omitempty` from the emitted `properties` entirely — which
// would silently hide the display field from MCP clients. Pin that the field
// IS reflected and IS NOT marked required.
func TestSchemaReflection_PointerOmitemptyDisplayIsReflected(t *testing.T) {
	r := &jsonschema.Reflector{
		DoNotReference:             true,
		AllowAdditionalProperties:  true,
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: true,
	}
	for _, tc := range []struct {
		name string
		v    any
	}{
		{"ComputerClickRequest", &ComputerClickRequest{}},
		{"ComputerScreenshotRequest", &ComputerScreenshotRequest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(r.Reflect(tc.v))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var s map[string]any
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			props, _ := s["properties"].(map[string]any)
			disp, ok := props["display"].(map[string]any)
			if !ok {
				t.Fatalf("schema missing 'display' property; properties=%v", props)
			}
			if disp["description"] == "" || disp["description"] == nil {
				t.Errorf("display property missing description; got %+v", disp)
			}
			required, _ := s["required"].([]any)
			for _, r := range required {
				if r == "display" {
					t.Error("display should not be required (it's optional via *int)")
				}
			}
		})
	}
}
