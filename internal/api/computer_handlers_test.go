package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/computer"
)

// newTestComputerServer builds a ComputerServer with mode=off so the gate
// doesn't block, suitable for unit tests of the routing/wiring.
func newTestComputerServer(t *testing.T) *ComputerServer {
	t.Helper()
	c, err := computer.New(computer.Config{
		CountdownMode:    computer.ModeOff,
		CountdownSeconds: 0,
	})
	if err != nil {
		t.Fatalf("computer.New: %v", err)
	}
	s := &ComputerServer{
		computer: c,
		mux:      http.NewServeMux(),
		mode:     computer.ModeOff,
	}
	s.registerComputerRoutes()
	return s
}

func doRequest(t *testing.T, srv *ComputerServer, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	resp := rr.Result()
	defer resp.Body.Close()
	bytes, _ := io.ReadAll(resp.Body)
	return resp, bytes
}

func decodeAPI(t *testing.T, b []byte) APIResponse {
	t.Helper()
	var r APIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, string(b))
	}
	return r
}

func TestComputerHealthGET(t *testing.T) {
	srv := newTestComputerServer(t)
	resp, body := doRequest(t, srv, http.MethodGet, "/api/v1/computer/health", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
	}
	r := decodeAPI(t, body)
	if !r.Success {
		t.Errorf("expected success, got error: %s", r.Error)
	}
	data, _ := r.Data.(map[string]any)
	if got, _ := data["mode"].(string); got != "off" {
		t.Errorf("mode = %q, want \"off\"", got)
	}
}

func TestComputerHealthRejectsPOST(t *testing.T) {
	srv := newTestComputerServer(t)
	resp, _ := doRequest(t, srv, http.MethodPost, "/api/v1/computer/health", "")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestComputerResetApprovals(t *testing.T) {
	srv := newTestComputerServer(t)
	resp, body := doRequest(t, srv, http.MethodPost, "/api/v1/computer/approvals/clear", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
	}
	r := decodeAPI(t, body)
	if !r.Success {
		t.Errorf("expected success, got error: %s", r.Error)
	}
}

func TestComputerClickRequiresValidJSON(t *testing.T) {
	srv := newTestComputerServer(t)
	// Non-JSON body
	resp, _ := doRequest(t, srv, http.MethodPost, "/api/v1/computer/click", "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestComputerScrollRequiresMethod(t *testing.T) {
	srv := newTestComputerServer(t)
	resp, _ := doRequest(t, srv, http.MethodGet, "/api/v1/computer/scroll", "")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestComputerKeyRejectsEmptyKey(t *testing.T) {
	srv := newTestComputerServer(t)
	resp, body := doRequest(t, srv, http.MethodPost, "/api/v1/computer/key", `{"key":""}`)
	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected non-200 for empty key, got 200 (body: %s)", string(body))
	}
}
