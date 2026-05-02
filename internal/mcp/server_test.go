package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// JSON-RPC 2.0 distinguishes notifications (no id) from requests (id present)
// by the presence of "id". The server MUST NOT respond to a notification, but
// MUST respond to a request — even if the method is conventionally a
// notification (some clients are quirky). Pin all four cells of that matrix
// for `notifications/initialized` so a future refactor doesn't silently
// regress to either dropping requests or replying to notifications.
func TestHandleRequest_InitializedNotificationVsRequest(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		id         any
		wantOutput bool
	}{
		{"new spec name as notification (no id)", "notifications/initialized", nil, false},
		{"new spec name as request (id present)", "notifications/initialized", 7, true},
		{"legacy name as notification (no id)", "initialized", nil, false},
		{"legacy name as request (id present)", "initialized", 9, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := &Server{writer: &buf}
			s.handleRequest(context.Background(), &Request{
				JSONRPC: "2.0",
				Method:  c.method,
				ID:      c.id,
			})

			got := strings.TrimSpace(buf.String())
			if !c.wantOutput {
				if got != "" {
					t.Fatalf("notification should produce no output, got: %s", got)
				}
				return
			}

			if got == "" {
				t.Fatal("request with id must receive a response, got nothing")
			}
			var resp Response
			if err := json.Unmarshal([]byte(got), &resp); err != nil {
				t.Fatalf("unmarshal response: %v (raw: %s)", err, got)
			}
			if resp.Error != nil {
				t.Errorf("expected success result, got error: %+v", resp.Error)
			}
			if resp.JSONRPC != "2.0" {
				t.Errorf("jsonrpc = %q, want 2.0", resp.JSONRPC)
			}
		})
	}
}

// Unknown methods are still subject to the same notification/request rule.
// A bare "foo" with no id is a notification and MUST be silently dropped
// (per JSON-RPC 2.0 §4.1); the same method *with* id MUST get a -32601.
func TestHandleRequest_UnknownMethodHonoursNotificationRule(t *testing.T) {
	t.Run("unknown notification → silent", func(t *testing.T) {
		var buf bytes.Buffer
		s := &Server{writer: &buf}
		s.handleRequest(context.Background(), &Request{
			JSONRPC: "2.0",
			Method:  "totally/made/up",
		})
		if got := buf.String(); got != "" {
			t.Errorf("unknown notification should be silent, got: %s", got)
		}
	})

	t.Run("unknown request → -32601", func(t *testing.T) {
		var buf bytes.Buffer
		s := &Server{writer: &buf}
		s.handleRequest(context.Background(), &Request{
			JSONRPC: "2.0",
			Method:  "totally/made/up",
			ID:      42,
		})
		var resp Response
		if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v (raw: %s)", err, buf.String())
		}
		if resp.Error == nil {
			t.Fatal("expected method-not-found error, got nil")
		}
		if resp.Error.Code != -32601 {
			t.Errorf("code = %d, want -32601", resp.Error.Code)
		}
	})
}
