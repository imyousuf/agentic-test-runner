package ops

import (
	"context"
	"strings"
	"testing"
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
